package llvm

import (
	"strings"
	"testing"
)

// `bytes.decode_utf8()` — a byte array read as UTF-8 text.
//
// A builtin because it has to be: it allocates a ref-counted string box and copies into
// it, and concatenation is the only string construction Lyra code has. The alternative
// was `s = s ++ "${rune(b)}"` in a loop, which is O(n²) in allocations — what
// `examples/zlib.lyra` did to read a six-byte version string.

// Both array flavours, because which one a byte buffer is depends on how it was written
// rather than on what it holds: the same literal is a `[N]u8` bare and a `[]u8` under an
// annotation, and a reader decoding bytes does not think of those as two questions.
func TestExec_DecodeUTF8BothArrayFlavours(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => {
  var dyn: []u8 = [104, 105, 33]
  let fixed: [3]u8 = [104, 105, 33]
  print("${dyn.decode_utf8()} ${fixed.decode_utf8()}")
}
`)
	if got := strings.TrimSpace(out); got != "hi! hi!" {
		t.Errorf("decode_utf8 = %q; want \"hi! hi!\"", got)
	}
}

// **The rune count comes from the bytes, not from their number.** Three bytes holding a
// two-byte λ and a `!` are two runes — the property a lowering that copied `byte_len` into
// the count field would get wrong, and would get wrong invisibly on ASCII.
func TestExec_DecodeUTF8CountsRunesNotBytes(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => {
  var bytes: []u8 = [206, 187, 33]
  let s = bytes.decode_utf8()
  print("${s} ${s.len()} ${s[0]}")
}
`)
	if got := strings.TrimSpace(out); got != "λ! 2 λ" {
		t.Errorf("decode_utf8 of a 3-byte, 2-rune buffer = %q; want \"λ! 2 λ\"", got)
	}
}

// An empty buffer decodes to the empty string. memcpy over a zero length is a valid
// no-op, so this needs no special case — the test is here to keep it that way.
func TestExec_DecodeUTF8OfAnEmptyBuffer(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => {
  var bytes: []u8 = []
  print("[${bytes.decode_utf8()}] ${bytes.decode_utf8().len()}")
}
`)
	if got := strings.TrimSpace(out); got != "[] 0" {
		t.Errorf("decode_utf8 of an empty buffer = %q; want \"[] 0\"", got)
	}
}

// **The copy is what makes the string independent of the array**, and that is the reason
// it copies rather than a cost it happens to pay: a string is a ref-counted box whose
// header sits at its start, so it could not point into the array's buffer — and a later
// `push` may move that buffer anyway.
func TestExec_DecodeUTF8IsIndependentOfTheArray(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => {
  var bytes: []u8 = [104, 105]
  let s = bytes.decode_utf8()
  bytes[0] = 72
  bytes.push(33)
  print("${s} ${bytes.decode_utf8()}")
}
`)
	if got := strings.TrimSpace(out); got != "hi Hi!" {
		t.Errorf("decoded string should not follow the array; got %q, want \"hi Hi!\"", got)
	}
}

// It allocates, so `noalloc` refuses it — the same rule `s.slice(a, b)` obeys, and the
// one `builtinMethodAllocates` exists to carry (the purity pass has three copies of the
// "what does this call call?" ladder and all three treat a builtin as effect-free, so an
// allocating one is invisible to every one of them at once).
func TestCheck_DecodeUTF8IsRefusedInNoalloc(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `module main
let f = pure noalloc (bytes: []u8) -> i64 => bytes.decode_utf8().len()
let main = () -> void => { var b: []u8 = [65]; println("${f(b)}") }
`)
	if len(errs) == 0 {
		t.Error("decode_utf8 allocates, so a `noalloc` function must not be able to call it")
	}
}

// `std.ffi`'s CBuffer carries the same name for the same operation on the same bytes —
// only the container differs, and the receiver tells them apart.
func TestExec_CBufferDecodeUTF8(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ CBuffer, cstring_len }
let main = () -> void => {
  var bytes: []u8 = [104, 105, 33, 0, 122]
  let p = unsafe { &bytes[0] }
  let buf = unsafe { CBuffer { ptr: p, len: cstring_len(p) } }
  print("${buf.decode_utf8()}")
}
`, "")
	// The byte after the NUL is not part of the string: the length came from the scan.
	if got := strings.TrimSpace(out); got != "hi!" {
		t.Errorf("CBuffer.decode_utf8 = %q; want \"hi!\"", got)
	}
}

// `s.encode_utf8()` — the inverse, and a builtin for the mirror-image reason: nothing in
// the language can read a byte out of a string. `byte_len` measures, `byte_offset` maps a
// rune position to a byte one, `compare_bytes_at` compares — none of them reads, and
// `s[i]` is a rune. The bytes were reachable only by re-encoding each rune by hand, which
// is a UTF-8 encoder written in user code to recover bytes the string already holds.
func TestExec_EncodeUTF8RoundTrips(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => {
  let s = "hi λ!"
  let b = s.encode_utf8()
  print("${b.len()} ${s.len()} ${b[0]} ${b.decode_utf8()}")
}
`)
	// Six bytes and five runes: the λ is two bytes, which is the whole point of having
	// both numbers. The first byte is 'h', not a rune.
	if got := strings.TrimSpace(out); got != "6 5 104 hi λ!" {
		t.Errorf("encode_utf8 = %q; want \"6 5 104 hi λ!\"", got)
	}
}

// The result is an ordinary mutable `[]u8` and owes nothing to the string it came from —
// which is what the copy buys, and what a language with immutable strings had better not
// get wrong.
func TestExec_EncodeUTF8ResultIsIndependentAndMutable(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => {
  let s = "abc"
  var b = s.encode_utf8()
  b[0] = 65
  b.push(33)
  print("${b.decode_utf8()} ${s}")
}
`)
	if got := strings.TrimSpace(out); got != "Abc! abc" {
		t.Errorf("mutating the bytes should not reach the string; got %q, want \"Abc! abc\"", got)
	}
}

// An empty string encodes to an empty array. `malloc(0)` is a pointer `free` accepts and
// memcpy over a zero length is a no-op, so this needs no case of its own — the test keeps
// it that way.
func TestExec_EncodeUTF8OfTheEmptyString(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `module main
let main = () -> void => { print("${"".encode_utf8().len()}") }
`)
	if got := strings.TrimSpace(out); got != "0" {
		t.Errorf(`"".encode_utf8().len() = %q; want "0"`, got)
	}
}

// It allocates two things — the box and its buffer — so `noalloc` refuses it, like its
// inverse and like `slice`.
func TestCheck_EncodeUTF8IsRefusedInNoalloc(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `module main
let f = pure noalloc (s: string) -> i64 => s.encode_utf8().len()
let main = () -> void => { println("${f("hi")}") }
`)
	if len(errs) == 0 {
		t.Error("encode_utf8 allocates, so a `noalloc` function must not be able to call it")
	}
}
