package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringElseStatement(node *sitter.Node, ctx *collctx.Ctx) *ast.ElseDestructuringStmt {
	destructuringStatement := CollectDestructuringDeclaration(node.ChildByFieldName("destructuring_declaration"), ctx)
	elseBlock := expressions.CollectBlockExpr(node.ChildByFieldName("else_block"), ctx)
	return &ast.ElseDestructuringStmt{
		DestructuringStatement: *destructuringStatement,
		Else:                   *elseBlock, // guaranteed to be non-nil
	}
}
