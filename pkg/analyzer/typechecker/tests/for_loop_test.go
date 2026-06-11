package typechecker_test

import (
	"testing"
)

// ── For loop condition must be bool ──────────────────────────────────────────
// The Lyra grammar restricts the for loop condition to boolean_expr, so the
// condition itself is always syntactically boolean. The typechecker validates
// that the operands of &&/|| are actually bool, catching mistakes like using
// an int or string variable where a bool is required.
//
// When a for loop has an init clause (e.g. `for var i = 0; i < 10; ...`),
// the init variable lives in a loop-local scope that the typechecker's scope
// table cannot reach (because ForLoopExpr.Body is stored by value, not
// pointer, so the scope table entry doesn't match). Condition checking is
// therefore skipped for init-clause loops to avoid false "undefined
// identifier" errors; tracking issue for the scope table fix is open.

func TestTypeCheck_ForLoop_InfiniteLoop_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `for { }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ForLoop_BoolVarCondition_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let done: bool = false
		for done == false { }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ForLoop_WithInitAndPost_NoError(t *testing.T) {
	// Init-clause loops are not condition-checked (see above), so no errors.
	res := parseCollectAndCheck(t, `for var i = 0; i < 10; i += 1 { }`, false)
	assertNoErrors(t, res)
}

// A for loop with && whose left operand is int (not bool) should error.
func TestTypeCheck_ForLoop_AndWithIntOperand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: int = 1
		let b: bool = true
		for a && b { }
	`, false)
	assertErrorsAre(t, res, "operator &&: operands must both be boolean, got int and boolean")
}

// A for loop with || whose right operand is a string should error.
func TestTypeCheck_ForLoop_OrWithStringOperand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: bool = true
		let s: string = "x"
		for a || s { }
	`, false)
	assertErrorsAre(t, res, "operator ||: operands must both be boolean, got boolean and string")
}

