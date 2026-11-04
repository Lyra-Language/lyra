package llvm

import (
	"strings"
	"testing"
)

// What a program writes now that a return takes no mode and an `own` function is no value
// (lyra-E093, lyra-E082). Under ASan, because each of the return shapes was a use after free
// written `-> ref`: the function released what it returned and the caller printed garbage.
func TestExec_ModesOutsideParameters_WhatToWriteInstead(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"a fresh value returned", `
let f = (a: string) -> string => a ++ "b"
let main = () -> void => {
  println(f("x"))
  let s = f("y")
  println(s)
}
`, "xb\nyb"},
		{"a field of a local returned", `
struct Holder { name: string }
let f = (a: string) -> string => {
  let b = Holder { name: a ++ "z" }
  b.name
}
let main = () -> void => {
  let s = f("y")
  println(s)
}
`, "yz"},
		{"a local array returned", `
let f = () -> []i64 => {
  let a = [1, 2, 3]
  a
}
let main = () -> void => println(f()[1])
`, "2"},
		{"an own parameter returned", `
let f = (s: own string) -> string => s
let main = () -> void => {
  let t = f("a" ++ "b")
  println(t)
}
`, "ab"},
		{"an accessor's result outlives what it read", `
struct Holder { name: string }
let name_of = (self: ref Holder) -> string => self.name
let main = () -> void => {
  var b = Holder { name: "a" ++ "z" }
  let s = name_of(b)
  b = Holder { name: "other" ++ "q" }
  println(s)
  println(b.name)
}
`, "az\notherq"},
		{"writing back what a function returned", `
let grown = (x: mut i64) -> i64 => {
  x += 1
  x
}
let main = () -> void => {
  var a = 1
  var b = grown(a)
  b = 9
  println(a)
  println(b)
}
`, "2\n9"},
		{"a tuple's elements owned where a mode was refused", `
let f = (x: (string, i64)) -> string => x.0 ++ "!"
let main = () -> void => {
  let p = ("a" ++ "b", 1)
  println(f(p))
  println(p.0)
}
`, "ab!\nab"},
		{"a nested own lambda called by its name", `
let main = () -> void => {
  let h = (s: own string) -> string => s ++ "?"
  println(h("a" ++ "b"))
}
`, "ab?"},
		{"an own function wrapped to be a value", `
let keep = (s: own string) -> string => s ++ "!"
let apply = (g: (string) -> string, t: string) -> string => g(t)
let main = () -> void => {
  let t = "c" ++ "d"
  println(apply((s) => keep(s), t))
  println(t)
}
`, "cd!\ncd"},
		{"a scalar own function as a value", `
let bump = (n: own i64) -> i64 => n + 1
let apply = (g: (i64) -> i64, n: i64) -> i64 => g(n)
let main = () -> void => println(apply(bump, 1))
`, "2"},
		{"a trait method's modes", `
trait Sink {
  put: (mut Self, own string, ref string) -> void
}
struct Log { lines: []string }
impl Sink for Log {
  put = (self, line, sep) => self.lines.push(line ++ sep)
}
let main = () -> void => {
  var l = Log { lines: [] }
  l.put("a" ++ "b", ";")
  println(l.lines[0])
}
`, "ab;"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := strings.TrimSpace(buildAndRunASanStdout(t, c.src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
