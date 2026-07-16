package typechecker_test

import "testing"

// Positional tuple access (`pair.0`) resolves to the indexed element's type. It
// works for named tuples (`Point(i32, i32)`) and anonymous ones (`(1, true)`) —
// both carry element types — and reports an out-of-range index or a non-tuple
// object.

// A named tuple element takes its declared type: assigning `p.0` (i32) to an
// i32-annotated binding is fine.
func TestTupleIndex_NamedElementType(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p = Point(1, 2)
let q: i32 = p.0
`, false)
	assertNoErrors(t, res)
}

// The element type is genuinely tracked (not just "some int"): the second
// element of `(i64, bool)` is bool, so it satisfies a bool annotation while the
// first (i64) would not.
func TestTupleIndex_AnonymousElementType(t *testing.T) {
	res := parseCollectAndCheck(t, `
let p = (1, true)
let q: bool = p.1
`, false)
	assertNoErrors(t, res)
}

// Assigning a differently-typed element surfaces the element's real type in the
// error — proof the index resolves positionally to i32, not to the binding's
// annotation.
func TestTupleIndex_ElementTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p = Point(1, 2)
let q: bool = p.0
`, false)
	assertErrorsAre(t, res, "q: cannot assign i32 to boolean")
}

// An index past the tuple's arity is a compile error.
func TestTupleIndex_OutOfRange(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p = Point(1, 2)
let q = p.2
`, false)
	assertErrorsAre(t, res, "tuple index 2 out of range (tuple has 2 element(s))")
}

// Positional access on a non-tuple value is a compile error.
func TestTupleIndex_NonTuple(t *testing.T) {
	res := parseCollectAndCheck(t, `
let x = 5
let y = x.0
`, false)
	assertErrorsAre(t, res, "tuple index access on non-tuple type i64")
}
