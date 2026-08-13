package llvm

import (
	"strings"
	"testing"
)

// A newtype's `where` constraint is enforced at run time (08/13).
//
// It was a compile-time assertion and nothing else: it caught a literal, and
// whatever the value-range pass could pin to an interval, and silently accepted
// everything else. So on `newtype Percent = u8 where range(0..<=100)`,
// `Percent(n)` with a runtime n of 200 built, ran and printed 200 — leaving the
// language's own ladder (provable → compile error, otherwise → trap) with a first
// rung and no second, in the one construct whose entire purpose is to be checked.
// The values a constrained newtype sees at run time are exactly the ones from
// outside the program, which is where a range or unit mistake lives.

const percentPrelude = `newtype Percent = u8 where range(0..<=100)
let mk = (n: u8) -> Percent => Percent(n)
`

func TestExec_RangeConstraintTrapsAtRuntime(t *testing.T) {
	t.Parallel()
	src := percentPrelude + `let main = () -> u8 => {
	  println(i64(u8(mk(50))))
	  println(i64(u8(mk(200))))
	  0
	}`
	out, code := buildAndRunCapture(t, src)
	if out != "50\n" {
		t.Errorf("stdout = %q; want %q (the in-range value prints, then the trap)", out, "50\n")
	}
	if code != trapExitCode {
		t.Errorf("exit = %d; want %d", code, trapExitCode)
	}
}

// Both bounds are inclusive here, and a constraint's whole job is to be exact about
// a boundary — so the edges must pass rather than trap.
func TestExec_RangeConstraintBoundariesPass(t *testing.T) {
	t.Parallel()
	src := percentPrelude + `let main = () -> u8 => {
	  println(i64(u8(mk(0))))
	  println(i64(u8(mk(100))))
	  0
	}`
	out, code := buildAndRunCapture(t, src)
	if out != "0\n100\n" {
		t.Errorf("stdout = %q; want %q", out, "0\n100\n")
	}
	if code != 0 {
		t.Errorf("exit = %d; want 0", code)
	}
}

// An exclusive end (`..<`) excludes its bound, where the inclusive one includes it.
func TestExec_RangeConstraintExclusiveEnd(t *testing.T) {
	t.Parallel()
	src := `newtype Heading = i64 where range(0..<360)
let mk = (n: i64) -> Heading => Heading(n)
let main = () -> u8 => {
  println(i64(mk(359)))
  println(i64(mk(360)))
  0
}`
	out, code := buildAndRunCapture(t, src)
	if out != "359\n" {
		t.Errorf("stdout = %q; want %q", out, "359\n")
	}
	if code != trapExitCode {
		t.Errorf("exit = %d; want %d", code, trapExitCode)
	}
}

func TestExec_ValuesConstraintTrapsAtRuntime(t *testing.T) {
	t.Parallel()
	src := `newtype Status = i32 where values(200, 404, 500)
let mk = (n: i32) -> Status => Status(n)
let main = () -> u8 => {
  println(i64(i32(mk(404))))
  println(i64(i32(mk(302))))
  0
}`
	out, code := buildAndRunCapture(t, src)
	if out != "404\n" {
		t.Errorf("stdout = %q; want %q", out, "404\n")
	}
	if code != trapExitCode {
		t.Errorf("exit = %d; want %d", code, trapExitCode)
	}
}

// `step(...)` means the values covered are start, start+step, start+2*step, … — the
// meaning types/step.go already fixed for both spellings — so the grid is measured
// from the range's start. Nothing read StepConstraint at all until 08/13.
func TestExec_StepConstraintTrapsAtRuntime(t *testing.T) {
	t.Parallel()
	src := `newtype Heading = i64 where range(0..<360), step(15)
let mk = (n: i64) -> Heading => Heading(n)
let main = () -> u8 => {
  println(i64(mk(45)))
  println(i64(mk(7)))
  0
}`
	out, code := buildAndRunCapture(t, src)
	if out != "45\n" {
		t.Errorf("stdout = %q; want %q", out, "45\n")
	}
	if code != trapExitCode {
		t.Errorf("exit = %d; want %d", code, trapExitCode)
	}
}

// The grid is offset by the range's start, not anchored at zero: with `range(5..<=95)`
// and `step(10)`, 15 is on the grid and 10 is not.
func TestExec_StepConstraintGridIsOffsetByRangeStart(t *testing.T) {
	t.Parallel()
	ok := `newtype Odd = i64 where range(5..<=95), step(10)
let mk = (n: i64) -> Odd => Odd(n)
let main = () -> void => println(i64(mk(15)))`
	out, code := buildAndRunCapture(t, ok)
	if out != "15\n" || code != 0 {
		t.Errorf("on-grid value: stdout = %q, exit = %d; want %q, 0", out, code, "15\n")
	}

	bad := `newtype Odd = i64 where range(5..<=95), step(10)
let mk = (n: i64) -> Odd => Odd(n)
let main = () -> void => println(i64(mk(10)))`
	if _, code := buildAndRunCapture(t, bad); code != trapExitCode {
		t.Errorf("off-grid value: exit = %d; want %d", code, trapExitCode)
	}
}

// A float base uses the same rules, with `fmod` for the step.
func TestExec_FloatConstraintTraps(t *testing.T) {
	t.Parallel()
	src := `newtype Quarter = f64 where range(0.0..<=1.0), step(0.25)
let mk = (x: f64) -> Quarter => Quarter(x)
let main = () -> u8 => {
  println(f64(mk(0.5)))
  println(f64(mk(0.3)))
  0
}`
	out, code := buildAndRunCapture(t, src)
	if out != "0.5\n" {
		t.Errorf("stdout = %q; want %q", out, "0.5\n")
	}
	if code != trapExitCode {
		t.Errorf("exit = %d; want %d", code, trapExitCode)
	}
}

// **A compile-time constant costs no check**, because it was already decided: a bad
// one is a compile error and a good one needs nothing. The check is emitted exactly
// where the compiler could not do better, so a literal construction stays as cheap
// as it was.
func TestEmit_ConstantConstructionEmitsNoConstraintCheck(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `newtype Percent = u8 where range(0..<=100)
let main = () -> void => {
  let p: Percent = 50
  println(i64(u8(p)))
}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "lyra_panic_constraint") {
		t.Errorf("a literal construction should need no runtime check:\n%s", ir)
	}
}

// One construction is one check. `Percent(n)` reaches the checker twice — as the
// constructor's operand and again as the constructor node when the enclosing context
// propagates the newtype onto it — and both being checked emitted the range test
// twice (four traps for one construction). The constructor is a TupleLiteralExpr,
// not a call, which is why the first attempt at this guard matched nothing.
func TestEmit_ConstructionEmitsOneCheckPerBound(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, percentPrelude+`let main = () -> void => println(i64(u8(mk(5))))`)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := functionBody(ir, "mk")
	if !ok {
		t.Fatalf("no emitted body for mk:\n%s", ir)
	}
	if got := strings.Count(body, "lyra_panic_constraint"); got != 2 {
		t.Errorf("expected 2 constraint traps in mk (one per bound), got %d:\n%s", got, body)
	}
}
