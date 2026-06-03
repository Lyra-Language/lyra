package regex

import (
	"reflect"
	"testing"
)

// ── helpers ─────────────────────────────────────────────────────────────────

func mustCompile(t *testing.T, pattern string) *Regex {
	t.Helper()
	re, err := Compile(pattern)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}
	return re
}

func mustCompileOpts(t *testing.T, pattern string, opts Options) *Regex {
	t.Helper()
	re, err := CompileWithOptions(pattern, opts)
	if err != nil {
		t.Fatalf("Compile(%q): %v", pattern, err)
	}
	return re
}

func assertMatch(t *testing.T, re *Regex, input string, want bool) {
	t.Helper()
	got, err := re.MatchString(input)
	if err != nil {
		t.Fatalf("IsMatch(%q) on /%s/: %v", input, re.Pattern(), err)
	}
	if got != want {
		t.Errorf("IsMatch(%q) on /%s/ = %v, want %v", input, re.Pattern(), got, want)
	}
}

func assertFindAll(t *testing.T, re *Regex, input string, want []string) {
	t.Helper()
	gotBytes, err := re.FindAll([]byte(input))
	if err != nil {
		t.Fatalf("FindAll(%q) on /%s/: %v", input, re.Pattern(), err)
	}
	got := make([]string, len(gotBytes))
	for i, b := range gotBytes {
		got[i] = string(b)
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindAll(%q) on /%s/ = %q, want %q", input, re.Pattern(), got, want)
	}
}

// ── basic literals and concatenation ────────────────────────────────────────

func TestIsMatch_Literal(t *testing.T) {
	re := mustCompile(t, `hello`)
	assertMatch(t, re, "hello", true)
	assertMatch(t, re, "hell", false)
	assertMatch(t, re, "hellos", false)
	assertMatch(t, re, "", false)
}

func TestIsMatch_Empty(t *testing.T) {
	re := mustCompile(t, ``)
	assertMatch(t, re, "", true)
	assertMatch(t, re, "x", false)
}

// ── character classes and shorthands ────────────────────────────────────────

func TestIsMatch_DigitClass(t *testing.T) {
	re := mustCompile(t, `\d+`)
	assertMatch(t, re, "123", true)
	assertMatch(t, re, "0", true)
	assertMatch(t, re, "", false)
	assertMatch(t, re, "1a", false)
}

func TestIsMatch_WordClass(t *testing.T) {
	re := mustCompile(t, `\w+`)
	assertMatch(t, re, "abc_123", true)
	assertMatch(t, re, "hello-world", false) // '-' not in word
}

func TestIsMatch_NegatedClass(t *testing.T) {
	re := mustCompile(t, `[^aeiou]+`)
	assertMatch(t, re, "xyz", true)
	assertMatch(t, re, "xayz", false)
}

func TestIsMatch_Range(t *testing.T) {
	re := mustCompile(t, `[a-z]+`)
	assertMatch(t, re, "abc", true)
	assertMatch(t, re, "abC", false)
}

// ── alternation and grouping ────────────────────────────────────────────────

func TestIsMatch_Alternation(t *testing.T) {
	re := mustCompile(t, `cat|dog|bird`)
	assertMatch(t, re, "cat", true)
	assertMatch(t, re, "dog", true)
	assertMatch(t, re, "bird", true)
	assertMatch(t, re, "fish", false)
}

func TestIsMatch_Group(t *testing.T) {
	re := mustCompile(t, `(ab|cd)+`)
	assertMatch(t, re, "abcdab", true)
	assertMatch(t, re, "ab", true)
	assertMatch(t, re, "", false)
	assertMatch(t, re, "abc", false)
}

// ── quantifiers ─────────────────────────────────────────────────────────────

func TestIsMatch_Quantifiers(t *testing.T) {
	cases := []struct {
		pat   string
		input string
		want  bool
	}{
		{`a*`, "", true},
		{`a*`, "aaaa", true},
		{`a+`, "", false},
		{`a+`, "a", true},
		{`a?`, "", true},
		{`a?`, "a", true},
		{`a?`, "aa", false},
		{`a{3}`, "aaa", true},
		{`a{3}`, "aa", false},
		{`a{2,4}`, "aa", true},
		{`a{2,4}`, "aaaa", true},
		{`a{2,4}`, "aaaaa", false},
		{`a{2,}`, "aaaaaaa", true},
		{`a{2,}`, "a", false},
	}
	for _, c := range cases {
		re := mustCompile(t, c.pat)
		assertMatch(t, re, c.input, c.want)
	}
}

// ── resharp extensions ──────────────────────────────────────────────────────

func TestResharp_AnyByteWildcard(t *testing.T) {
	re := mustCompile(t, `_*`)
	assertMatch(t, re, "", true)
	assertMatch(t, re, "hello", true)
	assertMatch(t, re, "hi\nbye", true) // _ matches '\n'
}

func TestResharp_DotVsUnderscore(t *testing.T) {
	// '.' does NOT match '\n' by default; '_' always does.
	dot := mustCompile(t, `.+`)
	und := mustCompile(t, `_+`)
	assertMatch(t, dot, "abc\ndef", false)
	assertMatch(t, und, "abc\ndef", true)
}

func TestResharp_DotAllFlag(t *testing.T) {
	re := mustCompile(t, `(?s).+`)
	assertMatch(t, re, "abc\ndef", true)
}

func TestResharp_Intersection_Password(t *testing.T) {
	// 8+ alphanumeric, must contain a digit, must contain an uppercase letter.
	// This is the canonical resharp readme example.
	re := mustCompile(t, `[A-Za-z0-9]{8,}&_*[0-9]_*&_*[A-Z]_*`)
	assertMatch(t, re, "Hunter2024", true)
	assertMatch(t, re, "hunter2024", false) // no uppercase
	assertMatch(t, re, "HUNTERONLY", false) // no digit
	assertMatch(t, re, "Short1", false)     // too short
	assertMatch(t, re, "Has-Dash1", false)  // non-alphanumeric
}

func TestResharp_Complement_NoConsecutiveDigits(t *testing.T) {
	// "string that does NOT contain two consecutive digits"
	re := mustCompile(t, `~(_*\d\d_*)`)
	assertMatch(t, re, "abc 1 def 2 ghi", true)
	assertMatch(t, re, "year 2024", false)
	assertMatch(t, re, "", true)
}

func TestResharp_Complement_NotContaining(t *testing.T) {
	// "does NOT contain 'cat'"
	re := mustCompile(t, `~(_*cat_*)`)
	assertMatch(t, re, "the dog ran", true)
	assertMatch(t, re, "the cat ran", false)
}

func TestResharp_Intersection_StartsAndDoesntEnd(t *testing.T) {
	// "starts with F, does not end with Finn"
	re := mustCompile(t, `F.*&~(_*Finn)`)
	assertMatch(t, re, "Fido", true)
	assertMatch(t, re, "Finn", false)
	assertMatch(t, re, "FionaFinn", false)
	assertMatch(t, re, "Finlay", true)
}

func TestResharp_LeftmostLongest_Alternation(t *testing.T) {
	// 'y|yes' matches "yes" not "y" — order-independent, longest wins.
	re := mustCompile(t, `y|yes`)
	matches, err := re.FindAll([]byte("yes please"))
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(matches) == 0 || string(matches[0]) != "yes" {
		t.Errorf("FindAll y|yes on \"yes please\" first match = %q, want %q", matches, "yes")
	}
}

// ── anchors ─────────────────────────────────────────────────────────────────

func TestAnchors_BeginEndText(t *testing.T) {
	re := mustCompile(t, `\Ahello\z`)
	assertMatch(t, re, "hello", true)
	assertMatch(t, re, "hello world", false)
	assertMatch(t, re, " hello", false)
}

func TestAnchors_LineMultiline(t *testing.T) {
	re := mustCompile(t, `^hello$`)
	// In multiline mode, the whole input "hello\nworld" matches?
	// No: IsMatch is whole-input. But find_all should locate "hello".
	assertMatch(t, re, "hello", true)
	assertFindAll(t, re, "hello\nworld", []string{"hello"})
	assertFindAll(t, re, "say hello\nworld", nil)
}

func TestAnchors_FindAll_Caret(t *testing.T) {
	re := mustCompile(t, `^\w+`)
	assertFindAll(t, re, "abc\ndef\nghi", []string{"abc", "def", "ghi"})
}

func TestAnchors_FindAll_Dollar(t *testing.T) {
	re := mustCompile(t, `\w+$`)
	assertFindAll(t, re, "abc\ndef\nghi", []string{"abc", "def", "ghi"})
}

// ── FindAll: leftmost-longest, non-overlapping ──────────────────────────────

func TestFindAll_Basic(t *testing.T) {
	re := mustCompile(t, `\d+`)
	assertFindAll(t, re, "call 555-1234 or 555-5678", []string{"555", "1234", "555", "5678"})
}

func TestFindAll_LongestAtPos(t *testing.T) {
	// 'a|aa|aaa' should produce "aaa" (longest) at each non-overlapping start.
	re := mustCompile(t, `a|aa|aaa`)
	assertFindAll(t, re, "aaaa", []string{"aaa", "a"})
}

func TestFindAll_NoMatches(t *testing.T) {
	re := mustCompile(t, `xyz`)
	matches, err := re.FindAll([]byte("abc def"))
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("FindAll on no-match input = %q, want empty", matches)
	}
}

func TestFindAll_Password_InText(t *testing.T) {
	// Locate a strong-password substring in surrounding text.
	re := mustCompile(t, `[A-Za-z0-9]{8,}&_*[0-9]_*&_*[A-Z]_*`)
	matches, err := re.FindAll([]byte("try Hunter2024 or password1"))
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(matches) != 1 || string(matches[0]) != "Hunter2024" {
		t.Errorf("FindAll strong-password = %q, want [Hunter2024]", matches)
	}
}

// ── flags ───────────────────────────────────────────────────────────────────

func TestFlags_CaseInsensitive_Inline(t *testing.T) {
	re := mustCompile(t, `(?i)hello`)
	assertMatch(t, re, "Hello", true)
	assertMatch(t, re, "HELLO", true)
	assertMatch(t, re, "hello", true)
}

func TestFlags_CaseInsensitive_Option(t *testing.T) {
	opts := DefaultOptions()
	opts.CaseInsensitive = true
	re := mustCompileOpts(t, `hello`, opts)
	assertMatch(t, re, "HeLLo", true)
}

func TestFlags_CaseInsensitive_InClass(t *testing.T) {
	re := mustCompile(t, `(?i)[a-f]+`)
	assertMatch(t, re, "ABCdef", true)
}

func TestFlags_IgnoreWhitespace(t *testing.T) {
	re := mustCompile(t, `(?x) \d+ # match digits`)
	assertMatch(t, re, "123", true)
}

func TestFlags_MultilineOff(t *testing.T) {
	// (?-m): ^ now anchors only to start of text
	re := mustCompile(t, `(?-m)^hello`)
	assertFindAll(t, re, "x\nhello", nil) // ^ no longer fires after '\n'
	assertFindAll(t, re, "hello", []string{"hello"})
}

// ── errors ──────────────────────────────────────────────────────────────────

func TestErrors_LazyQuantifier(t *testing.T) {
	if _, err := Compile(`a*?`); err == nil {
		t.Error("expected error for lazy quantifier")
	}
}

func TestErrors_Backreference(t *testing.T) {
	if _, err := Compile(`a\1`); err == nil {
		t.Error("expected error for backreference")
	}
}

// ── lookarounds ──────────────────────────────────────────────────────────────

// Positive lookahead inlined as rest ∩ R·_*:
// (?=abc)abcdef == abcdef ∩ abc·_* == abcdef.
func TestLookAhead_Positive_Leading(t *testing.T) {
	re := mustCompile(t, `(?=\w)\w{3}`)
	assertMatch(t, re, "abc", true)
	assertMatch(t, re, "ab", false)
	assertFindAll(t, re, "foobar", []string{"foo", "bar"})
}

// Negative lookahead at leading position.
func TestLookAhead_Negative_Leading(t *testing.T) {
	// (?!foo)\w+ — word run that does NOT start with "foo".
	re := mustCompile(t, `(?!foo)\w+`)
	assertMatch(t, re, "bar", true)
	assertMatch(t, re, "foo", false)
	assertMatch(t, re, "foobar", false)
}

// Middle lookahead inlined as A·(B ∩ R·_*):
// hello(?=world)world == hello·(world ∩ world·_*) == helloworld.
func TestLookAhead_Positive_Middle(t *testing.T) {
	re := mustCompile(t, `hello(?=world)world`)
	assertMatch(t, re, "helloworld", true)
	assertMatch(t, re, "helloxorld", false)
}

// Trailing positive lookahead in FindAll:
// \w+(?=:) matches word runs immediately before a colon (colon not consumed).
func TestLookAhead_Positive_Trailing_FindAll(t *testing.T) {
	re := mustCompile(t, `\w+(?=:)`)
	assertFindAll(t, re, "foo:bar:baz", []string{"foo", "bar"})
	assertFindAll(t, re, "nocolon", nil)
}

// Trailing negative lookahead:
// foo(?!bar) matches "foo" not followed by "bar".
func TestLookAhead_Negative_Trailing_FindAll(t *testing.T) {
	re := mustCompile(t, `foo(?!bar)`)
	assertFindAll(t, re, "foobar foobaz", []string{"foo"})
}

// Trailing positive lookahead in IsMatch.
// \w+(?=\d) in full-string mode: last character of \w+ must be followed by
// another digit — but in IsMatch there is nothing after the match, so this
// resolves to \w+ ∩ ~(...) depending on where the lookahead is placed.
// A simpler smoke test: (?=abc)abc matches "abc".
func TestLookAhead_Positive_Trailing_IsMatch(t *testing.T) {
	re := mustCompile(t, `\w+(?=!)`)
	// In IsMatch, nothing follows the match, so '!' is never there.
	assertMatch(t, re, "hello!", false) // "hello!" — '!' is not \w so \w+ = "hello", then nothing follows
	// In FindAll "hello!" the match is "hello".
	assertFindAll(t, re, "hello! world!", []string{"hello", "world"})
}

// Non-leading positive lookbehind inlined as (A ∩ _*·R)·B.
// \w+(?<=\d) — word run that ends with a digit.
func TestLookBehind_Positive_Trailing(t *testing.T) {
	re := mustCompile(t, `\w+(?<=\d)`)
	assertFindAll(t, re, "abc123 def456 ghi", []string{"abc123", "def456"})
	assertMatch(t, re, "abc123", true)
	assertMatch(t, re, "abc", false)
}

// Middle lookbehind: hello(?<=llo)world.
func TestLookBehind_Positive_Middle(t *testing.T) {
	re := mustCompile(t, `hello(?<=llo)world`)
	assertMatch(t, re, "helloworld", true)
	assertMatch(t, re, "helXoworld", false)
}

// Leading positive lookbehind uses the gate DFA.
// (?<=\d)\w+ — word chars immediately after a digit.
func TestLookBehind_Leading_Positive_FindAll(t *testing.T) {
	re := mustCompile(t, `(?<=\d)\w+`)
	assertFindAll(t, re, "a1foo b2bar", []string{"foo", "bar"})
	assertFindAll(t, re, "nopreceding", nil)
}

// Leading negative lookbehind: (?<!\d)\w+ — word run NOT preceded by a digit.
func TestLookBehind_Leading_Negative_FindAll(t *testing.T) {
	re := mustCompile(t, `(?<!\d)\w+`)
	// "foo" at pos 0: nothing precedes it → gate passes (not a digit).
	// "1bar" at pos 4: space precedes '1' → gate passes, so the whole run "1bar" matches.
	// "bar" at pos 5 is NOT scanned because pos 5 is preceded by '1' (digit) —
	// the gate skips it, preventing a separate "bar" match.
	assertFindAll(t, re, "foo 1bar", []string{"foo", "1bar"})
	// Simple case: neither word run is preceded by a digit.
	assertFindAll(t, re, "foo bar", []string{"foo", "bar"})
}

// Quantifier on a lookaround must be an error.
func TestErrors_LookAround_Quantifier(t *testing.T) {
	if _, err := Compile(`(?=foo)+`); err == nil {
		t.Error("expected error for quantifier on lookahead")
	}
}

// Combined lookahead + intersection: password-like test.
func TestLookAhead_Combined_Intersection(t *testing.T) {
	// Must start with an uppercase letter and contain a digit.
	re := mustCompile(t, `(?=[A-Z])\w+&_*\d_*`)
	assertMatch(t, re, "Hello1", true)
	assertMatch(t, re, "hello1", false) // no uppercase start
	assertMatch(t, re, "HELLO", false)  // no digit
}

func TestErrors_BadComplement(t *testing.T) {
	if _, err := Compile(`~a`); err == nil {
		t.Error("expected error: ~ without parentheses")
	}
}

func TestErrors_UnterminatedClass(t *testing.T) {
	if _, err := Compile(`[abc`); err == nil {
		t.Error("expected error for unterminated character class")
	}
}

func TestErrors_UnclosedGroup(t *testing.T) {
	if _, err := Compile(`(abc`); err == nil {
		t.Error("expected error for unclosed group")
	}
}

// ── empty language and ⊥ ────────────────────────────────────────────────────

func TestEmptyLanguage_Complement(t *testing.T) {
	// ~(_*) is the empty language: never matches anything.
	re := mustCompile(t, `~(_*)`)
	assertMatch(t, re, "", false)
	assertMatch(t, re, "anything", false)
}

func TestAllStrings_Complement(t *testing.T) {
	// _* is the all-strings language: always matches.
	re := mustCompile(t, `_*`)
	assertMatch(t, re, "", true)
	assertMatch(t, re, "anything\nat all", true)
}

// ── stress: derivative-based DFA convergence ────────────────────────────────

func TestStress_NestedRepeats(t *testing.T) {
	re := mustCompile(t, `(\d+|[a-z]+)+`)
	assertMatch(t, re, "abc123def", true)
	assertMatch(t, re, "abc-def", false)
}

func TestStress_IntersectionLength(t *testing.T) {
	re := mustCompile(t, `_{5,30}&_*[A-Z]_*&_*[a-z]_*&_*[0-9]_*&_*[!@#$%]_*`)
	assertMatch(t, re, "Aa1!Z", true)
	assertMatch(t, re, "Aa1!", false) // too short
	assertMatch(t, re, "AAAAA", false)
}

// ── Phase 3: Unicode properties ──────────────────────────────────────────────

// \p{L} / \p{Letter}: any Unicode letter (ASCII or multi-byte).
func TestUnicode_LetterProperty(t *testing.T) {
	re := mustCompile(t, `\p{L}+`)
	assertMatch(t, re, "hello", true)       // ASCII letters
	assertMatch(t, re, "caf\xC3\xA9", true) // UTF-8 for "café" (é = U+00E9)
	// Greek alpha α (U+03B1) = 0xCE 0xB1
	assertMatch(t, re, "\xCE\xB1\xCE\xB2\xCE\xB3", true) // αβγ
	assertMatch(t, re, "123", false)                     // digits are not letters
	assertMatch(t, re, "", false)
	assertMatch(t, re, "hello world", false) // space is not a letter
}

// \p{Lu}: uppercase letters only.
func TestUnicode_UppercaseLetterProperty(t *testing.T) {
	re := mustCompile(t, `\p{Lu}+`)
	assertMatch(t, re, "HELLO", true)
	assertMatch(t, re, "hello", false) // lowercase letters
	assertMatch(t, re, "Hello", false) // mixed: 'e' breaks the match
	// Greek capital Γ (U+0393) = 0xCE 0x93
	assertMatch(t, re, "\xCE\x93", true)
}

// \p{Ll}: lowercase letters only.
func TestUnicode_LowercaseLetterProperty(t *testing.T) {
	re := mustCompile(t, `\p{Ll}+`)
	assertMatch(t, re, "hello", true)
	assertMatch(t, re, "HELLO", false)
	// é (U+00E9) = 0xC3 0xA9, a lowercase letter.
	assertMatch(t, re, "caf\xC3\xA9", true)
}

// \p{Nd} / \p{Decimal_Number}: Unicode decimal digits.
func TestUnicode_DecimalDigitProperty(t *testing.T) {
	re := mustCompile(t, `\p{Nd}+`)
	assertMatch(t, re, "0123456789", true) // ASCII digits
	assertMatch(t, re, "abc", false)
	assertMatch(t, re, "", false)
	// Arabic-Indic digit four (U+0664) = 0xD9 0xA4
	assertMatch(t, re, "\xD9\xA4", true)
}

// \p{N} / \p{Number}: all numeric characters (Nd, Nl, No).
func TestUnicode_NumberProperty(t *testing.T) {
	re := mustCompile(t, `\p{N}+`)
	assertMatch(t, re, "42", true)
	assertMatch(t, re, "x", false)
}

// \p{Z} / \p{Separator}: separator characters (spaces, etc.).
func TestUnicode_SeparatorProperty(t *testing.T) {
	re := mustCompile(t, `\p{Z}+`)
	assertMatch(t, re, " ", true) // ASCII space (U+0020) is a Zs
	assertMatch(t, re, "a", false)
	// Non-breaking space U+00A0 = 0xC2 0xA0
	assertMatch(t, re, "\xC2\xA0", true)
}

// \p{Latin}: Latin script.
func TestUnicode_ScriptLatin(t *testing.T) {
	re := mustCompile(t, `\p{Latin}+`)
	assertMatch(t, re, "hello", true)
	// é (U+00E9) is in the Latin script.
	assertMatch(t, re, "caf\xC3\xA9", true)
	// CJK character 中 (U+4E2D) is not Latin.
	assertMatch(t, re, "\xE4\xB8\xAD", false)
}

// \p{Han}: CJK unified ideographs.
func TestUnicode_ScriptHan(t *testing.T) {
	re := mustCompile(t, `\p{Han}+`)
	// 中 (U+4E2D) = 0xE4 0xB8 0xAD — a CJK character
	assertMatch(t, re, "\xE4\xB8\xAD", true)
	assertMatch(t, re, "hello", false)
}

// \P{L}: complement — matches any byte sequence that is NOT a letter encoding.
func TestUnicode_NegatedLetterProperty(t *testing.T) {
	re := mustCompile(t, `\P{L}+`)
	assertMatch(t, re, "123", true)
	assertMatch(t, re, "   ", true) // spaces
	assertMatch(t, re, "hello", false)
}

// \p{L} inside a (non-negated) character class: [\p{L}_] matches a letter or '_'.
func TestUnicode_PropertyInClass(t *testing.T) {
	re := mustCompile(t, `[\p{L}_]+`)
	assertMatch(t, re, "hello_world", true)
	assertMatch(t, re, "_", true)
	assertMatch(t, re, "caf\xC3\xA9", true)
	assertMatch(t, re, "123", false) // digits not matched
}

// Multiple Unicode properties in one class: [\p{L}\p{Nd}].
func TestUnicode_MultiplePropertiesInClass(t *testing.T) {
	re := mustCompile(t, `[\p{L}\p{Nd}]+`)
	assertMatch(t, re, "hello123", true)
	assertMatch(t, re, "caf\xC3\xA9", true)
	assertMatch(t, re, "!", false)
	assertMatch(t, re, " ", false)
}

// Negated class with Unicode property: [^\p{Z}] matches one non-separator code point.
func TestUnicode_NegatedClassWithProperty(t *testing.T) {
	// [^\p{Z}]+ should match sequences of non-separator code points.
	re := mustCompile(t, `[^\p{Z}]+`)
	assertMatch(t, re, "hello", true)
	assertMatch(t, re, "hello world", false) // space (U+0020) breaks it
	assertFindAll(t, re, "foo bar baz", []string{"foo", "bar", "baz"})
}

// Mixed class: byte ranges + Unicode property.
func TestUnicode_MixedByteAndProperty(t *testing.T) {
	re := mustCompile(t, `[a-z\p{Lu}]+`) // lowercase ASCII OR Unicode uppercase
	assertMatch(t, re, "helloWORLD", true)
	assertMatch(t, re, "HELLO", true)
	assertMatch(t, re, "hello", true)
	assertMatch(t, re, "1234", false)
	// Greek capital Γ (U+0393) = 0xCE 0x93 — in Lu
	assertMatch(t, re, "\xCE\x93", true)
}

// Long name aliases.
func TestUnicode_LongNameAliases(t *testing.T) {
	re1 := mustCompile(t, `\p{Letter}+`)
	re2 := mustCompile(t, `\p{L}+`)
	// Both should accept the same inputs.
	for _, s := range []string{"hello", "\xCE\xB1", "123", ""} {
		got1, _ := re1.MatchString(s)
		got2, _ := re2.MatchString(s)
		if got1 != got2 {
			t.Errorf("MatchString(%q): \\p{Letter}=%v but \\p{L}=%v", s, got1, got2)
		}
	}

	re3 := mustCompile(t, `\p{Decimal_Number}+`)
	re4 := mustCompile(t, `\p{Nd}+`)
	for _, s := range []string{"0123", "\xD9\xA4", "abc", ""} {
		got3, _ := re3.MatchString(s)
		got4, _ := re4.MatchString(s)
		if got3 != got4 {
			t.Errorf("MatchString(%q): \\p{Decimal_Number}=%v but \\p{Nd}=%v", s, got3, got4)
		}
	}
}

// Unicode property combined with quantifier and intersection.
func TestUnicode_PropertyWithIntersection(t *testing.T) {
	// Unicode letters that are at least 3 code-points long.
	// \p{L}{3,} & ~(_*\p{Lu}_*)  → letters-only, no uppercase.
	re := mustCompile(t, `\p{L}{3,}&~(_*\p{Lu}_*)`)
	assertMatch(t, re, "hello", true)  // all lowercase, ≥3 chars
	assertMatch(t, re, "Hello", false) // has uppercase H
	assertMatch(t, re, "hi", false)    // too short
}

// Error: unknown property name.
func TestUnicode_UnknownProperty(t *testing.T) {
	if _, err := Compile(`\p{Bogus}`); err == nil {
		t.Error("expected error for unknown Unicode property")
	}
}

// Error: missing braces after \p.
func TestUnicode_MissingBraces(t *testing.T) {
	if _, err := Compile(`\pL`); err == nil {
		t.Error("expected error: \\p without '{}'")
	}
}

// Error: empty property name.
func TestUnicode_EmptyPropertyName(t *testing.T) {
	if _, err := Compile(`\p{}`); err == nil {
		t.Error("expected error: empty property name")
	}
}

// Error: Unicode property at lower bound of range in class.
func TestUnicode_PropertyAsRangeLower(t *testing.T) {
	if _, err := Compile(`[\p{L}-z]`); err == nil {
		t.Error("expected error: \\p{L} cannot be lower bound of range")
	}
}

// FindAll with \p{L}+ — non-overlapping letter words.
func TestUnicode_FindAllLetters(t *testing.T) {
	re := mustCompile(t, `\p{L}+`)
	assertFindAll(t, re, "hello 123 world", []string{"hello", "world"})
	// Mixed ASCII + non-ASCII words
	// "foo" + space + "\xCE\xB1\xCE\xB2" (αβ) + space + "bar"
	assertFindAll(t, re,
		"foo \xCE\xB1\xCE\xB2 bar",
		[]string{"foo", "\xCE\xB1\xCE\xB2", "bar"},
	)
}
