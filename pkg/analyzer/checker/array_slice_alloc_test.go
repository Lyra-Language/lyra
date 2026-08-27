package checker_test

import "testing"

// `noalloc` and `xs.slice(a, b)`. The copy allocates a `[]T` box and its element buffer,
// and there is no borrowing form to offer instead: sharing the parent's buffer would need
// that buffer ref-counted apart from the box that owns it, and a `push` on the parent
// reallocates it — so the slice would dangle while the array it came from is alive.
//
// The same answer `slice` on a string gives for its own version of the problem, so the
// two are refused by the same rule rather than by two.
func TestArraySliceAlloc_RefusedByNoalloc(t *testing.T) {
	src := `
let head = noalloc (xs: []i64) -> i64 => xs.slice(0, 2)[0]`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// **A `[N]T` receiver allocates too**, which is the case a reader is most likely to
// expect the other way: the receiver is stack storage, but the *result* is a `[]T`,
// because `end - start` is a run-time value and no fixed size could be written down.
func TestArraySliceAlloc_AFixedArrayReceiverStillAllocates(t *testing.T) {
	src := `
let head = noalloc () -> i64 => {
    let a = [1, 2, 3]
    a.slice(0, 2)[0]
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// Indexing one is not slicing one: `noalloc` still admits every read that copies nothing.
func TestArraySliceAlloc_IndexingIsStillAllowed(t *testing.T) {
	src := `
let head = noalloc (xs: []i64) -> i64 => xs[0]`
	assertPurityCount(t, checkPurity(t, src), 0)
}
