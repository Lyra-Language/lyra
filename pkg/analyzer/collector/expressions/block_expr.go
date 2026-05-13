package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectBlockExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.BlockExpr {
	if node == nil {
		return nil
	}
	ctx.PushBlockScope()
	defer ctx.PopScope()
	statements := []ast.Statement{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			statements = append(statements, ctx.CollectStatement(child))
		}
	}
	return &ast.BlockExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    nil,
		},
		Statements: statements,
	}
}
