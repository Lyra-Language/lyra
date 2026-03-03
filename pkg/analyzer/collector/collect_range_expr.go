package collector

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectRangeExpr(node *sitter.Node) *ast.RangeExpr {
	startNode := node.ChildByFieldName("start")
	if startNode == nil {
		c.errors = append(c.errors, fmt.Errorf("range expression must have a start"))
		return nil
	}
	endOperatorNode := node.ChildByFieldName("end_operator")
	if endOperatorNode == nil {
		c.errors = append(c.errors, fmt.Errorf("range expression must have an end operator"))
		return nil
	}
	endNode := node.ChildByFieldName("end")
	if endNode == nil {
		c.errors = append(c.errors, fmt.Errorf("range expression must have an end"))
		return nil
	}
	stepNode := node.ChildByFieldName("step")
	step := ast.Expression(nil)
	if stepNode != nil {
		step = c.collectExpression(stepNode)
	}
	return &ast.RangeExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: c.nodeLocation(node)}},
		Start:       c.collectExpression(startNode),
		EndOperator: c.nodeText(endOperatorNode),
		End:         c.collectExpression(endNode),
		Step:        step,
	}
}
