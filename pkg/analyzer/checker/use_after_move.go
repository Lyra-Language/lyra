package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckUseAfterMove flags reading a binding after its value was moved into an
// `own` parameter (`lyra-E019`) — part (a) of the borrow model (todo #8), and the
// definite-move analysis of todo #6.
//
// # What a move is
//
// Exactly one thing today: passing a *bare identifier* naming a **managed** value
// (a string or a `shared` value — ownership.IsManaged) as the argument of an `own`
// parameter. `own` is Perceus's owned calling convention: the callee adopts the
// reference and releases it, and under reuse/FBIP it may overwrite the box in
// place. Everything else borrows — a bare/`ref`/`mut` parameter leaves the caller
// owning the value — so nothing else consumes a binding.
//
// It is deliberately *not* a move to pass a non-managed value (an `own i64` or an
// `own` stack struct is copied by value, so the original stays valid), nor to pass
// a field (`p.name`) — a partial move is its own design question, so a field
// expression is left alone rather than guessed at.
//
// # Why it's an error even though the code runs correctly today
//
// This is not (yet) a memory-safety fix: the ownership pass retains a managed
// value flowing into an `own` argument whenever it isn't the binding's last use, so
// a use-after-move is currently *safe* — it just silently costs a refcount bump.
// What it breaks is the meaning of `own`, and it has a real performance
// consequence: the defensive retain leaves the box at rc = 2, so `lyra_rc_drop_reuse`
// reports it shared, reuse/FBIP silently stops firing, and a rebuild that should
// have been zero-allocation starts allocating every cell. That is exactly the
// "uniqueness perf cliff" Perceus warns about, and it is invisible without this
// diagnostic. Making it an error is also what lets the retain be dropped later, so
// `own` becomes a real move.
//
// # The analysis
//
// Flow-sensitive over each function body, with a moved-set per binding:
//
//   - an `if`/`match` analyzes each branch from the state at the join point and
//     takes the **union** — moved in either branch means moved afterwards
//     (conservative, matching Rust);
//   - a loop body is seeded with every move found anywhere inside it, so a move on
//     one iteration is visible to the reads of the next;
//   - a `let`/`var` declaration or a reassignment of the name re-initializes it and
//     clears the move.
//
// Uncertainty resolves toward *not* reporting: an unresolvable callee (a method
// call, a call through a local, a function value) records no move at all, so a new
// hard error can't fire on code the analysis doesn't actually understand.
func CheckUseAfterMove(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable) []diag.Diagnostic {
	c := &useAfterMove{symTable: symTable, tt: tt, reported: map[reportKey]bool{}}
	state := moveState{}
	for _, stmt := range program.Statements {
		if vds, ok := stmt.(*ast.VarDeclStmt); ok {
			if lam, ok := vds.Value.(*ast.LambdaExpr); ok {
				c.lambda(lam)
				continue
			}
		}
		if s, ok := stmt.(ast.Statement); ok {
			state = c.stmt(state, s)
		}
	}
	return c.diagnostics
}

type useAfterMove struct {
	symTable    *symbols.SymbolTable
	tt          *typetable.TypeTable
	diagnostics []diag.Diagnostic
	// reported dedupes by (binding, move site) so one mistake yields one error.
	// It matters most for a loop-carried move, where the seeded state makes *every*
	// read in the body — including the argument of the move itself — a use after the
	// previous iteration's move; all of them share one move site and one fix. A
	// binding moved at a genuinely different site keys differently and still reports.
	reported map[reportKey]bool
}

type reportKey struct {
	name string
	move ast.Location
}

// moveSite records where a binding's value was moved away, and whether that move
// reaches the current point *around a loop back-edge* (so the message can say the
// read happens on a later iteration rather than plainly after the move).
type moveSite struct {
	loc     ast.Location
	viaLoop bool
}

// moveState maps a binding name to the move that consumed it. Absent = still owned.
type moveState map[string]moveSite

func (s moveState) clone() moveState {
	out := make(moveState, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// mergeMoves is the join of two branch outcomes: moved in *either* branch means
// moved afterwards. The conservative direction — it can only add reports, never
// miss a genuine move.
func mergeMoves(a, b moveState) moveState {
	out := a.clone()
	for k, v := range b {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

// lambda analyzes one function/lambda body from a fresh state. A nested lambda is
// analyzed on its own rather than threaded through the enclosing state: whether a
// closure body runs once, later, or never is exactly the question captures raise,
// and guessing would risk a false positive.
func (c *useAfterMove) lambda(lam *ast.LambdaExpr) {
	if lam.Body != nil {
		c.expr(moveState{}, lam.Body)
	}
	for i := range lam.LambdaClauses {
		if b := lam.LambdaClauses[i].Body; b != nil {
			c.expr(moveState{}, b)
		}
	}
}

func (c *useAfterMove) stmt(st moveState, s ast.Statement) moveState {
	switch v := s.(type) {
	case nil:
		return st
	case *ast.VarDeclStmt:
		st = c.expr(st, v.Value)
		delete(st, v.Name) // a fresh binding of the name owns a value again
		return st
	case *ast.VarReassignmentStmt:
		st = c.expr(st, v.Value)
		delete(st, v.Name) // reassignment gives the name a new value
		return st
	case *ast.ExpressionStmt:
		return c.expr(st, v.Expression)
	case *ast.ReturnStmt:
		return c.expr(st, v.Value)

	case *ast.TraitImplStmt:
		// Each method is its own function, so each gets a fresh state. Letting the
		// generic walker handle this would thread one state through every method body
		// in the impl, and a move in one method would then flag a read in the next.
		for i := range v.Methods {
			c.expr(moveState{}, v.Methods[i].Clause.Body)
		}
		return st
	case *ast.TraitDeclStmt:
		for i := range v.Methods {
			if d := v.Methods[i].DefaultMethod; d != nil {
				c.expr(moveState{}, d.Body)
			}
		}
		return st
	}
	// Anything else: visit its children in order, routing each back through this
	// walker so nesting keeps its flow-sensitivity. Returning false prunes the
	// generic walker's own recursion, since we do it ourselves.
	ast.WalkStmt(s, func(child ast.Statement) bool {
		if child == s {
			return true
		}
		st = c.stmt(st, child)
		return false
	}, func(e ast.Expression) bool {
		st = c.expr(st, e)
		return false
	})
	return st
}

func (c *useAfterMove) expr(st moveState, e ast.Expression) moveState {
	switch v := e.(type) {
	case nil:
		return st

	case *ast.IdentifierExpr:
		c.reportIfMoved(st, v)
		return st

	case *ast.LambdaExpr:
		c.lambda(v) // analyzed independently; the outer state doesn't flow in
		return st

	case *ast.FunctionCallExpr:
		return c.call(st, v)

	case *ast.BlockExpr:
		for _, s := range v.Statements {
			st = c.stmt(st, s)
		}
		return st

	case *ast.IfExpr:
		st = c.expr(st, v.Condition)
		// Branches are alternatives: each starts from the state at the branch point,
		// and the join takes the union.
		return mergeMoves(c.expr(st.clone(), v.Then), c.expr(st.clone(), v.Else))

	case *ast.MatchExpr:
		st = c.expr(st, v.Scrutinee)
		merged := st.clone()
		for i := range v.MatchArms {
			arm := st.clone()
			if g := v.MatchArms[i].Guard; g != nil {
				arm = c.expr(arm, g.Condition)
			}
			merged = mergeMoves(merged, c.expr(arm, v.MatchArms[i].Body))
		}
		return merged

	case *ast.ForLoopExpr:
		if v.Init != nil {
			st = c.stmt(st, v.Init)
		}
		if v.Condition != nil {
			st = c.expr(st, *v.Condition)
		}
		st = c.loopBody(st, v.Body)
		if v.Post != nil {
			st = c.expr(st, *v.Post)
		}
		return st

	case *ast.ForInLoopExpr:
		st = c.expr(st, v.Iterable)
		return c.loopBody(st, v.Body)
	}

	// Straight-line expression: no joins, so just visit the children in order,
	// routing each back through expr (which re-establishes flow-sensitivity if a
	// child turns out to be a nested block/if/match/loop).
	ast.WalkExpr(e, func(s ast.Statement) bool {
		st = c.stmt(st, s)
		return false
	}, func(child ast.Expression) bool {
		if child == e {
			return true
		}
		st = c.expr(st, child)
		return false
	})
	return st
}

// call walks a call's arguments and records the moves it performs. Each argument
// is walked for *uses* before the move is recorded, so `consume(p)` reports only if
// p was already moved — and `consume(p, p)` correctly flags the second p.
func (c *useAfterMove) call(st moveState, e *ast.FunctionCallExpr) moveState {
	st = c.expr(st, e.Function)
	lam := c.resolveCallee(e)
	for i, arg := range e.Arguments {
		st = c.expr(st, arg)
		if lam == nil || i >= len(lam.Parameters) {
			continue // unresolvable callee, or a default/variadic position: assume no move
		}
		if lam.Parameters[i].TypeModifier != types.Own {
			continue // bare/`ref`/`mut` borrow — the caller keeps ownership
		}
		if name, ok := movedName(arg); ok && c.isManaged(arg) {
			st[name] = moveSite{loc: arg.GetLocation()}
		}
	}
	return st
}

// loopBody analyzes a loop body with every move inside it already in effect, so a
// read reached by the back-edge sees the move from the previous iteration. Moves
// found this way are marked viaLoop for the message; a binding *declared* inside
// the body clears itself at its declaration before its own move, so seeding never
// misreports a per-iteration local.
func (c *useAfterMove) loopBody(st moveState, body *ast.BlockExpr) moveState {
	seeded := st.clone()
	for name, site := range c.movesIn(body) {
		if _, already := seeded[name]; !already {
			seeded[name] = moveSite{loc: site.loc, viaLoop: true}
		}
	}
	for _, s := range body.Statements {
		seeded = c.stmt(seeded, s)
	}
	return seeded
}

// movesIn collects every move performed anywhere in a subtree, ignoring control
// flow — the "does this loop consume anything?" question, which has no ordering.
// It stops at a nested lambda, whose body is a separate function.
func (c *useAfterMove) movesIn(body *ast.BlockExpr) moveState {
	found := moveState{}
	var onExpr func(ast.Expression) bool
	onExpr = func(e ast.Expression) bool {
		if _, isLambda := e.(*ast.LambdaExpr); isLambda {
			return false
		}
		call, ok := e.(*ast.FunctionCallExpr)
		if !ok {
			return true
		}
		lam := c.resolveCallee(call)
		if lam == nil {
			return true
		}
		for i, arg := range call.Arguments {
			if i >= len(lam.Parameters) || lam.Parameters[i].TypeModifier != types.Own {
				continue
			}
			if name, ok := movedName(arg); ok && c.isManaged(arg) {
				found[name] = moveSite{loc: arg.GetLocation()}
			}
		}
		return true
	}
	for _, s := range body.Statements {
		ast.WalkStmt(s, nil, onExpr)
	}
	return found
}

func (c *useAfterMove) reportIfMoved(st moveState, id *ast.IdentifierExpr) {
	site, moved := st[id.Name]
	if !moved {
		return
	}
	key := reportKey{name: id.Name, move: site.loc}
	if c.reported[key] {
		return
	}
	c.reported[key] = true
	detail := fmt.Sprintf("it was moved into an `own` parameter at %s", site.loc.Pretty())
	if site.viaLoop {
		detail = fmt.Sprintf(
			"it is moved into an `own` parameter at %s, so a later iteration of this loop would read it after the move",
			site.loc.Pretty())
	}
	c.diagnostics = append(c.diagnostics, diag.Diagnostic{
		Location: id.GetLocation(),
		Severity: diag.SeverityError,
		Code:     diag.CodeUseAfterMove,
		Message: fmt.Sprintf(
			"%q is used after it was moved: %s. An `own` parameter takes ownership — the callee releases the value, and may reuse its storage in place — so the binding is consumed. Pass a borrow (`ref`) if the callee only needs to read it, or reassign %q before reading it again",
			id.Name, detail, id.Name),
	})
}

// resolveCallee returns the LambdaExpr for a direct call to a top-level named
// function, or nil when the callee can't be resolved (a method call, a call
// through a local, a type conversion) — in which case no move is recorded, so an
// unrecognized call shape can never produce a false positive. Mirrors the
// ownership pass's resolution, which is what actually decides the retain.
func (c *useAfterMove) resolveCallee(e *ast.FunctionCallExpr) *ast.LambdaExpr {
	id, ok := e.Function.(*ast.IdentifierExpr)
	if !ok || c.symTable == nil {
		return nil
	}
	fn, _ := c.symTable.LookupFunction(id.Name)
	return fn
}

// isManaged reports whether the argument's recorded type is reference-counted —
// the only kind of value an `own` parameter actually consumes. A non-managed value
// is copied, so passing it leaves the original intact.
func (c *useAfterMove) isManaged(e ast.Expression) bool {
	if c.tt == nil {
		return false
	}
	t, ok := c.tt.Get(e)
	return ok && ownership.IsManaged(t)
}

// movedName returns the binding name an argument expression moves, and whether it
// moves one at all. Only a bare identifier does: a field or index expression would
// be a *partial* move, which needs its own model, so it is not treated as
// consuming the whole binding.
func movedName(arg ast.Expression) (string, bool) {
	id, ok := arg.(*ast.IdentifierExpr)
	if !ok {
		return "", false
	}
	return id.Name, true
}
