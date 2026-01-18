package collector

import (
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
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
	case "integer", "integer_literal":
		value, _ := strconv.ParseInt(c.nodeText(node), 10, 64)
		return &ast.IntegerLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    value,
		}

	case "float", "float_literal":
		value, _ := strconv.ParseFloat(c.nodeText(node), 64)
		return &ast.FloatLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    value,
		}

	case "string", "string_literal":
		return &ast.StringLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    c.nodeText(node),
		}

	case "boolean", "boolean_literal":
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
