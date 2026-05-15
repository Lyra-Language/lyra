package collector_test

import "testing"

func TestCollect_NotBooleanExpr(t *testing.T) {
	source := `let x = !a`
	runGoldenTest(t, source, "expr_not_boolean")
}

func TestCollect_BooleanBinaryEq(t *testing.T) {
	source := `let x = a == b`
	runGoldenTest(t, source, "expr_boolean_binary_eq")
}

func TestCollect_BooleanBinaryNEq(t *testing.T) {
	source := `let x = a != b`
	runGoldenTest(t, source, "expr_boolean_binary_neq")
}

func TestCollect_BooleanBinaryLT(t *testing.T) {
	source := `let x = a < b`
	runGoldenTest(t, source, "expr_boolean_binary_lt")
}

func TestCollect_BooleanBinaryLTE(t *testing.T) {
	source := `let x = a <= b`
	runGoldenTest(t, source, "expr_boolean_binary_lte")
}

func TestCollect_BooleanBinaryGT(t *testing.T) {
	source := `let x = a > b`
	runGoldenTest(t, source, "expr_boolean_binary_gt")
}

func TestCollect_BooleanBinaryGTE(t *testing.T) {
	source := `let x = a >= b`
	runGoldenTest(t, source, "expr_boolean_binary_gte")
}

func TestCollect_BooleanBinaryAnd(t *testing.T) {
	source := `let x = a && b`
	runGoldenTest(t, source, "expr_boolean_binary_and")
}

func TestCollect_BooleanBinaryOr(t *testing.T) {
	source := `let x = a || b`
	runGoldenTest(t, source, "expr_boolean_binary_or")
}

func TestCollect_BooleanBinaryChained(t *testing.T) {
	source := `let x = a < b && c > d`
	runGoldenTest(t, source, "expr_boolean_binary_chained")
}
