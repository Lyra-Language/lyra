package typechecker_test

import (
	"testing"
)

// ── Range expression operand types ───────────────────────────────────────────

func TestTypeCheck_RangeExpr_IntLiterals_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `for i in 1..=10 { }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_RangeExpr_IntVars_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let start: i64 = 1
		let end: i64 = 5
		for i in start..=end { }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_RangeExpr_StringStart_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `for i in "a"..=10 { }`, false)
	assertErrorsAre(t, res, "range start must be numeric, got string")
}

func TestTypeCheck_RangeExpr_StringEnd_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `for i in 1..="z" { }`, false)
	assertErrorsAre(t, res, "range end must be numeric, got string")
}

func TestTypeCheck_RangeExpr_BoolStart_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `for i in true..=10 { }`, false)
	assertErrorsAre(t, res, "range start must be numeric, got boolean")
}

func TestTypeCheck_RangeExpr_BoolEnd_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `for i in 1..=true { }`, false)
	assertErrorsAre(t, res, "range end must be numeric, got boolean")
}

func TestTypeCheck_RangeExpr_IncompatibleOperands_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 0
		let b: f64 = 10.0
		for i in a..=b { }
	`, false)
	assertErrorsAre(t, res, "range operands have incompatible types: start is i32, end is f64")
}

func TestTypeCheck_RangeExpr_StringStart_Standalone_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `"a"..=10`, false)
	assertErrorsAre(t, res, "range start must be numeric, got string")
}

func TestTypeCheck_RangeExpr_StringEnd_Standalone_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `1..="z"`, false)
	assertErrorsAre(t, res, "range end must be numeric, got string")
}

func TestTypeCheck_RangeExpr_VarStringStart_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s: string = "hello"
		for i in s..=10 { }
	`, false)
	assertErrorsAre(t, res, "range start must be numeric, got string")
}

// ── For-in iterable must be iterable ─────────────────────────────────────────

func TestTypeCheck_ForIn_RangeIterable_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `for i in 1..=10 { }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ForIn_ArrayLiteralIterable_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `for x in [1, 2, 3] { }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ForIn_StringLiteralIterable_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `for c in "hello" { }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ForIn_StringVarIterable_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s: string = "hello"
		for c in s { }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ForIn_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `for x in 42 { }`, false)
	assertErrorsAre(t, res, "cannot iterate over integer literal: expected an array, string, or range")
}

func TestTypeCheck_ForIn_IntVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 5
		for x in n { }
	`, false)
	assertErrorsAre(t, res, "cannot iterate over i64: expected an array, string, or range")
}

func TestTypeCheck_ForIn_BoolVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let b: bool = true
		for x in b { }
	`, false)
	assertErrorsAre(t, res, "cannot iterate over boolean: expected an array, string, or range")
}

func TestTypeCheck_ForIn_FloatLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `for x in 3.14 { }`, false)
	assertErrorsAre(t, res, "cannot iterate over float literal: expected an array, string, or range")
}

// ── Range error causes for-in to still check iterability ────────────────────

func TestTypeCheck_ForIn_InvalidRange_EmitsRangeError(t *testing.T) {
	// A range with invalid operand types still emits a range error.
	// The for-in check sees nil iterType (start was cleared), so no double-error.
	res := parseCollectAndCheck(t, `for i in "a"..=10 { }`, false)
	assertErrorsAre(t, res, "range start must be numeric, got string")
}
