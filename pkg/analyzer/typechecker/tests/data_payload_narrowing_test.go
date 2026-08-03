package typechecker_test

import (
	"testing"
)

// An annotation narrows a data constructor's untyped payload — `let m: Maybe<u8> = Some 7`.
//
// It did not until 08/03: solving promoted the untyped 7 to its i64 default to bind `t`,
// recorded the result as `Maybe<i64>`, and the annotation was then rejected wholesale
// ("cannot assign Maybe<i64> to Maybe<u8>") against an annotation sitting right there. The
// distinction that was missing is between a width the *program* determined and one the
// expression guessed by defaulting; only the second may be overridden by a context.
//
// The scalar, tuple and array forms of the same narrowing already worked, which is what
// made this read as a data-type quirk rather than as the general rule failing in one place.

const maybeAndBox = `
data Maybe<t> = None | Some t
data Boxed = Wrapped(u8)
`

func TestPayloadNarrowing_AnnotatedLet(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<u8> = Some 7`, false)
	assertNoErrors(t, res)
}

func TestPayloadNarrowing_ReturnPosition(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let f = () -> Maybe<u8> => Some 7`, false)
	assertNoErrors(t, res)
}

// The parenthesized spelling is the same node, and must not diverge.
func TestPayloadNarrowing_BothSpellings(t *testing.T) {
	juxtaposed := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<u8> = Some 7`, false)
	parenthesized := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<u8> = Some(7)`, false)
	if len(juxtaposed.errors) != len(parenthesized.errors) {
		t.Errorf("the two spellings must agree: `Some 7` gave %v, `Some(7)` gave %v",
			juxtaposed.errors, parenthesized.errors)
	}
}

// The context reaches through nesting, one level of construction at a time.
func TestPayloadNarrowing_Nested(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<Maybe<u8>> = Some(Some 7)`, false)
	assertNoErrors(t, res)
}

// Floats narrow by the same route: an untyped 1.5 defaults to f64 and must still take f32
// from the annotation.
func TestPayloadNarrowing_Float(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<f32> = Some 1.5`, false)
	assertNoErrors(t, res)
}

// With no context the guess stands, which is the behaviour the fix must not disturb: an
// unannotated construction is still its default instantiation.
func TestPayloadNarrowing_UnannotatedStillDefaults(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m = Some 7
let n: Maybe<i64> = m`, false)
	assertNoErrors(t, res)
}

// **A concrete declared field is not a guess.** `Wrapped(u8)` fixes its payload from the
// declaration, so the literal narrows to u8 there and then — no context required, and no
// deferral allowed. Deferring it was the over-general first version of this fix, and the
// symptom was remote: the leaf stayed untyped and the backend refused to store an i64 into
// a u8 slot ("aggregate element type mismatch").
// The recorded width itself is asserted by TestDataConstructor_ArgTakesDeclaredFieldWidth
// (data_ctor_width_test.go), which is the test that caught the over-general version; this
// one keeps the behaviour beside the rest of the narrowing rules.
func TestPayloadNarrowing_ConcreteFieldTakesDeclaredWidth(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let b = Wrapped(7)`, false)
	assertNoErrors(t, res)
}

// A payload that genuinely does not fit is still an error, and names the payload rather
// than the two instantiations.
func TestPayloadNarrowing_OutOfRangeIsStillRejected(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<u8> = Some 300`, false)
	assertErrorsAre(t, res, "Some: literal value 300 overflows u8")
}

// The boundary value fits — the control for the check above, which would pass equally well
// if the range check rejected everything.
func TestPayloadNarrowing_BoundaryValueFits(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<u8> = Some 255`, false)
	assertNoErrors(t, res)
}

// A wrong payload *type* is still rejected: narrowing widens what a context may decide,
// not what it may claim.
func TestPayloadNarrowing_TypeMismatchStillRejected(t *testing.T) {
	res := parseCollectAndCheck(t, maybeAndBox+`
let m: Maybe<string> = Some 7`, false)
	assertErrorsAre(t, res, "Some: cannot assign integer literal to string")
}
