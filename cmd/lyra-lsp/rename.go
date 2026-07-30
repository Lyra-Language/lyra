package main

import (
	"context"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// renameAnchor holds the resolved name and locations for a rename operation,
// whether the cursor was on a usage (IdentifierExpr) or on a declaration name.
type renameAnchor struct {
	name    string
	declLoc ast.Location // whole-decl location: identity key for reference matching
	nameLoc ast.Location // name-only span: the range to replace at the declaration site
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
		scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.symTable.EntryScope(), line, col)
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
				default:
					return true // descend into nested stmts
				}
				if sName == "" || !locationContains(sNameLoc, line, col) {
					return true
				}
				// Resolve the binding from the scope at the name's position.
				scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.symTable.EntryScope(), sNameLoc.StartLine, sNameLoc.StartCol)
				if n, ok2 := scope.Lookup(sName); ok2 {
					name = sName
					named = n
				}
				return false // found; stop walking
			}, func(ast.Expression) bool { return false })
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

	if name == "" || named == nil {
		return renameAnchor{}, false
	}

	return renameAnchor{
		name:    name,
		declLoc: named.GetLocation(),
		nameLoc: namedNameLoc(named),
	}, true
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
	return analysis.symTable.EntryScope()
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("rename panic: %v\n%s", r, debug.Stack())
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

	log.Printf("rename: %q → %q, %d edit(s)", anchor.name, newName, len(edits))
	return &lsp.WorkspaceEdit{
		Changes: map[lsp.DocumentURI][]lsp.TextEdit{
			lsp.DocumentURI(uri): edits,
		},
	}, nil
}

// PrepareRename implements textDocument/prepareRename, validating that the
// cursor is on a renameable identifier and returning its current range and
// text as the rename placeholder. The go-lsp library registers this capability
// automatically via the PrepareRenameHandler interface.
func (h *Handler) PrepareRename(_ context.Context, params *lsp.PrepareRenameParams) (result *lsp.PrepareRenameResult, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("prepareRename panic: %v\n%s", r, debug.Stack())
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
