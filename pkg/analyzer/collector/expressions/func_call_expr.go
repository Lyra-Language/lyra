package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectFunctionCallExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.FunctionCallExpr {
	return &ast.FunctionCallExpr{
		ExprBase:         ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Function:         CollectExpression(node.ChildByFieldName("function"), ctx),
		GenericArguments: collectCallGenericArguments(node, ctx),
		Arguments:        collectArgumentList(node.ChildByFieldName("arguments"), ctx),
	}
}

func collectCallGenericArguments(node *sitter.Node, ctx *collctx.Ctx) []types.Type {
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	if genericArgumentsNode == nil {
		return nil
	}
	genericArguments := make([]types.Type, 0)
	for i := uint(0); i < genericArgumentsNode.ChildCount(); i++ {
		child := genericArgumentsNode.Child(i)
		if child.IsNamed() {
			genericArguments = append(genericArguments, ctx.ParseType(child))
		}
	}
	return genericArguments
}

func collectArgumentList(node *sitter.Node, ctx *collctx.Ctx) ast.ArgumentList {
	arguments := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			arguments = append(arguments, CollectExpression(child, ctx))
		}
	}
	return ast.ArgumentList{Arguments: arguments}
}
