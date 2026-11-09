package typechecker

import (
	"math/big"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// fixedLiteralValue answers the exact value of a literal, an integer or a float, negated or
// not, for measuring it against a fixed format.
func fixedLiteralValue(expr ast.Expression) (*big.Rat, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		if e.IsWide() {
			return new(big.Rat).SetInt(e.BigValue()), true
		}
		if e.Unsigned {
			return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(e.Value))), true
		}
		return new(big.Rat).SetInt64(e.Value), true
	case *ast.FloatLiteralExpr:
		value := new(big.Rat).SetFloat64(e.Value)
		return value, value != nil
	case *ast.NegationExpr:
		if inner, ok := fixedLiteralValue(e.Operand); ok {
			return inner.Neg(inner), true
		}
	}
	return nil, false
}

// isUntypedNumericOrFixed reports a type a fixed value orders against: its own, by the
// caller's numericResultType, or a literal adopting it.
func isUntypedNumericOrFixed(t types.Type) bool {
	if _, fixed := types.AsFixed(t); fixed {
		return true
	}
	p, ok := t.(types.PrimitiveType)
	return ok && isUntypedNumericLiteral(p.Name)
}

// fixedMethodSignature answers the methods a fixed value has of its own. Each answers its
// storage integer, the one type sure to hold the result whatever the format:
//
//   - `floor`, `ceil` and `round` drop the fraction, downward, upward, and to the nearest
//     with a half going up, as a multiply rounds;
//   - `to_bits` reads the stored integer out, unscaled.
func fixedMethodSignature(recv types.PrimitiveType, name string) (*types.LambdaType, bool) {
	format, ok := types.FixedFormatOf(recv.Name)
	if !ok {
		return nil, false
	}
	switch name {
	case "floor", "ceil", "round", "to_bits":
		return &types.LambdaType{
			ReturnType: types.ReturnType{Type: types.PrimitiveType{Name: format.Storage()}},
		}, true
	}
	return nil, false
}

// isFixedOverflowMethod reports the overflow-arithmetic builtins a fixed value has: every one
// over add, sub, mul and div. Those that are bit-level or are about a remainder have no
// fixed-point reading.
func isFixedOverflowMethod(recv types.PrimitiveType, name string) bool {
	if _, ok := types.FixedFormatOf(recv.Name); !ok {
		return false
	}
	switch name {
	case "wrapping_add", "wrapping_sub", "wrapping_mul",
		"saturating_add", "saturating_sub", "saturating_mul",
		"checked_add", "checked_sub", "checked_mul", "checked_div":
		return true
	}
	return false
}

// checkFixedConversion answers `fixed<I, F>(x)` and a conversion from a fixed value, reporting
// what is refused, and whether it was one of those.
//
// **What can change a value's whole part is refused here**, for the three methods whose names
// say what should happen to it (narrowing.go). What can lose only precision is allowed and
// rounds to nearest, as `f32(x)` does, and a value at the very top that rounds up past the
// format traps. An integer or a float is checked at run time, since a conversion names the
// target outright; an integer out of range traps and so does a float, a NaN included.
func (tc *TypeChecker) checkFixedConversion(call *ast.FunctionCallExpr, name string, from, to types.Type) bool {
	toFormat, toFixed := types.AsFixed(to)
	fromFormat, fromFixed := types.AsFixed(from)
	if !toFixed && !fromFixed {
		return false
	}
	if !toFixed {
		if !isFloatType(to) {
			tc.addError(call.GetLocation(), SeverityError,
				"cannot convert %s to %s: an integer comes from `.floor()`, `.ceil()`, `.round()` or, for the stored bits, `.to_bits()`",
				from, name)
		}
		return true
	}
	if fromFixed {
		if toFormat.Integer < fromFormat.Integer {
			tc.addError(call.GetLocation(), SeverityError,
				"cannot convert %s to %s: its whole part may not fit, so say what should happen: `.narrow::<%s>()`, `.saturate::<%s>()` or `.truncate::<%s>()`",
				from, name, name, name, name)
		}
		return true
	}
	if !isIntType(from) && !isFloatType(from) {
		tc.addError(call.GetLocation(), SeverityError, "cannot convert %s to %s", from, name)
		return true
	}
	// A literal becomes the value it names here, at compile time, and must fit as any
	// literal must.
	tc.propagateExpectedType(call.Arguments[0], to)
	tc.checkLiteralRange(name, call.Arguments[0], to)
	return true
}

// inferFixedFromBits answers `fixed<8, 8>.from_bits(n)`, reporting whether the call was one.
// The stored integer is the only source that needs no scaling, and the only one whose type
// says so: `n` must be the format's storage integer, as `to_bits` answers.
func (tc *TypeChecker) inferFixedFromBits(member *ast.MemberExpr, call *ast.FunctionCallExpr) (types.Type, bool) {
	head, ok := member.Object.(*ast.IdentifierExpr)
	if !ok {
		return nil, false
	}
	target := types.PrimitiveType{Name: types.PrimitiveTypeName(head.Name)}
	format, ok := types.AsFixed(target)
	if !ok {
		return nil, false
	}
	if member.Property.Name != "from_bits" || len(call.Arguments) != 1 {
		tc.addError(member.GetLocation(), SeverityError,
			"%s has one associated function, `from_bits(n)`, and a value is converted with `%s(x)`", target, target)
		return nil, true
	}
	storage := types.PrimitiveType{Name: format.Storage()}
	tc.typeTable.Set(head, target)
	tc.typeTable.Set(member, &types.LambdaType{
		Parameters: []types.ParameterType{{Type: storage}},
		ReturnType: types.ReturnType{Type: target},
	})
	tc.methodTable.SetBuiltinMethod(call, false, false)
	argType := tc.inferExprType(call.Arguments[0])
	if argType == nil {
		return target, true
	}
	tc.propagateExpectedType(call.Arguments[0], storage)
	tc.checkLiteralRange("from_bits", call.Arguments[0], storage)
	if settled, ok := tc.typeTable.Get(call.Arguments[0]); !ok || !isAssignable(settled, storage) {
		tc.addError(call.Arguments[0].GetLocation(), SeverityError,
			"from_bits on %s takes its storage integer, %s, and got %s", target, storage, argType)
	}
	return target, true
}
