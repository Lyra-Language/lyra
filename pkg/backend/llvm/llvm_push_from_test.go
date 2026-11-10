package llvm

import (
	"regexp"
	"strings"
	"testing"
)

// `xs.push_from(p, n)` — n elements read through a `^T`, appended to a `[]T` in one copy
// (11/10). The bindings that copy a C buffer into an array Lyra owns pushed it an element at
// a time; this grows once and copies once, through `copyBytes`.

// **Against a buffer C filled**, at lengths from empty to past a megabyte, checked by C: the
// fixture's checksum of the copy must equal its checksum of the source. Its fill does not
// repeat, so a copy shifted, cut short or overrun reads back different. The same bytes as
// `Color`s check a struct element: its stride and its fields.
func TestExec_FFIFixture_PushFromCopiesACBuffer(t *testing.T) {
	t.Parallel()
	checkFixture(t, `
module main
import std.ffi.{ data }

unsafe extern malloc: (size: u64) -> ^mut u8
unsafe extern free: (p: ^mut u8) -> void
unsafe extern pure lyra_fixture_fill_pattern: (p: ^mut u8, n: i64) -> void
unsafe extern pure lyra_fixture_checksum: (p: ^u8, n: i64) -> u64

struct Color { r: u8, g: u8, b: u8, a: u8 }

let main = () -> void => {
  for n in [0, 1, 7, 4096, 1000003] {
    let src = unsafe { malloc(u64(n + 1)) }
    unsafe { lyra_fixture_fill_pattern(src, n) }
    var bytes: []u8 = []
    unsafe { bytes.push_from(src, n) }
    let want = unsafe { lyra_fixture_checksum(src, n) }
    let got = if n == 0 { want } else { unsafe { lyra_fixture_checksum(bytes.data(), n) } }

    let count = n / 4
    var colors: []Color = []
    unsafe { colors.push_from(pointer_at(src.address()), count) }
    let color_bytes: ^u8 = if count == 0 { src } else { unsafe { pointer_at(colors.data().address()) } }
    let colors_same = unsafe { lyra_fixture_checksum(color_bytes, count * 4) } ==
      unsafe { lyra_fixture_checksum(src, count * 4) }
    let fields = count == 0 ||
      colors[count - 1].a == unsafe { src.offset(count * 4 - 1)^ } && colors[0].r == unsafe { src^ }

    // The copy is the program's: C writing its buffer again does not reach it.
    unsafe { lyra_fixture_fill_pattern(src, n / 2) }
    let kept = n < 2 || unsafe { lyra_fixture_checksum(bytes.data(), n) } == want
    unsafe { free(src) }
    print("${n}:${bytes.len() == n && got == want}${colors.len() == count && colors_same && fields}${kept} ")
  }
}
`, "0:truetruetrue 1:truetruetrue 7:truetruetrue 4096:truetruetrue 1000003:truetruetrue")
}

// **It appends**: onto what the array holds, growing to fit or into spare capacity, row by
// row, and from a pointer into the array itself — which growth must read before it frees
// the old buffer.
func TestExec_PushFromAppends(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data }
let main = () -> void => {
  var xs: []u8 = [1, 2, 3]
  let ys: []u8 = [10, 20, 30, 40]
  unsafe { xs.push_from(ys.data(), 4) }
  unsafe { xs.push_from(ys.data().offset(1), 2) }
  println(xs.map((b) => "${b}").join(","))
  unsafe { xs.push_from(xs.data(), xs.len()) }
  println("${xs.len()} ${xs[9]} ${xs[17]}")
  var grid: []i16 = []
  let row: []i16 = [1, -2, 300, -4000, 5]
  for _ in 0..<100 { unsafe { grid.push_from(row.data(), 5) } }
  var sum = 0
  for v in grid { sum += i64(v) }
  println("${grid.len()} ${sum} ${grid[498]}")
}
`, "")
	want := "1,2,3,10,20,30,40,20,30\n18 1 30\n500 -369600 -4000"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// A zero count does nothing, whatever the pointer — C's `(NULL, 0)` for an empty buffer is
// common enough that a trap there would put a guard at every call.
func TestExec_PushFromAZeroCount(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  var xs: []u8 = [7]
  let none: ^u8 = nullptr
  unsafe { xs.push_from(none, 0) }
  var empty: []f32 = []
  let nothing: ^f32 = nullptr
  unsafe { empty.push_from(nothing, 0) }
  print("${xs.len()} ${xs[0]} ${empty.len()}")
}
`, "")
	if got := strings.TrimSpace(out); got != "1 7 0" {
		t.Errorf("got %q; want \"1 7 0\"", got)
	}
}

// **A negative count traps**, as it would otherwise reach the copy as a huge size; **so does
// a null pointer with a positive one**, which is how a C function that failed answers.
func TestExec_PushFromTraps(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, call, want string }{
		{"a negative count", "xs.push_from(xs.data(), -1)", "element count must not be negative"},
		{"a null pointer", "xs.push_from(none, 3)", "push_from read elements through a null pointer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunPanicWithPrelude(t, `
module main
import std.ffi.{ data }
let main = () -> void => {
  var xs: []u8 = [1, 2]
  let none: ^u8 = nullptr
  unsafe { `+c.call+` }
  println(xs.len())
}
`)
			if code != 101 {
				t.Fatalf("exited %d; want the trap's 101", code)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("output = %q; want %q", out, c.want)
			}
		})
	}
}

// `noalloc` refuses it, on push's reasoning: it can grow the buffer.
func TestCheck_PushFromIsRefusedByNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let add = unsafe noalloc (b: mut []u8, p: ^u8) -> void => { b.push_from(p, 4) }
let main = () -> void => { var a: []u8 = []; let p: ^u8 = nullptr; unsafe { add(a, p) } }
`
	if diags := checkWithPrelude(t, src); len(diags) == 0 {
		t.Fatal("push_from can grow the buffer, so `noalloc` must refuse it")
	}
}

// **The copy is `llvm.memcpy`, never libc's `memcpy`**: the intrinsic's `i64` length is
// lowered with the target's own `size_t`, where a declared `memcpy(i8*, i8*, i64)` hands a
// 32-bit runtime the high half of the length (CLAUDE.md's single answer for a copy). The
// lowering is the same on every target, so the check is of the IR.
func TestEmit_PushFromCopiesWithTheIntrinsic(t *testing.T) {
	t.Parallel()
	ir := emitWithPrelude(t, `
module main
let fill = (xs: mut []u32, p: ^u32, n: i64) -> void => unsafe { xs.push_from(p, n) }
let main = () -> void => {
  var xs: []u32 = []
  let p: ^u32 = nullptr
  fill(xs, p, 0)
}
`)
	body := regexp.MustCompile(`(?s)define [^\n]*@lyra\.main\.fill[^\n]*\{\n.*?\n\}`).FindString(ir)
	if body == "" {
		t.Fatalf("no definition of fill in the IR:\n%s", ir)
	}
	if n := strings.Count(body, "call void @llvm.memcpy"); n != 3 {
		t.Errorf("fill makes %d llvm.memcpy calls; want 3 (old elements and source on growth, source into spare room)\n%s", n, body)
	}
	if strings.Contains(body, "@memcpy(") || strings.Contains(body, "@realloc(") {
		t.Errorf("fill calls libc's memcpy or realloc:\n%s", body)
	}
}

// **Under ASan, from memory C allocated to the exact size**: any read past the count, or a
// read of the old buffer after growth freed it (the self-append), is reported.
func TestExec_PushFromUnderASan(t *testing.T) {
	t.Parallel()
	if got := buildAndRunASanWithPrelude(t, `
module main
import std.ffi.{ data }
unsafe extern malloc: (size: u64) -> ^mut u8
unsafe extern free: (p: ^mut u8) -> void
let main = () -> u8 => {
  let n = 100003
  let src = unsafe { malloc(u64(n)) }
  for i in 0..<n { unsafe { src.offset(i)^ = u8(i % 251) } }
  var xs: []u8 = []
  unsafe { xs.push_from(src, n) }
  xs.reserve(3 * n)
  unsafe { xs.push_from(src, n) }
  unsafe { xs.push_from(xs.data(), xs.len()) }
  unsafe { free(src) }
  var ok = xs.len() == 4 * n
  for i in 0..<xs.len() { if xs[i] != u8((i % n) % 251) { ok = false } }
  if ok { 3 } else { 1 }
}
`); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// **Nothing it grows leaks**, on Linux where LeakSanitizer runs: the old buffer is freed on
// every growth, and an array filled this way is dropped like any other.
func TestExec_PushFromDoesNotLeak(t *testing.T) {
	t.Parallel()
	if got := buildAndRunLSanWithPrelude(t, `
module main
import std.ffi.{ data }
struct Held { bytes: []u8 }
let main = () -> u8 => {
  let row: []u8 = [1, 2, 3, 4, 5, 6, 7]
  var total = 0
  for _ in 0..<50 {
    var out: []u8 = []
    for _ in 0..<40 { unsafe { out.push_from(row.data(), 7) } }
    var held = Held { bytes: [] }
    unsafe { held.bytes.push_from(out.data(), out.len()) }
    total += held.bytes.len()
  }
  if total == 50 * 280 { 3 } else { 4 }
}
`); got != 3 {
		t.Errorf("under LSan: exited %d; want 3", got)
	}
}
