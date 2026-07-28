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

// An irrefutable destructuring arm covers every value of a tuple type (a tuple
// is single-shape — there is no tag to discriminate), so it *is* exhaustive and
// must not demand an unreachable wildcard. This previously warned.
func TestTypeCheck_TupleMatch_IrrefutablePattern_Exhaustive(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    (x, y) => "ok",
  }`, false)
	assertWarningsAre(t, res) // no errors and no warnings
}

// Nested destructuring is irrefutable too, as long as every leaf binds.
func TestTypeCheck_TupleMatch_NestedIrrefutable_Exhaustive(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = ((1, 2), 3)
  match p {
    ((a, b), c) => "ok",
  }`, false)
	assertWarningsAre(t, res) // no errors and no warnings
}

// A literal sub-pattern makes the arm refutable, so the match is genuinely
// non-exhaustive and still warns.
func TestTypeCheck_TupleMatch_LiteralSubPattern_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    (0, y) => "zero",
  }`, false)
	assertWarningsAre(t, res,
		"match on tuple type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// A guard can fail, so a guarded irrefutable arm doesn't seal the match.
func TestTypeCheck_TupleMatch_GuardedIrrefutable_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let p = (1, 2)
  match p {
    (x, y) if x > 0 => "ok",
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
