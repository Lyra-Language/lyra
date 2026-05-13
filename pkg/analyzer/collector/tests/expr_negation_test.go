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
