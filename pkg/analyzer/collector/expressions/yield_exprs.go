package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectYieldExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.YieldExpr {
	return &ast.YieldExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    CollectExpression(node.ChildByFieldName("value"), ctx),
	}
}

func collectYieldFromExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.YieldFromExpr {
	return &ast.YieldFromExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Generator: CollectExpression(node.ChildByFieldName("generator"), ctx),
	}
}
