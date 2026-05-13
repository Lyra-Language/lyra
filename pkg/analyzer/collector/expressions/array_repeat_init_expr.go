package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectArrayRepeatInitExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.ArrayRepeatExpr {
	return &ast.ArrayRepeatExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    CollectExpression(node.ChildByFieldName("value"), ctx),
		Count:    CollectExpression(node.ChildByFieldName("count"), ctx),
	}
}
