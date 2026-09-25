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
func CheckUseAfterMove(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable, mt *typetable.MethodTable) []diag.Diagnostic {
	c := &useAfterMove{symTable: symTable, tt: tt, mt: mt, reported: map[reportKey]bool{}}
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
	mt          *typetable.MethodTable // dispatch results, for a method call's borrow modes
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
	// resource marks a consumed `@must_release` value, whose message is a different
	// sentence: the callee *released* a foreign handle, so reading the binding again is a
	// use-after-free rather than a lost uniqueness.
	resource bool
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

// branchOutcome is one branch's contribution to the join: its resulting state, or **nil
// when control cannot leave the branch**.
//
// A branch ending in `return`, `break` or `continue` does not flow into the code after the
// `if` — so a release inside one has not happened on the path that reaches that code, and
// counting it produced a false "used after it was released". `lyrafmt` is written in
// exactly that shape twice:
//
//	if has_error(top) {
//	  tree_delete(tree)
//	  return Err(…)
//	}
//	…
//	tree_delete(tree)       // not a second release: the first one returned
//
// The union was "conservative, matching Rust" and this is the half of Rust's rule it was
// missing. It stayed invisible while only *managed* values could be moved, because nothing
// in this repo releases a string down one arm and uses it after; a `@must_release` handle
// is released down one arm all the time, since that is what an early return is for.
func (c *useAfterMove) branchOutcome(st moveState, branch ast.Expression) moveState {
	if branch == nil {
		return st.clone()
	}
	out := c.expr(st.clone(), branch)
	if b, ok := branch.(*ast.BlockExpr); ok && blockDiverges(b) {
		return nil
	}
	return out
}

// blockDiverges reports whether control **cannot** reach the end of a block: it ends in a
// `return`, a `break` or a `continue`, or in an `if` both of whose branches do.
//
// Deliberately syntactic and deliberately shallow. Answering "no" is always safe — it is
// today's behaviour, a wider join and at worst a spurious report — so every shape this does
// not recognise degrades to what was there before rather than to a missed move.
func blockDiverges(b *ast.BlockExpr) bool {
	if b == nil || len(b.Statements) == 0 {
		return false
	}
	switch last := b.Statements[len(b.Statements)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BreakStmt:
		return true
	case *ast.ContinueStmt:
		return true
	case *ast.ExpressionStmt:
		if inner, ok := last.Expression.(*ast.IfExpr); ok {
			then, thenOK := inner.Then.(*ast.BlockExpr)
			els, elseOK := inner.Else.(*ast.BlockExpr)
			return thenOK && elseOK && blockDiverges(then) && blockDiverges(els)
		}
	}
	return false
}

// mergeMoves is the join of two branch outcomes: moved in *either* branch means
// moved afterwards. A nil outcome is a branch control cannot leave and contributes
// nothing. The conservative direction — it can only add reports, never miss a genuine
// move.
func mergeMoves(a, b moveState) moveState {
	if a == nil {
		a = moveState{}
	}
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
	case *ast.DestructuringDeclStmt:
		// Every name the pattern binds is a *fresh* binding, exactly as VarDeclStmt's
		// single name is, so each stops being moved here.
		//
		// Without this the statement fell to the generic walker, which walks the value and
		// clears nothing — and the clearing is what makes a loop work. `loopBody` seeds the
		// state with every move anywhere in the body, so that a move flags the *next*
		// iteration's read; a declaration inside the body then deletes its own name,
		// because that binding is new each time round. A destructuring never did, so
		// `for pair in ps { let (x, y) = pair; take(x) }` reported x as used after a move
		// that can only have happened to a previous iteration's *different* value. The
		// identical loop written `let x = p` was clean, which is the shape of a missing
		// case rather than a rule.
		st = c.expr(st, v.Value)
		for _, name := range patternBoundNames(v.Pattern) {
			delete(st, name)
		}
		return st
	case *ast.ExpressionStmt:
		return c.expr(st, v.Expression)
	case *ast.ReturnStmt:
		return c.expr(st, v.Value)

	case *ast.IfDestructuringStmt:
		return c.destructuringBranches(st, &v.DestructuringStatement, v.Then, v.Else)
	case *ast.ElseDestructuringStmt:
		return c.letElse(st, &v.DestructuringStatement, v.Else)

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
	ast.WalkStmtChildren(s, func(child ast.Statement) bool {
		st = c.stmt(st, child)
		return false
	}, func(e ast.Expression) bool {
		st = c.expr(st, e)
		return false
	})
	return st
}

// destructuringBranches walks an `if let`: the scrutinee, then each branch as an
// alternative, joined by union — the convention IfExpr follows.
//
// The reason it exists rather than the DestructuringDeclStmt case covering it: these embed
// the declaration **by value** (`DestructuringStatement DestructuringDeclStmt`), so the
// walker never sees a `*ast.DestructuringDeclStmt` and that case never fires. The names the
// pattern binds are fresh in the *matching* branch only, which is also why the delete
// happens on the branch's state rather than on the state flowing past.
//
// **`let … else` is not this shape and must not come back here.** It used to, and the two
// false positives that cost are pinned by tests in use_after_move_destructuring_test.go.
func (c *useAfterMove) destructuringBranches(st moveState, d *ast.DestructuringDeclStmt, then, els *ast.BlockExpr) moveState {
	st = c.expr(st, d.Value)
	bound := patternBoundNames(d.Pattern)
	branch := func(b *ast.BlockExpr, binds bool) moveState {
		s := st.clone()
		if binds {
			for _, name := range bound {
				delete(s, name)
			}
		}
		if b == nil {
			return s
		}
		return c.expr(s, b)
	}
	// The pattern's names are in scope in `then` — the branch that ran because the
	// match succeeded.
	thenState, elseState := branch(then, true), branch(els, false)
	if then != nil && blockDiverges(then) {
		thenState = nil
	}
	if els != nil && blockDiverges(els) {
		elseState = nil
	}
	return mergeMoves(thenState, elseState)
}

// letElse walks `let Some(v) = m else { … }`, which reads like a branch and is not one.
//
// It is Rust's `let … else`: the payload binds in the **enclosing** scope and outlives the
// statement, while the else block is the diverging path that never sees it. The collector
// says so where it builds the node ("Else never sees them, matching let-else semantics"),
// and the compiler agrees — `let Some(v) = opt() else { println("${v}") }` is `undefined
// identifier "v"`, while a use *after* the statement checks clean.
//
// Analyzing it as a branch produced two false positives on correct code, and both are
// regression-tested:
//
//   - a move inside the diverging else escaped it, so `take(s)` after the statement was
//     reported against a move on a path that returns;
//   - the payload names were cleared only inside the else's own state, so the union threw
//     the clear away and a name **rebound** by the let-else kept a stale move record.
//
// Discarding the else's moves is sound because the else always diverges. Note that
// `lyrac check` alone does not enforce that — a non-diverging else passes the front end —
// but the backend refuses it by name, so it holds for every program that can be built.
// (That the enforcement sits a pass too late, with no location on the error, is a separate
// gap; see todo.md.)
//
// The else is still **walked**, against a copy, so a use-after-move *inside* it is reported
// as before. Only its moves are prevented from escaping.
func (c *useAfterMove) letElse(st moveState, d *ast.DestructuringDeclStmt, els *ast.BlockExpr) moveState {
	st = c.expr(st, d.Value)
	if els != nil {
		c.expr(st.clone(), els)
	}
	// The payload binds in the enclosing scope, so each name is a fresh binding owning a
	// value again — exactly what the VarDeclStmt case does with its single name.
	for _, name := range patternBoundNames(d.Pattern) {
		delete(st, name)
	}
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
		// and the join takes the union — of the branches control can **leave**.
		return mergeMoves(c.branchOutcome(st, v.Then), c.branchOutcome(st, v.Else))

	case *ast.MatchExpr:
		st = c.expr(st, v.Scrutinee)
		merged := st.clone()
		for i := range v.MatchArms {
			arm := st.clone()
			if g := v.MatchArms[i].Guard; g != nil {
				arm = c.expr(arm, g.Condition)
			}
			merged = mergeMoves(merged, c.branchOutcome(arm, v.MatchArms[i].Body))
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
	ast.WalkExprChildren(e, func(s ast.Statement) bool {
		st = c.stmt(st, s)
		return false
	}, func(child ast.Expression) bool {
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
	params, recv := c.calleeParams(e)
	// A `.`-call's receiver is parameter 0 and its arguments start at 1. **The offset
	// is the whole difference**, and getting it wrong is silent: every argument would
	// be checked against the mode of the parameter to its left, so an `own` parameter
	// would consume the wrong binding — or none. The purity pass pays the same tax
	// (methodArgumentAt); UFCS avoids it by desugaring the receiver into the argument
	// list before any of this runs.
	offset := 0
	if recv != nil {
		offset = 1
		// `own Self` consumes the receiver, so `h.into_tag()` moves `h`. Recorded
		// after e.Function was walked above, so the receiver's own read counts as a
		// use *before* the move rather than after it.
		if len(params) > 0 && params[0].TypeModifier == types.Own {
			if name, ok := movedName(recv); ok {
				if res, consumed := c.consumedByOwn(recv); consumed {
					st[name] = moveSite{loc: recv.GetLocation(), resource: res}
				}
			}
		}
	}
	for i, arg := range e.Arguments {
		st = c.expr(st, arg)
		p := i + offset
		if p >= len(params) {
			continue // unresolvable callee, or a default/variadic position: assume no move
		}
		if params[p].TypeModifier != types.Own {
			continue // bare/`ref`/`mut` borrow — the caller keeps ownership
		}
		if name, ok := movedName(arg); ok {
			if res, consumed := c.consumedByOwn(arg); consumed {
				st[name] = moveSite{loc: arg.GetLocation(), resource: res}
			}
		}
	}
	return st
}

// calleeParams returns the callee's declared parameters and, for a `.`-call, the
// receiver expression that fills parameter 0.
//
// A **trait method** resolves through the MethodTable rather than by name: dispatch
// already chose the impl, and its signature is where the borrow modes live. Until 08/03
// this path did not exist at all — resolveCallee handles an identifier callee only — so a
// method call recorded no moves whatever its signature said. That was invisible while
// `own` was rejected on trait signatures (lyra-E030); it is exactly the hole that
// restriction was standing in front of.
func (c *useAfterMove) calleeParams(e *ast.FunctionCallExpr) ([]ast.Parameter, ast.Expression) {
	if lam := c.resolveCallee(e); lam != nil {
		return lam.Parameters, nil
	}
	res, ok := c.mt.GetResolution(e)
	if !ok {
		return nil, nil
	}
	lam, err := res.Lambda()
	if err != nil {
		return nil, nil
	}
	// A fully-qualified `Trait::method(x)` passes the receiver *in* the argument list,
	// so it has no separate receiver and no offset; a `.`-call has both.
	if member, isDot := e.Function.(*ast.MemberExpr); isDot {
		return lam.Parameters, member.Object
	}
	return lam.Parameters, nil
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
		// **A block control cannot leave contributes no loop-carried move.** The move
		// happened on a path that returned, so the next iteration never sees it — the
		// same rule the branch join follows, applied to the seed. `lyrafmt` releases its
		// parser inside a `return 2` arm of a loop, which this reported as a use after
		// the previous iteration's release.
		if b, ok := e.(*ast.BlockExpr); ok && blockDiverges(b) {
			return false
		}
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
			if name, ok := movedName(arg); ok {
				if res, consumed := c.consumedByOwn(arg); consumed {
					found[name] = moveSite{loc: arg.GetLocation(), resource: res}
				}
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
	message := fmt.Sprintf(
		"%q is used after it was moved: %s. An `own` parameter takes ownership — the callee releases the value, and may reuse its storage in place — so the binding is consumed. Pass a borrow (`ref`) if the callee only needs to read it, or reassign %q before reading it again",
		id.Name, detail, id.Name)
	if site.resource {
		// A different sentence, because it is a different mistake: what the callee took
		// was a handle to something Lyra does not own, and it freed it. The value is not
		// merely consumed — it is gone, and reading it is a use-after-free that no
		// refcount is going to make safe.
		released := fmt.Sprintf("it was released at %s", site.loc.Pretty())
		if site.viaLoop {
			released = fmt.Sprintf(
				"it is released at %s, so a later iteration of this loop would use it after the release",
				site.loc.Pretty())
		}
		message = fmt.Sprintf(
			"%q is used after it was released: %s. It holds a resource Lyra does not own, and passing it to an `own` parameter hands over the obligation to free it — so the handle is dead afterwards, however ordinary the value looks. Acquire it again if you need another, or release it later",
			id.Name, released)
	}
	c.diagnostics = append(c.diagnostics, diag.Diagnostic{
		Location: id.GetLocation(),
		Severity: diag.SeverityError,
		Code:     diag.CodeUseAfterMove,
		Message:  message,
	})
}

// resolveCallee returns the LambdaExpr for a direct call to a top-level named
// function, or nil when the callee can't be resolved (a method call, a call
// through a local, a type conversion) — in which case no move is recorded, so an
// unrecognized call shape can never produce a false positive. Mirrors the
// ownership pass's resolution, which is what actually decides the retain.
func (c *useAfterMove) resolveCallee(e *ast.FunctionCallExpr) *ast.LambdaExpr {
	// The overload the typechecker picked, for the reason the ownership pass gives: a
	// name shared by several declarations does not resolve to one, and this must see the
	// same function the retain decision is made against.
	if fn, ok := c.tt.Callee(e); ok {
		return fn
	}
	id, ok := e.Function.(*ast.IdentifierExpr)
	if !ok || c.symTable == nil {
		return nil
	}
	// From the calling file, for the reason the ownership pass gives: a private or
	// prelude-shadowing declaration is keyed by module, and this must see the same
	// function the retain decision is made against.
	fn, _ := c.symTable.LookupFunctionFrom(id.Name, e.GetLocation())
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

// consumedByOwn reports whether an `own` parameter *consumes* this argument, and whether
// what it consumed was a foreign resource.
//
// Managed values are the original case: a refcounted box the callee adopts. The second is
// a **`@must_release` type**, and it is the one where the comment above — "a non-managed
// value is copied, so passing it leaves the original intact" — is exactly false. A
// `Parser` is a plain stack struct holding a `^u8`; `own` copies the struct, the callee
// frees the pointer, and the caller is left holding a copy of a handle to freed memory.
// Nothing reported it: `parser_delete(parser)` followed by `parse(parser, …)` checked
// clean, and so did reading a node out of a deleted tree (09/25).
//
// Found from the other side, through `lyra-W022`: a `delete` wrapper that *borrowed* its
// receiver discharged no obligation, and asking why led here — the release itself was not
// a move either.
func (c *useAfterMove) consumedByOwn(e ast.Expression) (resource bool, consumed bool) {
	if c.isManaged(e) {
		return false, true
	}
	if c.tt == nil || c.symTable == nil {
		return false, false
	}
	t, ok := c.tt.Get(e)
	if !ok {
		return false, false
	}
	name, hasHead := types.HeadName(t)
	if !hasHead || name == "" {
		return false, false
	}
	decl, ok := c.symTable.LookupTypeFrom(name, e.GetLocation())
	if !ok || decl == nil || decl.MustRelease == "" {
		return false, false
	}
	return true, true
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
