package llvm

import (
	"strings"
	"testing"
)

// A by-reference argument, and an argument after it that changes what its place is:
// `add(ys[0], renew(ys))`, where `renew` does `ys = [1, 2]`. The address used to be taken as the
// argument came up, so the callee wrote into a box the next argument had freed. A call now finds
// its by-reference places after every argument has run, as an assignment does. A global's array
// can't get here as a by-reference argument at all (lyra-E091), but a builtin's receiver can.
func TestExec_CallFindsByRefPlacesAfterItsArguments(t *testing.T) {
	t.Parallel()
	const lib = `
struct H { n: i64, xs: []i64 }
trait Bump { bump: (mut Self, i64) -> void }
impl Bump for H { bump = (self, n) => { self.n += n } }
var g = [0, 0, 0, 0, 0, 0]
var grid = [[0, 0], [0, 0]]
var b = H { n: 0, xs: [] }
var trail = ""
let shrink = () -> i64 => {
  g = [1, 2]
  3
}
let shrink_grid = () -> i64 => {
  grid = [[1]]
  3
}
let replace_b = () -> i64 => {
  b = H { n: 10, xs: [] }
  3
}
let renew_hs = (xs: mut []H) -> i64 => {
  xs = [H { n: 10, xs: [] }]
  3
}
let renew_rows = (xs: mut [][]i64) -> i64 => {
  xs = [[1]]
  3
}
let renew = (xs: mut []i64) -> i64 => {
  xs = [1, 2]
  3
}
let grow = (xs: mut []i64) -> i64 => {
  for i in 0..<1000 { xs.push(i) }
  3
}
let add = (x: mut i64, n: i64) -> void => {
  x += n
}
let add_after = (n: i64, x: mut i64) -> void => {
  x += n
}
let put = (xs: mut []i64, n: i64) -> void => {
  xs.push(n)
}
let up = (self: mut H, n: i64) -> void => {
  self.n += n
}
let mark = (s: string, n: i64) -> i64 => {
  trail = trail ++ s
  n
}
let both = (x: mut i64, y: mut i64) -> void => {
  x += 1
  y += 2
}
`
	cases := []struct {
		name, src, want string
	}{
		{"a global struct's field, the struct replaced", `add(b.n, replace_b())
  print("${b.n}")`, "13"},
		{"a local's element, renewed", `var ys = [0, 0, 0]
  add(ys[0], renew(ys))
  print("${ys[0]} ${ys[1]}")`, "4 2"},
		{"a local's element, grown", `var ys = [0, 0, 0]
  add(ys[0], grow(ys))
  print("${ys[0]} ${ys.len()}")`, "3 1003"},
		{"a block reassigning a local", `var ys = [0, 0, 0]
  add(ys[0], {
    ys = [1, 2]
    3
  })
  print("${ys[0]} ${ys[1]}")`, "4 2"},
		{"the by-reference argument after", `var ys = [0, 0, 0]
  add_after(renew(ys), ys[0])
  print("${ys[0]}")`, "4"},
		{"an element's array", `var rows = [[0, 0], [0, 0]]
  put(rows[0], renew_rows(rows))
  print("${rows[0].len()} ${rows[0][1]}")`, "2 3"},
		{"a method's receiver", `var hs = [H { n: 0, xs: [] }, H { n: 0, xs: [] }]
  up(hs[0], renew_hs(hs))
  print("${hs[0].n}")`, "13"},
		{"a method's receiver, by a dot", `var hs = [H { n: 0, xs: [] }, H { n: 0, xs: [] }]
  hs[0].up(renew_hs(hs))
  print("${hs[0].n}")`, "13"},
		{"a trait method's receiver", `var hs = [H { n: 0, xs: [] }, H { n: 0, xs: [] }]
  hs[0].bump(renew_hs(hs))
  print("${hs[0].n}")`, "13"},
		{"a trait path's receiver", `var hs = [H { n: 0, xs: [] }, H { n: 0, xs: [] }]
  Bump::bump(hs[0], renew_hs(hs))
  print("${hs[0].n}")`, "13"},

		// A builtin's receiver is a by-reference argument in all but name.
		{"push: a global", `g.push(shrink())
  print("${g.len()} ${g[2]}")`, "3 3"},
		{"push: a local", `var ys = [0, 0, 0]
  ys.push(renew(ys))
  print("${ys.len()} ${ys[2]}")`, "3 3"},
		{"push: an element", `grid[0].push(shrink_grid())
  print("${grid[0].len()} ${grid[0][1]}")`, "2 3"},
		{"push: its own length", `var ys = [0, 0, 0]
  ys.push(ys.len())
  print("${ys[3]}")`, "3"},
		{"push: a managed value", `var names = ["a" ++ ""]
  names.push(names[0] ++ "b")
  print("${names[1]}")`, "ab"},
		{"push_utf8", `var bytes: []u8 = []
  bytes.push_utf8({
    bytes = [97]
    "b" ++ ""
  })
  print("${bytes.len()}")`, "2"},
		{"reserve", `g.reserve(shrink() + 10)
  print("${g.len()}")`, "2"},

		// What doesn't change: left to right, each once, disjoint places, and a trap.
		{"operands left to right, each once", `var ys = [0, 0, 0]
  add_after(mark("a", 5), ys[mark("b", 1)])
  print("${trail} ${ys[1]}")`, "ab 5"},
		{"indexes before later arguments", `var ys = [0, 0, 0]
  add(ys[mark("a", 1)], mark("b", 5))
  print("${trail} ${ys[1]}")`, "ab 5"},
		{"a receiver's index before the pushed value", `var rows = [[0], [0]]
  rows[mark("a", 1)].push(mark("b", 5))
  print("${trail} ${rows[1][1]}")`, "ab 5"},
		{"two places", `struct Two { a: i64, b: i64 }
  var s = Two { a: 0, b: 0 }
  both(s.a, s.b)
  print("${s.a} ${s.b}")`, "1 2"},
		{"a temporary lent out", `print("${length(mk())}")`, "3"},
		{"an argument that doesn't come back", `var ys = [0, 0, 0]
  add(ys[0], if ys[0] == 0 { return } else { 0 })
  print("no")`, ""},
		{"an index that doesn't come back", `var ys = [0, 0, 0]
  add(ys[if ys[0] == 0 { return } else { 0 }], 1)
  print("no")`, ""},
		{"a trait method's receiver that doesn't come back", `var ys = [H { n: 0, xs: [] }]
  ys[if ys[0].n == 0 { return } else { 0 }].bump(1)
  print("no")`, ""},
		{"a pushed value that doesn't come back", `var ys = [0, 0, 0]
  ys.push(if ys[0] == 0 { return } else { 0 })
  print("no")`, ""},
	}
	const helpers = `
let mk = () -> string => "mk" ++ "!"
let length = (s: ref string) -> i64 => s.byte_len()
`
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := c.src
			decl := ""
			if strings.HasPrefix(src, "struct ") {
				line, rest, _ := strings.Cut(src, "\n")
				decl, src = line+"\n", strings.TrimLeft(rest, " ")
			}
			prog := lib + helpers + decl + "let run = () -> void => {\n  " + src + "\n}\nlet main = () -> void => { run() }"
			if got := strings.TrimSpace(buildAndRunASanStdout(t, prog)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The trap is a question about the array the callee will get, so it comes with finding it.
func TestExec_CallChecksByRefBoundsAfterItsArguments(t *testing.T) {
	t.Parallel()
	const lib = `
let renew = (xs: mut []i64) -> i64 => {
  xs = [1, 2]
  3
}
let boom = () -> i64 => panic("boom")
let add = (x: mut i64, n: i64) -> void => {
  x += n
}
`
	cases := []struct{ name, src, want string }{
		{"against the new array", "var ys = [0, 0, 0, 0, 0, 0]\n  add(ys[5], renew(ys))", "out of bounds"},
		{"after the other argument", "var ys = [0]\n  add(ys[9], boom())", "boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunPanic(t, lib+"let main = () -> void => {\n  "+c.src+"\n}\n")
			if code != 101 {
				t.Fatalf("exited %d; want the trap's 101, output %q", code, out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("output = %q; want %q", out, c.want)
			}
		})
	}
}

// A receiver reached through a namespace is the module's global, found after the arguments as a
// local receiver is (namesStorage).
func TestExec_ABuiltinsReceiverThroughANamespace(t *testing.T) {
	t.Parallel()
	got := buildAndRunModulesASan(t, map[string]string{
		"lib.lyra": `module lib
pub var queue = [1, 2]
`,
		"app.lyra": `import lib

let main = () -> u8 => {
  lib.queue.push(3)
  lib.queue.reserve(10)
  if lib.queue.len() == 3 { 3 } else { 0 }
}`,
	})
	if got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

// A write through a namespace lands in the module's global (11/11): an element, a field, the
// whole global, a compound assignment, a `mut` argument and `&mut`. Each said `undefined
// identifier "lib"` before, and the `mut` argument and `&mut` reached a copy. The managed
// ones release what they replace, which ASan would see read again if they released wrongly.
func TestExec_WritesThroughANamespace(t *testing.T) {
	t.Parallel()
	got := buildAndRunModulesASan(t, map[string]string{
		"lib.lyra": `module lib
pub struct Point { x: i64, name: string }
pub var queue: []i64 = [0]
pub var names: []string = ["a", "b"]
pub var at = Point { x: 1, name: "p" }
pub var count = 0
pub let mut grow: []i64 = [1]
pub let add = (x: i64) -> void => queue.push(x)
`,
		"app.lyra": `import lib

let bump = (xs: mut []i64) -> void => xs.push(1)
let rename = (p: mut lib.Point) -> void => { p.name = "renamed ${p.x}" }

let main = () -> u8 => {
  lib.queue[0] = 5
  lib.add(lib.queue[0])
  if lib.queue.len() != 2 || lib.queue[1] != 5 { return 1 }
  lib.names[0] = "x ${lib.count}"
  lib.names = ["n", "m ${lib.count}"]
  if lib.names[1] != "m 0" { return 2 }
  lib.at.name = "q ${lib.count}"
  lib.at.x += 4
  if lib.at.x != 5 || lib.at.name != "q 0" { return 3 }
  lib.at = lib.Point { x: 9, name: "r ${lib.count}" }
  lib.count = 3
  lib.count += 1
  lib.grow[0] = 7
  if lib.at.x != 9 || lib.at.name != "r 0" || lib.count != 4 || lib.grow[0] != 7 { return 4 }
  bump(lib.grow)
  rename(lib.at)
  if lib.grow.len() != 2 || lib.at.name != "renamed 9" { return 5 }
  let p = unsafe { &mut lib.count }
  unsafe { p^ = 11 }
  if lib.count != 11 { return 6 }
  0
}`,
	})
	if got != 0 {
		t.Errorf("exit %d: check %d failed", got, got)
	}
}
