package collector_test

import (
	"testing"
)

func TestCollectComplexPatternsDestructuring(t *testing.T) {
	source := `
	let {
		foo: [a, b, ...rest],
		bar: (c, Some d),
		baz: {e, f, ...rest}
	} = some_complex_struct`
	runGoldenTest(t, source, "complex_patterns_destructuring")
}

func TestCollectComplexPatternsPatternMatching(t *testing.T) {
	source := `
	match some_complex_struct {
		{ foo: [a, b, ...rest] } => print("An array with three elements"),
		{ bar: (c, Some d) } => print("A tuple with two elements"),
		{ baz: {e, f, ...rest} } => print("A struct with three elements"),
		_ => print("No match"),
	}`
	runGoldenTest(t, source, "complex_patterns_pattern_matching")
}
