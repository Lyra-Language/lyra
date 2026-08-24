package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckAwaitOutsideAsync walks the program AST and reports any await expression
// that is not enclosed within an async lambda (LambdaExpr.IsAsync == true).
func CheckAwaitOutsideAsync(program *ast.Program) []diag.Diagnostic {
	c := &aoaChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(false), c.exprVisitor(false))
		}
	}
	return c.errors
}

type aoaChecker struct {
	errors []diag.Diagnostic
}

func (c *aoaChecker) stmtVisitor(_ bool) func(ast.Statement) bool {
	return func(ast.Statement) bool { return true }
}

func (c *aoaChecker) exprVisitor(inAsync bool) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.AwaitExpr:
			if !inAsync {
				c.errors = append(c.errors, diag.Diagnostic{Severity: diag.SeverityError,
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
			walkLambdaBodies(e, c.stmtVisitor(isAsync), c.exprVisitor(isAsync))
			return false
		}
		return true
	}
}
