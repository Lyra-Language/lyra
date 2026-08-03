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

// --- the same rule for tuples and arrays -------------------------------------
//
// A literal narrowed by a *composite* context is range-checked too. It was not until
// 08/03: `checkIntegerLiteralRange` runs at the scalar assignment sites and reads the
// *declared* type, which for `let t: (u8, u8) = (300, 1)` is a tuple rather than an
// integer — so the check returned immediately and the 300 reached a u8 slot. The check
// now sits where the narrowing happens, so it covers every context that narrows, not only
// an annotated `let`.

func TestCompositeNarrowing_TupleElementOutOfRange(t *testing.T) {
	res := parseCollectAndCheck(t, `let t: (u8, u8) = (300, 1)`, false)
	assertErrorsAre(t, res, "element 1: literal value 300 overflows u8")
}

func TestCompositeNarrowing_ArrayElementOutOfRange(t *testing.T) {
	res := parseCollectAndCheck(t, `let a: [2]u8 = [300, 1]`, false)
	assertErrorsAre(t, res, "element 1: literal value 300 overflows u8")
}

// The boundary fits — the control, since a check that rejected every narrowed literal
// would satisfy the two above.
func TestCompositeNarrowing_BoundaryValuesFit(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let t: (u8, u8) = (255, 0)`, false))
	assertNoErrors(t, parseCollectAndCheck(t, `let a: [2]u8 = [0, 255]`, false))
	// The signed boundaries, which also mix a negated literal with a plain one — see
	// TestUntypedIntegerJoin_* for why that pairing is its own question.
	assertNoErrors(t, parseCollectAndCheck(t, `let a: [2]i8 = [-128, 127]`, false))
}

// Every narrowing context, not just the annotated `let` the bug was found through.
func TestCompositeNarrowing_ReturnAndArgumentPositions(t *testing.T) {
	assertErrorsAre(t, parseCollectAndCheck(t, `
let f = () -> (u8, u8) => (300, 1)`, false), "element 1: literal value 300 overflows u8")

	res := parseCollectAndCheck(t, `
let f = (t: (u8, u8)) -> u8 => 0
let x = f((300, 1))`, false)
	assertErrorsAre(t, res, "element 1: literal value 300 overflows u8")
}

// Nesting narrows one level at a time, so the innermost leaf is still reached.
func TestCompositeNarrowing_Nested(t *testing.T) {
	assertErrorsAre(t, parseCollectAndCheck(t, `let t: ((u8, u8), u8) = ((300, 1), 2)`, false),
		"element 1: literal value 300 overflows u8")
	assertErrorsAre(t, parseCollectAndCheck(t, `let a: [2][2]u8 = [[300, 1], [2, 3]]`, false),
		"element 1: literal value 300 overflows u8")
}

// One mistake, one diagnostic. A leaf can be narrowed by more than one context on the way
// down — here the struct field's tuple is narrowed by the field's declared type and again
// by the enclosing annotation — and the guard in checkIntegerLiteralRange is what keeps
// that from reading as two separate errors.
func TestCompositeNarrowing_ReportedOnce(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pair { xs: (u8, u8) }
let p: Pair = Pair { xs: (300, 1) }`, false)
	if len(res.errors) != 1 {
		t.Errorf("expected exactly one overflow error, got %d: %v", len(res.errors), res.errors)
	}
}

// --- mixing a negated and a plain untyped literal -----------------------------
//
// A negated literal is `untyped_signed_int` and a plain one `untyped_int`, and neither
// was assignable to the other — so every construct that needs a common type across
// branches rejected ordinary code, each with a message comparing a type to itself
// ("then is integer literal, else is integer literal"; the match form managed "i64 vs
// i64"). branchCommonType now joins them at the signed kind, which is the only one a set
// containing a negative value can settle to.

func TestUntypedIntegerJoin_ArrayLiteral(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let a: [2]i8 = [-1, 2]`, false))
	assertNoErrors(t, parseCollectAndCheck(t, `let a = [-1, 2]`, false)) // no annotation either
}

func TestUntypedIntegerJoin_IfElseBranches(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let f = (c: bool) -> i8 => if c { -1 } else { 2 }`, false))
}

func TestUntypedIntegerJoin_MatchArms(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let f = (n: i64) -> i8 => match n { 0 => -1, _ => 2 }`, false))
}

// The join produces an *untyped* result, so an annotation still narrows it — and the
// range check still applies to what it narrowed to.
func TestUntypedIntegerJoin_StillNarrowsAndRangeChecks(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let a: [2]i8 = [-128, 127]`, false))
	assertErrorsAre(t, parseCollectAndCheck(t, `let a: [2]i8 = [-1, 200]`, false),
		"element 2: literal value 200 overflows i8")
}

// It joins untyped *integers*, and nothing else: a genuine disagreement is still an error.
func TestUntypedIntegerJoin_DoesNotSwallowRealMismatches(t *testing.T) {
	res := parseCollectAndCheck(t, `let a = [1, "s"]`, false)
	if len(res.errors) == 0 {
		t.Fatal("an int and a string have no common type")
	}
}
