package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectForLoopStmt(node *sitter.Node, ctx *collctx.Ctx) *ast.ForLoopStmt {
	labelNode := node.ChildByFieldName("label")
	label := ""
	if labelNode != nil {
		label = ctx.NodeText(labelNode)
	}

	forConditionNode := node.ChildByFieldName("for_condition")
	var initExpr *ast.VarDeclStmt
	var conditionExpr *ast.Expression
	var postExpr *ast.Expression
	if forConditionNode != nil {
		conditionExprNode := forConditionNode.ChildByFieldName("condition_expr")
		if conditionExprNode == nil {
			ctx.AddError(forConditionNode, collctx.SeverityError, "Expected for loop condition expression, got %s", forConditionNode.Kind())
			return nil
		}
		maybeConditionExpr := ctx.CollectExpr(conditionExprNode)
		conditionExpr = &maybeConditionExpr

		initExprNode := forConditionNode.ChildByFieldName("initial_expr")
		if initExprNode != nil {
			stmt := ctx.CollectStatement(initExprNode)
			if varDecl, ok := stmt.(*ast.VarDeclStmt); ok {
				initExpr = varDecl
			} else {
				ctx.AddError(initExprNode, collctx.SeverityError, "Expected variable declaration in for loop initializer, got %s", initExprNode.Kind())
			}
		}

		postExprNode := forConditionNode.ChildByFieldName("post_expr")
		if postExprNode != nil {
			expr := ctx.CollectExpr(postExprNode)
			postExpr = &expr
		}
	}

	bodyNode := node.ChildByFieldName("for_body")
	if bodyNode == nil {
		ctx.AddError(node, collctx.SeverityError, "Expected for loop body")
		return nil
	}
	body := ctx.CollectExpr(bodyNode)
	bodyBlockPtr, ok := body.(*ast.BlockExpr)
	if !ok {
		ctx.AddError(bodyNode, collctx.SeverityError, "Expected block expression for for loop body")
		return nil
	}

	return &ast.ForLoopStmt{
		Label:     label,
		Init:      initExpr,
		Condition: conditionExpr,
		Post:      postExpr,
		Body:      *bodyBlockPtr,
	}
}
