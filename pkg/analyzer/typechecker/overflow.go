package typechecker

import (
	"math"
	"math/big"

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
// It delegates to `ast.FoldIntExpr`, which is the same walk: the backend needs the
// identical answer for an array-repeat count, and two copies of constant folding is the
// kind of divergence that shows up as an array of one length to the checker and another
// to codegen.
func extractIntLiteralValue(expr ast.Expression) (int64, bool) {
	return ast.FoldIntExpr(expr)
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

// signedTypeMinMagnitude returns the positive magnitude of a bounded signed
// integer type's minimum value — 2^(bits-1), e.g. 128 for i8 (min -128), 32768
// for i16, 2147483648 for i32 — and true when name is one of i8/i16/i32. This
// is the magnitude a *negated* literal must have to denote that type's minimum
// (`-128` → i8 min): it overflows the type as a positive literal but is exactly
// representable once negated. i64 is excluded — its min magnitude (2^63)
// overflows int64, and that case is handled separately in inferNegationExpr via
// the collector's Unsigned literal path.
func signedTypeMinMagnitude(name types.PrimitiveTypeName) (int64, bool) {
	switch name {
	case types.Int8:
		return -int64(math.MinInt8), true // 128
	case types.Int16:
		return -int64(math.MinInt16), true // 32768
	case types.Int32:
		return -int64(math.MinInt32), true // 2147483648
	}
	return 0, false
}

// checkIntegerLiteralRange emits an error when expr is a compile-time integer
// constant that does not fit in the concrete integer type targetType. It is a
// no-op when expr is not a literal constant or targetType is not a concrete
// integer. The variable name is used in the error message.
func (tc *TypeChecker) checkIntegerLiteralRange(varName string, expr ast.Expression, targetType types.Type) {
	// A newtype is checked against its base — a Percent value is a u8 and cannot
	// hold 300 either. Skipped when the newtype carries a range constraint, since
	// that constraint is a subset of the base and checkRangeConstraints already
	// reports the violation (reporting both would double up on one mistake).
	if ct, ok := targetType.(*types.ConstrainedType); ok {
		if hasRangeConstraint(ct) {
			return
		}
		targetType = tc.resolveTypeIfKnown(ct.Type, expr.GetLocation())
	}
	toP, ok := targetType.(types.PrimitiveType)
	if !ok || !isAnyConcreteInt(toP.Name) {
		return
	}
	// A **wide** constant does not fold to an int64, so it is range-checked against the
	// target's bounds in big.Int arithmetic instead. Without this a 128-bit magnitude
	// assigned to a `u8` would pass unchecked — it stays untyped where both 128-bit
	// types could hold it, so assignability has nothing to object to either.
	//
	// It folds the whole *expression*, not just a bare literal: `10^20 + 1` has no int64
	// to fold through, so until 08/08 the int64 walk declined and the value reached the
	// backend unchecked — as invalid IR, since the operand had already been narrowed to
	// a width it does not fit. The bare-literal case was caught and the arithmetic one
	// was not, which is the sort of gap that reads as "the check works" until it does
	// not.
	if wide, ok := ast.FoldBigExpr(expr, nil); ok && !wide.IsInt64() {
		if !bigFitsInType(wide, toP.Name) {
			if tc.overflowReported[expr] {
				return
			}
			if tc.overflowReported == nil {
				tc.overflowReported = map[ast.Expression]bool{}
			}
			tc.overflowReported[expr] = true
			tc.addError(expr.GetLocation(), SeverityError,
				"%s: literal value %s overflows %s", varName, wide, toP.Name)
		}
		return
	}
	value, isLiteral := extractIntLiteralValue(expr)
	if !isLiteral {
		return
	}
	if !integerFitsInType(value, toP.Name) {
		// At most once per literal. A leaf can be narrowed by more than one context on
		// the way down — a struct field whose value is a tuple is narrowed by
		// stampAggregate and again by the enclosing declaration — and one literal that
		// is too large is one mistake however many times it is checked.
		if tc.overflowReported[expr] {
			return
		}
		if tc.overflowReported == nil {
			tc.overflowReported = map[ast.Expression]bool{}
		}
		tc.overflowReported[expr] = true
		tc.addError(expr.GetLocation(), SeverityError,
			"%s: literal value %d overflows %s", varName, value, toP.Name)
	}
}

// wideLiteralMagnitude is the magnitude of a >64-bit literal, optionally negated —
// the two shapes a 128-bit constant can take in source, since a literal is always
// written non-negative and a leading `-` is its own node.
func wideLiteralMagnitude(expr ast.Expression) (*big.Int, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		if e.IsWide() {
			return e.BigValue(), true
		}
	case *ast.NegationExpr:
		if inner, ok := e.Operand.(*ast.IntegerLiteralExpr); ok && inner.IsWide() {
			return new(big.Int).Neg(inner.BigValue()), true
		}
	}
	return nil, false
}

// bigFitsInType is integerFitsInType for a magnitude that does not fit an int64 — the
// same question at 128-bit precision.
//
// It is a *second* function rather than a widening of the first because the first is on
// the hot path for every ordinary literal and has an int64 in hand; this one is reached
// only by a literal that already needed a big.Int to exist. They must agree, which is
// why the bounds here are computed rather than written out.
func bigFitsInType(v *big.Int, name types.PrimitiveTypeName) bool {
	bits, signed, ok := intWidthOf(name)
	if !ok {
		return true
	}
	if signed {
		max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)), big.NewInt(1))
		min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)))
		return v.Cmp(min) >= 0 && v.Cmp(max) <= 0
	}
	if v.Sign() < 0 {
		return false
	}
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits)), big.NewInt(1))
	return v.Cmp(max) <= 0
}

// intWidthOf gives an integer type's bit width and signedness.
func intWidthOf(name types.PrimitiveTypeName) (bits int, signed bool, ok bool) {
	switch name {
	case types.Int8:
		return 8, true, true
	case types.Int16:
		return 16, true, true
	case types.Int32:
		return 32, true, true
	case types.Int64:
		return 64, true, true
	case types.Int128:
		return 128, true, true
	case types.UInt8:
		return 8, false, true
	case types.UInt16:
		return 16, false, true
	case types.UInt32:
		return 32, false, true
	case types.UInt64:
		return 64, false, true
	case types.UInt128:
		return 128, false, true
	}
	return 0, false, false
}
