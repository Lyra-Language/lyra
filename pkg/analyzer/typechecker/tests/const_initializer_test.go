package typechecker_test

import "testing"

// --- constant initializers: no errors ---

func TestConst_IntLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `const X = 42`, false)
	assertNoErrors(t, res)
}

func TestConst_StringLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `const NAME = "lyra"`, false)
	assertNoErrors(t, res)
}

func TestConst_BoolLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `const FLAG = true`, false)
	assertNoErrors(t, res)
}

func TestConst_ArithmeticOfLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `const SIZE = 4 * 8 + 2`, false)
	assertNoErrors(t, res)
}

func TestConst_NegationOfLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `const NEG = -5`, false)
	assertNoErrors(t, res)
}

func TestConst_BooleanExpr(t *testing.T) {
	res := parseCollectAndCheck(t, `const OK = 1 < 2 && true`, false)
	assertNoErrors(t, res)
}

func TestConst_StringConcat(t *testing.T) {
	res := parseCollectAndCheck(t, `const GREETING = "hi " ++ "there"`, false)
	assertNoErrors(t, res)
}

func TestConst_ArrayOfLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `const NUMS = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

func TestConst_ReferenceAnotherConst(t *testing.T) {
	res := parseCollectAndCheck(t, `const BASE = 10
const DERIVED = BASE * 2`, false)
	assertNoErrors(t, res)
}

// --- non-constant initializers: errors ---

func TestConst_FunctionCall_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let compute = () => 42
const X = compute()`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: a function call is not constant")
}

func TestConst_NonConstVariable_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let y = 5
const X = y`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: variable `y` is not constant")
}

func TestConst_NonConstInArithmetic_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let y = 5
const X = 2 + y`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: variable `y` is not constant")
}

func TestConst_NonConstInArray_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let y = 5
const NUMS = [1, y, 3]`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: variable `y` is not constant")
}

// A **conversion of a constant operand is constant** (08/17).
//
// It is spelled as a call, which is why it used to land in the non-constant arm and be
// refused with advice to write the type as an annotation instead. The annotation still
// works and is often nicer for a bare literal, but it cannot express a conversion *inside*
// a larger expression — `X_LEN * ASPECT * f64(HEIGHT) / f64(WIDTH)` has no annotation that
// rescues it, which is what made that advice a workaround rather than an answer.
//
// Accepting it is safe because a `const` is inlined as its value *expression* and lowered
// like any other code, so nothing about what the program computes changes.
func TestConst_ConversionOfAConstantIsConstant(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `const A = u8(200)
const B = f64(5) + 1.0`, false))
}

// The motivating case: a conversion nested in arithmetic, which no annotation can express.
func TestConst_ConversionInsideArithmetic(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `const WIDTH = 125
const HEIGHT = 57
const ASPECT = 2.0
const X_LEN = 2.47
const Y_LEN = X_LEN * ASPECT * f64(HEIGHT) / f64(WIDTH)`, false))
}

// The annotation spelling keeps working; the two are alternatives now rather than a
// refusal and its workaround.
func TestConst_AnnotatedInsteadOfConverted_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `const A: u8 = 200
const B: f64 = 1.5`, false))
}

// A conversion of a *runtime* value is still refused, and names the operand rather than
// the conversion — the walk recurses into it, so the offender reported is the thing that
// is actually not constant.
func TestConst_ConversionOfAVariableNamesTheVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 3
const A = u8(x)`, false)
	assertHasErrorContaining(t, res, "variable `x` is not constant")
}

// A conversion that is a genuine *type* error now reports as one. The blanket "not
// constant" refusal used to mask these: `string(...)` is identity-only and `i64` of a
// float is lossy, and both said nothing about either.
func TestConst_BadConversionReportsTheRealError(t *testing.T) {
	res := parseCollectAndCheck(t, `const S = string(7)`, false)
	assertHasErrorContaining(t, res, "only reads a value of that type")
	res2 := parseCollectAndCheck(t, `const C = i64(2.5)`, false)
	assertHasErrorContaining(t, res2, "use floor(), ceil(), or round()")
}

// An ordinary call keeps the ordinary message: it is not a conversion and there is
// no annotation that would rescue it.
func TestConst_RealFunctionCallKeepsItsMessage(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (n: i64) -> i64 => n
const X = f(3)`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: a function call is not constant")
}
