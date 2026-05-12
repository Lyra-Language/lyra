package collector_test

import "testing"

func TestCollectSimpleIfBlockExpr(t *testing.T) {
	source := `
	if x == 3 {
		println("x is 3")
	}`
	runGoldenTest(t, source, "simple_if_block_expr")
}

func TestCollectIfBlockExprWithElse(t *testing.T) {
	source := `
	if x == 3 {
		println("x is 3")
	} else {
		println("x is not 3")
	}`
	runGoldenTest(t, source, "if_block_expr_with_else")
}

func TestCollectIfBlockExprWithElseIf(t *testing.T) {
	source := `
	if x == 3 {
		println("x is 3")
	} else if x == 4 {
		println("x is 4")
	} else {
		println("x is not 3 or 4")
	}`
	runGoldenTest(t, source, "if_block_expr_with_else_if")
}

func TestCollectNestedIfBlockExpr(t *testing.T) {
	source := `
	if x == 0 {
		if y == 0 {
			println("At Origin")
		} else {
			println("On Vertical Axis")
		}
	} else {
		if y == 0 {
			println("On Horizontal Axis")
		} else {
			println("At ${x},${y}")
		}
	}`
	runGoldenTest(t, source, "nested_if_block_expr")
}
