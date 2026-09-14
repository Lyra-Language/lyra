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
		if child.IsNamed() && !cst.IsComment(child) {
			args = append(args, ctx.ParseType(child))
		}
	}
	return args
}

// appendCollected appends a collected expression to a list, dropping a nil.
//
// ast.TrueNil turns a typed nil into the nil the typechecker checks for — but only where it
// checks: a nil *inside* an argument list, array or tuple literal, or a struct literal's
// fields is dereferenced unguarded (`inferLambdaCall` → `checkNamedArgument`). A collector
// returns nil only on an error path (a diagnostic, or a syntax error behind it), so dropping
// it cannot make a wrong program compile; at worst the list's length earns a second
// diagnostic. A collector that hits a value error should still return a placeholder
// (hazard 3) — this is the backstop for one that does not.
func appendCollected(list []ast.Expression, expr ast.Expression) []ast.Expression {
	if expr == nil {
		return list
	}
	return append(list, expr)
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
