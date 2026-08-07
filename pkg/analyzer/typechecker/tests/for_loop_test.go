package typechecker_test

import (
	"testing"
)

// ── For loop condition must be bool ──────────────────────────────────────────
// The grammar admits any `_bool_operand` as of 08/06 — a bare name, a call, a
// member access, as well as a `boolean_expr` — so bool-ness is entirely the
// typechecker's to enforce, and `for n { }` over an i64 is a type error rather
// than a syntax error. It restricted the condition to `boolean_expr` until then,
// which made `for done { }` unwritable and `for done == true { }` the workaround.
//
// A for loop is now checked inside its own scope (checkForLoopExpr enters the
// scope the collector registered on the loop node), so an init clause's variable
// resolves in the condition, post, and body — including for the three-clause
// `for var i = 0; i < 10; i += 1` form. (Known limitation: a `let`/`var` declared
// *inside* the body still doesn't resolve there; see lyra/todo.md.)

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

// The form the widening was for: a bool binding is the condition on its own, with
// no comparison to spell it. `for done == true { }` was the workaround, and it is
// not something anyone writes by choice.
func TestTypeCheck_ForLoop_BareBoolBinding_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let done: bool = false
		for done { }
	`, false)
	assertNoErrors(t, res)
}

// A call and a member access are conditions too — the operand set is
// `_bool_operand`, so anything postfix reaches it, and each is checked for
// bool-ness like any other.
func TestTypeCheck_ForLoop_BareCallAndMemberConditions_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Cfg { enabled: bool }
		let ready = (n: i64) -> bool => n < 3
		let go = (c: Cfg) -> void => {
			for ready(1) { }
			for c.enabled { }
		}
	`, false)
	assertNoErrors(t, res)
}

// The guard on the widening: admitting a bare operand must not admit a
// *non-bool* one silently. Each of these was a syntax error before 08/06 and is
// now a type error naming the type it got, which is the better diagnostic and the
// reason the check has to be real.
func TestTypeCheck_ForLoop_BareNonBoolCondition_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 0
		let s: string = "hi"
		for n { }
		for s { }
	`, false)
	assertErrorsAre(t, res,
		"for loop condition must be boolean, got i64",
		"for loop condition must be boolean, got string")
}

func TestTypeCheck_ForLoop_WithInitAndPost_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `for var i = 0; i < 10; i += 1 { }`, false)
	assertNoErrors(t, res)
}

// The three-clause form's init variable resolves in the body: `s = s + i` reads
// `i`, which used to be "undefined identifier i" before the loop-scope fix.
func TestTypeCheck_ForLoop_ThreeClause_InitVarVisibleInBody(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var s = 0
		for var i = 0; i < 10; i += 1 {
			s = s + i
		}
	`, false)
	assertNoErrors(t, res)
}

// The init-clause condition is now actually type-checked (it was skipped before
// the scope fix): a non-bool `&&` operand — here the i64 loop variable `i`, which
// must first resolve to be reported — is an error, proving both that the
// condition is checked and that the init variable is in scope.
func TestTypeCheck_ForLoop_InitClauseConditionChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `for var i = 0; i && true; i += 1 { }`, false)
	assertErrorsAre(t, res, "operator &&: operands must both be boolean, got i64 and boolean")
}

// A for loop with && whose left operand is i64 (not bool) should error.
func TestTypeCheck_ForLoop_AndWithIntOperand_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i64 = 1
		let b: bool = true
		for a && b { }
	`, false)
	assertErrorsAre(t, res, "operator &&: operands must both be boolean, got i64 and boolean")
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

// ── loop-body locals ─────────────────────────────────────────────────────────
//
// A `let`/`var` declared inside a loop body is visible there. It was not: the
// collector puts body-locals in a child block scope keyed on the body block, and
// both loop nodes held that block *by value*, so the copy had a different address
// than the scope was keyed on. enterScope missed — silently, since a miss just
// runs in the enclosing scope — and the body was checked in the loop scope, where
// those names were never defined.

func TestTypeCheck_ForLoop_BodyLocalResolves(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var total = 0
		  for var i = 0; i < 3; i += 1 {
		    let doubled = i * 2
		    total = total + doubled
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
func TestTypeCheck_ForLoop_BodyLocalReadsOuterBinding(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  let base = 10
		  var total = 0
		  for var i = 0; i < 3; i += 1 {
		    let scaled = base + i
		    total = total + scaled
		  }
		  u8(total)
		}
	`, false))
}

// A body-local stays inside the body: reading one after the loop is still an error.
func TestTypeCheck_ForLoop_BodyLocalDoesNotEscape(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  for var i = 0; i < 3; i += 1 {
		    let doubled = i * 2
		  }
		  u8(doubled)
		}
	`, false)
	assertErrorsAre(t, res, `undefined identifier "doubled"`)
}

// A loop body has no value, so its last statement is not in value position — a
// one-armed `if` there is a conditional side effect, not a valueless expression.
func TestTypeCheck_ForLoop_OneArmedIfAsLastStatement(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let main = () -> u8 => {
		  var n = 0
		  for var i = 0; i < 3; i += 1 {
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
