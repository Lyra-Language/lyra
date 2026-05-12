package collector_test

import "testing"

func TestCollectBasicArrayCompExpr(t *testing.T) {
	runGoldenTest(t, `let squares = [ 1..=5 -> x, | x * x ]`, "array_comp_basic")
}

func TestCollectArrayCompExprWithGuard(t *testing.T) {
	runGoldenTest(t, `let evens = [ 1..=5 -> x, | x % 2 == 0 | x * x ]`, "array_comp_with_guard")
}

func TestCollectArrayCompExprWithMultipleGuardsAndGenerators(t *testing.T) {
	runGoldenTest(t, `let foo = [ 1..=5 -> x, 1..=5 -> y | odd(x), even(y) | (x, y, x * y) ]`, "array_comp_multiple_guards")
}

func TestCollectArrayCompExprWithArrayLiteralGenerator(t *testing.T) {
	runGoldenTest(t, `let foo = [ [1, 2, 3] -> x | x * x ]`, "array_comp_array_literal_generator")
}
