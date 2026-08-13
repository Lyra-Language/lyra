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

// A **conversion** in a const initializer gets the fix, not the category (08/13).
//
// `const A = u8(200)` was reported as "a function call is not constant" — true of
// the mechanism, since a conversion is spelled as a call, and useless as advice: it
// named neither what was wrong nor what to do. The type belongs in an annotation,
// which is supported and is what the message now shows.
func TestConst_Conversion_SuggestsAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `const A = u8(200)`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: a conversion is not constant — "+
			"write the type as an annotation instead: `const A: u8 = ...`")
}

// The suggested spelling is the one that actually works, which is the point of
// naming it.
func TestConst_AnnotatedInsteadOfConverted_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `const A: u8 = 200
const B: f64 = 1.5`, false))
}

// Every conversion spelling reports its own target, including the identity forms
// that exist only as newtype read-outs.
func TestConst_ConversionNamesItsTarget(t *testing.T) {
	res := parseCollectAndCheck(t, `const S = string(7)`, false)
	assertHasErrorContaining(t, res,
		"a conversion is not constant — write the type as an annotation instead: `const S: string = ...`")
}

// An ordinary call keeps the ordinary message: it is not a conversion and there is
// no annotation that would rescue it.
func TestConst_RealFunctionCallKeepsItsMessage(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (n: i64) -> i64 => n
const X = f(3)`, false)
	assertErrorsAre(t, res,
		"`const` initializer must be a compile-time constant: a function call is not constant")
}
