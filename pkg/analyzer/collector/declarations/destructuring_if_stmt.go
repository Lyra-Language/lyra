package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringIfStatement(node *sitter.Node, ctx *collctx.Ctx) *ast.IfDestructuringStmt {
	destructuringStatement := CollectDestructuringDeclaration(node.ChildByFieldName("destructuring_declaration"), ctx)
	thenNode := node.ChildByFieldName("then_block")
	if thenNode == nil {
		ctx.AddError(node, collctx.SeverityError, "CollectDestructuringIfStatement: then block node is nil")
		return nil
	}
	thenBlock := expressions.CollectBlockExpr(thenNode, ctx, ctx.NodeLocation(thenNode))
	var elseBlock *ast.BlockExpr
	if elseNode := node.ChildByFieldName("else_block"); elseNode != nil {
		elseBlock = expressions.CollectBlockExpr(elseNode, ctx, ctx.NodeLocation(elseNode))
	}
	return &ast.IfDestructuringStmt{
		DestructuringStatement: *destructuringStatement,
		Then:                   *thenBlock,
		Else:                   elseBlock,
	}
}
