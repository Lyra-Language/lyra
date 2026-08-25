package main

import (
	"context"
	"log"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// renameAnchor holds the resolved name and locations for a rename operation,
// whether the cursor was on a usage (IdentifierExpr) or on a declaration name.
type renameAnchor struct {
	name string
	// exported says whether the name can be seen outside its module, which decides
	// whether the workspace has to be searched for importers at all — a private
	// declaration has none, and the search is tens of milliseconds.
	exported bool
	declLoc  ast.Location // whole-decl location: identity key for reference matching
	nameLoc  ast.Location // name-only span: the range to replace at the declaration site
}

// resolveRenameAnchor returns the rename anchor for the symbol at (line, col).
// It first tries to find an IdentifierExpr (usage position); if that fails it
// falls back to searching declaration NameLocations, which covers the case
// where the cursor is placed directly on a let/var/const/type/trait name.
func resolveRenameAnchor(line, col int, analysis *docAnalysis) (renameAnchor, bool) {
	var name string
	var named ast.Named

	// Fast path: cursor is on an expression-position identifier (a usage).
	if ident, ok := findExprAtPos(analysis.program, line, col).(*ast.IdentifierExpr); ok {
		scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), line, col)
		if n, ok := scope.Lookup(ident.Name); ok {
			name = ident.Name
			named = n
		}
	}

	// Slow path: cursor is on a declaration name — walk all statements and check
	// each NameLocation against the cursor position.
	if name == "" {
		for _, node := range analysis.program.Statements {
			stmt, ok := node.(ast.Statement)
			if !ok {
				continue
			}
			ast.WalkStmt(stmt, func(s ast.Statement) bool {
				var sName string
				var sNameLoc ast.Location
				switch x := s.(type) {
				case *ast.VarDeclStmt:
					sName, sNameLoc = x.Name, x.NameLocation
				case *ast.TypeDeclStmt:
					sName, sNameLoc = x.Name, x.NameLocation
				case *ast.TraitDeclStmt:
					sName, sNameLoc = x.Name, x.NameLocation
				case *ast.ExternDeclStmt:
					sName, sNameLoc = x.Name, x.NameLocation
				default:
					return true // descend into nested stmts
				}
				if sName == "" || !locationContains(sNameLoc, line, col) {
					return true
				}
				// Resolve the binding from the scope at the name's position.
				scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), sNameLoc.StartLine, sNameLoc.StartCol)
				if n, ok2 := scope.Lookup(sName); ok2 {
					name = sName
					named = n
				}
				return false // found; stop walking
				// **Descend into expressions**, or this walk never reaches a *local*
				// declaration: every `let` inside a function body sits under a
				// LambdaExpr, and returning false here stopped at it. So renaming a
				// local from its own `let` — the obvious place to start a rename —
				// silently did nothing, while renaming it from a *use* worked, which is
				// why it went unnoticed. Nothing else changes: the statement callback
				// still decides what matches.
			}, func(ast.Expression) bool { return true })
			if name != "" {
				break
			}
		}
	}

	// Parameter path: cursor is on a parameter name in a lambda parameter list.
	// Parameter positions are outside the body BlockExpr, so findScopeAtPos at
	// the parameter location returns the outer scope (missing the param). We
	// walk LambdaExprs explicitly and look up inside the body scope.
	if name == "" {
		walkExprs(analysis.program, func(e ast.Expression) {
			if name != "" {
				return
			}
			lambda, ok := e.(*ast.LambdaExpr)
			if !ok {
				return
			}
			for i := range lambda.Parameters {
				param := &lambda.Parameters[i]
				ip, ok := param.Pattern.(*ast.IdentifierPattern)
				if !ok || !locationContains(ip.GetLocation(), line, col) {
					continue
				}
				// Look up the parameter in the body's scope.
				scope := paramBodyScope(lambda, analysis)
				if n, ok2 := scope.Lookup(ip.Name); ok2 {
					name = ip.Name
					named = n
				}
				break
			}
		})
	}

	// A **type or trait name in a type position** — a parameter, a field, a bound, an
	// `impl`. Not an expression, so none of the paths above can see it; answered from the
	// collector's index, exactly as go-to-definition is. Last, so nothing that already
	// resolved changes: a struct literal's name is both an expression and a written type.
	if name == "" && analysis.symTable != nil {
		if ref, ok := analysis.symTable.TypeRefs.At(analysis.file, line, col); ok {
			if n, ok := lookupTypeOrTrait(analysis, ref); ok {
				name, named = ref.Name, n
			}
		}
	}

	// A struct literal's or constructor's name is a *value* position naming a type, and
	// is where a reader is as likely to start a rename as any signature. It resolves
	// through the same table; the walk above cannot reach it because it matches only an
	// IdentifierExpr.
	if name == "" && analysis.symTable != nil {
		if e := findExprAtPos(analysis.program, line, col); e != nil {
			if exprName, span, ok := typeExprOccurrence(e); ok && locationContains(span, line, col) {
				if decl, ok := analysis.symTable.LookupTypeFrom(exprName, span); ok {
					name, named = exprName, decl
				}
			}
		}
	}

	if name == "" || named == nil {
		return renameAnchor{}, false
	}

	anchor := renameAnchor{
		name:     name,
		exported: isExportedDecl(named),
		declLoc:  named.GetLocation(),
		nameLoc:  namedNameLoc(named),
	}

	// A name can now resolve into another file — the prelude, or another module of
	// the program — and this server renames within one document. Editing the
	// declaration's span in *this* buffer would splice the new name in at the other
	// file's line and column, so a cross-file declaration declines the rename instead.
	// (Doing it properly means collecting occurrences across every unit and returning
	// a multi-file WorkspaceEdit; see todo.md.)
	if !sameFile(anchor.nameLoc.File, analysis.file) {
		log.Printf("rename: %q is declared in %s, not this document — declining", name, anchor.nameLoc.File)
		return renameAnchor{}, false
	}

	// **An `extern`'s name is the C symbol**, so there is nothing here to rename: the
	// other half of the declaration is in a library this compiler did not build, and
	// renaming the Lyra side would emit `declare @newName` for a symbol nobody defines
	// — a link error naming something the source no longer contains. Declined rather
	// than performed, on the rule the cross-file case above follows: a rename that
	// cannot be carried out completely should not be carried out partially.
	//
	// It was worse than missing before 08/20. `namedNameLoc` had no extern case either,
	// so the anchor's nameLoc fell back to the *declaration's* start — the `@link` or
	// `unsafe` token — and renaming a usage spliced the new name over the first few
	// characters of the declaration line, corrupting the source.
	//
	// If externs ever gain a `@symbol("…")` attribute to decouple the Lyra name from the
	// linker's, this becomes an ordinary rename and the check comes out.
	if _, isExtern := named.(*ast.ExternDeclStmt); isExtern {
		log.Printf("rename: %q is an extern; its name is the C symbol — declining", name)
		return renameAnchor{}, false
	}

	// An **exported** type was declined here for a few hours on 08/22, on the grounds that
	// its importers were not analyzed and the rename would therefore be partial. That was
	// the right refusal for the server as it stood and the wrong thing to leave standing:
	// rename was unavailable for exactly the types that matter most, a module's public
	// surface, because of a limitation in what the server bothered to look at rather than
	// anything about the language. The search now exists (importers.go), so the rename can
	// be complete and the refusal is gone.
	//
	// What it cannot see is a file outside the workspace root that imports this module.
	// No tool can; that is what a workspace root means.

	return anchor, true
}

// namedNameLoc returns the span covering just the bound name of a Named node
// (not the full declaration). For VarDeclStmt/TypeDeclStmt/TraitDeclStmt it
// uses NameLocation; for Parameters it uses the pattern location.
// Falls back to the full node location if no specific name span is available.
func namedNameLoc(named ast.Named) ast.Location {
	switch n := named.(type) {
	case *ast.VarDeclStmt:
		if n.NameLocation != (ast.Location{}) {
			return n.NameLocation
		}
	case *ast.TypeDeclStmt:
		if n.NameLocation != (ast.Location{}) {
			return n.NameLocation
		}
	case *ast.TraitDeclStmt:
		if n.NameLocation != (ast.Location{}) {
			return n.NameLocation
		}
	case *ast.ExternDeclStmt:
		if n.NameLocation != (ast.Location{}) {
			return n.NameLocation
		}
	case *ast.Parameter:
		if n.Pattern != nil {
			return n.Pattern.GetLocation()
		}
	}
	return named.GetLocation()
}

// paramBodyScope returns the scope that holds a lambda's parameters. Parameters
// are registered in the body BlockExpr's scope, which is directly accessible
// via the ScopeTable without needing a position inside the block.
// Falls back to globalScope if the body has no registered scope.
func paramBodyScope(lambda *ast.LambdaExpr, analysis *docAnalysis) *symbols.Scope {
	if lambda.Body != nil {
		if sc, ok := analysis.scopeTable.Get(lambda.Body); ok {
			return sc
		}
	}
	// The lambda's own scope is where the parameters are bound, and it exists whether or
	// not the body is a block — only a *block* body records one of its own. Falling
	// straight to the file scope, where no parameter is bound, is what made a parameter
	// of an expression-bodied function unrenameable.
	if sc, ok := analysis.scopeTable.Get(lambda); ok {
		return sc
	}
	return analysis.fileScope()
}

// locationContains reports whether loc spans the 1-based (line, col) position.
func locationContains(loc ast.Location, line, col int) bool {
	if loc.StartLine == 0 {
		return false
	}
	if line < loc.StartLine || line > loc.EndLine {
		return false
	}
	if line == loc.StartLine && col < loc.StartCol {
		return false
	}
	if line == loc.EndLine && col > loc.EndCol {
		return false
	}
	return true
}

// Rename implements textDocument/rename, returning a WorkspaceEdit that
// replaces every occurrence of the identifier under the cursor with newName.
//
// Matching is scope-aware (same logic as References): an occurrence counts
// only when it resolves to the same declaration as the cursor symbol, so
// shadowed or sibling same-named bindings are excluded. The declaration site
// itself is always included in the edits. The go-lsp library registers this
// capability automatically via the RenameHandler interface.
func (h *Handler) Rename(_ context.Context, params *lsp.RenameParams) (result *lsp.WorkspaceEdit, retErr error) {
	defer recoverHandler("rename", &result, &retErr)

	uri := string(params.TextDocument.URI)
	analysis, source, ok := h.docFor(uri)
	if !ok {
		return nil, nil
	}

	line := params.Position.Line + 1
	col := byteColumn(source, params.Position.Line, params.Position.Character)

	anchor, ok := resolveRenameAnchor(line, col, analysis)
	if !ok {
		return nil, nil
	}

	newName := params.NewName
	seen := map[ast.Location]bool{}
	var edits []lsp.TextEdit

	addEdit := func(loc ast.Location) {
		if seen[loc] {
			return
		}
		seen[loc] = true
		edits = append(edits, lsp.TextEdit{
			Range:   locToRange(source, loc),
			NewText: newName,
		})
	}

	// Edit the declaration name span (not the whole-stmt location).
	addEdit(anchor.nameLoc)

	// Collect every usage that resolves to the same declaration.
	walkExprs(analysis.program, func(e ast.Expression) {
		name, loc, ok := referenceOccurrence(e)
		if !ok || name != anchor.name {
			return
		}
		if dl, ok := resolveDeclLocation(name, loc.StartLine, loc.StartCol, analysis); ok && dl == anchor.declLoc {
			addEdit(loc)
		}
	})

	// Keyed by file, and this document's entry is written *after* the loop below: addEdit
	// reassigns the `edits` slice, so a map entry taken before the last append holds a
	// stale header and silently loses edits.
	changes := map[lsp.DocumentURI][]lsp.TextEdit{}

	// A **type or trait** is also written in signatures, and those live in the index
	// rather than in the expression tree — and in *other files*, since the index covers
	// the whole import graph. This is the multi-file WorkspaceEdit the single-file note
	// on this function said doing it properly would need; it is affordable here only
	// because the index already holds every occurrence with its file.
	//
	// The declaration must still be in this document (checked in resolveRenameAnchor),
	// so renaming a prelude type from a use site is still declined. What changes is that
	// renaming *your own* type now reaches its uses in your other modules instead of
	// silently editing one file and leaving the program broken.
	indexed := h.importerAnalysis(analysis, source, anchor.exported)
	if indexed.symTable != nil {
		for _, ref := range indexed.symTable.TypeRefs.Named(anchor.name) {
			named, ok := lookupTypeOrTrait(indexed, ref)
			if !ok || namedNameLoc(named) != anchor.nameLoc {
				continue
			}
			if sameFile(ref.Loc.File, analysis.file) {
				addEdit(ref.Loc)
				continue
			}
			otherURI, otherSource, ok := h.sourceOf(ref.Loc.File)
			if !ok {
				// A file that cannot be read cannot be edited, and a rename that is
				// carried out partially is worse than one declined: the program would
				// stop compiling with no indication of where. Decline the whole thing.
				log.Printf("rename: cannot read %s — declining rather than editing partially", ref.Loc.File)
				return nil, nil
			}
			key := lsp.DocumentURI(otherURI)
			changes[key] = append(changes[key], lsp.TextEdit{
				Range:   locToRange(otherSource, ref.Loc),
				NewText: newName,
			})
		}
	}

	// The expression-position uses — a struct literal, a constructor — which the index
	// does not hold. **Renaming without these produces a broken program**: the type would
	// be renamed everywhere except where it is constructed, and the editor would report
	// success. The same walk `references` uses, so the two cannot come to disagree about
	// what a use is.
	for _, loc := range typeExprOccurrences(analysis, anchor.name, anchor.nameLoc) {
		addEdit(loc)
	}

	changes[lsp.DocumentURI(uri)] = edits

	total := 0
	for _, e := range changes {
		total += len(e)
	}
	log.Printf("rename: %q → %q, %d edit(s) across %d file(s)", anchor.name, newName, total, len(changes))
	return &lsp.WorkspaceEdit{Changes: changes}, nil
}

// PrepareRename implements textDocument/prepareRename, validating that the
// cursor is on a renameable identifier and returning its current range and
// text as the rename placeholder. The go-lsp library registers this capability
// automatically via the PrepareRenameHandler interface.
func (h *Handler) PrepareRename(_ context.Context, params *lsp.PrepareRenameParams) (result *lsp.PrepareRenameResult, retErr error) {
	defer recoverHandler("prepareRename", &result, &retErr)

	uri := string(params.TextDocument.URI)
	analysis, source, ok := h.docFor(uri)
	if !ok {
		return nil, nil
	}

	line := params.Position.Line + 1
	col := byteColumn(source, params.Position.Line, params.Position.Character)

	anchor, ok := resolveRenameAnchor(line, col, analysis)
	if !ok {
		return nil, nil
	}

	return &lsp.PrepareRenameResult{
		Range:       locToRange(source, anchor.nameLoc),
		Placeholder: anchor.name,
	}, nil
}
