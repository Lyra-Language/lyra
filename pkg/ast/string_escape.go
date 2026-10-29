package ast

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The one reading of Lyra's escapes, for a string literal, a character literal and a
// string pattern alike: the collector unescapes a literal with it, and the backend a
// pattern, which keeps its raw source text (LiteralPattern.Value) — so the two cannot
// disagree about what `"\\"` is.

// StringPatternContent is the string a string literal pattern (`"a\tb"` as written,
// quotes included) matches: its text between the quotes, unescaped.
func StringPatternContent(raw string) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", fmt.Errorf("expected a quoted string pattern, got %q", raw)
	}
	return UnescapeString(raw[1 : len(raw)-1])
}

// UnescapeString converts the raw text of a `string_content` node into its runtime
// value by resolving Lyra escape sequences. Supported escapes:
//
//	\0 \a \b \e \f \n \r \t \v \\ \' \" \#   simple escapes
//	\oNNN                                  octal (3 digits)
//	\xNN                                   hex   (2 digits)
//	\uNNNN                                 unicode (4 hex digits)
//	\UNNNNNNNN                             unicode (8 hex digits)
func UnescapeString(raw string) (string, error) {
	var sb strings.Builder
	sb.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' {
			sb.WriteByte(c)
			continue
		}
		if i+1 >= len(raw) {
			return "", fmt.Errorf("dangling backslash")
		}
		i++
		switch esc := raw[i]; esc {
		case '0':
			// NUL, and nothing more. C reads `\0` as the start of an octal run — which is
			// what makes `\012` a newline there and `\08` an error — but Lyra's octal
			// carries an explicit `\o` prefix, so there is no digit run for this to begin
			// and no ambiguity to inherit. `"\012"` here is NUL followed by "12".
			//
			// An interior NUL is an ordinary byte in a Lyra string: the length is
			// authoritative, never a terminator. What it is *not* ordinary for is FFI —
			// `cstring_ptr` refuses a string containing one — which is the same reason
			// being able to write one down deliberately is worth having.
			sb.WriteByte(0)
		case 'a':
			sb.WriteByte('\a')
		case 'b':
			sb.WriteByte('\b')
		case 'e':
			sb.WriteByte(0x1b)
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'v':
			sb.WriteByte('\v')
		case '\\', '\'', '"', '#':
			sb.WriteByte(esc)
		case 'o':
			if i+3 >= len(raw) {
				return "", fmt.Errorf("invalid \\o escape: expected 3 octal digits")
			}
			n, err := strconv.ParseUint(raw[i+1:i+4], 8, 16)
			if err != nil {
				return "", fmt.Errorf("invalid \\o escape: %v", err)
			}
			sb.WriteRune(rune(n))
			i += 3
		case 'x':
			if i+2 >= len(raw) {
				return "", fmt.Errorf("invalid \\x escape: expected 2 hex digits")
			}
			n, err := strconv.ParseUint(raw[i+1:i+3], 16, 16)
			if err != nil {
				return "", fmt.Errorf("invalid \\x escape: %v", err)
			}
			sb.WriteByte(byte(n))
			i += 2
		case 'u':
			if i+4 >= len(raw) {
				return "", fmt.Errorf("invalid \\u escape: expected 4 hex digits")
			}
			n, err := strconv.ParseUint(raw[i+1:i+5], 16, 32)
			if err != nil {
				return "", fmt.Errorf("invalid \\u escape: %v", err)
			}
			r := rune(n)
			if err := validScalarValue(r); err != nil {
				return "", fmt.Errorf("invalid \\u escape: %v", err)
			}
			sb.WriteRune(r)
			i += 4
		case 'U':
			if i+8 >= len(raw) {
				return "", fmt.Errorf("invalid \\U escape: expected 8 hex digits")
			}
			n, err := strconv.ParseUint(raw[i+1:i+9], 16, 32)
			if err != nil {
				return "", fmt.Errorf("invalid \\U escape: %v", err)
			}
			r := rune(n)
			if err := validScalarValue(r); err != nil {
				return "", fmt.Errorf("invalid \\U escape: %v", err)
			}
			sb.WriteRune(r)
			i += 8
		default:
			return "", fmt.Errorf("unknown escape sequence: \\%c", esc)
		}
	}
	return sb.String(), nil
}

func validScalarValue(r rune) error {
	if r < 0 {
		return fmt.Errorf("code point U+%X out of range (max U+10FFFF)", r)
	}
	if r > utf8.MaxRune {
		return fmt.Errorf("code point U+%X out of range (max U+10FFFF)", r)
	}
	if utf16.IsSurrogate(r) {
		return fmt.Errorf("code point U+%X is a surrogate", r)
	}
	return nil
}
