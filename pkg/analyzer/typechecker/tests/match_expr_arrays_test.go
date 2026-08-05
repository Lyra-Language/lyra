package typechecker_test

import "testing"

// ── arm-level pattern validation ───────────────────────────────────────────
// These tests verify which pattern kinds are accepted at the top level of an
// array match arm.

func TestTypeCheck_ArrayMatchExpr_WildcardPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[head, ...tail] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		xs => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_ArrayPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[a, b, c] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_BindingPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		all @ [head, ...tail] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_StringLiteralPattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		"hello" => "ok",
		_ => "ok",
	}
	`, false)
	assertErrorsAre(t, res, `expected array pattern, got "hello"`)
}

func TestTypeCheck_ArrayMatchExpr_RangePattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		0..<=10 => "ok",
		_ => "ok",
	}
	`, false)
	// RangePattern.GetName() formats via its child expression nodes, which now render as
	// source. This asserted the struct dump until 08/03 — it was the message that put
	// literal rendering on the bug list.
	assertErrorsAre(t, res, "expected array pattern, got 0..<=10")
}

// ── element type checking ──────────────────────────────────────────────────
// These tests verify that literal elements inside an array pattern are
// compatible with the array's element type.

func TestTypeCheck_ArrayMatchExpr_CorrectIntLiteralElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[1, 2, ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_CorrectStringLiteralElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let words: []string = ["hello", "world"]
	match words {
		["hello", ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_WrongLiteralElement_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		["hello", ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertErrorsAre(t, res, `element pattern "hello" does not match array element type i64`)
}

func TestTypeCheck_ArrayMatchExpr_BoolLiteralInIntArray_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[true, ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertErrorsAre(t, res, "element pattern true does not match array element type i64")
}

func TestTypeCheck_ArrayMatchExpr_WildcardElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[_, _, ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_RestElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[head, ...tail] => "ok",
	}
	`, false)
	// [head, ...tail] only covers length ≥ 1 — empty arrays are missed.
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_BindingElementDelegates_Error(t *testing.T) {
	// A binding pattern wrapping a mismatched literal element should still error.
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[first @ "oops", ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertErrorsAre(t, res, `element pattern "oops" does not match array element type i64`)
}

func TestTypeCheck_ArrayMatchExpr_MultipleWrongElements_MultipleErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		["a", "b", ...rest] => "ok",
		_ => "ok",
	}
	`, false)
	assertErrorsAre(t, res,
		`element pattern "a" does not match array element type i64`,
		`element pattern "b" does not match array element type i64`,
	)
}

// ── exhaustiveness ─────────────────────────────────────────────────────────
// Arrays are unbounded in length, so only a wildcard, bare identifier, or a
// [...rest]-only pattern (which matches any length) makes a match exhaustive.

func TestTypeCheck_ArrayMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[head, ...tail] => "ok",
		_ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_IdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		xs => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_RestOnlyPatternIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[...rest] => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_SpecificLengthOnly_Warning(t *testing.T) {
	// A fixed-arity pattern like [a, b] only covers length-2 arrays.
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[a, b] => "ok",
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_NoArms_Warning(t *testing.T) {
	// Even multiple specific-arity arms don't cover all lengths.
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		[a] => "ok",
		[a, b] => "ok",
		[a, b, c] => "ok",
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_GuardedCatchallOnly_Warning(t *testing.T) {
	// A guarded wildcard doesn't count — the guard may not hold.
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		xs if true => "ok",
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_BindingAroundWildcard_IsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		all @ _ => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_BindingAroundIdentifier_IsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		all @ xs => "ok",
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_BindingAroundSpecificPattern_NotExhaustive_Warning(t *testing.T) {
	// all @ [head, ...tail] only covers non-empty arrays.
	res := parseCollectAndCheck(t, `
	let nums: []i64 = [1, 2, 3]
	match nums {
		all @ [head, ...tail] => "ok",
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// ── length-union exhaustiveness ───────────────────────────────────────────────
//
// An array match is over *lengths*, so a union of arms can cover everything even
// when no single arm does: `[e1..en]` covers exactly n, `[e1..en, ...rest]` covers
// every length ≥ n. The recursive list idiom relies on this, and used to draw a
// spurious "not exhaustive" warning demanding an unreachable wildcard — the same
// corrosive false warning the tuple/struct case had.

func TestTypeCheck_ArrayMatch_EmptyPlusHeadTail_Exhaustive(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let sum = (xs: []i64) -> i64 => match xs {
			[] => 0,
			[h, ...t] => h,
		}
	`, false)
	assertWarningsAre(t, res) // no errors and no warnings
}

// Every length below the open-ended arm's minimum must be covered.
func TestTypeCheck_ArrayMatch_AllLengthsBelowRest_Exhaustive(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (xs: []i64) -> i64 => match xs {
			[] => 0,
			[x] => x,
			[p, q, ...r] => p,
		}
	`, false)
	assertWarningsAre(t, res)
}

// A gap below the open-ended arm leaves lengths unmatched — here the empty array.
func TestTypeCheck_ArrayMatch_GapBelowRest_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (xs: []i64) -> i64 => match xs {
			[x] => x,
			[p, q, ...r] => p,
		}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// A literal element test proves nothing about length coverage: `[1, ...r]` matches
// only arrays starting with 1, so it cannot stand in for the open-ended arm.
func TestTypeCheck_ArrayMatch_LiteralElementDoesNotCover_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (xs: []i64) -> i64 => match xs {
			[1, ...r] => 1,
			[] => 0,
		}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// Without any open-ended arm, infinitely many lengths are unmatched.
func TestTypeCheck_ArrayMatch_NoRestArm_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (xs: []i64) -> i64 => match xs {
			[] => 0,
			[x] => x,
		}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// A guarded arm may fail, so it contributes no coverage.
func TestTypeCheck_ArrayMatch_GuardedArmDoesNotCover_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (xs: []i64) -> i64 => match xs {
			[] => 0,
			[h, ...t] if h > 0 => h,
		}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}
