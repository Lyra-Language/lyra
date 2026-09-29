package llvm

import (
	"strings"
	"testing"
)

// `Eq` (std/prelude/ordering.lyra) **overrides** structural equality rather than
// enabling it. Equality already worked on every type, including a bare type variable,
// so this exists for the minority whose equality is not field-wise — case-insensitive
// text, a struct with a cache field that should not count.
//
// That is the opposite of Rust and Swift, and it follows from where this language
// started: requiring a bound would have removed capability to gain ceremony.

// The override itself: equality by length rather than by contents.
func TestExec_EqImplOverridesStructuralEquality(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Tag { text: string }
impl Eq for Tag {
  eq = (self, other) => self.text.len() == other.text.len()
}
let main = () -> void => {
  let a = Tag { text: "abc" };
  let b = Tag { text: "xyz" };
  let c = Tag { text: "wxyz" };
  println("${a == b} ${a != b} ${a == c} ${a != c}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true false false true" {
		t.Errorf("got %q; want \"true false false true\"", got)
	}
}

// An `Eq` impl must not change what equality means on the built-in types: `1 == 1`
// stays a machine comparison. Same rule Ord follows, and for the same reason.
func TestExec_EqDoesNotShadowPrimitiveEquality(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Tag { text: string }
impl Eq for Tag {
  eq = (self, other) => false
}
let main = () -> void => {
  println("${1 == 1} ${2 == 3} ${"a" == "a"} ${true == true} ${'x' == 'x'}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true false true true true" {
		t.Errorf("got %q; want all-but-one true — a deliberately false Eq impl must not reach primitives", got)
	}
}

// The hole found while building this: an operand that is a type *variable* names no
// impl at check time, so `p == q` used a type's Eq impl while `same(p, q)` — the same
// comparison through a generic — silently used structural equality. One operator
// meaning two things depending on whether it was written inside a generic.
//
// The typechecker now publishes a candidate per implementing type and the backend picks
// by the substituted operand type, the arrangement bound dispatch uses.
func TestExec_EqImplReachesThroughAGenericCall(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Tag { text: string }
impl Eq for Tag {
  eq = (self, other) => self.text.len() == other.text.len()
}
let same<t> = (a: t, b: t) -> bool => a == b
let main = () -> void => {
  let a = Tag { text: "abc" };
  let b = Tag { text: "xyz" };
  println("${a == b} ${same(a, b)} ${same(1, 2)} ${same("q", "q")}");
}
`
	// The first two must agree: that is the whole point.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true true false true" {
		t.Errorf("got %q; want \"true true false true\" — direct and generic `==` must agree", got)
	}
}

// Equality needs no bound: an unbounded generic `==` still compiles and runs, which is
// the capability full trait-dispatch would have removed.
func TestExec_GenericEqualityStillNeedsNoBound(t *testing.T) {
	t.Parallel()
	const src = `
module main
let same<t> = (a: t, b: t) -> bool => a == b
let main = () -> void => { println("${same(1, 1)} ${same("a", "b")}") }
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true false" {
		t.Errorf("got %q; want \"true false\"", got)
	}
}

// An array or tuple literal compared with a typed value takes its type, as a lone literal
// takes a typed number's width. The literal kept i64 elements, and `==` on a `[]u8` called
// the `[]u8` equality function with an `[]i64` — invalid IR that clang-15 refuses and
// Apple's clang does not notice (09/29), so this matters on Linux (./asan.sh) and CI.
func TestExec_AggregateLiteralComparedWithATypedValue(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  let x: []u8 = [1, 2, 200]
  let pair: (u8, i16) = (3, -4)
  let fixed: [2]u16 = #[500, 7]
  var n = 0
  if x == [1, 2, 200] { n += 1 }
  if [1, 2, 200] == x { n += 1 }
  if x != [1, 2] { n += 1 }
  if pair == (3, -4) { n += 1 }
  if fixed == #[500, 7] { n += 1 }
  u8(n)
}`
	if got := buildAndRun(t, src); got != 5 {
		t.Errorf("exited %d; want 5", got)
	}
}
