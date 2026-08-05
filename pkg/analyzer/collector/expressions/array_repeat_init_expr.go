package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectArrayRepeatInitExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.ArrayRepeatExpr {
	return &ast.ArrayRepeatExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    CollectExpression(cst.Field(node, "value"), ctx),
		Count:    CollectExpression(cst.Field(node, "count"), ctx),
	}
}
