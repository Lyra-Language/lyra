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
	genericArguments := []types.Type{}
	for i := uint(0); i < genericArgumentsNode.ChildCount(); i++ {
		child := genericArgumentsNode.Child(i)
		if child.IsNamed() {
			genericArguments = append(genericArguments, ctx.ParseType(child))
		}
	}
	return genericArguments
}

func collectArgumentList(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Expression {
	arguments := []ast.Expression{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			arguments = append(arguments, CollectExpression(child, ctx))
		}
	}
	return arguments
}

func collectMemberExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) ast.Expression {
	propertyNode := node.ChildByFieldName("property")
	isConst := propertyNode != nil && propertyNode.Kind() == "const_identifier"
	if propertyNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "member expression missing property")
		return nil
	}
	property := CollectIdentifierExpr(propertyNode, isConst, loc, ctx)
	if property == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "could not parse member expression property")
		return nil
	}
	return &ast.MemberExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   CollectExpression(node.ChildByFieldName("object"), ctx),
		Property: *property,
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
