package llvm

import (
	"errors"
	"os/exec"
	"strconv"
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

// `data`/`data_mut` — the buffer direction, and the one that copies nothing: a `[]T`'s
// elements already live behind a contiguous `T*` in its box, so the pointer C wants is
// the one Lyra is holding. Both directions in one program, against libc rather than by
// inspection: `strlen` reads through `data`, `memset` writes through `data_mut`, and the
// array is read back afterwards to prove the bytes C wrote are the bytes Lyra has.
func TestExec_DataIsTheBufferLibcReadsAndWrites(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data, data_mut }
unsafe extern pure strlen: (^u8) -> u64
unsafe extern memset: (^mut u8, i32, u64) -> ^mut u8
let main = () -> void => {
  var c = "hello λ".cstring()
  var xs: []u8 = [1, 2, 3, 4]
  unsafe { memset(xs.data_mut(), 65, 3) }
  print("${unsafe { strlen(c.data()) }} ${xs[0]} ${xs[2]} ${xs[3]}")
}
`, "")
	// Eight bytes of content, and memset filled exactly the three it was given.
	if got := strings.TrimSpace(out); got != "8 65 65 4" {
		t.Errorf("data/data_mut = %q; want \"8 65 65 4\"", got)
	}
}

// **An empty array traps**, rather than handing out the address of nothing. The message
// is the function's own and not the index check's: the caller wrote `data()`, so naming a
// `[0]` they did not write would send them looking for an index.
func TestExec_DataOnAnEmptyArrayTraps(t *testing.T) {
	t.Parallel()
	for _, fn := range []string{"data", "data_mut"} {
		src := `
module main
import std.ffi.{ ` + fn + ` }
let main = () -> void => {
  var xs: []u8 = []
  let p = unsafe { xs.` + fn + `() }
  print("${unsafe { p^ }}")
}
`
		out, err := exec.Command(preludeBinary(t, src)).CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 101 {
			t.Fatalf("%s() on an empty array must trap; got %v", fn, err)
		}
		if !strings.Contains(string(out), fn+"() on an empty array") {
			t.Errorf("%s(): output = %q; want its own empty-array message", fn, out)
		}
	}
}

// **`unsafe` binds to the function, not to the spelling of the call.** The method form
// goes through UFCS, which desugars to the bare call and used to skip the E011 check the
// bare path makes — so `xs.data()` was a way around the keyword while `data(xs)` was not.
// Latent since UFCS landed and invisible until `data` became the first unsafe function
// with a `self` receiver.
func TestCheck_AnUnsafeFunctionIsUnsafeInMethodForm(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
import std.ffi.{ data }
let escapes = (xs: []u8) -> ^u8 => xs.data()
let main = () -> void => { println("ok") }
`)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "lyra-E011") || strings.Contains(e, "requires an `unsafe`") {
			found = true
		}
	}
	if !found {
		t.Errorf("xs.data() outside an unsafe context must be refused; got %v", errs)
	}
}

// `data` is callable from `pure noalloc` code: taking an address is not an effect and
// allocates nothing, and the empty-array trap's message is a constant so it stays that
// way.
func TestExec_DataIsCallableFromNoallocCode(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data }
let head = pure noalloc (xs: []u8) -> u8 => unsafe { xs.data()^ }
let main = () -> void => { print("${head([9, 8, 7])}") }
`, "")
	if got := strings.TrimSpace(out); got != "9" {
		t.Errorf("head = %q; want \"9\"", got)
	}
}

// `with_cstring` — the scoped form, checked through libc so what is asserted is what C
// reads. The buffer's lifetime is the call's, which is the whole reason to prefer it over
// `cstring()` plus a pointer taken by hand.
func TestExec_WithCStringLendsTheBufferForTheCall(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ with_cstring }
unsafe extern pure strlen: (^u8) -> u64
let main = () -> void => {
  print("${"hello λ".with_cstring((p) => unsafe { strlen(p) })}")
}
`, "")
	// Eight bytes for seven characters: the lambda is two.
	if got := strings.TrimSpace(out); got != "8" {
		t.Errorf("with_cstring = %q; want \"8\"", got)
	}
}

// **It is not marked `unsafe`**, which is a decision rather than an omission: it lends a
// pointer for the duration of a call and takes it back, where `data` hands one out to
// keep. Asserted as a *check* — a safe caller with no `unsafe` block of its own around
// the call must compile — because the alternative made the safer shape the more
// ceremonious one.
func TestCheck_WithCStringNeedsNoUnsafeAtTheCall(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
import std.ffi.{ with_cstring }
unsafe extern pure strlen: (^u8) -> u64
let main = () -> void => { println("${"hi".with_cstring((p) => unsafe { strlen(p) })}") }
`)
	if len(errs) != 0 {
		t.Errorf("with_cstring must be callable without an enclosing unsafe block; got %v", errs)
	}
}

// The effect polymorphism the prelude's combinators rely on, at this boundary: a `pure`
// caller may pass a `pure` callback, and an impure one is refused *at the call site*.
// That is why no bound beyond `pure` is written on with_cstring — the callback's effects
// are the call's.
func TestCheck_WithCStringIsPolymorphicInItsCallback(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
import std.ffi.{ with_cstring }
unsafe extern pure strlen: (^u8) -> u64
let ok = pure () -> u64 => "hi".with_cstring((p) => unsafe { strlen(p) })
let bad = pure () -> u64 => "hi".with_cstring((p) => { println("boom"); unsafe { strlen(p) } })
let main = () -> void => { println("${ok()}") }
`)
	if len(errs) != 1 || !strings.Contains(errs[0], "impure") {
		t.Errorf("want exactly one impure-callback error, got %v", errs)
	}
}

// `with_cstrings` — the flat two-string form, and the nested spelling it exists to spare
// the reader. Both are run: the nested one is also the regression guard for a closure
// capturing a `^u8`, which did not lower until 08/22.
func TestExec_WithCStringsFlatAndNested(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ with_cstring, with_cstrings }
unsafe extern pure strcmp: (^u8, ^u8) -> i32
let main = () -> void => {
  print("${with_cstrings("abc", "abd", (x, y) => unsafe { strcmp(x, y) })} ")
  print("${with_cstrings("abc", "abc", (x, y) => unsafe { strcmp(x, y) })} ")
  print("${"abc".with_cstring((x) => "abd".with_cstring((y) => unsafe { strcmp(x, y) }))}")
}
`, "")
	// **Only the *sign* of `strcmp` is specified**, so this asserts the sign and not the
	// magnitude. C says the result is negative, zero or positive and nothing more: Apple
	// libc happens to answer the byte difference (`'c' - 'd'` is -1) while glibc on arm64
	// answers -32, so asserting "-1 0 -1" made `./asan.sh` fail on Linux for a reason
	// having nothing to do with memory — and a gate CLAUDE.md tells you to run before
	// pushing memory-model work is worth less than nothing when it is red on a clean tree.
	//
	// What the test is actually for survives intact: that the bytes Lyra hands over are
	// the bytes C reads (so an ordering comparison lands on the right side of zero, and
	// equal strings compare equal), and that the nested spelling — a closure capturing a
	// `^u8` — agrees with the flat one.
	fields := strings.Fields(out)
	if len(fields) != 3 {
		t.Fatalf("with_cstrings = %q; want three whitespace-separated results", out)
	}
	for _, w := range []struct {
		i      int
		what   string
		ok     func(int) bool
		expect string
	}{
		{0, `with_cstrings("abc", "abd")`, func(n int) bool { return n < 0 }, "negative"},
		{1, `with_cstrings("abc", "abc")`, func(n int) bool { return n == 0 }, "zero"},
		{2, `nested with_cstring, "abc" vs "abd"`, func(n int) bool { return n < 0 }, "negative"},
	} {
		n, err := strconv.Atoi(fields[w.i])
		if err != nil {
			t.Errorf("%s = %q; want an integer", w.what, fields[w.i])
			continue
		}
		if !w.ok(n) {
			t.Errorf("%s = %d; want %s", w.what, n, w.expect)
		}
	}
}

// **A raw pointer has a size**, which is what lets an aggregate holding one be captured
// by a closure or held in a `[]T`. `SizeAndAlign` had no case for `^T` until 08/22, and
// the symptom was never "a pointer has no size": `std.ffi`'s own `CBuffer` could not be
// captured ("cannot size captured binding") or stored in a dynamic array ("cannot size
// dynamic array element type").
func TestExec_ARawPointerCanBeCapturedAndStored(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ CBuffer }
let apply = pure (f: () -> u8) -> u8 => f()
let main = () -> void => {
  var xs: []u8 = [65, 66]
  let p = unsafe { &xs[0] }
  let b = CBuffer { ptr: p, len: 2 }
  var bs: []CBuffer = [b]
  print("${apply(() => unsafe { p^ })} ${apply(() => b.get(1))} ${bs[0].get(0)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "65 66 65" {
		t.Errorf("captured/stored pointer = %q; want \"65 66 65\"", got)
	}
}

// `CLong`/`CULong` cross an extern boundary and are the plain widths on the other side —
// the point of the alias being that it names the one C type whose width moves between
// LP64 and Windows' LLP64, without taxing every call site the way a newtype would.
//
// **`strtoul`'s `endptr` gets storage of its own, and that is not incidental.** This test
// used to pass `p` for it — a `char*` where C wants `char**` — and `strtoul` wrote eight
// bytes through it. That was invisible while `with_cstring` handed over a *heap copy* of
// the string, and became a segfault the moment it handed over the string's own bytes
// (08/26), which for a literal live in a `private constant` global. The pointer is a `^u8`
// precisely because those bytes are immutable; a C function that writes through one is
// undefined behaviour that only `unsafe` stands between.
func TestExec_CLongCrossesTheBoundaryAsItsWidth(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ CLong, CULong, with_cstring, data_mut }
unsafe extern pure labs: (CLong) -> CLong
unsafe extern pure strtoul: (^u8, ^mut u8, i32) -> CULong
let main = () -> void => {
  let n: i64 = -42
  var endptr: []u8 = [0; 8]
  let m: u64 = "1234".with_cstring((p) => unsafe { strtoul(p, endptr.data_mut(), 10) })
  print("${unsafe { labs(n) }} ${m}")
}
`, "")
	if got := strings.TrimSpace(out); got != "42 1234" {
		t.Errorf("CLong round trip = %q; want \"42 1234\"", got)
	}
}
