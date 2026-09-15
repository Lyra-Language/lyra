package typechecker_test

import "testing"

// An array literal mixing a **solved** and an **unsolved** generic element takes the
// context's element type.
//
// `[Some(200), None]` under `[]Maybe<u8>` was refused as "cannot assign
// StaticArray<Maybe<i64>, 2> to DynamicArray<Maybe<u8>>": `Some(200)` solved `t` locally
// to the i64 default, `None` solved nothing, and the literal's own type was built from
// that join before the annotation could narrow anything. The element-context fix of the
// same day repaired the *elements* and left the literal's own type alone — this is one
// step earlier.
//
// Two shapes hid it and are pinned below as regression guards: `[]Maybe<i64>` works
// because the default happens to match, and `[None; n]` works because nothing solves
// anything.

func TestArrayLiteralContext_AnnotatedBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let main = () -> void => {
  let xs: []Maybe<u8> = [Some(200), None]
}
`, false)
	assertNoErrors(t, res)
}

// Order must not matter — the unsolved element coming first is the same literal.
func TestArrayLiteralContext_UnsolvedElementFirst(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let main = () -> void => {
  let xs: []Maybe<u8> = [None, Some(200)]
}
`, false)
	assertNoErrors(t, res)
}

// A fixed-array context narrows the same way; only the recorded shape differs.
func TestArrayLiteralContext_StaticArrayAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let main = () -> void => {
  let xs: [2]Maybe<u8> = #[Some(200), None]
}
`, false)
	assertNoErrors(t, res)
}

// The other positions an array context arrives from. Each already handed its element type
// to propagateInstantiation, so one arm there covers all of them — which is the reason
// this is a single fix and not six.
func TestArrayLiteralContext_EveryContextPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
struct Holder { xs: []Maybe<u8> }
tuple Pair([]Maybe<u8>, i64)
let takes = pure (xs: []Maybe<u8>) -> i64 => 1
let gives = pure () -> []Maybe<u8> => [Some(200), None]
let main = () -> void => {
  let h = Holder { xs: [Some(200), None] }
  let p = Pair([Some(200), None], 1)
  let n = takes([Some(200), None])
}
`, false)
	assertNoErrors(t, res)
}

// The two shapes that always worked, kept working. `[]Maybe<i64>` matches the literal's
// own default, and the repeat form solves nothing to disagree about — that repeat is what
// a hash table uses to size itself, so it is load-bearing rather than incidental.
func TestArrayLiteralContext_ShapesThatAlreadyWorked(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let main = () -> void => {
  let a: []Maybe<i64> = [Some(200), None]
  let b: []Maybe<u8> = [None; 2]
}
`, false)
	assertNoErrors(t, res)
}

// Narrowing must still **check**: a payload that does not fit the width it was narrowed to
// is reported. Without this the fix would silently store 300 in a u8 — the failure mode
// stampDataConstruction's own range check exists for.
func TestArrayLiteralContext_PayloadTooWideIsStillReported(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let main = () -> void => {
  let xs: []Maybe<u8> = [Some(300), None]
}
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a payload too wide for the narrowed element type should be reported")
	}
}

// A genuine element mismatch is still an error — the context completes what was unsolved,
// it does not overwrite what the elements decided.
func TestArrayLiteralContext_WrongElementTypeIsStillReported(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let main = () -> void => {
  let xs: []Maybe<u8> = [Some("s"), None]
}
`, false)
	if len(res.errors) == 0 {
		t.Fatal("an element whose payload disagrees with the context should be reported")
	}
}

// A branching value whose branches are array literals takes a dynamic-array context the way a
// single literal does — through an `if`, an `else if` chain, a match, a nested block, and when
// the branches differ in length or one is empty, which do not join on their own. It used to be
// refused in every one of those shapes: `(c) -> []string => if c { ["a"] } else { ["b"] }`
// reported `expected DynamicArray<string>, got StaticArray<string, 1>`.
func TestArrayLiteralContext_BranchesTakeTheContext(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"if, same length":      `let g = (c: bool) -> []string => if c { ["a"] } else { ["b"] }`,
		"if, different length": `let g = (c: bool) -> []string => if c { ["a"] } else { ["b", "c"] }`,
		"if, empty else":       `let g = (c: bool) -> []i64 => if c { [1, 2] } else { [] }`,
		"else if chain":        `let g = (n: i64) -> []i64 => if n == 0 { [] } else if n == 1 { [1] } else { [1, 2, 3] }`,
		"match":                `let g = (n: i64) -> []i64 => match n { 0 => [], 1 => [1], _ => { let k = n; [k, k] } }`,
		"match inside if":      `let g = (c: bool, n: i64) -> []i64 => if c { match n { 0 => [], _ => [n] } } else { [7, 8] }`,
		"annotated binding":    `let g = (c: bool) -> i64 => { let xs: []i64 = if c { [1] } else { [2, 3] }; xs.len() }`,
		"argument slot": `
let take = (xs: []i64) -> i64 => xs.len()
let g = (c: bool) -> i64 => take(if c { [1] } else { [2, 3] })`,
		"a dynamic binding in one branch": `let g = (c: bool, ys: []i64) -> []i64 => if c { ys } else { [1, 2, 3] }`,
		"newtype over an array": `
newtype Bag = []string
let g = (c: bool) -> Bag => if c { ["a"] } else { ["b", "c"] }`,
		"the result is dynamic": `let g = (c: bool) -> i64 => { var xs: []i64 = if c { [1] } else { [2, 3] }; xs.push(4); xs.len() }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// Only literals narrow, and never across flavors: a fixed-array binding in a branch does not
// join a dynamic literal, and fixed literals of different lengths do not join at all.
func TestArrayLiteralContext_BranchesKeepTheirRefusals(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ src, want string }{
		"fixed binding, same length": {
			`let g = (c: bool) -> []i64 => { let fixed = #[1, 2]; if c { fixed } else { [3, 4] } }`,
			"if/else branches have incompatible types: then is StaticArray<i64, 2>, else is DynamicArray<integer literal>"},
		"fixed binding, different length": {
			`let g = (c: bool) -> []i64 => { let fixed = #[1, 2]; if c { fixed } else { #[3] } }`,
			"if/else branches have incompatible types: then is StaticArray<i64, 2>, else is StaticArray<integer literal, 1>"},
		"fixed literals of different lengths": {
			`let g = (c: bool) -> i64 => { let k = if c { #[1] } else { #[2, 3] }; 0 }`,
			"if/else branches have incompatible types: then is StaticArray<integer literal, 1>, else is StaticArray<integer literal, 2>"},
		"a binding into a newtype": {`
newtype Bag = []string
let g = (c: bool, ys: []string) -> Bag => if c { ys } else { ["b"] }`,
			"cannot use DynamicArray<string> as Bag implicitly: Bag is a distinct type over DynamicArray<string>, so the conversion must be written — `Bag(...)`"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertHasErrorContaining(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}
