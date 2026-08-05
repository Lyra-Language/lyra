package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectForLoopExpr(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ForLoopExpr {
	loopScope := ctx.PushLoopScope()
	defer ctx.PopScope()

	labelNode := cst.Field(node, "label")
	label := ""
	if labelNode != nil {
		label = ctx.NodeText(labelNode)
	}

	forConditionNode := cst.Field(node, "for_condition")
	var initExpr *ast.VarDeclStmt
	var conditionExpr *ast.Expression
	var postExpr *ast.Expression
	if forConditionNode != nil {
		conditionExprNode := cst.Field(forConditionNode, "condition_expr")
		if conditionExprNode == nil {
			ctx.AddError(forConditionNode, diag.SeverityError, "Expected for loop condition expression, got %s", forConditionNode.Kind())
			return nil
		}
		maybeConditionExpr := ctx.CollectExpr(conditionExprNode)
		conditionExpr = &maybeConditionExpr

		initExprNode := cst.Field(forConditionNode, "initial_expr")
		if initExprNode != nil {
			stmt := ctx.CollectStatement(initExprNode)
			if varDecl, ok := stmt.(*ast.VarDeclStmt); ok {
				initExpr = varDecl
			} else {
				ctx.AddError(initExprNode, diag.SeverityError, "Expected variable declaration in for loop initializer, got %s", initExprNode.Kind())
			}
		}

		postExprNode := cst.Field(forConditionNode, "post_expr")
		if postExprNode != nil {
			expr := ctx.CollectExpr(postExprNode)
			postExpr = &expr
		}
	}

	bodyNode := cst.Field(node, "for_body")
	if bodyNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected for loop body")
		return nil
	}
	body := ctx.CollectExpr(bodyNode)
	bodyBlockPtr, ok := body.(*ast.BlockExpr)
	if !ok {
		ctx.AddError(bodyNode, diag.SeverityError, "Expected block expression for for loop body")
		return nil
	}

	loop := &ast.ForLoopExpr{
		Label:     label,
		Init:      initExpr,
		Condition: conditionExpr,
		Post:      postExpr,
		Body:      bodyBlockPtr,
	}
	ctx.RecordScope(loop, loopScope)
	return loop
}
