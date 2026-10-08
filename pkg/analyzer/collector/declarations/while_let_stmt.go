package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectWhileLetLoop erases `label: while let p = v { body }` into
//
//	label: loop { if let p = v { body } else { break } }
//
// so no later pass knows the form exists. `if let` already has every rule the loop
// needs — p's names scoped to the body, the pattern checked like any `if let`'s, a
// `weak` value upgraded — and `continue` returning to the head re-evaluates v, which
// is the loop's meaning. The `break` is unlabeled: it sits directly in the loop.
//
// Every synthesized block gets a recorded scope, nested as the source would nest them
// (loop → body block → the if-let's scope → the user's block; the else block beside
// it), because the typechecker re-enters scopes by node and a miss is silent.
func CollectWhileLetLoop(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ExpressionStmt {
	declNode := cst.Field(node, "declaration")
	bodyNode := cst.Field(node, "while_body")
	if declNode == nil || bodyNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected a `let` and a body in `while let`")
		return nil
	}
	label := ""
	if labelNode := cst.Field(node, "label"); labelNode != nil {
		label = ctx.NodeText(labelNode)
	}
	loc := ctx.NodeLocation(node)

	loopScope := ctx.PushLoopScope()
	defer ctx.PopScope()
	bodyScope := ctx.PushBlockScope()

	destructuring := CollectDestructuringDeclaration(declNode, ctx)
	if destructuring == nil {
		ctx.PopScope()
		return nil
	}
	ifLet := &ast.IfDestructuringStmt{
		AstBase:                ast.AstBase{Location: loc},
		DestructuringStatement: *destructuring,
	}
	letScope := ctx.PushBlockScope()
	ctx.RecordScope(ifLet, letScope)
	registerDestructuredNames(declNode, destructuring.Pattern, destructuring, ctx)
	ifLet.Then = expressions.CollectBlockExpr(bodyNode, ctx, ctx.NodeLocation(bodyNode))
	ctx.PopScope()

	elseScope := ctx.PushBlockScope()
	ifLet.Else = &ast.BlockExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Statements: []ast.Statement{&ast.BreakStmt{AstBase: ast.AstBase{Location: loc}}},
	}
	ctx.ScopeTable.Set(ifLet.Else, elseScope)
	ctx.PopScope()

	body := &ast.BlockExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Statements: []ast.Statement{ifLet},
	}
	ctx.ScopeTable.Set(body, bodyScope)
	ctx.PopScope()

	loop := &ast.LoopExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Label:    label,
		Body:     body,
	}
	ctx.RecordScope(loop, loopScope)
	return &ast.ExpressionStmt{AstBase: ast.AstBase{Location: loc}, Expression: loop}
}
