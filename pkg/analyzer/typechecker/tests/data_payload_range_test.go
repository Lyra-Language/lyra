package typechecker_test

import "testing"

// **A `data` constructor's declared payload range-checks its literal argument** (09/16).
//
// It did not, and the failure was silent: `Wrapped(300)` against a `u8` payload compiled,
// ran, and produced 44. Every other position in the language reported it — a binding, a
// struct field, a tuple element, an array element, a function argument — and so did the
// *generic* spelling of the same constructor, which is what made it hard to see. A solved
// payload is narrowed in stampDataConstruction, which range-checks what it narrows; a
// concrete declared payload is narrowed in inferTupleLiteralExpr, which did not.
//
// `LANGUAGE.md` states the rule as "a compile error in every position", so this was the
// documentation being right and the implementation being wrong.
func TestData_AConcretePayloadRangeChecksItsLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `data W = Wrapped(u8)
let w = Wrapped(300)`, false)
	assertErrorsAre(t, res, "Wrapped: literal value 300 overflows u8")
}

// An annotation does not rescue it, and did not hide it either: the payload type comes from
// the declaration, so there is no context that could have narrowed it differently.
func TestData_AnAnnotatedConstructionRangeChecksToo(t *testing.T) {
	res := parseCollectAndCheck(t, `data W = Wrapped(u8)
let w: W = Wrapped(300)`, false)
	assertErrorsAre(t, res, "Wrapped: literal value 300 overflows u8")
}

// Each payload is checked, not just the first.
func TestData_EveryPayloadIsRangeChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `data P = Pair(u8, u8)
let p = Pair(1, 300)`, false)
	assertErrorsAre(t, res, "Pair: literal value 300 overflows u8")
}

// Floats take the same rule, with their own message — a literal that would become infinity.
func TestData_AFloatPayloadIsRangeChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `data F = Fl(f32)
let f = Fl(1.0e40)`, false)
	assertHasErrorContaining(t, res, "Fl: literal value 1e+40 overflows f32")
}

// The generic spelling still reports, through its own path — the two must not drift, since
// the whole bug was one having the check and the other not.
func TestData_AGenericPayloadStillRangeChecks(t *testing.T) {
	res := parseCollectAndCheck(t, `data Box<t> = Boxed(t)
let b: Box<u8> = Boxed(300)`, false)
	assertErrorsAre(t, res, "Boxed: literal value 300 overflows u8")
}

// A payload that fits is untouched, at the declared width rather than the i64 default.
func TestData_APayloadThatFitsIsAccepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `data W = Wrapped(u8)
let w = Wrapped(200)`, false))
}
