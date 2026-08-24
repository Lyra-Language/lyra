package typechecker_test

import "testing"

const personDecl = `
struct Person {
  name: string,
  age: i64,
}`

// ── arm pattern type-checking ─────────────────────────────────────────────────

func TestTypeCheck_StructMatch_StructPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    { name, age } => "ok",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructMatch_Wildcard_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    _ => "ok",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructMatch_UnguardedIdentifier_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    person => "ok",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructMatch_PartialFields_Ok(t *testing.T) {
	// Matching only a subset of fields is valid.
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    { name } => "ok",
  }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StructMatch_UnknownField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    { name, email } => "ok",
    _ => "other",
  }`, false)
	assertErrorsAre(t, res, `struct Person has no field "email"`)
}

func TestTypeCheck_StructMatch_NonStructPattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    42 => "ok",
    _ => "other",
  }`, false)
	assertErrorsAre(t, res, "expected struct pattern, got 42")
}

// ── exhaustiveness ────────────────────────────────────────────────────────────

// An irrefutable destructuring arm covers every value of a struct type (a struct
// is single-shape), so it *is* exhaustive — including a shorthand field list that
// names only some fields, since unlisted fields are never tested. This previously
// warned and demanded an unreachable wildcard.
func TestTypeCheck_StructMatch_IrrefutablePattern_Exhaustive(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    { name } => "ok",
  }`, false)
	assertWarningsAre(t, res) // no errors and no warnings
}

// A literal field sub-pattern is refutable, so this stays non-exhaustive.
func TestTypeCheck_StructMatch_LiteralField_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    { age: 30 } => "thirty",
  }`, false)
	assertWarningsAre(t, res,
		"match on struct type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_StructMatch_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, personDecl+`
  let p = Person { name: "Alice", age: 30 }
  match p {
    { name: "Alice" } => "ok",
    _ => "other",
  }`, false)
	assertNoErrors(t, res)
}
