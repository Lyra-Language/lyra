package expressions

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectRangeExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.RangeExpr {
	startNode := node.ChildByFieldName("start")
	if startNode == nil {
		ctx.AppendError(fmt.Errorf("range expression must have a start"))
		return nil
	}
	endOperatorNode := node.ChildByFieldName("end_operator")
	if endOperatorNode == nil {
		ctx.AppendError(fmt.Errorf("range expression must have an end operator"))
		return nil
	}
	endNode := node.ChildByFieldName("end")
	if endNode == nil {
		ctx.AppendError(fmt.Errorf("range expression must have an end"))
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
