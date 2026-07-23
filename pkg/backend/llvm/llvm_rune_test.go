package llvm

import (
	"strings"
	"testing"
)

// A rune is a Unicode code point (i32). A character literal lowers to an i32
// constant, and a `match` on a rune reuses the scalar if-else ladder: each
// char-literal arm is an `icmp eq` against the pre-decoded code point, an
// identifier/wildcard arm is the catch-all. These compile and run end to end,
// observable through the u8 exit code.
func TestExec_RuneMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Char-literal arms select by code point; the matched arm's value returns.
		{
			"char literal arms",
			`let classify = (r: rune) -> u8 => match r {
			   'a' => 1,
			   'b' => 2,
			   _ => 0,
			 }
			 let main = () -> u8 => classify('b')`,
			2,
		},
		// A code point with no arm falls through to the wildcard.
		{
			"wildcard fallthrough",
			`let classify = (r: rune) -> u8 => match r {
			   'a' => 1,
			   _ => 9,
			 }
			 let main = () -> u8 => classify('z')`,
			9,
		},
		// Escape sequences in a char pattern decode to the same code point as the
		// scrutinee (single-source escape handling in the collector).
		{
			"escape sequence pattern",
			`let f = (r: rune) -> u8 => match r {
			   '\n' => 7,
			   '\t' => 8,
			   _ => 0,
			 }
			 let main = () -> u8 => f('\t')`,
			8,
		},
		// A char literal used directly as a scrutinee lowers as an i32 constant.
		{
			"char literal scrutinee inline",
			`let main = () -> u8 => match 'x' {
			   'x' => 42,
			   _ => 0,
			 }`,
			42,
		},
		// An identifier catch-all binds the rune; a guard tests it via rune ==.
		{
			"guard on bound rune",
			`let f = (r: rune) -> u8 => match r {
			   x if x == 'a' => 3,
			   _ => 0,
			 }
			 let main = () -> u8 => f('a')`,
			3,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// A rune match lowers to the scalar ladder: an i32 comparison against the
// code point, not a data-tag switch.
func TestEmit_RuneMatchIR(t *testing.T) {
	got, err := emitSource(t, `let f = (r: rune) -> u8 => match r {
	   'a' => 1,
	   _ => 0,
	 }
	 let main = () -> u8 => f('a')`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"icmp eq i32", // the code-point comparison
		"i32 97",      // 'a' == U+0061 == 97, lowered as an i32 constant
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}
}
