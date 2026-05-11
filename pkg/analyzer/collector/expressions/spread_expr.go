package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectSpreadExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.SpreadExpr {
	spreadNameNode := node.ChildByFieldName("spread_name")
	if spreadNameNode == nil {
		return nil
	}
	return &ast.SpreadExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
		},
		Name: ctx.NodeText(spreadNameNode),
	}
}
