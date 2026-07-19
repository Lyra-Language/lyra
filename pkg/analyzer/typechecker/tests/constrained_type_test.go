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
