package collector_test

import "testing"

func TestCollect_InfiniteForLoop(t *testing.T) {
	source := `
	for {
		println("All work and no play makes Homer something something...")
	}`
	runGoldenTest(t, source, "infinite_for_loop")
}

func TestCollect_ForLoopWithCondition(t *testing.T) {
	source := `
	var i = 0
	for i < 10 {
		println("i == ${i}")
		i += 1
	}`
	runGoldenTest(t, source, "for_loop_with_condition")
}

func TestCollect_ForLoopWithInitAndCondition(t *testing.T) {
	source := `
	for var i = 0; i < 10 {
		println("i == ${i}")
		i += 1
	}`
	runGoldenTest(t, source, "for_loop_with_init_and_condition")
}

func TestCollect_ForLoopWithConditionAndPost(t *testing.T) {
	source := `
	var i = 0
	for i < 10; i += 1 {
		println("i == ${i}")
	}`
	runGoldenTest(t, source, "for_loop_with_condition_and_post")
}

func TestCollect_ForLoopWithInitAndConditionAndPost(t *testing.T) {
	source := `
	for var i = 0; i < 10; i += 1 {
		println("i == ${i}")
	}`
	runGoldenTest(t, source, "for_loop_with_init_and_condition_and_post")
}

func TestCollect_ForLoopWithBreak(t *testing.T) {
	source := `
	for var i = 0; i < 10; i += 1 {
		break
	}`
	runGoldenTest(t, source, "for_loop_with_break")
}

func TestCollect_LabeledForLoopWithBreak(t *testing.T) {
	source := `
	outer: for var i = 0; i < 10; i += 1 {
		for var y = 0; y < 10; y += 1 {
			if y == 5 {
				break outer
			}
			println("y == ${y}")
		}
	}`
	runGoldenTest(t, source, "labeled_for_loop_with_break")
}

func TestCollect_ForLoopWithContinue(t *testing.T) {
	source := `
	for var i = 0; i < 10; i += 1 {
		if i % 2 == 0 {
			continue
		}
		println("i == ${i}")
	}`
	runGoldenTest(t, source, "for_loop_with_continue")
}

func TestCollect_LabeledForLoopWithContinue(t *testing.T) {
	source := `
	outer: for var i = 0; i < 10; i += 1 {
		for var y = 0; y < 10; y += 1 {
			if y % 2 == 0 {
				continue outer
			}
			println("y == ${y}")
		}
	}`
	runGoldenTest(t, source, "labeled_for_loop_with_continue")
}

func TestCollect_ForLoopAsExpression(t *testing.T) {
	source := `
	let result = for var i = 0; i < 100; i += 1 {
		if i * i > 50 {
			break i * i
		}
	}`
	runGoldenTest(t, source, "for_loop_as_expression")
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

func TestCollect_LabeledForLoopBreakWithValue(t *testing.T) {
	source := `
	let result = outer: for i in 0..=10 {
		for j in 0..=10 {
			break outer i + j
		}
	}`
	runGoldenTest(t, source, "labeled_for_loop_break_with_value")
}
