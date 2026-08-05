package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectYieldExpr(node *sitter.Node, ctx *collector_ctx.Ctx, _ ast.Location) *ast.YieldExpr {
	return &ast.YieldExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node.Child(0))}},
		Value:    CollectExpression(cst.Field(node, "value"), ctx),
	}
}

func collectYieldFromExpr(node *sitter.Node, ctx *collector_ctx.Ctx, _ ast.Location) *ast.YieldFromExpr {
	return &ast.YieldFromExpr{
		// Span "yield from" (child 0 = "yield", child 1 = "from").
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: spanNodes(node.Child(0), node.Child(1), ctx)}},
		Generator: CollectExpression(cst.Field(node, "generator"), ctx),
	}
}
