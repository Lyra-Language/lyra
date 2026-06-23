package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringIfStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.IfDestructuringStmt {
	declNode := node.ChildByFieldName("declaration")
	destructuringStatement := CollectDestructuringDeclaration(declNode, ctx)
	thenNode := node.ChildByFieldName("then_block")
	if thenNode == nil {
		ctx.AddError(node, diag.SeverityError, "CollectDestructuringIfStatement: then block node is nil")
		return nil
	}

	// `if let pat = v { Then } else { Else }` scopes pat's bound names to Then
	// only (mirroring Rust's if-let): push a scope, register the names into it,
	// collect Then as a nested child of that scope (so it resolves them via the
	// parent chain), then pop before collecting Else — which must not see them,
	// since reaching Else means the pattern failed to match.
	ctx.PushBlockScope()
	if destructuringStatement != nil {
		registerDestructuredNames(declNode, destructuringStatement.Pattern, destructuringStatement, ctx)
	}
	thenBlock := expressions.CollectBlockExpr(thenNode, ctx, ctx.NodeLocation(thenNode))
	ctx.PopScope()

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
