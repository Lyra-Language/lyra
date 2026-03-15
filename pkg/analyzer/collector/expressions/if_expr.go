package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectIfThenExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.IfExpr {
	ifConditionNode := node.ChildByFieldName("condition")
	if ifConditionNode == nil {
		ctx.AddError(node, collctx.SeverityError, "collectIfThenExpr: if condition node is nil")
		return nil
	}

	thenExpressionNode := node.ChildByFieldName("then_expression")
	if thenExpressionNode == nil {
		ctx.AddError(node, collctx.SeverityError, "collectIfThenExpr: then expression node is nil")
		return nil
	}

	elseBranchNode := node.ChildByFieldName("else_branch")

	return &ast.IfExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    nil,
		},
		Condition: CollectExpression(ifConditionNode, ctx),
		Then:      CollectExpression(thenExpressionNode, ctx),
		Else:      CollectExpression(elseBranchNode, ctx),
	}
}

func collectIfBlockExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.IfExpr {
	ifConditionNode := node.ChildByFieldName("condition")
	if ifConditionNode == nil {
		ctx.AddError(node, collctx.SeverityError, "collectIfBlockExpr: if condition node is nil")
		return nil
	}

	thenBlockNode := node.ChildByFieldName("then_block")
	if thenBlockNode == nil {
		ctx.AddError(node, collctx.SeverityError, "collectIfBlockExpr: then block node is nil")
		return nil
	}

	elseBranchNode := node.ChildByFieldName("else_branch")

	return &ast.IfExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    nil,
		},
		Condition: CollectExpression(ifConditionNode, ctx),
		Then:      CollectExpression(thenBlockNode, ctx),
		Else:      CollectExpression(elseBranchNode, ctx),
	}
}
