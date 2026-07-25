package collector_test

import "testing"

func TestCollect_Negation(t *testing.T) {
	source := `let x = -42`
	runGoldenTest(t, source, "expr_negation")
}

func TestCollect_NegationOfExpr(t *testing.T) {
	source := `let x = -foo()`
	runGoldenTest(t, source, "expr_negation_of_expr")
}

// A narrow signed type's minimum written as a negated literal (i8 -128) collects
// as an ordinary NegationExpr over a *plain* IntegerLiteralExpr (Value 128, not
// Unsigned) — its magnitude fits int64, unlike the i64-min case (2^63), which
// overflows int64 as a positive literal and is marked Unsigned. The typechecker
// relies on this non-Unsigned representation to narrow the operand leaf to i8.
func TestCollect_NegationNarrowMin(t *testing.T) {
	source := `let x: i8 = -128`
	runGoldenTest(t, source, "expr_negation_narrow_min")
}
