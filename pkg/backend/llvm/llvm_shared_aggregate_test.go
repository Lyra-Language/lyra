package llvm

import "testing"

// A `shared` **named tuple** is a box, exactly as a `shared` struct is, and its positional
// access reads through that box. Both halves were missing: construction lowered the
// *recorded* type — which carries the flavor, so it lowers to a box pointer — and asked for
// a struct, failing outright as `llvm: tuple type Pair did not lower to a struct`, and
// `p.0` had no unboxing arm. `match` had always unboxed one, so the type was half-usable:
// it could be destructured but neither built nor indexed.
//
// The struct path is the model for both, which is hazard 8's shape — the trio
// (struct, anonymous struct, named tuple) travels together, and the named tuple was the one
// member with neither half.
func TestExec_SharedNamedTuple(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The annotated-binding path: the flavor comes from the annotation.
			"annotated binding, positional read",
			`tuple Pair(u8, u8)
let main = () -> u8 => {
  let p: shared Pair = Pair(3, 4)
  p.0 + p.1
}`,
			7,
		},
		{
			// The argument path: the flavor comes from the parameter, pushed down onto a
			// bare construction at the call site by propagateAllocation.
			"argument position, both spellings agree",
			`tuple Pair(u8, u8)
let sum = pure (p: shared Pair) -> u8 => p.0 + p.1
let main = () -> u8 => {
  let bound: shared Pair = Pair(1, 2)
  sum(bound) + sum(Pair(10, 20))
}`,
			33,
		},
		{
			// A managed element: the box's drop glue has to walk the payload as a tuple.
			"managed element",
			`tuple Tagged(string, u8)
let main = () -> u8 => {
  let t: shared Tagged = Tagged("ab" ++ "cd", 5)
  u8(t.0.len()) + t.1
}`,
			9,
		},
		{
			// Destructuring a shared tuple already worked (match_aggregate.go unboxes one);
			// pinned here so the three ways in stay together.
			"match destructures the same box",
			`tuple Pair(u8, u8)
let main = () -> u8 => {
  let p: shared Pair = Pair(6, 7)
  match p { (a, b) => a + b, _ => 0 }
}`,
			13,
		},
		{
			// One box, two bindings — a copy would still answer 6, so what this pins is that
			// the alias lowers and drops at all rather than that it aliases.
			"aliased binding",
			`tuple Pair(u8, string)
let main = () -> u8 => {
  let p: shared Pair = Pair(6, "x" ++ "y")
  let q = p
  p.0 + u8(q.1.len())
}`,
			8,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// The refcounting half: a shared tuple with a managed element, built and dropped in a loop
// and aliased across a call, is where a missing retain or an extra release shows up.
func TestExec_SharedNamedTupleUnderASan(t *testing.T) {
	t.Parallel()
	src := `tuple Tagged(string, u8)
let first = pure (t: shared Tagged) -> string => t.0
let main = () -> u8 => {
  var got = 0
  for i in 0..<50 {
    let t: shared Tagged = Tagged("alpha" ++ "!", 2)
    let alias = t
    got = got + first(t).len() + first(alias).len() + i64(alias.1)
  }
  if got == 700 { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// An array's **elements** carry a flavor independent of the array's own, and `[]shared T`
// is the spelling where the array has none — so the flavor reached the element's
// construction leaf through nothing, and an unboxed payload was stored into a box-pointer
// slot. Both array forms, because ArrayRepeatExpr is a variant of ArrayLiteralExpr that
// has now been left out of a walk eight times, and was left out of the flavor's own leaf
// case too (`shared [3]i64 = [7; 3]` did not build).
func TestExec_SharedElementAllocation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"[]shared named tuple",
			`tuple Pair(u8, u8)
let main = () -> u8 => {
  let xs: []shared Pair = [Pair(1, 2), Pair(3, 4)]
  xs[0].1 + xs[1].0
}`,
			5,
		},
		{
			"[N]shared struct",
			`struct Pt { x: u8, y: u8 }
let main = () -> u8 => {
  let xs: [2]shared Pt = [Pt { x: 1, y: 2 }, Pt { x: 3, y: 4 }]
  xs[0].y + xs[1].x
}`,
			5,
		},
		{
			// The repeat form of the element flavor. One box in three slots — the
			// value is evaluated once — which is the documented semantics W019 warns about.
			"[]shared struct from a repeat",
			`struct Pt { x: u8, y: u8 }
let main = () -> u8 => {
  let xs: []shared Pt = [Pt { x: 5, y: 6 }; 3]
  xs[2].y
}`,
			6,
		},
		{
			// The array's *own* flavor from a repeat: the eighth omission itself.
			"shared array from a repeat",
			`let main = () -> u8 => {
  let xs: shared [3]u8 = [7; 3]
  xs[1]
}`,
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// Structural equality on a `shared` aggregate compares the **payloads**, not the boxes —
// which is what equality.go's own header always claimed and never did. The field walk ran
// over the box, so it compared two refcounts and then failed on the payload field; rule 5
// is why that surfaced as a refusal rather than as an answer.
//
// Two boxes with equal payloads are equal, matching the string case beside it: a `shared`
// value is not compared by identity.
func TestExec_SharedAggregateEquality(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Distinct boxes, equal contents, including a managed field.
			"equal payloads in different boxes",
			`struct Pt { x: u8, y: string }
let main = () -> u8 => {
  let a: shared Pt = Pt { x: 1, y: "z" ++ "z" }
  let b: shared Pt = Pt { x: 1, y: "zz" }
  if a == b { 1 } else { 0 }
}`,
			1,
		},
		{
			"differing payloads",
			`struct Pt { x: u8, y: string }
let main = () -> u8 => {
  let a: shared Pt = Pt { x: 1, y: "zz" }
  let c: shared Pt = Pt { x: 2, y: "zz" }
  if a == c { 1 } else { 0 }
}`,
			0,
		},
		{
			"shared named tuple",
			`tuple Pair(u8, string)
let main = () -> u8 => {
  let p: shared Pair = Pair(1, "q")
  let q: shared Pair = Pair(1, "q")
  if p == q { 1 } else { 0 }
}`,
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}
