package typechecker_test

import "testing"

// The expression spelling of a range step, held to the same rule as the newtype
// `step()` constraint (types.InvalidStepReason, lyra-E033). Type compatibility —
// which is all this site checked before — does not subsume it: `0..<10:0`
// type-checks perfectly and is a loop that cannot terminate.

func TestRangeStep_ZeroIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let f = () -> void => {
    for i in 0..<10:0 { print("${i}") }
  }
	`, false)
	assertErrorsAre(t, res, "invalid range step: a step of 0 never advances")
}

func TestRangeStep_WholeStepOverIntegersIsAccepted(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let f = () -> void => {
    for i in 0..<10:2 { print("${i}") }
  }
	`, false)
	assertNoErrors(t, res)
}

func TestRangeStep_FractionalOverFloatRangeIsAccepted(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let f = () -> void => {
    for x in 0.0..<10.0:0.25 { print("${x}") }
  }
	`, false)
	assertNoErrors(t, res)
}

// A variable step is legal and not decidable at this point; it must not be
// flagged just because it cannot be folded.
func TestRangeStep_VariableStepIsNotFlagged(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let f = (s: i64) -> void => {
    for i in 0..<10:s { print("${i}") }
  }
	`, false)
	assertNoErrors(t, res)
}
