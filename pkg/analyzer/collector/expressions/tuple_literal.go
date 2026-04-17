package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectTupleLiteralExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.TupleLiteralExpr {
	tupleNameNode := node.ChildByFieldName("tuple_name")
	tupleName := "?"
	if tupleNameNode != nil {
		tupleName = ctx.NodeText(tupleNameNode)
	}
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = collectGenericArgs(genericArgumentsNode, ctx)
	}
	elements := make([]ast.Expression, 0)
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
