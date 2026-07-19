package typechecker_test

import "testing"

// A function with no return-type annotation (`() => { ... }`, an inferred
// return) must still have its body type-checked. Previously checkLambdaBody
// returned early whenever the return type was absent, so every body-level
// diagnostic — const reassignment, interior mutation, reassignment type
// mismatch, must-use — was silently skipped for inferred-return functions.

func TestInferredReturn_ConstReassignmentReported(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () => {
  const x = 5
  x = 10
  0
}`, false)
	assertErrorsAre(t, res, "x: 'const' binding is immutable and cannot be reassigned")
}

func TestInferredReturn_ReassignmentTypeMismatchReported(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () => {
  var x: i32 = 5
  x = "nope"
  0
}`, false)
	assertErrorsAre(t, res, `x: cannot assign string to i32`)
}

func TestInferredReturn_ValidBodyStillClean(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () => {
  var x = 5
  x = 10
  x
}`, false)
	assertNoErrors(t, res)
}

// An annotated function's body was already checked; this pins that the fix
// didn't change that path (no duplicate diagnostics).
func TestAnnotatedReturn_ConstReassignmentReportedOnce(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> u8 => {
  const x = 5
  x = 10
  0
}`, false)
	assertErrorsAre(t, res, "x: 'const' binding is immutable and cannot be reassigned")
}
