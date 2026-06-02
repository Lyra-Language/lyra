package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectVarReassignmentStmt(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.VarReassignmentStmt {
	// The identifier has no field name; it is the first named child.
	identNode := node.NamedChild(0)
	if identNode == nil {
		ctx.AddError(node, diag.SeverityError, "var_reassignment: missing identifier")
		return nil
	}
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AddError(node, diag.SeverityError, "var_reassignment: missing value")
		return nil
	}
	return &ast.VarReassignmentStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:    ctx.NodeText(identNode),
		Value:   ctx.CollectExpr(valueNode),
	}
}
