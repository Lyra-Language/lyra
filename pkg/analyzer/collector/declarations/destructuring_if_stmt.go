package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringIfStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.IfDestructuringStmt {
	declNode := cst.Field(node, "declaration")
	destructuringStatement := CollectDestructuringDeclaration(declNode, ctx)
	thenNode := cst.Field(node, "then_block")
	if thenNode == nil {
		ctx.AddError(node, diag.SeverityError, "CollectDestructuringIfStatement: then block node is nil")
		return nil
	}
	if destructuringStatement == nil {
		return nil
	}

	// Allocate stmt now (rather than at the end) so RecordScope below has a
	// stable node identity to key against — the same pointer the typechecker
	// will later look up via enterScope(stmt, ...).
	stmt := &ast.IfDestructuringStmt{
		AstBase:                ast.AstBase{Location: ctx.NodeLocation(node)},
		DestructuringStatement: *destructuringStatement,
	}

	// `if let pat = v { Then } else { Else }` scopes pat's bound names to Then
	// only (mirroring Rust's if-let): push a scope, record it against stmt so
	// the typechecker can re-enter it, register the names into it, collect Then
	// as a nested child of that scope (so it resolves them via the parent
	// chain), then pop before collecting Else — which must not see them, since
	// reaching Else means the pattern failed to match.
	letScope := ctx.PushBlockScope()
	ctx.RecordScope(stmt, letScope)
	registerDestructuredNames(declNode, destructuringStatement.Pattern, destructuringStatement, ctx)
	thenBlock := expressions.CollectBlockExpr(thenNode, ctx, ctx.NodeLocation(thenNode))
	ctx.PopScope()

	var elseBlock *ast.BlockExpr
	if elseNode := cst.Field(node, "else_block"); elseNode != nil {
		elseBlock = expressions.CollectBlockExpr(elseNode, ctx, ctx.NodeLocation(elseNode))
	}

	stmt.Then = thenBlock
	stmt.Else = elseBlock
	return stmt
}
