package typechecker_test

import "testing"

// The array-repeat literal, `[v; n]` — `[n]T` where T is v's type (08/08).
//
// The count is a compile-time constant **by construction**, not by a check here: the
// grammar admits only a number literal or a `const_identifier` in that position, so
// `[0; n]` for a runtime `n` is a syntax error. That is the right place for it — the
// size is part of the type, and a type cannot depend on a value the compiler has not got.

func TestArrayRepeat_InfersFixedSizeArray(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g = [0; 5]
let n: i64 = g[0]
`, false)
	assertNoErrors(t, res)
}

// The element is left untyped where it is an untyped literal, exactly as an array
// literal's elements are, so an annotation narrows it instead of the i64 default
// reaching the backend and mismatching.
func TestArrayRepeat_AnnotationNarrowsTheElement(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let a: [4]u8 = [7; 4]
`, false))
}

func TestArrayRepeat_ElementOutOfRangeForAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `
let a: [4]u8 = [300; 4]
`, false)
	assertHasErrorContaining(t, res, "300")
}

func TestArrayRepeat_SizeMustMatchTheAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `
let a: [4]i64 = [0; 5]
`, false)
	assertHasErrorContaining(t, res, "cannot assign")
}

// A `const` count, including one defined in terms of another.
func TestArrayRepeat_ConstCount(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
const N = 3
const M = N * 2
let a: [3]i64 = [1; N]
let b: [6]i64 = [2; M]
`, false))
}

func TestArrayRepeat_ConstCountThatIsNotAnInteger(t *testing.T) {
	res := parseCollectAndCheck(t, `
const K = "no"
let a = [0; K]
`, false)
	assertHasErrorContaining(t, res, "array repeat count K must be a `const` whose value is a compile-time integer")
}

// A size nothing could compile is refused with a number rather than left to the machine:
// the backend emits one element per count.
func TestArrayRepeat_CountTooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `
let a = [0; 99999999]
`, false)
	assertHasErrorContaining(t, res, "is too large (the limit is 1048576 elements)")
}

// A dynamic annotation builds a `[]T`, exactly as `[1, 2, 3]` does under the same one —
// and stays dynamic rather than being rewritten to static, for the reason the array
// literal's arm gives.
func TestArrayRepeat_DynamicContext(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let d: []i64 = [0; 3]
`, false))
}
