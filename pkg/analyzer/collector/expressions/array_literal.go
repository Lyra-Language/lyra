package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectArrayLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.ArrayLiteralExpr {
	elements := []ast.Expression{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			elements = append(elements, CollectExpression(child, ctx))
		}
	}
	return &ast.ArrayLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    nil,
		},
		Elements: elements,
	}
}
