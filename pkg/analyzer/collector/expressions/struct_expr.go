package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNamedStructLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	nameNode := node.ChildByFieldName("struct_name")
	name := "?"
	if nameNode != nil {
		name = ctx.NodeText(nameNode)
	}

	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = collectGenericArgs(genericArgumentsNode, ctx)
	}

	structBodyNode := node.ChildByFieldName("struct_body")
	structUpdateNode := structBodyNode.ChildByFieldName("struct_update")
	structShorthandNode := structBodyNode.ChildByFieldName("struct_shorthand")
	structFieldsNode := structBodyNode.ChildByFieldName("struct_fields")

	baseStruct := (*ast.IdentifierExpr)(nil)
	fields := []ast.StructField(nil)
	if structUpdateNode != nil {
		baseStructNode := structUpdateNode.ChildByFieldName("base")
		if baseStructNode == nil {
			ctx.AddError(node, collector_ctx.SeverityError, "struct update must have a base struct")
			return nil
		}
		expr := ctx.CollectExpr(baseStructNode)
		baseStruct = expr.(*ast.IdentifierExpr)
		fields = collectStructFields(structUpdateNode, ctx)
	} else if structShorthandNode != nil {
		fields = collectStructShorthandFields(structShorthandNode, ctx)
	} else {
		fields = collectStructFields(structFieldsNode, ctx)
	}
	return &ast.StructInstanceExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
		},
		Name:        name,
		GenericArgs: genericArguments,
		BaseStruct:  baseStruct,
		Fields:      fields,
	}
}

func collectAnonymousStructLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.AnonymousStructInstanceExpr {
	structBodyNode := node.ChildByFieldName("struct_body")

	fields := []ast.StructField(nil)
	var baseStructIdentifier *ast.IdentifierExpr
	if structBodyNode != nil {
		structUpdateNode := structBodyNode.ChildByFieldName("struct_update")
		structShorthandNode := structBodyNode.ChildByFieldName("struct_shorthand")
		structFieldsNode := structBodyNode.ChildByFieldName("struct_fields")
		if structUpdateNode != nil {
			baseStructIdentifier = collectBaseStruct(structUpdateNode, ctx)
			fields = collectStructFields(structUpdateNode, ctx)
		} else if structShorthandNode != nil {
			fields = collectStructShorthandFields(structShorthandNode, ctx)
		} else {
			fields = collectStructFields(structFieldsNode, ctx)
		}
	}
	return &ast.AnonymousStructInstanceExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		BaseStruct: baseStructIdentifier,
		Fields:     fields,
	}
}

func collectBaseStruct(structUpdateNode *sitter.Node, ctx *collector_ctx.Ctx) *ast.IdentifierExpr {
	baseStruct := ctx.CollectExpr(structUpdateNode)
	identifierExpr, ok := baseStruct.(*ast.IdentifierExpr)
	if ok {
		return identifierExpr
	} else {
		ctx.AddError(structUpdateNode, collector_ctx.SeverityError, "expected identifier, got %s", baseStruct.GetName())
		return nil
	}
}

func collectStructFields(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.StructField {
	fields := []ast.StructField(nil)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "struct_field" {
			fields = append(fields, collectStructInstanceField(child, ctx))
		}
	}
	return fields
}

func collectStructInstanceField(node *sitter.Node, ctx *collector_ctx.Ctx) ast.StructField {
	return ast.StructField{
		Name:  ctx.NodeText(node.ChildByFieldName("field_name")),
		Value: CollectExpression(node.ChildByFieldName("field_value"), ctx),
	}
}

func collectStructShorthandFields(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.StructField {
	fields := []ast.StructField(nil)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "field_value" {
			fields = append(fields, ast.StructField{
				Name:  "",
				Value: CollectExpression(child, ctx),
			})
		}
	}
	return fields
}
