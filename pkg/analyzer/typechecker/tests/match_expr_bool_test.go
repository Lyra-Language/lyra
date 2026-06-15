package typechecker_test

import "testing"

// ── arm pattern type-checking ─────────────────────────────────────────────────

func TestTypeCheck_BoolMatch_BothArms_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    true  => "yes",
    false => "no",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolMatch_Wildcard_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    true => "yes",
    _    => "no",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolMatch_UnguardedIdentifier_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = false
  match flag {
    true => "yes",
    x    => "no",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolMatch_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    1 => "one",
    _ => "other",
  }`, false)
	assertErrorsAre(t, res, "literal pattern '1' is not a boolean value (expected true or false)")
}

func TestTypeCheck_BoolMatch_StringLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    "true" => "yes",
    _      => "no",
  }`, false)
	assertErrorsAre(t, res, "literal pattern '\"true\"' is not a boolean value (expected true or false)")
}

// ── exhaustiveness ────────────────────────────────────────────────────────────

func TestTypeCheck_BoolMatch_MissingFalse_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    true => "yes",
  }`, false)
	assertErrorsAre(t, res,
		"match on bool is not exhaustive: add arms for both `true` and `false`, or a wildcard `_ => ...`")
}

func TestTypeCheck_BoolMatch_MissingTrue_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    false => "no",
  }`, false)
	assertErrorsAre(t, res,
		"match on bool is not exhaustive: add arms for both `true` and `false`, or a wildcard `_ => ...`")
}

func TestTypeCheck_BoolMatch_GuardedOnly_Error(t *testing.T) {
	// A guarded arm does not count toward exhaustiveness.
	res := parseCollectAndCheck(t, `
  let flag = true
  match flag {
    true  => "yes",
    x if x => "also yes",
  }`, false)
	assertErrorsAre(t, res,
		"match on bool is not exhaustive: add arms for both `true` and `false`, or a wildcard `_ => ...`")
}
