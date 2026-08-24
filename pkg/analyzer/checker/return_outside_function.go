package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckReturnOutsideFunction walks the program AST and reports any return
// statements that appear at the top level or in any context that is not
// nested inside a lambda / function body.
func CheckReturnOutsideFunction(program *ast.Program) []diag.Diagnostic {
	c := &rofChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(0), c.exprVisitor(0))
		}
	}
	return c.errors
}

type rofChecker struct {
	errors []diag.Diagnostic
}

func (c *rofChecker) report(loc ast.Location) {
	c.errors = append(c.errors, diag.Diagnostic{Severity: diag.SeverityError,
		Code:     diag.CodeReturnOutsideFunction,
		Message:  "return statement outside of a function body",
		Location: loc,
	})
}

// stmtVisitor reports a `return` at depth 0 and opens a new function scope for the
// two statements that hold a body without wrapping it in a LambdaExpr.
//
// **A trait method is a function, and this counted only lambdas.** `walkStmtChildren`
// descends into `TraitImplStmt.Methods[i].Clause.Body` with the *same* visitors, so a
// method body was walked at the enclosing depth — 0, for a top-level `impl` — and every
// `return` inside one was reported as being outside a function body. `impl Needle for
// rune { found_at = (…) => { if offset < 0 { return None } … } }` is ordinary code that
// could not be written; the workaround was to phrase the whole body as one tail
// expression, which is why nothing in the prelude had tripped it.
//
// The same holds for a trait's **default** method body, which walk.go descends into
// identically — a form nothing has used yet, and one that would have failed the same way.
func (c *rofChecker) stmtVisitor(depth int) func(ast.Statement) bool {
	return func(stmt ast.Statement) bool {
		switch s := stmt.(type) {
		case *ast.ReturnStmt:
			if depth == 0 {
				c.report(stmt.GetLocation())
			}
		case *ast.TraitImplStmt:
			for i := range s.Methods {
				ast.WalkExpr(s.Methods[i].Clause.Body, c.stmtVisitor(depth+1), c.exprVisitor(depth+1))
			}
			return false
		case *ast.TraitDeclStmt:
			for i := range s.Methods {
				if s.Methods[i].DefaultMethod != nil {
					ast.WalkExpr(s.Methods[i].DefaultMethod.Body, c.stmtVisitor(depth+1), c.exprVisitor(depth+1))
				}
			}
			return false
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
		walkLambdaBodies(lambda, c.stmtVisitor(depth+1), c.exprVisitor(depth+1))
		return false
	}
}
