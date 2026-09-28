package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Branches that each solve part of a generic type: `if c { Ok(5) } else { Err("x") }`.
// `Ok(5)` solves `t` and says nothing of `e`; `Err("x")` the reverse. Each records the
// bare `Result`, so the join of the two was the bare `Result` too and nothing settled it
// (lyra-E073 since 09/28, a backend failure before) — though the two arms together say
// exactly `Result<i64, string>`.
//
// An incomplete construction keeps what it did solve (recordPartialSolve), and a join
// whose common type is still a bare generic declaration merges its branches' partial
// solutions. Complete, the join is that instantiation, and the join sites' existing push
// (pushSettledInstantiation — provisional, so a guessed `i64` from `Ok(5)` still yields to
// a `-> Result<u8, string>` arriving later) stamps it onto every branch. Incomplete, the
// merge is recorded on the join itself, so an `if` nested in a branch contributes what it
// solved in turn. A conflict — two branches solving one parameter differently — merges
// nothing and leaves the join as it was.

// recordPartialSolve keeps an incomplete construction's partial substitution.
func (tc *TypeChecker) recordPartialSolve(expr ast.Expression, subst map[string]types.Type) {
	if len(subst) == 0 {
		return
	}
	if tc.partialSolves == nil {
		tc.partialSolves = map[ast.Expression]map[string]types.Type{}
	}
	tc.partialSolves[expr] = subst
}

// joinPartialSolves answers the instantiation `branches` jointly solve when `common`, their
// join, is a bare generic data type — or `common` unchanged.
func (tc *TypeChecker) joinPartialSolves(join ast.Expression, common types.Type, branches []ast.Expression) types.Type {
	dt, ok := common.(types.DataType)
	if !ok {
		return common
	}
	decl, found := tc.symTable.LookupTypeRef(dt.Name, dt.Key, join.GetLocation())
	if !found || decl == nil || len(decl.GenericParams) == 0 {
		return common
	}
	merged := map[string]types.Type{}
	for _, b := range branches {
		part, known := tc.partialSolveOf(b, decl)
		if !known {
			return common
		}
		for name, t := range part {
			if prev, seen := merged[name]; seen && !types.TypesEqual(prev, t) {
				return common
			}
			merged[name] = t
		}
	}
	if inst, complete := parameterizedResult(dt, decl, merged); complete {
		return inst
	}
	tc.recordPartialSolve(join, merged)
	return common
}

// partialSolveOf is what one branch solved of decl's parameters: a recorded partial
// solution, a full instantiation's arguments, nothing for a bare declaration (`None`) or a
// branch that never falls through, and not known for anything else.
func (tc *TypeChecker) partialSolveOf(e ast.Expression, decl *ast.TypeDeclStmt) (map[string]types.Type, bool) {
	if b, isBlock := e.(*ast.BlockExpr); isBlock && len(b.Statements) > 0 {
		if tail, isExpr := b.Statements[len(b.Statements)-1].(*ast.ExpressionStmt); isExpr {
			return tc.partialSolveOf(tail.Expression, decl)
		}
	}
	recorded, ok := tc.typeTable.Get(e)
	if !ok {
		return nil, false
	}
	if _, isNever := recorded.(types.NeverType); isNever {
		return map[string]types.Type{}, true
	}
	if part, has := tc.partialSolves[e]; has {
		return part, true
	}
	switch r := recorded.(type) {
	case types.ParameterizedType:
		if r.Name != decl.Name || len(r.TypeArguments) != len(decl.GenericParams) {
			return nil, false
		}
		return ast.BindGenericParams(decl.GenericParams, r.TypeArguments), true
	case types.DataType:
		if r.Name == decl.Name {
			return map[string]types.Type{}, true
		}
	}
	return nil, false
}
