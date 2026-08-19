package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectForInLoopExpr(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ForInLoopExpr {
	loopScope := ctx.PushLoopScope()
	defer ctx.PopScope()

	var label, key, value string
	var keyLoc, valueLoc ast.Location
	var iterable ast.Expression
	var body *ast.BlockExpr

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "label":
			label = ctx.NodeText(child)
		case "for_in_condition":
			key, value, keyLoc, valueLoc, iterable = collectForInCondition(child, ctx)
		case "for_in_body":
			expr := ctx.CollectExpr(child)
			if b, ok := expr.(*ast.BlockExpr); ok {
				body = b
			} else {
				ctx.AddError(child, diag.SeverityError, "expected block expression for for/in loop body")
			}
		}
	}

	if key == "" {
		ctx.AddError(node, diag.SeverityError, "for/in loop missing loop variable")
		return nil
	}
	if iterable == nil {
		ctx.AddError(node, diag.SeverityError, "for/in loop missing iterable expression")
		return nil
	}
	if body == nil {
		ctx.AddError(node, diag.SeverityError, "for/in loop missing body")
		return nil
	}

	loop := &ast.ForInLoopExpr{
		// The loop's own span. It carried none until 08/19, which is not the cosmetic
		// gap it looks like: a diagnostic reported against a zero Location prints with no
		// `line:col` *and* escapes the driver's per-file filtering, which keeps a
		// location-less diagnostic on the grounds that it is program-level. A warning on
		// a prelude loop therefore appeared on every file compiled.
		ExprBase:      ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Label:         label,
		Key:           key,
		Value:         value,
		KeyLocation:   keyLoc,
		ValueLocation: valueLoc,
		Iterable:      iterable,
		Body:          body,
	}
	ctx.RecordScope(loop, loopScope)
	return loop
}

func collectForInCondition(node *sitter.Node, ctx *collector_ctx.Ctx) (key, value string, keyLoc, valueLoc ast.Location, iterable ast.Expression) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "for_variable_or_key":
			key = ctx.NodeText(child)
			keyLoc = ctx.NodeLocation(child)
			registerLoopVar(child, key, ctx)
		case "for_index_or_value":
			value = ctx.NodeText(child)
			valueLoc = ctx.NodeLocation(child)
			registerLoopVar(child, value, ctx)
		default:
			if child.IsNamed() {
				iterable = ctx.CollectExpr(child)
			}
		}
	}
	return
}

func registerLoopVar(node *sitter.Node, name string, ctx *collector_ctx.Ctx) {
	v := &ast.VarDeclStmt{
		AstBase:     ast.AstBase{Location: ctx.NodeLocation(node)},
		BindingKind: ast.BindingLet,
		Name:        name,
	}
	if err := ctx.RegisterVariable(v); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register loop variable %q: %v", name, err)
	}
}
