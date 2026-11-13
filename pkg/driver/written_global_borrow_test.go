package driver_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// writtenGlobalBorrows is how many lyra-E098 the program draws; any other error fails.
func writtenGlobalBorrows(t *testing.T, files map[string]string) int {
	t.Helper()
	got := 0
	for _, d := range analyzeModules(t, files).Errors() {
		if d.Code == diag.CodeWrittenGlobalBorrow {
			got++
		} else {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
	return got
}

const writtenGlobalLib = `module main
var g = ["a ${1}"]
var counts = [0]
struct State { items: []string }
var state = State { items: ["s ${1}"] }
let grow = () -> void => { g.push("grown ${2}") }
let via = (s: ref string) -> i64 => {
  grow()
  "${s}".len()
}
let count = (s: ref string) -> i64 => {
  counts.push(1)
  "${s}".len()
}
let look = (s: ref string) -> i64 => "${s}".len()
let apply = (s: ref string, f: () -> void) -> i64 => {
  f()
  "${s}".len()
}
let reset = (xs: mut []string) -> void => { xs = [] }
let wipe = (s: ref string) -> i64 => {
  reset(g)
  "${s}".len()
}
let grow_state = (s: ref string) -> i64 => {
  state.items.push("more ${3}")
  "${s}".len()
}
struct Holder { n: i64 }
impl Holder {
  let take = (self: ref, s: ref string) -> i64 => {
    grow()
    self.n + "${s}".len()
  }
}
`

// Storage in a box lent to a call that can grow, replace or rewrite a mutable global able to
// hold that box is refused where it is lent: after `var zs = g`, the two are one array, and
// growing `g` frees the buffer `zs[0]` points into (it read freed memory under ASan, 11/12).
func TestWrittenGlobalBorrow_IsRefused(t *testing.T) {
	cases := []struct{ name, extra, body string }{
		{"a callee growing the global through another function", "", `var zs = g
  let n = via(zs[0])`},
		{"a callee lending the global `mut`", "", `var zs = g
  let n = wipe(zs[0])`},
		{"a field of a global struct grown", "", `var zs = state.items
  let n = grow_state(zs[0])`},
		{"a method that grows the global", "", `var zs = g
  let h = Holder { n: 1 }
  let n = h.take(zs[0])`},
		{"a lambda handed to the call", "", `var zs = g
  let n = apply(zs[0], () => g.push("cb ${4}"))`},
		{"a function named at the call", "", `var zs = g
  let n = apply(zs[0], grow)`},
		{"a parameter's array, which can be the global",
			"let from_param = (xs: []string) -> i64 => via(xs[0])\n", `let n = 0`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := writtenGlobalLib + c.extra + `let main = () -> void => {
  ` + c.body + `
}
`
			if got := writtenGlobalBorrows(t, map[string]string{"main.lyra": src}); got != 1 {
				t.Errorf("got %d lyra-E098, want 1", got)
			}
		})
	}
}

// A call through a function value — a field, a reassigned global, an element — may run any
// function that escaped as a value taking as many parameters: `grow`, named here, is one. The
// field's case read freed memory under ASan (11/12). Arity tells one apart: a hook taking an
// `i64` can't be `grow`.
func TestWrittenGlobalBorrow_ThroughAFunctionValue(t *testing.T) {
	const lib = `module main
var g = ["a ${1}"]
let grow = () -> void => { g.push("grown ${2}") }
struct Hooks { on_use: () -> void }
struct Counted { on_use: (i64) -> void }
var hook: () -> void = () => {}
let by_field = (s: ref string, h: Hooks) -> i64 => {
  h.on_use()
  "${s}".len()
}
let by_global = (s: ref string) -> i64 => {
  hook()
  "${s}".len()
}
let by_element = (s: ref string, hs: []() -> void) -> i64 => {
  for h in hs { h() }
  "${s}".len()
}
let by_count = (s: ref string, h: Counted) -> i64 => {
  h.on_use(1)
  "${s}".len()
}
`
	cases := []struct {
		name, body string
		want       int
	}{
		{"a struct's field", `let h = Hooks { on_use: grow }
  var zs = g
  let n = by_field(zs[0], h)`, 1},
		{"a global reassigned", `hook = grow
  var zs = g
  let n = by_global(zs[0])`, 1},
		{"an element of an array", `let hs = [grow]
  var zs = g
  let n = by_element(zs[0], hs)`, 1},
		{"a value of another arity", `let keep = grow
  let h = Counted { on_use: (n: i64) => println("${n}") }
  var zs = g
  let n = by_count(zs[0], h)`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := lib + `let main = () -> void => {
  ` + c.body + `
}
`
			if got := writtenGlobalBorrows(t, map[string]string{"main.lyra": src}); got != c.want {
				t.Errorf("got %d lyra-E098, want %d", got, c.want)
			}
		})
	}
}

// Another module's global, grown by a function of that module called through its namespace.
func TestWrittenGlobalBorrow_AnotherModulesGlobal(t *testing.T) {
	got := writtenGlobalBorrows(t, map[string]string{
		"lib.lyra": `module lib
pub var items = ["a ${1}"]
pub let add = () -> void => { items.push("b ${2}") }
`,
		"main.lyra": `module main
import lib
let via = (s: ref string) -> i64 => {
  lib.add()
  "${s}".len()
}
let main = () -> void => {
  var zs = lib.items
  let n = via(zs[0])
}
`,
	})
	if got != 1 {
		t.Errorf("got %d lyra-E098, want 1", got)
	}
}

// Nothing the call writes can be the box lent: a global of another type, no write at all, a
// scalar lent by `ref` (a value), and the element copied into a local first.
func TestWrittenGlobalBorrow_IsQuietWhereNothingCanBeFreed(t *testing.T) {
	cases := []struct{ name, body string }{
		{"a global that cannot hold the box", `var zs = g
  let n = count(zs[0])`},
		{"a callee writing nothing", `var zs = g
  let n = look(zs[0])`},
		{"a lambda that writes nothing", `var zs = g
  let n = apply(zs[0], () => println("hi"))`},
		{"a scalar lent by ref", `var ns = counts
  let n = scalar(ns[0])`},
		{"a copy in a local", `var zs = g
  let s = zs[0]
  let n = via(s)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := writtenGlobalLib + `let scalar = (x: ref i64) -> i64 => {
  counts.push(2)
  x
}
let main = () -> void => {
  ` + c.body + `
}
`
			if got := writtenGlobalBorrows(t, map[string]string{"main.lyra": src}); got != 0 {
				t.Errorf("got %d lyra-E098, want none", got)
			}
		})
	}
}
