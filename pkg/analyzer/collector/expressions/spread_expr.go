package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectSpreadExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.SpreadExpr {
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