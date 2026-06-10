package checker

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckUnusedVariables walks the program AST and reports local variable
// declarations that are never referenced within their scope. Top-level
// bindings are skipped. The returned diagnostics carry TagUnnecessary.
func CheckUnusedVariables(program *ast.Program) []diag.Diagnostic {
	c := &unusedVarChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			c.findScopesInStmt(stmt)
		}
	}
	return c.warnings
}

type unusedVarChecker struct {
	warnings []diag.Diagnostic
}

// checkBlock checks unused variables within a single block scope, then recurses
// into nested scopes as independent units.
func (c *unusedVarChecker) checkBlock(stmts []ast.Statement) {
	// Collect declarations made directly in this block (not crossing nested lambdas).
	type declInfo struct {
		name string
		loc  ast.Location
	}
	var decls []declInfo
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.VarDeclStmt:
			decls = append(decls, declInfo{s.Name, s.GetLocation()})
		case *ast.DestructuringDeclStmt:
			for _, name := range patternBoundNames(s.Pattern) {
				decls = append(decls, declInfo{name, s.GetLocation()})
			}
		}
	}

	if len(decls) > 0 {
		// Collect all identifier references in this scope via a full recursive walk.
		// This is conservative: captures in nested lambdas count as references,
		// preventing false positives on closed-over variables.
		refs := make(map[string]bool)
		for _, stmt := range stmts {
			ast.WalkStmt(stmt,
				func(s ast.Statement) bool {
					// Count writes (x = y) as references — avoids false positives
					// for write-before-read patterns.
					if r, ok := s.(*ast.VarReassignmentStmt); ok {
						refs[r.Name] = true
					}
					return true
				},
				func(e ast.Expression) bool {
					switch ex := e.(type) {
					case *ast.IdentifierExpr:
						refs[ex.Name] = true
					case *ast.SpreadExpr:
						refs[ex.Name] = true
					case *ast.MathAssignOpExpr:
						refs[ex.Left.Name] = true
					}
					return true
				},
			)
		}

		for _, d := range decls {
			if strings.HasPrefix(d.name, "_") {
				continue // _foo convention: intentionally unused
			}
			if !refs[d.name] {
				c.warnings = append(c.warnings, diag.Diagnostic{
					Location: d.loc,
					Severity: diag.SeverityWarning,
					Code:     diag.CodeUnusedVariable,
					Message:  fmt.Sprintf("variable %q is declared but never used", d.name),
					Tags:     []diag.Tag{diag.TagUnnecessary},
				})
			}
		}
	}

	// Recurse into nested scopes as independent scope units.
	c.findAndCheckNestedScopes(stmts)
}

// findAndCheckNestedScopes finds all scope-creating constructs in stmts and
// checks each one as an independent scope.
func (c *unusedVarChecker) findAndCheckNestedScopes(stmts []ast.Statement) {
	for _, stmt := range stmts {
		c.findScopesInStmt(stmt)
	}
}

// findScopesInStmt recurses into a statement looking for scope units.
func (c *unusedVarChecker) findScopesInStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.WithStmt:
		c.findScopesInExpr(s.Arena)
		c.checkBlock(s.Body.Statements)
	case *ast.IfDestructuringStmt:
		c.findScopesInExpr(s.DestructuringStatement.Value)
		c.checkBlock(s.Then.Statements)
		if s.Else != nil {
			c.checkBlock(s.Else.Statements)
		}
	case *ast.ElseDestructuringStmt:
		c.findScopesInExpr(s.DestructuringStatement.Value)
		c.checkBlock(s.Else.Statements)
	default:
		ast.WalkStmt(stmt, nil, func(expr ast.Expression) bool {
			c.findScopesInExpr(expr)
			return false
		})
	}
}

// findScopesInExpr recurses into an expression looking for scope units.
func (c *unusedVarChecker) findScopesInExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	ast.WalkExpr(expr, nil, func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.BlockExpr:
			c.checkBlock(ex.Statements)
			return false
		case *ast.LambdaExpr:
			c.findScopesInExpr(ex.Body)
			for _, clause := range ex.LambdaClauses {
				c.findScopesInExpr(clause.Body)
			}
			return false
		case *ast.ForLoopExpr:
			if ex.Init != nil {
				c.findScopesInExpr(ex.Init.Value)
			}
			if ex.Condition != nil {
				c.findScopesInExpr(*ex.Condition)
			}
			if ex.Post != nil {
				c.findScopesInExpr(*ex.Post)
			}
			c.checkBlock(ex.Body.Statements)
			return false
		case *ast.ForInLoopExpr:
			c.findScopesInExpr(ex.Iterable)
			c.checkBlock(ex.Body.Statements)
			return false
		case *ast.UnsafeBlockExpr:
			c.checkBlock(ex.Body.Statements)
			return false
		case *ast.GivenExpr:
			c.checkBlock(ex.Bindings)
			c.findScopesInExpr(ex.Body)
			return false
		case *ast.MatchExpr:
			c.findScopesInExpr(ex.Scrutinee)
			for _, arm := range ex.MatchArms {
				if arm.Guard != nil {
					c.findScopesInExpr(arm.Guard.Condition)
				}
				c.findScopesInExpr(arm.Body)
			}
			return false
		}
		return true
	})
}
