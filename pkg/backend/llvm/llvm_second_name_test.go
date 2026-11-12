package llvm

import "testing"

// The 11/11 use-after-frees, each run under ASan (and LeakSanitizer on Linux): a loop variable
// released by a write through a second name for its array, or a call that can write the global
// it walks, and a raw pointer bound out of a temporary. Each of these read freed memory before.
func TestExec_SecondNameUseAfterFrees(t *testing.T) {
	t.Parallel()
	const lib = `var g = ["a ${1}", "b ${2}"]
let poke = () -> void => { g[0] = "new ${3}" }
let reset = (xs: mut []string) -> void => { xs[0] = "reset ${4}" }
struct H { items: []string }
let word = (n: i64) -> string => "word ${n}"
let swap_in = (xs: []string, m: mut []string) -> i64 => { m = ["replaced ${5}"]; "${xs[0]}".len() }
let put = (s: string, m: mut []string) -> i64 => { m[0] = "over ${6}"; "${s}".len() }
struct Menu { focus: string, open: bool }
let open_at = (m: mut Menu, at: string) -> i64 => { m.focus = "elsewhere ${7}"; "${at}".len() }
`
	cases := []struct {
		name, src string
		want      int
	}{
		{"a second name writes the element", `let main = () -> u8 => {
  var ys = ["a ${1}", "b ${2}"]
  var zs = ys
  var n = 0
  for s in ys { zs[0] = "new ${3}"; n += "${s}".len() }
  u8(n)
}`, 6},
		{"a second name cleared", `let main = () -> u8 => {
  var ys = ["a ${1}", "b ${2}"]
  var zs = ys
  var n = 0
  for s in ys { zs.clear(); n += "${s}".len() }
  u8(n)
}`, 3},
		{"a second name handed to a call", `let main = () -> u8 => {
  var ys = ["a ${1}", "b ${2}"]
  var zs = ys
  var n = 0
  for s in ys { reset(zs); n += "${s}".len() }
  u8(n)
}`, 6},
		{"a struct holding the array", `let main = () -> u8 => {
  var ys = ["a ${1}", "b ${2}"]
  var h = H { items: ys }
  var n = 0
  for s in ys { h.items[0] = "new ${3}"; n += "${s}".len() }
  u8(n)
}`, 6},
		{"a call writes the global walked", `let main = () -> u8 => {
  var n = 0
  for y in g { poke(); n += "${y}".len() }
  u8(n)
}`, 6},
		{"a comprehension over a second name", `let main = () -> u8 => {
  var ys = ["a ${1}", "b ${2}"]
  var zs = ys
  let lens = [s in ys | { zs[0] = "z"; "${s}".len() }]
  u8(lens[0] + lens[1])
}`, 6},
		{"a bare argument beside a mut of its place", `let main = () -> u8 => {
  var ys = ["first ${1}"]
  let n = swap_in(ys, ys)
  u8(n + "${ys[0]}".len())
}`, 17},
		{"a bare element beside a second name's mut", `let main = () -> u8 => {
  var ys = ["first ${1}"]
  var zs = ys
  u8(put(ys[0], zs))
}`, 7},
		{"a bare field beside a mut of its struct", `let main = () -> u8 => {
  var m = Menu { focus: "here ${8}", open: false }
  u8(open_at(m, m.focus))
}`, 6},
		{"a pointer into a temporary", `let main = () -> u8 => {
  let p = unsafe { word(7).cstring_ptr() }
  let filler = word(9)
  let v = unsafe { p^ }
  u8(filler.len()) + v - 100
}`, 25},
		{"a pointer into a temporary, in a loop and past returns", `let first = (n: i64, stop: bool) -> u8 => {
  if stop { return 0 }
  let p = unsafe { word(n).cstring_ptr() }
  let filler = word(9)
  if n > 100 { return 1 }
  var c: u8 = 0
  for i in 0..<3 {
    let q = unsafe { word(i).cstring_ptr() }
    let more = word(i + 10)
    c += u8(unsafe { q^ } - 119) + u8(more.len())
  }
  unsafe { p^ } - 100 + u8(filler.len()) + c
}
let main = () -> u8 => first(7, false) + first(200, false) + first(1, true)`, 47},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, lib+tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}
