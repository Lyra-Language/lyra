package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectBooleanBinaryExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.BooleanBinaryOpExpr {
	return &ast.BooleanBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     CollectExpression(node.ChildByFieldName("left"), ctx),
		Operator: ast.BooleanBinaryOp(ctx.NodeText(node.ChildByFieldName("operator"))),
		Right:    CollectExpression(node.ChildByFieldName("right"), ctx),
	}
}
