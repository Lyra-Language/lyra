package llvm

import (
	"strings"
	"testing"
)

// An int literal in a float context lowers as a float constant of the recorded
// width — before the fix it fell back to i64 (typechecker: propagateExpectedType
// bailed on float contexts; backend: literalIntType's i64 default), so
// `let x: f64 = -5` printed 18446744073709551611 and `x + 0.5` emitted
// invalid IR (`@llvm.uadd.with.overflow.i64(i64, double)`) only clang caught.

// TestEmit_IntLiteralInFloatSlot_Width pins the lowered types: the binding
// allocas at the float width and arithmetic is float arithmetic, with no
// overflow intrinsic (float ops don't trap).
func TestEmit_IntLiteralInFloatSlot_Width(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
	  let x: f64 = 5
	  let y = x + 0.5
	  let c: f32 = 1
	  let d = c * 2
	  0
	}`
	out, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for _, want := range []string{"alloca double", "fadd double", "alloca float", "fmul float"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in IR, got:\n%s", want, out)
		}
	}
	for _, reject := range []string{"with.overflow", "alloca i64"} {
		if strings.Contains(out, reject) {
			t.Errorf("unexpected %q in IR (int lowering leaked into a float slot):\n%s", reject, out)
		}
	}
}

// TestExec_IntLiteralInFloatSlot runs the original miscompile: print of a
// negative float-adapted literal (used to print 18446744073709551611), plus
// the literal flowing through a call argument, arithmetic, and a comparison.
func TestExec_IntLiteralInFloatSlot(t *testing.T) {
	t.Parallel()
	src := `let half = (x: f64) -> f64 => x / 2
	let main = () -> u8 => {
	  let x: f64 = -5
	  print(x)
	  println("")
	  print(half(9))
	  println("")
	  let y = x + 0.5
	  print(y)
	  println("")
	  if half(9) > 4 { print("gt") }
	  println("")
	  0
	}`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d; want 0\noutput:\n%s", code, out)
	}
	want := "-5\n4.5\n-4.5\ngt\n"
	if out != want {
		t.Errorf("output = %q; want %q", out, want)
	}
}

// TestExec_IntLiteralInFloatAggregates covers the field/payload/return/match
// positions: every context that accepts an untyped int against a float type.
func TestExec_IntLiteralInFloatAggregates(t *testing.T) {
	t.Parallel()
	src := `struct Pt { v: f64 }
	data Boxed = Wrap(f64)
	let pick = (flag: bool) -> f64 => { if flag { 3 } else { 4 } }
	let main = () -> u8 => {
	  let p = Pt { v: 2 }
	  print(p.v)
	  println("")
	  match Wrap(7) { Wrap(f) => { print(f) } }
	  println("")
	  print(pick(true))
	  println("")
	  var m: f64 = 1
	  m += 2
	  print(m)
	  println("")
	  0
	}`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d; want 0\noutput:\n%s", code, out)
	}
	want := "2\n7\n3\n3\n"
	if out != want {
		t.Errorf("output = %q; want %q", out, want)
	}
}

// An integer literal beside a float literal is a float, in every operator (09/13). Before,
// `<` passed the checker and the backend refused a comparison of `i64` against `double`,
// `let z = 5 * 2.5` emitted an `i64` multiply over a `double` that clang rejected, and `==`
// refused the pair — three answers to one question. A context still narrows the pair:
// `let f: f32 = 5 * 2.5` is an f32 multiply.
func TestExec_IntLiteralBesideAFloatLiteralIsAFloat(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let z = 5 * 2.5
  let f: f32 = 5 * 2.5
  let m = 2.0 * (3 - 1)
  var score = 0
  if z == 12.5 { score += 1 }
  if f == 12.5 { score += 2 }
  if m == 4.0 { score += 4 }
  if 5 < 5.5 && !(5 < 5.0) && 5 <= 5.0 && -1 < 2.5 { score += 8 }
  if 5 == 5.0 && 5 != 5.5 && 5.0 > 4 { score += 16 }
  u8(score)
}`)
	if got != 31 {
		t.Errorf("exited %d; want 31 (each bit is one form that must hold)", got)
	}
}
