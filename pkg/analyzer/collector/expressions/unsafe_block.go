package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectUnsafeBlockExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.UnsafeBlockExpr {
	bodyNode := cst.Field(node, "body")
	if bodyNode == nil {
		ctx.AddError(node, diag.SeverityError, "unsafe_block: missing body")
		return nil
	}
	body := CollectBlockExpr(bodyNode, ctx, ctx.NodeLocation(bodyNode))
	return &ast.UnsafeBlockExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Body:     body,
	}
}
