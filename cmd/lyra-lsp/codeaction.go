package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CodeAction implements textDocument/codeAction. It offers quick fixes derived
// from the diagnostics in the requested range — "Add missing match arms"
// (lyra-E009), "Add missing struct fields" (lyra-E013), "Remove unused
// variable/import" (lyra-W003/W004) — plus a range-driven "Insert inferred type
// annotation" refactor for unannotated `let`/`var` bindings.
func (h *Handler) CodeAction(_ context.Context, params *lsp.CodeActionParams) (result []lsp.CodeAction, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("codeAction panic: %v\n%s", r, debug.Stack())
			result, retErr = nil, nil
		}
	}()

	uri := params.TextDocument.URI
	h.mu.Lock()
	analysis, ok := h.analysisStore[string(uri)]
	source := h.docStore[string(uri)]
	h.mu.Unlock()
	if !ok {
		return nil, nil
	}

	var actions []lsp.CodeAction

	// structFieldsDone dedupes the per-field lyra-E013 diagnostics emitted for a
	// single struct literal into one "Add missing fields" action.
	structFieldsDone := make(map[ast.Location]bool)

	for _, d := range params.Context.Diagnostics {
		switch diagCode(d) {
		case diag.CodeNonExhaustiveMatch:
			if a := matchArmsAction(analysis, source, uri, d); a != nil {
				actions = append(actions, *a)
			}
		case diag.CodeMissingStructField:
			if a := structFieldsAction(analysis, source, uri, d, structFieldsDone); a != nil {
				actions = append(actions, *a)
			}
		case diag.CodeUnusedVariable:
			if a := removeUnusedVariableAction(analysis, source, uri, d); a != nil {
				actions = append(actions, *a)
			}
		case diag.CodeUnusedImport:
			if a := removeUnusedImportAction(analysis, source, uri, d); a != nil {
				actions = append(actions, *a)
			}
		}
	}

	actions = append(actions, insertTypeAnnotationActions(analysis, uri, params.Range)...)

	return actions, nil
}

// diagCode decodes the JSON-encoded diagnostic code (e.g. `"lyra-E009"`) into a
// plain string, returning "" when absent or non-string.
func diagCode(d lsp.Diagnostic) string {
	if len(d.Code) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(d.Code, &s); err != nil {
		return ""
	}
	return s
}

// matchArmsAction builds an "Add missing match arms" quick fix for the match
// expression the lyra-E009 diagnostic points at.
func matchArmsAction(analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic) *lsp.CodeAction {
	line, col := diagStart(d)
	m := findMatchAt(analysis.program, line, col)
	if m == nil {
		return nil
	}
	st, ok := analysis.typeTable.Get(m.Scrutinee)
	if !ok {
		return nil
	}
	dt, ok := resolveDataType(st, analysis.symTable)
	if !ok {
		return nil
	}
	missing := typechecker.MissingMatchConstructors(m.MatchArms, dt)
	if len(missing) == 0 {
		return nil
	}

	// Param counts let us emit `Ctor _` for constructors that carry a payload
	// (a single wildcard matches the whole payload) and bare `Ctor` otherwise.
	params := make(map[string]int, len(dt.Constructors))
	for _, c := range dt.Constructors {
		params[c.Name] = len(c.Params)
	}

	loc := m.GetLocation()
	indent := strings.Repeat(" ", loc.StartCol-1) + "    "
	var b strings.Builder
	if needsSeparatorComma(source, loc) {
		b.WriteString(",")
	}
	for _, name := range missing {
		b.WriteString("\n")
		b.WriteString(indent)
		b.WriteString(name)
		if params[name] > 0 {
			b.WriteString(" _")
		}
		b.WriteString(" => todo(),")
	}

	edit := lsp.TextEdit{Range: beforeClosingBrace(loc), NewText: b.String()}
	return quickFix(fmt.Sprintf("Add missing match arms: %s", strings.Join(missing, ", ")), uri, edit, d)
}

// structFieldsAction builds an "Add missing struct fields" quick fix for the
// struct literal the lyra-E013 diagnostic points at. Multiple per-field
// diagnostics on the same literal collapse into one action via done.
func structFieldsAction(analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic, done map[ast.Location]bool) *lsp.CodeAction {
	line, col := diagStart(d)
	s := findStructAt(analysis.program, line, col)
	if s == nil || done[s.GetLocation()] {
		return nil
	}

	decl, ok := analysis.symTable.Types[s.Name]
	if !ok {
		return nil
	}
	st, ok := decl.Type.(types.NamedStructType)
	if !ok {
		return nil
	}

	present := make(map[string]bool, len(s.Fields))
	for i, f := range s.Fields {
		name := f.Name
		if name == "" && i < len(st.Fields) {
			name = st.Fields[i].Name // positional field
		}
		present[name] = true
	}

	var missing []string
	for _, f := range st.Fields {
		if !present[f.Name] && f.DefaultValue == nil {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	done[s.GetLocation()] = true

	loc := s.GetLocation()
	var parts []string
	for _, name := range missing {
		parts = append(parts, fmt.Sprintf("%s: todo()", name))
	}
	text := strings.Join(parts, ", ")
	if needsSeparatorComma(source, loc) {
		text = ", " + text
	} else {
		// First field inside the braces — add a space if the brace is bare.
		text = " " + text
	}

	edit := lsp.TextEdit{Range: beforeClosingBrace(loc), NewText: text}
	return quickFix(fmt.Sprintf("Add missing fields: %s", strings.Join(missing, ", ")), uri, edit, d)
}

// removeUnusedVariableAction deletes the whole declaration statement the
// lyra-W003 diagnostic points at.
func removeUnusedVariableAction(analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic) *lsp.CodeAction {
	line, col := diagStart(d)
	decl := findVarDeclAt(analysis.program, line, col)
	if decl == nil {
		return nil
	}
	edit := lsp.TextEdit{Range: deleteLinesRange(source, decl.GetLocation()), NewText: ""}
	return quickFix(fmt.Sprintf("Remove unused variable %q", decl.Name), uri, edit, d)
}

// removeUnusedImportAction deletes an unused import. It only fires when the
// whole statement can be removed safely: a plain/alias import, or a member
// import whose single member is the unused one. Multi-member imports are left
// alone (removing one member needs comma surgery we don't attempt).
func removeUnusedImportAction(analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic) *lsp.CodeAction {
	line, col := diagStart(d)
	imp := findImportContaining(analysis.program, line, col)
	if imp == nil || len(imp.Members) > 1 {
		return nil
	}
	edit := lsp.TextEdit{Range: deleteLinesRange(source, imp.GetLocation()), NewText: ""}
	return quickFix("Remove unused import", uri, edit, d)
}

// insertTypeAnnotationActions offers "Insert inferred type annotation" for each
// unannotated `let`/`var` binding whose declaration falls in the request range
// and whose value type has been inferred.
func insertTypeAnnotationActions(analysis *docAnalysis, uri lsp.DocumentURI, r lsp.Range) []lsp.CodeAction {
	var out []lsp.CodeAction
	for _, decl := range collectVarDecls(analysis.program) {
		if decl.Type != nil || !inRange(decl.GetLocation(), r) {
			continue
		}
		typ, ok := analysis.typeTable.Get(decl.Value)
		if !ok {
			continue
		}
		loc := decl.GetLocation()
		// Column just after the binding name: keyword (+ " mut") + space + name.
		col := lspPos(loc.StartCol) + len(decl.BindingKind.String()) + 1 + len(decl.Name)
		if decl.IsMut && decl.BindingKind == ast.BindingLet {
			col += len(" mut")
		}
		pos := lsp.Position{Line: lspPos(loc.StartLine), Character: col}
		edit := lsp.TextEdit{
			Range:   lsp.Range{Start: pos, End: pos},
			NewText: fmt.Sprintf(": %s", typ),
		}
		kind := lsp.CodeActionRefactorRewrite
		out = append(out, lsp.CodeAction{
			Title: fmt.Sprintf("Insert inferred type annotation: %s", typ),
			Kind:  &kind,
			Edit:  &lsp.WorkspaceEdit{Changes: map[lsp.DocumentURI][]lsp.TextEdit{uri: {edit}}},
		})
	}
	return out
}

// quickFix wraps a single text edit in a quickfix CodeAction bound to the
// originating diagnostic.
func quickFix(title string, uri lsp.DocumentURI, edit lsp.TextEdit, d lsp.Diagnostic) *lsp.CodeAction {
	kind := lsp.CodeActionQuickFix
	return &lsp.CodeAction{
		Title:       title,
		Kind:        &kind,
		Diagnostics: []lsp.Diagnostic{d},
		Edit:        &lsp.WorkspaceEdit{Changes: map[lsp.DocumentURI][]lsp.TextEdit{uri: {edit}}},
	}
}

// diagStart returns the 1-based line/col of a diagnostic's start, matching the
// 1-based ast.Location coordinates used throughout the analyzer.
func diagStart(d lsp.Diagnostic) (int, int) {
	return d.Range.Start.Line + 1, d.Range.Start.Character + 1
}

// beforeClosingBrace returns a zero-width range at the position immediately
// before the closing `}` of a brace-delimited node. ast.Location end columns are
// exclusive (one past the last char), so the brace sits at EndCol-1 (1-based),
// i.e. character EndCol-2 (0-based).
func beforeClosingBrace(loc ast.Location) lsp.Range {
	pos := lsp.Position{Line: lspPos(loc.EndLine), Character: loc.EndCol - 2}
	if pos.Character < 0 {
		pos.Character = 0
	}
	return lsp.Range{Start: pos, End: pos}
}

// needsSeparatorComma reports whether new content inserted before a node's
// closing brace must be prefixed with a comma to separate it from existing
// content. It scans back over whitespace from the brace: a separator is needed
// unless the last meaningful char is already a `,` or the opening `{`.
func needsSeparatorComma(source string, loc ast.Location) bool {
	off := posToOffset(source, lspPos(loc.EndLine), loc.EndCol-2)
	for i := off - 1; i >= 0; i-- {
		c := source[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		return c != ',' && c != '{'
	}
	return false
}

// deleteLinesRange returns a range covering every line spanned by loc, from the
// start of its first line through the start of the line after its last — so the
// statement and its trailing newline are removed cleanly.
func deleteLinesRange(source string, loc ast.Location) lsp.Range {
	start := lsp.Position{Line: lspPos(loc.StartLine), Character: 0}
	end := lsp.Position{Line: lspPos(loc.EndLine) + 1, Character: 0}
	// If the last line has no trailing newline (EOF), clamp to end-of-line so we
	// don't reference a non-existent line.
	if posToOffset(source, end.Line, 0) >= len(source) {
		end = lsp.Position{Line: lspPos(loc.EndLine), Character: loc.EndCol}
	}
	return lsp.Range{Start: start, End: end}
}

// resolveDataType returns the DataType underlying t, following an UnresolvedType
// name through the symbol table. Returns (_, false) when t is not a data type.
func resolveDataType(t types.Type, symTable *symbols.SymbolTable) (types.DataType, bool) {
	if t == nil {
		return types.DataType{}, false
	}
	if dt, ok := t.(types.DataType); ok {
		return dt, true
	}
	if u, ok := t.(types.UnresolvedType); ok {
		if decl, exists := symTable.Types[u.Name]; exists {
			if dt, ok := decl.Type.(types.DataType); ok {
				return dt, true
			}
		}
	}
	return types.DataType{}, false
}

// --- AST collection / lookup helpers ---

func findMatchAt(program *ast.Program, line, col int) *ast.MatchExpr {
	var found *ast.MatchExpr
	walkExprs(program, func(e ast.Expression) {
		if m, ok := e.(*ast.MatchExpr); ok && startsAt(m.GetLocation(), line, col) {
			found = m
		}
	})
	return found
}

func findStructAt(program *ast.Program, line, col int) *ast.StructInstanceExpr {
	var found *ast.StructInstanceExpr
	walkExprs(program, func(e ast.Expression) {
		if s, ok := e.(*ast.StructInstanceExpr); ok && startsAt(s.GetLocation(), line, col) {
			found = s
		}
	})
	return found
}

func findVarDeclAt(program *ast.Program, line, col int) *ast.VarDeclStmt {
	var found *ast.VarDeclStmt
	for _, d := range collectVarDecls(program) {
		if startsAt(d.GetLocation(), line, col) {
			found = d
		}
	}
	return found
}

func findImportContaining(program *ast.Program, line, col int) *ast.ImportStmt {
	for _, node := range program.Statements {
		if imp, ok := node.(*ast.ImportStmt); ok && containsPos(imp.GetLocation(), line, col) {
			return imp
		}
	}
	return nil
}

func collectVarDecls(program *ast.Program) []*ast.VarDeclStmt {
	var out []*ast.VarDeclStmt
	onStmt := func(s ast.Statement) bool {
		if d, ok := s.(*ast.VarDeclStmt); ok {
			out = append(out, d)
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, onStmt, nil)
		}
	}
	return out
}

// walkExprs visits every expression in the program.
func walkExprs(program *ast.Program, fn func(ast.Expression)) {
	onExpr := func(e ast.Expression) bool {
		fn(e)
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, onExpr)
		}
	}
}

// startsAt reports whether loc begins exactly at the given 1-based line/col.
// Diagnostics are emitted at their node's start, so this pinpoints the node.
func startsAt(loc ast.Location, line, col int) bool {
	return loc.StartLine == line && loc.StartCol == col
}
