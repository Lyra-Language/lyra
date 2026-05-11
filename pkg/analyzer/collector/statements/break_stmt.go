package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectBreakStatement(node *sitter.Node, ctx *collctx.Ctx) *ast.BreakStmt {
	labelNode := node.ChildByFieldName("label")
	label := ""
	if labelNode != nil {
		label = ctx.NodeText(labelNode)
	}

	return &ast.BreakStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Label:   label,
	}
}
