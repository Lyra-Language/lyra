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

// A string scrutinee was the one place a regex pattern was accepted; every other
// scrutinee kind already refused it. Accepted and then not lowered — the build
// died with `match pattern *ast.RegexPattern not implemented for a string
// scrutinee (only string literals; regex patterns deferred)` — so it is refused
// at the arm since 08/13 (lyra-E052). These four tests asserted the acceptance.
const regexPatternRefused = "matching on a regex pattern is not implemented: " +
	"a regex literal can only be used in a `where pattern(...)` constraint"

func TestTypeCheck_StringMatchExpr_RegexPattern_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let foo = "abc123"
  match foo {
    r"[0-9]+" => "ok",
    r"[a-z]+" => "ok",
    _ => "ok",
  }
	`, false)
	// One report per arm: each is its own mistake to remove.
	assertErrorsAre(t, res, regexPatternRefused, regexPatternRefused)
}

func TestTypeCheck_StringMatchExpr_RegexOnlyNoWildcard_StillNotExhaustive(t *testing.T) {
	// Regex arms never made a string match exhaustive on their own — even if they
	// collectively cover everything, we don't try to prove it — and that is
	// unchanged by the refusal: the arms are rejected *and* the match still wants
	// a catch-all. Worth keeping as a pair, since a refusal that also silenced the
	// exhaustiveness analysis would hide a second mistake behind the first.
	res := parseCollectAndCheck(t, `
  let foo = "abc"
  match foo {
    r"[0-9]+" => "ok",
    r"[a-z]+" => "ok",
  }
	`, false)
	// assertErrorsAre matches the full diagnostic set in order, warnings included.
	assertErrorsAre(t, res, regexPatternRefused, regexPatternRefused,
		"match on string type is not exhaustive: add a wildcard `_ => ...` or catch-all arm")
}

func TestTypeCheck_StringMatchExpr_MixedLiteralAndRegex_OnlyRegexRefused(t *testing.T) {
	// The string-literal arm is untouched — only the regex arm is refused.
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    "exact" => "ok",
    r"[A-Z][a-z]+" => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, regexPatternRefused)
}

func TestTypeCheck_StringMatchExpr_InvalidRegex_RefusedNotValidated(t *testing.T) {
	// A *malformed* regex in pattern position now draws the refusal rather than
	// `invalid regex pattern`, and that is deliberate: the construct has no
	// meaning at all, so a second error about the pattern's contents would be a
	// double report on one mistake. Compile-time syntax validation stays exactly
	// where it does real work — `where pattern(r"…")`, which is unaffected (see
	// TestTypeCheck_PatternConstraint_InvalidPattern_Error in regex_test.go).
	res := parseCollectAndCheck(t, `
  let foo = "hello"
  match foo {
    r"a*?b" => "ok",
    _ => "ok",
  }
	`, false)
	assertErrorsAre(t, res, regexPatternRefused)
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
