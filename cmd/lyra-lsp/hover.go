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
		// The **target** as well as the value: `p^ = v` mentions `p`, and looking only
		// at the right-hand side makes the pointer being written through the one name
		// in the statement an editor cannot resolve.
		if e := findExprInExpr(&s.Target, line, col); e != nil {
			return e
		}
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
	case *ast.UnsafeBlockExpr:
		// **Without this, every navigation feature is blind inside `unsafe { … }`** —
		// hover, go-to-definition, rename, document highlight, all of which start from
		// findExprAtPos and descend through this switch. That is the whole of a program's FFI and raw-pointer code,
		// since both require the block; go-to-definition on a call to an `extern` is
		// *only* reachable from inside one.
		//
		// Missing since `unsafe` blocks landed on 08/18, and not noticed because the
		// symptom is an editor doing nothing rather than doing something wrong.
		return findExprInExpr(e.Body, line, col)
	case *ast.AddressOfExpr:
		return findExprInExpr(e.Operand, line, col)
	case *ast.DerefExpr:
		return findExprInExpr(e.Operand, line, col)

	// The rest mirror `ast.walkExprChildren`, which is the canonical answer to "what are
	// this node's children" — and which had all of these while this switch had none of
	// them, so hover, go-to-definition and rename returned nothing inside a `for` body,
	// a comprehension, a range, `a ?? b`, `t.0` and the rest. `exhaustive_test.go` in
	// pkg/ast now fails when the two drift.
	case *ast.ForLoopExpr:
		if e.Init != nil {
			if r := findExprInExpr(e.Init.Value, line, col); r != nil {
				return r
			}
		}
		if e.Condition != nil {
			if r := findExprInExpr(*e.Condition, line, col); r != nil {
				return r
			}
		}
		if e.Post != nil {
			if r := findExprInExpr(*e.Post, line, col); r != nil {
				return r
			}
		}
		return findExprInStmts(e.Body.Statements, line, col)
	case *ast.ForInLoopExpr:
		if r := findExprInExpr(e.Iterable, line, col); r != nil {
			return r
		}
		return findExprInStmts(e.Body.Statements, line, col)
	case *ast.RangeExpr:
		if r := findExprInExpr(e.Start, line, col); r != nil {
			return r
		}
		if r := findExprInExpr(e.End, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Step, line, col)
	case *ast.ArrayCompExpr:
		for _, gen := range e.Generators {
			if r := findExprInExpr(gen.Value, line, col); r != nil {
				return r
			}
		}
		if r := firstIn(e.Guards, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Result, line, col)
	case *ast.ArrayRepeatExpr:
		if r := findExprInExpr(e.Value, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Count, line, col)
	case *ast.NullCoalescingExpr:
		if r := findExprInExpr(e.Optional, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Default, line, col)
	case *ast.ComposeExpr:
		if r := findExprInExpr(e.Left, line, col); r != nil {
			return r
		}
		return findExprInExpr(e.Right, line, col)
	case *ast.TupleIndexExpr:
		return findExprInExpr(e.Object, line, col)
	case *ast.BitwiseNotExpr:
		return findExprInExpr(e.Operand, line, col)
	case *ast.GuardExpr:
		return findExprInExpr(e.Condition, line, col)
	case *ast.AwaitExpr:
		return findExprInExpr(e.Operand, line, col)
	case *ast.YieldExpr:
		return findExprInExpr(e.Value, line, col)
	case *ast.YieldFromExpr:
		return findExprInExpr(e.Generator, line, col)
	}
	return nil
}

// findExprInStmts is `BlockExpr`'s loop, shared with the two loop forms — whose bodies are
// BlockExprs the switch above reaches through a field rather than as a case, so they
// cannot simply recurse.
func findExprInStmts(stmts []ast.Statement, line, col int) ast.Expression {
	for _, stmt := range stmts {
		if r := findExprInStmt(stmt, line, col); r != nil {
			return r
		}
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
