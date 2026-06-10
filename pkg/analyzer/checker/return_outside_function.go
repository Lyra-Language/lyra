package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// ReturnOutsideFunctionError reports a return statement that appears outside
// any function body.
type ReturnOutsideFunctionError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e ReturnOutsideFunctionError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckReturnOutsideFunction walks the program AST and reports any return
// statements that appear at the top level or in any context that is not
// nested inside a lambda / function body.
func CheckReturnOutsideFunction(program *ast.Program) []ReturnOutsideFunctionError {
	c := &rofChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(0), c.exprVisitor(0))
		}
	}
	return c.errors
}

type rofChecker struct {
	errors []ReturnOutsideFunctionError
}

func (c *rofChecker) report(loc ast.Location) {
	c.errors = append(c.errors, ReturnOutsideFunctionError{
		Code:     diag.CodeReturnOutsideFunction,
		Message:  "return statement outside of a function body",
		Location: loc,
	})
}

func (c *rofChecker) stmtVisitor(depth int) func(ast.Statement) bool {
	return func(stmt ast.Statement) bool {
		if _, ok := stmt.(*ast.ReturnStmt); ok && depth == 0 {
			c.report(stmt.GetLocation())
		}
		return true
	}
}

func (c *rofChecker) exprVisitor(depth int) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		lambda, ok := expr.(*ast.LambdaExpr)
		if !ok {
			return true
		}
		// Lambda body is a new function scope — recurse with depth+1.
		ast.WalkExpr(lambda.Body, c.stmtVisitor(depth+1), c.exprVisitor(depth+1))
		for _, clause := range lambda.LambdaClauses {
			ast.WalkExpr(clause.Body, c.stmtVisitor(depth+1), c.exprVisitor(depth+1))
		}
		return false
	}
}
