package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectCompoundAssignmentExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	leftNode := node.ChildByFieldName("left")
	opNode := node.ChildByFieldName("operator")
	rightNode := node.ChildByFieldName("right")
	if leftNode == nil || opNode == nil || rightNode == nil {
		ctx.AddError(node, diag.SeverityError, "Invalid compound assignment expression. Must have left, operator, and right operands")
		return nil
	}
	left := CollectExpression(leftNode, ctx)
	right := CollectExpression(rightNode, ctx)

	if left == nil {
		return nil
	}
	leftIdent, ok := left.(*ast.IdentifierExpr)
	if !ok {
		ctx.AddError(leftNode, diag.SeverityError, "Left side of compound assignment must be an identifier")
		return nil
	}

	var operator ast.MathAssignOp
	switch opNode.Kind() {
	case "add_assign_operator":
		operator = ast.MathAssignOpAdd
	case "sub_assign_operator":
		operator = ast.MathAssignOpSub
	case "mul_assign_operator":
		operator = ast.MathAssignOpMul
	case "div_assign_operator":
		operator = ast.MathAssignOpDiv
	case "mod_assign_operator":
		operator = ast.MathAssignOpMod
	case "remainder_assign_operator":
		operator = ast.MathAssignOpRemainder
	default:
		ctx.AddError(opNode, diag.SeverityError, "Unknown compound assignment operator: %s", opNode.Kind())
		return nil
	}
	return &ast.MathAssignOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     *leftIdent,
		Operator: operator,
		Right:    right,
	}
}
