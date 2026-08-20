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

// A **generic** function over a pointer, run rather than only checked: `^t` binds `t`,
// the specialization lowers, and a `^mut u8` argument reaches a `^t` parameter — the
// assignability relaxation and the unifier's raw-pointer case meeting in one program.
// Both were missing until 08/19, and each hid the other: with no unifier case the call
// never got as far as an assignability question.
func TestExec_GenericOverAPointer(t *testing.T) {
	t.Parallel()
	src := `module main
let first<t> = pure (p: ^t) -> t => unsafe { p^ }
let poke<t> = (p: ^mut t, v: t) -> void => unsafe { p^ = v }
let main = () -> u8 => {
  var xs: []u8 = [7, 0]
  var n: i64 = 30
  unsafe {
    poke(&mut n, 5)
    u8(first(&n)) + first(&mut xs[0])
  }
}
`
	if got := buildAndRun(t, src); got != 12 {
		t.Errorf("exited %d; want 12 (5 written through ^mut t, plus 7 read through ^t)", got)
	}
}

// `cstring_len` — strlen, in Lyra, over a buffer this program built so the test needs no
// library installed. It is library code rather than an `extern` because scanning for a
// zero byte became expressible the moment `offset` existed, which is the same division
// that puts `parse_i64` in the prelude and leaves `read_line` a builtin.
//
// `unsafe`, because the terminator is the caller's promise and nothing can check it —
// which is exactly what `CBuffer.get` does *not* need, its length having been promised
// once at construction.
func TestExec_CStringLenScansToTheTerminator(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ CBuffer, cstring_len }
let main = () -> void => {
  var bytes: []u8 = [104, 105, 33, 0, 122]
  let p = unsafe { &bytes[0] }
  let buf = unsafe { CBuffer { ptr: p, len: cstring_len(p) } }
  var s = ""
  for i in 0..<buf.len { s = s ++ "${rune(buf.get(i))}" }
  print("${buf.len} ${s}")
}
`, "")
	// 3, not 5: the scan stops at the NUL and the byte after it is not part of the string.
	if got := strings.TrimSpace(out); got != "3 hi!" {
		t.Errorf("cstring_len walk = %q; want \"3 hi!\"", got)
	}
}

// An empty C string is a lone NUL, and answers 0 rather than reading anything.
func TestExec_CStringLenOfAnEmptyString(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ cstring_len }
let main = () -> void => {
  var bytes: []u8 = [0, 65]
  print("${unsafe { cstring_len(&bytes[0]) }}")
}
`, "")
	if got := strings.TrimSpace(out); got != "0" {
		t.Errorf("cstring_len of an empty C string = %q; want \"0\"", got)
	}
}

// The two halves of the module's safety story, asserted against each other: `cstring_len`
// needs an `unsafe` context and `CBuffer.get` does not. That is the whole design in one
// test — the promise is made once, where the length is established, and every read after
// it is checked rather than trusted.
func TestCheck_CStringLenIsUnsafeAndGetIsNot(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
import std.ffi.{ CBuffer, cstring_len }
let main = () -> void => {
  var bytes: []u8 = [65, 0]
  let p = unsafe { &bytes[0] }
  println("${cstring_len(p)}")
}
`)
	if len(errs) != 1 || !strings.Contains(errs[0], `calling unsafe function "cstring_len"`) {
		t.Errorf("calling cstring_len outside `unsafe` should be refused; got %v", errs)
	}
	safe := checkWithPrelude(t, `
module main
import std.ffi.{ CBuffer }
let main = () -> void => {
  var bytes: []u8 = [65, 0]
  let buf = CBuffer { ptr: unsafe { &bytes[0] }, len: 2 }
  println("${buf.get(0)}")
}
`)
	if len(safe) != 0 {
		t.Errorf("CBuffer.get needs no `unsafe` — its length was promised at construction; got %v", safe)
	}
}
