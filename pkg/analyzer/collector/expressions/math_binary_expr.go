package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectMathBinaryExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.MathBinaryOpExpr {
	return &ast.MathBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     CollectExpression(cst.Field(node, "left"), ctx),
		Operator: ast.MathBinaryOp(ctx.NodeText(cst.Field(node, "operator"))),
		Right:    CollectExpression(cst.Field(node, "right"), ctx),
	}
}
