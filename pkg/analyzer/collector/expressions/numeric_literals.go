package expressions

import (
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectIntegerLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.IntegerLiteralExpr {
	base := ast.IntegerBase10
	value := int64(0)
	var err error

	child := node.NamedChild(0)
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

	valueString := ctx.NodeText(child)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	valueStringToParse := valueStringWithoutUnderscores
	switch base {
	case ast.IntegerBase2:
		valueStringToParse = strings.TrimPrefix(valueStringToParse, "0b")
	case ast.IntegerBase8:
		valueStringToParse = strings.TrimPrefix(valueStringToParse, "0o")
	case ast.IntegerBase16:
		valueStringToParse = strings.TrimPrefix(valueStringToParse, "0x")
	}
	value, err = strconv.ParseInt(valueStringToParse, int(base), 64)
	if err != nil {
		ctx.AddError(node, collector_ctx.SeverityError, "failed to parse integer literal: %v", err)
		return nil
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

func collectFloatLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.FloatLiteralExpr {
	valueString := ctx.NodeText(node)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	value, err := strconv.ParseFloat(valueStringWithoutUnderscores, 64)
	if err != nil {
		ctx.AddError(node, collector_ctx.SeverityError, "failed to parse float literal: %v", err)
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
