package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectGivenExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.GivenExpr {
	bodyNode := node.ChildByFieldName("body")
	if bodyNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "given_expr: missing body")
		return nil
	}
	bindingsNode := node.ChildByFieldName("bindings")
	if bindingsNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "given_expr: missing bindings")
		return nil
	}
	bindings := []ast.Statement{}
	for i := uint(0); i < bindingsNode.NamedChildCount(); i++ {
		stmt := ctx.CollectStatement(bindingsNode.NamedChild(i))
		if stmt != nil {
			bindings = append(bindings, stmt)
		}
	}
	return &ast.GivenExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Body:     CollectExpression(bodyNode, ctx),
		Bindings: bindings,
	}
}
