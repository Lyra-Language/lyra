package typechecker_test

import "testing"

// `step(...)` is enforced (lyra-E053, 08/13). Its meaning was already fixed for both
// spellings by types/step.go — **the values covered are start, start+step,
// start+2\*step, …** — and nothing read StepConstraint at all: the constraint was
// collected, validated for well-formedness (a zero step and a fractional step over an
// integer domain are both refused at the declaration) and then enforced against no
// value, so `newtype CompassHeading = i64 where range(0..<360), step(15)` accepted 7.
// That file's own comment recorded the gap as a known asymmetry.

func TestStep_ConstantOffGrid_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Heading = i64 where range(0..<360), step(15)
let a: Heading = 7`, false)
	assertHasErrorContaining(t, res, "value 7 is not a multiple of the step 15 of Heading")
}

func TestStep_ConstantOnGrid_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `newtype Heading = i64 where range(0..<360), step(15)
let a: Heading = 45
let b: Heading = 0
let c: Heading = 345`, false))
}

// The grid is measured from the **range's start**, not from zero — which is what
// "start, start+step, …" says. With `range(5..<=95), step(10)` the legal values are
// 5, 15, 25 …, so 10 is off the grid although it is a plain multiple of the step.
func TestStep_GridIsOffsetByRangeStart(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `newtype Odd = i64 where range(5..<=95), step(10)
let a: Odd = 15`, false))

	res := parseCollectAndCheck(t, `newtype Odd = i64 where range(5..<=95), step(10)
let b: Odd = 10`, false)
	assertHasErrorContaining(t, res, "is not a multiple of the step 10 from 5 of Odd")
}

// Without a range there is no named first value, so the grid is anchored at zero —
// the natural reading of "a multiple of the step".
func TestStep_WithoutRangeAnchorsAtZero(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `newtype Even = i64 where step(2)
let a: Even = 8`, false))

	res := parseCollectAndCheck(t, `newtype Even = i64 where step(2)
let b: Even = 7`, false)
	assertHasErrorContaining(t, res, "is not a multiple of the step 2 of Even")
}

// A float step uses exact arithmetic, which is what the constraint says. 0.25 is
// exactly representable, so the documented example behaves precisely.
func TestStep_FloatGrid(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `newtype Quarter = f64 where range(0.0..<=1.0), step(0.25)
let a: Quarter = 0.5`, false))

	res := parseCollectAndCheck(t, `newtype Quarter = f64 where range(0.0..<=1.0), step(0.25)
let b: Quarter = 0.3`, false)
	assertHasErrorContaining(t, res, "is not a multiple of the step 0.25")
}

// The constructor spelling reports the same way an annotation does — one value
// entering one newtype is one mistake however it is written.
func TestStep_ThroughConstructor(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Heading = i64 where range(0..<360), step(15)
let a = Heading(7)`, false)
	assertHasErrorContaining(t, res, "value 7 is not a multiple of the step 15 of Heading")
}
