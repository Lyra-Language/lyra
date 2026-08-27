package llvm

import (
	"strings"
	"testing"
)

// `p.decode_utf8(byte_len)` — foreign bytes read as UTF-8 text, in **one copy**.
//
// The `[]u8` spelling is the same operation on memory Lyra owns; this one is for memory it
// does not, which is the only kind a `^u8` can point at. Without it the route from a C
// buffer to a string was `CBuffer.get(i)` in a loop into a `[]u8` and *then* that array's
// own `decode_utf8` — a bounds check and a capacity check per byte, and a second full copy.
// Measured over 400 KB: **548 µs → 256 µs**, against 254 µs for the array builtin doing the
// same job on memory Lyra already owned, which is the floor (one memcpy, one count pass).

func TestExec_DecodeUTF8FromAPointer(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ CBuffer, decode_utf8, data }
let main = () -> void => {
  // Non-ASCII, so the rune count and the byte length disagree — the thing a naive
  // byte-per-rune reading gets wrong.
  var xs: []u8 = "héllo".encode_utf8()
  let s = unsafe { xs.data().decode_utf8(xs.len()) }
  // Through CBuffer, which is what std.ffi wraps it as.
  let b = unsafe { CBuffer { ptr: xs.data(), len: xs.len() } }
  let viaBuf = b.decode_utf8()
  print("${s} ${s.len()} ${s.byte_len()} ${viaBuf} ${viaBuf == s}")
}
`, "")
	if got := strings.TrimSpace(out); got != "héllo 5 6 héllo true" {
		t.Errorf("got %q; want \"héllo 5 6 héllo true\"", got)
	}
}

// A zero length is a valid no-op for `memcpy`, so the empty buffer needs no special case —
// it decodes to the empty string, which is the right answer rather than a trap.
func TestExec_DecodeUTF8FromAPointerEmpty(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data }
let main = () -> void => {
  var xs: []u8 = [65]
  let s = unsafe { xs.data().decode_utf8(0) }
  print("[${s}] ${s.len()}")
}
`, "")
	if got := strings.TrimSpace(out); got != "[] 0" {
		t.Errorf("got %q; want \"[] 0\"", got)
	}
}

// **A negative length traps**, because it would otherwise reach `memcpy` as a huge unsigned
// size. A length that is merely *too large* cannot be caught at all — a raw pointer carries
// no extent, which is the whole reason the caller had to write `unsafe`.
func TestExec_DecodeUTF8FromAPointerTrapsOnANegativeLength(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunPanicWithPrelude(t, `
module main
import std.ffi.{ data }
let main = () -> void => {
  var xs: []u8 = [65, 66]
  println(unsafe { xs.data().decode_utf8(-1) })
}
`)
	if code != 101 {
		t.Fatalf("exited %d; want the trap's 101", code)
	}
	if !strings.Contains(out, "byte length must not be negative") {
		t.Errorf("output = %q; want the byte-length message", out)
	}
}

// The result is **independent of its source**, which is the reason the copy is not
// avoidable: foreign memory may be freed or rewritten the moment the call returns, and a
// string is a ref-counted box whose header sits at its start, so it could not point into
// memory it does not own even if it wanted to.
func TestExec_DecodeUTF8FromAPointerCopies(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data }
let main = () -> void => {
  var xs: []u8 = [104, 105]
  let s = unsafe { xs.data().decode_utf8(2) }
  xs[0] = 74
  xs[1] = 79
  let after = unsafe { xs.data().decode_utf8(2) }
  print("${s} ${after}")
}
`, "")
	if got := strings.TrimSpace(out); got != "hi JO" {
		t.Errorf("got %q; want \"hi JO\" — the first string must not follow its source", got)
	}
}
