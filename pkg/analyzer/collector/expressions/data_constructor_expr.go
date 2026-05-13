package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectDataConstructorExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.DataConstructorExpr {
	constructorNode := node.ChildByFieldName("constructor")
	if constructorNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "data_constructor_expr: missing constructor")
		return nil
	}
	return &ast.DataConstructorExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Constructor: ctx.NodeText(constructorNode),
		Value:       CollectExpression(node.ChildByFieldName("value"), ctx),
	}
}
