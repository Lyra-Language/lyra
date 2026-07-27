package llvm

import (
	"os/exec"
	"testing"
)

// `match` on a dynamic array `[]T` lowers as an if-else ladder: each array-pattern
// arm is a length test plus per-element literal tests, with element bindings and a
// whole-array `[...rest]` borrowing (first match wins).

func TestExec_ArrayMatch(t *testing.T) {
	// One classifier reused with different-length inputs. (An `[]` empty pattern is a
	// grammar gap — it doesn't parse — so empty falls to the catch-all here.)
	classify := func(literal string) string {
		return `let f = (xs: []i64) -> u8 => match xs {
  [a] => u8(a),
  [a, b] => u8(a + b),
  _ => 99
}
let main = () -> u8 => {
  let xs: []i64 = ` + literal + `
  f(xs)
}`
	}
	cases := []struct {
		name    string
		literal string
		want    int
	}{
		{"one element binds a", "[5]", 5},
		{"two elements bind a,b", "[10, 20]", 30},
		{"three falls to catch-all", "[1, 2, 3]", 99},
		{"empty falls to catch-all", "[]", 99},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAndRun(t, classify(c.literal)); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// Literal element patterns: an arm matches only when the length AND every literal
// element agree (a length match with a mismatched element falls through).
func TestExec_ArrayMatch_LiteralElements(t *testing.T) {
	prog := func(literal string) string {
		return `let f = (xs: []i64) -> u8 => match xs {
  [1, 2] => 1,
  [3, 4] => 2,
  _ => 0
}
let main = () -> u8 => {
  let xs: []i64 = ` + literal + `
  f(xs)
}`
	}
	cases := []struct {
		literal string
		want    int
	}{
		{"[1, 2]", 1},
		{"[3, 4]", 2},
		{"[1, 3]", 0}, // right length, wrong second element
		{"[5, 6]", 0},
	}
	for _, c := range cases {
		t.Run(c.literal, func(t *testing.T) {
			if got := buildAndRun(t, prog(c.literal)); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// `[...rest]` binds the whole array (a borrow) and is exhaustive on its own.
func TestExec_ArrayMatch_RestBindsWhole(t *testing.T) {
	src := `let f = (xs: []i64) -> u8 => match xs {
  [...rest] => u8(rest[0] + rest[1])
}
let main = () -> u8 => {
  let xs: []i64 = [7, 8]
  f(xs)
}`
	if got := buildAndRun(t, src); got != 15 {
		t.Errorf("[...rest] rest[0]+rest[1]: expected 15, got %d", got)
	}
}

// Matching a `[]string` reads elements as borrows — memory-safe under ASan.
func TestExec_ArrayMatch_StringElementsASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `let f = (xs: []string) -> u8 => match xs {
  [a] => if a == "hi" { 1 } else { 2 },
  _ => 9
}
let main = () -> u8 => {
  let xs: []string = ["hi"]
  f(xs)
}
`
	if code := buildAndRunASan(t, clang, src); code != 1 {
		t.Errorf("ASan run: expected exit 1, got %d", code)
	}
}

// A `[head, ...tail]` pattern that binds a tail sub-array is deferred with a loud
// error (it needs an allocation + copy).
func TestEmit_ArrayMatch_TailBindingDeferred(t *testing.T) {
	src := `let f = (xs: []i64) -> u8 => match xs {
  [head, ...tail] => u8(head),
  _ => 0
}
let main = () -> u8 => {
  let xs: []i64 = [1, 2, 3]
  f(xs)
}
`
	if _, err := emitSource(t, src); err == nil {
		t.Fatal("expected a loud error for a `[head, ...tail]` tail-binding pattern, got none")
	}
}
