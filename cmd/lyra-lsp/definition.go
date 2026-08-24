package main

import (
	"context"
	"log"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// Definition implements textDocument/definition, returning the source location
// where the symbol under the cursor is declared.
func (h *Handler) Definition(_ context.Context, params *lsp.DefinitionParams) (result []lsp.Location, retErr error) {
	defer recoverHandler("definition", &result, &retErr)

	c, ok := h.cursorAt(string(params.TextDocument.URI), params.Position)
	if !ok {
		return nil, nil
	}
	uri, analysis, source, line, col := c.uri, c.analysis, c.source, c.line, c.col

	log.Printf("definition: request at %s line=%d col=%d", uri, line, col)

	expr := findExprAtPos(analysis.program, line, col)
	loc := resolveDefinition(expr, line, col, analysis)
	if loc == nil {
		log.Printf("definition: no definition found (expr %T)", expr)
		return nil, nil
	}

	log.Printf("definition: found at %s", loc.Pretty())
	target, ok := h.locationIn(uri, source, analysis, *loc)
	if !ok {
		return nil, nil
	}
	return []lsp.Location{target}, nil
}

// locationIn builds the lsp.Location for a definition, which since the server resolves
// a document's whole import graph may live in another file — `Some` is declared in the
// prelude, not in the buffer the user is looking at. Reporting it against the current
// URI would jump to those line and column numbers *in the open document*, so the
// target's own file has to supply both the URI and the source the columns are measured
// against. It reports false when that file cannot be read, since a location that cannot
// be converted is better dropped than sent to the wrong place.
func (h *Handler) locationIn(uri, source string, analysis *docAnalysis, loc ast.Location) (lsp.Location, bool) {
	if loc.File == "" || sameFile(loc.File, analysis.file) {
		return astLocToLSPLocation(uri, source, loc), true
	}
	targetURI, targetSource, ok := h.sourceOf(loc.File)
	if !ok {
		log.Printf("definition: cannot read %s", loc.File)
		return lsp.Location{}, false
	}
	return astLocToLSPLocation(targetURI, targetSource, loc), true
}

// resolveDefinition maps the expression at the cursor to its definition location.
// Returns nil when no definition can be found.
func resolveDefinition(expr ast.Expression, line, col int, analysis *docAnalysis) *ast.Location {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.IdentifierExpr:
		scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), line, col)
		named, ok := scope.Lookup(e.Name)
		if !ok {
			return nil
		}
		// The *name's* location, not the declaration's: `namedNameLoc` (rename.go) is
		// already the one answer to that question, and using it here rather than a
		// second copy is what makes an `extern` — whose declaration starts at an
		// `@link` or `unsafe` token several lines above its name — land on the name
		// like every other declaration does.
		loc := namedNameLoc(named)
		return &loc

	case *ast.StructInstanceExpr:
		// Cursor is on the struct type name (the name occupies the start of the expression).
		if cursorOnName(e.GetLocation(), e.Name, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(e.Name, e.GetLocation()); ok {
				loc := decl.GetLocation()
				return &loc
			}
		}

	case *ast.DataConstructorExpr:
		// Cursor is on a data-type constructor name (e.g. Some, None, Ok, Err).
		if cursorOnName(e.GetLocation(), e.Constructor, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(e.Constructor, e.GetLocation()); ok {
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
	case *ast.ForLoopExpr:
		// The **body**, not its statements: a loop's own bindings — the counter, or
		// `for i, c in s`'s pair — live in the body block's scope, so iterating the
		// statements finds every nested scope and misses the one the loop introduced.
		// The loop variable is then the one name inside a loop that cannot be resolved,
		// which is a strange enough hole to look like something else.
		return scopeInExpr(e.Body, scopeTable, line, col)
	case *ast.ForInLoopExpr:
		return scopeInExpr(e.Body, scopeTable, line, col)
	case *ast.UnsafeBlockExpr:
		// An `unsafe` block *is* its body, including for scoping: a binding declared
		// inside one is scoped to it (which is why UnsafeBlockExpr.Body is a pointer —
		// see the collector). Its twin is findExprInExpr in hover.go; a position lookup
		// that finds the expression but not its scope resolves the name in the wrong
		// place, so the two have to gain a node together.
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
