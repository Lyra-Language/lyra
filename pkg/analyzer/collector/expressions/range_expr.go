package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectRangeExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.RangeExpr {
	startNode := node.ChildByFieldName("start")
	if startNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "range expression must have a start")
		return nil
	}
	endOperatorNode := node.ChildByFieldName("end_operator")
	if endOperatorNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "range expression must have an end operator")
		return nil
	}
	endNode := node.ChildByFieldName("end")
	if endNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "range expression must have an end")
		return nil
	}
	step := ast.Expression(nil)
	if stepNode := node.ChildByFieldName("step"); stepNode != nil {
		step = CollectExpression(stepNode, ctx)
	}
	return &ast.RangeExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Start:       CollectExpression(startNode, ctx),
		EndOperator: ctx.NodeText(endOperatorNode),
		End:         CollectExpression(endNode, ctx),
		Step:        step,
	}
}
