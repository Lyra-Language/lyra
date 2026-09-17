package llvm

import (
	"strings"
	"testing"
)

// `lines`, `strip_prefix` and `strip_suffix` (`std/prelude/strings.lyra`), added 09/17
// because `examples/lyra-md` wrote all three by hand — which is what that example is for.
//
// Parts are joined with `|` and bracketed, as `split_when`'s test does and for the same
// reason: an empty line and a dropped line are different bugs, and `[a||b]` tells them
// apart where a length check would not.
func TestExec_Lines(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{"two lines", "a\nb", "[a|b]"},

		// **A trailing newline does not make a last empty line.** A file written by any
		// editor ends in one, so the alternative would give almost every real file a
		// phantom final line — which a renderer turns into an empty paragraph and a
		// counter reports as one line too many.
		{"a trailing newline", "a\nb\n", "[a|b]"},
		{"only a newline", "\n", "[]"},

		// An interior blank line *is* a line: it is what separates blocks in Markdown and
		// paragraphs in text, so dropping it would be dropping content.
		{"a blank line between two", "a\n\nb", "[a||b]"},
		{"two blank lines", "a\n\n\nb", "[a|||b]"},
		{"a leading blank line", "\na", "[|a]"},

		// **CRLF is one terminator.** A file written on Windows otherwise leaves an
		// invisible byte at the end of every line, where it silently breaks a comparison
		// against text written anywhere else — the failure that looks like a bug in
		// whatever compares.
		{"CRLF", "a\r\nb\r\n", "[a|b]"},
		{"a bare CR mid-line stays", "a\rb\n", "[a\rb]"},
		{"a line that is only CR", "a\n\r\n", "[a|]"},

		// Degenerate: an empty string has no lines, not one empty line.
		{"an empty string", "", "[]"},
		{"no terminator at all", "abc", "[abc]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  println("[" ++ ` + quoteLyra(c.input) + `.lines().join("|") ++ "]")
}
`
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("lines(%q) = %s; want %s", c.input, got, c.want)
			}
		})
	}
}

// `strip_prefix`/`strip_suffix` answer a `Maybe` so that "it was not there" is a case the
// caller handles rather than a length the caller computes. The pair of tests that matter
// are the ones where the affix is the whole string (the answer is `""`, not `None`) and
// where it is absent (the answer is `None`, not the string unchanged) — a version built
// from `starts_with` plus `slice` at the call site gets the second one right by accident
// and the first one wrong when the lengths are written twice.
func TestExec_StripPrefixAndSuffix(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show = (m: Maybe<string>) -> string => match m { Some s => "[" ++ s ++ "]", None => "none" }
let main = () -> void => {
  println(show("--flag".strip_prefix("--")));
  println(show("flag".strip_prefix("--")));
  println(show("--".strip_prefix("--")));
  println(show("x".strip_prefix("--")));
  println(show("post.md".strip_suffix(".md")));
  println(show("post.md".strip_suffix(".lyra")));
  println(show(".md".strip_suffix(".md")));
  println(show("".strip_prefix("")));
  println(show("abc".strip_prefix("")));
  println("héllo".strip_prefix("hé").unwrap_or("?"));
}
`
	want := strings.Join([]string{
		"[flag]", "none", "[]", "none",
		"[post]", "none", "[]",
		"[]", "[abc]",
		// Multi-byte: the slice is rune-indexed, so a two-rune prefix whose second rune is
		// two bytes must remove two runes and not two bytes.
		"llo",
	}, "\n")
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("strip_prefix/strip_suffix =\n%q\nwant\n%q", got, want)
	}
}
