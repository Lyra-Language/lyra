package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectDataConstructorExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.DataConstructorExpr {
	constructorNode := node.ChildByFieldName("constructor")
	if constructorNode == nil {
		ctx.AddError(node, diag.SeverityError, "data_constructor_expr: missing constructor")
		return nil
	}
	return &ast.DataConstructorExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Constructor: ctx.NodeText(constructorNode),
		Value:       CollectExpression(node.ChildByFieldName("value"), ctx),
	}
}

// collectNullaryConstructorExpr handles a bare `user_defined_type_name` used as
// an expression value — a nullary data constructor like `None` or `Red`. (The
// applied form `Some x` parses as `data_constructor_expr`; `Some(x)` parses as a
// `tuple_literal`.) The owning data type is resolved later by the typechecker
// from the constructor name, so Value is left nil.
func collectNullaryConstructorExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.DataConstructorExpr {
	return &ast.DataConstructorExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Constructor: ctx.NodeText(node),
		Value:       nil,
	}
}
