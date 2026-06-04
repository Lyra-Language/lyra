package main

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// findExprAtPos returns the deepest (most specific) expression in the program
// whose source range contains the given 1-based line/col, or nil if none found.
func findExprAtPos(program *ast.Program, line, col int) ast.Expression {
	if program == nil {
		return nil
	}
	for _, stmt := range program.Statements {
		if e := findExprInStmt(stmt, line, col); e != nil {
			return e
		}
	}
	return nil
}

// findExprInStmt walks a statement (or any AstNode) looking for an expression at line/col.
func findExprInStmt(stmt ast.AstNode, line, col int) ast.Expression {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		return findExprInExpr(s.Value, line, col)
	case *ast.VarReassignmentStmt:
		return findExprInExpr(s.Value, line, col)
	case *ast.ExpressionStmt:
		return findExprInExpr(s.Expression, line, col)
	case *ast.DerefAssignmentStmt:
		return findExprInExpr(s.Value, line, col)
	case *ast.ReturnStmt:
		return findExprInExpr(s.Value, line, col)
	}
	return nil
}

// findExprInExpr returns the deepest expression inside (or equal to) expr
// that contains line/col, or nil.
func findExprInExpr(expr ast.Expression, line, col int) ast.Expression {
	if expr == nil {
		return nil
	}
	if !containsPos(expr.GetLocation(), line, col) {
		return nil
	}
	// Try children first; they are more specific than the parent.
	if child := findInChildren(expr, line, col); child != nil {
		return child
	}
	return expr
}

// findInChildren recursively searches the child expressions of expr.
func findInChildren(expr ast.Expression, line, col int) ast.Expression {
	switch e := expr.(type) {
	case *ast.ArrayLiteralExpr:
		return firstIn(e.Elements, line, col)
	case *ast.TupleLiteralExpr:
		return firstIn(e.Elements, line, col)
	case *ast.InterpolatedStringExpr:
		return firstIn(e.Segments, line, col)
	case *ast.FunctionCallExpr:
		if r := findExprInExpr(e.Function, line, col); r != nil {
			return r
		}
		return firstIn(e.Arguments, line, col)
	case *ast.IndexExpr:
		if r := findExprInExpr(e.Object, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Index, line, col)
	case *ast.MemberExpr:
		return findExprInExpr(e.Object, line, col)
	case *ast.NegationExpr:
		return findExprInExpr(e.Operand, line, col)
	case *ast.NotBooleanExpr:
		return findExprInExpr(e.Expression, line, col)
	case *ast.BooleanBinaryOpExpr:
		if r := findExprInExpr(e.Left, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Right, line, col)
	case *ast.MathBinaryOpExpr:
		if r := findExprInExpr(e.Left, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Right, line, col)
	case *ast.StringConcatExpr:
		if r := findExprInExpr(e.Left, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Right, line, col)
	case *ast.IfExpr:
		if r := findExprInExpr(e.Condition, line, col); r != nil {
			return r
		}
		if r := findExprInExpr(e.Then, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Else, line, col)
	case *ast.MatchExpr:
		if r := findExprInExpr(e.Scrutinee, line, col); r != nil {
			return r
		}
		for _, arm := range e.MatchArms {
			if r := findExprInExpr(arm.Body, line, col); r != nil {
				return r
			}
		}
	case *ast.BlockExpr:
		for _, stmt := range e.Statements {
			if r := findExprInStmt(stmt, line, col); r != nil { //nolint:gosimple
				return r
			}
		}
	case *ast.LambdaExpr:
		return findExprInExpr(e.Body, line, col)
	case *ast.StructInstanceExpr:
		for _, f := range e.Fields {
			if r := findExprInExpr(f.Value, line, col); r != nil {
				return r
			}
		}
	case *ast.AnonymousStructInstanceExpr:
		for _, f := range e.Fields {
			if r := findExprInExpr(f.Value, line, col); r != nil {
				return r
			}
		}
	case *ast.DataConstructorExpr:
		return findExprInExpr(e.Value, line, col)
	case *ast.MathAssignOpExpr:
		return findExprInExpr(e.Right, line, col)
	case *ast.TryExpr:
		return findExprInExpr(e.Operand, line, col)
	}
	return nil
}

// firstIn returns the first expression in exprs that contains line/col.
func firstIn(exprs []ast.Expression, line, col int) ast.Expression {
	for _, e := range exprs {
		if r := findExprInExpr(e, line, col); r != nil {
			return r
		}
	}
	return nil
}

// containsPos reports whether loc (1-based) contains the given 1-based line/col.
func containsPos(loc ast.Location, line, col int) bool {
	if line < loc.StartLine || line > loc.EndLine {
		return false
	}
	if line == loc.StartLine && col < loc.StartCol {
		return false
	}
	if line == loc.EndLine && col > loc.EndCol {
		return false
	}
	return true
}

// hoverContent formats the hover markdown string for an expression and its type.
func hoverContent(expr ast.Expression, typ types.Type) string {
	if ident, ok := expr.(*ast.IdentifierExpr); ok {
		return fmt.Sprintf("```lyra\n%s: %s\n```", ident.Name, typ)
	}
	return fmt.Sprintf("```lyra\n%s\n```", typ)
}
