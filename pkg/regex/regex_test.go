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
	// {5,30} chars, must include an upper, lower, digit, and special.
	re := mustCompile(t, `_{5,30}&_*[A-Z]_*&_*[a-z]_*&_*[0-9]_*&_*[!@#$%]_*`)
	assertMatch(t, re, "Aa1!Z", true)
	assertMatch(t, re, "Aa1!", false) // too short
	assertMatch(t, re, "AAAAA", false)
}
