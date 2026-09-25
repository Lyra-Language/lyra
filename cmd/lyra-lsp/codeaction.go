package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CodeAction implements textDocument/codeAction. It offers quick fixes derived
// from the diagnostics in the requested range — "Add missing match arms"
// (lyra-E009), "Add missing struct fields" (lyra-E013), "Remove unused
// variable/import" (lyra-W003/W004), "Import x from lib" for a name a known module
// exports — plus a range-driven "Insert inferred type annotation" refactor for
// unannotated `let`/`var` bindings.
func (h *Handler) CodeAction(_ context.Context, params *lsp.CodeActionParams) (result []lsp.CodeAction, retErr error) {
	defer recoverHandler("codeAction", &result, &retErr)

	uri := params.TextDocument.URI
	// **Logged because its absence is a question we could not answer.** When a code
	// action does not appear, the two explanations — the client never asked, or the
	// server offered nothing — look identical from the editor, and only some handlers
	// here log their requests. A round trip was spent on exactly that ambiguity (09/25).
	log.Printf("codeAction: request at %s lines %d-%d with %d diagnostic(s)",
		uri, params.Range.Start.Line, params.Range.End.Line, len(params.Context.Diagnostics))
	analysis, source, ok := h.docFor(string(uri))
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
		default:
			// lyra-E001 is a general code, so the filter is what a module exports
			// rather than the code itself: an action appears only for a name some
			// module exports and this file has not imported.
			actions = append(actions, h.addImportActions(analysis, source, uri, d)...)
		}
	}

	actions = append(actions, insertTypeAnnotationActions(analysis, source, uri, params.Range)...)

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
	line, col := diagStart(source, d)
	m := findMatchAt(analysis.program, line, col)
	if m == nil {
		return nil
	}
	gaps, ok := typechecker.MatchGaps(analysis.symTable, analysis.scopeTable, analysis.typeTable, m)
	if !ok {
		return nil
	}
	// A partly covered constructor gets the same `Ctor _` arm as a missing one. Appended
	// after the arms that are there, it catches exactly the payloads they leave — which is
	// the only order that works, since before them it would shadow each one.
	missing := append(append([]string(nil), gaps.Missing...), gaps.Partial...)
	if len(missing) == 0 {
		return nil
	}
	dt := gaps.DataType

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

	edit := lsp.TextEdit{Range: beforeClosingBrace(source, loc), NewText: b.String()}
	return quickFix(fmt.Sprintf("Add missing match arms: %s", strings.Join(missing, ", ")), uri, edit, d)
}

// structFieldsAction builds an "Add missing struct fields" quick fix for the
// struct literal the lyra-E013 diagnostic points at. Multiple per-field
// diagnostics on the same literal collapse into one action via done.
func structFieldsAction(analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic, done map[ast.Location]bool) *lsp.CodeAction {
	line, col := diagStart(source, d)
	s := findStructAt(analysis.program, line, col)
	if s == nil || done[s.GetLocation()] {
		return nil
	}

	decl, ok := analysis.symTable.LookupTypeFrom(s.Name, s.GetLocation())
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

	edit := lsp.TextEdit{Range: beforeClosingBrace(source, loc), NewText: text}
	return quickFix(fmt.Sprintf("Add missing fields: %s", strings.Join(missing, ", ")), uri, edit, d)
}

// removeUnusedVariableAction deletes the whole declaration statement the
// lyra-W003 diagnostic points at.
func removeUnusedVariableAction(analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic) *lsp.CodeAction {
	line, col := diagStart(source, d)
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
	line, col := diagStart(source, d)
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
func insertTypeAnnotationActions(analysis *docAnalysis, source string, uri lsp.DocumentURI, r lsp.Range) []lsp.CodeAction {
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
		// Byte column just after the binding name: keyword (+ " mut") + space +
		// name; converted to a UTF-16 column for the edit position.
		byteCol := lspPos(loc.StartCol) + len(decl.BindingKind.String()) + 1 + len(decl.Name)
		if decl.IsMut && decl.BindingKind == ast.BindingLet {
			byteCol += len(" mut")
		}
		pos := lsp.Position{
			Line:      lspPos(loc.StartLine),
			Character: utf16Column(source, lspPos(loc.StartLine), byteCol),
		}
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

// diagStart returns the 1-based line and 1-based byte column of a diagnostic's
// start, matching the byte-based ast.Location coordinates used throughout the
// analyzer. The diagnostic's Character is a 0-based UTF-16 unit, converted to a
// byte column via source.
func diagStart(source string, d lsp.Diagnostic) (int, int) {
	return d.Range.Start.Line + 1, byteColumn(source, d.Range.Start.Line, d.Range.Start.Character)
}

// beforeClosingBrace returns a zero-width range at the position immediately
// before the closing `}` of a brace-delimited node. ast.Location end columns are
// exclusive (one past the last char), so the brace sits at EndCol-1 (1-based),
// i.e. byte column EndCol-2 (0-based), which source converts to UTF-16.
func beforeClosingBrace(source string, loc ast.Location) lsp.Range {
	byteCol := loc.EndCol - 2
	if byteCol < 0 {
		byteCol = 0
	}
	pos := lsp.Position{
		Line:      lspPos(loc.EndLine),
		Character: utf16Column(source, lspPos(loc.EndLine), byteCol),
	}
	return lsp.Range{Start: pos, End: pos}
}

// needsSeparatorComma reports whether new content inserted before a node's
// closing brace must be prefixed with a comma to separate it from existing
// content. It scans back over whitespace from the brace: a separator is needed
// unless the last meaningful char is already a `,` or the opening `{`.
func needsSeparatorComma(source string, loc ast.Location) bool {
	off := byteOffsetAt(source, lspPos(loc.EndLine), loc.EndCol-2)
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
	if byteOffsetAt(source, end.Line, 0) >= len(source) {
		end = lsp.Position{
			Line:      lspPos(loc.EndLine),
			Character: utf16Column(source, lspPos(loc.EndLine), lspPos(loc.EndCol)),
		}
	}
	return lsp.Range{Start: start, End: end}
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
	walkProgramExprs(program, func(e ast.Expression) bool {
		fn(e)
		return true
	})
}

// walkProgramExprs visits every expression in program, **the `const` bounds of range
// patterns included** (`LOW` in `LOW..<=HIGH`).
//
// Those are expressions no AST walk reaches: patterns are outside the expression walk, and
// the typechecker folds each bound to a literal in place, so the name survives only in
// RangePattern.ConstBounds. The position features all find a name through this walk, so
// without it renaming a const left every pattern bound naming one that no longer existed.
// They are fed to the callback as ordinary identifiers, which is what they were written as.
func walkProgramExprs(program *ast.Program, onExpr func(ast.Expression) bool) {
	bounds := func(n ast.AstNode) {
		for _, id := range ast.RangeBounds(n) {
			onExpr(id)
		}
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, func(s ast.Statement) bool {
				bounds(s)
				return true
			}, func(e ast.Expression) bool {
				bounds(e)
				return onExpr(e)
			})
		}
	}
}

// startsAt reports whether loc begins exactly at the given 1-based line/col.
// Diagnostics are emitted at their node's start, so this pinpoints the node.
func startsAt(loc ast.Location, line, col int) bool {
	return loc.StartLine == line && loc.StartCol == col
}

// addImportActions offers "Import x from lib" for a name the file uses and has not
// imported — one action per module that exports it.
//
// **Derived from the symbol table, not from the message.** The diagnostic already names
// the fix in prose ("module %q exports it, but this file does not import it; add `import
// …`"), and parsing that back out would tie a quick fix to a sentence someone will
// reword. `lyra-E001` is a general code, so the table is also what makes this specific:
// an action appears only when the name really is exported by a module this file does not
// list, which is the same question the diagnostic asked.
//
// Two shapes, because there are two right answers. A module the file **already imports**
// takes the name into its existing list, which is the common case and the one that keeps a
// file's imports from growing a second line per name. A module it mentions but does not
// take members from (`import lib`) gets a new line, placed after the last import — or
// after the `module` declaration, or at the top — so the file's shape stays what a reader
// expects, and the plain import keeps working beside it.
//
// **The boundary is what the program knows about.** `modules.Resolve` loads what a file
// imports and no more, so a module this file has never mentioned is not in the symbol
// table and cannot be offered — the compiler cannot name it either, and says only
// `undefined function "thrice"` where an imported module gets the whole "add `import …`"
// sentence. Offering those would mean the workspace search `importers.go` does for rename:
// a heavier thing, and a separate one.
func (h *Handler) addImportActions(
	analysis *docAnalysis, source string, uri lsp.DocumentURI, d lsp.Diagnostic,
) []lsp.CodeAction {
	if analysis.symTable == nil {
		return nil
	}
	line, col := diagStart(source, d)
	name, ok := unimportedNameAt(analysis, line, col)
	if !ok {
		return nil
	}
	var out []lsp.CodeAction
	for _, module := range h.modulesExporting(analysis, name) {
		edit, ok := importEdit(analysis.program, source, module, name)
		if !ok {
			continue
		}
		out = append(out, *quickFix(
			fmt.Sprintf("Import %s from %s", name, module), uri, edit, d))
	}
	return out
}

// modulesExporting answers every module that exports name: the ones already in the
// program, then — when none is — the workspace.
//
// **The second rung is the common case, which is why it exists.** The symbol table holds
// what this file's imports pulled in, so it can answer for a module already imported (a
// name missing from an otherwise-present list) and cannot answer at all for the mistake
// people actually make: using a name and forgetting the import entirely. There the module
// was never loaded, the compiler itself says only `undefined function "parse_args"` rather
// than naming a module, and a quick fix that stopped here would miss the case it exists
// for. Scoped to the imported half on 09/25 and widened the same day, after the first
// thing tried against it was `parse_args`.
//
// The walk is `importers.go`'s, for its reasons: resolution runs downward only, so an
// upward question has to be searched rather than resolved. Run on an explicit user action
// — a code-action request, never a keystroke — and reading each file once through the
// shared parse cache.
func (h *Handler) modulesExporting(analysis *docAnalysis, name string) []string {
	if loaded := analysis.symTable.ExportingModules(name); len(loaded) > 0 {
		return loaded
	}
	var out []string
	seen := map[string]bool{}
	// **The roots the resolver itself would use**: what the user opened, and the standard
	// library. `DefaultRoots` is [the document's directory, StdRoot], so offering an
	// import from anywhere else would name a module the compiler could not then find —
	// and offering from *fewer* places misses the one that sent me here, since
	// `parse_args` lives in `std.collections` and the open document was several
	// directories away from it.
	root, _ := h.searchRoot(analysis.file)
	for _, at := range []string{root, modules.StdRoot()} {
		if at == "" || seen["root:"+at] {
			continue
		}
		seen["root:"+at] = true
		h.walkForExports(at, analysis.file, name, seen, &out)
	}
	sort.Strings(out)
	return out
}

// walkForExports adds every module under root that exports name, skipping the open
// document itself — a name the file declares is not a name it needs to import.
func (h *Handler) walkForExports(root, skip, name string, seen map[string]bool, out *[]string) {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory is skipped, not fatal
		}
		if entry.IsDir() {
			if skipDir(path, entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// **A symlinked directory is walked through, which `WalkDir` does not do.**
		// `build/std` is a symlink to `../std` (build.sh makes it one deliberately, so a
		// copy cannot drift from the edited prelude), and the std root the *editor*
		// resolves against is `build` — so without this the standard library is
		// invisible to the search from that root, and `parse_args` is exactly the name
		// that lives there.
		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil || seen["root:"+target] {
				return nil
			}
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				seen["root:"+target] = true
				h.walkForExports(target, skip, name, seen, out)
			}
			return nil
		}
		if filepath.Ext(path) != ".lyra" || path == skip {
			return nil
		}
		source, ok := h.sourceFor(path)
		if !ok {
			return nil
		}
		header, exports := modules.FileExports(path, source, h.parseCache, name)
		if !exports || header.Module == "" || seen[header.Module] {
			return nil
		}
		seen[header.Module] = true
		*out = append(*out, header.Module)
		return nil
	})
	if err != nil {
		log.Printf("addImport: walk of %s failed: %v", root, err)
	}
}

// unimportedNameAt answers the identifier at a position when the file does not already
// import that name.
//
// The already-imported check is a **guard without a known trigger**, and it is here
// because this runs from the `default` arm: it sees every diagnostic code there is, not a
// code that means "missing import". An identifier the file does import is some other
// error, and offering to import it again would be noise. I could not construct a case
// that reaches it — a type mismatch on an imported function reports against the argument
// or the whole assignment, not against the name — so it is stated as insurance rather
// than dressed up as a fix, and it has no test, because a test that passes with the guard
// removed would be claiming something it does not check.
func unimportedNameAt(analysis *docAnalysis, line, col int) (string, bool) {
	expr := findExprAtPos(analysis.program, line, col)
	id, ok := expr.(*ast.IdentifierExpr)
	if !ok || id.Name == "" {
		return "", false
	}
	if fileImportsName(analysis.program, id.Name) {
		return "", false
	}
	return id.Name, true
}

// fileImportsName reports whether any import in this file already binds the bare name —
// as a member, or as an alias for one.
func fileImportsName(program *ast.Program, name string) bool {
	for _, node := range program.Statements {
		imp, ok := node.(*ast.ImportStmt)
		if !ok {
			continue
		}
		for _, m := range imp.Members {
			if m.Alias == name || (m.Alias == "" && m.Name == name) {
				return true
			}
		}
	}
	return false
}

// importEdit is the single edit that imports name from module: into an existing member
// list where the file already imports that module, and as a new line where it does not.
func importEdit(
	program *ast.Program, source, module, name string,
) (lsp.TextEdit, bool) {
	if imp := importOf(program, module); imp != nil && len(imp.Members) > 0 {
		last := imp.Members[len(imp.Members)-1].Location
		pos := lsp.Position{
			Line:      lspPos(last.EndLine),
			Character: utf16Column(source, lspPos(last.EndLine), last.EndCol),
		}
		return lsp.TextEdit{
			Range:   lsp.Range{Start: pos, End: pos},
			NewText: ", " + name,
		}, true
	}
	// A plain `import lib` binds a namespace and no bare names, so a member list has to
	// be introduced rather than extended — which is a rewrite of the reader's chosen
	// spelling. A new line beside it is the smaller edit and leaves both spellings
	// working.
	at := lspPos(importInsertLine(program))
	pos := lsp.Position{Line: at, Character: 0}
	return lsp.TextEdit{
		Range:   lsp.Range{Start: pos, End: pos},
		NewText: fmt.Sprintf("import %s.{ %s }\n", module, name),
	}, true
}

// importOf finds the file's member import of a module, by the dotted path it was written
// with. Nil for a module imported plainly or under an alias, which bind no bare names.
func importOf(program *ast.Program, module string) *ast.ImportStmt {
	for _, node := range program.Statements {
		imp, ok := node.(*ast.ImportStmt)
		if !ok || imp.Alias != "" || len(imp.Members) == 0 {
			continue
		}
		if importPath(imp) == module {
			return imp
		}
	}
	return nil
}

// importInsertLine is the 1-based line a new import belongs on: after the last existing
// import, else after the `module` declaration, else the top of the file.
func importInsertLine(program *ast.Program) int {
	after := 0
	for _, node := range program.Statements {
		switch s := node.(type) {
		case *ast.ImportStmt:
			if end := s.GetLocation().EndLine; end > after {
				after = end
			}
		case *ast.ModuleDeclStmt:
			if end := s.GetLocation().EndLine; end > after {
				after = end
			}
		}
	}
	return after + 1
}
