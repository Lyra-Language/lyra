package collector

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectStructLiteralExpr(node *sitter.Node) *ast.StructInstance {
	nameNode := node.ChildByFieldName("struct_name")
	name := "?"
	if nameNode != nil {
		name = c.nodeText(nameNode)
	}
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = c.collectGenericArgs(genericArgumentsNode)
	}
	structBodyNode := node.ChildByFieldName("struct_body")
	fmt.Println("structBodyNode.Kind():", structBodyNode.Kind())
	fields := c.collectStructInstanceFields(structBodyNode)
	return &ast.StructInstance{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
		},
		Name:        name,
		GenericArgs: genericArguments,
		Fields:      fields,
	}
}

func (c *Collector) collectStructInstanceFields(node *sitter.Node) []ast.StructField {
	fields := make([]ast.StructField, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "struct_shorthand" {
			fields = append(fields, c.collectStructInstanceShorthandField(child))
		} else {
			fields = append(fields, c.collectStructInstanceField(child))
		}
	}
	return fields
}

func (c *Collector) collectStructInstanceShorthandField(node *sitter.Node) ast.StructField {
	value := c.collectExpression(node.ChildByFieldName("field_value"))
	return ast.StructField{
		Name:  "",
		Value: value,
	}
}

func (c *Collector) collectStructInstanceField(node *sitter.Node) ast.StructField {
	name := c.nodeText(node.ChildByFieldName("field_name"))
	value := c.collectExpression(node.ChildByFieldName("field_value"))
	return ast.StructField{
		Name:  name,
		Value: value,
	}
}
