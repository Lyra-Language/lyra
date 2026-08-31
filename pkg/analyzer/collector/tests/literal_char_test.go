package collector_test

import (
	"strings"
	"testing"
)

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

// A `rune` type annotation collects as a PrimitiveType, not a generic type
// variable — regression test for the collector once treating this type (then named
// `char`) as a GenericType (it had no primitive-type grammar rule, so it fell
// through to the bare-lowercase-identifier path).
func TestCollectRuneTypeAnnotation(t *testing.T) {
	runGoldenTest(t, `let f = (c: rune) -> rune => c`, "rune_type_annotation")
}

// `'\0'` is NUL. It is the one simple escape C has that Lyra did not, and the reason it is
// safe to have is that Lyra's octal carries an explicit `\o` prefix: in C `\0` opens a
// digit run, which is what makes `'\012'` a newline and `"\08"` an error, and here there is
// no run for it to open. See TestExec_NulEscape for what `"\012"` is instead.
func TestCollectCharLiteralExprNulEscape(t *testing.T) {
	runGoldenTest(t, `let nul = '\0'`, "char_literal_expr_nul_escape")
}

// An **illegal** escape is the collector's error, not the parser's (08/30) — the same move
// the modifier-order tests describe, and for the same reason: a token that enumerates what
// is legal can only *fail to match* when something is not, and a literal that does not
// parse reports as whatever token failed to shift.
//
// `'\q'` used to give a syntax error and two cascading type errors, where `"\q"` gave
// "unknown escape sequence: \q" — one mistake, two reports, and the worse one on the
// smaller literal. The token now admits any escape and `unescapeStringContent` decides,
// which is the string path's diagnostic reached with no new code.
func TestCollectCharLiteralIllegalEscapeIsACollectorError(t *testing.T) {
	msgs := collectDiagnostics(t, `let bad = '\q'`)
	assertOneContaining(t, msgs, "unknown escape sequence: \\q")
	assertOneContaining(t, msgs, "failed to parse character literal")
	if len(msgs) != 1 {
		t.Errorf("one typo should be one diagnostic, got %v", msgs)
	}
}

// The placeholder the rejected literal stands in as (hazard 3) is what keeps that count at
// one. Returning nothing crashed the compiler outright — a nil concrete pointer is a
// non-nil interface at the call site — and returning a true nil interface instead left the
// declaration uninitialized, so a single typo reported the escape, then that the binding
// was unused, then that it must be initialized.
func TestCollectCharLiteralIllegalEscapeDoesNotCascade(t *testing.T) {
	msgs := collectDiagnostics(t, "let main = () -> void => {\n  let a: rune = '\\q'\n  println(\"${i64(a)}\")\n}")
	assertOneContaining(t, msgs, "unknown escape sequence")
	for _, m := range msgs {
		if strings.Contains(m, "must be initialized") || strings.Contains(m, "never used") {
			t.Errorf("the placeholder should stop the cascade, got %v", msgs)
		}
	}
}

// The shape that motivated it. A rune literal that did not parse left the *repeat
// expression* around it parsed as something else, so `['\q'; 4]` reported that a
// `StaticArray<AnonymousTuple(), 2>` could not be assigned to a `DynamicArray<rune>` —
// three errors, none of them the mistake. It is now one, naming the escape.
func TestCollectCharLiteralIllegalEscapeInARepeatLiteral(t *testing.T) {
	msgs := collectDiagnostics(t, `let xs: []rune = ['\q'; 4]`)
	assertOneContaining(t, msgs, "unknown escape sequence: \\q")
	for _, m := range msgs {
		if strings.Contains(m, "AnonymousTuple") || strings.Contains(m, "StaticArray") {
			t.Errorf("the repeat expression should not be mis-parsed around a bad escape; got %v", msgs)
		}
	}
}

// The whole legal set still parses and still decodes, which is what a broadened token has
// to be checked against: the longer alternatives must keep winning (`\x41` is hex 65, not
// `\x` followed by a stray `41`), and the token must stay bounded by its closing quote.
func TestCollectCharLiteralLegalEscapesAreUnaffected(t *testing.T) {
	for _, lit := range []string{
		`'\0'`, `'\a'`, `'\b'`, `'\e'`, `'\f'`, `'\n'`, `'\r'`, `'\t'`, `'\v'`,
		`'\\'`, `'\''`, `'"'`, `'\o101'`, `'\x41'`, `'A'`, `'\U0001F600'`, `'é'`,
	} {
		if msgs := collectDiagnostics(t, "let c = "+lit); len(msgs) != 0 {
			t.Errorf("%s should collect cleanly, got %v", lit, msgs)
		}
	}
}
