package checker

import (
	"fmt"
	"math"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// CheckIntegerRanges is a flow-sensitive value-range (interval) analysis over
// each function body. For every integer variable it tracks the interval [lo, hi]
// of values it can hold at each program point, and reports two things the
// literal-only checks can't:
//
//   - **lyra-E020 (error): a definite integer overflow.** An `+`/`-`/`*`/unary-`-`
//     whose operand ranges prove the result can't fit its type on *any* path
//     (`if x > 100 { x + 100 }` on an i8 — a guaranteed runtime trap). Only a
//     *definite* overflow is reported; a merely *possible* one (`a + b` on two
//     full-range i8s) is left to the runtime trap, so a correct program is never
//     flagged.
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
//     arithmetic is mostly ⊤). u64 is never tracked (2^64-1 doesn't fit int64).
//   - A branch refines a variable only against a comparison with a *pure* constant
//     side (literal / negated literal / another tracked variable); anything else
//     leaves the branch un-refined.
//   - A loop **havocs** every variable it assigns (⊤ for the body and after) — a
//     variable modified across 0..N iterations can hold any value in its type, so
//     a single iteration's interval would be unsound. Precise loop widening is a
//     later refinement.
//   - A `match` gets no per-arm scrutinee refinement yet (each arm from the
//     pre-match state), and its arm values merge by union.
//
// The pass runs after typechecking (it needs the TypeTable for each expression's
// width and signedness).
func CheckIntegerRanges(program *ast.Program, tt *typetable.TypeTable) ([]diag.Diagnostic, *SafetyTable) {
	c := &rangeChecker{tt: tt, safe: &SafetyTable{ops: map[ast.Expression]bool{}}}
	for _, stmt := range program.Statements {
		c.topLevel(stmt)
	}
	return c.diagnostics, c.safe
}

// SafetyTable records the integer arithmetic operations (`+`/`-`/`*`, and their
// `+=`-style compound forms) that the range analysis proved cannot overflow their
// type on any path — the operand ranges keep the result within the type. The
// backend consults it to **elide** the overflow check for those ops (emit the
// plain instruction, no `with.overflow`+trap). It's keyed by the AST expression
// node, which is the same object the backend lowers (both passes walk the one
// *ast.Program), so the lookup is a pointer match. Membership is conservative:
// only a *proven*-safe op is present; anything uncertain is absent and keeps its
// runtime trap, so a wrong entry — the only thing that could turn a real overflow
// into a silent miscompile — never occurs.
type SafetyTable struct {
	ops map[ast.Expression]bool
}

// NoOverflow reports whether e is a proven-non-overflowing arithmetic op. A nil
// table (no analysis ran) is safe-by-absence: reports false, so the trap stays.
func (t *SafetyTable) NoOverflow(e ast.Expression) bool {
	if t == nil {
		return false
	}
	return t.ops[e]
}

type rangeChecker struct {
	tt          *typetable.TypeTable
	diagnostics []diag.Diagnostic
	safe        *SafetyTable
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
		default:
			// Division / remainder can't grow magnitude and can't overflow the way
			// +/-/* do (the one signed div overflow, INT_MIN/-1, is left to the
			// runtime trap); its precise result range isn't tracked here.
			return c.typeIntervalIn(e, st)
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
	// Each arm from the post-scrutinee state (no per-arm scrutinee refinement yet);
	// the result env is the union of the arms', the value the union of arm values.
	result := rangeEnv{reachable: false}
	var val interval
	allTracked := true
	first := true
	for i := range v.MatchArms {
		arm := v.MatchArms[i]
		armEnv := st.clone()
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

func (c *rangeChecker) evalForLoop(st rangeEnv, v *ast.ForLoopExpr) (interval, bool, rangeEnv) {
	// The init runs once in the outer state (so `var i: i8 = <overflow>` is still
	// caught), then every variable the loop assigns is havoc'd for the body and
	// after — across 0..N iterations it can hold any value in its type.
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
	body := havoc(st.clone(), assigned)
	if v.Condition != nil {
		_, _, body = c.eval(body, *v.Condition)
	}
	_, _, body = c.eval(body, &v.Body)
	if v.Post != nil {
		_, _, _ = c.eval(body, *v.Post)
	}
	return interval{}, false, havoc(st, assigned)
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
	body := havoc(st.clone(), assigned)
	_, _, _ = c.eval(body, &v.Body)
	return havoc(st, assigned)
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
			fmt.Sprintf("this operation always overflows %s: its result is always in [%d, %d], outside the valid range [%d, %d]",
				c.typeName(typeExpr), m.lo, m.hi, tmin, tmax))
		return interval{tmin, tmax}, true
	}
	if m.lo >= tmin && m.hi <= tmax {
		// The whole (over-approximated) result range fits the type, so the op can't
		// overflow on any path — record it so the backend can drop the trap.
		c.safe.ops[reportAt] = true
		return m, true
	}
	// A possible overflow: keep the runtime trap. Clamp downstream to the type range
	// (a non-trapping execution stays in range).
	return interval{maxI(m.lo, tmin), minI(m.hi, tmax)}, true
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
	switch op {
	case ast.BooleanBinaryOpLT:
		if a.hi < b.lo {
			return true, true
		}
		if a.lo >= b.hi {
			return false, true
		}
	case ast.BooleanBinaryOpLTE:
		if a.hi <= b.lo {
			return true, true
		}
		if a.lo > b.hi {
			return false, true
		}
	case ast.BooleanBinaryOpGT:
		if a.lo > b.hi {
			return true, true
		}
		if a.hi <= b.lo {
			return false, true
		}
	case ast.BooleanBinaryOpGTE:
		if a.lo >= b.hi {
			return true, true
		}
		if a.hi < b.lo {
			return false, true
		}
	case ast.BooleanBinaryOpEq:
		if av, aok := a.single(); aok {
			if bv, bok := b.single(); bok && av == bv {
				return true, true
			}
		}
		if a.hi < b.lo || b.hi < a.lo {
			return false, true
		}
	case ast.BooleanBinaryOpNEq:
		if a.hi < b.lo || b.hi < a.lo {
			return true, true
		}
		if av, aok := a.single(); aok {
			if bv, bok := b.single(); bok && av == bv {
				return false, true
			}
		}
	}
	return false, false
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

// intBounds is the inclusive [min, max] of a concrete integer type. u64 (max
// 2^64-1 overflows int64), untyped, and non-integer types return ok=false.
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
