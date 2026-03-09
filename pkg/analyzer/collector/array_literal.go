package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectArrayLiteralExpr(node *sitter.Node) *ast.ArrayLiteralExpr {
	elements := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			elements = append(elements, c.collectExpression(child))
		}
	}
	return &ast.ArrayLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
			Type:    nil, // Type will be resolved during type checking
		},
		Elements: elements,
	}
}
