package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectFunctionCallExpr(node *sitter.Node, loc ast.Location) *ast.FunctionCallExpr {
	return &ast.FunctionCallExpr{
		ExprBase:         ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Function:         c.collectExpression(node.ChildByFieldName("function")),
		GenericArguments: c.collectGenericArguments(node),
		Arguments:        c.collectArgumentList(node.ChildByFieldName("arguments")),
	}
}

func (c *Collector) collectGenericArguments(node *sitter.Node) []types.Type {
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	if genericArgumentsNode == nil {
		return nil
	}
	genericArguments := make([]types.Type, 0)
	for i := uint(0); i < genericArgumentsNode.ChildCount(); i++ {
		child := genericArgumentsNode.Child(i)
		if child.IsNamed() {
			genericArguments = append(genericArguments, c.parseType(child))
		}
	}
	return genericArguments
}

func (c *Collector) collectArgumentList(node *sitter.Node) ast.ArgumentList {
	arguments := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			arguments = append(arguments, c.collectExpression(child))
		}
	}
	return ast.ArgumentList{Arguments: arguments}
}
