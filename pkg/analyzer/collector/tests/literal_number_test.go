package collector_test

import "testing"

func TestCollectSimpleIntegerLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let one_million = 1_000_000`, "simple_integer_literal_expr")
}

func TestCollectBinaryIntegerLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let fourty_two = 0b101010`, "binary_integer_literal_expr")
}

func TestCollectOctalIntegerLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let fourty_two = 0o52`, "octal_integer_literal_expr")
}

func TestCollectHexadecimalIntegerLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let fourty_two = 0x2A`, "hexadecimal_integer_literal_expr")
}

func TestCollectFloatLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let pi = 3.14159`, "simple_float_literal_expr")
}

func TestCollectFloatLiteralExprWithExponent(t *testing.T) {
	runGoldenTest(t, `let pi = 0.03141592e2`, "float_literal_expr_with_exponent")
}

func TestCollectFloatLiteralExprWithPositiveExponent(t *testing.T) {
	runGoldenTest(t, `let pi = 0.03141592e+2`, "float_literal_expr_with_positive_exponent")
}

func TestCollectFloatLiteralExprWithNegativeExponent(t *testing.T) {
	runGoldenTest(t, `let pi = 314.1592e-2`, "float_literal_expr_with_negative_exponent")
}

func TestCollectFloatLiteralExprWithUnderscores(t *testing.T) {
	runGoldenTest(t, `let pi = 3.141_59`, "float_literal_expr_with_underscores")
}
