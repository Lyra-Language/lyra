package expressions

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStringLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	// A string_literal spans `"…"` and has named children that are either
	// `string_content` (literal text chunks) or `string_interpolation` (`${expr}`).
	// We reconstruct the literal text from the *raw source between* the
	// interpolations rather than from the `string_content` node text: tree-sitter,
	// with `/\s/` in `extras`, strips a content chunk's leading whitespace as token
	// padding, so a `string_content` node that begins with a space (a plain `"  x"`,
	// or the text right after a `${…}`) loses it. Slicing the source directly — the
	// interpolation nodes' byte ranges are exact and whitespace-safe (they start at
	// `$`) — recovers every byte. When there are no interpolations we return a plain
	// StringLiteralExpr; otherwise an InterpolatedStringExpr whose segments
	// alternate literal chunks and the embedded expressions.
	innerStart := node.StartByte() + 1 // just past the opening quote
	innerEnd := node.EndByte() - 1     // just before the closing quote

	newLiteralChunk := func(start, end uint) (*ast.StringLiteralExpr, bool) {
		content, err := unescapeStringContent(string(ctx.Source[start:end]))
		if err != nil {
			ctx.AddError(node, diag.SeverityError, "invalid string literal: %v", err)
			return nil, false
		}
		return &ast.StringLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    content,
		}, true
	}

	// Gather the interpolation children in source order.
	var interps []*sitter.Node
	for i := range node.NamedChildCount() {
		if child := node.NamedChild(i); child.Kind() == "string_interpolation" {
			interps = append(interps, child)
		}
	}

	if len(interps) == 0 {
		lit, ok := newLiteralChunk(innerStart, innerEnd)
		if !ok {
			return nil
		}
		return lit
	}

	segments := []ast.Expression{}
	cursor := innerStart
	for _, interp := range interps {
		// The literal chunk is the raw source from the cursor up to this `${`.
		if start := interp.StartByte(); start > cursor {
			lit, ok := newLiteralChunk(cursor, start)
			if !ok {
				return nil
			}
			segments = append(segments, lit)
		}
		exprNode := interp.NamedChild(0)
		if exprNode == nil {
			ctx.AddError(interp, diag.SeverityError, "empty string interpolation")
			return nil
		}
		expr := CollectExpression(exprNode, ctx)
		if expr == nil {
			return nil
		}
		segments = append(segments, expr)
		cursor = interp.EndByte() // resume just past the closing `}`
	}
	// Trailing literal chunk after the last interpolation.
	if innerEnd > cursor {
		lit, ok := newLiteralChunk(cursor, innerEnd)
		if !ok {
			return nil
		}
		segments = append(segments, lit)
	}

	return &ast.InterpolatedStringExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Segments: segments,
	}
}

// unescapeStringContent converts the raw text of a `string_content` node into
// its runtime value by resolving Lyra escape sequences. Supported escapes:
//
//	\0 \a \b \e \f \n \r \t \v \\ \' \" \#   simple escapes
//	\oNNN                                  octal (3 digits)
//	\xNN                                   hex   (2 digits)
//	\uNNNN                                 unicode (4 hex digits)
//	\UNNNNNNNN                             unicode (8 hex digits)
func unescapeStringContent(raw string) (string, error) {
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
			if err := validateUnicodeScalarValue(r); err != nil {
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
			if err := validateUnicodeScalarValue(r); err != nil {
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

func validateUnicodeScalarValue(r rune) error {
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
