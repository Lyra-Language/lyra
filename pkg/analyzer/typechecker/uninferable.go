package typechecker

import (
	"strings"

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
// generic declaration (lyra-E073) — a nullary one (`None`), or an applied one whose payload
// solves only some of the parameters (`Ok(5)` says nothing of `e`).
//
// **The applied form was missed until 09/28**: `let r = Ok(5)` type-checked and failed in
// the backend ("type variable t has no concrete type here"), as did the same construction
// used later, returned later, discarded, or nested (`Some(Ok(5))`, whose inner `Ok` is the
// bare node reported here). A later use does not settle a binding — `let m = None` was
// already refused at the binding — so the construction is where the type must be written.
func (tc *TypeChecker) checkUninferableConstructions(node ast.AstNode) {
	onExpr := func(e ast.Expression) bool {
		if app, isApp := e.(*ast.TupleLiteralExpr); isApp {
			tc.reportUnsolvedApplication(app)
			return true
		}
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

// reportUnsolvedApplication is lyra-E073 for an applied constructor (`Ok(5)`) recorded as
// its bare generic declaration, naming the parameters its payload cannot solve — the ones
// the constructor's own fields never mention, which is why no argument could.
func (tc *TypeChecker) reportUnsolvedApplication(app *ast.TupleLiteralExpr) {
	if tc.badTurbofish[app] {
		return // its wrong count is the one mistake, and already reported
	}
	recorded, ok := tc.typeTable.Get(app)
	if !ok {
		return
	}
	dt, isBare := recorded.(types.DataType)
	if !isBare {
		return
	}
	decl, found := tc.symTable.LookupTypeFrom(dt.Name, app.GetLocation())
	if !found || decl == nil || len(decl.GenericParams) == 0 {
		return
	}
	mentioned := map[string]bool{}
	for _, c := range dt.Constructors {
		if c.Name == app.Name {
			for _, f := range c.FieldTypes() {
				types.CollectTypeVars(f, mentioned)
			}
		}
	}
	var open []string
	for _, p := range decl.GenericParams {
		if !mentioned[p.Name] {
			open = append(open, "`"+p.Name+"`")
		}
	}
	what := dt.Name + "'s type parameters"
	if len(open) > 0 {
		what = dt.Name + "'s " + strings.Join(open, " and ")
	}
	tc.addErrorCode(app.GetLocation(), SeverityError, diag.CodeUninferableType,
		"cannot tell what `%s(…)` builds: its payload does not say, and nothing here solves %s. "+
			"Write the type — an annotation on the binding (`let x: %s<…> = …`), a parameter "+
			"or return type, or a turbofish (`%s::<…>(…)`)",
		app.Name, what, dt.Name, app.Name)
}
