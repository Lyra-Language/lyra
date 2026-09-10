package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckContinuationLines flags a statement that looks like the continuation of the line
// above and is not one: `lyra-W023` for a leading `-`, `lyra-W024` for a leading `(` or
// `[`.
//
// # The mistake
//
//	x - x * x2 / 6.0 + x * x2 * x2 / 120.0        add
//	  - x * x2 * x2 * x2 / 5040.0                   (1, 2)
//
// Both are **two statements**. A line beginning with `-`, `(` or `[` is not a
// continuation — the scanner's continuation set is `.`, `|`, `else` and `where`, and those
// three are deliberately excluded because each can begin a statement (`-x`, `(a, b)` and
// `[1, 2]` are all statements in their own right). So the first line's value is computed
// and thrown away and the block evaluates to the second: an expression loses every term
// but its last, and a call loses its callee.
//
// **They compile, run, and are wrong.** The `-` form is the sharper of the two, because
// the same mistake written with a leading `+` is a *type error* — there is no unary plus —
// so the compiler catches that spelling and not this one. It was found in
// `examples/raylib/shapes.lyra`, where it had been shipping since the file was written.
//
// # What it takes to fire
//
// Two things, because the first alone has honest uses:
//
//  1. the statement **begins with** one of the three tokens, and
//  2. the statement *before* it is a **pure value expression whose value is discarded**.
//
// The second is the corroboration. `{ log(); -x }` is a perfectly ordinary body that
// prints and answers a negation, and warning about it would be noise; `{ a + b; -c }` has
// a first statement that computes something and drops it, which no one writes on purpose.
// `discardedValue`'s list is deliberately short and **defaults to silence**: an expression
// kind added later does not start firing this until someone decides it should, which is
// the direction a warning with no suppression syntax has to err in.
//
// # How "begins with" is decided
//
// Two signals, because the AST keeps only one of the three tokens:
//
//   - **By kind.** A negation, a tuple literal and the three array forms carry their own
//     opening token, so their presence at the head of the statement is the signal.
//   - **By column.** Parentheses around a *single* expression are erased — `(x)` collects
//     to a bare `IdentifierExpr` — so nothing about the node says they were there. The
//     **statement's** span still covers them, so a statement starting one column before
//     its own expression began with a `(`. That is what catches `f` then `(arg)`, the
//     commonest spelling of the call form.
//
// Either signal is taken at the **leftmost** expression, since precedence buries the
// opening token: `- x * x` is `((-x) * x)`, whose root is a multiplication, and
// `(x + 1) * 2` likewise. `leftmost` walks that spine once for both checks.
//
// `*` needs nothing: it cannot begin a statement at all, so the mistake is a syntax error.
func CheckContinuationLines(program *ast.Program) []diag.Diagnostic {
	c := &continuationLines{}
	for _, stmt := range program.Statements {
		s, ok := stmt.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(s, func(child ast.Statement) bool { return true }, func(e ast.Expression) bool {
			if b, ok := e.(*ast.BlockExpr); ok {
				c.block(b)
			}
			return true
		})
	}
	return c.diagnostics
}

type continuationLines struct {
	diagnostics []diag.Diagnostic
}

func (c *continuationLines) block(b *ast.BlockExpr) {
	for i := 1; i < len(b.Statements); i++ {
		prev, ok := b.Statements[i-1].(*ast.ExpressionStmt)
		if !ok || !discardedValue(prev.Expression) {
			continue
		}
		cur, ok := b.Statements[i].(*ast.ExpressionStmt)
		if !ok {
			continue
		}
		// Written on one line with an explicit `;` the split is deliberate and spelled
		// out, so there is nothing to warn about.
		if cur.GetLocation().StartLine <= prev.GetLocation().StartLine {
			continue
		}
		head := leftmost(cur.Expression)
		switch {
		case isNegation(head):
			c.warn(head.GetLocation(), diag.CodeLeadingMinusContinuation,
				"this `-` starts a new statement rather than continuing the line above. "+
					"A line beginning with `-` is not a continuation, because `-x` is a "+
					"statement in its own right — so the expression above has its value "+
					"computed and discarded, and this one is what the block evaluates to. "+
					"Put the operator at the end of the previous line instead. (The same "+
					"mistake with `+` is a type error, since there is no unary plus; only "+
					"`-` is silent.)")
		case opensWithBracket(head):
			c.warn(cur.GetLocation(), diag.CodeSplitCallOrIndex,
				"this `[` starts a new statement rather than continuing the line above, so "+
					"the expression above has its value computed and discarded. A line "+
					"beginning with `[` is not a continuation, because `[1, 2]` is an array "+
					"literal and a statement in its own right. If an index was meant, move "+
					"the `[` up to the end of the previous line — a newline *inside* the "+
					"brackets is not a statement terminator, so `xs[` and the index on the "+
					"next line is one expression.")
		case opensWithParen(cur, head):
			c.warn(cur.GetLocation(), diag.CodeSplitCallOrIndex,
				"this `(` starts a new statement rather than continuing the line above, so "+
					"the expression above has its value computed and discarded. A line "+
					"beginning with `(` is not a continuation, because `(a, b)` is a tuple "+
					"and a statement in its own right. If a call was meant, move the `(` up "+
					"to the end of the previous line — a newline *inside* the parentheses "+
					"is not a statement terminator, so `f(` and the arguments on the next "+
					"line is one call.")
		}
	}
}

func (c *continuationLines) warn(loc ast.Location, code, message string) {
	c.diagnostics = append(c.diagnostics, diag.Diagnostic{
		Location: loc,
		Severity: diag.SeverityWarning,
		Code:     code,
		Message:  message,
	})
}

// leftmost walks the left spine to the expression a statement visually begins with.
//
// Precedence buries the opening token: `- x * x / 6.0` parses as `(((-x) * x) / 6.0)`,
// whose root is a division, and `(x + 1) * 2` likewise. Testing the root — the obvious
// first attempt — finds nothing.
func leftmost(e ast.Expression) ast.Expression {
	for {
		switch v := e.(type) {
		case *ast.MathBinaryOpExpr:
			e = v.Left
		case *ast.MemberExpr:
			e = v.Object
		case *ast.IndexExpr:
			e = v.Object
		case *ast.TupleIndexExpr:
			e = v.Object
		case *ast.FunctionCallExpr:
			e = v.Function
		default:
			return e
		}
	}
}

func isNegation(e ast.Expression) bool {
	_, ok := e.(*ast.NegationExpr)
	return ok
}

// opensWithBracket reports whether the head is one of the array forms, each of which
// carries its own `[`.
func opensWithBracket(head ast.Expression) bool {
	switch head.(type) {
	case *ast.ArrayLiteralExpr, *ast.ArrayRepeatExpr, *ast.ArrayCompExpr:
		return true
	}
	return false
}

// opensWithParen reports whether the statement begins with a `(`.
//
// A tuple literal carries its own parentheses and is recognised by kind. A *single*
// parenthesised expression is not: `(x)` collects to a bare `IdentifierExpr` with nothing
// to say the parentheses were there. The **statement's** span still covers them, so
// starting before its own expression on the same line is what betrays it — and that is
// the commonest form of this mistake, a one-argument call split across two lines.
func opensWithParen(stmt *ast.ExpressionStmt, head ast.Expression) bool {
	if _, ok := head.(*ast.TupleLiteralExpr); ok {
		return true
	}
	s, h := stmt.GetLocation(), head.GetLocation()
	return s.StartLine == h.StartLine && s.StartCol < h.StartCol
}

// discardedValue reports whether a statement's expression computes a value and no effect,
// so that using it as a statement achieves nothing at all.
//
// Short, and short on purpose — see the pass header. Anything not named here is assumed to
// have a reason for being a statement, which is why a *call* on the line above does not
// corroborate: a void call is an ordinary statement.
func discardedValue(e ast.Expression) bool {
	switch e.(type) {
	case *ast.MathBinaryOpExpr, *ast.NegationExpr, *ast.IdentifierExpr,
		*ast.IntegerLiteralExpr, *ast.FloatLiteralExpr,
		*ast.MemberExpr, *ast.IndexExpr, *ast.TupleIndexExpr:
		return true
	}
	return false
}
