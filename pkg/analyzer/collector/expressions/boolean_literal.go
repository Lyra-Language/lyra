package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectBooleanLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.BooleanLiteralExpr {
	return &ast.BooleanLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.Bool},
		},
		Value: ctx.NodeText(node) == "true",
	}
}
