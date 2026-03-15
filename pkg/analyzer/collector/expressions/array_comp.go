package expressions

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectArrayCompExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.ArrayCompExpr {
	resultNode := node.ChildByFieldName("result_expression")
	if resultNode == nil {
		ctx.AppendError(fmt.Errorf("array comprehension must have a result"))
		return nil
	}
	return &ast.ArrayCompExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Generators: collectGenerators(node, ctx),
		Guards:     collectGuards(node, ctx),
		Result:     CollectExpression(resultNode, ctx),
	}
}

func collectGenerators(node *sitter.Node, ctx *collctx.Ctx) []ast.Generator {
	generators := make([]ast.Generator, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "generator" {
			generators = append(generators, collectGenerator(child, ctx))
		}
	}
	return generators
}

func collectGenerator(node *sitter.Node, ctx *collctx.Ctx) ast.Generator {
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AppendError(fmt.Errorf("generator must have a value"))
		return ast.Generator{}
	}
	identifierNode := node.ChildByFieldName("identifier")
	if identifierNode == nil {
		ctx.AppendError(fmt.Errorf("generator must have an identifier"))
		return ast.Generator{}
	}
	return ast.Generator{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Value:      CollectExpression(valueNode, ctx),
		Identifier: ctx.NodeText(identifierNode),
	}
}

func collectGuards(node *sitter.Node, ctx *collctx.Ctx) []ast.Expression {
	if node == nil {
		return nil
	}
	guards := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "comprehension_guard" {
			guards = append(guards, CollectExpression(child, ctx))
		}
	}
	return guards
}
