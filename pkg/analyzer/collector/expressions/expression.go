package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectExpressionStatement(node *sitter.Node, ctx *collctx.Ctx) *ast.ExpressionStmt {
	if node.NamedChildCount() < 1 {
		return nil
	}
	expr := CollectExpression(node.NamedChild(0), ctx)
	if expr != nil {
		return &ast.ExpressionStmt{
			AstBase:    ast.AstBase{Location: ctx.NodeLocation(node)},
			Expression: expr,
		}
	}
	return nil
}

func CollectExpression(node *sitter.Node, ctx *collctx.Ctx) ast.Expression {
	if node == nil {
		return nil
	}
	loc := ctx.NodeLocation(node)

	switch node.Kind() {
	case "integer_literal":
		return collectIntegerLiteralExpr(node, ctx, loc)
	case "float_literal":
		return collectFloatLiteralExpr(node, ctx, loc)
	case "boolean_literal":
		return collectBooleanLiteralExpr(node, ctx, loc)
	case "string_literal":
		return collectStringLiteralExpr(node, ctx, loc)
	case "array_literal":
		return collectArrayLiteralExpr(node, ctx, loc)
	case "array_repeat_init":
		return collectArrayRepeatInitExpr(node, ctx, loc)
	case "array_comp_expr":
		return collectArrayCompExpr(node, ctx, loc)
	case "if_then_expr":
		return collectIfThenExpr(node, ctx, loc)
	case "if_block_expr":
		return collectIfBlockExpr(node, ctx, loc)
	case "match_expr":
		return CollectMatchExpression(node, ctx, loc)
	case "block":
		return CollectBlockExpr(node, ctx, loc)
	case "identifier":
		return CollectIdentifierExpr(node, false, loc, ctx)
	case "const_identifier":
		return CollectIdentifierExpr(node, true, loc, ctx)
	case "tuple_literal":
		return collectTupleLiteralExpr(node, ctx, loc)
	case "struct_literal":
		return collectStructLiteralExpr(node, ctx, loc)
	case "boolean_expr":
		return collectBooleanBinaryExpr(node, ctx, loc)
	case "binary_expression":
		return collectMathBinaryExpr(node, ctx, loc)
	case "call_expression":
		return collectFunctionCallExpr(node, ctx, loc)
	case "member_expression":
		return collectMemberExpr(node, ctx, loc, false)
	case "optional_member_expression":
		return collectMemberExpr(node, ctx, loc, true)
	case "index_expression":
		return collectIndexExpr(node, ctx, loc, false)
	case "optional_index_expression":
		return collectIndexExpr(node, ctx, loc, true)
	case "try_expression":
		return collectTryExpr(node, ctx, loc)
	case "range_expression":
		return collectRangeExpr(node, ctx, loc)
	case "null_coalescing_expression":
		return collectNullCoalescingExpr(node, ctx, loc)
	case "lambda_expression":
		return collectLambdaExpr(node, ctx, loc)
	}

	// For wrapper nodes (e.g. parenthesized), recurse into the first named child.
	if node.NamedChildCount() >= 1 {
		return CollectExpression(node.NamedChild(0), ctx)
	}
	return nil
}
