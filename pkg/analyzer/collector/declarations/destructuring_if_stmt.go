package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringIfStatement(node *sitter.Node, ctx *collctx.Ctx) *ast.IfDestructuringStmt {
	destructuringStatement := CollectDestructuringDeclaration(node.ChildByFieldName("destructuring_declaration"), ctx)
	thenBlock := expressions.CollectBlockExpr(node.ChildByFieldName("then_block"), ctx)
	var elseBlock *ast.BlockExpr
	if elseNode := node.ChildByFieldName("else_block"); elseNode != nil {
		elseBlock = expressions.CollectBlockExpr(elseNode, ctx)
	}
	return &ast.IfDestructuringStmt{
		DestructuringStatement: *destructuringStatement,
		Then:                   *thenBlock,
		Else:                   elseBlock,
	}
}
