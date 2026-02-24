package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectTupleLiteralExpr(node *sitter.Node) *ast.TupleLiteralExpr {
	elements := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			elements = append(elements, c.collectExpression(child))
		}
	}
	tupleNameNode := node.ChildByFieldName("tuple_name")
	tupleName := "?"
	if tupleNameNode != nil {
		tupleName = c.nodeText(tupleNameNode)
	}
	return &ast.TupleLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
			Type:    types.TupleType{Name: tupleName, Elements: make([]types.Type, len(elements))},
		},
		Name:     tupleName,
		Elements: elements,
	}
}
