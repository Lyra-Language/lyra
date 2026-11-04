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
//
// **An `own` parameter is refused the same way**, on anything but a scalar, where it is
// inert. It goes by value, so nothing misreads an address, but a call through a function
// value keeps its argument while the callee releases it: `apply(keep, t)` freed `t` under
// the caller. A nested lambda is excepted when it is bound to a name and only called by it,
// since that call knows the mode; used any other way, the name is refused below.
func CheckByRefFunctionValues(program *ast.Program, tt *typetable.TypeTable) []diag.Diagnostic {
	declared := map[*ast.LambdaExpr]bool{}
	for _, node := range program.Statements {
		if v, ok := node.(*ast.VarDeclStmt); ok {
			if lam, isLambda := v.Value.(*ast.LambdaExpr); isLambda {
				declared[lam] = true
			}
		}
	}
	c := &byRefValues{tt: tt, declared: declared, named: map[*ast.LambdaExpr]bool{}, callees: map[ast.Expression]bool{}}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.visitStmt, c.visit)
		}
	}
	return c.out
}

// ownedThroughValue reports whether a parameter's `own` would be broken by a call through a
// function value: the callee releases what that call keeps. A scalar has nothing to release.
func ownedThroughValue(mod types.TypeModifier, t types.Type) bool {
	return mod == types.Own && !types.IsCopiedScalar(t)
}

type byRefValues struct {
	tt       *typetable.TypeTable
	declared map[*ast.LambdaExpr]bool
	// named are the lambdas bound to a name by a `let`, nested ones included: a use of the
	// name other than a call is what a by-value `own` is refused at.
	named map[*ast.LambdaExpr]bool
	// callees are the expressions in a call's function position: there a function is
	// called directly, with its declaration's calling convention, not used as a value.
	callees map[ast.Expression]bool
	out     []diag.Diagnostic
}

func (c *byRefValues) visitStmt(s ast.Statement) bool {
	if v, ok := s.(*ast.VarDeclStmt); ok {
		if lam, isLambda := v.Value.(*ast.LambdaExpr); isLambda {
			c.named[lam] = true
		}
	}
	return true
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
				if !c.named[ex] && ownedThroughValue(p.TypeModifier, p.Type) {
					c.report(p.GetLocation(),
						"a lambda used as a function value can't take an `own` parameter: a call through one keeps its argument, and this lambda would release it — take the parameter bare")
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
				if ownedThroughValue(p.Borrow, p.Type) {
					c.report(ex.GetLocation(), fmt.Sprintf(
						"%q takes an `own` parameter, so it cannot be used as a function value: a call through one keeps its argument, and %q would release it — call it directly, or wrap the call in a function that takes the value",
						ex.Name, ex.Name))
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
