package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectLValueAssignmentStmt collects an interior-mutation statement whose
// target is a member or index path (`p.x = v`, `arr[i] = v`, `grid[i].y = v`).
// The target expression is collected as an ordinary postfix expression; the
// typechecker is responsible for verifying the path is rooted at a binding that
// permits interior mutation.
func CollectLValueAssignmentStmt(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.LValueAssignmentStmt {
	targetNode := node.ChildByFieldName("target")
	if targetNode == nil {
		ctx.AddError(node, diag.SeverityError, "lvalue_assignment: missing target")
		return nil
	}
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AddError(node, diag.SeverityError, "lvalue_assignment: missing value")
		return nil
	}
	return &ast.LValueAssignmentStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Target:  ctx.CollectExpr(targetNode),
		Value:   ctx.CollectExpr(valueNode),
	}
}
