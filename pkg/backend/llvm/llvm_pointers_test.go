package llvm

import (
	"strings"
	"testing"
)

// Raw pointers lower to LLVM pointers and nothing more: `&x` is the address of x's
// storage — the same address a `mut` parameter is passed by — `p^` is a load, `p^ = v` a
// store, and `unsafe { … }` is its body. There is no ownership, no refcounting and no drop
// glue, which is what makes a raw pointer raw.
//
// The type, the grammar, the collector and lyra-E011's unsafe-context policy all existed
// since long before; what was missing was inference and lowering, so every form was
// refused at the expression (lyra-E051) until 08/19.
func TestExec_RawPointers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"read through a pointer",
			`let main = () -> void => {
			   var n = 41
			   unsafe { println("${(&n)^}") }
			 }`,
			"41",
		},
		{
			// The write is the half that proves `&` yields the address of the *storage*
			// rather than of a copy: `n` itself has to change.
			"write through a mutable pointer",
			`let main = () -> void => {
			   var n = 41
			   unsafe {
			     let p = &mut n
			     p^ = p^ + 1
			   }
			   println("${n}")
			 }`,
			"42",
		},
		{
			// A field and an element are storage too. Both go through the same
			// lvalue-address machinery a `mut` argument uses.
			"pointer to a field and to an element",
			`struct Pt { x: i64, y: i64 }
			 let main = () -> void => {
			   var p = Pt { x: 1, y: 2 }
			   var xs: []i64 = [10, 20, 30]
			   unsafe {
			     let fp = &mut p.y
			     fp^ = 99
			     let ep = &mut xs[1]
			     ep^ = 21
			   }
			   println("${p.y} ${xs[1]}")
			 }`,
			"99 21",
		},
		{
			// A pointer as a parameter type, through an `unsafe` function — which is the
			// shape any future FFI shim takes.
			"pointer parameters and unsafe functions",
			`let bump = unsafe (p: ^mut i64) -> void => { p^ = p^ + 1 }
			 let readAt = unsafe (p: ^i64) -> i64 => p^
			 let main = () -> void => {
			   var n = 41
			   unsafe {
			     bump(&mut n)
			     println("${readAt(&n)}")
			   }
			 }`,
			"42",
		},
		{
			// An `unsafe` block in *value* position. It is its body, so it yields what
			// the body yields — the keyword changes what is permitted inside it and has
			// no runtime meaning of its own.
			"an unsafe block as a value",
			`let main = () -> void => {
			   var n = 20
			   let doubled = unsafe {
			     let p = &n
			     p^ * 2
			   }
			   println("${doubled}")
			 }`,
			"40",
		},
		{
			// A pointer to a narrower width. The store has to be coerced, or an untyped
			// literal arrives at the i64 default and clang refuses the module.
			"a pointer to a narrow integer",
			`let main = () -> void => {
			   var n: u8 = 1
			   unsafe {
			     let p = &mut n
			     p^ = 200
			   }
			   println("${n}")
			 }`,
			"200",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "module main\n" + tc.src
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}
