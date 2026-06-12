package typechecker_test

import "testing"

// ── arm pattern type-checking ─────────────────────────────────────────────────

func TestTypeCheck_TupleMatch_TuplePattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, "hello")
  match p {
    (x, y) => "ok",
    _ => "other",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TupleMatch_Wildcard_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    _ => "ok",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TupleMatch_UnguardedIdentifier_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    pair => "ok",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TupleMatch_WrongArity_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    (x, y, z) => "ok",
    _ => "other",
  }`, false)
	assertErrorsAre(t, res, "tuple pattern has 3 element(s) but scrutinee has 2")
}

func TestTypeCheck_TupleMatch_NonTuplePattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    42 => "ok",
    _ => "other",
  }`, false)
	assertErrorsAre(t, res, "expected tuple pattern, got 42")
}

// ── exhaustiveness ────────────────────────────────────────────────────────────

func TestTypeCheck_TupleMatch_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    (x, y) => "ok",
  }`, false)
	assertWarningsAre(t, res,
		"match on tuple type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_TupleMatch_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    (x, y) => "ok",
    _ => "other",
  }`, false)
	assertNoErrors(t, res)
}
