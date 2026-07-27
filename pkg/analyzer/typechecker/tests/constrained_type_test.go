package typechecker_test

import "testing"

// checkConstrainedTypeDecl compiles every pattern() constraint on a
// newtype/constrained-type declaration so a malformed regex is reported at
// declaration time rather than first use.

func TestConstrainedType_ValidPattern(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype HexStr = string where pattern(r/^#[0-9a-fA-F]{6}$/)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_InvalidPatternReported(t *testing.T) {
	// `[` opens a character class that is never closed — an invalid regex.
	res := parseCollectAndCheck(t, `newtype Bad = string where pattern(r/[/)`, false)
	assertErrorsAre(t, res,
		"type Bad: invalid pattern constraint r/[/: regex parse error at offset 0: unterminated character class")
}

// The non-pattern constraint kinds (range/values/step/precision) carry no
// declaration-time validation today, so a well-formed constrained type
// type-checks cleanly. These pin that and exercise checkTypeDecl's dispatch
// into the constrained-type branch.

func TestConstrainedType_RangeNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Angle = f64 where range(0.0..<360.0)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_ValuesNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Status = i32 where values(200, 404, 500)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_StepConstraintNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Heading = i64 where range(0..<360), step(15)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_PrecisionConstraintNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0.0..<1.0), precision(0.01)`, false)
	assertNoErrors(t, res)
}

// ── range-constraint value enforcement (lyra-E023) ───────────────────────────
//
// A compile-time numeric constant assigned to a range-constrained newtype must
// fall within the declared range.

func TestRangeConstraint_IntAboveInclusiveEnd(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..=100)
let p: Percent = 150`, false)
	assertErrorsAre(t, res, "p: value 150 is outside the range 0..=100 of Percent")
}

func TestRangeConstraint_IntBelowStart(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Grade = i32 where range(1..=5)
let g: Grade = 0`, false)
	assertErrorsAre(t, res, "g: value 0 is outside the range 1..=5 of Grade")
}

func TestRangeConstraint_IntInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..=100)
let p: Percent = 50`, false)
	assertNoErrors(t, res)
}

// The exclusive end `..<`: the end value itself is out of range, the one below is in.
func TestRangeConstraint_ExclusiveEndBoundary(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Angle = i32 where range(0..<360)
let a: Angle = 360`, false)
	assertErrorsAre(t, res, "a: value 360 is outside the range 0..<360 of Angle")
}

func TestRangeConstraint_ExclusiveEndInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Angle = i32 where range(0..<360)
let a: Angle = 359`, false)
	assertNoErrors(t, res)
}

// Open-ended bounds: only a lower / only an upper bound.
func TestRangeConstraint_OpenLowerBound(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype NonNeg = i32 where range(0..)
let n: NonNeg = -5`, false)
	assertErrorsAre(t, res, "n: value -5 is outside the range 0.. of NonNeg")
}

func TestRangeConstraint_OpenUpperBound(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Small = i32 where range(..=100)
let s: Small = 150`, false)
	assertErrorsAre(t, res, "s: value 150 is outside the range ..=100 of Small")
}

// A negative start via a negated-literal bound.
func TestRangeConstraint_NegativeStart(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Temp = i32 where range(-40..=50)
let t2: Temp = -50`, false)
	assertErrorsAre(t, res, "t2: value -50 is outside the range -40..=50 of Temp")
}

func TestRangeConstraint_FloatAboveRange(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0..=1)
let r: Ratio = 1.5`, false)
	assertErrorsAre(t, res, "r: value 1.5 is outside the range 0..=1 of Ratio")
}

func TestRangeConstraint_FloatInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0..=1)
let r: Ratio = 0.5`, false)
	assertNoErrors(t, res)
}

// A non-constant value is not checked at compile time (a future flow-sensitive
// pass / the runtime owns it) — no false positive.
func TestRangeConstraint_NonConstant_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..=100)
let f = (x: u8) -> Percent => x`, false)
	assertNoErrors(t, res)
}

// Reassigning an out-of-range constant to a constrained var is also enforced.
func TestRangeConstraint_Reassignment(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..=100)
let f = () -> u8 => {
	var p: Percent = 50
	p = 200
	0
}`, false)
	assertErrorsAre(t, res, "p: value 200 is outside the range 0..=100 of Percent")
}
