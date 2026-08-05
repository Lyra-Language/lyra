package collector_test

import "testing"

func TestCollectBasicArrayCompExpr(t *testing.T) {
	runGoldenTest(t, `let squares = [ x in 1..<=5 | x * x ]`, "array_comp_basic")
}

func TestCollectArrayCompExprWithGuard(t *testing.T) {
	runGoldenTest(t, `let evens = [ x in 1..<=5 | x % 2 == 0 | x * x ]`, "array_comp_with_guard")
}

func TestCollectArrayCompExprWithMultipleGuardsAndGenerators(t *testing.T) {
	runGoldenTest(t, `let foo = [ x in 1..<=5, y in 1..<=5 | odd(x), even(y) | (x, y, x * y) ]`, "array_comp_multiple_guards")
}

func TestCollectArrayCompExprWithArrayLiteralGenerator(t *testing.T) {
	runGoldenTest(t, `let foo = [ x in [1, 2, 3] | x * x ]`, "array_comp_array_literal_generator")
}
