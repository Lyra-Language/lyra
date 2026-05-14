package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringElseStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ElseDestructuringStmt {
	if node == nil {
		return nil
	}
	declNode := node.ChildByFieldName("declaration")
	elseNode := node.ChildByFieldName("else_block")
	if declNode == nil || elseNode == nil {
		return nil
	}
	destructuringStatement := CollectDestructuringDeclaration(declNode, ctx)
	elseBlock := expressions.CollectBlockExpr(elseNode, ctx, ctx.NodeLocation(elseNode))
	if destructuringStatement == nil || elseBlock == nil {
		return nil
	}
	return &ast.ElseDestructuringStmt{
		DestructuringStatement: *destructuringStatement,
		Else:                   *elseBlock,
	}
}
