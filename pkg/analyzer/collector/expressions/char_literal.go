package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectCharacterLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.CharacterLiteralExpr {
	raw := ctx.NodeText(node)
	// strip surrounding single quotes
	inner := raw[1 : len(raw)-1]
	content, err := unescapeStringContent(inner)
	if err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to parse character literal: %v", err)
		return nil
	}
	runes := []rune(content)
	if len(runes) != 1 {
		ctx.AddError(node, diag.SeverityError, "character literal must contain exactly one character")
		return nil
	}
	return &ast.CharacterLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    runes[0],
	}
}
