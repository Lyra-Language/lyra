package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// YieldOutsideGeneratorError reports a yield or yield-from expression that
// appears outside a generator function body.
type YieldOutsideGeneratorError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e YieldOutsideGeneratorError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckYieldOutsideGenerator walks the program AST and reports any yield or
// yield-from expression that is not enclosed within a generator lambda
// (LambdaExpr.IsGenerator == true).
func CheckYieldOutsideGenerator(program *ast.Program) []YieldOutsideGeneratorError {
	c := &yogChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(false), c.exprVisitor(false))
		}
	}
	return c.errors
}

type yogChecker struct {
	errors []YieldOutsideGeneratorError
}

func (c *yogChecker) report(loc ast.Location, isYieldFrom bool) {
	kw := "yield"
	if isYieldFrom {
		kw = "yield from"
	}
	c.errors = append(c.errors, YieldOutsideGeneratorError{
		Code:     diag.CodeYieldOutsideGenerator,
		Message:  fmt.Sprintf("%s expression outside of a generator function", kw),
		Location: loc,
	})
}

func (c *yogChecker) stmtVisitor(_ bool) func(ast.Statement) bool {
	return func(ast.Statement) bool { return true }
}

func (c *yogChecker) exprVisitor(inGenerator bool) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.YieldExpr:
			if !inGenerator {
				c.report(expr.GetLocation(), false)
			}
			return true

		case *ast.YieldFromExpr:
			if !inGenerator {
				c.report(expr.GetLocation(), true)
			}
			return true

		case *ast.LambdaExpr:
			// Parameter default values run outside the lambda body.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(false), c.exprVisitor(false))
			}
			isGen := e.IsGenerator
			ast.WalkExpr(e.Body, c.stmtVisitor(isGen), c.exprVisitor(isGen))
			for _, clause := range e.LambdaClauses {
				ast.WalkExpr(clause.Body, c.stmtVisitor(isGen), c.exprVisitor(isGen))
			}
			return false
		}
		return true
	}
}
