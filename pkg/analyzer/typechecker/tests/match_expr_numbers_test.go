package typechecker_test

import (
	"testing"
)

func TestTypeCheck_NumericMatchExpr_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    42 => println("foo is 42"),
    _ => println("foo is not 42"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatchExpr_String_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    "42" => println("foo is 42"),
    _ => println("foo is not 42"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '\"42\"' is not an integer type")
}

func TestTypeCheck_NumericMatchExpr_Boolean_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    true => println("foo is 42"),
    _ => println("foo is not 42"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 'true' is not an integer type")
}

func TestTypeCheck_NumericMatchExpr_Float_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    4.2 => println("foo is 42"),
    _ => println("foo is not 42"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '4.2' is not an integer type")
}

func TestTypeCheck_NumericMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  let bar = 7
  match foo {
    bar => println("foo is 7"),
    _ => println("foo is not 7"),
  }
	`, false)
	assertNoErrors(t, res)
}

// ── Exhaustiveness checks ────────────────────────────────────────────────────

func TestTypeCheck_NumericMatchExpr_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1 => println("one"),
    2 => println("two"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1  => println("one"),
    2  => println("two"),
    _  => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatchExpr_UnguardedIdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1   => println("one"),
    n   => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatchExpr_GuardedCatchallOnly_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1 => println("one"),
    n if n > 1 => println("more than one"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatchExpr_RangeOnlyNoWildcard_Warning(t *testing.T) {
	// Untyped int (no fixed bounds) — interval analysis can't help; wildcard needed.
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1..=10 => println("small"),
    11..=99 => println("medium"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// ── Range-pattern interval analysis ──────────────────────────────────────────

func TestTypeCheck_NumericMatch_U8_FullRange_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=255 => println("all"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_TwoRanges_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=127 => println("low"),
    128..=255 => println("high"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_RangePlusLiteral_Ok(t *testing.T) {
	// 0..=254 plus a single literal for 255 covers all of u8.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=254 => println("not max"),
    255 => println("max"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_ExclusiveRange_Ok(t *testing.T) {
	// 0..<256 (exclusive end) is identical to 0..=255 for integers.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..<256 => println("all"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_OverlappingRanges_Ok(t *testing.T) {
	// Overlapping ranges still cover the full range.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=200   => println("low"),
    150..=255 => println("high"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_MissingTop_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=254 => println("not max"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_U8_MissingBottom_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    1..=255 => println("not zero"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_U8_Gap_Warning(t *testing.T) {
	// Covers 0..=100 and 102..=255, leaving 101 uncovered.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=100  => println("low"),
    102..=255 => println("high"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_I8_FullRange_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i8 = -5
  match x {
    -128..=127 => println("all"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_I8_NegAndPos_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i8 = -5
  match x {
    -128..=-1 => println("negative"),
    0         => println("zero"),
    1..=127   => println("positive"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_I8_MissingNegatives_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i8 = -5
  match x {
    0..=127 => println("non-negative"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_I32_FullRange_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i32 = 0
  match x {
    -2147483648..=2147483647 => println("all"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_GuardedRangeNotExhaustive_Warning(t *testing.T) {
	// A range arm with a guard does not count towards coverage.
	res := parseCollectAndCheck(t, `
  let x: u8 = 10
  match x {
    n if n > 0 => println("positive"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// ── Float match expressions ─────────────────────────────────────────────────────────

// — Literal kind checking —

func TestTypeCheck_FloatMatch_FloatLiteral_Wildcard_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    3.14 => println("pi-ish"),
    _    => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FloatMatch_IntLiteral_Error(t *testing.T) {
	// An integer literal is not a valid pattern for a float scrutinee.
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    1 => println("one"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '1' is not a float type")
}

func TestTypeCheck_FloatMatch_BoolLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    true => println("true?"),
    _    => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 'true' is not a float type")
}

func TestTypeCheck_FloatMatch_StringLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    "3.14" => println("string pi"),
    _      => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '\"3.14\"' is not a float type")
}

func TestTypeCheck_FloatMatch_F32_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f32 = 1.5
  match x {
    1 => println("one"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '1' is not a float type")
}

// — Exhaustiveness: floats have no finite discrete bounds, so only a wildcard
//   or an unguarded identifier can make the match exhaustive. —

func TestTypeCheck_FloatMatch_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    0.0 => println("zero"),
    1.0 => println("one"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_FloatMatch_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    0.0 => println("zero"),
    _   => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FloatMatch_UnguardedIdentifier_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f32 = 1.5
  match x {
    0.0 => println("zero"),
    v   => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FloatMatch_GuardedCatchallOnly_Warning(t *testing.T) {
	// A guarded identifier does not guarantee exhaustiveness.
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    0.0      => println("zero"),
    v if v > 0.0 => println("positive"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_FloatMatch_RangePatterns_NoWildcard_Warning(t *testing.T) {
	// Float range patterns do not count toward interval-coverage analysis
	// (floats have no fixed discrete bounds), so a wildcard is still required.
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    0.0..=1.0 => println("small"),
    1.0..=2.0 => println("medium"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}
