package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectNullaryConstructorExpr handles a bare `user_defined_type_name` used as
// an expression value — a nullary data constructor like `None` or `Red`. The
// owning data type is resolved later from the name, so Value is left nil.
func collectNullaryConstructorExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.DataConstructorExpr {
	return &ast.DataConstructorExpr{
		ExprBase:    ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Constructor: ctx.NodeText(node),
		Value:       nil,
	}
}

// collectAppliedConstructorExpr handles juxtaposition application —
// `Some 42`, `Err -1`, `Some x` — and deliberately produces **the same node the
// parenthesized spelling produces**: a named `TupleLiteralExpr`, which the
// typechecker already resolves to its owning data type.
//
// That is the whole implementation strategy. `Some 42` and `Some(42)` are one
// language construct with two spellings, so the difference is erased here rather
// than propagated: the typechecker, purity, ownership, exhaustiveness and the
// backend never learn that juxtaposition exists. The alternative — a
// DataConstructorExpr carrying a Value — would have needed a case in each, and
// the backend errors on exactly that shape today ("non-nullary
// DataConstructorExpr not expected here").
//
// The operand is one value, never a curried list: a constructor's positional
// payload is already a single anonymous tuple internally (`Rect(f64, f64)` → one
// TupleType param), which is why `Rect(3, 4)` keeps its named-tuple parse and
// reads as "Rect applied to the tuple (3, 4)". So a juxtaposed operand is always
// exactly one element here.
func collectAppliedConstructorExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	ctorNode, ok := ctx.MustField(node, "constructor")
	if !ok {
		return &ast.DataConstructorExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}}
	}
	valueNode, ok := ctx.MustField(node, "value")
	if !ok {
		// Never return a nil expression into the AST (hazard 3) — a typed nil
		// slips past `expr == nil` and crashes a later pass. A constructor with
		// no operand is a nullary one as far as everything downstream cares.
		return collectNullaryConstructorExpr(ctorNode, ctx, loc)
	}
	return &ast.TupleLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Name:     ctx.NodeText(ctorNode),
		Elements: []ast.Expression{CollectExpression(valueNode, ctx)},
	}
}
