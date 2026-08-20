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
