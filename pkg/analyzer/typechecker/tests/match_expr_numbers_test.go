package typechecker_test

import (
	"testing"
)

func TestTypeCheck_NumericMatchExpr_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    42 => "ok",
    _ => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatchExpr_String_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    "42" => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '\"42\"' is not an integer type")
}

func TestTypeCheck_NumericMatchExpr_Boolean_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    true => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 'true' is not an integer type")
}

func TestTypeCheck_NumericMatchExpr_Float_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    4.2 => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '4.2' is not an integer type")
}

func TestTypeCheck_NumericMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  let bar = 7
  match foo {
    bar => "ok",
    _ => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

// ── Exhaustiveness checks ────────────────────────────────────────────────────

func TestTypeCheck_NumericMatchExpr_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1 => "ok",
    2 => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1  => "ok",
    2  => "ok",
    _  => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatchExpr_UnguardedIdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1   => "ok",
    n   => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatchExpr_GuardedCatchallOnly_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    1 => "ok",
    n if n > 1 => "ok",
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
    1..=10 => "ok",
    11..=99 => "ok",
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
    0..=255 => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_TwoRanges_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=127 => "ok",
    128..=255 => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_RangePlusLiteral_Ok(t *testing.T) {
	// 0..=254 plus a single literal for 255 covers all of u8.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=254 => "ok",
    255 => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_ExclusiveRange_Ok(t *testing.T) {
	// 0..<256 (exclusive end) is identical to 0..=255 for integers.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..<256 => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_U8_OverlappingRanges_Warning(t *testing.T) {
	// Overlapping ranges still cover the full range, but the overlap portion
	// of the second arm (150..=200) is unreachable — so a warning is emitted.
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=200   => "ok",
    150..=255 => "ok",
  }
	`, false)
	assertWarningsAre(t, res, "overlapping match arm: this range overlaps with a previous arm")
}

func TestTypeCheck_NumericMatch_U8_MissingTop_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    0..=254 => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_U8_MissingBottom_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 200
  match x {
    1..=255 => "ok",
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
    0..=100  => "ok",
    102..=255 => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_I8_FullRange_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i8 = -5
  match x {
    -128..=127 => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_I8_NegAndPos_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i8 = -5
  match x {
    -128..=-1 => "ok",
    0         => "ok",
    1..=127   => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_I8_MissingNegatives_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i8 = -5
  match x {
    0..=127 => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_NumericMatch_I32_FullRange_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: i32 = 0
  match x {
    -2147483648..=2147483647 => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NumericMatch_GuardedRangeNotExhaustive_Warning(t *testing.T) {
	// A range arm with a guard does not count towards coverage.
	res := parseCollectAndCheck(t, `
  let x: u8 = 10
  match x {
    n if n > 0 => "ok",
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
    3.14 => "ok",
    _    => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FloatMatch_IntLiteral_Error(t *testing.T) {
	// An integer literal is not a valid pattern for a float scrutinee.
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    1 => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '1' is not a float type")
}

func TestTypeCheck_FloatMatch_BoolLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    true => "ok",
    _    => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 'true' is not a float type")
}

func TestTypeCheck_FloatMatch_StringLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    "3.14" => "ok",
    _      => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '\"3.14\"' is not a float type")
}

func TestTypeCheck_FloatMatch_F32_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f32 = 1.5
  match x {
    1 => "ok",
    _ => "ok",
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
    0.0 => "ok",
    1.0 => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_FloatMatch_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    0.0 => "ok",
    _   => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FloatMatch_UnguardedIdentifier_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: f32 = 1.5
  match x {
    0.0 => "ok",
    v   => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FloatMatch_GuardedCatchallOnly_Warning(t *testing.T) {
	// A guarded identifier does not guarantee exhaustiveness.
	res := parseCollectAndCheck(t, `
  let x: f64 = 3.14
  match x {
    0.0      => "ok",
    v if v > 0.0 => "ok",
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
    0.0..=1.0 => "ok",
    1.0..=2.0 => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}
