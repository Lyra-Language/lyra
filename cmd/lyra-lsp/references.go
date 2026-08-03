package main

import (
	"context"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// References implements textDocument/references, returning every occurrence
// that refers to the same binding as the identifier under the cursor.
//
// Matching is scope-aware: an occurrence counts only when it resolves to the
// same declaration as the cursor symbol, so a same-named binding in a sibling
// or nested (shadowing) scope is correctly excluded. The go-lsp library
// registers this capability automatically via the ReferencesHandler interface.
func (h *Handler) References(_ context.Context, params *lsp.ReferenceParams) (result []lsp.Location, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("references panic: %v\n%s", r, debug.Stack())
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

	ident, ok := findExprAtPos(analysis.program, line, col).(*ast.IdentifierExpr)
	if !ok {
		// References is only offered on identifier usages (variables, params,
		// function names). Type/constructor names are handled by go-to-definition.
		return nil, nil
	}

	// The declaration the cursor resolves to is the identity every other
	// occurrence is matched against.
	declLoc, ok := resolveDeclLocation(ident.Name, line, col, analysis)
	if !ok {
		return nil, nil
	}

	var out []lsp.Location
	seen := map[ast.Location]bool{}
	add := func(loc ast.Location) {
		if seen[loc] {
			return
		}
		seen[loc] = true
		// The occurrences come from this document, but the declaration may be in
		// another file now that the whole import graph is analyzed — a use of a
		// prelude function resolves to the prelude. locationIn puts it in that file.
		if target, ok := h.locationIn(uri, source, analysis, loc); ok {
			out = append(out, target)
		}
	}

	walkExprs(analysis.program, func(e ast.Expression) {
		name, loc, ok := referenceOccurrence(e)
		if !ok || name != ident.Name {
			return
		}
		if dl, ok := resolveDeclLocation(name, loc.StartLine, loc.StartCol, analysis); ok && dl == declLoc {
			add(loc)
		}
	})

	if params.Context.IncludeDeclaration {
		add(declLoc)
	}

	log.Printf("references: %q resolved to %d occurrence(s)", ident.Name, len(out))
	return out, nil
}

// resolveDeclLocation returns the declaration location that name resolves to
// from the scope enclosing (line, col). Returns false when the name is unbound.
func resolveDeclLocation(name string, line, col int, analysis *docAnalysis) (ast.Location, bool) {
	scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), line, col)
	named, ok := scope.Lookup(name)
	if !ok {
		return ast.Location{}, false
	}
	return named.GetLocation(), true
}

// referenceOccurrence reports whether expr names a binding and, if so, returns
// the name and the range to highlight: a plain identifier, or the name portion
// of a `...spread` (skipping the three leading dots).
func referenceOccurrence(expr ast.Expression) (string, ast.Location, bool) {
	switch e := expr.(type) {
	case *ast.IdentifierExpr:
		return e.Name, e.GetLocation(), true
	case *ast.SpreadExpr:
		loc := e.GetLocation()
		loc.StartCol += len("...")
		return e.Name, loc, true
	}
	return "", ast.Location{}, false
}
