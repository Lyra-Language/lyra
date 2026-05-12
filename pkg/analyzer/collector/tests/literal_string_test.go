package collector_test

import "testing"

func TestCollectSimpleStringLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let greeting = "Hello, World!"`, "simple_string_literal_expr")
}

func TestCollectEmptyStringLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let blank = ""`, "empty_string_literal_expr")
}

func TestCollectStringLiteralExprWithSimpleEscapes(t *testing.T) {
	runGoldenTest(t, `let line = "Hello\n\tWorld\\!"`, "string_literal_expr_with_simple_escapes")
}

func TestCollectStringLiteralExprWithNumericEscapes(t *testing.T) {
	// \x41 == 'A', é == 'é', \o101 == 'A'.
	runGoldenTest(t, `let s = "A=\x41 e=é A'=\o101"`, "string_literal_expr_with_numeric_escapes")
}

func TestCollectStringLiteralExprWithHashEscape(t *testing.T) {
	// \# escapes the hash so that "${" is treated as literal content rather
	// than starting an interpolation. The resulting value contains a literal "#".
	runGoldenTest(t, `let s = "Phone \#: not an interp"`, "string_literal_expr_with_hash_escape")
}

func TestCollectInterpolatedStringExprSimple(t *testing.T) {
	runGoldenTest(t, `let greeting = "Hello, ${name}!"`, "interpolated_string_expr_simple")
}

func TestCollectInterpolatedStringExprLeadingInterpolation(t *testing.T) {
	runGoldenTest(t, `let greeting = "${name} is here"`, "interpolated_string_expr_leading")
}

func TestCollectInterpolatedStringExprOnlyInterpolation(t *testing.T) {
	runGoldenTest(t, `let v = "${name}"`, "interpolated_string_expr_only")
}

func TestCollectInterpolatedStringExprMultipleSegments(t *testing.T) {
	runGoldenTest(t, `let point = "At (${x}, ${y})"`, "interpolated_string_expr_multiple_segments")
}

func TestCollectInterpolatedStringExprComplexExpression(t *testing.T) {
	runGoldenTest(t, `let msg = "Hello, ${employee.get_name()}!"`, "interpolated_string_expr_complex_expression")
}

func TestCollectInterpolatedStringExprEscapedHash(t *testing.T) {
	// The \# is a literal '#', while the second ${...} starts an interpolation.
	runGoldenTest(t, `let s = "Phone \#: ${phone_num}"`, "interpolated_string_expr_escaped_hash")
}

func TestCollectInterpolatedStringExprWithArithmetic(t *testing.T) {
	runGoldenTest(t, `let msg = "Total: ${price * qty}"`, "interpolated_string_expr_with_arithmetic")
}

func TestCollectInterpolatedStringExprNested(t *testing.T) {
	runGoldenTest(t, `let greeting = "Hello, ${if is_male then "Mr. ${last_name}" else "Mrs. ${last_name}" end}!"`, "interpolated_string_expr_nested")
}
