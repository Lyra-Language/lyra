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
