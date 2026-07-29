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
	t.Parallel()
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
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A rune match lowers to the scalar ladder: an i32 comparison against the
// code point, not a data-tag switch.
func TestEmit_RuneMatchIR(t *testing.T) {
	t.Parallel()
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

// Rune ordering and conversions lower: comparisons are i32 icmp on the code
// point, `i32(c)`/`i64(c)` widen (sign-extending, since a rune is a signed i32
// like Go's), and `rune(n)` narrows back. Together these make classification
// logic expressible, which is what the gap blocked.
func TestExec_RuneOrderingAndConversions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"is-digit true", `let isDigit = (c: rune) -> bool => c >= '0' && c <= '9'
let main = () -> u8 => { if isDigit('7') { 1 } else { 0 } }`, 1,
		},
		{
			"is-digit false", `let isDigit = (c: rune) -> bool => c >= '0' && c <= '9'
let main = () -> u8 => { if isDigit('q') { 1 } else { 0 } }`, 0,
		},
		{
			"is-alpha across both ranges", `let isAlpha = (c: rune) -> bool =>
  (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
let main = () -> u8 => { if isAlpha('Q') { 1 } else { 0 } }`, 1,
		},
		{
			// The idiom the conversions exist for: arithmetic on code points.
			"digit value via conversion", `let digitValue = (c: rune) -> i32 => i32(c) - i32('0')
let main = () -> u8 => u8(digitValue('7'))`, 7,
		},
		{
			"round trip through rune(n)", `let upper = (c: rune) -> rune => rune(i32(c) - 32)
let main = () -> u8 => { if upper('a') == 'A' { 0 } else { 1 } }`, 0,
		},
		{
			// A multibyte code point orders and widens correctly (é is U+00E9).
			"multibyte code point", `let big = (c: rune) -> bool => c > '~'
let main = () -> u8 => { if big('é') { u8(i64('é')) } else { 0 } }`, 0xE9,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// A rune comparison is an i32 icmp on the code point, and a widening conversion
// sign-extends — the signed predicate/extension matching its i32 representation.
func TestEmit_RuneComparisonIsI32(t *testing.T) {
	t.Parallel()
	out, err := emitSource(t, `let f = (c: rune) -> bool => c < 'z'
let w = (c: rune) -> i64 => i64(c)
let main = () -> u8 => 0`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(out, "icmp slt i32") {
		t.Errorf("expected a signed i32 comparison, got:\n%s", out)
	}
	if !strings.Contains(out, "sext i32") {
		t.Errorf("expected a sign-extending widening conversion, got:\n%s", out)
	}
}
