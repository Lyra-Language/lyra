package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckUnreachableCode walks the program AST and reports statements that appear
// after a terminating statement (return, break, or continue) in the same block.
// The returned diagnostics carry TagUnnecessary.
func CheckUnreachableCode(program *ast.Program) []diag.Diagnostic {
	c := &unreachableChecker{}
	stmts := make([]ast.Statement, 0, len(program.Statements))
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			stmts = append(stmts, stmt)
		}
	}
	c.checkStmts(stmts)
	return c.warnings
}

type unreachableChecker struct {
	warnings []diag.Diagnostic
}

func (c *unreachableChecker) warn(loc ast.Location) {
	if loc.StartLine <= 0 {
		return
	}
	c.warnings = append(c.warnings, diag.Diagnostic{
		Location: loc,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeUnreachableCode,
		Message:  "unreachable code",
		Tags:     []diag.Tag{diag.TagUnnecessary},
	})
}

// checkStmts scans a flat statement list for unreachable code and recurses
// into nested scopes.
func (c *unreachableChecker) checkStmts(stmts []ast.Statement) {
	for i, stmt := range stmts {
		switch stmt.(type) {
		case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
			for _, dead := range stmts[i+1:] {
				c.warn(dead.GetLocation())
			}
			c.checkStmt(stmt) // still recurse into the terminator's own expressions
			return
		}
		c.checkStmt(stmt)
	}
}

// checkStmt recurses into a single statement's child scopes.
func (c *unreachableChecker) checkStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.WithStmt:
		c.checkExpr(s.Arena)
		c.checkStmts(s.Body.Statements)
	case *ast.IfDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value)
		c.checkStmts(s.Then.Statements)
		if s.Else != nil {
			c.checkStmts(s.Else.Statements)
		}
	case *ast.ElseDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value)
		c.checkStmts(s.Else.Statements)
	default:
		ast.WalkStmt(stmt, nil, func(expr ast.Expression) bool {
			c.checkExpr(expr)
			return false
		})
	}
}

// checkExpr recurses into an expression, descending into every nested scope.
func (c *unreachableChecker) checkExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	ast.WalkExpr(expr, nil, func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.BlockExpr:
			c.checkStmts(ex.Statements)
			return false
		case *ast.LambdaExpr:
			c.checkExpr(ex.Body)
			for _, clause := range ex.LambdaClauses {
				c.checkExpr(clause.Body)
			}
			return false
		case *ast.ForLoopExpr:
			if ex.Init != nil {
				c.checkExpr(ex.Init.Value)
			}
			if ex.Condition != nil {
				c.checkExpr(*ex.Condition)
			}
			if ex.Post != nil {
				c.checkExpr(*ex.Post)
			}
			c.checkStmts(ex.Body.Statements)
			return false
		case *ast.ForInLoopExpr:
			c.checkExpr(ex.Iterable)
			c.checkStmts(ex.Body.Statements)
			return false
		case *ast.MatchExpr:
			c.checkExpr(ex.Scrutinee)
			for _, arm := range ex.MatchArms {
				if arm.Guard != nil {
					c.checkExpr(arm.Guard.Condition)
				}
				c.checkExpr(arm.Body)
			}
			return false
		case *ast.UnsafeBlockExpr:
			c.checkStmts(ex.Body.Statements)
			return false
		case *ast.GivenExpr:
			c.checkStmts(ex.Bindings)
			c.checkExpr(ex.Body)
			return false
		}
		return true
	})
}
