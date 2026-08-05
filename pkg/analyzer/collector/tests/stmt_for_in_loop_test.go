package collector_test

import "testing"

func TestCollect_ForInLoopWithIdentifier(t *testing.T) {
	source := `
	for item in my_collection {
		println(item)
	}`
	runGoldenTest(t, source, "for_in_loop_with_identifier")
}

func TestCollect_ForInLoopWithRange(t *testing.T) {
	source := `
	for i in 1..<=3 {
		println("i == ${i}")
	}`
	runGoldenTest(t, source, "for_in_loop_with_range")
}

func TestCollect_ForInLoopWithTuple(t *testing.T) {
	source := `
	for i in (1, "2", 3) {
		println("i == ${i}")
	}`
	runGoldenTest(t, source, "for_in_loop_with_tuple")
}

func TestCollect_ForInLoopWithKeyAndValue(t *testing.T) {
	source := `
	for item, idx in [1, 2, 3] {
		println(item)
	}`
	runGoldenTest(t, source, "for_in_loop_with_key_and_value")
}

func TestCollect_ForInLoopWithPostfixExpr(t *testing.T) {
	source := `
	for item in get_array() {
		println(item)
	}`
	runGoldenTest(t, source, "for_in_loop_with_postfix_expr")
}

func TestCollect_LabeledForInLoop(t *testing.T) {
	source := `
	outer: for item in [1, 2, 3] {
		break outer
	}`
	runGoldenTest(t, source, "labeled_for_in_loop")
}
