package llvm

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// `std.ffi`, against the real standard library — `CBuffer`, `cstring_len`, `decode_utf8`
// and `cstring`, each exercised through a libc function rather than a fixture, because
// what is worth proving is that the bytes Lyra hands over are the bytes C reads.

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

// `cstring` — the out direction, and option A: a plain `[]u8` the caller keeps alive,
// with the pointer taken at the call site. Checked against libc rather than by
// inspection, so what is asserted is what C reads.
func TestExec_CStringIsReadableByLibc(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ cstring }
unsafe extern pure strlen: (^u8) -> u64
unsafe extern pure strcmp: (^u8, ^u8) -> i32
let main = () -> void => {
  var a = "hello λ".cstring()
  var b = "hello λ".cstring()
  print("${a.len()} ${unsafe { strlen(&a[0]) }} ${a.from_end(1)} ${unsafe { strcmp(&a[0], &b[0]) }}")
}
`, "")
	// Nine bytes for eight of content plus the terminator, which strlen does not count;
	// the last byte is the NUL; and two encodings of one string compare equal.
	if got := strings.TrimSpace(out); got != "9 8 0 0" {
		t.Errorf("cstring = %q; want \"9 8 0 0\"", got)
	}
}

// The empty string is one byte: just the terminator.
func TestExec_CStringOfTheEmptyString(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ cstring }
unsafe extern pure strlen: (^u8) -> u64
let main = () -> void => {
  var e = "".cstring()
  print("${e.len()} ${unsafe { strlen(&e[0]) }}")
}
`, "")
	if got := strings.TrimSpace(out); got != "1 0" {
		t.Errorf(`"".cstring() = %q; want "1 0"`, got)
	}
}

// **An interior NUL traps.** C cannot represent one — the string would arrive truncated
// at that byte, which is the silently-wrong answer this language traps for everywhere
// else. Rust returns an error here instead; a trap is the call `split("")` already makes,
// for an argument with no meaningful answer rather than input a program did not choose.
func TestExec_CStringWithAnInteriorNulTraps(t *testing.T) {
	t.Parallel()
	out, err := exec.Command(preludeBinary(t, `
module main
import std.ffi.{ cstring }
let main = () -> void => {
  var bytes: []u8 = [104, 0, 105]
  var c = bytes.decode_utf8().cstring()
  print("${c.len()}")
}
`)).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 101 {
		t.Fatalf("a NUL inside a C string must trap; got %v", err)
	}
	if !strings.Contains(string(out), "cannot contain a NUL byte") {
		t.Errorf("output = %q; want the interior-NUL message", out)
	}
}

// **The shape that would dangle does not compile**, which is the argument for option A
// over a `CString` type: `&"x".cstring()[0]` names a buffer that stops existing at the end
// of the statement. This is the bug `CString::new(…).as_ptr()` produces silently in Rust,
// and that Clippy carries a dedicated lint for; here the type checker refuses it.
func TestCheck_PointerIntoATemporaryCStringIsRefused(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
import std.ffi.{ cstring }
unsafe extern pure strlen: (^u8) -> u64
let main = () -> void => {
  println("${unsafe { strlen(&"boom".cstring()[0]) }}")
}
`)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "address of a temporary") {
			found = true
		}
	}
	if !found {
		t.Errorf("a pointer into a temporary C string must be refused; got %v", errs)
	}
}
