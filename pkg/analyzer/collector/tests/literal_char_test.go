package collector_test

import "testing"

func TestCollectSimpleCharLiteralExpr(t *testing.T) {
	runGoldenTest(t, `let c = 'a'`, "simple_char_literal_expr")
}

func TestCollectCharLiteralExprSimpleEscape(t *testing.T) {
	runGoldenTest(t, `let newline = '\n'`, "char_literal_expr_simple_escape")
}

func TestCollectCharLiteralExprHexEscape(t *testing.T) {
	// \x41 == 'A'
	runGoldenTest(t, `let a = '\x41'`, "char_literal_expr_hex_escape")
}

func TestCollectCharLiteralExprUnicodeDirectly(t *testing.T) {
	// é (U+00E9) written directly in the literal
	runGoldenTest(t, "let e_accent = 'é'", "char_literal_expr_unicode_directly")
}

func TestCollectCharLiteralExprUnicodeEscape(t *testing.T) {
	// é == e with acute accent (U+00E9)
	runGoldenTest(t, `let e_accent = 'é'`, "char_literal_expr_unicode_escape")
}

func TestCollectCharLiteralExprLargeUnicodeEscape(t *testing.T) {
	// \U0001F600 == grinning face emoji
	runGoldenTest(t, `let emoji = '\U0001F600'`, "char_literal_expr_large_unicode_escape")
}
