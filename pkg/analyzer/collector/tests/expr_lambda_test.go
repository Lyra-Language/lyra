package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectLambdaExpression(t *testing.T) {
	source := `
	(a: int, b: int) -> int => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "lambda_expression.golden"))
}

func TestCollectLambdaExpressionWithNoReturnType(t *testing.T) {
	source := `
	let double = (x: int) => x * 2`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "lambda_expression_with_no_return_type.golden"))
}

func TestCollectFunctionThatReturnsLambda(t *testing.T) {
	source := `
	let make_sum_fn = (a: int) -> (int) -> int => (b) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_that_returns_lambda.golden"))
}

func TestCollect_FunctionThatAcceptsLambdaAsParameter(t *testing.T) {
	source := `
	let nums = [1, 2, 3]
	let doubled = map::<int, int>(nums, (x) => 2 * x)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_that_accepts_lambda_as_parameter.golden"))
}
