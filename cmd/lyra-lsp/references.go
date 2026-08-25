package main

import (
	"context"
	"log"
	"sort"

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
	defer recoverHandler("references", &result, &retErr)

	c, ok := h.cursorAt(string(params.TextDocument.URI), params.Position)
	if !ok {
		return nil, nil
	}
	uri, analysis, source, line, col := c.uri, c.analysis, c.source, c.line, c.col

	ident, ok := findExprAtPos(analysis.program, line, col).(*ast.IdentifierExpr)
	if !ok {
		// A **type or trait name**, which is not an expression and so cannot be found by
		// the walk above — the same gap go-to-definition had. Answered from the
		// collector's index of written type names rather than by walking, which is also
		// what lets it reach *other files*: the index covers the whole import graph.
		return h.typeReferences(uri, source, analysis, line, col, params.Context.IncludeDeclaration)
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

// typeReferences answers "find every use of this type" for a cursor on a type or trait
// name — in a signature, a field, an annotation, a bound, an `impl`, or on the
// declaration's own name.
//
// **Cross-file, unlike the identifier path above.** That is not ambition, it is what the
// index already holds: the collector records every written type name in the program, so
// a use in a sibling module costs a map lookup rather than a second walk. The identifier
// half still searches only the open document (todo.md).
//
// **Matched by declaration, never by name.** Two modules may each declare a private
// `Point`, so every candidate is resolved from its own position and kept only when it
// lands on the same declaration the cursor did — the discipline rule 4 asks of anything
// that answers a question about a name.
func (h *Handler) typeReferences(uri, source string, analysis *docAnalysis, line, col int, includeDecl bool) ([]lsp.Location, error) {
	if analysis.symTable == nil {
		return nil, nil
	}
	name, declLoc, ok := typeAnchorAt(analysis, line, col)
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
		if target, ok := h.locationIn(uri, source, analysis, loc); ok {
			out = append(out, target)
		}
	}

	for _, ref := range analysis.symTable.TypeRefs.Named(name) {
		if named, ok := lookupTypeOrTrait(analysis, ref); ok && namedNameLoc(named) == declLoc {
			add(ref.Loc)
		}
	}

	for _, loc := range typeExprOccurrences(analysis, name, declLoc) {
		add(loc)
	}

	if includeDecl {
		add(declLoc)
	}
	// Sorted, because the answers arrive in two passes — the index, then this document's
	// expression occurrences — so an unsorted list interleaves a struct literal after the
	// signatures below it. A client that sorts for itself is unaffected; one that does not
	// shows the list in the order it was built.
	sort.Slice(out, func(i, j int) bool {
		if out[i].URI != out[j].URI {
			return out[i].URI < out[j].URI
		}
		if out[i].Range.Start.Line != out[j].Range.Start.Line {
			return out[i].Range.Start.Line < out[j].Range.Start.Line
		}
		return out[i].Range.Start.Character < out[j].Range.Start.Character
	})
	log.Printf("references: type %q resolved to %d occurrence(s)", name, len(out))
	return out, nil
}

// typeExprOccurrences finds the uses of a type that are *expressions* — a struct literal's
// name and a data constructor's — which the type index does not hold, since the index
// records written *types* and these are values.
//
// **One walk shared by references and rename**, because the two must agree about what
// counts as a use. If rename missed what references reports, a rename would leave
// `Point { x: 1 }` behind after renaming the type, and the program would stop compiling
// with the editor showing the operation as successful.
//
// This document only: it is the one program the server has walked. The index half reaches
// every file, so a cross-file *literal* is the one occurrence kind that does not — which
// is why rename declines when the declaration is not local (see resolveRenameAnchor).
func typeExprOccurrences(analysis *docAnalysis, name string, declLoc ast.Location) []ast.Location {
	var out []ast.Location
	walkExprs(analysis.program, func(e ast.Expression) {
		exprName, loc, ok := typeExprOccurrence(e)
		if !ok || exprName != name {
			return
		}
		if decl, ok := analysis.symTable.LookupTypeFrom(exprName, loc); ok && namedNameLoc(decl) == declLoc {
			out = append(out, loc)
		}
	})
	return out
}

// typeExprOccurrence reports whether expr names a *type* in expression position, and the
// span of that name.
func typeExprOccurrence(expr ast.Expression) (string, ast.Location, bool) {
	switch x := expr.(type) {
	case *ast.StructInstanceExpr:
		return x.Name, nameSpanAt(x.GetLocation(), x.Name), true
	case *ast.DataConstructorExpr:
		return x.Constructor, nameSpanAt(x.GetLocation(), x.Constructor), true
	}
	return "", ast.Location{}, false
}

// typeAnchorAt resolves the cursor to a type or trait, from either side: a *use* (found in
// the index) or the declaration's own name, which is where "find references" is most often
// asked from and which the index does not contain — a declaration is not a reference.
func typeAnchorAt(analysis *docAnalysis, line, col int) (string, ast.Location, bool) {
	if ref, ok := analysis.symTable.TypeRefs.At(analysis.file, line, col); ok {
		if named, ok := lookupTypeOrTrait(analysis, ref); ok {
			return ref.Name, namedNameLoc(named), true
		}
		return "", ast.Location{}, false
	}
	// A struct literal's or constructor's name — an expression, and the position a reader
	// is as likely to ask from as any signature.
	if e, ok := findExprAtPos(analysis.program, line, col).(ast.Expression); ok && e != nil {
		if exprName, span, ok := typeExprOccurrence(e); ok && locationContains(span, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(exprName, span); ok {
				return exprName, namedNameLoc(decl), true
			}
		}
	}
	var name string
	var loc ast.Location
	for _, node := range analysis.program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			switch x := s.(type) {
			case *ast.TypeDeclStmt:
				if locationContains(x.NameLocation, line, col) {
					name, loc = x.Name, x.NameLocation
					return false
				}
			case *ast.TraitDeclStmt:
				if locationContains(x.NameLocation, line, col) {
					name, loc = x.Name, x.NameLocation
					return false
				}
			}
			return true
		}, func(ast.Expression) bool { return false })
		if name != "" {
			break
		}
	}
	return name, loc, name != ""
}

// nameSpanAt narrows a node's whole span to the name that begins it, so a reference to
// `Point` highlights the five characters rather than the whole `Point { … }` literal.
func nameSpanAt(loc ast.Location, name string) ast.Location {
	loc.EndLine = loc.StartLine
	loc.EndCol = loc.StartCol + len(name)
	return loc
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
