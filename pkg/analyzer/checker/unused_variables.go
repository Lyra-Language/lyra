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
		refs := referencedNames(stmts)
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

// referencedNames is every name read (or written) anywhere in stmts, by a full recursive
// walk. Deliberately conservative in two directions: a *write* counts as a reference, so a
// write-before-read pattern is not reported, and the walk descends into nested lambdas, so
// a captured name is not either.
//
// One walk for two questions — an unused local and an unused loop binding — because they
// are the same question about different declarations, and two copies of "is this name
// referenced" would drift about exactly the cases that make it conservative.
func referencedNames(stmts []ast.Statement) map[string]bool {
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
				case *ast.MathAssignOpExpr:
					refs[rootIdentName(ex.Left)] = true
				}
				return true
			},
		)
	}
	return refs
}

// checkLoopBindings reports a `for-in` binding the body never reads (`lyra-W020`).
//
// It is a separate code from the unused *local* above because the fix is different and is
// the whole point of the diagnostic: a local can be deleted, while a loop binding cannot —
// the loop still has to iterate — so the answer is to write `_`, which says "no name" and
// is unforgeable by anything the body could refer to.
//
// **The warning had nowhere to point until `for _ in` existed** (08/18), which is why a
// loop counter nobody reads went unremarked while the identical `let` did not. Advice to
// use a spelling the parser rejects would have been worse than silence.
//
// Both bindings are checked, and the two-name form is where it earns its keep:
// `for k, v in xs` reading only `v` is the case `for _, v in xs` was added for. A name
// already starting with `_` is exempt, matching the unused-local rule — `_i` is the older
// spelling of the same intent and still reads as deliberate.
func (c *unusedVarChecker) checkLoopBindings(loop *ast.ForInLoopExpr) {
	refs := referencedNames(loop.Body.Statements)
	for _, b := range []struct {
		name string
		loc  ast.Location
		role string
	}{
		{loop.Key, loop.KeyLocation, "loop variable"},
		{loop.Value, loop.ValueLocation, "loop value"},
	} {
		if b.name == "" || strings.HasPrefix(b.name, "_") {
			continue
		}
		if refs[b.name] {
			continue
		}
		c.warnings = append(c.warnings, diag.Diagnostic{
			Location: b.loc,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeUnusedLoopBinding,
			Message: fmt.Sprintf(
				"%s %q is never read; write `_` to iterate without naming it", b.role, b.name),
			Tags: []diag.Tag{diag.TagUnnecessary},
		})
	}
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
			c.checkLoopBindings(ex)
			c.checkBlock(ex.Body.Statements)
			return false
		case *ast.UnsafeBlockExpr:
			c.checkBlock(ex.Body.Statements)
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
