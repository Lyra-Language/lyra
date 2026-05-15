package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNotBooleanExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.NotBooleanExpr {
	return &ast.NotBooleanExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Expression: CollectExpression(node.ChildByFieldName("expression"), ctx),
	}
}

func collectBinaryBooleanExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.BooleanBinaryOpExpr {
	return &ast.BooleanBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     CollectExpression(node.ChildByFieldName("left"), ctx),
		Operator: ast.BooleanBinaryOp(ctx.NodeText(node.ChildByFieldName("operator"))),
		Right:    CollectExpression(node.ChildByFieldName("right"), ctx),
	}
}
