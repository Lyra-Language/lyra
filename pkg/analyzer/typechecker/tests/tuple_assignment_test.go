package typechecker_test

import "testing"

// `(a, b) = (b, a)` is checked by what it desugars to: the destructuring `let`
// checks arity and element types, and each assignment checks its own place.

func TestTupleAssignment_SwapAndCallSource(t *testing.T) {
	source := `
let divmod = pure (n: i64, d: i64) -> (i64, i64) => (n / d, n %% d)
let f = () -> i64 => {
    var a = 1
    var b = 2
    (a, b) = (b, a)
    var xs: []i64 = [1, 2]
    (xs[0], xs[1]) = (xs[1], xs[0])
    (a, b) = divmod(7, 2)
    a + b + xs[0]
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

func TestTupleAssignment_ElementTypeMismatch(t *testing.T) {
	source := `
let f = () -> void => {
    var a = 1
    var b = 2
    (a, b) = (b, "s")
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "b: cannot assign string to i64")
}

func TestTupleAssignment_ImmutablePlace(t *testing.T) {
	source := `
let f = () -> void => {
    let c = 3
    var a = 1
    (c, a) = (a, c)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "c: 'let' binding is immutable; use 'var' to allow reassignment")
}

// An arity mismatch is reported once. It used to cascade into "undefined identifier"
// at every later use of a name the mismatch left unbound — for a tuple assignment,
// the *synthesized* names — so the names are now bound with no type instead.
func TestTupleAssignment_ArityMismatchReportsOnce(t *testing.T) {
	source := `
let f = () -> void => {
    var a = 1
    var b = 2
    (a, b) = (1, 2, 3)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "tuple pattern has 2 element(s) but tuple has 3")
}

func TestDestructuring_ArityMismatchReportsOnce(t *testing.T) {
	source := `
let f = () -> i64 => {
    let (a, b) = (1, 2, 3)
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "tuple pattern has 2 element(s) but tuple has 3")
}
