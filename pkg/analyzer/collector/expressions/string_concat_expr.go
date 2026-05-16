package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStringConcatExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.StringConcatExpr {
	return &ast.StringConcatExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     CollectExpression(node.ChildByFieldName("left"), ctx),
		Operator: ctx.NodeText(node.ChildByFieldName("operator")),
		Right:    CollectExpression(node.ChildByFieldName("right"), ctx),
	}
}
