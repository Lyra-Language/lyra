package llvm

import (
	"strings"
	"testing"
)

// Explicit type arguments on a call — `empty::<i64>()`.
//
// The grammar has parsed `generic_arguments` on a `call_expr` all along and the collector
// recorded them onto the node; nothing downstream read them, so a turbofish on a free
// function was accepted and silently discarded and the call reported the same "cannot
// infer" as if it were absent. Silent acceptance is worse than a refusal: the author wrote
// the answer and was told it could not be worked out.
//
// It matters because solving reads **argument types only**. A type variable mentioned only
// in the return type has nothing to bind it, so a function shaped like a constructor —
// takes a capacity, returns the thing the variables live in — was uncallable in every
// position, an annotation on the binding included.
func TestExec_TurbofishOnAFreeFunction(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			// The minimal shape: nothing but the return type mentions `t`.
			name: "a type variable only the return type mentions",
			src: `
let empty<t> = pure () -> []t => []
let main = () -> void => { println(empty::<i64>().len()) }`,
			want: "0",
		},
		{
			// What the feature is for. `cap` is an i64 and says nothing about either
			// parameter, so the turbofish is the only thing that can.
			name: "a generic collection constructor",
			src: `
struct HashMap<k, v> { slots: []Maybe<(k, v)>, count: i64 }
let with_capacity<k,v> = pure (cap: i64) -> HashMap<k,v> =>
  HashMap { slots: [None; cap], count: 0 }
let main = () -> void => {
  let m = with_capacity::<string, i64>(16)
  println("${m.count} ${m.slots.len()}")
}`,
			want: "0 16",
		},
		{
			// An explicit binding overrides what solving would have chosen, rather than
			// merely filling a gap solving could not reach.
			name: "overriding an inferable variable",
			src: `
let wrap<t> = pure (x: t) -> Maybe<t> => Some(x)
let main = () -> void => {
  match wrap::<u8>(200) { Some(v) => println(v), None => println("-") }
}`,
			want: "200",
		},
		{
			// Declaration order, not order of first appearance in the signature. These
			// two disagree here, so binding by appearance would swap the arguments and
			// silently produce a `Pair<string, i64>`.
			name: "binds in declaration order, not appearance order",
			src: `
struct Pair<a, b> { first: a, second: b }
let make<a, b> = pure (y: b, x: a) -> Pair<a, b> => Pair { first: x, second: y }
let main = () -> void => {
  let p = make::<i64, string>("s", 7)
  println("${p.first} ${p.second}")
}`,
			want: "7 s",
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
