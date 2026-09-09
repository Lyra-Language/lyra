package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// `nullptr` carries no text worth keeping — the node kind is the whole of it,
// exactly as a boolean literal's would be if there were only one of them.
func collectNullPtrExpr(_ *sitter.Node, _ *collector_ctx.Ctx, loc ast.Location) *ast.NullPtrExpr {
	return &ast.NullPtrExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
	}
}
