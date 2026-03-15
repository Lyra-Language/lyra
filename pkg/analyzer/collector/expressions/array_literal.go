package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectArrayLiteralExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.ArrayLiteralExpr {
	elements := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			elements = append(elements, CollectExpression(child, ctx))
		}
	}
	return &ast.ArrayLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    nil,
		},
		Elements: elements,
	}
}
