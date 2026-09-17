package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectCompoundAssignmentExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	leftNode := cst.Field(node, "left")
	opNode := cst.Field(node, "operator")
	rightNode := cst.Field(node, "right")
	if leftNode == nil || opNode == nil || rightNode == nil {
		ctx.AddError(node, diag.SeverityError, "Invalid compound assignment expression. Must have left, operator, and right operands")
		return nil
	}
	left := CollectExpression(leftNode, ctx)
	right := CollectExpression(rightNode, ctx)

	if left == nil {
		return nil
	}
	// Any assignable place, matching what `=` accepts: a binding, or the member/index
	// path an LValueAssignmentStmt targets. Restricting this to a bare identifier made
	// `counts[i].n += 1` a syntax-level refusal while `counts[i].n = counts[i].n + 1`
	// on the identical place compiled — the compound form is the shorter spelling of
	// that statement, so it has no business accepting less.
	switch left.(type) {
	case *ast.IdentifierExpr, *ast.MemberExpr, *ast.IndexExpr, *ast.TupleIndexExpr:
	default:
		ctx.AddError(leftNode, diag.SeverityError,
			"left side of compound assignment must be a binding, field or element")
		// The operand stands in, never nil (hazard 3): the grammar accepts any expression
		// on the left, so this refusal is live wherever an expression is — `f(1 += 2)`, a
		// struct field value. A placeholder MathAssignOpExpr would be refused again as an
		// unwritable place and, in value position, as void; the operand is inert.
		return left
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
	case "rem_assign_operator":
		operator = ast.MathAssignOpRem
	case "rem_floor_assign_operator":
		operator = ast.MathAssignOpRemFloor
	case "bitand_assign_operator":
		operator = ast.MathAssignOpBitAnd
	case "bitor_assign_operator":
		operator = ast.MathAssignOpBitOr
	case "bitxor_assign_operator":
		operator = ast.MathAssignOpBitXor
	case "shl_assign_operator":
		operator = ast.MathAssignOpShl
	case "shr_assign_operator":
		operator = ast.MathAssignOpShr
	default:
		ctx.AddError(opNode, diag.SeverityError, "Unknown compound assignment operator: %s", opNode.Kind())
		return nil
	}
	return &ast.MathAssignOpExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Left:     left,
		Operator: operator,
		Right:    right,
	}
}
