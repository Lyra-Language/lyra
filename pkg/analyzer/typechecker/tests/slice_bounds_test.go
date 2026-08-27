package typechecker_test

import "testing"

// **A provable negative slice bound is a compile error**, which is the rule an index already
// followed and the one position that had been left out. `xs[-1]` has been refused since
// 08/12 — an index does not count from the end, and in a language whose thesis is
// trap-over-silently-wrong the most common off-by-one deserves the error rather than a valid
// read of the wrong element. A slice bound is the same value in the same kind of position,
// and it was reaching the run-time trap instead.

func TestSliceBounds_ANegativeStartIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => println("hello".slice(-1, 2))
`, false)
	assertHasErrorContaining(t, res, "slice start bound -1 is negative")
}

func TestSliceBounds_ANegativeEndIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  println(xs.slice(0, -1).len())
}
`, false)
	assertHasErrorContaining(t, res, "slice end bound -1 is negative")
}

// **The message cannot name `from_end`**, which is what the index rule offers: a slice takes
// two *positions* rather than one element, so the end-relative accessor answers a different
// question. It names computing the position instead.
func TestSliceBounds_TheMessageNamesComputingThePosition(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => println("hello".slice(0, -1))
`, false)
	assertHasErrorContaining(t, res, "a bound is a position, not an offset from the end")
	assertHasErrorContaining(t, res, "`slice(0, len() - 1)`")
}

// A **newtype is transparent to its base's methods**, so it is transparent to their argument
// rules too: `newtype Name = string` slices exactly as a string does, and a negative bound is
// as wrong through the wrapper as without it.
func TestSliceBounds_RefusedThroughANewtype(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Name = string
let main = () -> void => {
  let n: Name = "hello"
  println(n.slice(-1, 2))
}
`, false)
	assertHasErrorContaining(t, res, "slice start bound -1 is negative")
}

// The reach is exactly the index rule's, because both go through `resolveConstantInt`: a
// literal, a negation of one, and a `const` bound to either. Constant *arithmetic*
// (`const LO = 0 - 2`) folds in neither, which is a property of that helper rather than of
// this check — the two cannot drift apart.
func TestSliceBounds_AConstBoundToANegativeLiteralIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
const LO = -2
let main = () -> void => println("hello".slice(LO, 3))
`, false)
	assertHasErrorContaining(t, res, "slice start bound -2 is negative")
}

// A bound the compiler cannot settle is left to the run-time trap, unchanged — the same
// ladder every bounds rule in the language rides.
func TestSliceBounds_ARuntimeBoundIsNotRefused(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let head = (s: string, n: i64) -> string => s.slice(0, n)
let main = () -> void => println(head("hello", 3))
`, false))
}

// And a valid slice is untouched, including the end position `slice(0, len())`.
func TestSliceBounds_ValidBoundsAreAccepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  println(xs.slice(0, xs.len()).len() + "hello".slice(1, 1).len())
}
`, false))
}
