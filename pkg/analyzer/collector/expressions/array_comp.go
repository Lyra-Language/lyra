package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectArrayCompExpr collects `[ x in xs | guard | result ]`.
//
// **A scope is pushed around the whole thing**, and each generator's name registered in it
// as a placeholder binding the typechecker later fills in — the same arrangement a for-in
// loop uses (`registerLoopVar`), for the same reason: the element type is not known until
// the source has been inferred, but the *name* has to be resolvable before the guards and
// result are collected or references to it collect as undefined identifiers.
//
// The scope also confines the binding, which matters more here than in a loop: a
// comprehension is an ordinary expression and can appear anywhere, so leaking `x` into the
// enclosing scope would let `[ x in xs | x ]` silently rebind an `x` the reader is using.
//
// Order is load-bearing. Generators are collected **first** so that a guard or result
// mentioning the bound name resolves; and a generator's *source* is collected before its
// own name is registered, so `[ x in x | x ]` reads the outer `x` as the source rather than
// itself.
func collectArrayCompExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	resultNode := node.ChildByFieldName("result_expr")
	if resultNode == nil {
		ctx.AddError(node, diag.SeverityError, "array comprehension must have a result")
		return nil
	}
	scope := ctx.PushBlockScope()
	defer ctx.PopScope()

	comp := &ast.ArrayCompExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Generators: collectGenerators(node, ctx),
	}
	comp.Guards = collectGuards(node, ctx)
	comp.Result = CollectExpression(resultNode, ctx)
	// Recorded so the typechecker can re-enter this scope to type the guards and result
	// against the generator bindings, the way it re-enters a block's.
	ctx.RecordScope(comp, scope)
	return comp
}

func collectGenerators(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Generator {
	generators := []ast.Generator{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "generator" {
			generators = append(generators, collectGenerator(child, ctx))
		}
	}
	return generators
}

func collectGenerator(node *sitter.Node, ctx *collector_ctx.Ctx) ast.Generator {
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AddError(node, diag.SeverityError, "generator must have a value")
		return ast.Generator{}
	}
	identifierNode := node.ChildByFieldName("identifier")
	if identifierNode == nil {
		ctx.AddError(node, diag.SeverityError, "generator must have an identifier")
		return ast.Generator{}
	}
	// The source before the binding: see the note on ordering above.
	value := CollectExpression(valueNode, ctx)
	name := ctx.NodeText(identifierNode)
	binding := &ast.VarDeclStmt{
		AstBase:     ast.AstBase{Location: ctx.NodeLocation(identifierNode)},
		BindingKind: ast.BindingLet,
		Name:        name,
	}
	if err := ctx.RegisterVariable(binding); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register generator variable %q: %v", name, err)
	}
	return ast.Generator{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Value:      value,
		Identifier: name,
	}
}

func collectGuards(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Expression {
	if node == nil {
		return nil
	}
	guards := []ast.Expression{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "comprehension_guard" {
			guards = append(guards, CollectExpression(child, ctx))
		}
	}
	return guards
}
