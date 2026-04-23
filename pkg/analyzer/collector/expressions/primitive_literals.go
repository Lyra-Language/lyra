package expressions

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectIntegerLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.IntegerLiteralExpr {
	base := ast.IntegerBase10
	value := int64(0)
	var err error

	child := node.NamedChild(0)
	valueString := ctx.NodeText(child)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	value, err = strconv.ParseInt(valueStringWithoutUnderscores, 0, 64)
	if err != nil {
		ctx.AddError(node, collctx.SeverityError, "failed to parse integer literal: %v", err)
		return nil
	}
	switch child.Kind() {
	case "binary_int":
		base = ast.IntegerBase2
	case "octal_int":
		base = ast.IntegerBase8
	case "decimal_int":
		base = ast.IntegerBase10
	case "hexadecimal_int":
		base = ast.IntegerBase16
	}

	return &ast.IntegerLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.Int},
		},
		Value: value,
		Base:  base,
	}
}

func collectFloatLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.FloatLiteralExpr {
	valueString := ctx.NodeText(node)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	value, err := strconv.ParseFloat(valueStringWithoutUnderscores, 64)
	if err != nil {
		ctx.AddError(node, collctx.SeverityError, "failed to parse float literal: %v", err)
		return nil
	}
	return &ast.FloatLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.Float},
		},
		Value: value,
	}
}

func collectStringLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) ast.Expression {
	// string_literal has named children that are either `string_content` or
	// `string_interpolation`. When no interpolation segments are present we
	// return a plain StringLiteralExpr; otherwise an InterpolatedStringExpr
	// whose segments alternate between literal text chunks and the expressions
	// embedded inside `#{ ... }`.
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
				ctx.AddError(child, collctx.SeverityError, "invalid string literal: %v", err)
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
				ctx.AddError(child, collctx.SeverityError, "invalid string literal: %v", err)
				return nil
			}
			segments = append(segments, newLiteral(ctx.NodeLocation(child), content))
		case "string_interpolation":
			exprNode := child.NamedChild(0)
			if exprNode == nil {
				ctx.AddError(child, collctx.SeverityError, "empty string interpolation")
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
			sb.WriteRune(rune(n))
			i += 4
		case 'U':
			if i+8 >= len(raw) {
				return "", fmt.Errorf("invalid \\U escape: expected 8 hex digits")
			}
			n, err := strconv.ParseUint(raw[i+1:i+9], 16, 32)
			if err != nil {
				return "", fmt.Errorf("invalid \\U escape: %v", err)
			}
			sb.WriteRune(rune(n))
			i += 8
		default:
			return "", fmt.Errorf("unknown escape sequence: \\%c", esc)
		}
	}
	return sb.String(), nil
}

func collectBooleanLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.BooleanLiteralExpr {
	return &ast.BooleanLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.Bool},
		},
		Value: ctx.NodeText(node) == "true",
	}
}
