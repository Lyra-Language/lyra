package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectLambdaExpression(t *testing.T) {
	source := `let sum = (a, b) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "lambda_expression.golden"))
}

func TestCollectLambdaExpressionWithTypeAnnotation(t *testing.T) {
	source := `let sum: (int, int) -> int = (a, b) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "lambda_expression_with_type_annotation.golden"))
}

func TestCollectLambdaExpressionWithGuard(t *testing.T) {
	source := `let divide = (a, b) if b != 0 => a / b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "lambda_expression_with_guard.golden"))
}

func TestCollectFunctionThatReturnsLambdaExpression(t *testing.T) {
	source := `
	def make_sum_fn: (int) -> (int) -> int =
		(a) => (b) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_that_returns_lambda_expression.golden"))
}

func TestCollectFunctionThatAcceptsLambdaExpression(t *testing.T) {
	source := `
	let doubled = map::<int, int>([1, 2, 3], (x) => 2 * x)
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_that_accepts_lambda_expression.golden"))
}