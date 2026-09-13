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

// An untyped literal takes its place's width, as `a = 3.0` does. The temporaries the
// desugaring binds used to settle to the literals' defaults first, so an f32 place was
// handed an f64 (09/13).
func TestTupleAssignment_LiteralsTakeThePlacesWidths(t *testing.T) {
	source := `
struct Pt { x: u8, y: u8 }
let f = () -> void => {
    var a: f32 = 1.0
    var b: f32 = 2.0
    (a, b) = (4.0, 5.0)
    var p = Pt { x: 0, y: 0 }
    (p.x, p.y) = (200, 1)
    var n: u8 = 0
    unsafe {
      let pn = &mut n
      (pn^, a) = (255, 0.5)
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// A literal that does not fit its place is still refused, at the place — the context
// narrows the width, it does not wave the value through.
func TestTupleAssignment_LiteralOutOfItsPlacesRange(t *testing.T) {
	source := `
let f = () -> void => {
    var a: u8 = 0
    var b: f32 = 0.0
    (a, b) = (300, "s")
}`
	res := parseCollectAndCheck(t, source, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected errors for 300 into a u8 and a string into an f32")
	}
	assertHasErrorContaining(t, res, "string to f32")
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
