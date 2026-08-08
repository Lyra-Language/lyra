package typechecker

import (
	"fmt"
	"math/big"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// checkRangeConstraints tests a compile-time numeric value against every
// RangeConstraint on a range-constrained newtype (`newtype Percent = u8 where
// range(0..<=100)`) — the numeric analogue of checkPatternConstraints (which does
// the same for string PatternConstraints). It fires only for a foldable constant
// value (an int/float literal, incl. a negated one, or a folded arithmetic
// constant for the integer case) against foldable literal bounds; a non-constant
// value is left unchecked here (a future flow-sensitive pass, or the runtime, owns
// it) — so this is a definite-only compile-time check, like checkIntegerLiteralRange.
//
// A constrained newtype is a *ConstrainedType, which checkIntegerLiteralRange
// skips (it only matches a bare PrimitiveType), so there is no double report: the
// constraint is normally a subset of the base type, so a constraint violation
// subsumes any base-type overflow.
func (tc *TypeChecker) checkRangeConstraints(name string, value ast.Expression, declType types.Type) {
	ct, ok := declType.(*types.ConstrainedType)
	if !ok {
		return
	}
	base, ok := ct.Type.(types.PrimitiveType)
	if !ok {
		return
	}
	for _, c := range ct.Constraints {
		rc, ok := c.(*types.RangeConstraint)
		if !ok {
			continue
		}
		switch {
		case isAnyConcreteInt(base.Name):
			tc.checkIntRange(name, ct.Name, value, rc)
		case isFloatType(base):
			tc.checkFloatRange(name, ct.Name, value, rc)
		}
	}
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

func (tc *TypeChecker) checkIntRange(name, typeName string, value ast.Expression, rc *types.RangeConstraint) {
	v, ok := extractIntLiteralValue(value)
	if !ok {
		return // not a compile-time integer constant
	}
	belowStart := false
	if rc.Start != nil {
		if lo, ok := foldConstraintInt(rc.Start); ok && v < lo {
			belowStart = true
		}
	}
	aboveEnd := false
	if rc.End != nil {
		if hi, ok := foldConstraintInt(rc.End); ok {
			if rc.Comparator == "<" {
				aboveEnd = v >= hi // exclusive end (..<)
			} else {
				aboveEnd = v > hi // inclusive end (..<=)
			}
		}
	}
	if belowStart || aboveEnd {
		tc.reportRangeViolation(name, typeName, fmt.Sprintf("%d", v), value, rc)
	}
}

func (tc *TypeChecker) checkFloatRange(name, typeName string, value ast.Expression, rc *types.RangeConstraint) {
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
		tc.reportRangeViolation(name, typeName, fmt.Sprintf("%g", v), value, rc)
	}
}

func (tc *TypeChecker) reportRangeViolation(name, typeName, valueStr string, value ast.Expression, rc *types.RangeConstraint) {
	tc.addErrorCode(value.GetLocation(), SeverityError, diag.CodeRangeConstraintViolation,
		"%s: value %s is outside the range %s of %s",
		name, valueStr, rangeConstraintString(rc), typeName)
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
