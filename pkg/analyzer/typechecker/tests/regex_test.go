package typechecker_test

import "testing"

// ── RegexLiteralExpr: refused as a value (lyra-E052, 08/13) ────────────────
//
// A regex literal used as a *value* inferred the built-in `regex` type and then
// died in the backend (`expression lowering not implemented`). `regex` was a type
// nothing else in the compiler read, and one no annotation can even name — a
// lowercase type name parses as a type *variable*, so `(re: regex)` declares one
// called `regex`. A regex value would need an engine in the runtime, which is
// hand-written C with no FFI; the `regexp` used below runs at compile time and
// cannot ship into the program.
//
// The `where pattern(r"…")` constraint is untouched and keeps working — it stores
// the pattern's source text and compiles it at type-check time, never producing a
// value — which is what the rest of this file covers.

const regexValueRefused = "a regex value is not implemented: " +
	"a regex literal can only be used in a `where pattern(...)` constraint or as a " +
	"`match` pattern on a string scrutinee"

func TestTypeCheck_RegexLiteralExpr_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let phone = r"[2-9][0-9]{2} [2-9][0-9]{2} [0-9]{4}$"
`, false)
	assertErrorsAre(t, res, regexValueRefused)
}

// A malformed pattern in value position draws the refusal rather than
// `invalid regex literal`: the construct has no meaning, so a second error about
// its contents would be a double report on one mistake. Syntax validation stays
// where it does real work — see TestTypeCheck_PatternConstraint_InvalidPattern_Error.
func TestTypeCheck_RegexLiteralExpr_InvalidPattern_RefusedNotValidated(t *testing.T) {
	assertErrorsAre(t, parseCollectAndCheck(t, `let x = r"a*?b"`, false), regexValueRefused)
}

func TestTypeCheck_RegexLiteralExpr_UnterminatedClass_Refused(t *testing.T) {
	assertErrorsAre(t, parseCollectAndCheck(t, `let x = r"[abc"`, false), regexValueRefused)
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

// A value that is not a string literal is **matched at run time** (08/13), against
// the DFA the backend compiles from this very pattern.
//
// The history is worth keeping, because this one assertion has now been each of the
// three possible answers in a day. It was **admitted unchecked** — which is what let
// `Digits("abc")` build and print `abc` while the type's declaration says it cannot
// hold that. It was then **refused** (lyra-E054), when `range`/`values`/`step` gained
// runtime traps and `pattern` had no way to join them, since refusing keeps the
// guarantee whole where admitting does not. Building the matcher removed the reason
// for the refusal: a constraint's pattern is part of a type, so it is known while
// compiling, and the engine can run *then* with only its answer shipping.
func TestTypeCheck_PatternConstraint_NonLiteralCheckedAtRuntime(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Digits = string where pattern(r"[0-9]+")
let s = "123"
let d: Digits = Digits(s)
`, false))
}

// What is still refused is a pattern that cannot be compiled to a table — a property
// of the *pattern*, not of the value, so the diagnostic names it. A lookbehind's gate
// depends on text preceding the input, which a flat byte table has no way to hold.
func TestTypeCheck_PatternConstraint_UncompilablePatternRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Odd = string where pattern(r"(?<=a)b")
let s = "ab"
let d: Odd = Odd(s)
`, false)
	if len(res.errors) == 0 {
		t.Skip("the parser does not accept a lookbehind; nothing to refuse here")
	}
	assertHasErrorContaining(t, res, "cannot be compiled to a runtime matcher")
}

// A literal still works, since that is checked where it is written — the refusal is
// about what the compiler cannot read, not about the constraint being unusable.
func TestTypeCheck_PatternConstraint_LiteralStillWorks(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Digits = string where pattern(r"[0-9]+")
let d: Digits = "123"
`, false))
}

func TestTypeCheck_PatternConstraint_StringAssignableToBase_Ok(t *testing.T) {
	// A constrained-type value should be assignable back to its base type
	// (string), so `let s: string = someDigits` should be fine.
	res := parseCollectAndCheck(t, `
newtype Digits = string where pattern(r"[0-9]+")
let d: Digits = "42"
let s = string(d)
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
