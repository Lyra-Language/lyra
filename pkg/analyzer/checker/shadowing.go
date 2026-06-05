package checker

import (
	"fmt"
	"maps"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// ShadowingWarning reports a variable declaration that shadows a name from an
// enclosing scope.
type ShadowingWarning struct {
	Message  string
	Location ast.Location
}

func (w ShadowingWarning) Error() string { return w.Message }

// CheckShadowing analyzes the given program for declarations that shadow names
// from enclosing scopes. It returns a (possibly empty) slice of warnings.
func CheckShadowing(program *ast.Program) []ShadowingWarning {
	c := &shadowChecker{}

	stmts := make([]ast.Statement, 0, len(program.Statements))
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			stmts = append(stmts, stmt)
		}
	}
	c.checkStatements(stmts, map[string]bool{})
	return c.warnings
}

// shadowChecker accumulates shadowing warnings as it walks the AST.
type shadowChecker struct {
	warnings []ShadowingWarning
}

func (c *shadowChecker) warn(loc ast.Location, name string) {
	c.warnings = append(c.warnings, ShadowingWarning{
		Message:  fmt.Sprintf("%s shadows a variable declared in an outer scope", name),
		Location: loc,
	})
}

// checkStatements walks a list of statements, threading the outerNames set
// (names visible from ancestor scopes) and an accumulated set (outerNames plus
// names declared so far in this block).
func (c *shadowChecker) checkStatements(stmts []ast.Statement, outerNames map[string]bool) {
	accumulated := copyMap(outerNames)
	for _, stmt := range stmts {
		c.checkStmt(stmt, outerNames, accumulated)
		for _, name := range directDeclaredNames(stmt) {
			accumulated[name] = true
		}
	}
}

// checkStmt walks a single statement for shadowing.
func (c *shadowChecker) checkStmt(stmt ast.Statement, outerNames, accumulated map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		c.checkExpr(s.Value, accumulated)
		if outerNames[s.Name] {
			c.warn(s.GetLocation(), s.Name)
		}

	case *ast.DestructuringDeclStmt:
		c.checkExpr(s.Value, accumulated)
		for _, name := range patternBoundNames(s.Pattern) {
			if outerNames[name] {
				c.warn(s.GetLocation(), name)
			}
		}

	case *ast.WithStmt:
		c.checkExpr(s.Arena, accumulated)
		c.checkStatements(s.Body.Statements, accumulated)

	case *ast.IfDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value, accumulated)
		thenOuter := copyMap(accumulated)
		for _, name := range patternBoundNames(s.DestructuringStatement.Pattern) {
			thenOuter[name] = true
		}
		c.checkStatements(s.Then.Statements, thenOuter)
		if s.Else != nil {
			c.checkStatements(s.Else.Statements, accumulated)
		}

	case *ast.ElseDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value, accumulated)
		elseOuter := copyMap(accumulated)
		for _, name := range patternBoundNames(s.DestructuringStatement.Pattern) {
			elseOuter[name] = true
		}
		c.checkStatements(s.Else.Statements, elseOuter)

	default:
		// All other statements have only expression children; delegate to walker.
		ast.WalkStmt(stmt, nil, func(expr ast.Expression) bool {
			c.checkExpr(expr, accumulated)
			return false
		})
	}
}

// checkExpr walks an expression for shadowing. Scope-creating nodes are handled
// explicitly; all other nodes are handled by returning true to let the walker
// recurse automatically.
func (c *shadowChecker) checkExpr(expr ast.Expression, outerNames map[string]bool) {
	ast.WalkExpr(expr, nil, func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.BlockExpr:
			c.checkStatements(ex.Statements, outerNames)
			return false

		case *ast.LambdaExpr:
			bodyOuter := copyMap(outerNames)
			for _, param := range ex.Parameters {
				for _, name := range patternBoundNames(param.Pattern) {
					bodyOuter[name] = true
				}
			}
			c.checkExpr(ex.Body, bodyOuter)
			for _, clause := range ex.LambdaClauses {
				clauseOuter := copyMap(outerNames)
				for _, pat := range clause.Patterns {
					for _, name := range patternBoundNames(pat) {
						clauseOuter[name] = true
					}
				}
				c.checkExpr(clause.Body, clauseOuter)
			}
			return false

		case *ast.ForLoopExpr:
			forOuter := copyMap(outerNames)
			if ex.Init != nil {
				c.checkExpr(ex.Init.Value, outerNames)
				if outerNames[ex.Init.Name] {
					c.warn(ex.Init.GetLocation(), ex.Init.Name)
				}
				forOuter[ex.Init.Name] = true
			}
			if ex.Condition != nil {
				c.checkExpr(*ex.Condition, forOuter)
			}
			if ex.Post != nil {
				c.checkExpr(*ex.Post, forOuter)
			}
			c.checkStatements(ex.Body.Statements, forOuter)
			return false

		case *ast.ForInLoopExpr:
			c.checkExpr(ex.Iterable, outerNames)
			bodyOuter := copyMap(outerNames)
			if ex.Key != "" {
				bodyOuter[ex.Key] = true
			}
			if ex.Value != "" {
				bodyOuter[ex.Value] = true
			}
			c.checkStatements(ex.Body.Statements, bodyOuter)
			return false

		case *ast.MatchExpr:
			c.checkExpr(ex.Scrutinee, outerNames)
			for _, arm := range ex.MatchArms {
				armOuter := copyMap(outerNames)
				for _, name := range patternBoundNames(arm.Pattern) {
					armOuter[name] = true
				}
				if arm.Guard != nil {
					c.checkExpr(arm.Guard.Condition, armOuter)
				}
				c.checkExpr(arm.Body, armOuter)
			}
			return false

		case *ast.UnsafeBlockExpr:
			c.checkStatements(ex.Body.Statements, outerNames)
			return false

		case *ast.GivenExpr:
			bindingOuter := copyMap(outerNames)
			for _, bstmt := range ex.Bindings {
				switch bs := bstmt.(type) {
				case *ast.VarDeclStmt:
					c.checkExpr(bs.Value, bindingOuter)
				case *ast.DestructuringDeclStmt:
					c.checkExpr(bs.Value, bindingOuter)
				}
				for _, name := range directDeclaredNames(bstmt) {
					bindingOuter[name] = true
				}
			}
			c.checkExpr(ex.Body, bindingOuter)
			return false
		}

		return true
	})
}

// copyMap returns a shallow copy of a map[string]bool.
func copyMap(m map[string]bool) map[string]bool {
	result := make(map[string]bool, len(m))
	maps.Copy(result, m)
	return result
}
