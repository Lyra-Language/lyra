package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// UseBeforeDeclarationError reports a variable that was used before its
// declaration within the same lexical scope.
type UseBeforeDeclarationError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e UseBeforeDeclarationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckUseBeforeDeclaration analyzes the given program for uses of variables
// before their declaration within the same scope. It returns a (possibly empty)
// slice of errors.
func CheckUseBeforeDeclaration(program *ast.Program) []UseBeforeDeclarationError {
	c := &ubeChecker{}

	stmts := make([]ast.Statement, 0, len(program.Statements))
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			stmts = append(stmts, stmt)
		}
	}
	c.checkStatements(stmts)
	return c.errors
}

// ubeChecker accumulates use-before-declaration errors as it walks the AST.
type ubeChecker struct {
	errors []UseBeforeDeclarationError
}

func (c *ubeChecker) report(loc ast.Location, name string) {
	c.errors = append(c.errors, UseBeforeDeclarationError{
		Code:     diag.CodeUseBeforeDeclaration,
		Message:  fmt.Sprintf("variable %q used before its declaration", name),
		Location: loc,
	})
}

// checkStatements performs the two-pass algorithm over a flat list of statements.
func (c *ubeChecker) checkStatements(stmts []ast.Statement) {
	// Pass 1: collect names of variables declared directly in this block.
	declared := map[string]bool{}
	for _, stmt := range stmts {
		for _, name := range directDeclaredNames(stmt) {
			declared[name] = true
		}
	}

	// Pass 2: walk each statement in order; flag identifiers whose declaration
	// has not been seen yet.
	seen := map[string]bool{}
	for _, stmt := range stmts {
		c.checkStmt(stmt, declared, seen)
		for _, name := range directDeclaredNames(stmt) {
			seen[name] = true
		}
	}
}

// checkStmt walks a single statement for use-before-declaration.
func (c *ubeChecker) checkStmt(stmt ast.Statement, declared, seen map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		c.checkExpr(s.Value, declared, seen)

	case *ast.DestructuringDeclStmt:
		c.checkExpr(s.Value, declared, seen)

	case *ast.VarReassignmentStmt:
		if declared[s.Name] && !seen[s.Name] {
			c.report(s.GetLocation(), s.Name)
		}
		c.checkExpr(s.Value, declared, seen)

	case *ast.WithStmt:
		c.checkExpr(s.Arena, declared, seen)
		c.checkStatements(s.Body.Statements)

	case *ast.IfDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value, declared, seen)
		c.checkStatements(s.Then.Statements)
		if s.Else != nil {
			c.checkStatements(s.Else.Statements)
		}

	case *ast.ElseDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value, declared, seen)
		c.checkStatements(s.Else.Statements)

	default:
		// All other statements have only expression children; delegate to walker.
		ast.WalkStmt(stmt, nil, func(expr ast.Expression) bool {
			c.checkExpr(expr, declared, seen)
			return false
		})
	}
}

// checkExpr walks an expression for use-before-declaration. Scope-creating
// nodes and identifier checks are handled explicitly; everything else returns
// true to let the walker recurse automatically.
func (c *ubeChecker) checkExpr(expr ast.Expression, declared, seen map[string]bool) {
	ast.WalkExpr(expr, nil, func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.IdentifierExpr:
			if declared[ex.Name] && !seen[ex.Name] {
				c.report(ex.GetLocation(), ex.Name)
			}
			return false

		case *ast.SpreadExpr:
			if declared[ex.Name] && !seen[ex.Name] {
				c.report(ex.GetLocation(), ex.Name)
			}
			return false

		case *ast.MathAssignOpExpr:
			if declared[ex.Left.Name] && !seen[ex.Left.Name] {
				c.report(ex.Left.GetLocation(), ex.Left.Name)
			}
			c.checkExpr(ex.Right, declared, seen)
			return false

		case *ast.BlockExpr:
			c.checkStatements(ex.Statements)
			return false

		case *ast.LambdaExpr:
			c.checkExprNewScope(ex.Body)
			for _, clause := range ex.LambdaClauses {
				c.checkExprNewScope(clause.Body)
			}
			return false

		case *ast.MatchExpr:
			c.checkExpr(ex.Scrutinee, declared, seen)
			for _, arm := range ex.MatchArms {
				if arm.Guard != nil {
					c.checkExprNewScope(arm.Guard.Condition)
				}
				c.checkExprNewScope(arm.Body)
			}
			return false

		case *ast.ForLoopExpr:
			c.checkStatements(ex.Body.Statements)
			return false

		case *ast.ForInLoopExpr:
			c.checkExpr(ex.Iterable, declared, seen)
			c.checkStatements(ex.Body.Statements)
			return false

		case *ast.UnsafeBlockExpr:
			c.checkStatements(ex.Body.Statements)
			return false
		}

		return true
	})
}

// checkExprNewScope checks an expression in a completely fresh scope.
func (c *ubeChecker) checkExprNewScope(expr ast.Expression) {
	if expr == nil {
		return
	}
	if block, ok := expr.(*ast.BlockExpr); ok {
		c.checkStatements(block.Statements)
		return
	}
	c.checkExpr(expr, map[string]bool{}, map[string]bool{})
}
