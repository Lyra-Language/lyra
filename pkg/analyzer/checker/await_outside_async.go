package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// AwaitOutsideAsyncError reports an await expression that appears outside an
// async function body.
type AwaitOutsideAsyncError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e AwaitOutsideAsyncError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckAwaitOutsideAsync walks the program AST and reports any await expression
// that is not enclosed within an async lambda (LambdaExpr.IsAsync == true).
func CheckAwaitOutsideAsync(program *ast.Program) []AwaitOutsideAsyncError {
	c := &aoaChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(false), c.exprVisitor(false))
		}
	}
	return c.errors
}

type aoaChecker struct {
	errors []AwaitOutsideAsyncError
}

func (c *aoaChecker) stmtVisitor(_ bool) func(ast.Statement) bool {
	return func(ast.Statement) bool { return true }
}

func (c *aoaChecker) exprVisitor(inAsync bool) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.AwaitExpr:
			if !inAsync {
				c.errors = append(c.errors, AwaitOutsideAsyncError{
					Code:     diag.CodeAwaitOutsideAsync,
					Message:  "await expression outside of an async function",
					Location: expr.GetLocation(),
				})
			}
			return true

		case *ast.LambdaExpr:
			// Parameter default values run outside the lambda body.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(false), c.exprVisitor(false))
			}
			isAsync := e.IsAsync
			ast.WalkExpr(e.Body, c.stmtVisitor(isAsync), c.exprVisitor(isAsync))
			for _, clause := range e.LambdaClauses {
				ast.WalkExpr(clause.Body, c.stmtVisitor(isAsync), c.exprVisitor(isAsync))
			}
			return false
		}
		return true
	}
}
