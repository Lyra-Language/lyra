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

	// A **pattern**: a constructor in a `match` arm, or a name the pattern binds. Neither
	// is an expression, so the walk below cannot reach it — the same gap definition and
	// hover had. Tried before the type index because a constructor is not a type name and
	// would not be found there anyway.
	if locs, handled := h.patternReferences(uri, source, analysis, line, col, params.Context.IncludeDeclaration); handled {
		return locs, nil
	}

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

// patternReferences answers for a cursor inside a pattern, and reports whether it was its
// question to answer.
//
// Two quite different questions share the entry point, because two quite different things
// are written in a pattern:
//
//   - a **constructor** (`Keyboard(Up)`), whose uses are spread across both halves of the
//     AST — every `match` arm that names it *and* every place it is applied as a value.
//     Finding only one half would be worse than finding neither: a reader would take the
//     answer as complete.
//   - a **binding** (`m` in `Mouse(m)`), which is an ordinary local. Its uses are
//     expressions, so the existing occurrence walk answers once it is given the name and
//     the scope — what it could not do is *start* from the binding, since a binding is a
//     pattern.
func (h *Handler) patternReferences(uri, source string, analysis *docAnalysis, line, col int, includeDecl bool) ([]lsp.Location, bool) {
	pat := findPatternAtPos(analysis.program, line, col)
	if pat == nil || analysis.symTable == nil {
		return nil, false
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

	switch p := pat.(type) {
	case *ast.DataPattern:
		if !cursorOnName(p.GetLocation(), p.Name, line, col) {
			return nil, false
		}
		for _, loc := range constructorOccurrences(analysis, p.Name) {
			add(loc)
		}
		if includeDecl {
			if decl, ok := analysis.symTable.DeclaringDataType(p.Name, p.GetLocation()); ok {
				add(namedNameLoc(decl))
			} else if decl, ok := anyDeclaringDataType(analysis, p.Name); ok {
				add(namedNameLoc(decl))
			}
		}
		sortLocations(out)
		log.Printf("references: constructor %q resolved to %d occurrence(s)", p.Name, len(out))
		return out, true

	case *ast.IdentifierPattern:
		// The identity is taken from the **scope**, not from the pattern node, and the two
		// are not the same thing: what a scope binds for `m` is whatever the collector
		// registered, which is not this `*ast.IdentifierPattern`. Comparing against the
		// pattern's own location matched nothing, so the binding answered with itself and
		// none of its uses — a "find references" that is confidently wrong. Asking
		// resolveDeclLocation at the binding's own position makes both sides of the
		// comparison come from one function.
		declLoc, ok := resolveDeclLocation(p.Name, p.GetLocation().StartLine, p.GetLocation().StartCol, analysis)
		if !ok {
			// **A `match` arm registers nothing.** Arms create no scope and their pattern
			// bindings are never entered in the symbol table, so `m` in `Mouse(m) => …` is
			// invisible to every scope-based question — which is why the identifier path
			// finds nothing at `m.button` either. Declined rather than answered from the
			// pattern alone: matching by name within the arm would over-report a shadowing
			// binding, and answering with the binding *itself* and none of its uses is a
			// result a reader takes as complete. Definition still answers here, since a
			// binding resolves to itself without needing a scope.
			//
			// The fix is in the collector — push a scope per arm and register what the
			// pattern binds — and is recorded in todo.md.
			return nil, false
		}
		walkExprs(analysis.program, func(e ast.Expression) {
			name, loc, ok := referenceOccurrence(e)
			if !ok || name != p.Name {
				return
			}
			// Scope-matched exactly as the identifier path is: a same-named binding in a
			// sibling or nested arm resolves elsewhere and is not this one.
			if dl, ok := resolveDeclLocation(name, loc.StartLine, loc.StartCol, analysis); ok && dl == declLoc {
				add(loc)
			}
		})
		if includeDecl {
			add(declLoc)
		}
		sortLocations(out)
		log.Printf("references: pattern binding %q resolved to %d occurrence(s)", p.Name, len(out))
		return out, true
	}
	return nil, false
}

// constructorOccurrences finds every mention of a constructor in this document, in both
// halves of the AST: as a pattern, and as a value.
//
// **Both halves, or neither.** `Keyboard` appears in `match e { Keyboard(k) => … }` and in
// `Keyboard(key)` as a construction, and a "find all references" that reported one kind
// would be read as complete. This document only — the same limit the identifier path has.
func constructorOccurrences(analysis *docAnalysis, name string) []ast.Location {
	var out []ast.Location
	walkExprs(analysis.program, func(e ast.Expression) {
		switch x := e.(type) {
		case *ast.DataConstructorExpr:
			if x.Constructor == name {
				out = append(out, nameSpanAt(x.GetLocation(), name))
			}
		case *ast.TupleLiteralExpr:
			// How `Keyboard(k)` parses: a tuple literal carrying the constructor's name.
			if x.Name == name {
				out = append(out, nameSpanAt(x.GetLocation(), name))
			}
		}
	})
	for _, node := range analysis.program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		consider := func(n ast.AstNode) {
			for _, p := range ast.PatternsOf(n) {
				ast.WalkPattern(p, func(sub ast.Pattern) bool {
					if dp, ok := sub.(*ast.DataPattern); ok && dp.Name == name {
						out = append(out, nameSpanAt(dp.GetLocation(), name))
					}
					return true
				})
			}
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool { consider(s); return true },
			func(e ast.Expression) bool { consider(e); return true })
	}
	return out
}

// sortLocations orders a result by position, so a list built from two passes does not
// interleave.
func sortLocations(locs []lsp.Location) {
	sort.Slice(locs, func(i, j int) bool {
		if locs[i].URI != locs[j].URI {
			return locs[i].URI < locs[j].URI
		}
		if locs[i].Range.Start.Line != locs[j].Range.Start.Line {
			return locs[i].Range.Start.Line < locs[j].Range.Start.Line
		}
		return locs[i].Range.Start.Character < locs[j].Range.Start.Character
	})
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
	name, declLoc, exported, ok := typeAnchorAt(analysis, line, col)
	if !ok {
		return nil, nil
	}

	// Widen to the modules that import this one. Resolution runs downward only, so a use
	// of an exported type in a consumer is not in the index the document's own analysis
	// built — and "find every use" is exactly the upward question (importers.go). Done
	// here rather than in the analysis every keystroke runs, because it walks the
	// workspace and this is an explicit action.
	indexed := h.importerAnalysis(analysis, source, exported)

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

	for _, ref := range indexed.symTable.TypeRefs.Named(name) {
		if named, ok := lookupTypeOrTrait(indexed, ref); ok && namedNameLoc(named) == declLoc {
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
	sortLocations(out)
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
func typeAnchorAt(analysis *docAnalysis, line, col int) (string, ast.Location, bool, bool) {
	if ref, ok := analysis.symTable.TypeRefs.At(analysis.file, line, col); ok {
		if named, ok := lookupTypeOrTrait(analysis, ref); ok {
			return ref.Name, namedNameLoc(named), isExportedDecl(named), true
		}
		return "", ast.Location{}, false, false
	}
	// A struct literal's or constructor's name — an expression, and the position a reader
	// is as likely to ask from as any signature.
	if e, ok := findExprAtPos(analysis.program, line, col).(ast.Expression); ok && e != nil {
		if exprName, span, ok := typeExprOccurrence(e); ok && locationContains(span, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(exprName, span); ok {
				return exprName, namedNameLoc(decl), isExportedDecl(decl), true
			}
		}
	}
	var name string
	var loc ast.Location
	exported := false
	for _, node := range analysis.program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			switch x := s.(type) {
			case *ast.TypeDeclStmt:
				if locationContains(x.NameLocation, line, col) {
					name, loc, exported = x.Name, x.NameLocation, x.IsPublic
					return false
				}
			case *ast.TraitDeclStmt:
				if locationContains(x.NameLocation, line, col) {
					name, loc, exported = x.Name, x.NameLocation, x.IsPublic
					return false
				}
			}
			return true
		}, func(ast.Expression) bool { return false })
		if name != "" {
			break
		}
	}
	return name, loc, exported, name != ""
}

// isExportedDecl reports whether a type or trait declaration is visible outside its module.
func isExportedDecl(named ast.Named) bool {
	switch d := named.(type) {
	case *ast.TypeDeclStmt:
		return d.IsPublic
	case *ast.TraitDeclStmt:
		return d.IsPublic
	}
	return false
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
