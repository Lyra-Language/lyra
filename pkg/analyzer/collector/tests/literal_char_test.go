package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectSimpleCharLiteralExpr(t *testing.T) {
	source := `let c = 'a'`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_char_literal_expr.golden"))
}

func TestCollectCharLiteralExprSimpleEscape(t *testing.T) {
	source := `let newline = '\n'`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "char_literal_expr_simple_escape.golden"))
}

func TestCollectCharLiteralExprHexEscape(t *testing.T) {
	// \x41 == 'A'
	source := `let a = '\x41'`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "char_literal_expr_hex_escape.golden"))
}

func TestCollectCharLiteralExprUnicodeDirectly(t *testing.T) {
	// é (U+00E9) written directly in the literal
	source := "let e_accent = 'é'"
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "char_literal_expr_unicode_directly.golden"))
}

func TestCollectCharLiteralExprUnicodeEscape(t *testing.T) {
	// \u00e9 == e with acute accent (U+00E9)
	source := `let e_accent = '\u00e9'`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "char_literal_expr_unicode_escape.golden"))
}

func TestCollectCharLiteralExprLargeUnicodeEscape(t *testing.T) {
	// \U0001F600 == grinning face emoji
	source := `let emoji = '\U0001F600'`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "char_literal_expr_large_unicode_escape.golden"))
}
