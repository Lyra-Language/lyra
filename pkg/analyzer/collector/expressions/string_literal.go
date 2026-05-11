package expressions

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStringLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	// string_literal has named children that are either `string_content` or
	// `string_interpolation`. When no interpolation segments are present we
	// return a plain StringLiteralExpr; otherwise an InterpolatedStringExpr
	// whose segments alternate between literal text chunks and the expressions
	// embedded inside `${ ... }`.
	childCount := node.NamedChildCount()

	stringType := types.PrimitiveType{Name: types.String}
	newLiteral := func(l ast.Location, value string) *ast.StringLiteralExpr {
		return &ast.StringLiteralExpr{
			ExprBase: ast.ExprBase{
				AstBase: ast.AstBase{Location: l},
				Type:    stringType,
			},
			Value: value,
		}
	}

	hasInterpolation := false
	for i := uint(0); i < childCount; i++ {
		if node.NamedChild(i).Kind() == "string_interpolation" {
			hasInterpolation = true
			break
		}
	}

	if !hasInterpolation {
		var sb strings.Builder
		for i := uint(0); i < childCount; i++ {
			child := node.NamedChild(i)
			content, err := unescapeStringContent(ctx.NodeText(child))
			if err != nil {
				ctx.AddError(child, collector_ctx.SeverityError, "invalid string literal: %v", err)
				return nil
			}
			sb.WriteString(content)
		}
		return newLiteral(loc, sb.String())
	}

	segments := make([]ast.Expression, 0, childCount)
	for i := uint(0); i < childCount; i++ {
		child := node.NamedChild(i)
		switch child.Kind() {
		case "string_content":
			content, err := unescapeStringContent(ctx.NodeText(child))
			if err != nil {
				ctx.AddError(child, collector_ctx.SeverityError, "invalid string literal: %v", err)
				return nil
			}
			segments = append(segments, newLiteral(ctx.NodeLocation(child), content))
		case "string_interpolation":
			exprNode := child.NamedChild(0)
			if exprNode == nil {
				ctx.AddError(child, collector_ctx.SeverityError, "empty string interpolation")
				return nil
			}
			expr := CollectExpression(exprNode, ctx)
			if expr == nil {
				return nil
			}
			segments = append(segments, expr)
		}
	}

	return &ast.InterpolatedStringExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    stringType,
		},
		Segments: segments,
	}
}

// unescapeStringContent converts the raw text of a `string_content` node into
// its runtime value by resolving Lyra escape sequences. Supported escapes:
//
//	\a \b \e \f \n \r \t \v \\ \' \" \#   simple escapes
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
