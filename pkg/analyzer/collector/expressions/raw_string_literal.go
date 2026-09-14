package expressions

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectRawStringLiteralExpr lowers “ `…` “, “ #`…`# “, “ ##`…`## “ to a plain
// string literal. The value is every byte between the delimiters — newlines, `\`,
// `${` and all — so it is sliced from the source by the opener's `#` count rather
// than read from the `raw_string_content` node, which is absent for an empty string.
func collectRawStringLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	text := ctx.NodeText(node)
	hashes := len(text) - len(strings.TrimLeft(text, "#"))
	return &ast.StringLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    text[hashes+1 : len(text)-hashes-1],
	}
}
