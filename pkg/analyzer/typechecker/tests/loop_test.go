package typechecker_test

import (
	"testing"
)

// ── A while condition must be bool ───────────────────────────────────────────
// The grammar admits any `_bool_operand` as of 08/06 — a bare name, a call, a
// member access, as well as a `boolean_expr` — so bool-ness is entirely the
// typechecker's to enforce, and `while n { }` over an i64 is a type error rather
// than a syntax error. It restricted the condition to `boolean_expr` until then,
// which made `while done { }` unwritable and `while done == true { }` the workaround.
//
// A loop is checked inside its own scope (checkLoopExpr enters the scope the
// collector registered on the loop node).

func TestTypeCheck_Loop_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `loop { }`, false)
	assertNoErrors(t, res)
}

// `while true` warns and names the spelling that does not: `loop { }` is the infinite
// loop (and the one that is `never` — TestTailLoop_WhileTrueIsRefused).
func TestTypeCheck_While_LiteralTrue_WarnsWithLoop(t *testing.T) {
	res := parseCollectAndCheck(t, `while true { }`, false)
	assertWarningsAre(t, res,
		"condition is always true: write `loop { … }` for a loop that runs until it breaks")
	assertSingleWarningWithCode(t, res, "lyra-W027")
}

func TestTypeCheck_While_LiteralFalse_Warns(t *testing.T) {
	res := parseCollectAndCheck(t, `while false { }`, false)
	assertWarningsAre(t, res, "condition is always false: the loop body never runs")
	assertSingleWarningWithCode(t, res, "lyra-W027")
}

func TestTypeCheck_While_BoolVarCondition_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let done: bool = false
		while done == false { }
	`, false)
	assertNoErrors(t, res)
}

// The form the widening was for: a bool binding is the condition on its own, with
// no comparison to spell it. `while done == true { }` was the workaround, and it is
// not something anyone writes by choice.
func TestTypeCheck_While_BareBoolBinding_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let done: bool = false
		while done { }
	`, false)
	assertNoErrors(t, res)
}

// A call and a member access are conditions too — the operand set is
// `_bool_operand`, so anything postfix reaches it, and each is checked for
// bool-ness like any other.
func TestTypeCheck_While_BareCallAndMemberConditions_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Cfg { enabled: bool }
		let ready = (n: i64) -> bool => n < 3
		let go = (c: Cfg) -> void => {
			while ready(1) { }
			while c.enabled { }
		}
	`, false)
	assertNoErrors(t, res)
}

// The guard on the widening: admitting a bare operand must not admit a
// *non-bool* one silently. Each of these was a syntax error before 08/06 and is
// now a type error naming the type it got, which is the better diagnostic and the
// reason the check has to be real.
func TestTypeCheck_While_BareNonBoolCondition_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 0
		let s: string = "hi"
		while n { }
		while s { }
	`, false)
	assertErrorsAre(t, res,
		"while condition must be boolean, got i64",
		"while condition must be boolean, got string")
}

// A while with && whose left operand is i64 (not bool) should error.
func TestTypeCheck_While_AndWithIntOperand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i64 = 1
		let b: bool = true
		while a && b { }
	`, false)
	assertErrorsAre(t, res, "operator &&: operands must both be boolean, got i64 and boolean")
}

// A while with || whose right operand is a string should error.
func TestTypeCheck_While_OrWithStringOperand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: bool = true
		let s: string = "x"
		while a || s { }
	`, false)
	assertErrorsAre(t, res, "operator ||: operands must both be boolean, got boolean and string")
}

// ── loop-body locals ─────────────────────────────────────────────────────────
//
// A `let`/`var` declared inside a loop body is visible there. It was not: the
// collector puts body-locals in a child block scope keyed on the body block, and
// both loop nodes held that block *by value*, so the copy had a different address
// than the scope was keyed on. enterScope missed — silently, since a miss just
// runs in the enclosing scope — and the body was checked in the loop scope, where
// those names were never defined.

func TestTypeCheck_While_BodyLocalResolves(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var total = 0
		  var i = 0
		  while i < 3 {
		    let doubled = i * 2
		    total = total + doubled
		    i += 1
		  }
		  u8(total)
		}
	`, false))
}

func TestTypeCheck_ForInLoop_BodyLocalResolves(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var total = 0
		  for x in [1, 2, 3] {
		    let doubled = x * 2
		    total = total + doubled
		  }
		  u8(total)
		}
	`, false))
}

// The body's scope chains outward, so a body-local can read an enclosing binding.
func TestTypeCheck_While_BodyLocalReadsOuterBinding(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  let base = 10
		  var total = 0
		  var i = 0
		  while i < 3 {
		    let scaled = base + i
		    total = total + scaled
		    i += 1
		  }
		  u8(total)
		}
	`, false))
}

// A body-local stays inside the body: reading one after the loop is still an error.
func TestTypeCheck_While_BodyLocalDoesNotEscape(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var i = 0
		  while i < 3 {
		    let doubled = i * 2
		    i += 1
		  }
		  u8(doubled)
		}
	`, false)
	assertErrorsAre(t, res, `undefined identifier "doubled"`)
}

// A loop body has no value, so its last statement is not in value position — a
// one-armed `if` there is a conditional side effect, not a valueless expression.
func TestTypeCheck_While_OneArmedIfAsLastStatement(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var n = 0
		  var i = 0
		  while i < 3 {
		    i += 1
		    if i > 1 { n = n + 1 }
		  }
		  u8(n)
		}
	`, false))
}

// The same for a for-in body.
func TestTypeCheck_ForInLoop_OneArmedIfAsLastStatement(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var n = 0
		  for x in [1, 2, 3] {
		    if x > 1 { n = n + 1 }
		  }
		  u8(n)
		}
	`, false))
}
