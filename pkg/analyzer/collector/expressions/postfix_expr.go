package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectFunctionCallExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.FunctionCallExpr {
	return &ast.FunctionCallExpr{
		ExprBase:         ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Function:         CollectExpression(node.ChildByFieldName("function"), ctx),
		GenericArguments: collectCallGenericArguments(node, ctx),
		Arguments:        collectArgumentList(node.ChildByFieldName("arguments"), ctx),
	}
}

func collectCallGenericArguments(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Type {
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

func collectArgumentList(node *sitter.Node, ctx *collector_ctx.Ctx) ast.ArgumentList {
	arguments := make([]ast.Expression, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			arguments = append(arguments, CollectExpression(child, ctx))
		}
	}
	return ast.ArgumentList{Arguments: arguments}
}

func collectMemberExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) *ast.MemberExpr {
	propertyNode := node.ChildByFieldName("property")
	isConst := propertyNode != nil && propertyNode.Kind() == "const_identifier"
	return &ast.MemberExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   CollectExpression(node.ChildByFieldName("object"), ctx),
		Property: *CollectIdentifierExpr(propertyNode, isConst, loc, ctx),
		Optional: optional,
	}
}

func collectIndexExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) *ast.IndexExpr {
	return &ast.IndexExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   CollectExpression(node.ChildByFieldName("object"), ctx),
		Index:    CollectExpression(node.ChildByFieldName("index"), ctx),
		Optional: optional,
	}
}

func collectTryExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.TryExpr {
	return &ast.TryExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(node.ChildByFieldName("operand"), ctx),
	}
}
