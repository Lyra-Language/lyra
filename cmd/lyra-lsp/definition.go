package main

import (
	"context"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// Definition implements textDocument/definition, returning the source location
// where the symbol under the cursor is declared.
func (h *Handler) Definition(_ context.Context, params *lsp.DefinitionParams) (result []lsp.Location, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("definition panic: %v\n%s", r, debug.Stack())
			result, retErr = nil, nil
		}
	}()

	uri := string(params.TextDocument.URI)
	h.mu.Lock()
	analysis, ok := h.analysisStore[uri]
	source := h.docStore[uri]
	h.mu.Unlock()
	if !ok {
		return nil, nil
	}

	// LSP positions are 0-based UTF-16; ast.Location is 1-based bytes.
	line := params.Position.Line + 1
	col := byteColumn(source, params.Position.Line, params.Position.Character)

	log.Printf("definition: request at %s line=%d col=%d", uri, line, col)

	expr := findExprAtPos(analysis.program, line, col)
	loc := resolveDefinition(expr, line, col, analysis)
	if loc == nil {
		log.Printf("definition: no definition found (expr %T)", expr)
		return nil, nil
	}

	log.Printf("definition: found at %s", loc.Pretty())
	return []lsp.Location{astLocToLSPLocation(uri, source, *loc)}, nil
}

// resolveDefinition maps the expression at the cursor to its definition location.
// Returns nil when no definition can be found.
func resolveDefinition(expr ast.Expression, line, col int, analysis *docAnalysis) *ast.Location {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.IdentifierExpr:
		scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.symTable.EntryScope(), line, col)
		named, ok := scope.Lookup(e.Name)
		if !ok {
			return nil
		}
		loc := named.GetLocation()
		return &loc

	case *ast.StructInstanceExpr:
		// Cursor is on the struct type name (the name occupies the start of the expression).
		if cursorOnName(e.GetLocation(), e.Name, line, col) {
			if decl, ok := analysis.symTable.Types[e.Name]; ok {
				loc := decl.GetLocation()
				return &loc
			}
		}

	case *ast.DataConstructorExpr:
		// Cursor is on a data-type constructor name (e.g. Some, None, Ok, Err).
		if cursorOnName(e.GetLocation(), e.Constructor, line, col) {
			if decl, ok := analysis.symTable.Types[e.Constructor]; ok {
				loc := decl.GetLocation()
				return &loc
			}
		}
	}
	return nil
}

// cursorOnName reports whether (line, col) falls on the name string that begins
// at loc.StartLine/StartCol. Both are 1-based.
func cursorOnName(loc ast.Location, name string, line, col int) bool {
	return line == loc.StartLine && col >= loc.StartCol && col < loc.StartCol+len(name)
}

// findScopeAtPos returns the innermost scope whose block contains (line, col).
//
// Falls back to fileScope when no nested block matches — a top-level position. That is
// the scope of the module the document belongs to (SymbolTable.EntryScope for the
// single file the LSP analyzes), *not* the global scope: a file's own top-level
// declarations live in its module scope, and the chain out from there reaches the
// prelude's names and other modules' exports in the order the language resolves them.
func findScopeAtPos(program *ast.Program, scopeTable *symbols.ScopeTable, fileScope *symbols.Scope, line, col int) *symbols.Scope {
	for _, node := range program.Statements {
		if s := scopeInNode(node, scopeTable, line, col); s != nil {
			return s
		}
	}
	return fileScope
}

// scopeInNode descends into an AST node looking for the innermost BlockExpr
// that contains (line, col), returning its scope from scopeTable.
func scopeInNode(node ast.AstNode, scopeTable *symbols.ScopeTable, line, col int) *symbols.Scope {
	if node == nil || !containsPos(node.GetLocation(), line, col) {
		return nil
	}
	return scopeInExpr(nodeToExpr(node), scopeTable, line, col)
}

func scopeInExpr(expr ast.Expression, scopeTable *symbols.ScopeTable, line, col int) *symbols.Scope {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.BlockExpr:
		sc, hasScope := scopeTable.Get(e)
		// Recurse first — a deeper nested block is more specific.
		for _, stmt := range e.Statements {
			if inner := scopeInNode(stmt, scopeTable, line, col); inner != nil {
				return inner
			}
		}
		if hasScope {
			return sc
		}
	case *ast.IfExpr:
		if r := scopeInExpr(e.Then, scopeTable, line, col); r != nil {
			return r
		}
		return scopeInExpr(e.Else, scopeTable, line, col)
	case *ast.MatchExpr:
		for _, arm := range e.MatchArms {
			if r := scopeInExpr(arm.Body, scopeTable, line, col); r != nil {
				return r
			}
		}
	case *ast.LambdaExpr:
		return scopeInExpr(e.Body, scopeTable, line, col)
	}
	return nil
}

// nodeToExpr extracts the primary expression from a statement node.
func nodeToExpr(node ast.AstNode) ast.Expression {
	switch s := node.(type) {
	case *ast.VarDeclStmt:
		return s.Value
	case *ast.ExpressionStmt:
		return s.Expression
	case *ast.ReturnStmt:
		return s.Value
	}
	return nil
}

// astLocToLSPLocation converts a 1-based, byte-based ast.Location to an
// lsp.Location, using source to translate byte columns to UTF-16.
func astLocToLSPLocation(uri string, source string, loc ast.Location) lsp.Location {
	return lsp.Location{
		URI:   lsp.DocumentURI(uri),
		Range: locToRange(source, loc),
	}
}
