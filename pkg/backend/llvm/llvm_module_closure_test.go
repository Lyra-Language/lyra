package llvm

import (
	"strings"
	"testing"
)

// A closure over a value of a **named aggregate** must lower in a program with a
// `module` header.
//
// It did not: `llvm: cannot lower captured binding "s": llvm: unknown named type "Pt"`
// for a lambda capturing a struct, in any file declaring a module. The nested-lambda
// loops (llvm.go) lower a lifted body from the top level rather than from the enclosing
// function, so neither declareClosure nor defineClosure entered its module — the same
// omission the specialization path had (llvm_module_generic_test.go) — and
// `l.currentLoc` was the zero Location by then. A named type is keyed
// `<module>::<name>` (rule 4), so the lookup went out under the bare name and missed.
//
// Every capture test that existed omits the `module` line, which is why this sat
// undetected: without a header the module is "" and the bare key is the right one. It is
// not a bug about closures either — a *signature* mentioning the type fails the same way
// at declareClosure, so the cases below cover the return and parameter positions too.
func TestExec_ClosureOverAModuleType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		// The reported case: a struct captured by a lambda.
		{
			"captures a struct",
			`
module main
struct Pt { a: u32 }
let main = () -> void => {
  let s = Pt { a: 5 }
  let cap = pure () -> u32 => s.a
  println("${cap()}")
}
`,
			"5",
		},
		// A `data` type — the tagged union — captured the same way. Every named
		// aggregate shares the lookup, so no kind is what the bug was about.
		{
			"captures a data value",
			`
module main
data Shape = Circle(u32) | Square(u32)
let main = () -> void => {
  let s = Circle(5)
  let cap = pure () -> u32 => match s { Circle(r) => r, Square(w) => w }
  println("${cap()}")
}
`,
			"5",
		},
		// A `union` — the untagged, C-shaped one. It was already known to fail here
		// when unions landed, and routed around in llvm_union_test.go rather than
		// pinned there, since the bug was not the feature's.
		{
			"captures a union",
			`
module main
union U { a: u32, b: f32 }
let main = () -> void => {
  let u = U { a: 5 }
  let cap = () -> u32 => unsafe { u.a }
  println("${cap()}")
}
`,
			"5",
		},
		// A named tuple, the fourth named aggregate.
		{
			"captures a named tuple",
			`
module main
tuple Pair(u32, u32)
let main = () -> void => {
  let p = Pair(3, 4)
  let cap = pure () -> u32 => p.0 + p.1
  println("${cap()}")
}
`,
			"7",
		},
		// No capture at all: the type is only in the lambda's *return* position, so
		// this fails at declareClosure rather than at the environment's layout.
		{
			"a lambda returning a module type",
			`
module main
struct Pt { a: u32 }
let main = () -> void => {
  let make = pure () -> Pt => Pt { a: 5 }
  println("${make().a}")
}
`,
			"5",
		},
		// The other half of the signature, for the same reason.
		{
			"a lambda taking a module type",
			`
module main
struct Pt { a: u32 }
let main = () -> void => {
  let f = pure (p: Pt) -> u32 => p.a
  println("${f(Pt { a: 7 })}")
}
`,
			"7",
		},
		// The closure outlives the frame the struct was created in, so the capture is
		// a real owned copy in the environment rather than a read of a live local.
		{
			"a captured struct escapes with the closure",
			`
module main
struct Pt { a: u32 }
let mk = (p: Pt) -> () -> u32 => pure () -> u32 => p.a
let main = () -> void => {
  let f = mk(Pt { a: 9 })
  println("${f()}")
}
`,
			"9",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, c.src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}
