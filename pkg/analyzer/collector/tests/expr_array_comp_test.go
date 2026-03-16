package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectBasicArrayCompExpr(t *testing.T) {
	source := `let squares = [ 1..=5 -> x, | x * x ]`
	program, _ := parseAndCollect(t, source)

	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_comp_basic.golden"))
}

func TestCollectArrayCompExprWithGuard(t *testing.T) {
	source := `let evens = [ 1..=5 -> x, | x % 2 == 0 | x * x ]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_comp_with_guard.golden"))
}

func TestCollectArrayCompExprWithMultipleGuardsAndGenerators(t *testing.T) {
	source := `let foo = [ 1..=5 -> x, 1..=5 -> y | odd(x), even(y) | (x, y, x * y) ]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_comp_multiple_guards.golden"))
}

func TestCollectArrayCompExprWithArrayLiteralGenerator(t *testing.T) {
	source := `let foo = [ [1, 2, 3] -> x | x * x ]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_comp_array_literal_generator.golden"))
}
