package typechecker_test

import (
	"testing"
)

func TestMatchExpr_ArmsHaveSameType(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => "one",
		2 => "two",
		3 => "three",
		_ => "other",
	}`, false)
	assertNoErrors(t, res)
}

func TestMatchExpr_ArmsHaveSameTypeWithBlocks(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => { "one" },
		2 => { "two" },
		3 => { "three" },
		_ => { "other" },
	}`, false)
	assertNoErrors(t, res)
}

func TestMatchExpr_ArmsHaveDifferentTypes(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => "one",
		2 => 2,
		3 => "three",
		_ => "other",
	}`, false)
	assertErrorsAre(t, res, "match arms have incompatible types: string vs i64")
}

func TestMatchExpr_ArmsHaveDifferentTypesWithBlocks(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => { "one" },
		2 => { false },
		3 => { "three" },
		_ => { "other" },
	}`, false)
	assertErrorsAre(t, res, "match arms have incompatible types: string vs boolean")
}

// A `match` used as a statement checks its arms individually and does not require their
// types to agree — the policy checkIfExpr has always applied with requireType=false.
//
// Withholding it here made a one-armed `if` illegal as an arm body, refused with "`if`
// used as a value must have an `else` branch" while naming a value nobody wanted. The
// loop-body form of exactly this was fixed when checkBlockForEffect was introduced; a
// match arm in statement position never got it, which is the fourth member of the
// "block value vs statement" family. Found writing examples/tui_events.lyra, where
// dispatching on a key is the natural shape.

func TestMatch_StatementArmAllowsAOneArmedIf(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => println("f")
		let g = (n: i64) -> void => {
			match n { 0 => { if n == 0 { f() } }, _ => { } }
		}
	`, false)
	assertNoErrors(t, res)
}

// The unbraced form of the same arm body, since the brace was the whole difference in
// the sibling W006 bug and is not here.
func TestMatch_StatementArmAllowsAnUnbracedOneArmedIf(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => println("f")
		let g = (n: i64) -> void => {
			match n { 0 => if n == 0 { f() }, _ => { } }
		}
	`, false)
	assertNoErrors(t, res)
}

// A nested `match` in statement position gets the same treatment, which is what makes
// checkExprForEffect recursive rather than a two-case test.
func TestMatch_StatementArmAllowsANestedStatementMatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => println("f")
		let g = (n: i64) -> void => {
			match n { 0 => { f() }, _ => match n { 1 => { f() }, _ => { } } }
		}
	`, false)
	assertNoErrors(t, res)
}

// Value position must stay strict: the arms' types still have to agree.
func TestMatch_ValueArmsMustStillAgree(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (n: i64) -> i64 => match n { 0 => 1, _ => "x" }
	`, false)
	assertHasErrorContaining(t, res, "match arms have incompatible types")
}

// And a one-armed `if` is still refused where the match's value is used — the guard
// against having simply switched the check off.
func TestMatch_ValueArmStillRefusesAOneArmedIf(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (n: i64) -> i64 => match n { 0 => if n == 0 { 1 }, _ => 2 }
	`, false)
	assertHasErrorContaining(t, res, "must have an `else` branch")
}

// The `if` half of the same rule, and the fifth member of the family: a statement-position
// `if` checks its *branches* for effect too, so a one-armed `if` nested inside one is
// legal. checkIfExpr took its requireType flag for the mismatch check but still inferred
// both branches as values, which is what refused this.
func TestIf_StatementBranchAllowsANestedOneArmedIf(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => println("f")
		let g = (n: i64) -> void => {
			if n == 0 { if n == 0 { f() } } else { f() }
		}
	`, false)
	assertNoErrors(t, res)
}

// A braced `else { if … }`, which the special case this replaced did not cover: it
// propagated statement context only through a bare `else if`.
func TestIf_StatementBracedElseAllowsANestedOneArmedIf(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => println("f")
		let g = (n: i64) -> void => {
			if n == 0 { f() } else { if n == 1 { f() } }
		}
	`, false)
	assertNoErrors(t, res)
}

// Value position keeps requiring an else, at any nesting depth.
func TestIf_ValueBranchStillRequiresAnElse(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let g = (n: i64) -> i64 => if n == 0 { if n == 0 { 1 } } else { 2 }
	`, false)
	assertHasErrorContaining(t, res, "must have an `else` branch")
}

// And incompatible branches are still an error where the value is used.
func TestIf_ValueBranchesMustStillAgree(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let g = (n: i64) -> i64 => if n == 0 { 1 } else { "x" }
	`, false)
	assertHasErrorContaining(t, res, "incompatible types")
}
