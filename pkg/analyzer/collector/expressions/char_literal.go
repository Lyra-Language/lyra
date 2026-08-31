package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectCharacterLiteralExpr builds a rune literal, or reports why it could not and hands
// back a placeholder.
//
// **Both error paths return a node rather than nil, which is hazard 3.** Returning a `nil`
// `*ast.CharacterLiteralExpr` here becomes a *non-nil* interface at the call site holding a
// nil pointer, and the typechecker's first `GetLocation` on it segfaults. Nothing had
// noticed, because neither path was reachable: the grammar's escape set made an illegal
// escape fail to *parse*, and the token admits exactly one character between the quotes.
// Broadening the escape token (08/30) made the first path live, and it crashed the compiler
// on `'\q'` until this was written.
//
// The placeholder is also what keeps the diagnostic to one line. A literal that collects to
// nothing leaves its declaration uninitialized, so `let a: rune = '\q'` reported the escape,
// then that `a` was unused, then that `a` must be initialized — three errors for one typo.
// A node the later passes can type as a rune ends the report at the mistake.
func collectCharacterLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.CharacterLiteralExpr {
	raw := ctx.NodeText(node)
	// strip surrounding single quotes
	inner := raw[1 : len(raw)-1]
	content, err := unescapeStringContent(inner)
	if err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to parse character literal: %v", err)
		return placeholderChar(loc)
	}
	runes := []rune(content)
	if len(runes) != 1 {
		ctx.AddError(node, diag.SeverityError, "character literal must contain exactly one character")
		return placeholderChar(loc)
	}
	return &ast.CharacterLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    runes[0],
	}
}

// placeholderChar is the node a rejected literal stands in as: NUL, which is a rune like
// any other. Its value is never reached — a diagnostic has already been recorded, so the
// compile fails before anything runs — and it exists only so the passes after the collector
// have a well-formed node to walk.
func placeholderChar(loc ast.Location) *ast.CharacterLiteralExpr {
	return &ast.CharacterLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    0,
	}
}
