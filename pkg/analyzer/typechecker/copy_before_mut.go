package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Operands are evaluated left to right (LANGUAGE.md § 3), so in `(s, proceed(s, then))`
// the tuple's `s` is a copy taken before `proceed` writes through its `mut` parameter, and
// what the call did is not in the tuple. That is the rule working, and it reads as the
// opposite: Vega's Don't Save returned the tuple as the new state and did nothing (11/02).
// lyra-W032 names it where it is written.
//
// **Narrow by design.** The earlier operand must be a place on its own — `s`, `s.doc`,
// `xs[i]` — because that is the spelling that reads as "the value", where `s.n + 1` reads as
// a value computed now. It must be taken by value: a `mut` or `ref` argument lends the
// caller's storage, so it sees the write (`look(s, bump(s))` reads the new `s`). And the later
// write must be a `mut` argument the later operand passes, outside any lambda, whose place
// overlaps the earlier one (placeOverlaps: `s` and `s.doc` do, `s.a` and `s.b` do not).
// A mutating builtin cannot be the later operand: each returns `void`.

// lentArgument is what a call does with an argument it borrows.
type lentArgument struct {
	callee string
	mut    bool
}

// lend records that a call passes arg to a `mut` or `ref` parameter (checkMutArgument,
// checkExclusiveMutableBorrow), for the sweep below.
func (tc *TypeChecker) lend(arg ast.Expression, callee string, mut bool) {
	if tc.lentArguments == nil {
		tc.lentArguments = map[ast.Expression]lentArgument{}
	}
	if prior, ok := tc.lentArguments[arg]; ok && prior.mut {
		return
	}
	tc.lentArguments[arg] = lentArgument{callee: callee, mut: mut}
}

// checkCopiedBeforeMut reports, in every operand list of node, an operand copied from a
// place that a later operand of the same list changes through a `mut` argument
// (lyra-W032). Run once the statement is checked, when every call's arguments are known.
func (tc *TypeChecker) checkCopiedBeforeMut(node ast.AstNode) {
	tc.sweepingChecked = true
	defer func() { tc.sweepingChecked = false }()
	onExpr := func(e ast.Expression) bool {
		if operands, what := operandList(e); operands != nil {
			tc.checkOperands(operands, what)
		}
		return true
	}
	switch n := node.(type) {
	case ast.Statement:
		ast.WalkStmt(n, nil, onExpr)
	case ast.Expression:
		ast.WalkExpr(n, nil, onExpr)
	}
}

// operandList is the operands e evaluates left to right, and what receives them, for the
// message: a call's arguments, a tuple's or a constructor's elements, an array's, and a
// struct literal's fields as written (a record update's base first).
func operandList(e ast.Expression) ([]ast.Expression, string) {
	switch v := e.(type) {
	case *ast.FunctionCallExpr:
		if id, ok := v.Function.(*ast.IdentifierExpr); ok {
			return v.Arguments, id.Name
		}
		if m, ok := v.Function.(*ast.MemberExpr); ok {
			return v.Arguments, m.Property.Name
		}
		return v.Arguments, "the call"
	case *ast.TupleLiteralExpr:
		if !types.IsAnonymousTupleName(v.Name) {
			return v.Elements, v.Name
		}
		return v.Elements, "the tuple"
	case *ast.ArrayLiteralExpr:
		return v.Elements, "the array"
	case *ast.StructInstanceExpr:
		return structOperands(v.BaseStruct, v.Fields), "the struct"
	case *ast.AnonymousStructInstanceExpr:
		return structOperands(v.BaseStruct, v.Fields), "the struct"
	}
	return nil, ""
}

func structOperands(base ast.Expression, fields []ast.StructField) []ast.Expression {
	var operands []ast.Expression
	if base != nil {
		operands = append(operands, base)
	}
	for _, f := range fields {
		operands = append(operands, f.Value)
	}
	return operands
}

func (tc *TypeChecker) checkOperands(operands []ast.Expression, receiver string) {
	for i, early := range operands {
		if early == nil {
			continue
		}
		if _, lent := tc.lentArguments[early]; lent {
			continue // a borrow, not a copy: it sees the write
		}
		read, ok := tc.placeOf(early)
		if !ok {
			continue
		}
		for _, later := range operands[i+1:] {
			arg, callee, ok := tc.mutWriteTo(later, read)
			if !ok {
				continue
			}
			written, _ := tc.placeOf(arg)
			tc.addErrorCode(early.GetLocation(), SeverityWarning, diag.CodeCopiedBeforeMut,
				"%q is copied here, and %s changes %q later through a `mut` argument, so %s gets %q as it was before that call — operands run left to right; to keep the change, call %s first (`let r = %s(…)`) and read %q after it",
				read.String(), callee, written.String(), receiver, read.String(), callee, callee, read.String())
			return
		}
	}
}

// mutWriteTo finds a `mut` argument within e whose place overlaps read, and the callee it
// is lent to. A lambda's body is not run by e, so it is not looked in. Nor is e itself: a
// call's own `mut` argument is written once every operand is read, and a bare argument beside
// it is a value by design (`f(ys, ys)`, LANGUAGE.md § 4) — there is no earlier call to make.
func (tc *TypeChecker) mutWriteTo(e ast.Expression, read place) (ast.Expression, string, bool) {
	var found ast.Expression
	var callee string
	ast.WalkExpr(e, nil, func(x ast.Expression) bool {
		if found != nil {
			return false
		}
		if _, isLambda := x.(*ast.LambdaExpr); isLambda {
			return false
		}
		if x == e {
			return true
		}
		lent, ok := tc.lentArguments[x]
		if !ok || !lent.mut {
			return true
		}
		if p, ok := tc.placeOf(x); ok {
			if overlap, _ := placeOverlaps(read, p); overlap {
				found, callee = x, lent.callee
				return false
			}
		}
		return true
	})
	return found, callee, found != nil
}
