package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// TryOutsideResultError reports a `?` (try) postfix operator that appears in a
// function whose declared return type is neither Result nor Maybe — there is no
// channel for the operator to short-circuit the Err/None into.
type TryOutsideResultError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e TryOutsideResultError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckTryOutsideResult walks the program AST and reports any `?` operator whose
// nearest enclosing lambda does not declare a Result/Maybe return type (or which
// appears at the top level, outside any function).
//
// This is the structural ("context") half of the try-operator checks, mirroring
// CheckAwaitOutsideAsync. Whether the operand's *kind* matches the enclosing
// return kind (e.g. propagating a Maybe out of a Result-returning function) is
// checked in the typechecker, which has access to inferred types.
func CheckTryOutsideResult(program *ast.Program) []TryOutsideResultError {
	c := &tryChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// nil enclosing return => not inside any function (top level).
			ast.WalkStmt(stmt, c.stmtVisitor(), c.exprVisitor(nil))
		}
	}
	return c.errors
}

type tryChecker struct {
	errors []TryOutsideResultError
}

func (c *tryChecker) stmtVisitor() func(ast.Statement) bool {
	return func(ast.Statement) bool { return true }
}

func (c *tryChecker) exprVisitor(enclosing *types.ReturnType) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.TryExpr:
			if enclosing == nil || !isResultOrMaybeName(enclosing.Type) {
				c.errors = append(c.errors, TryOutsideResultError{
					Code:     diag.CodeTryOutsideResult,
					Message:  "`?` can only be used inside a function returning Result or Maybe",
					Location: e.GetLocation(),
				})
			}
			return true

		case *ast.LambdaExpr:
			// Parameter default values run outside the lambda body, so they see
			// the *outer* enclosing return type (mirror the await checker).
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(), c.exprVisitor(enclosing))
			}
			// The nearest enclosing lambda's declared return type governs `?`
			// inside its body and clauses.
			ret := e.ReturnType
			walkLambdaBodies(e, c.stmtVisitor(), c.exprVisitor(&ret))
			return false
		}
		return true
	}
}

// isResultOrMaybeName reports whether t names the built-in Result or Maybe type.
//
// Recognition is by name only: Result and Maybe are currently ordinary
// user-defined data types with no canonical/prelude identity, so this also
// matches a user's own `data Result`/`data Maybe`. Harden once a prelude exists.
func isResultOrMaybeName(t types.Type) bool {
	var name string
	switch tt := t.(type) {
	case types.ParameterizedType:
		name = tt.Name
	case types.UnresolvedType:
		name = tt.Name
	case types.DataType:
		name = tt.Name
	default:
		return false
	}
	return name == "Result" || name == "Maybe"
}
