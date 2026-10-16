package llvm

import (
	"strings"
	"testing"
)

// A context type reaches the value it is wanted as in every position, including the ones
// a construction or a literal sits *inside* (10/16). Each of these was refused or
// miscompiled while its sibling spelling worked.

// An anonymous tuple pushes each element's type onto the element before inferring it, as
// an array literal does: `Some(-3)` inside `(u8, Maybe<i16>)` solved alone as
// `Maybe<i64>`, and the tuple was refused at a binding, an argument and a `push`, while
// `(9, None)` — left open — was accepted.
func TestExec_TupleContextReachesAConstructionInside(t *testing.T) {
	t.Parallel()
	const src = `
module main
let second = (p: (u8, Maybe<i16>)) -> i16 => match p.1 {
  Some(v) => v,
  None => 0,
}
let main = () -> void => {
  let b: (u8, Maybe<i16>) = (9, Some(-3))
  var xs: [](u8, Maybe<i16>) = []
  xs.push((1, Some(-30000)))
  let nested: ((u8, Maybe<i16>), u8) = ((2, Some(7)), 3)
  print("${second(b)} ${second((4, Some(-5)))} ${second(xs[0])} ${second(nested.0)}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-3 -5 -30000 7" {
		t.Errorf("got %q; want \"-3 -5 -30000 7\"", got)
	}
}

// …and a payload too large for the width the tuple's context gave it is still refused.
func TestCheck_TupleContextPayloadOverflow(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
let main = () -> void => {
  let b: (u8, Maybe<i16>) = (9, Some(40000))
}
`)
	if len(errs) != 1 || !strings.Contains(errs[0], "literal value 40000 overflows i16") {
		t.Fatalf("want one overflow error, got %v", errs)
	}
}

// A comprehension's result takes the context's element as an array literal's elements do:
// `[i in 0..<3 | 7]` under `[]u8` was settled at `[]i64` and refused, and `None` under
// `[]Maybe<i16>` never had its parameter solved (lyra-E073), where `[7, 7]` and
// `[None; 4]` were accepted.
func TestExec_ComprehensionResultTakesItsContext(t *testing.T) {
	t.Parallel()
	const src = `
module main
let total = (xs: []u8) -> u8 => {
  var n: u8 = 0
  for x in xs { n = n.wrapping_add(x) }
  n
}
let main = () -> void => {
  let xs: []u8 = [i in 0..<40 | 7]
  var ms: []Maybe<i16> = [p in 0..<4 | None]
  ms[2] = Some(-9)
  let tuples: [](u8, Maybe<i16>) = [i in 0..<2 | (1, Some(-3))]
  let sum = match ms[2] { Some(v) => v, None => 0 }
  print("${total(xs)} ${ms.len()} ${sum} ${tuples.len()}")
}
`
	// 40 × 7 = 280, which a u8 total wraps to 24 — the elements are bytes, not i64s.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "24 4 -9 2" {
		t.Errorf("got %q; want \"24 4 -9 2\"", got)
	}
}

// …and a result that does not fit the element is refused, by the narrowing that sets it.
func TestCheck_ComprehensionResultOverflow(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
let main = () -> void => {
  let xs: []u8 = [i in 0..<3 | 300]
}
`)
	if len(errs) != 1 || !strings.Contains(errs[0], "literal value 300 overflows u8") {
		t.Fatalf("want one overflow error, got %v", errs)
	}
}

// A literal must fit through an `unsafe { … }` block, as through a plain one: the context
// narrowed into it, the fit check did not follow, and `let x: u8 = unsafe { 300 }` printed
// 300 from a u8.
func TestCheck_LiteralThroughAnUnsafeBlockMustFit(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
let f = () -> u8 => unsafe { 300 }
let g = (x: u8) -> u8 => x
let main = () -> void => {
  let a: u8 = unsafe { 256 }
  let ok: u8 = unsafe { 255 }
  let b = g(unsafe { 1000 })
}
`)
	want := []string{"300 overflows u8", "256 overflows u8", "1000 overflows u8"}
	if len(errs) != len(want) {
		t.Fatalf("want %d overflow errors, got %v", len(want), errs)
	}
	for _, w := range want {
		found := false
		for _, e := range errs {
			found = found || strings.Contains(e, w)
		}
		if !found {
			t.Errorf("no error saying %q in %v", w, errs)
		}
	}
}

// A `#[…]` literal argument narrows to a fixed-array parameter's element type. It was
// refused ("cannot assign StaticArray<i64, 3>") on 09/30 and is right now; this keeps it.
func TestExec_FixedArrayLiteralArgumentNarrows(t *testing.T) {
	t.Parallel()
	const src = `
module main
let sum3 = (xs: ref [3]u16) -> u16 => xs[0] + xs[1] + xs[2]
let main = () -> void => {
  print("${sum3(#[1000, 2000, 60000])}")
}
`
	// The check that matters is that it compiles at all; the sum is the value's check.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "63000" {
		t.Errorf("got %q; want \"63000\"", got)
	}
}
