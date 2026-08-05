package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNullCoalescingExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	optional := CollectExpression(cst.Field(node, "optional"), ctx)
	if optional == nil {
		ctx.AddError(node, diag.SeverityError, "null coalescing expression must have an optional expression")
		return nil
	}
	defaultExpr := CollectExpression(cst.Field(node, "default"), ctx)
	if defaultExpr == nil {
		ctx.AddError(node, diag.SeverityError, "null coalescing expression must have a default expression")
		return nil
	}
	return &ast.NullCoalescingExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Optional: optional,
		Default:  defaultExpr,
	}
}
