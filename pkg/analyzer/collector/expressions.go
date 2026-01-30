package collector

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectExpressionStatement(node *sitter.Node) *ast.ExpressionStmt {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			expr := c.collectExpression(child)
			if expr != nil {
				return &ast.ExpressionStmt{
					AstBase:    ast.AstBase{Location: c.nodeLocation(node)},
					Expression: expr,
				}
			}
		}
	}
	return nil
}

func (c *Collector) collectExpression(node *sitter.Node) ast.Expression {
	if node == nil {
		return nil
	}
	loc := c.nodeLocation(node)

	switch node.Kind() {
	case "integer_literal":
		return c.collectIntegerLiteralExpr(node)

	case "float_literal":
		return c.collectFloatLiteralExpr(node)

	case "string_literal":
		return &ast.StringLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    c.nodeText(node),
		}

	case "boolean_literal":
		value := c.nodeText(node) == "true"
		return &ast.BooleanLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    value,
		}

	case "identifier":
		return &ast.IdentifierExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Name:     c.nodeText(node),
		}

	case "boolean_expr":
		return &ast.BooleanBinaryOpExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Left:     c.collectExpression(node.ChildByFieldName("left")),
			Operator: ast.BooleanBinaryOp(c.nodeText(node.ChildByFieldName("operator"))),
			Right:    c.collectExpression(node.ChildByFieldName("right")),
		}
	case "addition", "subtraction", "multiplication", "division":
		return &ast.MathBinaryOpExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Left:     c.collectExpression(node.ChildByFieldName("left")),
			Operator: ast.MathBinaryOp(c.nodeText(node.ChildByFieldName("operator"))),
			Right:    c.collectExpression(node.ChildByFieldName("right")),
		}
	case "call_expression":
		return &ast.FunctionCallExpr{
			ExprBase:         ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Function:         c.collectExpression(node.ChildByFieldName("function")),
			GenericArguments: c.collectGenericArguments(node),
			Arguments:        c.collectArgumentList(node.ChildByFieldName("arguments")),
		}
	}

	// For wrapper nodes, recurse into the first named child
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return c.collectExpression(child)
		}
	}

	return nil
}

func (c *Collector) collectIntegerLiteralExpr(node *sitter.Node) *ast.IntegerLiteralExpr {
	loc := c.nodeLocation(node)
	base := ast.IntegerBase10
	value := int64(0)
	err := error(nil)

	for i := uint(0); i < node.ChildCount(); i++ {
		valueString := c.nodeText(node.Child(i))
		valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
		value, err = strconv.ParseInt(valueStringWithoutUnderscores, 0, 64)
		if err != nil {
			c.errors = append(c.errors, fmt.Errorf("failed to parse integer literal: %w", err))
			return nil
		}
		switch node.Child(i).Kind() {
		case "binary_int":
			base = ast.IntegerBase2
		case "octal_int":
			base = ast.IntegerBase8
		case "decimal_int":
			base = ast.IntegerBase10
		case "hexadecimal_int":
			base = ast.IntegerBase16
		}
	}
	return &ast.IntegerLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: "Int"},
		},
		Value: value,
		Base:  base,
	}
}

func (c *Collector) collectFloatLiteralExpr(node *sitter.Node) *ast.FloatLiteralExpr {
	loc := c.nodeLocation(node)
	valueString := c.nodeText(node)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	value, err := strconv.ParseFloat(valueStringWithoutUnderscores, 64)
	if err != nil {
		c.errors = append(c.errors, fmt.Errorf("failed to parse float literal: %w", err))
		return nil
	}
	return &ast.FloatLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: "Float"},
		},
		Value: value,
	}
}

func (c *Collector) collectGenericArguments(node *sitter.Node) []types.Type {
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	if genericArgumentsNode == nil {
		return nil
	}
	genericArguments := make([]types.Type, 0)
	for i := uint(0); i < genericArgumentsNode.ChildCount(); i++ {
		child := node.Child(i)
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
