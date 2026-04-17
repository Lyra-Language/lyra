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

	for i := uint(0); i < node.ChildCount(); i++ {
		valueString := ctx.NodeText(node.Child(i))
		valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
		value, err = strconv.ParseInt(valueStringWithoutUnderscores, 0, 64)
		if err != nil {
			ctx.AppendError(fmt.Errorf("failed to parse integer literal: %w", err))
			return nil
		}
		switch node.Child(i).Kind() {
		case "binary_int":
			base = ast.IntegerBase2
		case "octal_int":
			base = ast.IntegerBase8
		case "decimal_int":
			base = ast.IntegerBase10
		case "hexadecimal_int":
			base = ast.IntegerBase16
		}
	}
	return &ast.IntegerLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    nil,
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
		ctx.AppendError(fmt.Errorf("failed to parse float literal: %w", err))
		return nil
	}
	return &ast.FloatLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    types.PrimitiveType{Name: "float"},
		},
		Value: value,
	}
}

func collectStringLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.StringLiteralExpr {
	value, err := strconv.Unquote(ctx.NodeText(node))
	if err != nil {
		ctx.AppendError(fmt.Errorf("invalid string literal: %v", err))
		return nil
	}
	return &ast.StringLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.String},
		},
		Value: value,
	}
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
