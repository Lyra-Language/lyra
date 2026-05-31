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
	// Top-level has no outer scope, so outerNames starts empty.
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
		// After processing the statement, its declared names become part of the
		// accumulated set so they are visible to later statements in the same block
		// and to any nested scope created by those later statements.
		for _, name := range directDeclaredNames(stmt) {
			accumulated[name] = true
		}
	}
}

// checkStmt walks a single statement for shadowing.
// outerNames: names visible from ancestor scopes (before this block started).
// accumulated: outerNames ∪ names declared so far in the current block.
func (c *shadowChecker) checkStmt(stmt ast.Statement, outerNames, accumulated map[string]bool) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		// Check the RHS in the current accumulated context first.
		c.checkExpr(s.Value, accumulated)
		// Then check whether this name shadows an outer-scope declaration.
		if outerNames[s.Name] {
			c.warn(s.GetLocation(), s.Name)
		}

	case *ast.DestructuringDeclStmt:
		// Check the RHS first.
		c.checkExpr(s.Value, accumulated)
		// Warn for any pattern-bound name that shadows an outer-scope variable.
		// A destructuring declaration is an explicit binding statement, so it
		// follows the same rule as a plain VarDeclStmt.
		for _, name := range patternBoundNames(s.Pattern) {
			if outerNames[name] {
				c.warn(s.GetLocation(), name)
			}
		}

	case *ast.ExpressionStmt:
		c.checkExpr(s.Expression, accumulated)

	case *ast.VarReassignmentStmt:
		c.checkExpr(s.Value, accumulated)

	case *ast.DerefAssignmentStmt:
		c.checkExpr(s.Target.Operand, accumulated)
		c.checkExpr(s.Value, accumulated)

	case *ast.ReturnStmt:
		c.checkExpr(s.Value, accumulated)

	case *ast.BreakStmt:
		c.checkExpr(s.Value, accumulated)

	case *ast.WithStmt:
		c.checkExpr(s.Arena, accumulated)
		c.checkStatements(s.Body.Statements, accumulated)

	case *ast.IfDestructuringStmt:
		// The RHS value is in the current scope.
		c.checkExpr(s.DestructuringStatement.Value, accumulated)
		// Pattern-bound names enter the then-block without producing warnings.
		thenOuter := copyMap(accumulated)
		for _, name := range patternBoundNames(s.DestructuringStatement.Pattern) {
			thenOuter[name] = true
		}
		c.checkStatements(s.Then.Statements, thenOuter)
		if s.Else != nil {
			// The else block does not bind pattern names.
			c.checkStatements(s.Else.Statements, accumulated)
		}

	case *ast.ElseDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value, accumulated)
		// Pattern-bound names enter the else-block without producing warnings.
		elseOuter := copyMap(accumulated)
		for _, name := range patternBoundNames(s.DestructuringStatement.Pattern) {
			elseOuter[name] = true
		}
		c.checkStatements(s.Else.Statements, elseOuter)

		// TypeDeclStmt, ImportStmt, ContinueStmt, and other leaf statements need
		// no further action.
	}
}

// checkExpr walks an expression for shadowing, propagating outerNames (the set
// of all names visible from scopes enclosing the innermost scope being analyzed)
// into nested constructs.
func (c *shadowChecker) checkExpr(expr ast.Expression, outerNames map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.BlockExpr:
		// A block creates a new scope; outerNames becomes the outer set for it.
		c.checkStatements(e.Statements, outerNames)

	case *ast.IfExpr:
		c.checkExpr(e.Condition, outerNames)
		c.checkExpr(e.Then, outerNames)
		c.checkExpr(e.Else, outerNames)

	case *ast.LambdaExpr:
		// Lambda parameters are pattern-bound — add them to outer without warning.
		bodyOuter := copyMap(outerNames)
		for _, param := range e.Parameters {
			for _, name := range patternBoundNames(param.Pattern) {
				bodyOuter[name] = true
			}
		}
		c.checkExpr(e.Body, bodyOuter)
		// Lambda clauses use their own pattern parameters.
		for _, clause := range e.LambdaClauses {
			clauseOuter := copyMap(outerNames)
			for _, pat := range clause.Patterns {
				for _, name := range patternBoundNames(pat) {
					clauseOuter[name] = true
				}
			}
			c.checkExpr(clause.Body, clauseOuter)
		}

	case *ast.ForLoopExpr:
		// The init variable may shadow an outer name — warn if so.
		forOuter := copyMap(outerNames)
		if e.Init != nil {
			c.checkExpr(e.Init.Value, outerNames)
			if outerNames[e.Init.Name] {
				c.warn(e.Init.GetLocation(), e.Init.Name)
			}
			forOuter[e.Init.Name] = true
		}
		// Condition and post expressions are inside the loop scope.
		if e.Condition != nil {
			c.checkExpr(*e.Condition, forOuter)
		}
		if e.Post != nil {
			c.checkExpr(*e.Post, forOuter)
		}
		c.checkStatements(e.Body.Statements, forOuter)

	case *ast.ForInLoopExpr:
		// The iterable expression is in the current scope.
		c.checkExpr(e.Iterable, outerNames)
		// Key and Value are pattern-bound iteration variables — no warning.
		bodyOuter := copyMap(outerNames)
		if e.Key != "" {
			bodyOuter[e.Key] = true
		}
		if e.Value != "" {
			bodyOuter[e.Value] = true
		}
		c.checkStatements(e.Body.Statements, bodyOuter)

	case *ast.MatchExpr:
		c.checkExpr(e.Scrutinee, outerNames)
		for _, arm := range e.MatchArms {
			// Pattern-bound names enter the arm body without producing warnings.
			armOuter := copyMap(outerNames)
			for _, name := range patternBoundNames(arm.Pattern) {
				armOuter[name] = true
			}
			if arm.Guard != nil {
				c.checkExpr(arm.Guard.Condition, armOuter)
			}
			c.checkExpr(arm.Body, armOuter)
		}

	case *ast.FunctionCallExpr:
		c.checkExpr(e.Function, outerNames)
		for _, arg := range e.Arguments {
			c.checkExpr(arg, outerNames)
		}

	case *ast.MemberExpr:
		c.checkExpr(e.Object, outerNames)

	case *ast.IndexExpr:
		c.checkExpr(e.Object, outerNames)
		c.checkExpr(e.Index, outerNames)

	case *ast.MathBinaryOpExpr:
		c.checkExpr(e.Left, outerNames)
		c.checkExpr(e.Right, outerNames)

	case *ast.MathAssignOpExpr:
		c.checkExpr(e.Right, outerNames)

	case *ast.BooleanBinaryOpExpr:
		c.checkExpr(e.Left, outerNames)
		c.checkExpr(e.Right, outerNames)

	case *ast.NotBooleanExpr:
		c.checkExpr(e.Expression, outerNames)

	case *ast.NegationExpr:
		c.checkExpr(e.Operand, outerNames)

	case *ast.AwaitExpr:
		c.checkExpr(e.Operand, outerNames)

	case *ast.TryExpr:
		c.checkExpr(e.Operand, outerNames)

	case *ast.AddressOfExpr:
		c.checkExpr(e.Operand, outerNames)

	case *ast.DerefExpr:
		c.checkExpr(e.Operand, outerNames)

	case *ast.ArrayLiteralExpr:
		for _, el := range e.Elements {
			c.checkExpr(el, outerNames)
		}

	case *ast.TupleLiteralExpr:
		for _, el := range e.Elements {
			c.checkExpr(el, outerNames)
		}

	case *ast.StructInstanceExpr:
		if e.BaseStruct != nil {
			c.checkExpr(e.BaseStruct, outerNames)
		}
		for _, f := range e.Fields {
			c.checkExpr(f.Value, outerNames)
		}

	case *ast.SpreadExpr:
		// Leaf reference; no nested scopes to recurse into.

	case *ast.ArrayCompExpr:
		for _, gen := range e.Generators {
			c.checkExpr(gen.Value, outerNames)
		}
		for _, g := range e.Guards {
			c.checkExpr(g, outerNames)
		}
		c.checkExpr(e.Result, outerNames)

	case *ast.RangeExpr:
		c.checkExpr(e.Start, outerNames)
		c.checkExpr(e.End, outerNames)
		c.checkExpr(e.Step, outerNames)

	case *ast.NullCoalescingExpr:
		c.checkExpr(e.Optional, outerNames)
		c.checkExpr(e.Default, outerNames)

	case *ast.StringConcatExpr:
		c.checkExpr(e.Left, outerNames)
		c.checkExpr(e.Right, outerNames)

	case *ast.ArrayRepeatExpr:
		c.checkExpr(e.Value, outerNames)
		c.checkExpr(e.Count, outerNames)

	case *ast.ComposeExpr:
		c.checkExpr(e.Left, outerNames)
		c.checkExpr(e.Right, outerNames)

	case *ast.YieldExpr:
		c.checkExpr(e.Value, outerNames)

	case *ast.YieldFromExpr:
		c.checkExpr(e.Generator, outerNames)

	case *ast.UnsafeBlockExpr:
		c.checkStatements(e.Body.Statements, outerNames)

	case *ast.GivenExpr:
		// Binding LHS names are added silently (like destructuring); their RHS
		// values are checked in turn so that nested shadowing inside them is caught.
		bindingOuter := copyMap(outerNames)
		for _, bstmt := range e.Bindings {
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
		c.checkExpr(e.Body, bindingOuter)

	case *ast.DataConstructorExpr:
		c.checkExpr(e.Value, outerNames)

	case *ast.GuardExpr:
		c.checkExpr(e.Condition, outerNames)

		// Leaf nodes (IdentifierExpr, literals, SizeofExpr, etc.) contain no
		// nested scopes and require no action.
	}
}

// copyMap returns a shallow copy of a map[string]bool.
func copyMap(m map[string]bool) map[string]bool {
	result := make(map[string]bool, len(m))
	maps.Copy(result, m)
	return result
}
