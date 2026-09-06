package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckUseBeforeDeclaration analyzes the given program for uses of variables
// before their declaration within the same scope. It returns a (possibly empty)
// slice of errors.
func CheckUseBeforeDeclaration(program *ast.Program) []diag.Diagnostic {
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
	errors []diag.Diagnostic
}

func (c *ubeChecker) report(loc ast.Location, name string) {
	c.errors = append(c.errors, diag.Diagnostic{Severity: diag.SeverityError,
		Code:     diag.CodeUseBeforeDeclaration,
		Message:  fmt.Sprintf("variable %q used before its declaration", name),
		Location: loc,
	})
}

// checkStatements performs the two-pass algorithm over a flat list of statements.
func (c *ubeChecker) checkStatements(stmts []ast.Statement) {
	c.checkStatementsInScope(stmts, nil)
}

// checkStatementsInScope is checkStatements with names that are already in scope
// before the first statement runs — a function's parameters, for the body it encloses.
//
// Without them a `let` that *shadows* a parameter reads as a use before declaration:
// `(s: string) => { let s = s ++ "!"  s }` flagged the `s` on the right, because the
// block declares `s` and the checker had no idea the name also came in as a parameter.
// The equivalent shadowing of a block-local (`let x = 5` then `let x = x + 1`) always
// worked, since the first declaration marks the name seen — a parameter is simply the
// same thing declared by the signature instead of by a statement.
//
// This matters beyond the inconsistency: shadowing is the idiom that *replaces*
// reassigning a borrowed parameter, which is now an error (lyra-E025), so the escape
// hatch has to exist for the rule to be reasonable.
func (c *ubeChecker) checkStatementsInScope(stmts []ast.Statement, inScope map[string]bool) {
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
	for name := range inScope {
		seen[name] = true
	}
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

		case *ast.MathAssignOpExpr:
			if root := rootIdentExpr(ex.Left); root != nil && declared[root.Name] && !seen[root.Name] {
				c.report(root.GetLocation(), root.Name)
			}
			c.checkExpr(ex.Right, declared, seen)
			return false

		case *ast.BlockExpr:
			c.checkStatements(ex.Statements)
			return false

		case *ast.LambdaExpr:
			// A parameter is in scope for the whole body, so it is seeded as already
			// seen — a later `let` of the same name shadows it rather than using it
			// early.
			params := parameterNames(ex)
			c.checkExprInScope(ex.Body, params)
			for _, clause := range ex.LambdaClauses {
				c.checkExprInScope(clause.Body, params)
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
	c.checkExprInScope(expr, nil)
}

// checkExprInScope checks an expression in a fresh scope that already has inScope's
// names bound — a lambda body under its parameters.
func (c *ubeChecker) checkExprInScope(expr ast.Expression, inScope map[string]bool) {
	if expr == nil {
		return
	}
	if block, ok := expr.(*ast.BlockExpr); ok {
		c.checkStatementsInScope(block.Statements, inScope)
		return
	}
	seen := map[string]bool{}
	for name := range inScope {
		seen[name] = true
	}
	c.checkExpr(expr, map[string]bool{}, seen)
}

// parameterNames collects the names a lambda's parameters bind, including those bound
// by a destructuring parameter pattern.
func parameterNames(lambda *ast.LambdaExpr) map[string]bool {
	names := map[string]bool{}
	for _, p := range lambda.Parameters {
		if p.Pattern != nil {
			collectPatternNames(p.Pattern, names)
		}
	}
	return names
}

// collectPatternNames adds every name a pattern binds to names. Mirrors the capture
// pass's equivalent walker: a parameter may destructure, so the names it brings into
// scope are not always just the parameter's own identifier.
func collectPatternNames(p ast.Pattern, names map[string]bool) {
	ast.EachPatternBinding(p, func(b ast.PatternBinding) { names[b.Name] = true })
}
