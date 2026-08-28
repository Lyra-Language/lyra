package typechecker_test

import "testing"

// ── basic arm type-checking ────────────────────────────────────────────────

func TestTypeCheck_StringMatchExpr_StringLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hello" => "ok",
    _ => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_IntLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    42 => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '42' is not a string type")
}

func TestTypeCheck_StringMatchExpr_BoolLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    true => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern 'true' is not a string type")
}

func TestTypeCheck_StringMatchExpr_FloatLiteral_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    3.14 => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, "literal pattern '3.14' is not a string type")
}

func TestTypeCheck_StringMatchExpr_IdentifierPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => "ok",
    other => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

// ── exhaustiveness ─────────────────────────────────────────────────────────

func TestTypeCheck_StringMatchExpr_NoWildcard_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => "ok",
    "bye" => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_StringMatchExpr_WildcardIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => "ok",
    "bye" => "ok",
    _ => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_UnguardedIdentifierIsExhaustive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => "ok",
    rest => "ok",
  }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringMatchExpr_GuardedCatchallOnly_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "hi" => "ok",
    rest if rest != "" => "ok",
  }
	`, false)
	assertWarningsAre(t, res,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

// ── regex patterns ─────────────────────────────────────────────────────────

// A regex pattern on a string scrutinee is **accepted** (08/28): a match pattern is
// a compile-time constant by grammar, so it compiles to the same DFA tables a
// `where pattern(...)` constraint ships and the arm's test is one call to the shared
// driver — the settled sequencing, where `r"…"` means the compile-time, linear-time
// subset in every position. The 08/13 refusal these tests used to pin cited a
// runtime with no regex engine, reasoning the constraint machinery had retired the
// same day.

func TestTypeCheck_StringMatchExpr_RegexPatterns_Accepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
  let foo = "abc123"
  match foo {
    r"[0-9]+" => "ok",
    r"[a-z]+" => "ok",
    _ => "ok",
  }
	`, false))
}

func TestTypeCheck_StringMatchExpr_RegexOnlyNoWildcard_StillNotExhaustive(t *testing.T) {
	// Regex arms never make a string match exhaustive on their own — even if they
	// collectively cover everything, we don't try to prove it — so the match still
	// wants a catch-all.
	res := parseCollectAndCheck(t, `
  let foo = "abc"
  match foo {
    r"[0-9]+" => "ok",
    r"[a-z]+" => "ok",
  }
	`, false)
	assertErrorsAre(t, res,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_StringMatchExpr_MixedLiteralAndRegex_Accepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "exact" => "ok",
    r"[A-Z][a-z]+" => "ok",
    _ => "ok",
  }
	`, false))
}

func TestTypeCheck_StringMatchExpr_InvalidRegex_ReportedAtTheArm(t *testing.T) {
	// A malformed regex is a compile error at the arm, exactly as it is at a
	// constraint declaration — the pattern is compiled at type-check time, so its
	// contents are checked where they are written.
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    r"a*?b" => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res,
		`invalid regex pattern r"a*?b": regex parse error at offset 2: lazy quantifiers (*?, +?, ??, {n,m}?) are not supported; rewrite with complement`)
}

func TestTypeCheck_StringMatchExpr_UncompilablePattern_E054AtTheArm(t *testing.T) {
	// What lyra-E054 refuses at a constraint it refuses at an arm: a pattern that
	// cannot become a table is a property of the language's regex, in every
	// position alike.
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    r"(?<=a)b" => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res,
		"this pattern cannot be compiled to a runtime matcher (regex: pattern cannot be compiled to a match table: (?<=a)b uses a lookbehind, whose gate depends on text before the input)")
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
    r"[0-9]+" => "ok",
    _ => "ok",
  }
	`, false)
	if len(res.errors) == 0 {
		t.Errorf("expected an error for regex pattern on numeric scrutinee, got none")
	}
}
