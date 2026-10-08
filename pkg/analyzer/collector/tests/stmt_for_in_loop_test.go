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

// A written loop-variable type (09/29) is the loop's `KeyType`, not an iterable.
func TestCollectForInLoopWithTypedVariable(t *testing.T) {
	runGoldenTest(t, "\n\tfor i: u16 in 0..<100 {\n\t\tprintln(i)\n\t}", "for_in_loop_with_typed_variable")
}

func TestCollect_ForInLoopAsExpression(t *testing.T) {
	source := `
	let found = for item in items {
		if item > 10 {
			break item
		}
	}`
	runGoldenTest(t, source, "for_in_loop_as_expression")
}

func TestCollect_LabeledForInLoopBreakWithValue(t *testing.T) {
	source := `
	let result = outer: for i in 0..<=10 {
		for j in 0..<=10 {
			break outer i + j
		}
	}`
	runGoldenTest(t, source, "labeled_for_loop_break_with_value")
}
