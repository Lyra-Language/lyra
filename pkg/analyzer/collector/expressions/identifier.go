package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectIdentifierExpr(node *sitter.Node, isConst bool, loc ast.Location, ctx *collctx.Ctx) *ast.IdentifierExpr {
	return &ast.IdentifierExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Name:     ctx.NodeText(node),
		IsConst:  isConst,
	}
}
