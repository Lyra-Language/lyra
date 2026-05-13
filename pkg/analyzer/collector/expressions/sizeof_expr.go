package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectSizeofExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.SizeofExpr {
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "sizeof_expr: missing type")
		return nil
	}
	return &ast.SizeofExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Type:     ctx.ParseType(typeNode),
	}
}
