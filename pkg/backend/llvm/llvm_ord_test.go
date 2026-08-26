package llvm

import (
	"errors"
	"os/exec"
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

// Ordering for `string`, over the `compare_bytes` builtin.
//
// **Byte order is code-point order in UTF-8**, by design of the encoding, so one memcmp
// answers exactly what a rune-by-rune walk would. That equivalence is why the primitive
// is a builtin at all: written in the prelude with `s[i]` it would be O(n²), since
// indexing a string is O(i).
func TestExec_StringOrdering(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  println("${"a" < "b"} ${"b" < "a"} ${"a" <= "a"} ${"Z" < "a"}");
  // A prefix sorts first, which falls out of comparing the common prefix then lengths.
  println("${"ab" < "abc"} ${"abc" < "ab"} ${"" < "a"} ${"" < ""}");
  // Code-point order, not alphabetical: é is U+00E9, above 'e'.
  println("${"héllo" < "hello"}");
}
`
	want := "true false true true\ntrue false true false\nfalse"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The case that motivated it: a string payload or field made a type underivable, since
// `<=>` on two strings had nothing to dispatch to.
func TestExec_DeriveOrdOverStrings(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Ord)
data Tok = Num(i64) | Word(string) | End
@derive(Ord)
struct Person { last: string, first: string }
let main = () -> void => {
  println("${Word("apple") < Word("banana")} ${Num(9) < Word("a")} ${Word("z") < End}");
  let a = Person { last: "Smith", first: "Ann" };
  let b = Person { last: "Smith", first: "Bob" };
  let c = Person { last: "Jones", first: "Zoe" };
  println("${a < b} ${c < a} ${a == a}");
}
`
	want := "true true true\ntrue true true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// `min`/`max`/`clamp` — the prelude's, generic over `where t: Ord`, run rather than only
// checked. Every type with an ordering: the default integer width, a string, a rune.
func TestExec_MinMaxClampOverOrd(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  print("${3.min(5)} ${min(3, 5)} ${max(3, 5)} ")
  print("${(7).clamp(0, 3)} ${(-2).clamp(0, 3)} ${(2).clamp(0, 3)} ")
  print("${"b".min("a")} ${'z'.max('c')}")
}
`, "")
	if got := strings.TrimSpace(out); got != "3 3 5 3 0 2 a z" {
		t.Errorf("min/max/clamp = %q; want \"3 3 5 3 0 2 a z\"", got)
	}
}

// **Ties go to `self` for `min` and to `other` for `max`**, so the pair still names both
// values when two compare equal but are not interchangeable. Asserted on a type ordered
// by one field while carrying another, since on a scalar the rule is unobservable — which
// is exactly why it would otherwise be got wrong and never noticed.
//
// Card is deliberately **not** `pub`: it was, for as long as a specialization of a prelude
// generic at a privately declared type failed to lower, and that is fixed (08/22).
func TestExec_MinAndMaxSplitATie(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
struct Card { rank: i64, suit: string }
impl Ord for Card { compare = pure (self, other) => self.rank <=> other.rank }
let main = () -> void => {
  let a = Card { rank: 7, suit: "hearts" }
  let b = Card { rank: 7, suit: "spades" }
  print("${a.min(b).suit} ${a.max(b).suit}")
}
`, "")
	if got := strings.TrimSpace(out); got != "hearts spades" {
		t.Errorf("tie-breaking = %q; want \"hearts spades\" (min keeps self, max takes other)", got)
	}
}

// A reversed range describes an empty one, so there is no nearest value to answer with.
// Returning either bound would pick one arbitrarily and hide the mistake that produced it.
func TestExec_ClampWithAReversedRangeTraps(t *testing.T) {
	t.Parallel()
	out, err := exec.Command(preludeBinary(t, `
module main
let main = () -> void => { println("${(5).clamp(10, 0)}") }
`)).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 101 {
		t.Fatalf("clamp with lo > hi must trap; got %v", err)
	}
	if !strings.Contains(string(out), "lo is greater than hi") {
		t.Errorf("output = %q; want the reversed-range message", out)
	}
}

// The primitive `Ord` impls exist **so a bound can be satisfied**, on the same footing as
// math.lyra's arithmetic impls — and the body's `<=>` is the machine comparison, not
// recursion, because a primitive is never routed through an impl. Asserted by demanding
// the bound of a number at all, which is what failed before they existed.
//
// The **free-function** form here is the one that has always worked; the method spelling
// of the same call is TestExec_AMethodCallOnATypeParameterReachesUFCS below.
func TestExec_APrimitiveSatisfiesAnOrdBound(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let largest<t> where t: Ord = pure noalloc (xs: []t, seed: t) -> t => {
  var best = seed
  for x in xs { best = max(best, x) }
  best
}
let main = () -> void => {
  var ns: []i64 = [3, 11, 7]
  var ws: []u8 = [3, 11, 7]
  print("${largest(ns, 0)} ${largest(ws, u8(0))}")
}
`, "")
	if got := strings.TrimSpace(out); got != "11 11" {
		t.Errorf("bounded max = %q; want \"11 11\"", got)
	}
}

// **A method call on a bare type parameter reaches UFCS**, as of 08/26. `best.max(x)`
// inside `where t: Ord` is the prelude's `max(self: t, other: t)`, and until the ladder
// tried the UFCS rung for a type-variable receiver it reported *"type parameter t has no
// method max; add a `where t: Trait` bound"* — advice naming a bound already written.
//
// Run rather than checked, because the front end is only half of it: the desugared call
// records a *template* instantiation, in the enclosing body's own type variables, which
// has to compose into a real specialization at each of `largest`'s.
func TestExec_AMethodCallOnATypeParameterReachesUFCS(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let largest<t> where t: Ord = pure noalloc (xs: []t, seed: t) -> t => {
  var best = seed
  for x in xs { best = best.max(x) }
  best
}
let span<t> where t: Ord = pure (lo: t, hi: t, v: t) -> t => v.clamp(lo, hi).min(hi)
let main = () -> void => {
  var ns: []i64 = [3, 11, 7]
  var ws: []u8 = [3, 11, 7]
  print("${largest(ns, 0)} ${largest(ws, u8(0))} ${span(1, 10, 42)} ${span('a', 'y', 'z')}")
}
`, "")
	if got := strings.TrimSpace(out); got != "11 11 10 y" {
		t.Errorf("method-style bound call = %q; want \"11 11 10 y\"", got)
	}
}
