package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectBlockExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.BlockExpr {
	if node == nil {
		return nil
	}
	statements := make([]ast.Statement, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			statements = append(statements, ctx.CollectStatement(child))
		}
	}
	return &ast.BlockExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
			Type:    nil,
		},
		Statements: statements,
	}
}
