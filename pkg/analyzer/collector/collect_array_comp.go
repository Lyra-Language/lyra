package collector

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectArrayCompExpr(node *sitter.Node) *ast.ArrayCompExpr {
	result_node := node.ChildByFieldName("result_expression")
	if result_node == nil {
		c.errors = append(c.errors, fmt.Errorf("array comprehension must have a result"))
		return nil
	}
	return &ast.ArrayCompExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: c.nodeLocation(node)}},
		Generators: c.collectGenerators(node),
		Guards:     c.collectGuards(node),
		Result:     c.collectResultExpression(result_node),
	}
}

func (c *Collector) collectGenerators(node *sitter.Node) []ast.Generator {
	generators := make([]ast.Generator, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "generator" {
			generators = append(generators, c.collectGenerator(child))
		}
	}
	return generators
}

func (c *Collector) collectGenerator(node *sitter.Node) ast.Generator {
	value_node := node.ChildByFieldName("value")
	if value_node == nil {
		c.errors = append(c.errors, fmt.Errorf("generator must have a value"))
		return ast.Generator{}
	}
	generator_identifier_node := node.ChildByFieldName("identifier")
	if generator_identifier_node == nil {
		c.errors = append(c.errors, fmt.Errorf("generator must have an identifier"))
		return ast.Generator{}
	}
	return ast.Generator{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: c.nodeLocation(node)}},
		Value:      c.collectExpression(value_node),
		Identifier: c.nodeText(generator_identifier_node),
	}
}

func (c *Collector) collectGuards(node *sitter.Node) []ast.Expression {
	if node == nil {
		return nil
	}
	guards := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "comprehension_guard" {
			guards = append(guards, c.collectExpression(child))
		}
	}
	return guards
}

func (c *Collector) collectResultExpression(result_node *sitter.Node) ast.Expression {
	if result_node == nil {
		c.errors = append(c.errors, fmt.Errorf("array comprehension must have a result"))
		return nil
	}
	return c.collectExpression(result_node)
}
