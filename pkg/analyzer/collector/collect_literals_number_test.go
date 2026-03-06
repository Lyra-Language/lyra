package collector

import (
	"path/filepath"
	"testing"
)

func TestCollectSimpleIntegerLiteralExpr(t *testing.T) {
	source := `let one_million = 1_000_000`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_integer_literal_expr.golden"))
}

func TestCollectBinaryIntegerLiteralExpr(t *testing.T) {
	source := `let fourty_two = 0b101010`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "binary_integer_literal_expr.golden"))
}

func TestCollectOctalIntegerLiteralExpr(t *testing.T) {
	source := `let fourty_two = 0o52`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "octal_integer_literal_expr.golden"))
}

func TestCollectHexadecimalIntegerLiteralExpr(t *testing.T) {
	source := `let fourty_two = 0x2A`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "hexadecimal_integer_literal_expr.golden"))
}

func TestCollectFloatLiteralExpr(t *testing.T) {
	source := `let pi = 3.14159`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_float_literal_expr.golden"))
}

func TestCollectFloatLiteralExprWithExponent(t *testing.T) {
	source := `let pi = 0.03141592e2`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "float_literal_expr_with_exponent.golden"))
}

func TestCollectFloatLiteralExprWithPositiveExponent(t *testing.T) {
	source := `let pi = 0.03141592e+2`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "float_literal_expr_with_positive_exponent.golden"))
}

func TestCollectFloatLiteralExprWithNegativeExponent(t *testing.T) {
	source := `let pi = 314.1592e-2`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "float_literal_expr_with_negative_exponent.golden"))
}

func TestCollectFloatLiteralExprWithUnderscores(t *testing.T) {
	source := `let pi = 3.141_59`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "float_literal_expr_with_underscores.golden"))
}
