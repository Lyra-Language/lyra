package checker

import (
	"fmt"
	"math"
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckIntegerRanges is a flow-sensitive value-range (interval) analysis over
// each function body. For every integer variable it tracks the interval [lo, hi]
// of values it can hold at each program point, and reports four things the
// literal-only checks can't:
//
//   - **lyra-E020 (error): a definite integer overflow.** An `+`/`-`/`*`/unary-`-`
//     whose operand ranges prove the result can't fit its type on *any* path
//     (`if x > 100 { x + 100 }` on an i8 — a guaranteed runtime trap). Only a
//     *definite* overflow is reported; a merely *possible* one (`a + b` on two
//     full-range i8s) is left to the runtime trap, so a correct program is never
//     flagged.
//   - **lyra-E021 (error): a definite divide-by-zero.** A `/`/`%`/`%%` whose
//     *identifier* divisor is proven `[0,0]` on a reachable path (`let b = 0; a / b`,
//     or `if b == 0 { a / b }`) — a guaranteed runtime trap. A literal / folded-
//     constant zero (`5 / 0`) stays the typechecker's constant-fold check, so this
//     adds only the non-constant, flow-proven case (the twin of the divide-by-zero
//     trap elision). Same definite-only bias as E020.
//   - **lyra-E022 (error): a definite out-of-bounds index.** An `xs[i]` whose index
//     range is *non-singleton* and entirely outside `[-size, size)` (`if i >= size
//     { xs[i] }`) — a guaranteed runtime bounds trap. A single constant index stays
//     the typechecker's own range check; the non-singleton requirement means that
//     check didn't fire (it resolves only a single constant), so no double report.
//     The twin of the bounds-trap elision; same definite-only bias.
//   - **lyra-W011 (warning): a constant comparison.** An integer comparison whose
//     ranges prove it always yields the same result (`x < 0` on a `u8`, or a
//     comparison made trivial by a branch refinement) — dead code or a likely bug.
//
// # Soundness bias
//
// The bar is zero false positives: every diagnostic must be provably correct.
// Whenever a value can't be tracked precisely the analysis *widens* to the whole
// type range (⊤), which can only miss a diagnostic, never invent one. Concretely:
//
//   - A variable absent from the environment is ⊤ (its type's full range).
//   - Interval arithmetic that would overflow int64 falls back to ⊤ (so the
//     narrow types i8..u32 — where the math fits int64 — are precise, while i64
//     arithmetic is mostly ⊤). u64 is tracked with a +∞ upper sentinel (its true
//     max 2^64-1 doesn't fit int64): the exact lower bound of 0 is load-bearing
//     (`x < 0` is always false, a refined `x >= size` index is a definite OOB),
//     while the +∞ upper only ever causes conservative untracking — see intBounds
//     and compareConst's sentinel guards.
//   - A branch refines a variable only against a comparison with a *pure* constant
//     side (literal / negated literal / another tracked variable); anything else
//     leaves the branch un-refined.
//   - A C-style `for` loop is analyzed with a **widening/narrowing fixpoint**
//     (evalForLoop), so its counter is tracked precisely inside the body
//     (`for var i: u8 = 0; i < 3; i += 1` → the body sees i ∈ [0,2]); an
//     accumulator with no bounding guard still widens to ⊤. The *after*-loop state
//     havocs the loop-assigned variables (sound under `break`). A `for … in` loop
//     (no counter/guard to narrow) still havocs its variables.
//   - A `match` on an integer *variable* refines the scrutinee per arm to the
//     values its pattern matches (a literal or numeric range), the analogue of
//     branch refinement; an arm whose pattern can't overlap the scrutinee's range
//     is unreachable and skipped. Arm values merge by union.
//
// The pass runs after typechecking (it needs the TypeTable for each expression's
// width and signedness).
func CheckIntegerRanges(program *ast.Program, tt *typetable.TypeTable) ([]diag.Diagnostic, *SafetyTable) {
	c := &rangeChecker{tt: tt, safe: newSafetyTable()}
	for _, stmt := range program.Statements {
		c.topLevel(stmt)
	}
	return c.diagnostics, c.safe
}

// SafetyTable records the integer operations the range analysis proved cannot hit
// a particular runtime trap on any path, so the backend can **elide** that trap
// (emit the plain instruction / a bare load). It is keyed by the AST expression
// node, which is the same object the backend lowers (both passes walk the one
// *ast.Program), so the lookup is a pointer match. Membership is conservative in
// every dimension: only a *proven*-safe op is present; anything uncertain is
// absent and keeps its runtime trap, so a wrong entry — the only thing that could
// turn a real fault into a silent miscompile — never occurs. A nil table (no
// analysis ran) reports false for everything, so the trap stays.
//
// The tracked facts:
//
//   - noOverflow — `+`/`-`/`*` (and `+=`-style) whose operand ranges keep the
//     result within the type (drops the `with.overflow`+trap; see checkArith).
//   - noDivZero — `/`/`%`/`%%` (and their compound forms) whose divisor is provably
//     ≠ 0 (drops the divide-by-zero check).
//   - noDivOverflow — signed division that can't be INT_MIN/-1, plus every unsigned
//     division (drops the signed-division overflow check).
//   - inBounds — `xs[i]` whose index is provably within [0, size) (drops the array
//     bounds check and the negative-index adjustment).
type SafetyTable struct {
	noOverflow    map[ast.Expression]bool
	noDivZero     map[ast.Expression]bool
	noDivOverflow map[ast.Expression]bool
	inBounds      map[ast.Expression]bool
}

func newSafetyTable() *SafetyTable {
	return &SafetyTable{
		noOverflow:    map[ast.Expression]bool{},
		noDivZero:     map[ast.Expression]bool{},
		noDivOverflow: map[ast.Expression]bool{},
		inBounds:      map[ast.Expression]bool{},
	}
}

// NoOverflow reports whether e is a proven-non-overflowing `+`/`-`/`*`. A nil table
// (no analysis ran) is safe-by-absence: reports false, so the trap stays. Every
// accessor below follows the same nil-safe contract.
func (t *SafetyTable) NoOverflow(e ast.Expression) bool { return t.has(t.noOverflow, e) }

// NoDivZero reports whether e is a division-family op with a provably-nonzero divisor.
func (t *SafetyTable) NoDivZero(e ast.Expression) bool { return t.has(t.noDivZero, e) }

// NoDivOverflow reports whether e is a division-family op that can't overflow (never
// signed INT_MIN/-1, or any unsigned division).
func (t *SafetyTable) NoDivOverflow(e ast.Expression) bool { return t.has(t.noDivOverflow, e) }

// IndexInBounds reports whether e is an index whose value is provably within
// [0, size) — so both the bounds trap and the negative-index adjustment drop.
func (t *SafetyTable) IndexInBounds(e ast.Expression) bool { return t.has(t.inBounds, e) }

func (t *SafetyTable) has(m map[ast.Expression]bool, e ast.Expression) bool {
	if t == nil || m == nil {
		return false
	}
	return m[e]
}

type rangeChecker struct {
	tt          *typetable.TypeTable
	diagnostics []diag.Diagnostic
	safe        *SafetyTable
	// silent suppresses diagnostics and safe-marking during a loop's widening /
	// narrowing fixpoint (the body is analyzed many times to find its invariant,
	// then once loudly with that invariant); side-effect-free otherwise-identical.
	silent bool
}

// topLevel analyzes each function body from a fresh environment. Every function
// is its own scope, so — like the other flow-sensitive passes — trait-impl and
// trait-default method bodies are analyzed independently rather than threaded
// through one shared state.
func (c *rangeChecker) topLevel(stmt ast.AstNode) {
	switch v := stmt.(type) {
	case *ast.VarDeclStmt:
		if lam, ok := v.Value.(*ast.LambdaExpr); ok {
			c.analyzeLambda(lam)
		}
	case *ast.TraitImplStmt:
		for i := range v.Methods {
			if b := v.Methods[i].Clause.Body; b != nil {
				c.eval(newEnv(), b)
			}
		}
	case *ast.TraitDeclStmt:
		for i := range v.Methods {
			if d := v.Methods[i].DefaultMethod; d != nil && d.Body != nil {
				c.eval(newEnv(), d.Body)
			}
		}
	}
}

func (c *rangeChecker) analyzeLambda(lam *ast.LambdaExpr) {
	if lam.Body != nil {
		c.eval(newEnv(), lam.Body)
	}
	for i := range lam.LambdaClauses {
		if b := lam.LambdaClauses[i].Body; b != nil {
			c.eval(newEnv(), b)
		}
	}
}

// ── the abstract domain ──────────────────────────────────────────────────────

// interval is an inclusive [lo, hi] range of int64. lo > hi means the empty set
// (an unsatisfiable refinement → an unreachable branch).
type interval struct{ lo, hi int64 }

func (iv interval) empty() bool           { return iv.lo > iv.hi }
func (iv interval) single() (int64, bool) { return iv.lo, iv.lo == iv.hi }

// rangeEnv is the abstract state at a program point: each *tracked* integer
// variable's interval, plus a reachability flag. A variable that is absent is ⊤
// (its type's full range); an unreachable env (a contradictory refinement)
// suppresses all diagnostics.
type rangeEnv struct {
	vars      map[string]interval
	reachable bool
}

func newEnv() rangeEnv { return rangeEnv{vars: map[string]interval{}, reachable: true} }

func (e rangeEnv) clone() rangeEnv {
	out := rangeEnv{vars: make(map[string]interval, len(e.vars)), reachable: e.reachable}
	for k, v := range e.vars {
		out.vars[k] = v
	}
	return out
}

// mergeEnv is the join of two branch outcomes (an if/match). An unreachable side
// contributes nothing. A variable is kept only when tracked on *both* sides,
// unioned — tracked on one side but ⊤ on the other joins to ⊤ (absent).
func mergeEnv(a, b rangeEnv) rangeEnv {
	if !a.reachable {
		return b
	}
	if !b.reachable {
		return a
	}
	out := rangeEnv{vars: map[string]interval{}, reachable: true}
	for name, av := range a.vars {
		if bv, ok := b.vars[name]; ok {
			out.vars[name] = interval{minI(av.lo, bv.lo), maxI(av.hi, bv.hi)}
		}
	}
	return out
}

// ── statement evaluation (threads the environment) ───────────────────────────

func (c *rangeChecker) evalStmt(st rangeEnv, s ast.Statement) rangeEnv {
	if !st.reachable {
		return st
	}
	switch v := s.(type) {
	case nil:
		return st
	case *ast.VarDeclStmt:
		iv, tracked, after := c.eval(st, v.Value)
		st = after
		if tracked {
			c.checkConstraintViolation(v.Value, iv)
			// Clamp the initializer's interval to the declared type — a well-typed
			// value can't exceed its type's range.
			if lo, hi, ok := c.intBoundsOf(v.Value); ok {
				iv = interval{maxI(iv.lo, lo), minI(iv.hi, hi)}
			}
			st.vars[v.Name] = iv
		} else {
			delete(st.vars, v.Name) // untracked initializer → ⊤
		}
		return st
	case *ast.VarReassignmentStmt:
		iv, tracked, after := c.eval(st, v.Value)
		st = after
		if tracked {
			st.vars[v.Name] = iv
		} else {
			delete(st.vars, v.Name)
		}
		return st
	case *ast.ExpressionStmt:
		_, _, after := c.eval(st, v.Expression)
		return after
	case *ast.ReturnStmt:
		_, _, after := c.eval(st, v.Value)
		return after
	}
	// Any other statement: visit children in order, routing each back through this
	// walker so nesting keeps its flow-sensitivity (pruning the generic recursion).
	ast.WalkStmt(s, func(child ast.Statement) bool {
		if child == s {
			return true
		}
		st = c.evalStmt(st, child)
		return false
	}, func(e ast.Expression) bool {
		_, _, st = c.eval(st, e)
		return false
	})
	return st
}

// ── expression evaluation ────────────────────────────────────────────────────
//
// eval returns the value interval of e, whether it is tracked (an integer whose
// range is known — false for a non-integer, u64, or an untrackable source), and
// the environment *after* evaluating e (control-flow expressions thread and merge
// it; a plain expression returns it unchanged). Diagnostics are emitted here.
func (c *rangeChecker) eval(st rangeEnv, e ast.Expression) (interval, bool, rangeEnv) {
	if !st.reachable {
		return interval{}, false, st
	}
	switch v := e.(type) {
	case nil:
		return interval{}, false, st

	case *ast.IntegerLiteralExpr:
		if v.Unsigned {
			return interval{}, false, st // large-u64 bit pattern, not tracked
		}
		return interval{v.Value, v.Value}, true, st

	case *ast.IdentifierExpr:
		if iv, ok := st.vars[v.Name]; ok {
			return iv, true, st
		}
		return c.typeIntervalIn(v, st)

	case *ast.NegationExpr:
		inner, tracked, after := c.eval(st, v.Operand)
		st = after
		if !tracked {
			return c.typeIntervalIn(e, st)
		}
		r, ok := negI(inner)
		if !ok {
			return c.typeIntervalIn(e, st)
		}
		res, resOK := c.checkArith(e, e, r)
		return res, resOK, st

	case *ast.MathBinaryOpExpr:
		lv, lt, st1 := c.eval(st, v.Left)
		rv, rt, st2 := c.eval(st1, v.Right)
		st = st2
		switch v.Operator {
		case ast.MathBinaryOpDiv, ast.MathBinaryOpMod, ast.MathBinaryOpRemainder:
			// Division / remainder can't grow magnitude, so there's no E020 to report
			// (its precise result range isn't tracked — it widens to the type range).
			// Instead report a definite divide-by-zero (E021) and prove the two runtime
			// traps LLVM's div carries (divisor≠0, signed INT_MIN/-1) for elision.
			c.checkDivision(e, e, v.Right, lv, lt, rv, rt)
			return c.typeIntervalIn(e, st)
		}
		if !lt || !rt {
			return c.typeIntervalIn(e, st)
		}
		var r interval
		var ok bool
		switch v.Operator {
		case ast.MathBinaryOpAdd:
			r, ok = addI(lv, rv)
		case ast.MathBinaryOpSub:
			r, ok = subI(lv, rv)
		case ast.MathBinaryOpMul:
			r, ok = mulI(lv, rv)
		}
		if !ok {
			return c.typeIntervalIn(e, st)
		}
		res, resOK := c.checkArith(e, e, r)
		return res, resOK, st

	case *ast.MathAssignOpExpr:
		// `x OP= rhs` is `x = x OP rhs`: check the operation for overflow and update
		// x's tracked interval. (In a loop x is already havoc'd, so this can't fire.)
		lv, lt, _ := c.eval(st, &v.Left)
		rv, rt, after := c.eval(st, v.Right)
		st = after
		switch v.Operator {
		case ast.MathAssignOpDiv, ast.MathAssignOpMod, ast.MathAssignOpRemainder:
			// The compound-assign result is typed void and the target identifier isn't
			// recorded, so v.Right (which carries the target's propagated width) supplies
			// the operation's integer type and is the divisor. The new value of x isn't
			// tracked → ⊤.
			c.checkDivision(e, v.Right, v.Right, lv, lt, rv, rt)
			delete(st.vars, v.Left.Name)
			return interval{}, false, st
		}
		if lt && rt {
			var r interval
			var ok bool
			switch v.Operator {
			case ast.MathAssignOpAdd:
				r, ok = addI(lv, rv)
			case ast.MathAssignOpSub:
				r, ok = subI(lv, rv)
			case ast.MathAssignOpMul:
				r, ok = mulI(lv, rv)
			}
			if ok {
				// The compound-assign expression itself is typed void, and its target
				// identifier isn't recorded; the RHS carries the target's (propagated)
				// width, so use it for the overflow bounds.
				if res, resOK := c.checkArith(e, v.Right, r); resOK {
					st.vars[v.Left.Name] = res
					return interval{}, false, st
				}
			}
		}
		delete(st.vars, v.Left.Name) // couldn't track the new value → ⊤
		return interval{}, false, st

	case *ast.BooleanBinaryOpExpr:
		lv, lt, st1 := c.eval(st, v.Left)
		rv, rt, st2 := c.eval(st1, v.Right)
		st = st2
		if isComparison(v.Operator) && lt && rt && involvesVariable(v.Left, v.Right) {
			if result, constant := compareConst(v.Operator, lv, rv); constant {
				c.report(e, diag.SeverityWarning, diag.CodeConstantComparison,
					fmt.Sprintf("comparison is always %t (the operands' value ranges never overlap the other outcome)", result))
			}
		}
		return interval{}, false, st // a comparison / && / || yields a bool

	case *ast.NotBooleanExpr:
		_, _, after := c.eval(st, v.Expression)
		return interval{}, false, after

	case *ast.IfExpr:
		return c.evalIf(st, v)

	case *ast.BlockExpr:
		return c.evalBlock(st, v)

	case *ast.MatchExpr:
		return c.evalMatch(st, v)

	case *ast.ForLoopExpr:
		return c.evalForLoop(st, v)

	case *ast.ForInLoopExpr:
		return interval{}, false, c.evalForIn(st, v)

	case *ast.IndexExpr:
		return c.evalIndex(st, v)

	case *ast.LambdaExpr:
		c.analyzeLambda(v) // nested lambda: its own scope, outer state doesn't flow in
		return interval{}, false, st
	}

	// Any other expression (call, member, index, string, …): walk its children for
	// their diagnostics, threading the env, and report the value as untracked.
	ast.WalkExpr(e, func(child ast.Statement) bool {
		st = c.evalStmt(st, child)
		return false
	}, func(child ast.Expression) bool {
		if child == e {
			return true
		}
		_, _, st = c.eval(st, child)
		return false
	})
	return c.typeIntervalIn(e, st)
}

func (c *rangeChecker) evalIf(st rangeEnv, v *ast.IfExpr) (interval, bool, rangeEnv) {
	_, _, st1 := c.eval(st, v.Condition) // emits comparison/overflow diagnostics once
	thenEnv, elseEnv := c.refine(st1, v.Condition)

	tv, tt, tAfter := c.evalBranch(thenEnv, v.Then)
	ev, et, eAfter := c.evalBranch(elseEnv, v.Else)

	merged := mergeEnv(tAfter, eAfter)
	if v.Else == nil {
		// A one-armed `if` is a statement (no value) — the merge already unions the
		// then-outcome with the fall-through (elseEnv), so just report untracked.
		return interval{}, false, merged
	}
	if tt && et {
		return interval{minI(tv.lo, ev.lo), maxI(tv.hi, ev.hi)}, true, merged
	}
	return interval{}, false, merged
}

// evalBranch evaluates one branch body; a nil body (a missing else) contributes
// its entry env unchanged. An unreachable entry env is threaded through so the
// merge drops it.
func (c *rangeChecker) evalBranch(st rangeEnv, body ast.Expression) (interval, bool, rangeEnv) {
	if body == nil {
		return interval{}, false, st
	}
	return c.eval(st, body)
}

func (c *rangeChecker) evalBlock(st rangeEnv, v *ast.BlockExpr) (interval, bool, rangeEnv) {
	saved := st.clone()
	declared := map[string]bool{}
	inner := st
	var lastVal interval
	var lastTracked bool
	for _, s := range v.Statements {
		if vds, ok := s.(*ast.VarDeclStmt); ok {
			declared[vds.Name] = true
		}
		if es, ok := s.(*ast.ExpressionStmt); ok {
			lastVal, lastTracked, inner = c.eval(inner, es.Expression)
		} else {
			lastVal, lastTracked = interval{}, false
			inner = c.evalStmt(inner, s)
		}
	}
	// Restore block scoping: block-local declarations (incl. ones shadowing an
	// outer name) don't leak out; reassignments to pre-existing outer variables do.
	out := saved
	out.reachable = inner.reachable
	for name, iv := range inner.vars {
		if !declared[name] {
			out.vars[name] = iv
		}
	}
	return lastVal, lastTracked, out
}

func (c *rangeChecker) evalMatch(st rangeEnv, v *ast.MatchExpr) (interval, bool, rangeEnv) {
	_, _, st = c.eval(st, v.Scrutinee)
	// When the scrutinee is a tracked integer *variable*, each arm refines it to the
	// values its pattern matches — a literal (`0 => …`, refine to [0,0]) or a numeric
	// range (`1..=10 => …`). So a definite overflow / OOB / divide-by-zero inside an
	// arm whose pattern constrains the scrutinee is caught (`match x { 100..=127 =>
	// x + 100 }` on an i8 overflows), and an in-range arm elides its checks — the
	// analogue of branch refinement for `match`. A non-identifier / non-integer
	// scrutinee, or a non-numeric pattern (a wildcard/identifier catch-all, a
	// data/tuple/struct pattern), refines nothing (that arm sees the scrutinee's full
	// range). A pattern that can't overlap the scrutinee's range makes the arm
	// unreachable and it's skipped (contributing no value, env, or diagnostics).
	scrutID, _ := v.Scrutinee.(*ast.IdentifierExpr)
	result := rangeEnv{reachable: false}
	var val interval
	allTracked := true
	first := true
	for i := range v.MatchArms {
		arm := v.MatchArms[i]
		armEnv := st.clone()
		if scrutID != nil {
			if lo, hi, ok := patternInterval(arm.Pattern); ok {
				c.refineScrutinee(&armEnv, scrutID, lo, hi)
			}
		}
		if !armEnv.reachable {
			continue // the arm's pattern can't match the scrutinee's value range
		}
		if arm.Guard != nil {
			_, _, armEnv = c.eval(armEnv, arm.Guard.Condition)
		}
		av, at, aAfter := c.eval(armEnv, arm.Body)
		result = mergeEnv(result, aAfter)
		if !at {
			allTracked = false
		} else if first {
			val, first = av, false
		} else {
			val = interval{minI(val.lo, av.lo), maxI(val.hi, av.hi)}
		}
	}
	if first || !allTracked {
		return interval{}, false, mergeEnv(result, st)
	}
	return val, true, mergeEnv(result, st)
}

// refineScrutinee narrows the match scrutinee id to the intersection of its current
// range and [lo, hi] (the values an arm's pattern matches) within that arm's env.
// An empty intersection means the pattern can't match → the arm is unreachable.
func (c *rangeChecker) refineScrutinee(env *rangeEnv, id *ast.IdentifierExpr, lo, hi int64) {
	cur, ok := c.curInterval(*env, id)
	if !ok {
		return // scrutinee isn't a tracked integer — nothing to refine
	}
	n := interval{maxI(cur.lo, lo), minI(cur.hi, hi)}
	if n.empty() {
		env.reachable = false
		return
	}
	env.vars[id.Name] = n
}

// patternInterval returns the inclusive [lo, hi] integer interval a match pattern
// matches, for the numeric literal / range patterns (`0`, `1..=10`, `0..<3`). Any
// other pattern — a wildcard/identifier catch-all, a rune/string literal, a
// data/tuple/struct pattern, or a bound that isn't a compile-time integer — returns
// ok=false (no refinement). Mirrors the typechecker's exhaustiveness reader so the
// two agree on what a pattern covers; guards are irrelevant here (the pattern still
// constrains the scrutinee whether or not a guard also holds).
func patternInterval(p ast.Pattern) (lo, hi int64, ok bool) {
	switch pat := p.(type) {
	case *ast.LiteralPattern:
		s, isStr := pat.Value.(string) // a rune pattern stores a RunePatternValue
		if !isStr {
			return 0, 0, false
		}
		n, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return 0, 0, false // out of int64 range or non-integer → don't refine
		}
		return n, n, true
	case *ast.RangePattern:
		start, sok := patternBound(pat.Start)
		end, eok := patternBound(pat.End)
		if !sok || !eok {
			return 0, 0, false
		}
		if pat.EndOperator == "<" { // exclusive end (..<)
			if end == math.MinInt64 {
				return 0, 0, false
			}
			end--
		}
		return start, end, true
	}
	return 0, 0, false
}

// patternBound extracts a compile-time int64 from a range-pattern bound (an integer
// literal, or a negated one for a negative bound like -128).
func patternBound(e ast.Expression) (int64, bool) {
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		return v.Value, true
	case *ast.NegationExpr:
		if inner, ok := v.Operand.(*ast.IntegerLiteralExpr); ok {
			return -inner.Value, true
		}
	}
	return 0, false
}

// maxFixpointIters bounds the widening and narrowing phases. Widening guarantees
// convergence in a handful of steps (each unstable bound jumps to ±∞ once), and
// narrowing only replaces ∞ bounds with finite ones (monotone), so the cap is a
// safety net, never the normal exit.
const maxFixpointIters = 32

// evalForLoop analyzes a C-style `for` with a **widening/narrowing fixpoint**, so
// a loop counter is tracked precisely (`for var i: u8 = 0; i < 3; i += 1` → the
// body sees i ∈ [0,2]) instead of havoc'd. The body is analyzed *silently* to find
// the loop-head invariant H — a sound over-approximation of every state the loop
// head can be in — then **once loudly** with H, so diagnostics fire once and
// elision keys off the precise ranges. Because H over-approximates, both the
// overflow error and elision stay sound (a wider interval only makes them fire
// less often, never wrongly).
//
// The after-loop state havocs the loop-assigned variables, which is sound in the
// presence of `break` (a mid-body break carries a state H — the loop *head*
// invariant — need not cover); the precision that matters is inside the body.
func (c *rangeChecker) evalForLoop(st rangeEnv, v *ast.ForLoopExpr) (interval, bool, rangeEnv) {
	// The init runs once, loudly (so `var i: i8 = <overflow>` is still caught).
	if v.Init != nil {
		st = c.evalStmt(st, v.Init)
	}
	assigned := assignedNames(&v.Body)
	if v.Init != nil {
		assigned[v.Init.Name] = true
	}
	if v.Post != nil {
		for n := range assignedNames(*v.Post) {
			assigned[n] = true
		}
	}

	// Compute the loop-head invariant H silently: widen to a fixpoint (fast), then
	// narrow to recover the bounds the loop guard implies.
	saved := c.silent
	c.silent = true
	H := st.clone()
	for i := range maxFixpointIters { // widening
		joined := mergeEnv(H, c.stepLoopBody(H, v))
		next := joined
		if i > 0 {
			next = widenEnv(H, joined)
		}
		if envEqual(next, H) {
			break
		}
		H = next
	}
	for range maxFixpointIters { // narrowing
		next := narrowEnv(H, mergeEnv(st, c.stepLoopBody(H, v)))
		if envEqual(next, H) {
			break
		}
		H = next
	}
	c.silent = saved

	// One loud pass over the body with the invariant: emits diagnostics + marks
	// provably-safe ops with the precise counter ranges.
	c.stepLoopBody(H, v)

	return interval{}, false, havoc(st, assigned)
}

// stepLoopBody runs one iteration's worth of the loop from a loop-head state:
// evaluate the condition, refine to the guard-true branch, then the body and the
// post step. Returns the state at the *next* loop head (before its guard).
func (c *rangeChecker) stepLoopBody(H rangeEnv, v *ast.ForLoopExpr) rangeEnv {
	entry := H.clone()
	if v.Condition != nil {
		_, _, entry = c.eval(entry, *v.Condition)
		entry, _ = c.refine(entry, *v.Condition) // guard held to enter the body
	}
	_, _, exit := c.eval(entry, &v.Body)
	if v.Post != nil {
		_, _, exit = c.eval(exit, *v.Post)
	}
	return exit
}

// widenInterval accelerates convergence: a bound that grew (lower decreasing /
// upper increasing) jumps to ±∞ (int64 min/max), so an unbounded counter reaches
// a fixpoint in one step instead of iterating N times. A stable bound is kept.
func widenInterval(old, next interval) interval {
	lo, hi := old.lo, old.hi
	if next.lo < old.lo {
		lo = math.MinInt64
	}
	if next.hi > old.hi {
		hi = math.MaxInt64
	}
	return interval{lo, hi}
}

// narrowInterval tightens a widened bound back down: an ∞ bound is replaced by the
// recomputed finite value (which the loop guard now implies), a finite bound is
// kept. It only ever replaces ∞→finite, so it's monotone and terminates.
func narrowInterval(old, next interval) interval {
	lo, hi := old.lo, old.hi
	if old.lo == math.MinInt64 {
		lo = next.lo
	}
	if old.hi == math.MaxInt64 {
		hi = next.hi
	}
	return interval{lo, hi}
}

func widenEnv(old, joined rangeEnv) rangeEnv {
	out := rangeEnv{vars: make(map[string]interval, len(joined.vars)), reachable: joined.reachable}
	for name, jv := range joined.vars {
		if ov, ok := old.vars[name]; ok {
			out.vars[name] = widenInterval(ov, jv)
		} else {
			out.vars[name] = jv
		}
	}
	return out
}

func narrowEnv(old, recomputed rangeEnv) rangeEnv {
	out := rangeEnv{vars: make(map[string]interval, len(old.vars)), reachable: recomputed.reachable}
	for name, ov := range old.vars {
		if rv, ok := recomputed.vars[name]; ok {
			out.vars[name] = narrowInterval(ov, rv)
		} else {
			out.vars[name] = ov
		}
	}
	return out
}

func envEqual(a, b rangeEnv) bool {
	if a.reachable != b.reachable || len(a.vars) != len(b.vars) {
		return false
	}
	for name, av := range a.vars {
		if bv, ok := b.vars[name]; !ok || av != bv {
			return false
		}
	}
	return true
}

func (c *rangeChecker) evalForIn(st rangeEnv, v *ast.ForInLoopExpr) rangeEnv {
	_, _, st = c.eval(st, v.Iterable)
	assigned := assignedNames(&v.Body)
	if v.Key != "" {
		assigned[v.Key] = true
	}
	if v.Value != "" {
		assigned[v.Value] = true
	}
	// A numeric-range iterable bounds the loop variable precisely (`for i in 0..<n`
	// → i ∈ [start, end)), the for-in analogue of the C-style loop's counter — so the
	// body sees a tracked counter (elision + diagnostics) instead of ⊤. Other
	// iterables (an array/string, whose element/index we don't track here) keep the
	// variable havoc'd.
	body := havoc(st.clone(), assigned)
	if kv, ok := c.forInRangeKey(st, v); ok {
		body.vars[v.Key] = kv
	}
	_, _, _ = c.eval(body, &v.Body)
	return havoc(st, assigned)
}

// forInRangeKey returns the interval the loop's single variable takes over a
// numeric-range iterable, when the range is **provably non-empty** — so the loop
// definitely runs, and a body diagnostic keyed off the variable is genuinely
// definite (it holds at the first iteration, `i = start`) rather than a maybe-empty
// false positive. Only a single-variable, step-less range binds here; a two-variable
// form, a stepped range (direction/values unclear), a range with a non-foldable
// bound, or a maybe-empty one leaves the variable havoc'd (sound).
func (c *rangeChecker) forInRangeKey(st rangeEnv, v *ast.ForInLoopExpr) (interval, bool) {
	r, ok := v.Iterable.(*ast.RangeExpr)
	if !ok || v.Value != "" || r.Step != nil {
		return interval{}, false
	}
	start, sok := c.pureInterval(st, r.Start)
	end, eok := c.pureInterval(st, r.End)
	if !sok || !eok {
		return interval{}, false
	}
	hi := end.hi
	var nonEmpty bool
	if r.EndOperator == "<" { // exclusive: i ∈ [start, end); non-empty iff start < end
		if hi != posInf {
			hi = dec(hi)
		}
		nonEmpty = start.hi < end.lo
	} else { // inclusive: i ∈ [start, end]; non-empty iff start <= end
		nonEmpty = start.hi <= end.lo
	}
	kv := interval{start.lo, hi}
	if !nonEmpty || kv.empty() {
		return interval{}, false
	}
	return kv, true
}

// ── branch refinement (pure — emits no diagnostics) ──────────────────────────

// refine returns the environments for the true and false branches of cond,
// narrowing variables where a comparison lets it. It reads intervals purely (via
// pureInterval), since the condition's diagnostics were already emitted by eval.
func (c *rangeChecker) refine(st rangeEnv, cond ast.Expression) (rangeEnv, rangeEnv) {
	switch v := cond.(type) {
	case *ast.BooleanBinaryOpExpr:
		switch v.Operator {
		case ast.BooleanBinaryOpAnd:
			// then = both refinements; else stays conservative (De Morgan would need
			// a disjunction the interval domain can't represent).
			t1, _ := c.refine(st, v.Left)
			t2, _ := c.refine(t1, v.Right)
			return t2, st.clone()
		case ast.BooleanBinaryOpOr:
			_, e1 := c.refine(st, v.Left)
			_, e2 := c.refine(e1, v.Right)
			return st.clone(), e2
		default:
			if isComparison(v.Operator) {
				return c.refineComparison(st, v)
			}
		}
	case *ast.NotBooleanExpr:
		t, e := c.refine(st, v.Expression)
		return e, t
	}
	return st.clone(), st.clone()
}

func (c *rangeChecker) refineComparison(st rangeEnv, v *ast.BooleanBinaryOpExpr) (rangeEnv, rangeEnv) {
	thenEnv, elseEnv := st.clone(), st.clone()
	if id, ok := v.Left.(*ast.IdentifierExpr); ok {
		if k, kok := c.pureInterval(st, v.Right); kok {
			c.applyRefine(&thenEnv, id, v.Operator, k)
			c.applyRefine(&elseEnv, id, negateOp(v.Operator), k)
			return thenEnv, elseEnv
		}
	}
	if id, ok := v.Right.(*ast.IdentifierExpr); ok {
		if k, kok := c.pureInterval(st, v.Left); kok {
			flipped := flipOp(v.Operator) // `k OP x` ≡ `x flip(OP) k`
			c.applyRefine(&thenEnv, id, flipped, k)
			c.applyRefine(&elseEnv, id, negateOp(flipped), k)
		}
	}
	return thenEnv, elseEnv
}

// applyRefine narrows id's interval in env for the comparison `id op k` holding.
// An unsatisfiable narrowing (empty interval) marks the branch unreachable.
func (c *rangeChecker) applyRefine(env *rangeEnv, id *ast.IdentifierExpr, op ast.BooleanBinaryOp, k interval) {
	cur, ok := c.curInterval(*env, id)
	if !ok {
		return // id isn't a tracked integer — nothing to narrow
	}
	n := narrow(cur, op, k)
	if n.empty() {
		env.reachable = false
		return
	}
	env.vars[id.Name] = n
}

// narrow returns cur restricted to the values satisfying `x op k`.
func narrow(cur interval, op ast.BooleanBinaryOp, k interval) interval {
	switch op {
	case ast.BooleanBinaryOpLT: // x < k  →  x <= k.hi - 1
		return interval{cur.lo, minI(cur.hi, dec(k.hi))}
	case ast.BooleanBinaryOpLTE:
		return interval{cur.lo, minI(cur.hi, k.hi)}
	case ast.BooleanBinaryOpGT: // x > k  →  x >= k.lo + 1
		return interval{maxI(cur.lo, inc(k.lo)), cur.hi}
	case ast.BooleanBinaryOpGTE:
		return interval{maxI(cur.lo, k.lo), cur.hi}
	case ast.BooleanBinaryOpEq:
		return interval{maxI(cur.lo, k.lo), minI(cur.hi, k.hi)}
	case ast.BooleanBinaryOpNEq:
		// Only narrows when k pins a single value at a boundary of cur.
		if p, single := k.single(); single {
			if cur.lo == p {
				return interval{inc(p), cur.hi}
			}
			if cur.hi == p {
				return interval{cur.lo, dec(p)}
			}
		}
		return cur
	}
	return cur
}

// pureInterval computes the interval of a *simple constant-ish* expression with
// no side effects — a literal, a negated literal, or a tracked variable. Anything
// more (arithmetic, a call) returns not-tracked, so refinement stays conservative.
func (c *rangeChecker) pureInterval(st rangeEnv, e ast.Expression) (interval, bool) {
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		if v.Unsigned {
			return interval{}, false
		}
		return interval{v.Value, v.Value}, true
	case *ast.NegationExpr:
		if inner, ok := c.pureInterval(st, v.Operand); ok {
			if r, rok := negI(inner); rok {
				return r, true
			}
		}
		return interval{}, false
	case *ast.IdentifierExpr:
		if iv, ok := st.vars[v.Name]; ok {
			return iv, true
		}
		if lo, hi, ok := c.intBoundsOf(v); ok {
			return interval{lo, hi}, true
		}
	}
	return interval{}, false
}

func (c *rangeChecker) curInterval(env rangeEnv, id *ast.IdentifierExpr) (interval, bool) {
	if iv, ok := env.vars[id.Name]; ok {
		return iv, true
	}
	if lo, hi, ok := c.intBoundsOf(id); ok {
		return interval{lo, hi}, true
	}
	return interval{}, false
}

// ── overflow check ───────────────────────────────────────────────────────────

// checkArith takes the mathematical result interval of an integer op and its
// result expression e. If that interval lies entirely outside e's type range the
// op always overflows → error, and the result widens to ⊤. Otherwise it clamps to
// the type range: an execution that didn't trap left the value in range.
func (c *rangeChecker) checkArith(reportAt, typeExpr ast.Expression, m interval) (interval, bool) {
	tmin, tmax, ok := c.intBoundsOf(typeExpr)
	if !ok {
		return interval{}, false // result type isn't a tracked integer
	}
	if m.lo > tmax || m.hi < tmin { // the whole result range is outside the type
		c.report(reportAt, diag.SeverityError, diag.CodeIntegerOverflow,
			fmt.Sprintf("this operation always overflows %s: its result is always in [%s, %s], outside the valid range [%s, %s]",
				c.typeName(typeExpr), fmtBound(m.lo), fmtBound(m.hi), fmtBound(tmin), fmtBound(tmax)))
		return interval{tmin, tmax}, true
	}
	if m.lo >= tmin && m.hi <= tmax {
		// The whole (over-approximated) result range fits the type, so the op can't
		// overflow on any path — record it so the backend can drop the trap.
		if !c.silent {
			c.safe.noOverflow[reportAt] = true
		}
		return m, true
	}
	// A possible overflow: keep the runtime trap. Clamp downstream to the type range
	// (a non-trapping execution stays in range).
	return interval{maxI(m.lo, tmin), minI(m.hi, tmax)}, true
}

// ── range-constraint enforcement (lyra-E023, flow-sensitive) ─────────────────

// checkConstraintViolation reports E023 when valueExpr's tracked interval is
// entirely outside the range constraint of the range-constrained newtype it is
// assigned to — the flow-sensitive twin of the typechecker's constant-value
// RangeConstraint check (`typechecker/range_constraint.go`). The target's
// constraint comes from the type recorded for valueExpr: an annotated `let p:
// Percent = x` has the typechecker stamp `Percent` onto `x` (`checkVarDecl`), which
// is the site this runs from. (A plain reassignment isn't stamped, so its
// non-constant case isn't covered here — only its constant case, by the typechecker.)
//
// Scoped to an *identifier* value: a literal / folded constant is the typechecker's
// own check (which folds exactly those), and an identifier is what it can't fold,
// so restricting to a variable both avoids a double report and captures this pass's
// value-add — a variable refined by flow (`if x > 100 { let p: Percent = x }`) or
// bound to a constant (`let y = 150; let p: Percent = y`). Definite-only, like every
// other range-analysis diagnostic: a value that merely *might* be out of range is
// left to the runtime.
func (c *rangeChecker) checkConstraintViolation(valueExpr ast.Expression, iv interval) {
	if c.silent {
		return
	}
	if _, isID := valueExpr.(*ast.IdentifierExpr); !isID {
		return
	}
	t, ok := c.tt.Get(valueExpr)
	if !ok {
		return
	}
	ct, ok := t.(*types.ConstrainedType)
	if !ok {
		return
	}
	for _, con := range ct.Constraints {
		rc, ok := con.(*types.RangeConstraint)
		if !ok {
			continue
		}
		lo, hi, hasLo, hasHi := foldConstraintRange(rc)
		if (hasLo && iv.hi < lo) || (hasHi && iv.lo > hi) {
			c.report(valueExpr, diag.SeverityError, diag.CodeRangeConstraintViolation,
				fmt.Sprintf("value in [%s, %s] is always outside the range %s of %s",
					fmtBound(iv.lo), fmtBound(iv.hi), rangeConstraintString(rc), ct.Name))
			return
		}
	}
}

// foldConstraintRange folds a RangeConstraint's bounds to an inclusive [lo, hi]
// integer interval, with hasLo/hasHi marking which ends are present (a range may be
// open-ended). An `..<` exclusive end is decremented to inclusive. An unfoldable
// bound (an identifier / compound expression) leaves that end absent — conservative.
func foldConstraintRange(rc *types.RangeConstraint) (lo, hi int64, hasLo, hasHi bool) {
	if rc.Start != nil {
		if v, ok := foldConstraintBound(rc.Start); ok {
			lo, hasLo = v, true
		}
	}
	if rc.End != nil {
		if v, ok := foldConstraintBound(rc.End); ok {
			if rc.Comparator == "<" {
				v-- // exclusive end (..<)
			}
			hi, hasHi = v, true
		}
	}
	return
}

func foldConstraintBound(m types.MathConstraintExpr) (int64, bool) {
	switch e := m.(type) {
	case *types.MathConstraintLiteralExpr:
		return e.Value.Int64()
	case *types.MathConstraintNegationExpr:
		if v, ok := foldConstraintBound(e.Operand); ok {
			return -v, true
		}
	}
	return 0, false
}

// rangeConstraintString renders a RangeConstraint back to its source form for a
// diagnostic (`0..=100`, `0..<360`, `..=100`, `0..`).
func rangeConstraintString(rc *types.RangeConstraint) string {
	start := ""
	if rc.Start != nil {
		start = rc.Start.GetName()
	}
	end := ""
	if rc.End != nil {
		end = rc.Comparator + rc.End.GetName()
	}
	return fmt.Sprintf("%s..%s", start, end)
}

// ── divide analysis: diagnostic (E021) + trap elision ────────────────────────

// checkDivision handles a division-family op (`/`, `%`, `%%`, and their compound
// forms) two ways from the operand ranges:
//
//   - **Diagnostic (lyra-E021).** A definite divide-by-zero — the divisor is
//     *always* 0 on this (reachable) path — is a guaranteed runtime trap, so it's
//     reported at compile time, the error-reporting twin of the elision below and
//     symmetric with E020. Scoped to an *identifier* divisor proven `[0,0]`: a
//     literal / constant-folded zero (`5 / 0`, `10 / (5-5)`) is already the
//     typechecker's constant-fold check, so restricting to a variable both avoids
//     double-reporting and captures exactly this pass's value-add — a *non-constant*
//     divisor proven zero by flow (`let b = 0; a / b`, or `if b == 0 { a / b }`).
//
//   - **Elision (SafetyTable).** Whether the two runtime traps the op carries can
//     be dropped by the backend: divisor ≠ 0 → `noDivZero`; not INT_MIN/-1 →
//     `noDivOverflow` (provable when the dividend is never the type minimum OR the
//     divisor is never -1; unsigned division never overflows).
//
// typeExpr supplies the operation's integer type (its INT_MIN): the op node itself
// for a binary `/`, or the RHS for a compound `/=` (whose void result type isn't
// the integer width). Nothing fires while silent (a loop's fixpoint) or when the
// relevant operand isn't a tracked integer — absence keeps the runtime trap and
// reports nothing, so an imprecise analysis is always safe.
func (c *rangeChecker) checkDivision(e, typeExpr, divisorExpr ast.Expression, dividend interval, dividendTracked bool, divisor interval, divisorTracked bool) {
	if c.silent {
		return
	}
	// Diagnostic: an identifier divisor whose tracked range is exactly {0}.
	if id, ok := divisorExpr.(*ast.IdentifierExpr); ok && divisorTracked && divisor.lo == 0 && divisor.hi == 0 {
		c.report(e, diag.SeverityError, diag.CodeDivideByZero,
			fmt.Sprintf("this operation always divides by zero: %s is always 0 here", id.Name))
	}
	if divisorTracked && (divisor.lo > 0 || divisor.hi < 0) {
		c.safe.noDivZero[e] = true
	}
	tmin, _, ok := c.intBoundsOf(typeExpr)
	if !ok {
		return
	}
	if tmin >= 0 { // unsigned type: division never overflows
		c.safe.noDivOverflow[e] = true
		return
	}
	dividendNeverMin := dividendTracked && dividend.lo > tmin
	divisorNeverNegOne := divisorTracked && (divisor.lo > -1 || divisor.hi < -1)
	if dividendNeverMin || divisorNeverNegOne {
		c.safe.noDivOverflow[e] = true
	}
}

// evalIndex analyzes an array index `xs[i]` two ways from the index range (a
// negative index counts from the end, so the valid range is `[-size, size)`):
//
//   - **Elision.** An index provably within `[0, size)` is in range *and*
//     non-negative, so both the bounds trap and the from-the-end adjustment are
//     unnecessary — marked `inBounds`. A negative-but-valid index (`[-size,-1]`) is
//     left to the runtime check; only the common non-negative case is elided.
//   - **Diagnostic (lyra-E022).** A *non-singleton* index range entirely outside
//     `[-size, size)` is a definite out-of-bounds — a guaranteed runtime trap,
//     reported at compile time (the error-reporting twin of the elision). The
//     non-singleton requirement is what keeps it from double-reporting the
//     typechecker's own constant-index check (`inferIndexExpr`/`resolveConstantInt`):
//     that check only resolves a *single* constant index, so whenever it would fire
//     the range pass sees a singleton `[k,k]` — hence a non-singleton range means
//     the typechecker didn't report it. A single constant OOB stays the
//     typechecker's; the range pass adds the flow-proven range case
//     (`if i >= size { xs[i] }`).
//
// Nothing fires while silent or when the index isn't a tracked integer; absence
// keeps the runtime trap and reports nothing, so an imprecise analysis is always
// safe. The value of `xs[i]` isn't tracked (⊤ / its element type range).
func (c *rangeChecker) evalIndex(st rangeEnv, v *ast.IndexExpr) (interval, bool, rangeEnv) {
	_, _, st = c.eval(st, v.Object) // an array, not an int — threads state + diagnostics
	idx, idxTracked, st2 := c.eval(st, v.Index)
	st = st2
	if idxTracked && !c.silent {
		if size, ok := c.arraySize(v.Object); ok {
			switch {
			case idx.lo >= 0 && idx.hi < size:
				c.safe.inBounds[v] = true
			case idx.lo < idx.hi && (idx.lo >= size || idx.hi < -size):
				c.report(v, diag.SeverityError, diag.CodeIndexOutOfBounds,
					fmt.Sprintf("this index is always out of bounds: it is always in [%s, %s], outside the valid range [-%d, %d) for a size-%d array",
						fmtBound(idx.lo), fmtBound(idx.hi), size, size, size))
			}
		}
	}
	return c.typeIntervalIn(v, st)
}

// arraySize returns the element count of obj's type when it's a fixed-size array.
func (c *rangeChecker) arraySize(obj ast.Expression) (int64, bool) {
	t, ok := c.tt.Get(obj)
	if !ok {
		return 0, false
	}
	if at, ok := t.(types.StaticArrayType); ok {
		return int64(at.Size), true
	}
	return 0, false
}

// ── type lookups ─────────────────────────────────────────────────────────────

// typeIntervalIn returns e's type range as a tracked value (threading st), or
// not-tracked when e's type isn't a bounded integer representable in int64.
func (c *rangeChecker) typeIntervalIn(e ast.Expression, st rangeEnv) (interval, bool, rangeEnv) {
	if lo, hi, ok := c.intBoundsOf(e); ok {
		return interval{lo, hi}, true, st
	}
	return interval{}, false, st
}

func (c *rangeChecker) intBoundsOf(e ast.Expression) (int64, int64, bool) {
	t, ok := c.tt.Get(e)
	if !ok {
		return 0, 0, false
	}
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return 0, 0, false
	}
	return intBounds(p.Name)
}

func (c *rangeChecker) typeName(e ast.Expression) string {
	if t, ok := c.tt.Get(e); ok {
		return t.String()
	}
	return "the target type"
}

func (c *rangeChecker) report(e ast.Expression, sev diag.Severity, code, msg string) {
	if c.silent {
		return
	}
	c.diagnostics = append(c.diagnostics, diag.Diagnostic{
		Location: e.GetLocation(),
		Severity: sev,
		Code:     code,
		Message:  msg,
	})
}

// ── helpers ──────────────────────────────────────────────────────────────────

// havoc drops the given variables from env (widening each to ⊤ / its type range).
func havoc(env rangeEnv, names map[string]bool) rangeEnv {
	for n := range names {
		delete(env.vars, n)
	}
	return env
}

// assignedNames collects every variable a subtree declares or reassigns (a
// `let`/`var`, a plain reassignment, or a `+=`-style compound assignment).
func assignedNames(node any) map[string]bool {
	out := map[string]bool{}
	onStmt := func(s ast.Statement) bool {
		switch v := s.(type) {
		case *ast.VarDeclStmt:
			out[v.Name] = true
		case *ast.VarReassignmentStmt:
			out[v.Name] = true
		}
		return true
	}
	onExpr := func(e ast.Expression) bool {
		if m, ok := e.(*ast.MathAssignOpExpr); ok {
			out[m.Left.Name] = true
		}
		return true
	}
	switch n := node.(type) {
	case ast.Statement:
		ast.WalkStmt(n, onStmt, onExpr)
	case ast.Expression:
		ast.WalkExpr(n, onStmt, onExpr)
	}
	return out
}

func isComparison(op ast.BooleanBinaryOp) bool {
	switch op {
	case ast.BooleanBinaryOpLT, ast.BooleanBinaryOpLTE, ast.BooleanBinaryOpGT,
		ast.BooleanBinaryOpGTE, ast.BooleanBinaryOpEq, ast.BooleanBinaryOpNEq:
		return true
	}
	return false
}

func involvesVariable(a, b ast.Expression) bool {
	_, aVar := a.(*ast.IdentifierExpr)
	_, bVar := b.(*ast.IdentifierExpr)
	return aVar || bVar
}

// compareConst reports whether `a op b` is constant over the intervals, and its
// value. The always-true / always-false conditions per operator.
func compareConst(op ast.BooleanBinaryOp, a, b interval) (result, constant bool) {
	// A fold must not treat a ±∞ sentinel bound as a finite value: a u64's upper is
	// +∞ (its true max is unrepresentable), so e.g. `x > MaxInt64` is satisfiable and
	// must NOT fold to always-false. Each conclusion is therefore guarded so it fires
	// only when the bounds it treats as finite really are finite. `upperFin(iv)` =
	// "iv's upper is a real bound (not +∞)", `lowerFin(iv)` = "iv's lower is real (not
	// -∞)". These change nothing for i8..u32 (their bounds are never the sentinels);
	// they only suppress folds keyed off a u64's +∞ upper or an i64 extreme.
	upperFin := func(iv interval) bool { return iv.hi != posInf }
	lowerFin := func(iv interval) bool { return iv.lo != negInf }
	switch op {
	case ast.BooleanBinaryOpLT: // a < b
		if upperFin(a) && lowerFin(b) && a.hi < b.lo {
			return true, true
		}
		if lowerFin(a) && upperFin(b) && a.lo >= b.hi {
			return false, true
		}
	case ast.BooleanBinaryOpLTE:
		if upperFin(a) && lowerFin(b) && a.hi <= b.lo {
			return true, true
		}
		if lowerFin(a) && upperFin(b) && a.lo > b.hi {
			return false, true
		}
	case ast.BooleanBinaryOpGT: // a > b
		if lowerFin(a) && upperFin(b) && a.lo > b.hi {
			return true, true
		}
		if upperFin(a) && lowerFin(b) && a.hi <= b.lo {
			return false, true
		}
	case ast.BooleanBinaryOpGTE:
		if lowerFin(a) && upperFin(b) && a.lo >= b.hi {
			return true, true
		}
		if upperFin(a) && lowerFin(b) && a.hi < b.lo {
			return false, true
		}
	case ast.BooleanBinaryOpEq:
		if av, ok := finiteSingle(a); ok {
			if bv, bok := finiteSingle(b); bok && av == bv {
				return true, true
			}
		}
		if disjoint(a, b) {
			return false, true
		}
	case ast.BooleanBinaryOpNEq:
		if disjoint(a, b) {
			return true, true
		}
		if av, ok := finiteSingle(a); ok {
			if bv, bok := finiteSingle(b); bok && av == bv {
				return false, true
			}
		}
	}
	return false, false
}

// finiteSingle reports iv's value when it is a single *finite* point (not a ±∞
// sentinel, which represents an unbounded — hence not truly single — end).
func finiteSingle(iv interval) (int64, bool) {
	v, ok := iv.single()
	if !ok || v == posInf || v == negInf {
		return 0, false
	}
	return v, true
}

// disjoint reports whether a and b provably share no value — the two ordered gaps,
// each guarded so a ±∞ end (which is not a finite separator) can't manufacture it.
func disjoint(a, b interval) bool {
	return (a.hi != posInf && b.lo != negInf && a.hi < b.lo) ||
		(b.hi != posInf && a.lo != negInf && b.hi < a.lo)
}

// negateOp returns the operator of the negated comparison (the else-branch).
func negateOp(op ast.BooleanBinaryOp) ast.BooleanBinaryOp {
	switch op {
	case ast.BooleanBinaryOpLT:
		return ast.BooleanBinaryOpGTE
	case ast.BooleanBinaryOpLTE:
		return ast.BooleanBinaryOpGT
	case ast.BooleanBinaryOpGT:
		return ast.BooleanBinaryOpLTE
	case ast.BooleanBinaryOpGTE:
		return ast.BooleanBinaryOpLT
	case ast.BooleanBinaryOpEq:
		return ast.BooleanBinaryOpNEq
	case ast.BooleanBinaryOpNEq:
		return ast.BooleanBinaryOpEq
	}
	return op
}

// flipOp swaps the operands: `a OP b` ≡ `b flip(OP) a`.
func flipOp(op ast.BooleanBinaryOp) ast.BooleanBinaryOp {
	switch op {
	case ast.BooleanBinaryOpLT:
		return ast.BooleanBinaryOpGT
	case ast.BooleanBinaryOpLTE:
		return ast.BooleanBinaryOpGTE
	case ast.BooleanBinaryOpGT:
		return ast.BooleanBinaryOpLT
	case ast.BooleanBinaryOpGTE:
		return ast.BooleanBinaryOpLTE
	}
	return op // == and != are symmetric
}

// ── interval arithmetic (all overflow-guarded in int64) ──────────────────────

func addI(a, b interval) (interval, bool) {
	lo, ok1 := addChecked(a.lo, b.lo)
	hi, ok2 := addChecked(a.hi, b.hi)
	if !ok1 || !ok2 {
		return interval{}, false
	}
	return interval{lo, hi}, true
}

func subI(a, b interval) (interval, bool) {
	lo, ok1 := subChecked(a.lo, b.hi)
	hi, ok2 := subChecked(a.hi, b.lo)
	if !ok1 || !ok2 {
		return interval{}, false
	}
	return interval{lo, hi}, true
}

func mulI(a, b interval) (interval, bool) {
	corners := [4][2]int64{{a.lo, b.lo}, {a.lo, b.hi}, {a.hi, b.lo}, {a.hi, b.hi}}
	var lo, hi int64
	for i, cn := range corners {
		p, ok := mulChecked(cn[0], cn[1])
		if !ok {
			return interval{}, false
		}
		if i == 0 {
			lo, hi = p, p
			continue
		}
		lo, hi = minI(lo, p), maxI(hi, p)
	}
	return interval{lo, hi}, true
}

func negI(a interval) (interval, bool) {
	if a.lo == math.MinInt64 || a.hi == math.MinInt64 {
		return interval{}, false
	}
	return interval{-a.hi, -a.lo}, true
}

func addChecked(a, b int64) (int64, bool) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, false
	}
	return s, true
}

func subChecked(a, b int64) (int64, bool) {
	d := a - b
	if (b < 0 && d < a) || (b > 0 && d > a) {
		return 0, false
	}
	return d, true
}

func mulChecked(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a == math.MinInt64 || b == math.MinInt64 {
		return 0, false
	}
	p := a * b
	if p/b != a {
		return 0, false
	}
	return p, true
}

// posInf / negInf are the interval-domain sentinels for an unbounded end (±∞).
// They reuse the extreme int64 values, so an interval bound at ±∞ is a sound
// over-approximation (nothing lies beyond it). Two consequences: `u64`'s upper is
// +∞ (its true max 2^64-1 doesn't fit int64 — see intBounds), and a *real* i64
// value of exactly MaxInt64/MinInt64 is modelled as ±∞ (a harmless conservatism —
// it only ever *widens*). compareConst is written to never fold a comparison based
// on treating a sentinel bound as a finite value (which would be unsound for u64,
// where `x > MaxInt64` is genuinely satisfiable).
const (
	posInf int64 = math.MaxInt64
	negInf int64 = math.MinInt64
)

// fmtBound renders an interval bound for a diagnostic, showing a ±∞ sentinel as
// such rather than as its raw int64 value — so a u64's unbounded-above upper reads
// as `+∞`, not the misleading `9223372036854775807`.
func fmtBound(v int64) string {
	switch v {
	case posInf:
		return "+∞"
	case negInf:
		return "-∞"
	default:
		return fmt.Sprintf("%d", v)
	}
}

// intBounds is the inclusive [min, max] of a concrete integer type. `u64`'s true
// max (2^64-1) overflows int64, so its upper is the +∞ sentinel: the lower bound
// (0) is exact and load-bearing (every diagnostic/elision that fires for a u64
// keys off it or off an "entirely outside" test, never off the fake upper), while
// the +∞ upper only ever causes conservative untracking via the int64-overflow
// guards in the interval arithmetic — never a wrong result. Untyped and
// non-integer types return ok=false.
func intBounds(name types.PrimitiveTypeName) (min, max int64, ok bool) {
	switch name {
	case types.Int8:
		return math.MinInt8, math.MaxInt8, true
	case types.UInt8:
		return 0, math.MaxUint8, true
	case types.Int16:
		return math.MinInt16, math.MaxInt16, true
	case types.UInt16:
		return 0, math.MaxUint16, true
	case types.Int32:
		return math.MinInt32, math.MaxInt32, true
	case types.UInt32:
		return 0, math.MaxUint32, true
	case types.Int64:
		return math.MinInt64, math.MaxInt64, true
	case types.UInt64:
		return 0, posInf, true // [0, +∞): true max 2^64-1 is unrepresentable
	}
	return 0, 0, false
}

func dec(v int64) int64 {
	if v == math.MinInt64 {
		return v
	}
	return v - 1
}

func inc(v int64) int64 {
	if v == math.MaxInt64 {
		return v
	}
	return v + 1
}

func minI(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxI(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
