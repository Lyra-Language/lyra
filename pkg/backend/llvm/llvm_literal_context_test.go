package llvm

import (
	"strings"
	"testing"
)

// A literal inside a tuple or a construction takes its width from what it meets, as a
// bare literal does. Two positions missed it until 09/30 (found writing Vega's build
// progress, which carries an `(f32, string)`):
//
//   - **a tuple literal passed where the receiver has already solved the variable**:
//     `t.unwrap_or((0.0, "x"))` on a `Maybe<(f32, string)>` bound `t` twice — `(f32,
//     string)` from the receiver, `(f64, string)` from the literal — and reported "cannot
//     infer type variable t". Integers too: `(3, "x")` against `(u8, string)`.
//   - **a construction compared with a typed value**: `m == Some(0.4)` on a `Maybe<f32>`
//     settled the construction at `Maybe<f64>` and refused the comparison.

func TestExec_ATupleLiteralTakesTheVariableItsReceiverSolved(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => {
  let t: Maybe<(f32, string)> = None
  let a = t.unwrap_or((0.5, "x"))
  let narrow: f32 = a.0
  let u: Maybe<(u8, string)> = None
  let b = u.unwrap_or((200, "y"))
  let small: u8 = b.0
  let n: Maybe<((u8, i16), string)> = None
  let c = n.unwrap_or(((3, -4), "z"))
  var xs: [](f32, i16) = []
  xs.push((0.25, 7))
  println("${narrow} ${a.1} ${small} ${c.0.0} ${c.0.1} ${xs[0].0} ${xs[0].1}")
}`
	if got, want := buildAndRunWithPrelude(t, src, ""), "0.5 x 200 3 -4 0.25 7\n"; got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
}

// Narrowed, the literal is range-checked at the width it took.
func TestCheck_ATupleLiteralNarrowedByItsReceiverIsRangeChecked(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `let main = () -> void => {
  let u: Maybe<(u8, string)> = None
  let b = u.unwrap_or((300, "y"))
  println(b.1)
}`)
	if len(errs) != 1 || !strings.Contains(errs[0], "300 overflows u8") {
		t.Errorf("want the literal reported against u8, got %q", errs)
	}
}

func TestExec_AConstructionComparedTakesTheOtherSidesType(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => {
  let m: Maybe<f32> = Some(0.4)
  let t: Maybe<(f32, string)> = Some((0.25, "y"))
  let r: Result<u8, string> = Ok(7)
  println("${m == Some(0.4)} ${Some(0.5) != m} ${t == Some((0.25, "y"))} ${t == None} ${r == Ok(7)} ${r != Err("no")}")
}`
	if got, want := buildAndRunWithPrelude(t, src, ""), "true true true false true true\n"; got != want {
		t.Errorf("stdout %q, want %q", got, want)
	}
}

// A construction that cannot fill the other side is still the mismatch it was, named by
// both types — the context completes a construction, it does not excuse one.
func TestCheck_AConstructionThatCannotFillTheOtherSideIsIncompatible(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `let main = () -> void => {
  let m: Maybe<f32> = None
  println(m == Some("x"))
}`)
	if len(errs) != 1 || !strings.Contains(errs[0], "incompatible types: Maybe<f32> and Maybe<string>") {
		t.Errorf("want the two types named, got %q", errs)
	}
}
