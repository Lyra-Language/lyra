package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectSpreadExpr builds `...expr`. The operand is a full postfix expression — a call, a
// member access, an index — not the bare name it was until 08/27, so it is collected rather
// than read as text.
func collectSpreadExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	valueNode, ok := ctx.MustField(node, "value")
	if !ok {
		return nil
	}
	return &ast.SpreadExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
		},
		Value: CollectExpression(valueNode, ctx),
	}
}
