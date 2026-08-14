package typechecker_test

import (
	"strings"
	"testing"
)

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
	// The message changed on 08/14 and is more accurate for it: `K` *is* a `const`, so
	// "must be a `const`" mis-stated the problem. A count that does not fold is now read
	// as a runtime one, and the runtime path's check is what rejects it — by its type,
	// which is the actual fault.
	assertHasErrorContaining(t, res, "array repeat count must be an integer, got string")
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

// ── A runtime count builds a dynamic array (08/14) ──────────────────────────
//
// The count was a compile-time constant *by grammar*, which is right for a fixed array —
// its length is part of its type, and a type cannot depend on a value the compiler has not
// got — and was inherited rather than reasoned for a dynamic one, whose length rides the
// value at run time. So a buffer sized by a window resize was a *syntax* error, and `push`
// in a loop was the only way to build one.

// A runtime count infers `[]T`, which is the only type it can have: no fixed type
// describes it. The fall-through is not a preference between two readings.
func TestArrayRepeat_RuntimeCountInfersDynamic(t *testing.T) {
	res := parseCollectAndCheck(t, `
let make = (w: i64, h: i64) -> []u32 => [0; w * h]
let main = () => println(make(4, 5).len())
`, false)
	assertNoErrors(t, res)
}

// The element still narrows to the annotation. Inference leaves it untyped so a context
// can fix its width, and the dynamic-to-dynamic pair is a *second* shape the shape check
// has to admit — the widening helper requires a static source, so without an arm for this
// the element never narrowed and `() -> []u32 => [0; n]` reported
// "expected DynamicArray<u32>, got DynamicArray<integer literal>".
func TestArrayRepeat_RuntimeCountNarrowsItsElement(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () => {
  let n = 3
  let buf: []u8 = [0; n]
  println(buf.len())
}
`, false)
	assertNoErrors(t, res)
}

// A fixed array still refuses it, and the diagnostic names the cause and the fix. Left to
// assignability this reads "cannot assign DynamicArray<integer literal> to
// StaticArray<u32, 3>", which names two types and not the count.
func TestArrayRepeat_RuntimeCountInFixedPositionIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () => {
  let n = 3
  let a: [3]u32 = [0; n]
  println(a[0])
}
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a runtime count must be refused in fixed-array position")
	}
	msg := res.errors[0].Error()
	for _, want := range []string{"part of its type", "compile-time constant", "`[]T`"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the diagnostic should mention %q; got: %s", want, msg)
		}
	}
}

// A count that is not an integer at all is caught on the runtime path, which is the only
// thing that checks it: the constant path proves it by folding.
func TestArrayRepeat_RuntimeCountMustBeAnInteger(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () => {
  let buf: []u32 = [0; "three"]
  println(buf.len())
}
`, false)
	assertHasErrorContaining(t, res, "array repeat count must be an integer, got string")
}
