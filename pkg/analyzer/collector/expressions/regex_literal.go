package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectRegexLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.RegexLiteralExpr {
	raw := ctx.NodeText(node) // e.g. r"[0-9]+"
	if len(raw) < 3 || raw[:2] != `r"` || raw[len(raw)-1] != '"' {
		ctx.AddError(node, diag.SeverityError, "collectRegexLiteralExpr: malformed regex literal %q", raw)
		return nil
	}
	return &ast.RegexLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Pattern:  raw[2 : len(raw)-1],
	}
}
