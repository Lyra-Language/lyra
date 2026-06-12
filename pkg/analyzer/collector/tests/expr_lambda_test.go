package collector_test

import "testing"

func TestCollectLambdaExpression(t *testing.T) {
	runGoldenTest(t, `
	(a: i64, b: i64) -> i64 => a + b`, "lambda_expression")
}

func TestCollectLambdaExpressionWithNoReturnType(t *testing.T) {
	runGoldenTest(t, `
	let double = (x: i64) => x * 2`, "lambda_expression_with_no_return_type")
}

func TestCollectFunctionThatReturnsLambda(t *testing.T) {
	runGoldenTest(t, `
	let make_sum_fn = (a: i64) -> (i64) -> i64 => (b) => a + b`, "function_that_returns_lambda")
}

func TestCollect_FunctionThatAcceptsLambdaAsParameter(t *testing.T) {
	source := `
	let nums = [1, 2, 3]
	let doubled = map::<i64, i64>(nums, (x) => 2 * x)`
	runGoldenTest(t, source, "function_that_accepts_lambda_as_parameter")
}
