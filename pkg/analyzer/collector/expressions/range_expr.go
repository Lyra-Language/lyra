package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectRangeExpr collects `0..<n`, `0..<=10`, `0..<=10:2`.
//
// Unlike a range *pattern*, an expression range has no open form — both bounds
// are required by the grammar, because an open-ended one would need the lazy
// iterator the language does not have. The end *operator* is optional in the
// grammar and required here, shared with the other two range collectors via
// ctx.RangeEndOperator (lyra-E032).
func collectRangeExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	rangeExpr := &ast.RangeExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		EndOperator: ctx.RangeEndOperator(node, "range expression"),
	}

	// Both bounds are grammar-required, so an absent one is a malformed parse
	// rather than an open range. Report it, but keep building: a nil returned as
	// an ast.Expression is a *typed* nil that slips past `expr == nil` and
	// crashes a later pass on its first field access (hazard 3).
	startNode := node.ChildByFieldName("start")
	if collector_ctx.RangeBound(startNode) {
		rangeExpr.Start = CollectExpression(startNode, ctx)
	} else {
		ctx.AddError(node, diag.SeverityError, "range expression must have a start bound")
	}
	endNode := node.ChildByFieldName("end")
	if collector_ctx.RangeBound(endNode) {
		rangeExpr.End = CollectExpression(endNode, ctx)
	} else {
		ctx.AddError(node, diag.SeverityError, "range expression must have an end bound")
	}
	if stepNode := node.ChildByFieldName("step"); stepNode != nil {
		rangeExpr.Step = CollectExpression(stepNode, ctx)
	}
	return rangeExpr
}
