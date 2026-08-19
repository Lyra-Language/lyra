package llvm

import (
	"strings"
	"testing"
)

// Three bound-dispatch bugs, each of which type-checked cleanly and failed at or after
// lowering. They are grouped because they share one mechanism: a call the typechecker
// resolved *abstractly* — the receiver was a type variable there — and that only a
// specialization can name a function for.
func TestExec_BoundCallDispatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// **The module makes it fail.** The candidate is published under the
			// typechecker's spelling of the type (`Box<i64>`, and the mono key
			// `Box$i64`); the backend used to ask under the *instantiated* name, which
			// carries the declaring module's key — `main__Box$i64` — so a generic impl's
			// candidate was never found. Drop the `module` line and the same program ran,
			// which is what kept this out of every snippet-sized reproduction.
			"a bound call whose impl target is generic",
			`struct Box<t> { v: t }
			 trait Sized2 { pure size: (Self) -> i64 }
			 impl Sized2 for Box<t> { size = pure (self) => 3 }
			 let twice<t> where t: Sized2 = pure (v: t) -> i64 => v.size() * 2
			 let main = () -> void => {
			   let b: Box<i64> = Box { v: 5 }
			   println(twice(b))
			 }`,
			"6",
		},
		{
			// **A `mut` receiver through a bound was a wild load, not a mismatch.** A
			// bound call has no entry in the resolution table — its concrete impl comes
			// from the candidate table at lowering — so reading modes from the table
			// alone returned nil and every operand went by value. The emitted method
			// takes a pointer for a `mut` receiver, so it was handed a struct.
			"a bound call with a mut receiver",
			`trait Bump { bump: (mut Self, i64) -> void }
			 struct Counter { n: i64 }
			 impl Bump for Counter { bump = (self, n) => { self.n = self.n + n } }
			 let twice<t> where t: Bump = (v: mut t, n: i64) -> void => {
			   v.bump(n)
			   v.bump(n)
			 }
			 let main = () -> void => {
			   var c = Counter { n: 0 }
			   twice(c, 10)
			   println(c.n)
			 }`,
			"20",
		},
		{
			// The same fault reached through a trait **default**, which is the shape that
			// found it: a default body's `self` is a type variable, so every call on it
			// is a bound call.
			"a default with a mut receiver",
			`trait Bump {
			   bump: (mut Self, i64) -> void
			   bump_twice: (mut Self, i64) -> void = (self, n) => {
			     self.bump(n)
			     self.bump(n)
			   }
			 }
			 struct Counter { n: i64 }
			 impl Bump for Counter { bump = (self, n) => { self.n = self.n + n } }
			 let main = () -> void => {
			   var c = Counter { n: 0 }
			   c.bump_twice(10)
			   println(c.n)
			 }`,
			"20",
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

// `Trait::method(receiver, …)` — the fully-qualified call form — type-checked and then
// died in the backend as "no type recorded for the callee of an indirect call", because
// the call lowering knew a `.`-callee and an identifier and nothing else, so a
// TraitMethodPathExpr fell through to the function-value path.
//
// It is the spelling the ambiguity diagnostic tells you to reach for ("use
// TraitName::method(...) to disambiguate"), so the compiler was recommending a form that
// did not build.
func TestExec_TraitMethodPathCall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"on an impl's own method",
			`trait Named { pure name: (Self) -> string }
			 struct Cat { n: i64 }
			 impl Named for Cat { name = pure (self) => "cat" }
			 let main = () -> void => { println(Named::name(Cat { n: 1 })) }`,
			"cat",
		},
		{
			// The receiver is argument 0 here rather than a separate expression, so
			// arguments and signature parameters are index-aligned — the offset a
			// `.`-call applies would be off by one.
			"with arguments after the receiver",
			`trait Add2 { pure add2: (Self, i64, i64) -> i64 }
			 struct N { v: i64 }
			 impl Add2 for N { add2 = pure (self, a, b) => self.v + a + b }
			 let main = () -> void => { println(Add2::add2(N { v: 1 }, 2, 3)) }`,
			"6",
		},
		{
			"on a trait method's default",
			`trait Named {
			   pure name: (Self) -> string
			   pure shout: (Self) -> string = (self) => self.name() ++ "!"
			 }
			 struct Cat { n: i64 }
			 impl Named for Cat { name = pure (self) => "cat" }
			 let main = () -> void => { println(Named::shout(Cat { n: 1 })) }`,
			"cat!",
		},
		{
			// The by-reference modes have to travel here too.
			"with a mut receiver",
			`trait Bump { bump: (mut Self, i64) -> void }
			 struct Counter { n: i64 }
			 impl Bump for Counter { bump = (self, n) => { self.n = self.n + n } }
			 let main = () -> void => {
			   var c = Counter { n: 0 }
			   Bump::bump(c, 5)
			   println(c.n)
			 }`,
			"5",
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
