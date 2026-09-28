package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckByRefFunctionValues refuses a function with a by-reference parameter — any `mut`, or
// a `ref` on a non-scalar (types.IsByRefParam) — used as a *value* (`lyra-E082`).
//
// **A call through a function value passes every argument by value**: the backend lowers
// an indirect call from the function type alone, and a function type carries no borrow
// mode it acts on. A function compiled to receive a pointer and called with the value
// reads the value as an address. Two shapes reach that, and both are refused here:
//
//   - a **nested lambda** — any lambda not the value of a top-level `let` — is always a
//     closure value, even when only ever called by the name it is bound to. The backend
//     refused it at `build` while `check` accepted it (the gap todo.md recorded).
//   - a **named function referenced other than as a callee** (`apply(inc, n)`). This one
//     was worse than a late error: the adapter a named function gets as a value passed the
//     argument by value, so `inc`'s `mut i64` received the number as an address and the
//     program segfaulted. It became reachable for scalars when `mut` went by reference
//     (09/27); a `mut` struct had it already (09/28, found fixing the first shape).
//
// Carrying the mode in the function type and passing through it by reference would lift
// both; until then the refusal is the honest answer, at `check`.
func CheckByRefFunctionValues(program *ast.Program, tt *typetable.TypeTable) []diag.Diagnostic {
	declared := map[*ast.LambdaExpr]bool{}
	for _, node := range program.Statements {
		if v, ok := node.(*ast.VarDeclStmt); ok {
			if lam, isLambda := v.Value.(*ast.LambdaExpr); isLambda {
				declared[lam] = true
			}
		}
	}
	c := &byRefValues{tt: tt, declared: declared, callees: map[ast.Expression]bool{}}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, c.visit)
		}
	}
	return c.out
}

type byRefValues struct {
	tt       *typetable.TypeTable
	declared map[*ast.LambdaExpr]bool
	// callees are the expressions in a call's function position: there a function is
	// called directly, with its declaration's calling convention, not used as a value.
	callees map[ast.Expression]bool
	out     []diag.Diagnostic
}

func (c *byRefValues) visit(e ast.Expression) bool {
	switch ex := e.(type) {
	case *ast.FunctionCallExpr:
		c.callees[ex.Function] = true
	case *ast.LambdaExpr:
		if !c.declared[ex] {
			for i := range ex.Parameters {
				p := &ex.Parameters[i]
				if types.IsByRefParam(p.TypeModifier, p.Type) {
					c.report(p.GetLocation(), fmt.Sprintf(
						"a lambda written inside a function is a function value, and its `%s` parameter cannot be passed by reference through one — declare it as a top-level function, or take the parameter by value and return the result",
						p.TypeModifier))
					break
				}
			}
		}
	case *ast.IdentifierExpr:
		if c.callees[ex] {
			return true
		}
		t, ok := c.tt.Get(ex)
		if !ok {
			return true
		}
		if lt, isLambda := t.(*types.LambdaType); isLambda {
			for _, p := range lt.Parameters {
				if types.IsByRefParam(p.Borrow, p.Type) {
					c.report(ex.GetLocation(), fmt.Sprintf(
						"%q takes a `%s` parameter, so it cannot be used as a function value: a call through one passes every argument by value — call it directly, or wrap the call in a function that takes the value",
						ex.Name, p.Borrow))
					break
				}
			}
		}
	}
	return true
}

func (c *byRefValues) report(loc ast.Location, message string) {
	c.out = append(c.out, diag.Diagnostic{
		Location: loc,
		Severity: diag.SeverityError,
		Code:     diag.CodeByRefFunctionValue,
		Message:  message,
	})
}
