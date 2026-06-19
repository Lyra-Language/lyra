package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/statements"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectExpressionStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ExpressionStmt {
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

func CollectExpression(node *sitter.Node, ctx *collector_ctx.Ctx) ast.Expression {
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
	case "char_literal":
		return collectCharacterLiteralExpr(node, ctx, loc)
	case "regex_literal":
		return collectRegexLiteralExpr(node, ctx, loc)
	case "string_literal":
		return collectStringLiteralExpr(node, ctx, loc)
	case "array_literal":
		return collectArrayLiteralExpr(node, ctx, loc)
	case "array_repeat_init":
		return collectArrayRepeatInitExpr(node, ctx, loc)
	case "array_comp_expr":
		return collectArrayCompExpr(node, ctx, loc)
	case "if_block_expr":
		return collectIfExpr(node, ctx, loc)
	case "match_expr":
		return CollectMatchExpression(node, ctx, loc)
	case "block", "for_body", "for_in_body":
		return CollectBlockExpr(node, ctx, loc)
	case "identifier":
		return CollectIdentifierExpr(node, false, loc, ctx)
	case "const_identifier":
		return CollectIdentifierExpr(node, true, loc, ctx)
	case "tuple_literal":
		return collectTupleLiteralExpr(node, ctx, loc)
	case "user_defined_type_name":
		return collectNullaryConstructorExpr(node, ctx, loc)
	case "named_struct_literal":
		return collectNamedStructLiteralExpr(node, ctx, loc)
	case "anonymous_struct_literal":
		return collectAnonymousStructLiteralExpr(node, ctx, loc)
	case "spread_expr":
		return collectSpreadExpr(node, ctx, loc)
	case "boolean_expr", "for_condition_expr":
		opNode := node.ChildByFieldName("operator")
		if opNode != nil && opNode.Kind() == "not" {
			return collectNotBooleanExpr(node, ctx, loc)
		}
		return collectBinaryBooleanExpr(node, ctx, loc)
	case "binary_expr":
		return collectMathBinaryExpr(node, ctx, loc)
	case "string_concat_expr":
		return collectStringConcatExpr(node, ctx, loc)
	case "call_expr":
		return collectFunctionCallExpr(node, ctx, loc)
	case "member_expr":
		return collectMemberExpr(node, ctx, loc, false)
	case "optional_member_expr":
		return collectMemberExpr(node, ctx, loc, true)
	case "index_expr":
		return collectIndexExpr(node, ctx, loc, false)
	case "optional_index_expr":
		return collectIndexExpr(node, ctx, loc, true)
	case "try_expr":
		return collectTryExpr(node, ctx, loc)
	case "range_expr":
		return collectRangeExpr(node, ctx, loc)
	case "null_coalescing_expr":
		return collectNullCoalescingExpr(node, ctx, loc)
	case "lambda_expr":
		return CollectLambdaExpr(node, ctx, loc)
	case "await_expr":
		return collectAwaitExpr(node, ctx, loc)
	case "sizeof_expr":
		return collectSizeofExpr(node, ctx, loc)
	case "compose_expr":
		return collectComposeExpr(node, ctx, loc)
	case "yield_expr":
		return collectYieldExpr(node, ctx, loc)
	case "yield_from_expr":
		return collectYieldFromExpr(node, ctx, loc)
	case "unsafe_block":
		return collectUnsafeBlockExpr(node, ctx, loc)
	case "given_expr":
		return collectGivenExpr(node, ctx, loc)
	case "negation":
		return collectNegationExpr(node, ctx, loc)
	case "address_of_expr":
		return collectAddressOfExpr(node, ctx, loc)
	case "deref_expr":
		return collectDerefExpr(node, ctx, loc)
	case "compound_assignment":
		return collectCompoundAssignmentExpr(node, ctx, loc)
	case "for_loop":
		return statements.CollectForLoopExpr(node, ctx)
	case "for_in_loop":
		return statements.CollectForInLoopExpr(node, ctx)
	}

	// For wrapper nodes (e.g. parenthesized), recurse into the first named child.
	if node.NamedChildCount() >= 1 {
		return CollectExpression(node.NamedChild(0), ctx)
	}
	return nil
}
