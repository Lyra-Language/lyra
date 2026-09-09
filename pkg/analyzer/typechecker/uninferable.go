package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A construction whose type parameters nothing ever solved.
//
// `let t = (None, 1)` is not a propagation bug — there is no context anywhere, so nothing
// could be propagated. `None` solves none of `Maybe`'s parameters, the tuple supplies
// none, and the binding has no annotation: the program is genuinely ill-typed and the
// parameter is unsolvable. It used to type-check clean and fail in the **backend** with
// `unknown named type "Maybe"`, which is the wrong end of the compiler and a message about
// an internal table rather than about the program.
//
// **A sweep rather than a check at the construction**, for the reason lyra-E069's is one:
// a constructor is settled by whichever of several contexts happens to reach it, and no
// single site knows whether another already did. Only once the statement is finished does
// "nothing solved this" mean what it says.

// checkUninferableConstructions reports every construction in node left at its bare
// generic declaration (lyra-E073).
func (tc *TypeChecker) checkUninferableConstructions(node ast.AstNode) {
	onExpr := func(e ast.Expression) bool {
		ctor, ok := e.(*ast.DataConstructorExpr)
		if !ok {
			return true
		}
		name, unsolved := tc.unsolvedConstruction(ctor)
		if !unsolved {
			return true
		}
		tc.addErrorCode(ctor.GetLocation(), SeverityError, diag.CodeUninferableType,
			"cannot tell what `%s` holds: nothing here solves %s's type parameter. "+
				"Write the type — an annotation on the binding (`let x: %s<i64> = …`), "+
				"a parameter or return type, or a turbofish",
			ctor.Constructor, name, name)
		return true
	}
	switch n := node.(type) {
	case ast.Statement:
		ast.WalkStmt(n, nil, onExpr)
	case ast.Expression:
		ast.WalkExpr(n, nil, onExpr)
	}
}

// unsolvedConstruction reports whether a construction is still the *bare* declaration of a
// **generic** data type, and names it.
//
// Three things it must not flag, each a legitimate bare `DataType`:
//
//   - a **non-generic** data type. `North` of `data Dir = North | South` records as the
//     bare `Dir` and always will — there is nothing to solve, so the declaration having no
//     parameters is the whole test.
//   - a construction already stamped to a `ParameterizedType`, which is every solved one.
//   - a `Maybe<t>` inside a **generic body**, where `t` is the enclosing function's
//     variable. That is a ParameterizedType too, so it falls out of the same test — the
//     specialization substitutes it later, and flagging it would refuse every generic
//     function that mentions a `Maybe`.
func (tc *TypeChecker) unsolvedConstruction(ctor *ast.DataConstructorExpr) (string, bool) {
	recorded, ok := tc.typeTable.Get(ctor)
	if !ok {
		return "", false
	}
	dt, isBare := recorded.(types.DataType)
	if !isBare {
		return "", false
	}
	decl, found := tc.symTable.LookupTypeFrom(dt.Name, ctor.GetLocation())
	if !found || decl == nil || len(decl.GenericParams) == 0 {
		return "", false
	}
	return dt.Name, true
}
