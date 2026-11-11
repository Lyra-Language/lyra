package llvm

import (
	"regexp"
	"testing"
)

// A call handing back a raw pointer pins what it borrowed to the end of its scope, as `&xs[0]`
// does: a raw pointer keeps storage alive without counting as a reference, so last use, which
// sees only the names, freed the array the moment the pointer was taken (11/02). Under ASan,
// which is Linux's to catch: macOS's stays silent on these.
func TestExec_ARawPointerPinsWhatItBorrowed(t *testing.T) {
	t.Parallel()
	const common = `import std.ffi
import std.ffi.{ CBuffer }
struct Buf { bytes: []u8 }
impl Buf {
  let first_byte = unsafe (self) -> ^u8 => &self.bytes[0]
}
struct Holder { items: []u8, n: i64 }
let of_holder = unsafe (h: Holder) -> ^u8 => &h.items[0]
let of_pair = unsafe (w: ([]u8, i64)) -> ^u8 => &w.0[0]
let first_of = unsafe (xs: []u8) -> ^u8 => &xs[0]
let view = unsafe (xs: []u8) -> CBuffer => CBuffer { ptr: &xs[0], len: xs.len() }
let bytes = (a: u8, b: u8) -> []u8 => {
  var xs: []u8 = [a]
  xs.push(b)
  xs
}
var kept: ^u8 = nullptr
let remember = (p: ^u8) -> void => { kept = p }
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"data, read on a later line", `let main = () -> u8 => {
  let xs = bytes(7, 8)
  let p = unsafe { xs.data() }
  unsafe { p^ }
}`, 7},
		{"data_mut, written on a later line", `let main = () -> u8 => {
  var xs = bytes(7, 8)
  let p = unsafe { xs.data_mut() }
  unsafe { p^ = 9 }
  unsafe { p^ }
}`, 9},
		{"kept past the call, as C keeps it", `let main = () -> u8 => {
  let xs = bytes(11, 8)
  remember(unsafe { xs.data() })
  println("a statement between")
  unsafe { kept^ }
}`, 11},
		{"a string's own bytes", `let main = () -> u8 => {
  let s = program_args()[0] ++ "x"
  let p = unsafe { s.cstring_ptr() }
  println("a statement between")
  if unsafe { p^ } == 0 { 1 } else { 12 }
}`, 12},
		{"a free function's result", `let main = () -> u8 => {
  let xs = bytes(13, 8)
  let p = unsafe { first_of(xs) }
  unsafe { p^ }
}`, 13},
		{"a method of the program's own", `let main = () -> u8 => {
  let b = Buf { bytes: bytes(14, 8) }
  let p = unsafe { b.first_byte() }
  unsafe { p^ }
}`, 14},
		{"rooted at a field", `let main = () -> u8 => {
  let h = Holder { items: bytes(15, 8), n: 1 }
  let p = unsafe { h.items.data() }
  unsafe { p^ }
}`, 15},
		{"held in a struct the call returns", `let main = () -> u8 => {
  let xs = bytes(16, 8)
  let c = unsafe { view(xs) }
  c.get(0)
}`, 16},
		{"through a literal in the argument", `let main = () -> u8 => {
  let xs = bytes(18, 8)
  let p = unsafe { of_holder(Holder { items: xs, n: 1 }) }
  println("a statement between")
  unsafe { p^ }
}`, 18},
		{"through a tuple in the argument", `let main = () -> u8 => {
  let xs = bytes(19, 8)
  let p = unsafe { of_pair((xs, 1)) }
  println("a statement between")
  unsafe { p^ }
}`, 19},
		{"each pass of a loop frees its own", `let main = () -> u8 => {
  var total: u8 = 0
  for i in 0..<3 {
    let xs = bytes(u8(i + 1), 8)
    let p = unsafe { xs.data() }
    total += unsafe { p^ }
  }
  total
}`, 6},
		// A pointer into a borrowed element pins what the element is borrowed from: the
		// pin fell on the binder, and the array it borrows from was freed at its last
		// mention, the comprehension (bindings.imgui's `[]string` → `^^u8`, 11/11).
		{"from a comprehension's element", `let main = () -> u8 => {
  let rows = [bytes(21, 8), bytes(22, 8)]
  let ptrs = [r in rows | unsafe { r.data() }]
  println("a statement between")
  unsafe { ptrs[1]^ }
}`, 22},
		{"from a for loop's element", `let main = () -> u8 => {
  let rows = [bytes(23, 8), bytes(24, 8)]
  var ptrs: []^u8 = []
  for r in rows { ptrs.push(unsafe { r.data() }) }
  println("a statement between")
  unsafe { ptrs[0]^ }
}`, 23},
		{"from an if let's payload", `let main = () -> u8 => {
  let row: Maybe<[]u8> = Some(bytes(25, 8))
  var p: ^u8 = nullptr
  if let Some(r) = row { p = unsafe { r.data() } }
  println("a statement between")
  unsafe { p^ }
}`, 25},
		{"inside a branch", `let main = () -> u8 => {
  let xs = bytes(17, 8)
  if xs.len() == 2 {
    let p = unsafe { xs.data() }
    unsafe { p^ }
  } else { 0 }
}`, 17},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, common+tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// The same, read off the IR, so a Mac sees it too: the release follows the read.
func TestEmit_ARawPointerKeepsItsArrayPastItsRead(t *testing.T) {
	t.Parallel()
	ir := emitWithPrelude(t, `import std.ffi
@symbol("puts")
unsafe extern c_puts: (s: ^u8) -> i32
let plain = (a: u8) -> i32 => {
  var xs: []u8 = [a]
  xs.push(0)
  let p = unsafe { xs.data() }
  unsafe { c_puts(p) }
}
let main = () -> u8 => {
  plain(104)
  0
}`)
	body := regexp.MustCompile(`(?s)define [^\n]*plain\(.*?\n}`).FindString(ir)
	order := regexp.MustCompile(`@puts|@lyra_rc_release`).FindAllString(body, -1)
	if len(order) == 0 || order[0] != "@puts" {
		t.Errorf("the array is released before the read through its pointer: %v\n%s", order, body)
	}
}
