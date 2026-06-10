package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectAwaitExpr(node *sitter.Node, ctx *collector_ctx.Ctx, _ ast.Location) *ast.AwaitExpr {
	return &ast.AwaitExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node.Child(0))}},
		Operand:  CollectExpression(node.ChildByFieldName("operand"), ctx),
	}
}
