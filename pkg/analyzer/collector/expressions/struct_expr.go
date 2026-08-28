package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNamedStructLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	nameNode := cst.Field(node, "struct_name")
	name := "?"
	if nameNode != nil {
		name = ctx.NodeText(nameNode)
	}

	genericArgumentsNode := cst.Field(node, "generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = collectGenericArgs(genericArgumentsNode, ctx)
	}

	structBodyNode := cst.Field(node, "struct_body")
	structUpdateNode := cst.Field(structBodyNode, "struct_update")
	structShorthandNode := cst.Field(structBodyNode, "struct_shorthand")
	structFieldsNode := cst.Field(structBodyNode, "struct_fields")

	baseStruct := (*ast.IdentifierExpr)(nil)
	fields := []ast.StructField(nil)
	if structUpdateNode != nil {
		baseStructNode := cst.Field(structUpdateNode, "base")
		if baseStructNode == nil {
			ctx.AddError(node, diag.SeverityError, "struct update must have a base struct")
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
	structBodyNode := cst.Field(node, "struct_body")

	fields := []ast.StructField(nil)
	var baseStructIdentifier *ast.IdentifierExpr
	if structBodyNode != nil {
		structUpdateNode := cst.Field(structBodyNode, "struct_update")
		structShorthandNode := cst.Field(structBodyNode, "struct_shorthand")
		structFieldsNode := cst.Field(structBodyNode, "struct_fields")
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
		ctx.AddError(structUpdateNode, diag.SeverityError, "expected identifier, got %s", baseStruct.GetName())
		return nil
	}
}

func collectStructFields(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.StructField {
	fields := []ast.StructField(nil)
	// An **empty** literal body — `Person {}`, every field defaulted — has no
	// `struct_fields` child, so the lookup that produced this node answers nil. Hazard
	// 2: calling NamedChildCount on it does not panic, it *hangs* inside the CGO
	// binding, so the guard is the difference between an empty field list and a
	// wedged compiler.
	if node == nil {
		return fields
	}
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
		Name:  ctx.NodeText(cst.Field(node, "field_name")),
		Value: CollectExpression(cst.Field(node, "field_value"), ctx),
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
