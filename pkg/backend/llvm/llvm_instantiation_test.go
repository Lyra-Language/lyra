package llvm

import (
	"strings"
	"testing"
)

// **A bare nullary constructor takes its instantiation from context**, in every position a
// context can arrive from.
//
// `None` solves none of `Maybe`'s parameters, so it is *always* recorded as the bare
// declaration and always needs the surrounding type — and the backend has no layout to
// lower `Maybe` at, so a site that pushed a width without the instantiation failed with
// `unknown named type "Maybe"` on a program the front end checked clean.
//
// The rule this pins is that **every site pushing a context into a construction must pair
// `propagateExpectedType` with `propagateInstantiation`**. Three sites had only one when
// this was written — a comparison, a reassignment, and a tuple/anonymous-struct element —
// after the array arms had already been fixed for it on 08/27. That is four instances of
// one omission, which is why the positions are enumerated here rather than sampled.
func TestExec_BareConstructorTakesItsInstantiation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// The reported case: `m != None` as a loop condition.
			"compared against a binding",
			`let get = pure (n: i64) -> Maybe<i64> => if n > 0 { Some(n) } else { None }
			 let main = () -> void => {
			   var m = get(3)
			   var seen = 0
			   for m != None {
			     seen += 1
			     m = get(0)
			   }
			   println("${seen}")
			 }`,
			"1",
		},
		{
			// Either side, since the operand that needs the context may be on the left.
			"compared on the left",
			`let get = pure (n: i64) -> Maybe<i64> => if n > 0 { Some(n) } else { None }
			 let main = () -> void => println("${None == get(0)} ${get(1) == None}")`,
			"true false",
		},
		{
			// Reassignment: the binding's type is the context.
			"reassigned to a binding",
			`let get = pure (n: i64) -> Maybe<i64> => if n > 0 { Some(n) } else { None }
			 let main = () -> void => {
			   var m = get(3)
			   m = None
			   println("${m == None}")
			 }`,
			"true",
		},
		{
			// A tuple element, from the annotation.
			"a tuple element",
			`let main = () -> void => {
			   let t: (Maybe<i64>, i64) = (None, 7)
			   println("${t.1} ${t.0 == None}")
			 }`,
			"7 true",
		},
		{
			// An anonymous struct's field, matched by name rather than position.
			"an anonymous struct field",
			`let main = () -> void => {
			   let r: { m: Maybe<i64>, n: i64 } = { m: None, n: 4 }
			   println("${r.n} ${r.m == None}")
			 }`,
			"4 true",
		},
		{
			// The positions that already worked, kept so a change here cannot quietly
			// break them: an argument, a named struct's field, an array element, an
			// annotated binding.
			"argument, field, element and annotation",
			`struct Box { m: Maybe<i64> }
			 let accept = pure (v: Maybe<i64>) -> bool => v == None
			 let main = () -> void => {
			   let b = Box { m: None }
			   let xs: []Maybe<i64> = [None, Some(1)]
			   let r: Maybe<i64> = None
			   println("${accept(None)} ${b.m == None} ${xs[0] == None} ${r == None}")
			 }`,
			"true true true true",
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
