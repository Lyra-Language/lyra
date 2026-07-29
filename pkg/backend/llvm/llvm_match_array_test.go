package llvm

import (
	"os/exec"
	"testing"
)

// `match` on a dynamic array `[]T` lowers as an if-else ladder: each array-pattern
// arm is a length test plus per-element literal tests, with element bindings and a
// whole-array `[...rest]` borrowing (first match wins).

func TestExec_ArrayMatch(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if got := buildAndRun(t, classify(c.literal)); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// Literal element patterns: an arm matches only when the length AND every literal
// element agree (a length match with a mismatched element falls through).
func TestExec_ArrayMatch_LiteralElements(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if got := buildAndRun(t, prog(c.literal)); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// `[...rest]` binds the whole array (a borrow) and is exhaustive on its own.
func TestExec_ArrayMatch_RestBindsWhole(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// A `[head, ...tail]` pattern binds a **fresh** `[]T` holding the suffix — the one
// array binding that is not a borrow, since the elements it needs are a suffix of a
// box whose header describes the whole array, so there is no existing storage to
// alias. It allocates, copies, and is released at the arm's scope exit.
//
// This was deferred with a loud error for a long time, filed as blocked on an
// ownership-pass change. It needed none: the pass keys managed-ness off the recorded
// type, and a pattern binding is never last-use-eligible, so an owning use inside the
// arm records a plain Retain — exactly the +1 an escape needs.
func TestExec_ArrayMatch_TailBinding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The canonical recursive list idiom, now expressible end to end.
			"recursive sum", `let sum = (xs: []i64) -> i64 => match xs {
  [] => 0,
  [h, ...t] => h + sum(t),
}
let main = () -> u8 => u8(sum([1, 2, 3, 4]))`, 10,
		},
		{
			// More than one fixed element before the rest.
			"two fixed elements", `let sumTwo = (xs: []i64) -> i64 => match xs {
  [a, b, ...rest] => a + b + sumTwo(rest),
  [a] => a,
  [] => 0,
}
let main = () -> u8 => u8(sumTwo([1, 2, 3, 4, 5]))`, 15,
		},
		{
			// A one-element array yields an *empty* tail (len 0), not a fault.
			"empty tail", `let tailLen = (xs: []i64) -> i64 => match xs {
  [h, ...t] => t.len(),
  _ => 99,
}
let main = () -> u8 => u8(tailLen([7]))`, 0,
		},
		{
			// A literal test in front of the rest: the length test is `>=` now, so the
			// arm must still be selected by the element comparison.
			"literal element then rest", `let f = (xs: []i64) -> i64 => match xs {
  [1, ...rest] => 100 + rest.len(),
  [...rest] => rest.len(),
}
let main = () -> u8 => u8(f([1, 2, 3]) + f([5, 6]))`, 104,
		},
		{
			// The tail escapes the match as the arm's value — the Retain path.
			"tail escapes the arm", `let tailOf = (xs: []i64) -> []i64 => match xs {
  [h, ...t] => t,
  _ => xs,
}
let main = () -> u8 => u8(tailOf([9, 8, 7]).len())`, 2,
		},
		{
			// Managed elements: the tail owns its own references, so each copied
			// element is retained — otherwise the source and the tail both free them.
			"managed elements", `let count = (xs: []string) -> i64 => match xs {
  [] => 0,
  [h, ...t] => 1 + count(t),
}
let main = () -> u8 => u8(count(["a" ++ "1", "b" ++ "2", "c" ++ "3"]))`, 3,
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

// The memory contract, where a missing retain or a misplaced release shows up: a
// `[]string` tail outlives the match, and the source array must still be intact.
func TestExec_ArrayMatch_TailBinding_ManagedIsSafe(t *testing.T) {
	t.Parallel()
	src := `let rest2 = (xs: []string) -> []string => match xs {
  [h, ...t] => t,
  _ => xs,
}
let main = () -> u8 => {
  let src: []string = ["a" ++ "1", "b" ++ "2", "c" ++ "3"]
  let t = rest2(src)
  if t[0] == "b2" && src[0] == "a1" && t.len() == 2 { 0 } else { 1 }
}`
	if got := buildAndRun(t, src); got != 0 {
		t.Fatalf("exited %d; want 0", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil || !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; ran without it")
	}
	if got := buildAndRunASan(t, clang, src); got != 0 {
		t.Errorf("under ASan: exited %d; want 0", got)
	}
}

// An `[]` empty array pattern (now that it parses) matches a zero-length array — the
// base case of a list match. Verifies the grammar change end to end.
func TestExec_ArrayMatch_EmptyPattern(t *testing.T) {
	t.Parallel()
	prog := func(literal string) string {
		return `let classify = (xs: []i64) -> u8 => match xs {
  [] => 0,
  [a] => u8(a),
  _ => 99
}
let main = () -> u8 => {
  let xs: []i64 = ` + literal + `
  classify(xs)
}`
	}
	cases := []struct {
		literal string
		want    int
	}{
		{"[]", 0},      // empty matches []
		{"[5]", 5},     // one element
		{"[1, 2]", 99}, // longer → catch-all
	}
	for _, c := range cases {
		t.Run(c.literal, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, prog(c.literal)); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}
