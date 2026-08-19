package typechecker_test

import (
	"testing"
)

// Equality operators
func TestTypeCheck_Equality_TwoIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5 == 5", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_TwoFloatLiterals_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 == 5.0", false)
	assertWarningsAre(t, res, "operator ==: comparing float values with == or != may give unexpected results due to floating-point precision")
}

func TestTypeCheck_Inequality_TwoIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5 != 6", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Inequality_TwoFloatLiterals_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 != 6.0", false)
	assertWarningsAre(t, res, "operator !=: comparing float values with == or != may give unexpected results due to floating-point precision")
}

func TestTypeCheck_Equality_IntAndFloatLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5 == 5.0", false)
	assertErrorsAre(t, res, "operator ==: incompatible types: integer literal and float literal")
}

func TestTypeCheck_Equality_FloatAndIntLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 == 5", false)
	assertErrorsAre(t, res, "operator ==: incompatible types: float literal and integer literal")
}

func TestTypeCheck_Inequality_IntAndFloatLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5 != 6.0", false)
	assertErrorsAre(t, res, "operator !=: incompatible types: integer literal and float literal")
}

func TestTypeCheck_Inequality_FloatAndIntLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 != 6", false)
	assertErrorsAre(t, res, "operator !=: incompatible types: float literal and integer literal")
}

// Equality - concrete typed identifiers
func TestTypeCheck_Equality_SameConcreteInt_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: i32 = 2
		a == b`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_SameConcreteFloat_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: f64 = 1.0
		let b: f64 = 2.0
		a == b`, false)
	assertWarningsAre(t, res, "operator ==: comparing float values with == or != may give unexpected results due to floating-point precision")
}

// Untyped i64 should widen to the concrete type on the other side, just as it
// does for ordering operators and arithmetic. The equality checker currently
// has a bug where it uses strict Go interface equality after the numeric guard,
// so this test is expected to fail until that is fixed.
func TestTypeCheck_Equality_UntypedIntAndConcreteInt_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		a == 5`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_DifferentConcreteInts_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: i64 = 2
		a == b`, false)
	assertErrorsAre(t, res, "operator ==: incompatible types: i32 and i64")
}

func TestTypeCheck_Equality_ConcreteIntAndConcreteFloat_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: f64 = 2.0
		a == b`, false)
	assertErrorsAre(t, res, "operator ==: incompatible types: i32 and f64")
}

// Equality - non-numeric primitive types
func TestTypeCheck_Equality_TwoBoolLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "true == false", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_TwoStringLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `"hello" == "world"`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_TwoCharLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "'a' == 'b'", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_StringAndInt_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"hello" == 5`, false)
	assertErrorsAre(t, res, "operator ==: incompatible types: string and integer literal")
}

func TestTypeCheck_Equality_BoolAndInt_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "true == 5", false)
	assertErrorsAre(t, res, "operator ==: incompatible types: boolean and integer literal")
}

// Inequality - concrete typed identifiers
func TestTypeCheck_Inequality_SameConcreteInt_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: i32 = 2
		a != b`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Inequality_TwoBoolLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "true != false", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Inequality_DifferentConcreteInts_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: i64 = 2
		a != b`, false)
	assertErrorsAre(t, res, "operator !=: incompatible types: i32 and i64")
}

// Comparison operators
func TestTypeCheck_Comparison_IntAndIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "6 > 5", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_IntAndFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "6 < 5.0", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_ConcreteIntAndFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let i: i32 = 6
		i < 5.0`, false)
	assertErrorsAre(t, res, "operator <: incompatible types: i32 and float literal")
}

func TestTypeCheck_Comparison_TwoNonNumericOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a" > true`, false)
	assertErrorsAre(t, res, "operator >: operands must be numeric or implement Ord, got string and boolean")
}

func TestTypeCheck_Comparison_OneNonNumericOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a" < 5`, false)
	assertErrorsAre(t, res, "operator <: operands must be numeric or implement Ord, got string and integer literal")
}

// <= and >= operators (previously untested)
func TestTypeCheck_Comparison_LessThanOrEqual_TwoIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5 <= 6", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_GreaterThanOrEqual_TwoIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "6 >= 5", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_LessThanOrEqual_IntAndFloatLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 5
		a <= 3.0`, false)
	assertErrorsAre(t, res, "operator <=: incompatible types: i32 and float literal")
}

// Float literal operands
func TestTypeCheck_Comparison_TwoFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 > 3.0", false)
	assertNoErrors(t, res)
}

// Concrete typed identifiers
func TestTypeCheck_Comparison_SameConcreteInt_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 5
		let b: i32 = 3
		a > b`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_SameConcreteFloat_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: f64 = 5.0
		let b: f64 = 3.0
		a >= b`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_UntypedIntAndConcreteInt_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 5
		a > 3`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Comparison_DifferentConcreteInts_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 5
		let b: i64 = 3
		a > b`, false)
	assertErrorsAre(t, res, "operator >: incompatible types: i32 and i64")
}

// Non-numeric types are always invalid for ordering operators
func TestTypeCheck_Comparison_BoolOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "true > false", false)
	assertErrorsAre(t, res, "operator >: operands must be numeric or implement Ord, got boolean and boolean")
}

func TestTypeCheck_Comparison_StringOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a" < "b"`, false)
	assertErrorsAre(t, res, "operator <: operands must be numeric or implement Ord, got string and string")
}

// **Two untyped integers of different signedness compare.** A negative literal is
// `untyped_signed_int` and a non-negative one is `untyped_int`; neither is assignable to
// the other, because assignability widens an untyped type to a *concrete* one and says
// nothing about two placeholders. Both pin to the same concrete type in the end.
//
// It is reachable from a loop, not from a hand-written literal pair — a range with a
// negative bound gives its counter the signed untyped type:
//
//	for d in -1..<=1 { if d != 0 { … } }
//
// and there is no annotation to reach for, since `for d: i64 in …` is a syntax error. `<`
// accepted the pair through numericResultType all along, so this brings equality into
// line with ordering rather than inventing a rule.
func TestTypeCheck_Equality_UntypedSignedAndUnsignedLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let main = () -> void => {
		  for d in -1..<=1 {
		    if d != 0 { println("${d}") }
		  }
		}`, false)
	assertNoErrors(t, res)
}

// The same pair under `==`, and written directly rather than through a loop counter.
func TestTypeCheck_Equality_NegativeAndNonNegativeLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let main = () -> void => {
		  var d = -1
		  if d == 0 { println("zero") }
		}`, false)
	assertNoErrors(t, res)
}

// The widening this does *not* do. Equality's refusal of int/float mixing is deliberate,
// and the numeric rule ordering uses is wider in that direction (`5 < 5.0` compiles), so
// the untyped-integer case is admitted on its own rather than by adopting that rule
// wholesale.
func TestTypeCheck_Equality_StillRejectsIntAgainstFloat(t *testing.T) {
	res := parseCollectAndCheck(t, "5 == 5.0", false)
	assertErrorsAre(t, res, "operator ==: incompatible types: integer literal and float literal")
}
