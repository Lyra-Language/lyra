package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectWithStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.WithStmt {
	name := ""
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		name = ctx.NodeText(nameNode)
	}

	arenaNode := node.ChildByFieldName("arena")
	if arenaNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "Expected arena expression in with statement")
		return nil
	}
	arena := ctx.CollectExpr(arenaNode)

	bodyNode := node.ChildByFieldName("body")
	if bodyNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "Expected body block in with statement")
		return nil
	}
	body := ctx.CollectExpr(bodyNode)
	bodyBlock, ok := body.(*ast.BlockExpr)
	if !ok {
		ctx.AddError(bodyNode, collector_ctx.SeverityError, "Expected block expression for with statement body")
		return nil
	}

	return &ast.WithStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:    name,
		Arena:   arena,
		Body:    *bodyBlock,
	}
}
