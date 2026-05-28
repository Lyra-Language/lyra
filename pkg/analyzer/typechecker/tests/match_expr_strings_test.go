package typechecker_test

import "testing"

// ── basic arm type-checking ────────────────────────────────────────────────

func TestTypeCheck_StringMatchExpr_StringLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hello" => println("greeting"),
    _ => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    42 => println("number"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '42' is not a string type")
}

func TestTypeCheck_StringMatchExpr_BoolLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    true => println("bool"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 'true' is not a string type")
}

func TestTypeCheck_StringMatchExpr_FloatLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    3.14 => println("float"),
    _ => println("other"),
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '3.14' is not a string type")
}

func TestTypeCheck_StringMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => println("greeting"),
    other => println(other),
  }
	`, false)
	assertNoErrors(t, res)
}

// ── exhaustiveness ─────────────────────────────────────────────────────────

func TestTypeCheck_StringMatchExpr_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => println("greeting"),
    "bye" => println("farewell"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_StringMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => println("greeting"),
    "bye" => println("farewell"),
    _ => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_UnguardedIdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => println("greeting"),
    rest => println(rest),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_GuardedCatchallOnly_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => println("greeting"),
    rest if rest != "" => println("non-empty"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// ── regex patterns ─────────────────────────────────────────────────────────

func TestTypeCheck_StringMatchExpr_RegexPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "abc123"
  match foo {
    r/[0-9]+/ => println("digits"),
    r/[a-z]+/ => println("lowercase"),
    _ => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_RegexOnlyNoWildcard_Warning(t *testing.T) {
	// Regex arms never make a string match exhaustive on their own — even if
	// they collectively cover everything, we don't try to prove it.
	res := parseCollectAndCheck(t, `
  let foo = "abc"
  match foo {
    r/[0-9]+/ => println("digits"),
    r/[a-z]+/ => println("lowercase"),
  }
	`, false)
	assertWarningsAre(t, res,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_StringMatchExpr_MixedLiteralAndRegex_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "exact" => println("exact match"),
    r/[A-Z][a-z]+/ => println("capitalized word"),
    _ => println("other"),
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_InvalidRegex_Error(t *testing.T) {
	// Lazy quantifiers are rejected by the engine; the typechecker should
	// surface that error at compile time rather than waiting until runtime.
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    r/a*?b/ => println("lazy"),
    _ => println("other"),
  }
	`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected an error for an invalid regex pattern, got none")
	}
	found := false
	for _, e := range res.errors {
		if e.Message != "" && containsSubstring(e.Message, "invalid regex pattern") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an error mentioning 'invalid regex pattern', got: %+v", res.errors)
	}
}

// containsSubstring is a tiny stand-in for strings.Contains to keep the test
// file free of stdlib imports beyond testing.
func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── leftover non-string scrutinee should not regress ───────────────────────

func TestTypeCheck_StringMatchExpr_RegexOnNonString_NoStringCheck(t *testing.T) {
	// A regex literal in the match-arm position on a numeric scrutinee
	// should still be rejected by the existing numeric arm checker (which
	// is unchanged by this work). We don't assert the exact error, only
	// that something flags it.
	res := parseCollectAndCheck(t, `
  let foo = 42
  match foo {
    r/[0-9]+/ => println("digits"),
    _ => println("other"),
  }
	`, false)
	if len(res.errors) == 0 {
		t.Errorf("expected an error for regex pattern on numeric scrutinee, got none")
	}
}
