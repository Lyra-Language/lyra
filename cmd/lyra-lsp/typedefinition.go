package main

import (
	"context"
	"log"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// TypeDefinition implements textDocument/typeDefinition: given a *value*, jump to the
// declaration of its **type**.
//
// The one of the three "Go to …" items that asks a different question. Definition and
// Declaration both answer "where does this name come from"; this answers "what is this
// thing", which for `let e = events.next_event()` is `Event` rather than the `let`. On a
// constructor it is the data type the constructor belongs to; on a binding, the declaration
// of whatever it holds.
//
// **It answers only where a declaration exists to jump to.** A value whose type is `i64`, a
// tuple, a function or an array has no declaration — the type is structural — and the honest
// answer there is nothing, not the nearest enclosing something.
func (h *Handler) TypeDefinition(_ context.Context, params *lsp.TypeDefinitionParams) (result []lsp.Location, retErr error) {
	defer recoverHandler("typeDefinition", &result, &retErr)

	c, ok := h.cursorAt(string(params.TextDocument.URI), params.Position)
	if !ok {
		return nil, nil
	}
	uri, analysis, source, line, col := c.uri, c.analysis, c.source, c.line, c.col

	decl, ok := typeDeclarationAt(analysis, line, col)
	if !ok {
		log.Printf("typeDefinition: nothing with a declared type at %d:%d", line, col)
		return nil, nil
	}
	target, ok := h.locationIn(uri, source, analysis, namedNameLoc(decl))
	if !ok {
		return nil, nil
	}
	return []lsp.Location{target}, nil
}

// typeDeclarationAt is the declaration of the type of whatever the cursor is on.
//
// Three positions can answer, and they are asked in the order that makes each mean what a
// reader expects:
//
//   - an **expression**, whose recorded type is what the typechecker settled on. This is the
//     ordinary case: a binding, a call result, a field access.
//   - a **constructor in a pattern**, whose type is the data type declaring it — `Keyboard`
//     answers `Event`, not `Key`. Asked before the pattern-binding case because a
//     constructor's own span encloses its payload's.
//   - a **name a pattern binds**, whose type is not recorded anywhere: the TypeTable is
//     keyed by expression. Answered by looking at what the *scrutinee* was, which the arm
//     scope now makes reachable — the binding resolves to a declaration whose type is known.
func typeDeclarationAt(analysis *docAnalysis, line, col int) (*ast.TypeDeclStmt, bool) {
	if analysis.symTable == nil {
		return nil, false
	}
	if expr := findExprAtPos(analysis.program, line, col); expr != nil {
		if t, ok := analysis.typeTable.Get(expr); ok && t != nil {
			if decl, ok := declarationOfType(analysis, t, expr.GetLocation()); ok {
				return decl, true
			}
		}
	}
	// A **declaration's own name**, which is neither an expression nor a pattern: a
	// `VarDeclStmt` holds its name as a bare string, so no walk sees it (CLAUDE.md rule 8's
	// "a field that is a bare string is invisible to every walk"). Asking from a `let` what
	// type it holds is an ordinary thing to want, and rename already walks declaration names
	// for the same reason.
	if decl, ok := declAtNamePos(analysis, line, col); ok {
		if t := declaredType(analysis, decl); t != nil {
			if td, ok := declarationOfType(analysis, t, decl.GetLocation()); ok {
				return td, true
			}
		}
		return nil, false
	}
	if pat := findPatternAtPos(analysis.program, line, col); pat != nil {
		if dp, ok := pat.(*ast.DataPattern); ok && cursorOnName(dp.GetLocation(), dp.Name, line, col) {
			if decl, ok := analysis.symTable.DeclaringDataType(dp.Name, dp.GetLocation()); ok {
				return decl, true
			}
			return anyDeclaringDataType(analysis, dp.Name)
		}
	}
	return nil, false
}

// declAtNamePos is the `let`/`var`/`const` whose *name* the cursor sits on.
func declAtNamePos(analysis *docAnalysis, line, col int) (*ast.VarDeclStmt, bool) {
	var found *ast.VarDeclStmt
	for _, node := range analysis.program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			if v, ok := s.(*ast.VarDeclStmt); ok && locationContains(v.NameLocation, line, col) {
				found = v
				return false
			}
			return true
		}, func(ast.Expression) bool { return true })
		if found != nil {
			break
		}
	}
	return found, found != nil
}

// declaredType is what a binding holds: its annotation when it has one, and otherwise the
// type its initializer was inferred to have.
func declaredType(analysis *docAnalysis, decl *ast.VarDeclStmt) types.Type {
	if decl.Type != nil {
		return decl.Type
	}
	if decl.Value != nil {
		if t, ok := analysis.typeTable.Get(decl.Value); ok {
			return t
		}
	}
	return nil
}

// declarationOfType maps a type to the declaration that introduced it, if one did.
//
// **Through the type's head**, so `Maybe<Point>` answers `Maybe` and `[]Point` answers
// nothing: an array is structural, and the element is a different question from the value's
// own type. A newtype answers itself rather than its base — it is a declaration, and the
// name the value carries.
func declarationOfType(analysis *docAnalysis, t types.Type, loc ast.Location) (*ast.TypeDeclStmt, bool) {
	name, ok := types.HeadName(t)
	if !ok || name == "" {
		return nil, false
	}
	decl, ok := analysis.symTable.LookupTypeFrom(name, loc)
	if !ok {
		// A type the file cannot name — the payload of a constructor it imported, say. The
		// same concession `Definition` makes for a constructor, and for the same reason: a
		// value legitimately has this type whether or not its module can spell it.
		return anyDeclarationNamed(analysis, name)
	}
	return decl, true
}

// anyDeclarationNamed finds a type declaration by name without requiring it to be visible
// from the asking file, deterministically.
func anyDeclarationNamed(analysis *docAnalysis, name string) (*ast.TypeDeclStmt, bool) {
	var best *ast.TypeDeclStmt
	for _, decl := range analysis.symTable.Types {
		if decl.Name != name {
			continue
		}
		if best == nil || decl.GetLocation().File < best.GetLocation().File ||
			(decl.GetLocation().File == best.GetLocation().File &&
				decl.GetLocation().StartLine < best.GetLocation().StartLine) {
			best = decl
		}
	}
	return best, best != nil
}
