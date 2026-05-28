package collector_test

import (
	"testing"
)

func TestBindingPatternInMatch(t *testing.T) {
	source := `
	match xs {
		all @ [head, ...tail] => print(all),
		_ => print("no match"),
	}`
	runGoldenTest(t, source, "binding_pattern_in_match")
}

func TestBindingPatternInDestructuring(t *testing.T) {
	source := `let all @ [a, b, ...rest] = some_array`
	runGoldenTest(t, source, "binding_pattern_in_destructuring")
}

func TestBindingPatternNested(t *testing.T) {
	source := `
	match point {
		{ x: px @ 0..=100, y } => print(px),
		_ => print("out of range"),
	}`
	runGoldenTest(t, source, "binding_pattern_nested")
}
