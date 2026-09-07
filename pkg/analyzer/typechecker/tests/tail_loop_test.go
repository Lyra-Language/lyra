package typechecker_test

import "testing"

// A non-void function whose body ends in a loop: refused when the loop can finish (the
// fall-through path has no value), accepted when it cannot (`for { … }` with no `break`
// is a `never`). Before 09/07 every shape passed, and the backend refused the ones that
// could finish with "block has no value".

func TestTailLoop_WhileFormIsRefused(t *testing.T) {
	source := `
let find = (n: i64) -> i64 => {
    var i = 0
    for i < n {
        i += 1
        if i >= n { return i }
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "find: the body ends in a loop that can finish, so the function reaches its end without a value; end with an expression or a `return` (a `for { … }` with no `break` never finishes and needs neither)")
}

func TestTailLoop_ForInIsRefused(t *testing.T) {
	source := `
let first = (xs: []i64) -> i64 => {
    for x in xs { return x }
}`
	res := parseCollectAndCheck(t, source, false)
	assertHasErrorContaining(t, res, "the body ends in a loop that can finish")
}

func TestTailLoop_InfiniteLoopIsNever(t *testing.T) {
	source := `
let find = (n: i64) -> i64 => {
    var i = 0
    for {
        i += 1
        if i >= n { return i }
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

func TestTailLoop_InfiniteLoopWithBreakIsRefused(t *testing.T) {
	source := `
let find = (n: i64) -> i64 => {
    var i = 0
    for {
        i += 1
        if i >= n { break }
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertHasErrorContaining(t, res, "the body ends in a loop that can finish")
}

// An unlabeled `break` in a nested loop leaves the nested loop only; a labeled one
// naming the outer loop leaves the outer.
func TestTailLoop_NestedBreakDoesNotCount_LabeledOneDoes(t *testing.T) {
	inner := `
let f = (n: i64) -> i64 => {
    var i = 0
    for {
        for i < n { i += 1; break }
        if i >= n { return i }
    }
}`
	res := parseCollectAndCheck(t, inner, false)
	assertNoErrors(t, res)

	outer := `
let f = (n: i64) -> i64 => {
    var i = 0
    outer: for {
        for i < n { i += 1; break outer }
        if i >= n { return i }
    }
}`
	res = parseCollectAndCheck(t, outer, false)
	assertHasErrorContaining(t, res, "the body ends in a loop that can finish")
}

func TestTailLoop_VoidFunctionIsUnaffected(t *testing.T) {
	source := `
let count = (n: i64) -> void => {
    var i = 0
    for i < n { i += 1 }
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}
