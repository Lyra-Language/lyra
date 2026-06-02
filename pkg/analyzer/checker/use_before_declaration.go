package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// UseBeforeDeclarationError reports a variable that was used before its
// declaration within the same lexical scope.
type UseBeforeDeclarationError struct {
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
		// After processing the statement, its declared names become visible.
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

	case *ast.ExpressionStmt:
		c.checkExpr(s.Expression, declared, seen)

	case *ast.VarReassignmentStmt:
		// Reassigning a variable is also a use of it.
		if declared[s.Name] && !seen[s.Name] {
			c.report(s.GetLocation(), s.Name)
		}
		c.checkExpr(s.Value, declared, seen)

	case *ast.DerefAssignmentStmt:
		c.checkExpr(s.Target.Operand, declared, seen)
		c.checkExpr(s.Value, declared, seen)

	case *ast.ReturnStmt:
		c.checkExpr(s.Value, declared, seen)

	case *ast.BreakStmt:
		// break may carry a value expression
		c.checkExpr(s.Value, declared, seen)

	case *ast.WithStmt:
		// The arena expression is in the current scope; the body is a new scope.
		c.checkExpr(s.Arena, declared, seen)
		c.checkStatements(s.Body.Statements)

	case *ast.IfDestructuringStmt:
		// The destructuring value is in the current scope.
		c.checkExpr(s.DestructuringStatement.Value, declared, seen)
		// Then and Else blocks are each a new scope.
		c.checkStatements(s.Then.Statements)
		if s.Else != nil {
			c.checkStatements(s.Else.Statements)
		}

	case *ast.ElseDestructuringStmt:
		c.checkExpr(s.DestructuringStatement.Value, declared, seen)
		c.checkStatements(s.Else.Statements)

		// TypeDeclStmt, ImportStmt, ContinueStmt, and any other statement types
		// that introduce no identifier uses need no further action.
	}
}

// checkExpr walks an expression for use-before-declaration, using the provided
// declared and seen maps from the enclosing scope.
func (c *ubeChecker) checkExpr(expr ast.Expression, declared, seen map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.IdentifierExpr:
		if declared[e.Name] && !seen[e.Name] {
			c.report(e.GetLocation(), e.Name)
		}

	case *ast.BlockExpr:
		// A block creates a new scope.
		c.checkStatements(e.Statements)

	case *ast.IfExpr:
		// Condition is in the current scope; Then/Else are new scopes when they
		// are BlockExprs (handled automatically by checkExpr's BlockExpr case).
		c.checkExpr(e.Condition, declared, seen)
		c.checkExpr(e.Then, declared, seen)
		c.checkExpr(e.Else, declared, seen)

	case *ast.MatchExpr:
		// The matched value is in the current scope.
		c.checkExpr(e.Scrutinee, declared, seen)
		// Each arm's guard and body are in a fresh scope (pattern binds variables
		// that are not VarDeclStmts, so they will not appear in any declared set).
		for _, arm := range e.MatchArms {
			if arm.Guard != nil {
				c.checkExprNewScope(arm.Guard.Condition)
			}
			c.checkExprNewScope(arm.Body)
		}

	case *ast.LambdaExpr:
		// Lambda body is a new scope; parameters are pre-declared there but are
		// not VarDeclStmts, so the fresh declared map will be empty and they
		// will never be flagged.
		c.checkExprNewScope(e.Body)
		for _, clause := range e.LambdaClauses {
			c.checkExprNewScope(clause.Body)
		}

	case *ast.ForLoopExpr:
		// The for-loop body is a new scope.
		c.checkStatements(e.Body.Statements)

	case *ast.ForInLoopExpr:
		// The iterable is in the current scope; the body is a new scope.
		c.checkExpr(e.Iterable, declared, seen)
		c.checkStatements(e.Body.Statements)

	case *ast.FunctionCallExpr:
		c.checkExpr(e.Function, declared, seen)
		for _, arg := range e.Arguments {
			c.checkExpr(arg, declared, seen)
		}

	case *ast.MemberExpr:
		// Only the object is a variable reference; the property is a field name.
		c.checkExpr(e.Object, declared, seen)

	case *ast.IndexExpr:
		c.checkExpr(e.Object, declared, seen)
		c.checkExpr(e.Index, declared, seen)

	case *ast.MathBinaryOpExpr:
		c.checkExpr(e.Left, declared, seen)
		c.checkExpr(e.Right, declared, seen)

	case *ast.MathAssignOpExpr:
		// The left-hand side is both used and assigned (compound assignment).
		if declared[e.Left.Name] && !seen[e.Left.Name] {
			c.report(e.Left.GetLocation(), e.Left.Name)
		}
		c.checkExpr(e.Right, declared, seen)

	case *ast.BooleanBinaryOpExpr:
		c.checkExpr(e.Left, declared, seen)
		c.checkExpr(e.Right, declared, seen)

	case *ast.NotBooleanExpr:
		c.checkExpr(e.Expression, declared, seen)

	case *ast.NegationExpr:
		c.checkExpr(e.Operand, declared, seen)

	case *ast.AwaitExpr:
		c.checkExpr(e.Operand, declared, seen)

	case *ast.TryExpr:
		c.checkExpr(e.Operand, declared, seen)

	case *ast.AddressOfExpr:
		c.checkExpr(e.Operand, declared, seen)

	case *ast.DerefExpr:
		c.checkExpr(e.Operand, declared, seen)

	case *ast.ArrayLiteralExpr:
		for _, el := range e.Elements {
			c.checkExpr(el, declared, seen)
		}

	case *ast.TupleLiteralExpr:
		for _, el := range e.Elements {
			c.checkExpr(el, declared, seen)
		}

	case *ast.StructInstanceExpr:
		// Name is a type name, not a variable reference.
		if e.BaseStruct != nil {
			c.checkExpr(e.BaseStruct, declared, seen)
		}
		for _, f := range e.Fields {
			c.checkExpr(f.Value, declared, seen)
		}

	case *ast.SpreadExpr:
		if declared[e.Name] && !seen[e.Name] {
			c.report(e.GetLocation(), e.Name)
		}

	case *ast.ArrayCompExpr:
		// Generators' Identifier fields are bound variables, not uses.
		for _, gen := range e.Generators {
			c.checkExpr(gen.Value, declared, seen)
		}
		for _, g := range e.Guards {
			c.checkExpr(g, declared, seen)
		}
		c.checkExpr(e.Result, declared, seen)

	case *ast.RangeExpr:
		c.checkExpr(e.Start, declared, seen)
		c.checkExpr(e.End, declared, seen)
		c.checkExpr(e.Step, declared, seen)

	case *ast.NullCoalescingExpr:
		c.checkExpr(e.Optional, declared, seen)
		c.checkExpr(e.Default, declared, seen)

	case *ast.StringConcatExpr:
		c.checkExpr(e.Left, declared, seen)
		c.checkExpr(e.Right, declared, seen)

	case *ast.ArrayRepeatExpr:
		c.checkExpr(e.Value, declared, seen)
		c.checkExpr(e.Count, declared, seen)

	case *ast.ComposeExpr:
		c.checkExpr(e.Left, declared, seen)
		c.checkExpr(e.Right, declared, seen)

	case *ast.YieldExpr:
		c.checkExpr(e.Value, declared, seen)

	case *ast.YieldFromExpr:
		c.checkExpr(e.Generator, declared, seen)

	case *ast.UnsafeBlockExpr:
		c.checkStatements(e.Body.Statements)

	case *ast.GivenExpr:
		// The bindings form a small scope; the body is evaluated in that scope.
		// Check the bindings for internal UBD, then check the body in a fresh
		// scope (binding names are not in declared there, so they won't produce
		// false positives when legitimately referenced in the body).
		c.checkStatements(e.Bindings)
		c.checkExprNewScope(e.Body)

	case *ast.DataConstructorExpr:
		c.checkExpr(e.Value, declared, seen)

	case *ast.GuardExpr:
		c.checkExpr(e.Condition, declared, seen)

		// Leaf nodes (literals, SizeofExpr, etc.) contain no variable references.
	}
}

// checkExprNewScope checks an expression in a completely fresh scope.
// If expr is a BlockExpr its statements are checked with checkStatements (which
// itself builds a fresh declared/seen pair); all other expressions are walked
// with empty maps so that none of the parent scope's declarations are visible.
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
