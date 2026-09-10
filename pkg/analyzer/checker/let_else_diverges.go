package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckLetElseDiverges reports a `let … else` whose else block can fall through
// (`lyra-E074`).
//
// # Why the rule exists
//
//	let Some(v) = opt() else { println("no match") }
//	println("${v}")
//
// The payload binds in the **enclosing** scope — that is the whole point of the form —
// so the statement after it reads `v`. The else block runs only when the pattern did
// *not* match, in which case `v` was never bound; if that block can fall through, control
// reaches the read with nothing behind the name. Rust requires the else to diverge for
// exactly this reason.
//
// # Why it moved here
//
// The rule was already enforced, but only by the **backend** — `lowerElseDestructuring`
// refuses an else block whose lowered form has no terminator. That is a pass too late and
// two ways worse: the error arrives at `lyrac build` rather than `lyrac check`, and it
// carries **no location**, so it names the mistake without saying where it is.
//
// It also had two silent consumers. `CheckUseAfterMove.letElse` and
// `CheckMustRelease.letElse` both discard the else block's effects on the grounds that
// nothing after the statement is reachable from it — sound only because the else always
// diverges. An invariant two analyses depend on should be stated by the front end, not
// left to a check that runs after they do. (The backend's own comment claimed "the
// typechecker requires it to diverge", which was not true of any pass until this one.)
//
// **The backend check stays**, per the rule that it errors rather than emitting wrong
// code even where the front end has already looked.
//
// # What counts as diverging
//
// `return`, `break`, `continue`, and any expression the typechecker gave the type
// `never` — which is `panic(…)`. Compound forms count when every path through them does:
// a nested block, an `if` with both branches diverging, a `match` whose every arm does.
// A statement list diverges if **any** statement in it does, since nothing after a
// diverging statement runs.
//
// # Two known gaps, both in the direction of accepting
//
//   - **An infinite loop is not recognised.** `else { for true { } }` diverges and is
//     reported here — deciding otherwise means proving no `break` is reachable, which is
//     a real analysis rather than a shape test. The backend refuses it too, so the two
//     agree and nothing that used to build stops.
//   - **`if` with both branches diverging is accepted here and refused by the backend.**
//     That one is the backend's gap (its `if` lowering leaves the merge block without a
//     terminator), and the direction is deliberate: a front-end error means "this is not
//     a legal program", and that program is legal. See todo.md.
func CheckLetElseDiverges(program *ast.Program, tt *typetable.TypeTable) []diag.Diagnostic {
	c := &letElseDiverges{tt: tt}
	for _, stmt := range program.Statements {
		if s, ok := stmt.(ast.Statement); ok {
			ast.WalkStmt(s, func(child ast.Statement) bool {
				c.check(child)
				return true
			}, nil)
		}
	}
	return c.diagnostics
}

type letElseDiverges struct {
	tt          *typetable.TypeTable
	diagnostics []diag.Diagnostic
}

func (c *letElseDiverges) check(s ast.Statement) {
	v, ok := s.(*ast.ElseDestructuringStmt)
	if !ok || v.Else == nil || c.blockDiverges(v.Else) {
		return
	}
	c.diagnostics = append(c.diagnostics, diag.Diagnostic{
		Location: v.Else.GetLocation(),
		Severity: diag.SeverityError,
		Code:     diag.CodeLetElseMustDiverge,
		Message: "the `else` branch of a `let … else` must not fall through: it runs when the " +
			"pattern did not match, and the names the pattern binds belong to the enclosing " +
			"scope — so falling through reaches code that reads a name nothing bound. End it " +
			"with `return`, `break`, `continue` or `panic(…)`. To run this block *without* " +
			"leaving, the form you want is `if let … { … } else { … }`, whose bindings are " +
			"scoped to its own branch",
	})
}

// blockDiverges reports whether control can leave this block by falling off its end. A
// list diverges if **any** statement does — a `return` in the middle makes the rest
// unreachable, so the block still cannot fall through.
func (c *letElseDiverges) blockDiverges(b *ast.BlockExpr) bool {
	if b == nil {
		return false
	}
	for _, s := range b.Statements {
		if c.stmtDiverges(s) {
			return true
		}
	}
	return false
}

func (c *letElseDiverges) stmtDiverges(s ast.Statement) bool {
	switch v := s.(type) {
	case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.ExpressionStmt:
		return c.exprDiverges(v.Expression)
	}
	return false
}

func (c *letElseDiverges) exprDiverges(e ast.Expression) bool {
	if e == nil {
		return false
	}
	// `panic(…)` is the whole of this case today: `never` has no syntax, so nothing
	// else can carry the type. Asking the TypeTable rather than matching the name is
	// what keeps a program's own `panic` from being mistaken for the builtin.
	if t, ok := c.tt.Get(e); ok {
		if _, isNever := t.(types.NeverType); isNever {
			return true
		}
	}
	switch v := e.(type) {
	case *ast.BlockExpr:
		return c.blockDiverges(v)
	case *ast.UnsafeBlockExpr:
		return c.blockDiverges(v.Body)
	case *ast.IfExpr:
		// Both branches must exist and both must diverge. A missing `else` falls
		// through by definition.
		return v.Else != nil && c.exprDiverges(v.Then) && c.exprDiverges(v.Else)
	case *ast.MatchExpr:
		// Every arm, and at least one. An arm's body is a block by the time it gets
		// here — the collector erases a bare `None => break` into the equivalent
		// single-statement block — so no arm needs a case of its own.
		if len(v.MatchArms) == 0 {
			return false
		}
		for i := range v.MatchArms {
			if !c.exprDiverges(v.MatchArms[i].Body) {
				return false
			}
		}
		return true
	}
	return false
}
