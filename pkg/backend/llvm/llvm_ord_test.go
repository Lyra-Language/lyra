package llvm

import (
	"strings"
	"testing"
)

// `Ord` (std/prelude/ordering.lyra) extends comparison beyond the primitives: a type
// that implements `compare: (Self, Self) -> Ordering` can be compared with `<=>` and
// ordered with `<`, `<=`, `>`, `>=`.
//
// Nothing could order a user type before — `<` on a struct was "operands must be
// numeric" and `<=>` was numeric+rune only — and unlike equality there is no
// structural default to fall back on: lexicographic-by-declaration-order is a choice,
// and a footgun, since reordering fields would silently change the order.

// All three outcomes against all four relational operators. Twelve answers, because
// the derived operators are the part that can silently invert: each is one compare
// against the Ordering's tag, and a wrong tag is a wrong answer rather than a crash.
func TestExec_OrdDerivesTheRelationalOperators(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Ver { v: i64 }
impl Ord for Ver {
  compare = (self, other) => self.v <=> other.v
}
let show = (a: Ver, b: Ver) -> string => {
  let s = match a <=> b { Less => "L", Equal => "E", Greater => "G" };
  "${s} ${a < b} ${a <= b} ${a > b} ${a >= b}"
}
let main = () -> void => {
  println(show(Ver { v: 1 }, Ver { v: 2 }));
  println(show(Ver { v: 2 }, Ver { v: 2 }));
  println(show(Ver { v: 3 }, Ver { v: 2 }));
}
`
	want := "L true true false false\nE false true false true\nG false false true true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// An `Ord` impl must not change what the operators mean on the built-in types:
// `1 < 2` stays a machine comparison, never a call. dispatchOrdCompare refuses a
// numeric or rune operand for exactly this reason.
func TestExec_OrdDoesNotShadowPrimitiveComparison(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Ver { v: i64 }
impl Ord for Ver {
  compare = (self, other) => Greater
}
let main = () -> void => {
  println("${1 < 2} ${2 < 1} ${'a' < 'b'} ${1.5 < 2.5}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true false true true" {
		t.Errorf("got %q; want \"true false true true\" — a deliberately wrong Ord impl must not reach primitives", got)
	}
}

// A type with no `Ord` impl still gets the operand-domain error rather than silence,
// and the message names the new option.
func TestCheck_ComparingATypeWithNoOrdImplIsRefused(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Pt { x: i64 }
let go = (a: Pt, b: Pt) -> bool => a < b
let main = () -> void => { println("${go(Pt { x: 1 }, Pt { x: 2 })}") }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("a type with no Ord impl must not be comparable")
	}
	if !strings.Contains(diags[0], "implement Ord") {
		t.Errorf("the message should offer Ord, got: %s", diags[0])
	}
}

// Floats stay out of `Ord` — NaN is neither less than, equal to nor greater than
// anything, so a three-way answer has to pick one and every choice is wrong. `<=>`
// refuses them; `<` keeps its two-way IEEE answer through the primitive path (above).
func TestCheck_FloatsAreStillRefusedByTheThreeWayOperator(t *testing.T) {
	t.Parallel()
	const src = `
module main
let go = (a: f64, b: f64) -> i64 => match a <=> b { Less => -1, Equal => 0, Greater => 1 }
let main = () -> void => { println("${go(1.0, 2.0)}") }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("`<=>` must still refuse floats")
	}
	if !strings.Contains(diags[0], "NaN") {
		t.Errorf("the message should say why, got: %s", diags[0])
	}
}
