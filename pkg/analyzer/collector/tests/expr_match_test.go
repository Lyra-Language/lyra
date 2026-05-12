package collector_test

import "testing"

func TestCollectMatchExpression(t *testing.T) {
	source := `
	let bar = match foo {
		Some 42 => "The Answer!",
		Some _ => "Just some number",
		None => "huh?",
	}`
	runGoldenTest(t, source, "match_expression")
}

func TestCollectMatchExpressionWithBlocks(t *testing.T) {
	source := `
	match foo {
		[a] => {
			println("An array with one element")
		},
		[a, b] => {
			println("An array with two elements")
		},
		_ => {
			println("A wildcard match")
		},
	}`
	runGoldenTest(t, source, "match_expression_with_blocks")
}

func TestCollectMatchExpressionWithStructsReturned(t *testing.T) {
	source := `
	let foo = match bar {
		"foo" => { a: "b" },
		"bar" => { b: "a" },
		_ => { c: "d" },
	}`
	runGoldenTest(t, source, "match_expression_with_structs_returned")
}

func TestCollectMatchExpressionWithRangePatterns(t *testing.T) {
	source := `
	match foo {
		0..=9 => print("one digit"),
		42 => print("The Answer!"),
		10..=99 => print("two digits"),
		_ => print("lots of digits!"),
	}`
	runGoldenTest(t, source, "match_expression_with_range_patterns")
}

func TestCollectMatchExpressionWithGuards(t *testing.T) {
	source := `
	match foo {
		Some x if x > 0 && x < 10 => print("1-9"),
		Some x if x >= 10 && x < 100 => print("10-99"),
		None => print("No number!"),
	}`
	runGoldenTest(t, source, "match_expression_with_guards")
}
