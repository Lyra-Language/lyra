package typechecker

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// checkNewtypeConstraints tests a compile-time constant against **every** constraint
// kind a newtype can declare — `range(...)`, `values(...)` and `pattern(...)` — and is
// the one place any of them is enforced.
//
// **It is called from propagateLiteralType's newtype arm, which is what makes the
// check follow the type instead of the binding.** Until 08/12 the three checks were
// called from the two assignment sites only, so `let p: Percent = 150` was caught while
// the same literal reaching the same newtype through an **argument**
// (`show_it(150)`), a **return** (`-> Percent => 150`) or an **array element**
// (`let xs: []Percent = [10, 150]`) was not — silently, in a language whose whole
// argument for `range(...)` is that the constraint is checked. Those are exactly the
// positions where a newtype context arrives by propagation rather than by annotation,
// which is why the propagation point is the right home: one predicate on the path the
// type already travels, rather than the same check re-attached at each consumer
// (`CLAUDE.md` hazard 8).
//
// It fires only for a **foldable constant** — a literal, a negated literal, or a folded
// arithmetic constant for the integer case — against foldable literal bounds. A
// non-constant value is left to the value-range pass (which independently reports a
// provable violation, e.g. `let n: u8 = 150` then `let p: Percent = n`) and otherwise
// to the runtime. So this stays a definite-only compile-time check, like
// checkIntegerLiteralRange, and never yields a false positive.
//
// A constrained newtype is a *ConstrainedType, which checkIntegerLiteralRange skips (it
// only matches a bare PrimitiveType), so there is no double report against the *base*
// type's own bounds: a constraint is normally a subset of the base, so a constraint
// violation subsumes a base overflow.
func (tc *TypeChecker) checkNewtypeConstraints(value ast.Expression, declType types.Type) {
	ct, ok := declType.(*types.ConstrainedType)
	if !ok || value == nil {
		return
	}
	// One value is one mistake, however many contexts narrow it. A leaf can be reached
	// by propagation more than once (a generic call site propagates against both the
	// declared and the instantiated signature), and the same guard exists one layer over
	// for the base-width check — see overflowReported, whose comment records the same
	// finding.
	if tc.constraintReported[value] {
		return
	}
	// **A constructor call is not a second construction.** `Percent(n)` reaches here
	// twice — once as the constructor's own operand (`n`), and again as the call node
	// when the enclosing context propagates the newtype onto it — and the two are one
	// value entering one newtype. Checking both emitted the runtime check twice (four
	// traps for one `Percent(n)`) and reported lyra-E054 twice for one `Digits(s)`.
	// The operand is the one to keep: it is where the value actually crosses into the
	// newtype, and it is the node every other position has too.
	if isNewtypeConstructorCall(value, ct) {
		return
	}
	if tc.constraintReported == nil {
		tc.constraintReported = map[ast.Expression]bool{}
	}
	tc.constraintReported[value] = true

	tc.checkPatternConstraints(value, ct)
	tc.checkLiteralUnionConstraints(value, ct)
	base, ok := ct.Type.(types.PrimitiveType)
	if !ok {
		return
	}
	for _, c := range ct.Constraints {
		switch con := c.(type) {
		case *types.RangeConstraint:
			switch {
			case isAnyConcreteInt(base.Name):
				tc.checkIntRange(ct.Name, value, con)
			case isFloatType(base):
				tc.checkFloatRange(ct.Name, value, con)
			}
		case *types.StepConstraint:
			tc.checkStep(ct, value, con, base)
		}
	}
	tc.scheduleRuntimeConstraintCheck(value, ct, base)
}

// scheduleRuntimeConstraintCheck records value for a **run-time** constraint check
// when the checks above could not settle it — the second rung of the language's own
// ladder, which `where` constraints did not have until 08/13.
//
// Everything above is definite-only: it fires for a foldable constant and stays
// silent otherwise, which is right for a *compile-time* answer and was the whole
// enforcement story. So a value the compiler could not see through — a parameter, a
// parsed number, anything computed — entered a constrained newtype unchecked, and
// `mk(200)` on a `Percent = u8 where range(0..<=100)` ran and printed 200. Those are
// precisely the values worth checking: a literal is written by the author, while a
// runtime value came from outside.
//
// A **foldable constant is not recorded**, because it was already decided above: a
// bad one is a compile error and a good one needs nothing. So the check costs a
// branch exactly where the compiler could not do better, and the optimizer folds
// away the ones that are provable but not foldable here.
//
// `pattern(...)` is deliberately not part of this — matching one needs a regex engine
// in the runtime, which `lyra-E052` records the absence of, so a non-provable value
// is *refused* instead (checkPatternConstraints).
func (tc *TypeChecker) scheduleRuntimeConstraintCheck(value ast.Expression, ct *types.ConstrainedType, base types.PrimitiveType) {
	// A **string** base carries `pattern(...)`, checked at run time by the DFA the
	// backend compiles from the pattern (08/13). A string *literal* was already
	// matched above, so only a value the compiler could not read is scheduled.
	if base.Name == types.String {
		if _, isLiteral := value.(*ast.StringLiteralExpr); isLiteral {
			return
		}
		if _, hasPattern := firstPatternConstraint(ct); hasPattern {
			tc.constraintChecks.Require(value, ct)
		}
		return
	}
	if !isAnyConcreteInt(base.Name) && !isFloatType(base) {
		return // a runtime check exists only for the numeric constraint kinds
	}
	if !hasRuntimeCheckableConstraint(ct) {
		return
	}
	if constraintValueIsFoldable(value, base) {
		return // settled at compile time, in either direction
	}
	tc.constraintChecks.Require(value, ct)
}

// isNewtypeConstructorCall reports whether value is `Name(x)` for this very newtype —
// the explicit constructor, whose operand carries the constraint check.
//
// It is a **TupleLiteralExpr**, not a FunctionCallExpr: `Percent(n)` parses as a
// named tuple literal, which is the same node `tuple Rgb(u8, u8, u8)` constructs
// with (see inferNewtypeConstruction, and the parenthesised-operand note in the
// workspace CLAUDE.md). Testing for a call matched nothing and left the duplicate in
// place — worth stating here, since "constructor" reads like "call" everywhere else.
func isNewtypeConstructorCall(value ast.Expression, ct *types.ConstrainedType) bool {
	tup, ok := value.(*ast.TupleLiteralExpr)
	return ok && tup.Name == ct.Name && len(tup.Elements) == 1
}

// hasRuntimeCheckableConstraint reports whether ct declares a constraint the backend
// can test — `range`, `values` or `step`. A newtype with only `pattern` (or with no
// constraints at all) schedules nothing.
func hasRuntimeCheckableConstraint(ct *types.ConstrainedType) bool {
	for _, c := range ct.Constraints {
		switch c.(type) {
		case *types.RangeConstraint, *types.LiteralUnionConstraint, *types.StepConstraint:
			return true
		}
	}
	return false
}

// constraintValueIsFoldable reports whether the compile-time checks could evaluate
// value — the same question they each ask, asked once, so "was this already decided?"
// cannot drift from "did that check run?".
func constraintValueIsFoldable(value ast.Expression, base types.PrimitiveType) bool {
	if isFloatType(base) {
		_, ok := extractFloatLiteralValue(value)
		return ok
	}
	_, ok := extractIntLiteralValue(value)
	return ok
}

// checkLiteralUnionConstraints tests a literal against a `values(...)` constraint
// (`newtype Status = i32 where values(200, 404, 500)`).
//
// **Nothing enforced this until 08/12** — `let s: Status = 302` compiled clean — which
// made `values(...)` a constraint the compiler collected, validated the *shape* of, and
// then ignored: the collected-and-unread shape this project keeps digging out, in the
// one place where the declaration's entire purpose is to be checked.
//
// The comparison is by value and not by source text, so `200` matches the allowed `200`
// while `2.0` and `2` are compared numerically rather than as the strings "2.0" and "2".
// A value that is not a foldable literal is skipped, as everywhere else here.
func (tc *TypeChecker) checkLiteralUnionConstraints(value ast.Expression, ct *types.ConstrainedType) {
	for _, c := range ct.Constraints {
		lu, ok := c.(*types.LiteralUnionConstraint)
		if !ok || len(lu.Values) == 0 {
			continue
		}
		got, ok := literalConstantText(value)
		if !ok {
			continue // not a compile-time literal — nothing to compare
		}
		allowed := make([]string, 0, len(lu.Values))
		matched := false
		for _, av := range lu.Values {
			expr, isExpr := av.(ast.Expression)
			if !isExpr {
				continue
			}
			text, ok := literalConstantText(expr)
			if !ok {
				continue
			}
			allowed = append(allowed, text)
			if text == got {
				matched = true
			}
		}
		if !matched && len(allowed) > 0 {
			tc.addErrorCode(value.GetLocation(), SeverityError, diag.CodeValuesConstraintViolation,
				"value %s is not one of the values allowed by %s (%s)",
				got, ct.Name, strings.Join(allowed, ", "))
		}
	}
}

// literalConstantText renders a literal expression to a canonical string used to
// compare a value against a `values(...)` member. Both sides go through this one
// function, so the comparison is between *values* rather than between spellings —
// `0x10` and `16` render alike, and an integer written into a float union renders as a
// float. Anything that is not a compile-time literal answers false.
func literalConstantText(expr ast.Expression) (string, bool) {
	switch e := expr.(type) {
	case *ast.StringLiteralExpr:
		return fmt.Sprintf("%q", e.Value), true
	case *ast.BooleanLiteralExpr:
		return fmt.Sprintf("%t", e.Value), true
	case *ast.FloatLiteralExpr:
		return fmt.Sprintf("%g", e.Value), true
	case *ast.IntegerLiteralExpr:
		return e.BigValue().String(), true
	case *ast.NegationExpr:
		if inner, ok := literalConstantText(e.Operand); ok {
			return "-" + inner, true
		}
	}
	return "", false
}

// checkStep tests a compile-time constant against a `step(...)` constraint, whose
// meaning `types/step.go` already fixed for both spellings: **the values covered are
// start, start+step, start+2\*step, …**. So the test is that the value sits on the
// grid, `(v - start) % step == 0`, with `start` taken from the newtype's own
// `range(...)` when it declares one and 0 otherwise — the natural origin for "a
// multiple of step" when no range names a first value.
//
// **Nothing read StepConstraint until 08/13.** `step(15)` was collected, validated for
// well-formedness (zero and fractional-over-integer steps refused) and then enforced
// against no value at all, so `newtype CompassHeading = i64 where range(0..<360),
// step(15)` accepted 7. That is the collected-and-unread shape this project keeps
// digging out, and `types/step.go`'s own comment recorded it as a known asymmetry.
//
// Floats use exact arithmetic — `math.Mod` — which is what the constraint literally
// says. A step the base cannot represent exactly (`step(0.1)` on an f32) will
// therefore reject values that look like multiples of it; that is the constraint the
// author wrote rather than a defect here, and `range(0..<=100), step(0.25)` (the
// documented example) is exact.
func (tc *TypeChecker) checkStep(ct *types.ConstrainedType, value ast.Expression, sc *types.StepConstraint, base types.PrimitiveType) {
	switch {
	case isAnyConcreteInt(base.Name):
		step, ok := foldConstraintInt(sc.Value)
		if !ok || step == 0 {
			return // an unfoldable or degenerate step; the collector reports the latter
		}
		v, ok := extractIntLiteralValue(value)
		if !ok {
			return
		}
		start, _ := constraintGridOrigin(ct)
		if (v-start)%step != 0 {
			tc.reportStepViolation(ct.Name, fmt.Sprintf("%d", v), value, sc, start)
		}
	case isFloatType(base):
		step, ok := foldConstraintFloat(sc.Value)
		if !ok || step == 0 {
			return
		}
		v, ok := extractFloatLiteralValue(value)
		if !ok {
			return
		}
		_, startF := constraintGridOrigin(ct)
		if math.Mod(v-startF, step) != 0 {
			tc.reportStepViolation(ct.Name, fmt.Sprintf("%g", v), value, sc, 0)
		}
	}
}

// constraintGridOrigin is the value a `step(...)` grid is measured from: the start of
// the newtype's `range(...)` when it declares one with a foldable start, and 0
// otherwise. Both flavors are returned because the caller knows which base it is on.
func constraintGridOrigin(ct *types.ConstrainedType) (int64, float64) {
	for _, c := range ct.Constraints {
		rc, ok := c.(*types.RangeConstraint)
		if !ok || rc.Start == nil {
			continue
		}
		if i, ok := foldConstraintInt(rc.Start); ok {
			f, _ := foldConstraintFloat(rc.Start)
			return i, f
		}
		if f, ok := foldConstraintFloat(rc.Start); ok {
			return 0, f
		}
	}
	return 0, 0
}

func (tc *TypeChecker) reportStepViolation(typeName, valueStr string, value ast.Expression, sc *types.StepConstraint, start int64) {
	from := ""
	if start != 0 {
		from = fmt.Sprintf(" from %d", start)
	}
	tc.addErrorCode(value.GetLocation(), SeverityError, diag.CodeStepConstraintViolation,
		"value %s is not a multiple of the step %s%s of %s",
		valueStr, sc.Value.GetName(), from, typeName)
}

// hasRangeConstraint reports whether a newtype declares any range(...) constraint.
// checkIntegerLiteralRange uses it to decide who reports an out-of-range constant:
// with a range constraint this function's caller owns it (lyra-E023), without one
// the base type's own bounds are the only check there is.
func hasRangeConstraint(ct *types.ConstrainedType) bool {
	for _, c := range ct.Constraints {
		if _, ok := c.(*types.RangeConstraint); ok {
			return true
		}
	}
	return false
}

func (tc *TypeChecker) checkIntRange(typeName string, value ast.Expression, rc *types.RangeConstraint) {
	v, ok := extractIntLiteralValue(value)
	if !ok {
		return // not a compile-time integer constant
	}
	// The judgment itself is intOutsideRangeConstraint (pattern_literals.go), shared
	// with the pattern check so an expression and a pattern cannot disagree about
	// one constraint.
	if intOutsideRangeConstraint(v, rc) {
		tc.reportRangeViolation(typeName, fmt.Sprintf("%d", v), value, rc)
	}
}

func (tc *TypeChecker) checkFloatRange(typeName string, value ast.Expression, rc *types.RangeConstraint) {
	v, ok := extractFloatLiteralValue(value)
	if !ok {
		return
	}
	belowStart := false
	if rc.Start != nil {
		if lo, ok := foldConstraintFloat(rc.Start); ok && v < lo {
			belowStart = true
		}
	}
	aboveEnd := false
	if rc.End != nil {
		if hi, ok := foldConstraintFloat(rc.End); ok {
			if rc.Comparator == "<" {
				aboveEnd = v >= hi
			} else {
				aboveEnd = v > hi
			}
		}
	}
	if belowStart || aboveEnd {
		tc.reportRangeViolation(typeName, fmt.Sprintf("%g", v), value, rc)
	}
}

// reportRangeViolation names no binding, where it used to lead with one (`p: value 150
// …`). The check moved onto the type's propagation path (checkNewtypeConstraints), and
// most positions it now covers have no name to give — an array element, a return, an
// argument — so a name would have had to be invented for them or omitted
// inconsistently. The diagnostic's *location* is the literal itself, which is what the
// prefix was standing in for, and the value-range pass's sibling message ("value in
// [150, 150] is always outside the range …") already reads this way.
func (tc *TypeChecker) reportRangeViolation(typeName, valueStr string, value ast.Expression, rc *types.RangeConstraint) {
	tc.addErrorCode(value.GetLocation(), SeverityError, diag.CodeRangeConstraintViolation,
		"value %s is outside the range %s of %s",
		valueStr, rangeConstraintString(rc), typeName)
}

// rangeConstraintString renders a RangeConstraint back to its source form for a
// diagnostic (`0..<=100`, `0..<360`, `..<=100`, `0..`).
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

// extractFloatLiteralValue folds a value expression to a float64 constant: a float
// literal, an integer literal (a float newtype may be assigned `90`), or a negated
// one. Anything else is not a compile-time float constant.
func extractFloatLiteralValue(expr ast.Expression) (float64, bool) {
	switch e := expr.(type) {
	case *ast.FloatLiteralExpr:
		return e.Value, true
	case *ast.IntegerLiteralExpr:
		f, _ := new(big.Float).SetInt(e.BigValue()).Float64()
		return f, true
	case *ast.NegationExpr:
		if inner, ok := extractFloatLiteralValue(e.Operand); ok {
			return -inner, true
		}
	}
	return 0, false
}

// foldConstraintInt / foldConstraintFloat evaluate a range-constraint bound to a
// compile-time number. They cover the literal and negated-literal forms; an
// identifier (a named constant) or a compound arithmetic bound isn't folded, so
// that side of the range is simply not enforced (conservative — never a false
// positive).
func foldConstraintInt(m types.MathConstraintExpr) (int64, bool) {
	switch e := m.(type) {
	case *types.MathConstraintLiteralExpr:
		return e.Value.Int64()
	case *types.MathConstraintNegationExpr:
		if v, ok := foldConstraintInt(e.Operand); ok {
			return -v, true
		}
	}
	return 0, false
}

func foldConstraintFloat(m types.MathConstraintExpr) (float64, bool) {
	switch e := m.(type) {
	case *types.MathConstraintLiteralExpr:
		if f, ok := e.Value.Float64(); ok {
			return f, true
		}
		if i, ok := e.Value.Int64(); ok { // an integer bound on a float range
			return float64(i), true
		}
	case *types.MathConstraintNegationExpr:
		if v, ok := foldConstraintFloat(e.Operand); ok {
			return -v, true
		}
	}
	return 0, false
}
