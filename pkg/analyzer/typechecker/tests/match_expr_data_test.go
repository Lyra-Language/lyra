package typechecker_test

import "testing"

// ── helpers ──────────────────────────────────────────────────────────────────

// maybeDecl declares a simple two-constructor data type used across tests.
const maybeDecl = `data Maybe = None | Some i32`

// ── arm pattern type-checking ─────────────────────────────────────────────────

func TestTypeCheck_DataMatchExpr_AllConstructors_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    None => println("none"),
    Some x => println("some"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_DataMatchExpr_WildcardMakesExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    Some x => println("some"),
    _ => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_DataMatchExpr_UnguardedIdentifierMakesExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    Some x => println("some"),
    other => println("catch-all"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_DataMatchExpr_DirectScrutinee_Ok(t *testing.T) {
	// The scrutinee can be a data constructor expression inline.
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  match Some 42 {
    None => println("none"),
    Some x => println("some"),
  }
	`, false)
	assertNoErrors(t, res)
}

// ── exhaustiveness warnings ───────────────────────────────────────────────────

func TestTypeCheck_DataMatchExpr_MissingOneConstructor_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    Some x => println("some"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on Maybe is not exhaustive: missing constructors: None")
}

func TestTypeCheck_DataMatchExpr_MissingMultipleConstructors_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Result = Ok i32 | Err i32 | Pending i32
  let r = Ok 0
  match r {
    Ok x => println("ok"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on Result is not exhaustive: missing constructors: Err, Pending")
}

func TestTypeCheck_DataMatchExpr_AllMissing_Warning(t *testing.T) {
	// Only a guarded arm — not exhaustive.
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    Some x if x > 0 => println("positive"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on Maybe is not exhaustive: missing constructors: None, Some")
}

func TestTypeCheck_DataMatchExpr_GuardedCatchallNotExhaustive_Warning(t *testing.T) {
	// A guarded wildcard does not count as a catch-all.
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    None => println("none"),
    _ if true => println("guarded wildcard"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on Maybe is not exhaustive: missing constructors: Some")
}

// ── invalid pattern types ─────────────────────────────────────────────────────

func TestTypeCheck_DataMatchExpr_LiteralPattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    42 => println("literal"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal patterns are not allowed on a data type scrutinee")
}

func TestTypeCheck_DataMatchExpr_RangePattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    0..=100 => println("range"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "range patterns are not allowed on a data type scrutinee")
}

func TestTypeCheck_DataMatchExpr_RegexPattern_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    r/[0-9]+/ => println("regex"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "regex patterns are not allowed on a data type scrutinee")
}

// ── unknown constructor ───────────────────────────────────────────────────────

func TestTypeCheck_DataMatchExpr_UnknownConstructor_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe = None | Some i32
  let foo = Some 42
  match foo {
    Unknown x => println("huh"),
    None => println("none"),
    Some x => println("some"),
  }
	`, false)
	assertErrorsAre(t, res, "Unknown is not a constructor of Maybe")
}

// ── multi-constructor data types ──────────────────────────────────────────────

func TestTypeCheck_DataMatchExpr_ThreeConstructors_AllCovered_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Shape = Circle i32 | Rect i32 | Triangle i32
  let s = Circle 5
  match s {
    Circle r => println("circle"),
    Rect w => println("rect"),
    Triangle b => println("triangle"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_DataMatchExpr_ThreeConstructors_TwoCovered_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Shape = Circle i32 | Rect i32 | Triangle i32
  let s = Circle 5
  match s {
    Circle r => println("circle"),
    Rect w => println("rect"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on Shape is not exhaustive: missing constructors: Triangle")
}

// ── variable scrutinee with inferred data type ────────────────────────────────

func TestTypeCheck_DataMatchExpr_ViaVariable_Exhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Result = Ok i32 | Err i32
  let r = Ok 1
  match r {
    Ok v => println("ok"),
    Err e => println("err"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_DataMatchExpr_ViaVariable_Missing_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Result = Ok i32 | Err i32
  let r = Ok 1
  match r {
    Ok v => println("ok"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on Result is not exhaustive: missing constructors: Err")
}
