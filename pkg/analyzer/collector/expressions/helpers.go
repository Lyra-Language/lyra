package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// spanNodes returns a Location from the start of `first` to the end of `last`.
func spanNodes(first, last *sitter.Node, ctx *collector_ctx.Ctx) ast.Location {
	start := ctx.NodeLocation(first)
	end := ctx.NodeLocation(last)
	return ast.Location{
		File:      start.File,
		StartLine: start.StartLine,
		StartCol:  start.StartCol,
		EndLine:   end.EndLine,
		EndCol:    end.EndCol,
	}
}

// collectGenericArgs collects type arguments from a generic_arguments node,
// delegating type parsing to ctx.ParseType.
func collectGenericArgs(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Type {
	args := []types.Type{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			args = append(args, ctx.ParseType(child))
		}
	}
	return args
}

func collectGuard(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.GuardExpr {
	guardExpression := CollectExpression(cst.Field(node, "guard_expression"), ctx)
	if guardExpression == nil {
		ctx.AddError(node, diag.SeverityError, "CollectGuard: guard expression is nil")
		return nil
	}
	return &ast.GuardExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Condition: guardExpression,
	}
}
