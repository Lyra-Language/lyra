package collector_test

import "testing"

func TestCollect_InfiniteLoop(t *testing.T) {
	source := `
	loop {
		println("All work and no play makes Homer something something...")
	}`
	runGoldenTest(t, source, "infinite_loop")
}

func TestCollect_WhileLoop(t *testing.T) {
	source := `
	var i = 0
	while i < 10 {
		println("i == ${i}")
		i += 1
	}`
	runGoldenTest(t, source, "while_loop")
}

func TestCollect_WhileLoopWithBreak(t *testing.T) {
	source := `
	while running {
		break
	}`
	runGoldenTest(t, source, "while_loop_with_break")
}

func TestCollect_WhileLoopWithContinue(t *testing.T) {
	source := `
	var i = 0
	while i < 10 {
		i += 1
		if i % 2 == 0 {
			continue
		}
		println("i == ${i}")
	}`
	runGoldenTest(t, source, "while_loop_with_continue")
}

func TestCollect_LabeledLoopWithBreak(t *testing.T) {
	source := `
	outer: loop {
		while ready {
			break outer
		}
	}`
	runGoldenTest(t, source, "labeled_loop_with_break")
}

func TestCollect_LabeledWhileLoopWithContinue(t *testing.T) {
	source := `
	var i = 0
	outer: while i < 10 {
		i += 1
		loop {
			continue outer
		}
	}`
	runGoldenTest(t, source, "labeled_while_loop_with_continue")
}

func TestCollect_LoopAsExpression(t *testing.T) {
	source := `
	var i = 0
	let result = loop {
		i += 1
		if i * i > 50 {
			break i * i
		}
	}`
	runGoldenTest(t, source, "loop_as_expression")
}
