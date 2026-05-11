package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectGenericArgs collects type arguments from a generic_arguments node,
// delegating type parsing to ctx.ParseType.
func collectGenericArgs(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Type {
	args := make([]types.Type, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			args = append(args, ctx.ParseType(child))
		}
	}
	return args
}

func collectGuard(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.GuardExpr {
	guardExpression := CollectExpression(node.ChildByFieldName("guard_expression"), ctx)
	if guardExpression == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "CollectGuard: guard expression is nil")
		return nil
	}
	return &ast.GuardExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Condition: guardExpression,
	}
}
