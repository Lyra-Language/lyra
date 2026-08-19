package llvm

import (
	"strings"
	"testing"
)

// A trait method may carry a **default body**, which an impl inherits by writing nothing
// and overrides by writing a clause. It parsed and collected from the beginning and was
// dispatched to by nobody: `n.twice()` on a type with an `impl` reported "has no method".
//
// The body is checked once with `Self` as a type variable bounded by the declaring trait,
// and monomorphized per implementing type — the arrangement a generic function already
// has, which is why the backend needed no new machinery for any of this. These tests are
// the evidence for that claim: every case below is a distinct way the shared body could
// have been lowered at the wrong type.
func TestExec_TraitDefaultMethods(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// The base case, and the one the todo entry reported.
			"an impl inherits the default",
			`trait Named {
			   pure name: (Self) -> string
			   pure shout: (Self) -> string = (self) => self.name() ++ "!"
			 }
			 struct Cat { n: i64 }
			 impl Named for Cat { name = pure (self) => "cat" }
			 let main = () -> void => { println(Cat { n: 1 }.shout()) }`,
			"cat!",
		},
		{
			// **The case that fails if the body is lowered at one type for all of them.**
			// One body, two receivers: each specialization must call its own `name`.
			"two impls share one body and keep their own types",
			`trait Named {
			   pure name: (Self) -> string
			   pure shout: (Self) -> string = (self) => self.name() ++ "!"
			 }
			 struct Cat { n: i64 }
			 struct Dog { n: i64 }
			 impl Named for Cat { name = pure (self) => "cat" }
			 impl Named for Dog { name = pure (self) => "dog" }
			 let main = () -> void => {
			   println(Cat { n: 1 }.shout())
			   println(Dog { n: 2 }.shout())
			 }`,
			"cat!\ndog!",
		},
		{
			// An impl's own clause wins. Dispatch tries the impl's methods first and
			// falls back to the default only when they match nothing, so an override is
			// an override rather than an ambiguity.
			"an impl clause overrides the default",
			`trait Named {
			   pure name: (Self) -> string
			   pure shout: (Self) -> string = (self) => self.name() ++ "!"
			 }
			 struct Fox { n: i64 }
			 impl Named for Fox {
			   name = pure (self) => "fox"
			   shout = pure (self) => "FOX!!"
			 }
			 let main = () -> void => { println(Fox { n: 3 }.shout()) }`,
			"FOX!!",
		},
		{
			// A default calling another default, where the second is overridden: the
			// inner call must reach the *override*, not the body it was written beside.
			"a default reaches an override through another default",
			`trait Named {
			   pure name: (Self) -> string
			   pure shout: (Self) -> string = (self) => self.name() ++ "!"
			   pure twice: (Self) -> string = (self) => self.shout() ++ self.shout()
			 }
			 struct Cat { n: i64 }
			 struct Fox { n: i64 }
			 impl Named for Cat { name = pure (self) => "cat" }
			 impl Named for Fox {
			   name = pure (self) => "fox"
			   shout = pure (self) => "FOX!!"
			 }
			 let main = () -> void => {
			   println(Cat { n: 1 }.twice())
			   println(Fox { n: 3 }.twice())
			 }`,
			"cat!cat!\nFOX!!FOX!!",
		},
		{
			// The bound `Self` carries is closed over supertraits, so a default may call
			// a method the *supertrait* declares — refusing that would make `trait B: A`
			// mean less inside A's own defaults than at any call site.
			"a default calls a supertrait's method",
			`trait Base { pure base: (Self) -> i64 }
			 trait Loud: Base { pure boom: (Self) -> i64 = (self) => self.base() * 10 }
			 struct Bell { n: i64 }
			 impl Base for Bell { base = pure (self) => 7 }
			 impl Loud for Bell
			 let main = () -> void => { println(Bell { n: 0 }.boom()) }`,
			"70",
		},
		{
			// A default reached through a `where` bound rather than a concrete receiver:
			// two levels of the same substitution, and the inner one is published per
			// implementing type.
			"a default is reachable through a where bound",
			`trait Loud { pure base: (Self) -> i64
			              pure boom: (Self) -> i64 = (self) => self.base() * 10 }
			 struct Bell { n: i64 }
			 impl Loud for Bell { base = pure (self) => 7 }
			 let describe<t> where t: Loud = pure (v: t) -> i64 => v.boom() + 1
			 let main = () -> void => { println(describe(Bell { n: 0 })) }`,
			"71",
		},
		{
			// A **generic** impl target, at two instantiations. The default's inner bound
			// call is published per concrete instantiation, not at the impl's declared
			// `Box<t>` — and the lookup has to ask in the typechecker's spelling of the
			// type, which is what candidateKey exists for.
			"a default on a generic impl, at two instantiations",
			`struct Box<t> { v: t }
			 trait Sized2 { pure size: (Self) -> i64
			                pure doubled: (Self) -> i64 = (self) => self.size() * 2 }
			 impl Sized2 for Box<t> { size = pure (self) => 3 }
			 let main = () -> void => {
			   let b: Box<i64> = Box { v: 5 }
			   let c: Box<string> = Box { v: "x" }
			   println(b.doubled() + c.doubled())
			 }`,
			"12",
		},
		{
			// A default that calls itself. The candidate-publication guard is what keeps
			// the typechecker from spinning here; the emitted function is ordinarily
			// recursive.
			"a recursive default",
			`trait Countdown {
			   pure base: (Self) -> i64
			   pure go: (Self, i64) -> i64 = (self, n) =>
			     if n <= 0 { self.base() } else { self.go(n - 1) + 1 }
			 }
			 struct Step { n: i64 }
			 impl Countdown for Step { base = pure (self) => 100 }
			 let main = () -> void => { println(Step { n: 0 }.go(3)) }`,
			"103",
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
