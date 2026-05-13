package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectRegexLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.RegexLiteralExpr {
	raw := ctx.NodeText(node) // e.g. r/[0-9]+/
	pattern := raw[2 : len(raw)-1]
	return &ast.RegexLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Pattern:  pattern,
	}
}
