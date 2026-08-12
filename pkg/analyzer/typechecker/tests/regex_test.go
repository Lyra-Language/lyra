package typechecker_test

import "testing"

// ── RegexLiteralExpr: type inference and compile-time validation ────────────

func TestTypeCheck_RegexLiteralExpr_Valid_Ok(t *testing.T) {
	// A well-formed regex literal should infer the built-in `regex` type with
	// no type errors.
	res := parseCollectAndCheck(t, `
let phone = r"[2-9][0-9]{2} [2-9][0-9]{2} [0-9]{4}$"
`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_RegexLiteralExpr_InvalidPattern_Error(t *testing.T) {
	// Lazy quantifiers are rejected by the engine; the error should surface
	// at compile time when the literal is inferred.
	res := parseCollectAndCheck(t, `let x = r"a*?b"`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected a compile-time error for invalid regex literal, got none")
	}
	found := false
	for _, e := range res.errors {
		if containsSubstring(e.Message, "invalid regex literal") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning 'invalid regex literal', got: %+v", res.errors)
	}
}

func TestTypeCheck_RegexLiteralExpr_UnterminatedClass_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = r"[abc"`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected a compile-time error for malformed regex literal, got none")
	}
}

// ── PatternConstraint on type declarations ──────────────────────────────────
//
// Lyra syntax: newtype Name = string where pattern(r"…")
// The PatternConstraint.Pattern field stores the full r"…" literal text;
// the typechecker strips the r" and " delimiters before calling regex.Compile.

func TestTypeCheck_PatternConstraint_ValidDecl_Ok(t *testing.T) {
	// A well-formed pattern constraint on a type declaration should produce no
	// type errors.
	res := parseCollectAndCheck(t, `
newtype Slug = string where pattern(r"[a-z][a-z0-9-]*")
`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_PatternConstraint_InvalidPattern_Error(t *testing.T) {
	// An invalid regex in a pattern constraint should be flagged at the type
	// declaration site.
	res := parseCollectAndCheck(t, `newtype Bad = string where pattern(r"a*?")`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected a compile-time error for invalid pattern constraint, got none")
	}
	found := false
	for _, e := range res.errors {
		if containsSubstring(e.Message, "invalid pattern constraint") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning 'invalid pattern constraint', got: %+v", res.errors)
	}
}

// ── PatternConstraint: value assignment checking ────────────────────────────

func TestTypeCheck_PatternConstraint_MatchingLiteral_Ok(t *testing.T) {
	// A string literal that satisfies the pattern constraint should be accepted.
	res := parseCollectAndCheck(t, `
newtype Hex = string where pattern(r"[0-9a-fA-F]+")
let color: Hex = "1a2b3c"
`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_PatternConstraint_NonMatchingLiteral_Error(t *testing.T) {
	// A string literal that does NOT match the pattern should produce an error.
	res := parseCollectAndCheck(t, `
newtype Digits = string where pattern(r"[0-9]+")
let bad: Digits = "abc"
`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected a pattern mismatch error, got none")
	}
	found := false
	for _, e := range res.errors {
		if containsSubstring(e.Message, "does not satisfy pattern constraint") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning 'does not satisfy pattern constraint', got: %+v", res.errors)
	}
}

func TestTypeCheck_PatternConstraint_EmptyString_Error(t *testing.T) {
	// The pattern .+ requires at least one character; empty string should fail.
	res := parseCollectAndCheck(t, `
newtype NonEmpty = string where pattern(r".+")
let x: NonEmpty = ""
`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected pattern mismatch error for empty string, got none")
	}
}

func TestTypeCheck_PatternConstraint_NonLiteralNotChecked_Ok(t *testing.T) {
	// When the assigned value is not a string literal (e.g., a variable), the
	// pattern constraint cannot be verified statically — no error expected.
	res := parseCollectAndCheck(t, `
newtype Digits = string where pattern(r"[0-9]+")
let s = "123"
let d: Digits = Digits(s)
`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_PatternConstraint_StringAssignableToBase_Ok(t *testing.T) {
	// A constrained-type value should be assignable back to its base type
	// (string), so `let s: string = someDigits` should be fine.
	res := parseCollectAndCheck(t, `
newtype Digits = string where pattern(r"[0-9]+")
let d: Digits = "42"
let s: string = d
`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_PatternConstraint_MultiPattern_AllMustMatch(t *testing.T) {
	// "123" matches r"[0-9a-f]+" but NOT r"[a-f]+", so the second pattern
	// should produce an error.
	res := parseCollectAndCheck(t, `
newtype HexLower = string where pattern(r"[0-9a-f]+"), pattern(r"[a-f]+")
let x: HexLower = "123"
`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected a pattern mismatch error, got none")
	}
}

// Dividing by a variable named `r` is division, never a regex. Under the old
// `r/…/` delimiters this was genuinely ambiguous — `r` is an ordinary identifier
// and the regex token outranked it, so `r/2 + a/b` lexed as the regex `r/2 + a/`
// plus a stray `b`, silently. Quote delimiters remove the ambiguity outright: a
// `"` cannot follow an identifier in any valid expression, so `r"` can only ever
// start a regex. Both same-line and cross-line forms are now plain arithmetic.
func TestTypeCheck_RegexLiteral_DoesNotShadowDivision(t *testing.T) {
	res := parseCollectAndCheck(t, `
let r = 10
let a = 8
let b = 2
let ratio = r/2 + a/b
let tail = r/2
let commented = r/2 // a trailing comment's slashes are not a delimiter either
`, false)
	assertNoErrors(t, res)
}

// The delimiter change also means a `/` inside a pattern needs no escaping,
// which the old form required for every slash (`r/\/usr\/local\/bin/`).
func TestTypeCheck_RegexLiteral_SlashesNeedNoEscape(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Path = string where pattern(r"/usr/local/.*")
let p: Path = "/usr/local/bin"
`, false)
	assertNoErrors(t, res)
}

// The delimiter itself can still be matched, via `\"`.
func TestTypeCheck_RegexLiteral_EscapedQuote(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Quoted = string where pattern(r"\"[a-z]+\"")`, false)
	assertNoErrors(t, res)
}
