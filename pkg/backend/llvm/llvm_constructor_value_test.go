package llvm

import (
	"strings"
	"testing"
)

// **A constructor with a payload is a function value** (10/19): `Width` where a function
// is wanted is `(p0) => Width(p0)` (pkg/driver/constructor_value.go), typed by its context
// or, with none, by its declaration. Every position a function value reaches: an argument,
// an annotated and an unannotated binding, a top-level one, a struct field, a generic call
// (`map(Wrap)` solves Box's `t` from `map`'s), two payload fields, and a closure
// capturing a local around one.
func TestExec_AConstructorIsAFunctionValue(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
data Ev = Width(i64) | Named(string, i64) | Quit
data Box<t> = Wrap(t) | Empty
struct Handler { on_change: (i64) -> Ev }
let show = (e: Ev) -> string => match e {
  Width(n) => "w${n}",
  Named(s, n) => "${s}${n}",
  Quit => "q",
}
let apply = (f: (i64) -> Ev, n: i64) -> Ev => f(n)
let make = Width
let main = () -> void => {
  let typed: (i64) -> Ev = Width
  let inferred = Width
  let pair: (string, i64) -> Ev = Named
  let h = Handler { on_change: Width }
  let boxes: []Box<i64> = [1, 2, 3].map(Wrap)
  let k = 10
  let shifted = (x: i64) -> Ev => [x + k].map(Width)[0]
  let unboxed = match boxes[2] { Wrap(n) => n, Empty => 0 }
  println("${show(apply(Width, 1))} ${show(typed(2))} ${show(inferred(3))} ${show(make(4))}")
  println("${show(pair("p", 5))} ${show(h.on_change(6))} ${unboxed} ${show(shifted(1))} ${show(Quit)}")
}
`, "")
	want := "w1 w2 w3 w4\np5 w6 3 w11 q"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("constructors as function values:\n%s\nwant:\n%s", got, want)
	}
}

// A string payload through the lambda a constructor becomes, under ASan: the parameter is
// owned and moved into the payload, so a missing retain is a use-after-free once the array
// mapped over is released.
func TestExec_AConstructorValueMovesItsPayloadASan(t *testing.T) {
	t.Parallel()
	src := `module main
data Tag = Label(string) | Blank
let main = () -> u8 => {
  var total = 0
  for round in 0..<10 {
    let names = ["a" ++ "${round}", "b" ++ "${round}", "c" ++ "${round}"]
    let tags = names.map(Label)
    for tag in tags {
      match tag { Label(s) => total += s.len(), Blank => total += 100 }
    }
  }
  if total == 60 { 3 } else { 1 }
}`
	if got := exitCode(t, command(preludeBinary(t, src)).Run()); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// **A lambda in a generic struct's field takes what the field states** (10/19): against
// `(i64) -> e`, `(v) => Width(v)` gets `v: i64` before `e` is solved — from the lambda's own
// body — and its return once it is, which the backend lowers it from. Each spelling: an
// annotated binding, an unannotated one, an argument, a parameter written, a branching
// body, a capture, and a field whose `e` is a plain integer.
func TestExec_ALambdaInAGenericStructFieldIsTypedByIt(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
data Ev = Width(i64) | Quit
struct IntAttrs<e> { on_change: (i64) -> e, label: string }
let show = (e: Ev) -> string => match e { Width(n) => "w${n}", Quit => "q" }
let fire = (a: IntAttrs<Ev>) -> Ev => a.on_change(5)
let main = () -> void => {
  let step = 100
  let annotated: IntAttrs<Ev> = IntAttrs { on_change: (v) => Width(v), label: "a" }
  let bare = IntAttrs { on_change: (v) => Width(v), label: "b" }
  let written = IntAttrs { on_change: (v: i64) => Width(v), label: "c" }
  let branching = IntAttrs { on_change: (v) => if v > 3 { Width(v) } else { Quit }, label: "d" }
  let capturing = IntAttrs { on_change: (v) => Width(v + step), label: "e" }
  let doubled = IntAttrs { on_change: (v) => v * 2, label: "f" }
  println("${show(annotated.on_change(1))} ${show(bare.on_change(2))} ${show(fire(IntAttrs { on_change: (v) => Width(v), label: "g" }))}")
  println("${show(written.on_change(6))} ${show(branching.on_change(1))} ${show(capturing.on_change(7))} ${doubled.on_change(4)}")
}
`, "")
	want := "w1 w2 w5\nw6 q w107 8"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("lambdas in a generic struct field:\n%s\nwant:\n%s", got, want)
	}
}
