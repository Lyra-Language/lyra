package collector_test

import "testing"

func TestCollectMathAssignOpExpr(t *testing.T) {
	source := `
	var x = 5
	x += 3
	x -= 1
	x *= 3
	x /= 3
	x %= 3
	x %%= 3
	`
	runGoldenTest(t, source, "math_assign_op_expr")
}
