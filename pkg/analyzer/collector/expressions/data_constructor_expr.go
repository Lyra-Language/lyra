package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectNullaryConstructorExpr handles a bare `user_defined_type_name` used as
// an expression value — a nullary data constructor like `None` or `Red`. The
// applied form uses call syntax (`Some(x)`), which parses as a `tuple_literal`;
// the typechecker resolves a tuple-literal name that is a data constructor to its
// owning data type. The owning data type for a nullary constructor is likewise
// resolved later from its name, so Value is left nil.
func collectNullaryConstructorExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.DataConstructorExpr {
	return &ast.DataConstructorExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Constructor: ctx.NodeText(node),
		Value:       nil,
	}
}
