package llvm

import (
	"strings"
	"testing"
)

// `split_when` — `split`'s predicate form (`std/prelude/strings.lyra`).
//
// The two differ deliberately on empty parts, and that is the thing to keep pinned. The
// `Needle` `split` must preserve them, because `"a,,b"` in a CSV has a genuinely empty
// middle field. `split_when` takes a predicate meaning *"is this rune a boundary"*, and a
// run of boundaries is one boundary — so it collapses. A caller splitting words wants
// `"hello, world."` to be two words, not four.
//
// Parts are joined with `|` and bracketed rather than compared as a slice: an empty part
// and a dropped part are different bugs, and `[a||b]` distinguishes them from `[a|b]`
// where a length check alone would not.
func TestExec_SplitWhen(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{"separated words", "a b c", "[a|b|c]"},
		{"a sentence with punctuation", "hello, world.", "[hello|world]"},

		// Every part is emitted where the separator *runs*, not just where one occurs.
		// The first draft advanced `start` only when it also pushed, so a second adjacent
		// separator was neither emitted nor stepped over and leaked into the next part —
		// `"a  b"` came back as `["a", " b"]`. Silent, and worse than a dropped word: a
		// leading space makes `" world"` a different map key from `"world"`, so a
		// frequency count splits in two and still looks plausible.
		{"a run of separators collapses", "a  b", "[a|b]"},
		{"a long run collapses", "a     b", "[a|b]"},
		{"mixed adjacent separators collapse", "a, b", "[a|b]"},

		// The tail. A loop that only pushes on finding a separator drops whatever follows
		// the last one, so these two are the same bug at two severities: `"a b c"` lost
		// `c`, and a string containing no separator at all came back empty.
		{"the final part is emitted", "a b", "[a|b]"},
		{"a single word with no separator", "abc", "[abc]"},

		// Leading and trailing separators are the collapse rule at the edges: there is no
		// part before the first boundary or after the last, so neither contributes one.
		{"a leading separator", " lead", "[lead]"},
		{"a trailing separator", "trail ", "[trail]"},
		{"both", "  both  ", "[both]"},

		// Degenerate inputs. Empty in, empty out — an empty document has no words, and
		// the alternative (one part that is the empty string) would become a map key.
		{"an empty string", "", "[]"},
		{"only separators", "   ", "[]"},
		{"one separator", " ", "[]"},

		// Multi-byte runes are covered by the whitespace-boundary test below rather than
		// here: `is_ascii_alpha` is ASCII-only, so `é` is legitimately a boundary under this
		// predicate and the case would be testing the classifier, not the split.
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  let parts = ` + quoteLyra(c.input) + `.split_when((r) => !r.is_ascii_alpha())
  println("[" ++ parts.join("|") ++ "]")
}
`
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("split_when(%q) = %s; want %s", c.input, got, c.want)
			}
		})
	}
}

// The predicate is the caller's, so the same string splits differently under a different
// boundary — which is the whole reason this exists beside the `Needle` form.
func TestExec_SplitWhenPredicateChoosesTheBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, pred, want string }{
		{"on non-letters", `!r.is_ascii_alpha()`, "[ab|cd]"},
		{"on digits", `r.is_ascii_digit()`, "[ab-|-cd]"},
		{"on whitespace only", `r.is_ascii_space()`, "[ab-12-cd]"},
		{"on nothing, so the whole string is one part", `false`, "[ab-12-cd]"},
		{"on everything, so there are no parts", `true`, "[]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  let parts = "ab-12-cd".split_when((r) => ` + c.pred + `)
  println("[" ++ parts.join("|") ++ "]")
}
`
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("predicate %s = %s; want %s", c.pred, got, c.want)
			}
		})
	}
}

// The shape the function was added for: tokenize a line, fold case, count. This is the
// word-frequency example's inner loop, and it is here because the unit cases above each
// pin one rule while the bug that mattered only showed up in the composition — a leaked
// leading space is invisible until two spellings of one word land in separate buckets.
func TestExec_SplitWhenTokenizesForWordCounting(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let line = "The quick, THE lazy;  the END."
  var seen: []string = []
  for w in line.split_when((r) => !r.is_ascii_alpha()) {
    var folded = ""
    for c in w { folded = folded ++ "${c.to_ascii_lower()}" }
    seen.push(folded)
  }
  println(seen.join("|"))
}
`
	want := "the|quick|the|lazy|the|end"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("tokenized = %q; want %q", got, want)
	}
}

// A multi-byte rune is one position to both the predicate and the slice. The walk is
// rune-indexed, so a `slice` bound taken from `i` counts code points and cannot land
// mid-character — a byte-indexed loop would cut `é` in half and produce two broken runes.
//
// Split on whitespace rather than on `!is_ascii_alpha()`: `is_ascii_alpha` is ASCII-only, so under
// that predicate `é` is itself a boundary and the case would be testing the classifier
// instead of the split.
func TestExec_SplitWhenIsRuneIndexed(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let parts = "héllo wörld ünïcode".split_when((r) => r.is_ascii_space())
  println("[" ++ parts.join("|") ++ "]")
  println(parts[0].len())
}
`
	// Three parts intact, and the first is five *runes* though it is six bytes.
	want := "[héllo|wörld|ünïcode]\n5"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
