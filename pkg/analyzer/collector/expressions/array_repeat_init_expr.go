package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectArrayRepeatInitExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.ArrayRepeatExpr {
	return &ast.ArrayRepeatExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    nil,
		},
		Value: CollectExpression(node.ChildByFieldName("value"), ctx),
		Count: CollectExpression(node.ChildByFieldName("count"), ctx),
	}
}
