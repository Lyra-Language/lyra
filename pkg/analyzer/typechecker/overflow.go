package typechecker

import (
	"math"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// extractIntLiteralValue attempts to fold expr to a compile-time integer
// constant. It handles:
//
//   - *ast.IntegerLiteralExpr                → (value, true)
//   - *ast.NegationExpr over a foldable expr → (-value, true)
//   - *ast.MathBinaryOpExpr (+, -, *) over    → (folded value, true)
//     two foldable integer operands
//
// Folding is done in int64. If any intermediate result overflows int64 the
// whole expression is treated as non-constant (returns false) rather than
// reporting a wrapped value — i64-range overflow is a separate concern and a
// wrapped value would produce a misleading message. Division/modulo are not
// folded: they cannot increase magnitude (so they can't cause the overflow
// this check targets) and Lyra's `%`/`%%` semantics shouldn't be guessed here.
//
// All other expressions return (0, false).
func extractIntLiteralValue(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return e.Value, true
	case *ast.NegationExpr:
		if inner, ok := extractIntLiteralValue(e.Operand); ok {
			// -math.MinInt64 would overflow; in practice the parser can't
			// produce a positive int64 that large, so this is safe.
			return -inner, true
		}
	case *ast.MathBinaryOpExpr:
		left, lok := extractIntLiteralValue(e.Left)
		right, rok := extractIntLiteralValue(e.Right)
		if !lok || !rok {
			return 0, false
		}
		switch e.Operator {
		case ast.MathBinaryOpAdd:
			return checkedAddInt64(left, right)
		case ast.MathBinaryOpSub:
			return checkedSubInt64(left, right)
		case ast.MathBinaryOpMul:
			return checkedMulInt64(left, right)
		}
	}
	return 0, false
}

// checkedAddInt64 / checkedSubInt64 / checkedMulInt64 perform the operation in
// int64, returning ok=false if the result overflows the int64 range.
func checkedAddInt64(a, b int64) (int64, bool) {
	sum := a + b
	if (a > 0 && b > 0 && sum < 0) || (a < 0 && b < 0 && sum >= 0) {
		return 0, false
	}
	return sum, true
}

func checkedSubInt64(a, b int64) (int64, bool) {
	diff := a - b
	if (b < 0 && diff < a) || (b > 0 && diff > a) {
		return 0, false
	}
	return diff, true
}

func checkedMulInt64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	product := a * b
	if product/b != a {
		return 0, false
	}
	return product, true
}

// integerFitsInType reports whether value is within the valid range for the
// given concrete integer type. Returns true for types that are not bounded
// integers (e.g. floats) so callers can safely call it unconditionally.
func integerFitsInType(value int64, name types.PrimitiveTypeName) bool {
	switch name {
	case types.Int8:
		return value >= math.MinInt8 && value <= math.MaxInt8
	case types.Int16:
		return value >= math.MinInt16 && value <= math.MaxInt16
	case types.Int32:
		return value >= math.MinInt32 && value <= math.MaxInt32
	case types.Int64:
		// IntegerLiteralExpr.Value is int64, so any stored value already fits.
		return true
	case types.UInt8:
		return value >= 0 && value <= math.MaxUint8
	case types.UInt16:
		return value >= 0 && value <= math.MaxUint16
	case types.UInt32:
		return value >= 0 && value <= math.MaxUint32
	case types.UInt64:
		// Values > math.MaxInt64 cannot be stored in int64 by the parser, so
		// the only out-of-range case we can catch is a negative value — which
		// the assignability check already rejects via UntypedSignedInt. Always
		// return true here to avoid a double-error.
		return true
	}
	return true
}

// checkIntegerLiteralRange emits an error when expr is a compile-time integer
// constant that does not fit in the concrete integer type targetType. It is a
// no-op when expr is not a literal constant or targetType is not a concrete
// integer. The variable name is used in the error message.
func (tc *TypeChecker) checkIntegerLiteralRange(varName string, expr ast.Expression, targetType types.Type) {
	toP, ok := targetType.(types.PrimitiveType)
	if !ok || !isAnyConcreteInt(toP.Name) {
		return
	}
	value, isLiteral := extractIntLiteralValue(expr)
	if !isLiteral {
		return
	}
	if !integerFitsInType(value, toP.Name) {
		tc.addError(expr.GetLocation(), SeverityError,
			"%s: literal value %d overflows %s", varName, value, toP.Name)
	}
}
