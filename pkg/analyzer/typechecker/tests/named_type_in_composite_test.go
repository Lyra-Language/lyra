package typechecker_test

import "testing"

// `resolveType` expands a named type into its declaration, and **every composite that can
// hold a type needs a case there** — a miss leaves the annotation holding an
// `UnresolvedType` while the value's type resolved, so assignability compares two
// spellings of the same type and fails with a message naming it twice. The static-array,
// dynamic-array, tuple and weak cases all carry a comment saying exactly that. Two
// composites did not have one.

// A named type inside a **function type**: `(g: (Pair) -> i64)` rejected a real
// `(Pair) -> i64` with "cannot assign (Pair(i64, i64)) -> i64 to (Pair) -> i64". Only
// through a function type — a plain `p: Pair` parameter always worked — so naming a type
// failed exactly where a signature is long enough to want the name.
func TestResolveType_NamedTypeInsideFunctionType(t *testing.T) {
	for _, source := range []string{
		// Parameter position, passing a named function.
		`tuple Pair(i64, i64)
let apply = (g: (Pair) -> i64, p: Pair) -> i64 => g(p)
let add = (q: Pair) -> i64 => q.0 + q.1
let use = () -> i64 => apply(add, Pair(3, 4))`,
		// Return position.
		`struct Pt { x: i64, y: i64 }
let apply = (f: (i64) -> Pt, n: i64) -> i64 => f(n).x
let mk = (n: i64) -> Pt => Pt { x: n, y: 0 }
let use = () -> i64 => apply(mk, 3)`,
		// Nested two deep: a named type inside a tuple inside a function type.
		`struct Pt { x: i64 }
let apply = (g: ((Pt, i64)) -> i64) -> i64 => g((Pt { x: 5 }, 2))
let unpack = ((p, n): (Pt, i64)) -> i64 => p.x + n
let use = () -> i64 => apply(unpack)`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// A named type inside a **parameterized type**'s arguments: `Box<Pt>`. The variable is in
// the argument list, never at the leaf, so the UnresolvedType case never saw it —
// "cannot assign Box<Pt> to Box<Pt>". This one bit a plain parameter too, not only a
// function type. It is the same composite `mentionsTypeVar` forgot (see todo.md); the
// argument-list case is the one every such switch misses.
func TestResolveType_NamedTypeInsideParameterizedType(t *testing.T) {
	for _, source := range []string{
		`struct Pt { x: i64 }
data Box<t> = B(t)
let get = (b: Box<Pt>) -> i64 => match b { B(p) => p.x }
let use = () -> i64 => get(B(Pt { x: 7 }))`,
		// Both holes at once: a parameterized type carrying a named type, inside a
		// function type.
		`struct Pt { x: i64 }
data Box<t> = B(t)
let apply = (g: (Box<Pt>) -> i64, b: Box<Pt>) -> i64 => g(b)
let unbox = (b: Box<Pt>) -> i64 => match b { B(p) => p.x }
let use = () -> i64 => apply(unbox, B(Pt { x: 7 }))`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}
