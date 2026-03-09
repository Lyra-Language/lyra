package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectMathBinaryExpr(node *sitter.Node, loc ast.Location) *ast.MathBinaryOpExpr {
	return &ast.MathBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     c.collectExpression(node.ChildByFieldName("left")),
		Operator: ast.MathBinaryOp(c.nodeText(node.ChildByFieldName("operator"))),
		Right:    c.collectExpression(node.ChildByFieldName("right")),
	}
}
