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
// nothing and leaves the join as it was. The same holds one level down and deeper
// (`Some(Ok(1))` beside `Some(Err("x"))`), through the payloads (settleOpenArguments).

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
// join, is still open — or `common` unchanged — recording an incomplete top-level merge on
// the join so an enclosing join can use it.
func (tc *TypeChecker) joinPartialSolves(join ast.Expression, common types.Type, branches []ast.Expression) types.Type {
	settled, partial := tc.mergeBranches(join.GetLocation(), common, branches)
	if partial != nil {
		tc.recordPartialSolve(join, partial)
	}
	return settled
}

// mergeBranches is joinPartialSolves without the recording, so it can recurse. `common` is
// open in one of two ways:
//
//   - **a bare generic declaration** (`Result`): the branches' partial solutions merge
//     into its arguments, answering the instantiation when they cover every parameter, or
//     `common` with the incomplete merge;
//   - **an instantiation with an open argument** (`Maybe<Result>`, from `Some(Ok(1))` beside
//     `Some(Err("x"))`): each open argument is settled by merging, recursively, the
//     payloads the branches supply at the fields that parameter types (settleOpenArguments).
func (tc *TypeChecker) mergeBranches(loc ast.Location, common types.Type, branches []ast.Expression) (types.Type, map[string]types.Type) {
	switch c := common.(type) {
	case types.ParameterizedType:
		return tc.settleOpenArguments(loc, c, branches), nil
	case types.DataType:
		decl, found := tc.symTable.LookupTypeRef(c.Name, c.Key, loc)
		if !found || decl == nil || len(decl.GenericParams) == 0 {
			return common, nil
		}
		merged := map[string]types.Type{}
		for _, b := range branches {
			part, known := tc.partialSolveOf(b, decl)
			if !known {
				return common, nil
			}
			for name, t := range part {
				if prev, seen := merged[name]; seen && !types.TypesEqual(prev, t) {
					return common, nil
				}
				merged[name] = t
			}
		}
		if inst, complete := parameterizedResult(c, decl, merged); complete {
			return inst, nil
		}
		return common, merged
	}
	return common, nil
}

// settleOpenArguments settles each open argument of inst from the branches' payloads.
// Nothing changes unless every branch's payload is known — a construction of inst's type,
// a nullary one, a branch that never falls through, or an `if`/`match` of those.
func (tc *TypeChecker) settleOpenArguments(loc ast.Location, inst types.ParameterizedType, branches []ast.Expression) types.Type {
	if !tc.leavesArgumentOpen(inst, loc) {
		return inst
	}
	decl, found := tc.symTable.LookupTypeRef(inst.Name, inst.Key, loc)
	if !found || decl == nil || len(decl.GenericParams) != len(inst.TypeArguments) {
		return inst
	}
	args := append([]types.Type(nil), inst.TypeArguments...)
	changed := false
	for i, arg := range inst.TypeArguments {
		if !tc.argumentIsOpen(arg, loc) {
			continue
		}
		var payloads []ast.Expression
		for _, b := range branches {
			more, known := tc.payloadsFor(b, decl, i)
			if !known {
				return inst
			}
			payloads = append(payloads, more...)
		}
		if len(payloads) == 0 {
			continue
		}
		if settled, _ := tc.mergeBranches(loc, arg, payloads); !types.TypesEqual(settled, arg) {
			args[i] = settled
			changed = true
		}
	}
	if !changed {
		return inst
	}
	out := inst
	out.TypeArguments = args
	return out
}

// argumentIsOpen reports whether a type argument is what a construction leaves for its
// context: a bare generic data type, or an instantiation carrying one.
func (tc *TypeChecker) argumentIsOpen(arg types.Type, loc ast.Location) bool {
	switch a := arg.(type) {
	case types.DataType:
		decl, ok := tc.symTable.LookupTypeRef(a.Name, a.Key, loc)
		return ok && decl != nil && len(decl.GenericParams) > 0
	case types.ParameterizedType:
		return tc.leavesArgumentOpen(a, loc)
	}
	return false
}

// payloadsFor is what branch e supplies for decl's parameter `index`: the payload at each
// field whose declared type is exactly that parameter. Known and empty for a nullary
// construction or a branch that never falls through; an `if` or `match` supplies its own
// branches'. Not known for anything else, including a field that mentions the parameter
// inside another type (`[]t`), whose payload is not itself of the argument's type.
func (tc *TypeChecker) payloadsFor(e ast.Expression, decl *ast.TypeDeclStmt, index int) ([]ast.Expression, bool) {
	e = tailExpression(e)
	if e == nil {
		return nil, false
	}
	if t, ok := tc.typeTable.Get(e); ok {
		if _, isNever := t.(types.NeverType); isNever {
			return nil, true
		}
	}
	switch v := e.(type) {
	case *ast.IfExpr:
		if v.Else == nil {
			return nil, false
		}
		a, okA := tc.payloadsFor(v.Then, decl, index)
		b, okB := tc.payloadsFor(v.Else, decl, index)
		return append(a, b...), okA && okB
	case *ast.MatchExpr:
		var out []ast.Expression
		for _, arm := range v.MatchArms {
			more, ok := tc.payloadsFor(arm.Body, decl, index)
			if !ok {
				return nil, false
			}
			out = append(out, more...)
		}
		return out, true
	case *ast.DataConstructorExpr:
		return nil, true
	case *ast.TupleLiteralExpr:
		dt, isData := decl.Type.(types.DataType)
		if !isData {
			return nil, false
		}
		param := decl.GenericParams[index].Name
		for _, c := range dt.Constructors {
			if c.Name != v.Name {
				continue
			}
			var out []ast.Expression
			for j, f := range c.FieldTypes() {
				if g, isVar := f.(types.GenericType); isVar && g.Name == param {
					if j < len(v.Elements) {
						out = append(out, v.Elements[j])
					}
					continue
				}
				mentions := map[string]bool{}
				types.CollectTypeVars(f, mentions)
				if mentions[param] {
					return nil, false
				}
			}
			return out, true
		}
	}
	return nil, false
}

// tailExpression is a branch's value expression: a block's final expression statement,
// or the expression itself.
func tailExpression(e ast.Expression) ast.Expression {
	for {
		b, isBlock := e.(*ast.BlockExpr)
		if !isBlock || len(b.Statements) == 0 {
			return e
		}
		tail, isExpr := b.Statements[len(b.Statements)-1].(*ast.ExpressionStmt)
		if !isExpr {
			return e
		}
		e = tail.Expression
	}
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
