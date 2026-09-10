package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckLeadingMinusContinuation flags a statement that begins with a unary `-` and looks
// like the continuation of the line above (`lyra-W023`).
//
// # The mistake
//
//	let sin32 = pure (x: f32) -> f32 => {
//	  x - x * x2 / 6.0 + x * x2 * x2 / 120.0
//	    - x * x2 * x2 * x2 / 5040.0
//	}
//
// That is **two statements**, not one expression. A line beginning with `-` is not a
// continuation — the scanner's continuation set is `.`, `|`, `else` and `where`, and `-`
// is deliberately excluded because `-x` is itself a valid statement (see the grammar's
// notes on the statement terminator). So the first line's value is computed and thrown
// away, and the block evaluates to the second: the function returns its last term and
// nothing else.
//
// **It compiles, runs, and is wrong**, which is what earns it a diagnostic. The same
// mistake written with a leading `+` is a type error, because there is no unary plus — so
// the `+` spelling is caught by the compiler and the `-` spelling is not. That asymmetry
// is the whole reason this pass exists; it was found in `examples/raylib/shapes.lyra`,
// where it had been shipping since the file was written.
//
// # What it takes to fire
//
// Two things, because the first alone has honest uses:
//
//  1. the statement's expression **begins with a unary minus** — down the left spine, since
//     `- x * x` parses as `((-x) * x)` and the negation is a leaf rather than the root; and
//  2. the statement *before* it is a **pure value expression whose value is discarded** —
//     arithmetic, a negation, a bare name, a literal.
//
// The second is the corroboration. `{ log(); -x }` is a perfectly ordinary body that
// prints and answers a negation, and warning about it would be noise; `{ a + b; -c }` has
// a first statement that computes something and drops it, which no one writes on purpose.
//
// The list in `discardedValue` is deliberately short and **defaults to silence**: an
// expression kind added later does not start firing this warning until someone decides it
// should. That is the direction a warning with no suppression syntax has to err in.
//
// # Not covered
//
// `(` and `[` are excluded from the continuation set for the same reason and misparse
// differently — `f\n(x)` becomes a *call* rather than two statements, which needs its own
// signature. `*` cannot begin a statement at all, so it is a syntax error and needs
// nothing.
func CheckLeadingMinusContinuation(program *ast.Program) []diag.Diagnostic {
	c := &leadingMinus{}
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

type leadingMinus struct {
	diagnostics []diag.Diagnostic
}

func (c *leadingMinus) block(b *ast.BlockExpr) {
	for i := 1; i < len(b.Statements); i++ {
		prev, ok := b.Statements[i-1].(*ast.ExpressionStmt)
		if !ok || !discardedValue(prev.Expression) {
			continue
		}
		cur, ok := b.Statements[i].(*ast.ExpressionStmt)
		if !ok {
			continue
		}
		minus := leadingNegation(cur.Expression)
		if minus == nil {
			continue
		}
		// Written on one line with an explicit `;` the split is deliberate and spelled
		// out, so there is nothing to warn about.
		if minus.GetLocation().StartLine <= prev.GetLocation().StartLine {
			continue
		}
		c.diagnostics = append(c.diagnostics, diag.Diagnostic{
			Location: minus.GetLocation(),
			Severity: diag.SeverityWarning,
			Code:     diag.CodeLeadingMinusContinuation,
			Message: "this `-` starts a new statement rather than continuing the line above. " +
				"A line beginning with `-` is not a continuation, because `-x` is a statement " +
				"in its own right — so the expression above has its value computed and " +
				"discarded, and this one is what the block evaluates to. Put the operator at " +
				"the end of the previous line instead. (The same mistake with `+` is a type " +
				"error, since there is no unary plus; only `-` is silent.)",
		})
	}
}

// leadingNegation answers the unary minus an expression begins with, or nil.
//
// It walks the **left spine** rather than testing the root, because precedence puts the
// negation at a leaf: `- x * x / 6.0` is `(((-x) * x) / 6.0)`, whose root is a division.
func leadingNegation(e ast.Expression) ast.Expression {
	switch v := e.(type) {
	case *ast.NegationExpr:
		return v
	case *ast.MathBinaryOpExpr:
		return leadingNegation(v.Left)
	}
	return nil
}

// discardedValue reports whether a statement's expression computes a value and no effect,
// so that using it as a statement achieves nothing at all.
//
// Short, and short on purpose — see the pass header. Anything not named here is assumed to
// have a reason for being a statement.
func discardedValue(e ast.Expression) bool {
	switch e.(type) {
	case *ast.MathBinaryOpExpr, *ast.NegationExpr, *ast.IdentifierExpr,
		*ast.IntegerLiteralExpr, *ast.FloatLiteralExpr:
		return true
	}
	return false
}
