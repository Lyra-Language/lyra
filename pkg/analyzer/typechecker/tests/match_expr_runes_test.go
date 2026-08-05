package typechecker_test

import "testing"

// ── basic arm type-checking ────────────────────────────────────────────────

func TestTypeCheck_RuneMatchExpr_CharLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    'a' => 1,
    'b' => 2,
    _ => 0,
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_RuneMatchExpr_EscapedCharLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = '\n'
  match c {
    '\n' => 1,
    '\t' => 2,
    _ => 0,
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_RuneMatchExpr_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    5 => 1,
    _ => 0,
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 5 is not a rune (character) value")
}

func TestTypeCheck_RuneMatchExpr_StringLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    "a" => 1,
    _ => 0,
  }
	`, false)
	assertErrorsAre(t, res, `literal pattern "a" is not a rune (character) value`)
}

func TestTypeCheck_RuneMatchExpr_RangePattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    0..<=9 => 1,
    _ => 0,
  }
	`, false)
	assertErrorsAre(t, res, "range patterns are not allowed on a rune scrutinee")
}

func TestTypeCheck_RuneMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    'a' => 1,
    other => 0,
  }
	`, false)
	assertNoErrors(t, res)
}

// ── exhaustiveness ─────────────────────────────────────────────────────────

func TestTypeCheck_RuneMatchExpr_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    'a' => 1,
    'b' => 2,
  }
	`, false)
	assertWarningsAre(t, res,
		"match on rune type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_RuneMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    'a' => 1,
    _ => 0,
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_RuneMatchExpr_UnguardedIdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    'a' => 1,
    rest => 0,
  }
	`, false)
	assertNoErrors(t, res)
}

// ── guards ─────────────────────────────────────────────────────────────────

func TestTypeCheck_RuneMatchExpr_Guard_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    x if x == 'a' => 1,
    _ => 0,
  }
	`, false)
	assertNoErrors(t, res)
}
