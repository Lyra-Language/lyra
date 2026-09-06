package llvm

import (
	"strings"
	"testing"
)

// The ASCII rune classifiers and case conversions (`std/prelude/strings.lyra`).
//
// Written against the **real** prelude for the reason `buildAndRunWithPrelude` gives: these
// are ordinary Lyra built on rune literals and `Ord for rune`, so a copy pasted into a test
// would be a second implementation free to drift from the one users get.
//
// The cases below are mostly *boundaries*. Each predicate is a range compare over a rune
// literal, and the failure mode that survives review is an inclusive/exclusive slip at an
// edge — `'@'` and `'['` bracket the uppercase run, “ '`' “ and `'{'` bracket the lowercase
// one, and both neighbours look nothing like letters, so an off-by-one reads as correct.
func TestExec_RuneClassifiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, expr string
		want       bool
	}{
		// is_ascii_upper / is_ascii_lower — the four characters adjacent to the two letter runs.
		{"upper accepts A", `'A'.is_ascii_upper()`, true},
		{"upper accepts Z", `'Z'.is_ascii_upper()`, true},
		{"upper rejects @, one below A", `'@'.is_ascii_upper()`, false},
		{"upper rejects [, one above Z", `'['.is_ascii_upper()`, false},
		{"upper rejects a lowercase letter", `'a'.is_ascii_upper()`, false},
		{"lower accepts a", `'a'.is_ascii_lower()`, true},
		{"lower accepts z", `'z'.is_ascii_lower()`, true},
		{"lower rejects backtick, one below a", "'`'.is_ascii_lower()", false},
		{"lower rejects {, one above z", `'{'.is_ascii_lower()`, false},

		// is_ascii_digit — same shape, and `'/'` and `':'` are the neighbours.
		{"digit accepts 0", `'0'.is_ascii_digit()`, true},
		{"digit accepts 9", `'9'.is_ascii_digit()`, true},
		{"digit rejects /, one below 0", `'/'.is_ascii_digit()`, false},
		{"digit rejects :, one above 9", `':'.is_ascii_digit()`, false},

		// is_ascii_alpha is the union, so it inherits both pairs of boundaries.
		{"alpha accepts an uppercase letter", `'Q'.is_ascii_alpha()`, true},
		{"alpha accepts a lowercase letter", `'q'.is_ascii_alpha()`, true},
		{"alpha rejects a digit", `'7'.is_ascii_alpha()`, false},
		{"alpha rejects a space", `' '.is_ascii_alpha()`, false},

		// is_ascii_space — the five it documents, and the near-misses.
		{"space accepts a space", `' '.is_ascii_space()`, true},
		{"space accepts a tab", `'\t'.is_ascii_space()`, true},
		{"space accepts a newline", `'\n'.is_ascii_space()`, true},
		{"space accepts a carriage return", `'\r'.is_ascii_space()`, true},
		{"space accepts a form feed", `'\x0c'.is_ascii_space()`, true},
		{"space rejects a vertical tab", `'\x0b'.is_ascii_space()`, false},
		{"space rejects NUL", `'\x00'.is_ascii_space()`, false},

		// is_ascii_control_code — the two disjoint runs, 0..31 and DEL.
		{"control accepts NUL", `'\x00'.is_ascii_control_code()`, true},
		{"control accepts US, the last of the low run", `'\x1f'.is_ascii_control_code()`, true},
		{"control rejects space, one above US", `' '.is_ascii_control_code()`, false},
		{"control accepts DEL", `'\x7f'.is_ascii_control_code()`, true},
		{"control rejects ~, one below DEL", `'~'.is_ascii_control_code()`, false},

		// is_ascii_punctuation is the complement of alpha, digit and space over the printable
		// set — which is what makes the backtick right. Spelled out as an explicit list
		// it was the one character missing, in the same slot (96) an earlier hand-written
		// ASCII table also skipped: it hides between `_` and `a`, where the eye expects
		// letters to begin.
		{"punctuation accepts a backtick", "'`'.is_ascii_punctuation()", true},
		{"punctuation accepts a tilde, the last printable", `'~'.is_ascii_punctuation()`, true},
		{"punctuation accepts a bang, the first after space", `'!'.is_ascii_punctuation()`, true},
		{"punctuation accepts a backslash", `'\\'.is_ascii_punctuation()`, true},
		{"punctuation accepts a quote", `'\''.is_ascii_punctuation()`, true},
		{"punctuation rejects a letter", `'m'.is_ascii_punctuation()`, false},
		{"punctuation rejects a digit", `'4'.is_ascii_punctuation()`, false},
		{"punctuation rejects a space", `' '.is_ascii_punctuation()`, false},
		{"punctuation rejects a newline", `'\n'.is_ascii_punctuation()`, false},
		{"punctuation rejects DEL", `'\x7f'.is_ascii_punctuation()`, false},

		// is_ascii_printable is an explicit range, space through tilde — not the
		// complement of the control codes. The difference is everything above 127: a
		// complement calls every non-ASCII rune printable, which then made `é` and `π`
		// punctuation, since they are not alpha, digit or space either. The `ascii`
		// prefix and the bound arrived together for that reason.
		{"printable accepts a space, the first", `' '.is_ascii_printable()`, true},
		{"printable accepts a tilde, the last", `'~'.is_ascii_printable()`, true},
		{"printable rejects US, one below space", `'\x1f'.is_ascii_printable()`, false},
		{"printable rejects DEL, one above tilde", `'\x7f'.is_ascii_printable()`, false},
		{"printable rejects a letter above ASCII", `'é'.is_ascii_printable()`, false},

		// The bound is what these three pin: a non-ASCII rune is outside every ASCII
		// classifier, rather than falling through a complement into punctuation.
		{"punctuation rejects an accented letter", `'é'.is_ascii_punctuation()`, false},
		{"punctuation rejects a Greek letter", `'π'.is_ascii_punctuation()`, false},
		{"punctuation rejects an em dash", `'—'.is_ascii_punctuation()`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "let main = () -> void => { println(" + c.expr + ") }\n"
			want := "true"
			if !c.want {
				want = "false"
			}
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
				t.Errorf("%s = %s; want %s", c.expr, got, want)
			}
		})
	}
}

// `to_ascii_lower` / `to_ascii_upper` are **total** — identity on anything that is not a
// letter of the opposite case, rather than a `Result` whose error arm is the common case.
// A caller folding a document hits far more spaces and punctuation than letters, so the
// identity is what lets the call sit inline with nothing to unwrap.
func TestExec_RuneCaseConversion(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, expr, want string }{
		{"lower maps an uppercase letter", `'A'.to_ascii_lower()`, "a"},
		{"lower maps the last uppercase letter", `'Z'.to_ascii_lower()`, "z"},
		{"lower is identity on a lowercase letter", `'q'.to_ascii_lower()`, "q"},
		{"lower is identity on a digit", `'7'.to_ascii_lower()`, "7"},
		{"lower is identity on punctuation", `'!'.to_ascii_lower()`, "!"},
		{"upper maps a lowercase letter", `'a'.to_ascii_upper()`, "A"},
		{"upper maps the last lowercase letter", `'z'.to_ascii_upper()`, "Z"},
		{"upper is identity on an uppercase letter", `'Q'.to_ascii_upper()`, "Q"},
		{"upper is identity on a space", `' '.to_ascii_upper()`, " "},

		// The neighbours of the letter runs are where a `+ 32` applied one slot too wide
		// would show: `'@' + 32` is a backtick and `'[' + 32` is `'{'`, both plausible
		// enough on a screen to survive an eyeball.
		{"lower is identity on @, one below A", `'@'.to_ascii_lower()`, "@"},
		{"lower is identity on [, one above Z", `'['.to_ascii_lower()`, "["},
		{"upper is identity on a backtick, one below a", "'`'.to_ascii_upper()", "`"},
		{"upper is identity on {, one above z", `'{'.to_ascii_upper()`, "{"},

		// Round-tripping is only an identity in one direction per case, which is the
		// property worth pinning: folding is idempotent, not invertible.
		{"lower is idempotent", `'A'.to_ascii_lower().to_ascii_lower()`, "a"},
		{"upper then lower folds", `'a'.to_ascii_upper().to_ascii_lower()`, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// Bracketed because one case is a space, which a bare TrimSpace would eat —
			// making an empty answer indistinguishable from the right one.
			src := `let main = () -> void => { println("[" ++ "${` + c.expr + `}" ++ "]") }` + "\n"
			want := "[" + c.want + "]"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
				t.Errorf("%s = %q; want %q", c.expr, got, want)
			}
		})
	}
}
