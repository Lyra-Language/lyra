package collector_test

import "testing"

func TestCollect_GivenExprSingleBinding(t *testing.T) {
	source := `let y = a + b given { let a = 1 }`
	runGoldenTest(t, source, "expr_given_single_binding")
}

func TestCollect_GivenExprMultipleBindings(t *testing.T) {
	source := `
	let result = sqrt(a_sq + b_sq) given {
		let a_sq = a * a
		let b_sq = b * b
	}`
	runGoldenTest(t, source, "expr_given_multiple_bindings")
}

func TestCollect_GivenExprWithConst(t *testing.T) {
	source := `let circumference = 2 * PI * r given { const PI = 3.1415 }`
	runGoldenTest(t, source, "expr_given_with_const")
}

func TestCollect_GivenExprWithDestructuringBinding(t *testing.T) {
	source := `
	let msg = greet(name) given {
		let Some name = get_name() ?? "World"
	}`
	runGoldenTest(t, source, "expr_given_with_destructuring_binding")
}
