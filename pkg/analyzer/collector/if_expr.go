package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectIfThenExpr(node *sitter.Node) *ast.IfExpr {
	ifConditionNode := node.ChildByFieldName("condition")
	if ifConditionNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectIfThenExpr: if condition node is nil")
		return nil
	}

	thenExpressionNode := node.ChildByFieldName("then_expression")
	if thenExpressionNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectIfThenExpr: then expression node is nil")
		return nil
	}

	elseBranchNode := node.ChildByFieldName("else_branch")

	return &ast.IfExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
			Type:    nil, // Type will be resolved during type checking
		},
		Condition: c.collectExpression(ifConditionNode),
		Then:      c.collectExpression(thenExpressionNode),
		Else:      c.collectExpression(elseBranchNode),
	}
}

func (c *Collector) collectIfBlockExpr(node *sitter.Node) *ast.IfExpr {
	ifConditionNode := node.ChildByFieldName("condition")
	if ifConditionNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectIfBlockExpr: if condition node is nil")
		return nil
	}

	thenBlockNode := node.ChildByFieldName("then_block")
	if thenBlockNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectIfBlockExpr: then block node is nil")
		return nil
	}

	elseBranchNode := node.ChildByFieldName("else_branch")

	return &ast.IfExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
			Type:    nil, // Type will be resolved during type checking
		},
		Condition: c.collectExpression(ifConditionNode),
		Then:      c.collectExpression(thenBlockNode),
		Else:      c.collectExpression(elseBranchNode),
	}
}
