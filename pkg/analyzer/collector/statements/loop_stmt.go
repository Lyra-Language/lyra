package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectLoopExpr collects `loop { … }` (no condition) and `while cond { … }` into
// one LoopExpr; the two node kinds differ only in the condition.
func CollectLoopExpr(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.LoopExpr {
	loopScope := ctx.PushLoopScope()
	defer ctx.PopScope()

	labelNode := cst.Field(node, "label")
	label := ""
	if labelNode != nil {
		label = ctx.NodeText(labelNode)
	}

	var conditionExpr *ast.Expression
	bodyField := "loop_body"
	if node.Kind() == "while_loop" {
		bodyField = "while_body"
		conditionNode := cst.Field(node, "condition")
		if conditionNode == nil {
			ctx.AddError(node, diag.SeverityError, "Expected while loop condition")
			return nil
		}
		condition := ctx.CollectExpr(conditionNode)
		conditionExpr = &condition
	}

	bodyNode := cst.Field(node, bodyField)
	if bodyNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected loop body")
		return nil
	}
	body := ctx.CollectExpr(bodyNode)
	bodyBlockPtr, ok := body.(*ast.BlockExpr)
	if !ok {
		ctx.AddError(bodyNode, diag.SeverityError, "Expected block expression for loop body")
		return nil
	}

	loop := &ast.LoopExpr{
		// **The loop's own span.** It carried none until 09/25, which is the same gap its
		// `for/in` sibling had until 08/18 and the same cost: a diagnostic reported against
		// a zero Location prints with no `line:col` *and* escapes the driver's per-file
		// filtering, which keeps a location-less diagnostic on the grounds that it is
		// program-level — so a warning on a prelude loop appeared on every file compiled.
		// Found by the bootstrap: the Lyra collector set a span here and the two printed
		// ASTs disagreed, which is the only way a *missing* location shows up at all.
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Label:     label,
		Condition: conditionExpr,
		Body:      bodyBlockPtr,
	}
	ctx.RecordScope(loop, loopScope)
	return loop
}
