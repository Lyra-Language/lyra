package typechecker_test

import (
	"testing"
)

// Equality operators
func TestTypeCheck_Equality_TwoIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5 == 5", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_TwoFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 == 5.0", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Inequality_TwoIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5 != 6", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Inequality_TwoFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 != 6.0", false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_IntAndFloatLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5 == 5.0", false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ==: incompatible types: integer literal and float literal")
}

func TestTypeCheck_Equality_FloatAndIntLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 == 5", false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ==: incompatible types: float literal and integer literal")
}

func TestTypeCheck_Inequality_IntAndFloatLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5 != 6.0", false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator !=: incompatible types: integer literal and float literal")
}

func TestTypeCheck_Inequality_FloatAndIntLiterals_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "5.0 != 6", false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator !=: incompatible types: float literal and integer literal")
}

// Equality - concrete typed identifiers
func TestTypeCheck_Equality_SameConcreteInt_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: i32 = 2
		a == b`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Equality_SameConcreteFloat_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: f64 = 1.0
		let b: f64 = 2.0
		a == b`, false)
	assertNoErrors(t, res)
}

// Untyped int should widen to the concrete type on the other side, just as it
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
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ==: incompatible types: i32 and i64")
}

func TestTypeCheck_Equality_ConcreteIntAndConcreteFloat_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: f64 = 2.0
		a == b`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ==: incompatible types: i32 and f64")
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
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ==: incompatible types: string and integer literal")
}

func TestTypeCheck_Equality_BoolAndInt_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "true == 5", false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ==: incompatible types: boolean and integer literal")
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
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator !=: incompatible types: i32 and i64")
}

// Comparison operators
func TestTypeCheck_Comparison_IntAndIntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "6 > 5", false)
	assertErrorCount(t, res, 0)
}

func TestTypeCheck_Comparison_IntAndFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, "6 < 5.0", false)
	assertErrorCount(t, res, 0)
}

func TestTypeCheck_Comparison_ConcreteIntAndFloatLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let i: i32 = 6
		i < 5.0`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator <: incompatible types: i32 and float literal")
}

func TestTypeCheck_Comparison_TwoNonNumericOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a" > true`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator >: operands must be numeric, got string and boolean instead")
}

func TestTypeCheck_Comparison_OneNonNumericOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a" < 5`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator <: operands must be numeric, got string and integer literal instead")
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
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator <=: incompatible types: i32 and float literal")
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
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator >: incompatible types: i32 and i64")
}

// Non-numeric types are always invalid for ordering operators
func TestTypeCheck_Comparison_BoolOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, "true > false", false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator >: operands must be numeric, got boolean and boolean instead")
}

func TestTypeCheck_Comparison_StringOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a" < "b"`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator <: operands must be numeric, got string and string instead")
}
