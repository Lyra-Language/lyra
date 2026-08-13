package typechecker_test

import "testing"

// A `[N]T` **value** cannot take a `[]T` slot; a `[N]T` **literal** can (08/13).
//
// The two are not the same claim. `[N]T` is stack storage and `[]T` is a
// ref-counted box, so passing a built fixed array where a box is expected is a
// misinterpretation of memory — the callee indexes through a pointer that is really
// the array's first element, and the program **segfaults**. A literal has not been
// built yet and is made in whichever shape its context asks for.
//
// One type-level rule served both until 08/13, with a comment reading "a static
// array *literal* is assignable to a dynamic array" above code that tested only the
// type. The comment named the right rule; the code admitted every `[N]T` value, and
// `take(ys)` below compiled clean and crashed. The allowance now walks the
// expression alongside the type, so it applies exactly where a literal sits.

const dynArrayRefusal = "cannot assign StaticArray<i64, 3> to DynamicArray<i64>"

// ── refused: a built fixed array reaching a dynamic slot ────────────────────

func TestStaticToDynamic_BindingAsArgument_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let take = (xs: []i64) -> i64 => xs[0]
let ys: [3]i64 = [1, 2, 3]
let n = take(ys)
`, false)
	assertHasErrorContaining(t, res, dynArrayRefusal)
}

func TestStaticToDynamic_BindingInAnnotation_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let ys: [3]i64 = [1, 2, 3]
let xs: []i64 = ys
`, false)
	assertHasErrorContaining(t, res, dynArrayRefusal)
}

func TestStaticToDynamic_BindingInReturn_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let ret = () -> []i64 => {
  let ys: [3]i64 = [1, 2, 3]
  ys
}
`, false)
	assertHasErrorContaining(t, res, "expected DynamicArray<i64>, got StaticArray<i64, 3>")
}

// The nested variant, and the reason the allowance walks expressions rather than
// types: `[[1, 2], [3, 4]]` and `[y1, y2]` have the *same* type, `[2][2]i64`, so
// only the expressions tell the legal case from the crashing one.
func TestStaticToDynamic_BindingsNestedInsideALiteral_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let y1: [2]i64 = [1, 2]
let y2: [2]i64 = [3, 4]
let bad: [][]i64 = [y1, y2]
`, false)
	assertHasErrorContaining(t, res,
		"cannot assign StaticArray<StaticArray<i64, 2>, 2> to DynamicArray<DynamicArray<i64>>")
}

// ── allowed: a literal, wherever one legitimately sits ──────────────────────

func TestStaticToDynamic_LiteralForms_Allowed(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Row = []i64
let take = (xs: []i64) -> i64 => xs[0]
let mk = () -> []i64 => [4, 5, 6]

let plain: []i64 = [1, 2, 3]
let nested: [][]i64 = [[1, 2], [3, 4]]
let viaNewtype: Row = [9, 9]
let empty: []i64 = []
let repeated: []i64 = [7; 3]
let asArgument = take([1, 2, 3])
let asRepeatArgument = take([0; 4])
`, false))
}

// A tuple literal is not itself malleable, but it contains elements that are — the
// return type is what tells the inner `[p]` to be a box.
func TestStaticToDynamic_ArrayLiteralInsideATupleLiteral_Allowed(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let mk = () -> (i64, []i64) => (1, [2, 3])
`, false))
}

// The tuple arm must not become a second way past the *name* check: a named tuple
// is nominal, and this allowance is only about array shape inside one.
func TestStaticToDynamic_TupleArmDoesNotBypassNominalCheck(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i64, i64)
tuple Vector(i64, i64)
let p: Point = Point(1, 2)
let v: Vector = p
`, false)
	assertHasErrorContaining(t, res, "cannot assign")
}
