package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectExpressionStatement(node *sitter.Node) *ast.ExpressionStmt {
	if node.NamedChildCount() < 1 {
		return nil
	}
	expr := c.collectExpression(node.NamedChild(0))
	if expr != nil {
		return &ast.ExpressionStmt{
			AstBase:    ast.AstBase{Location: c.nodeLocation(node)},
			Expression: expr,
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
		return c.collectStringLiteralExpr(node, loc)

	case "array_literal":
		return c.collectArrayLiteralExpr(node)

	case "array_repeat_init":
		return c.collectArrayRepeatInitExpr(node)

	case "array_comp_expr":
		return c.collectArrayCompExpr(node)

	case "boolean_literal":
		return c.collectBooleanLiteralExpr(node, loc)

	case "identifier":
		return c.collectIdentifierExpr(node, false, loc)

	case "const_identifier":
		return c.collectIdentifierExpr(node, true, loc)

	case "tuple_literal":
		return c.collectTupleLiteralExpr(node)

	case "struct_literal":
		return c.collectStructLiteralExpr(node)

	case "boolean_expr":
		return c.collectBooleanBinaryExpr(node, loc)

	case "addition", "subtraction", "multiplication", "division":
		return c.collectMathBinaryExpr(node, loc)

	case "call_expression":
		return c.collectFunctionCallExpr(node, loc)

	case "range_expression":
		return c.collectRangeExpr(node)
	}

	// For wrapper nodes (e.g. parenthesized), recurse into the first named child
	if node.NamedChildCount() >= 1 {
		return c.collectExpression(node.NamedChild(0))
	}
	return nil
}
