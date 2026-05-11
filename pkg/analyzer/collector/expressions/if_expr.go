package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectIfExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	ifConditionNode, ok := ctx.MustField(node, "condition")
	if !ok {
		return nil
	}

	thenBlockNode, ok := ctx.MustField(node, "then_block")
	if !ok {
		return nil
	}

	elseBranchNode := node.ChildByFieldName("else_branch")

	return &ast.IfExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    nil,
		},
		Condition: CollectExpression(ifConditionNode, ctx),
		Then:      CollectExpression(thenBlockNode, ctx),
		Else:      CollectExpression(elseBranchNode, ctx),
	}
}
