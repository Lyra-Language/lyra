package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDerefAssignmentStmt(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.DerefAssignmentStmt {
	targetNode := node.ChildByFieldName("target")
	if targetNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "deref_assignment: missing target")
		return nil
	}
	operandNode := targetNode.ChildByFieldName("operand")
	if operandNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "deref_assignment: missing operand on target")
		return nil
	}
	target := ast.DerefExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(targetNode)}},
		Operand:  ctx.CollectExpr(operandNode),
	}

	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "deref_assignment: missing value")
		return nil
	}

	return &ast.DerefAssignmentStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Target:  target,
		Value:   ctx.CollectExpr(valueNode),
	}
}
