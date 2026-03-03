package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectBooleanBinaryExpr(node *sitter.Node, loc ast.Location) *ast.BooleanBinaryOpExpr {
	return &ast.BooleanBinaryOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     c.collectExpression(node.ChildByFieldName("left")),
		Operator: ast.BooleanBinaryOp(c.nodeText(node.ChildByFieldName("operator"))),
		Right:    c.collectExpression(node.ChildByFieldName("right")),
	}
}
