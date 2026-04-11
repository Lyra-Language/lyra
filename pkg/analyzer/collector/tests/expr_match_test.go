package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectMatchExpression(t *testing.T) {
	source := `
	let bar = match foo {
		Some 42 => "The Answer!",
		Some _ => "Just some number",
		None => "huh?",
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "match_expression.golden"))
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
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "match_expression_with_blocks.golden"))
}

func TestCollectMatchExpressionWithStructsReturned(t *testing.T) {
	source := `
	let foo = match bar {
		"foo" => { a: "b" },
		"bar" => { b: "a" },
		_ => { c: "d" },
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "match_expression_with_structs_returned.golden"))
}

func TestCollectMatchExpressionWithRangePatterns(t *testing.T) {
	source := `
	match foo {
		0..=9 => print("one digit"),
		42 => print("The Answer!"),
		10..=99 => print("two digits"),
		_ => print("lots of digits!"),
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "match_expression_with_range_patterns.golden"))
}

func TestCollectMatchExpressionWithGuards(t *testing.T) {
	source := `
	match foo {
		Some x if x > 0 && x < 10 => print("1-9"),
		Some x if x >= 10 && x < 100 => print("10-99"),
		None => print("No number!"),
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "match_expression_with_guards.golden"))
}