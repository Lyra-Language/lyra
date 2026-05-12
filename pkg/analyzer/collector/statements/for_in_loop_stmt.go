package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectForInLoopStmt(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ForInLoopStmt {
	var label, key, value string
	var iterable ast.Expression
	var body *ast.BlockExpr

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "label":
			label = ctx.NodeText(child)
		case "for_in_condition":
			key, value, iterable = collectForInCondition(child, ctx)
		case "for_in_body":
			expr := ctx.CollectExpr(child)
			if b, ok := expr.(*ast.BlockExpr); ok {
				body = b
			} else {
				ctx.AddError(child, collector_ctx.SeverityError, "expected block expression for for/in loop body")
			}
		}
	}

	if key == "" {
		ctx.AddError(node, collector_ctx.SeverityError, "for/in loop missing loop variable")
		return nil
	}
	if iterable == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "for/in loop missing iterable expression")
		return nil
	}
	if body == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "for/in loop missing body")
		return nil
	}

	return &ast.ForInLoopStmt{
		Label:    label,
		Key:      key,
		Value:    value,
		Iterable: iterable,
		Body:     *body,
	}
}

func collectForInCondition(node *sitter.Node, ctx *collector_ctx.Ctx) (key, value string, iterable ast.Expression) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "for_variable_or_key":
			key = ctx.NodeText(child)
		case "for_index_or_value":
			value = ctx.NodeText(child)
		default:
			if child.IsNamed() {
				iterable = ctx.CollectExpr(child)
			}
		}
	}
	return
}
