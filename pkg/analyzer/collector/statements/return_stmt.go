package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectReturnStatement(node *sitter.Node, ctx *collctx.Ctx) *ast.ReturnStmt {
	valueNode := node.ChildByFieldName("value")
	var value ast.Expression
	if valueNode != nil {
		value = ctx.CollectExpr(valueNode)
	}

	return &ast.ReturnStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Value:   value,
	}
}