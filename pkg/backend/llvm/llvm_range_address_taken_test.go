package llvm

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// **A binding whose address is taken with `&mut` is never tracked by the value-range pass**,
// and these are the reason: the backend *drops* the runtime check for anything that pass
// proves safe, so a stale interval is not a wrong warning but a missing trap.
//
// Until 09/11 the pass did not know `&mut` existed. After `set(&mut i, 7)` it still believed
// `i == 0`, and each of these ran to completion — reading past an array, wrapping a u8, and
// dividing by zero — in code whose only `unsafe` was the call that did the write. Each test
// takes the checked operation through the binary and demands the trap, since a stdout
// assertion would pass on a program that printed garbage.
func assertTraps(t *testing.T, src, wantMessage string) {
	t.Helper()
	out, err := exec.Command(preludeBinary(t, src)).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("the program must trap; it exited cleanly with %q", out)
	}
	if ee.ExitCode() != 101 {
		t.Errorf("exited %d; want 101 (a trap)", ee.ExitCode())
	}
	if !strings.Contains(string(out), wantMessage) {
		t.Errorf("output = %q; want %q", out, wantMessage)
	}
}

func TestExec_BoundsCheckSurvivesAWriteThroughAMutablePointer(t *testing.T) {
	t.Parallel()
	assertTraps(t, `
module main
let set = (p: ^mut i64, v: i64) -> void => unsafe { p^ = v }
let main = () -> void => {
  let xs: [3]i64 = [10, 20, 30]
  var i: i64 = 0
  unsafe { set(&mut i, 7) }
  println("${xs[i]}")
}
`, "array index out of bounds")
}

func TestExec_OverflowCheckSurvivesAWriteThroughAMutablePointer(t *testing.T) {
	t.Parallel()
	assertTraps(t, `
module main
let set = (p: ^mut u8, v: u8) -> void => unsafe { p^ = v }
let main = () -> void => {
  var x: u8 = 0
  unsafe { set(&mut x, 250) }
  println("${x + 10}")
}
`, "arithmetic overflow")
}

func TestExec_DivideByZeroCheckSurvivesAWriteThroughAMutablePointer(t *testing.T) {
	t.Parallel()
	assertTraps(t, `
module main
let set = (p: ^mut i64, v: i64) -> void => unsafe { p^ = v }
let main = () -> void => {
  var d: i64 = 1
  unsafe { set(&mut d, 0) }
  println("${10 / d}")
}
`, "divide by zero")
}

// **Flow-insensitive, and this is why.** Widening only at the `&mut` site is undone by the
// reassignment after it: the pass re-learns `i == 0` from `i = 0`, and the write through
// `p` is invisible to it. A name the program has handed a mutable pointer to is untracked
// for the whole function.
func TestExec_ATakenAddressUntracksTheNameForTheWholeFunction(t *testing.T) {
	t.Parallel()
	assertTraps(t, `
module main
let main = () -> void => {
  let xs: [3]i64 = [10, 20, 30]
  var i: i64 = 5
  let p = unsafe { &mut i }
  i = 0
  unsafe { p^ = 7 }
  println("${xs[i]}")
}
`, "array index out of bounds")
}
