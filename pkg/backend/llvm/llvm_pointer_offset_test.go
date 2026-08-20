package llvm

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// `p.offset(n)` — the one form of pointer arithmetic the language has — and `std.ffi`'s
// `CBuffer`, which is the whole reason it is a primitive rather than a feature: a raw
// pointer carries no length so nothing about it can be checked, and a pointer *plus* a
// length can be. The checking is ordinary Lyra written over this.

// **Elements, not bytes**, which is the decision most easily got wrong and least easily
// noticed: on a `^i64` the scaling is by 8, so a byte-offset lowering answers garbage
// from inside the first element rather than failing. Chosen to match the rest of the
// language — a string is rune-indexed so `len` and `[i]` agree about the unit.
func TestExec_PointerOffsetIsInElements(t *testing.T) {
	t.Parallel()
	src := `module main
let main = () -> u8 => {
  var xs: []i64 = [10, 20, 30, 40]
  unsafe {
    let p = &xs[0]
    u8(p.offset(2)^)
  }
}
`
	if got := buildAndRun(t, src); got != 30 {
		t.Errorf("p.offset(2)^ on []i64 exited %d; want 30 (elements, not bytes)", got)
	}
}

// The write direction. `^` stays the only load and the only store, so `offset` composes
// with both rather than adding a second way to reach memory.
func TestExec_PointerOffsetWrites(t *testing.T) {
	t.Parallel()
	src := `module main
let main = () -> u8 => {
  var xs: []u8 = [1, 2, 3, 4]
  unsafe {
    let p = &mut xs[0]
    p.offset(3)^ = 42
  }
  xs[3]
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("p.offset(3)^ = 42 left xs[3] = %d; want 42", got)
	}
}

// Signed, because a negative offset is meaningful in C and refusing it buys nothing when
// nothing here is checked anyway.
func TestExec_PointerOffsetAcceptsANegativeIndex(t *testing.T) {
	t.Parallel()
	src := `module main
let main = () -> u8 => {
  var xs: []u8 = [5, 6, 7]
  unsafe {
    let p = &xs[2]
    u8(p.offset(-2)^)
  }
}
`
	if got := buildAndRun(t, src); got != 5 {
		t.Errorf("p.offset(-2)^ from xs[2] exited %d; want 5", got)
	}
}

// **`std.ffi`'s CBuffer, against the real standard library.** The point of the type is
// that `buf.get(i)` is checked where `p.offset(i)^` cannot be, so both halves are
// asserted: an index inside the buffer reads, and one outside traps rather than reading
// whatever is there.
func TestExec_CBufferGetReadsWithinTheBuffer(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ CBuffer }
let main = () -> void => {
  var xs: []u8 = [65, 66, 67]
  let buf = CBuffer { ptr: unsafe { &xs[0] }, len: 3 }
  for i in 0..<buf.len { print("${buf.get(i)} ") }
}
`, "")
	if got := strings.TrimSpace(out); got != "65 66 67" {
		t.Errorf("CBuffer walk = %q; want \"65 66 67\"", got)
	}
}

func TestExec_CBufferGetTrapsOutsideTheBuffer(t *testing.T) {
	t.Parallel()
	// Through the binary rather than the stdout runner: a trap writes to stderr and
	// exits 101, and a stdout-only assertion would pass on a program that printed
	// nothing for any other reason.
	out, err := exec.Command(preludeBinary(t, `
module main
import std.ffi.{ CBuffer }
let main = () -> void => {
  var xs: []u8 = [65, 66, 67]
  let buf = CBuffer { ptr: unsafe { &xs[0] }, len: 3 }
  print("${buf.get(3)}")
}
`)).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("reading past a CBuffer must trap; got %v", err)
	}
	if ee.ExitCode() != 101 {
		t.Errorf("exited %d; want 101 (a trap)", ee.ExitCode())
	}
	if !strings.Contains(string(out), "CBuffer index out of bounds") {
		t.Errorf("output = %q; want the out-of-bounds message", out)
	}
}
