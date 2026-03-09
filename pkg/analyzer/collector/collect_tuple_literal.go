package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectTupleLiteralExpr(node *sitter.Node) *ast.TupleLiteralExpr {
	tupleNameNode := node.ChildByFieldName("tuple_name")
	tupleName := "?"
	if tupleNameNode != nil {
		tupleName = c.nodeText(tupleNameNode)
	}
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = c.collectGenericArgs(genericArgumentsNode)
	}
	elements := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "tuple_value" {
			tupleValueExprNode := child.Child(0)
			elements = append(elements, c.collectExpression(tupleValueExprNode))
		}
	}
	return &ast.TupleLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
		},
		Name:             tupleName,
		GenericArguments: genericArguments,
		Elements:         elements,
	}
}
