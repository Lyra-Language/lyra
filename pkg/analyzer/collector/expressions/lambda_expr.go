package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectLambdaExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.LambdaExpr {
	if node.ChildCount() < 1 {
		ctx.AddError(node, collctx.SeverityError, "collectLambdaExpr: node has no children")
		return nil
	}
	isAsync := node.ChildByFieldName("is_async") != nil
	isPure := node.ChildByFieldName("is_pure") != nil
	return &ast.LambdaExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		IsAsync: isAsync,
		IsPure: isPure,
		FunctionClause: ctx.CollectFunctionClause(node.Child(0)),
	}
}