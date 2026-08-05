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

// **`resolveTypeIfKnown` is `resolveType`'s twin and had drifted from it by exactly
// the same two composites** — the argument-list pair above. It resolves the *return*
// annotation (checkLambdaBody uses it so an unknown name is not reported twice), so
// the hole shows up only in return position, and only for a type whose name sits in
// an argument list: `-> Maybe<weak Node>` kept `Node` unresolved while the body's
// value resolved it, giving "return type mismatch: expected Maybe<weak Node>, got
// Maybe<weak Node>".
//
// This is the fifth instance of the hazard and the second in this one file, which is
// what the rule in lyra/CLAUDE.md means by "these travel in pairs": fixing a switch
// means checking its twin, not only its neighbours.
func TestResolveTypeIfKnown_NamedTypeInsideReturnAnnotation(t *testing.T) {
	for _, source := range []string{
		// The motivating case: an optional weak back-edge, which is the only way to
		// spell a constructible `weak` field (todo.md).
		`struct Node { n: i64, parent: Maybe<weak Node> }
data Maybe<t> = None | Some(t)
let orphan = () -> Maybe<weak Node> => {
  let tmp: shared Node = Node { n: 9, parent: None }
  Some(tmp.weak())
}`,
		// No `weak` involved — a named type in a return annotation's argument list is
		// enough on its own.
		`struct Pt { x: i64 }
data Box<t> = B(t)
let mk = () -> Box<Pt> => B(Pt { x: 1 })`,
		// The other half of the drift: a named type inside a function type in return
		// position.
		`struct Pt { x: i64 }
let mk = () -> (Pt) -> i64 => (p: Pt) -> i64 => p.x`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// **The twins are one walk as of 08/05.** `resolveType` and `resolveTypeIfKnown` had
// been two copies of the same ~120-line recursion differing only at the unknown-name
// leaf, which is how the drift above happened twice; they now share
// `resolveTypeWith`, parameterized by that leaf, so a composite added later reaches
// both or neither. `lyra/CLAUDE.md` hazard 8 named this pair as its outstanding
// instance of "the durable fix is to stop having more than one of it".
//
// The cases below put a named type inside *every* composite the walk handles, in
// return position — the half resolved by the quiet twin, and so the half that was
// missing cases. They are a guard on the fold itself: each of these resolved before,
// and must still.
func TestResolveType_EveryCompositeInReturnPosition(t *testing.T) {
	for _, source := range []string{
		// Static array.
		`struct Pt { x: i64 }
let mk = () -> [2]Pt => [Pt { x: 1 }, Pt { x: 2 }]`,
		// Dynamic array.
		`struct Pt { x: i64 }
let mk = () -> []Pt => [Pt { x: 1 }]`,
		// Tuple.
		`struct Pt { x: i64 }
let mk = () -> (Pt, i64) => (Pt { x: 1 }, 2)`,
		// Parameterized type, and a function type, each already covered above but
		// repeated here so this case list is the whole switch rather than its tail.
		`struct Pt { x: i64 }
data Box<t> = B(t)
let mk = () -> Box<Pt> => B(Pt { x: 1 })`,
		`struct Pt { x: i64 }
let mk = () -> (Pt) -> i64 => (p: Pt) -> i64 => p.x`,
		// Nested composites: the walk has to keep descending, not stop at the first.
		`struct Pt { x: i64 }
data Box<t> = B(t)
let mk = () -> [2]Box<Pt> => [B(Pt { x: 1 }), B(Pt { x: 2 })]`,
		`struct Pt { x: i64 }
data Box<t> = B(t)
let mk = () -> (Box<Pt>, []Pt) => (B(Pt { x: 1 }), [Pt { x: 2 }])`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}
