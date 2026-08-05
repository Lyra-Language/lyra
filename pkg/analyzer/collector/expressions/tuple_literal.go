package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectTupleLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.TupleLiteralExpr {
	tupleNameNode := cst.Field(node, "tuple_name")
	tupleName := "?"
	if tupleNameNode != nil {
		tupleName = ctx.NodeText(tupleNameNode)
	}
	genericArgumentsNode := cst.Field(node, "generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = collectGenericArgs(genericArgumentsNode, ctx)
	}
	elements := []ast.Expression{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "tuple_value" {
			elements = append(elements, CollectExpression(child.Child(0), ctx))
		}
	}
	return &ast.TupleLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
		},
		Name:             tupleName,
		GenericArguments: genericArguments,
		Elements:         elements,
	}
}
