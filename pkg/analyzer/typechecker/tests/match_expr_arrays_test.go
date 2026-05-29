package typechecker_test

import "testing"

// ── arm-level pattern validation ───────────────────────────────────────────
// These tests verify which pattern kinds are accepted at the top level of an
// array match arm.

func TestTypeCheck_ArrayMatchExpr_WildcardPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[head, ...tail] => println("some"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		xs => println("any"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_ArrayPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[a, b, c] => println("three"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_BindingPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		all @ [head, ...tail] => println("got array"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_StringLiteralPattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		"hello" => println("string"),
		_ => println("other"),
	}
	`, false)
	assertErrorsAre(t, res, `expected array pattern, got "hello"`)
}

func TestTypeCheck_ArrayMatchExpr_RangePattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		0..=10 => println("range"),
		_ => println("other"),
	}
	`, false)
	// RangePattern.GetName() formats via its child expression nodes.
	assertErrorsAre(t, res, "expected array pattern, got IntegerLiteralExpr(0, Base: 10)..IntegerLiteralExpr(10, Base: 10)=")
}

// ── element type checking ──────────────────────────────────────────────────
// These tests verify that literal elements inside an array pattern are
// compatible with the array's element type.

func TestTypeCheck_ArrayMatchExpr_CorrectIntLiteralElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[1, 2, ...rest] => println("starts with 1 2"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_CorrectStringLiteralElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let words: []string = ["hello", "world"]
	match words {
		["hello", ...rest] => println("greeting"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_WrongLiteralElement_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		["hello", ...rest] => println("bad"),
		_ => println("other"),
	}
	`, false)
	assertErrorsAre(t, res, `element pattern "hello" does not match array element type int`)
}

func TestTypeCheck_ArrayMatchExpr_BoolLiteralInIntArray_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[true, ...rest] => println("bad"),
		_ => println("other"),
	}
	`, false)
	assertErrorsAre(t, res, "element pattern true does not match array element type int")
}

func TestTypeCheck_ArrayMatchExpr_WildcardElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[_, _, ...rest] => println("at least two"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_RestElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[head, ...tail] => println("head and tail"),
	}
	`, false)
	// [head, ...tail] only covers length ≥ 1 — empty arrays are missed.
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_BindingElementDelegates_Error(t *testing.T) {
	// A binding pattern wrapping a mismatched literal element should still error.
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[first @ "oops", ...rest] => println("bad"),
		_ => println("other"),
	}
	`, false)
	assertErrorsAre(t, res, `element pattern "oops" does not match array element type int`)
}

func TestTypeCheck_ArrayMatchExpr_MultipleWrongElements_MultipleErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		["a", "b", ...rest] => println("bad"),
		_ => println("other"),
	}
	`, false)
	assertErrorsAre(t, res,
		`element pattern "a" does not match array element type int`,
		`element pattern "b" does not match array element type int`,
	)
}

// ── exhaustiveness ─────────────────────────────────────────────────────────
// Arrays are unbounded in length, so only a wildcard, bare identifier, or a
// [...rest]-only pattern (which matches any length) makes a match exhaustive.

func TestTypeCheck_ArrayMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[head, ...tail] => println("some"),
		_ => println("other"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_IdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		xs => println("any array"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_RestOnlyPatternIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[...rest] => println("any length"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_SpecificLengthOnly_Warning(t *testing.T) {
	// A fixed-arity pattern like [a, b] only covers length-2 arrays.
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[a, b] => println("exactly two"),
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_NoArms_Warning(t *testing.T) {
	// Even multiple specific-arity arms don't cover all lengths.
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		[a] => println("one"),
		[a, b] => println("two"),
		[a, b, c] => println("three"),
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_GuardedCatchallOnly_Warning(t *testing.T) {
	// A guarded wildcard doesn't count — the guard may not hold.
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		xs if true => println("guarded"),
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_ArrayMatchExpr_BindingAroundWildcard_IsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		all @ _ => println("any array, also bound to all"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_BindingAroundIdentifier_IsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		all @ xs => println("any array"),
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ArrayMatchExpr_BindingAroundSpecificPattern_NotExhaustive_Warning(t *testing.T) {
	// all @ [head, ...tail] only covers non-empty arrays.
	res := parseCollectAndCheck(t, `
	let nums: []int = [1, 2, 3]
	match nums {
		all @ [head, ...tail] => println("non-empty"),
	}
	`, false)
	assertWarningsAre(t, res,
		"match on array type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}
