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
	named    ast.Named    // the declaration itself
	// cursorLoc is the span of the occurrence the rename started from, in this document —
	// what PrepareRename highlights when the declaration is in another file, where nameLoc's
	// line and column mean nothing in this buffer.
	cursorLoc ast.Location
}

// resolveRenameAnchor returns the rename anchor for the symbol at (line, col).
// It first tries to find an IdentifierExpr (usage position); if that fails it
// falls back to searching declaration NameLocations, which covers the case
// where the cursor is placed directly on a let/var/const/type/trait name.
func resolveRenameAnchor(line, col int, analysis *docAnalysis) (renameAnchor, bool) {
	var name string
	var named ast.Named
	var cursorLoc ast.Location

	// Fast path: cursor is on an expression-position identifier (a usage).
	if ident, ok := findExprAtPos(analysis.program, line, col).(*ast.IdentifierExpr); ok {
		scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), line, col)
		if n, ok := scope.Lookup(ident.Name); ok {
			name = ident.Name
			named = n
			cursorLoc = ident.GetLocation()
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
					cursorLoc = sNameLoc
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
					cursorLoc = ip.GetLocation()
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
				name, named, cursorLoc = ref.Name, n, ref.Loc
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
					name, named, cursorLoc = exprName, decl, span
				}
			}
		}
	}

	if name == "" || named == nil {
		return renameAnchor{}, false
	}

	anchor := renameAnchor{
		name:      name,
		exported:  isExportedDecl(named),
		declLoc:   named.GetLocation(),
		nameLoc:   namedNameLoc(named),
		named:     named,
		cursorLoc: cursorLoc,
	}

	// **A declaration in another file renames there**, since 09/13: the edit is routed to
	// the file each occurrence is in (Rename). Until then a cross-file declaration declined
	// the rename, because editing its span in *this* buffer would have spliced the new name
	// in at the other file's line and column.
	//
	// The standard library still declines. Every program on the machine depends on it and
	// none of them is in the workspace being searched, so the rename could never be
	// complete — and a partial rename is worse than none.
	if isStdFile(analysis, anchor.nameLoc.File) {
		log.Printf("rename: %q is declared in the standard library (%s) — declining", name, anchor.nameLoc.File)
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
	changes := map[lsp.DocumentURI][]lsp.TextEdit{}
	unreadable := ""

	// addEdit routes an occurrence to the file it is in. A file that cannot be read cannot
	// be edited, and a rename carried out partially is worse than one declined — the
	// program would stop compiling with no indication of where — so one such file declines
	// the whole rename below.
	addEdit := func(loc ast.Location) {
		if seen[loc] {
			return
		}
		seen[loc] = true
		editURI, editSource := uri, source
		if loc.File != "" && !sameFile(loc.File, analysis.file) {
			otherURI, otherSource, ok := h.sourceOf(loc.File)
			if !ok {
				unreadable = loc.File
				return
			}
			editURI, editSource = otherURI, otherSource
		}
		key := lsp.DocumentURI(editURI)
		changes[key] = append(changes[key], lsp.TextEdit{
			Range:   locToRange(editSource, loc),
			NewText: newName,
		})
	}

	// Edit the declaration name span (not the whole-stmt location).
	addEdit(anchor.nameLoc)

	// Every file of the program, widened to the importers of an exported name, whose
	// uses are the rest of the rename (importers.go). Found from the declaring module,
	// since a rename may start from a use in one of its importers.
	indexed := h.importerAnalysis(analysis, source, anchor.exported, anchor.nameLoc.File)
	views := programViews(indexed)

	// The binding's uses: identifiers, calls through a namespace or UFCS, and import
	// members. The walk references uses, so the two cannot disagree about what a use is.
	for _, loc := range bindingOccurrences(views, anchor.name, anchor.named) {
		addEdit(loc)
	}

	// A **type or trait** is also written in signatures, and those live in the index
	// rather than in the expression tree — in every file, since the index covers the whole
	// import graph.
	if indexed.symTable != nil {
		for _, ref := range indexed.symTable.TypeRefs.Named(anchor.name) {
			named, ok := lookupTypeOrTrait(indexed, ref)
			if ok && namedNameLoc(named) == anchor.nameLoc {
				addEdit(ref.Loc)
			}
		}
	}

	// The expression-position uses of a type — a struct literal, a constructor — which the
	// index does not hold. **Renaming without these produces a broken program**: the type
	// would be renamed everywhere except where it is constructed, and the editor would
	// report success.
	for _, loc := range typeExprOccurrences(views, anchor.name, anchor.nameLoc) {
		addEdit(loc)
	}

	if unreadable != "" {
		log.Printf("rename: cannot read %s — declining rather than editing partially", unreadable)
		return nil, nil
	}

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

	span := anchor.nameLoc
	if span.File != "" && !sameFile(span.File, analysis.file) {
		span = anchor.cursorLoc
	}
	return &lsp.PrepareRenameResult{
		Range:       locToRange(source, span),
		Placeholder: anchor.name,
	}, nil
}
