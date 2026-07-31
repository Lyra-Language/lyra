package typechecker_test

import "testing"

// A declared effect bound is part of a function *type*, so `pure () -> i64` and
// `() -> i64` are different types — otherwise isAssignable's equality short-circuit would
// fire and the annotation would be decorative. Assignability deliberately lets a value
// through in either direction anyway: whether it *satisfies* the bound is decided by the
// purity pass against the argument's inferred effect, which is the only pass that knows.
func TestDeclaredBound_ShapeIsCheckedButBoundsAreNot(t *testing.T) {
	res := parseCollectAndCheck(t, `
let strict = (f: pure () -> i64) -> i64 => f()
let loose = (f: () -> i64) -> i64 => f()
let mk = () -> i64 => 1
let a = () -> i64 => strict(mk)
let b = () -> i64 => loose(mk)`, false)
	assertNoErrors(t, res)
}

// The shape still has to match: a bound does not excuse a wrong signature.
func TestDeclaredBound_ShapeMismatchIsStillRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
let strict = (f: pure () -> i64) -> i64 => f()
let wrong = (n: i64) -> string => "x"
let a = () -> i64 => strict(wrong)`, false)
	if len(res.errors) == 0 {
		t.Fatal("a function of the wrong shape should not satisfy a bounded parameter")
	}
}
