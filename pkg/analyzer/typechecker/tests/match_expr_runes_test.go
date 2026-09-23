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

// A range over a rune is legal as of 09/23 — but written in runes. `0..<=9` is the same
// set as `'0'..<='9'` with its meaning removed, and the scrutinee's type is what decides
// how its patterns are spelled.
func TestTypeCheck_RuneMatchExpr_RangePattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    0..<=9 => 1,
    _ => 0,
  }
	`, false)
	assertErrorsAre(t, res,
		"a range pattern on a rune scrutinee takes rune bounds: write 0 and 9 as a character literal")
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

// A rune range written in runes is accepted, and its bounds may be open on either side —
// an open bound names the type's own edge rather than a value, so there is nothing there to
// be in the wrong units.
func TestTypeCheck_RuneMatchExpr_RuneBoundedRange_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    '0'..<='9' => 1,
    'a'..     => 2,
    _ => 0,
  }
	`, false)
	assertNoErrors(t, res)
}

// **An alternation is checked alternative by alternative**, so a bad one is reported where
// it is written. Here the rune scrutinee's rule rejects the numeric bound in the second
// alternative and says nothing about the first.
func TestTypeCheck_RuneMatchExpr_AlternationChecksEachAlternative(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let c: rune = 'a'
  match c {
    '0'..<='9' | 48..<=57 => 1,
    _ => 0,
  }
	`, false)
	assertErrorsAre(t, res,
		"a range pattern on a rune scrutinee takes rune bounds: write 48 and 57 as a character literal")
}
