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
	// Collect Else (the diverging branch, taken only when the pattern fails to
	// match) before registering pat's bound names: Else gets its own pushed/popped
	// child scope via CollectBlockExpr, so collecting it first means that scope
	// is already gone by the time the names are registered into the *current*
	// (enclosing) scope below — Else never sees them, matching let-else
	// semantics. Statements after this one, sharing the same enclosing scope,
	// resolve them normally, like a plain `let`.
	elseBlock := expressions.CollectBlockExpr(elseNode, ctx, ctx.NodeLocation(elseNode))
	if destructuringStatement == nil || elseBlock == nil {
		return nil
	}
	registerDestructuredNames(declNode, destructuringStatement.Pattern, destructuringStatement, ctx)
	return &ast.ElseDestructuringStmt{
		DestructuringStatement: *destructuringStatement,
		Else:                   *elseBlock,
	}
}
