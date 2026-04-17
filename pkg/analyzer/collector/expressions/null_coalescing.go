package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNullCoalescingExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.NullCoalescingExpr {
	optional := CollectExpression(node.ChildByFieldName("optional"), ctx)
	if optional == nil {
		ctx.AddError(node, collctx.SeverityError, "null coalescing expression must have an optional expression")
		return nil
	}
	defaultExpr := CollectExpression(node.ChildByFieldName("default"), ctx)
	if defaultExpr == nil {
		ctx.AddError(node, collctx.SeverityError, "null coalescing expression must have a default expression")
		return nil
	}
	return &ast.NullCoalescingExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Optional: optional,
		Default:  defaultExpr,
	}
}