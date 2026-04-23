package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectSimpleStringLiteralExpr(t *testing.T) {
	source := `let greeting = "Hello, World!"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_string_literal_expr.golden"))
}

func TestCollectEmptyStringLiteralExpr(t *testing.T) {
	source := `let blank = ""`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "empty_string_literal_expr.golden"))
}

func TestCollectStringLiteralExprWithSimpleEscapes(t *testing.T) {
	source := `let line = "Hello\n\tWorld\\!"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "string_literal_expr_with_simple_escapes.golden"))
}

func TestCollectStringLiteralExprWithNumericEscapes(t *testing.T) {
	// \x41 == 'A', \u00e9 == 'é', \o101 == 'A'.
	source := `let s = "A=\x41 e=\u00e9 A'=\o101"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "string_literal_expr_with_numeric_escapes.golden"))
}

func TestCollectStringLiteralExprWithHashEscape(t *testing.T) {
	// \# escapes the hash so that "#{" is treated as literal content rather
	// than starting an interpolation. The resulting value contains a literal "#".
	source := `let s = "Phone \#: not an interp"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "string_literal_expr_with_hash_escape.golden"))
}

func TestCollectInterpolatedStringExprSimple(t *testing.T) {
	source := `let greeting = "Hello, #{name}!"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_simple.golden"))
}

func TestCollectInterpolatedStringExprLeadingInterpolation(t *testing.T) {
	source := `let greeting = "#{name} is here"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_leading.golden"))
}

func TestCollectInterpolatedStringExprOnlyInterpolation(t *testing.T) {
	source := `let v = "#{name}"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_only.golden"))
}

func TestCollectInterpolatedStringExprMultipleSegments(t *testing.T) {
	source := `let point = "At (#{x}, #{y})"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_multiple_segments.golden"))
}

func TestCollectInterpolatedStringExprComplexExpression(t *testing.T) {
	source := `let msg = "Hello, #{employee.get_name()}!"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_complex_expression.golden"))
}

func TestCollectInterpolatedStringExprEscapedHash(t *testing.T) {
	// The \# is a literal '#', while the second #{...} starts an interpolation.
	source := `let s = "Phone \#: #{phone_num}"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_escaped_hash.golden"))
}

func TestCollectInterpolatedStringExprWithArithmetic(t *testing.T) {
	source := `let msg = "Total: #{price * qty}"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_with_arithmetic.golden"))
}

func TestCollectInterpolatedStringExprNested(t *testing.T) {
	source := `let greeting = "Hello, #{if is_male then "Mr. #{last_name}" else "Mrs. #{last_name}" end}!"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "interpolated_string_expr_nested.golden"))
}
