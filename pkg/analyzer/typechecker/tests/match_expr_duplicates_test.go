package typechecker_test

import "testing"

// ── duplicate literal arms ────────────────────────────────────────────────────

func TestTypeCheck_DuplicateArms_IntLiteral_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x = 1
  match x {
    1 => "first",
    1 => "second",
    _ => "other",
  }`, false)
	assertWarningsAre(t, res,
		"duplicate match arm: pattern 1 is already covered by an earlier arm")
}

func TestTypeCheck_DuplicateArms_StringLiteral_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let s = "hello"
  match s {
    "hello" => "first",
    "hello" => "second",
    _ => "other",
  }`, false)
	assertWarningsAre(t, res,
		"duplicate match arm: pattern \"hello\" is already covered by an earlier arm")
}

func TestTypeCheck_DuplicateArms_BoolLiteral_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    true => "first",
    true => "second",
    false => "no",
  }`, false)
	assertWarningsAre(t, res,
		"duplicate match arm: pattern true is already covered by an earlier arm")
}

func TestTypeCheck_DuplicateArms_GuardedNotDuplicate_Ok(t *testing.T) {
	// A guarded arm with the same literal is not a duplicate — the guard may fail.
	res := parseCollectAndCheck(t, `
  let x = 1
  match x {
    1 if true => "guarded",
    1 => "unguarded",
    _ => "other",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_DuplicateArms_NoDuplicates_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x = 1
  match x {
    1 => "one",
    2 => "two",
    _ => "other",
  }`, false)
	assertNoErrors(t, res)
}

// ── overlapping numeric range arms ───────────────────────────────────────────

func TestTypeCheck_OverlappingArms_Ranges_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 100
  match x {
    0..<=200   => "low",
    150..<=255 => "high",
  }`, false)
	assertWarningsAre(t, res,
		"overlapping match arm: this range overlaps with a previous arm")
}

func TestTypeCheck_OverlappingArms_NonOverlapping_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let x: u8 = 100
  match x {
    0..<=127   => "low",
    128..<=255 => "high",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_OverlappingArms_Ranges_NoWildcard_BothWarnings(t *testing.T) {
	// Two overlapping ranges that together don't cover all of u8: both the
	// overlap warning and the exhaustiveness warning are emitted.
	res := parseCollectAndCheck(t, `
  let x: u8 = 50
  match x {
    0..<=100 => "low",
    50..<=200 => "mid",
  }`, false)
	assertWarningsAre(t, res,
		"match on numeric type is not exhaustive: add a wildcard `_ => ...` or catch-all arm",
		"overlapping match arm: this range overlaps with a previous arm")
}

func TestTypeCheck_OverlappingArms_GuardedNotOverlapping_Ok(t *testing.T) {
	// Guarded arms are excluded from interval analysis.
	res := parseCollectAndCheck(t, `
  let x: u8 = 100
  match x {
    0..<=255 => "full",
    n if n > 150 => "guarded",
  }`, false)
	assertNoErrors(t, res)
}
