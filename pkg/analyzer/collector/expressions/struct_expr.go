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

	baseStruct := ast.Expression(nil)
	fields := []ast.StructField(nil)
	if structUpdateNode != nil {
		baseStruct = collectBaseStruct(structUpdateNode, ctx)
		if baseStruct == nil {
			ctx.AddError(node, diag.SeverityError, "struct update must have a base struct")
			return nil
		}
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
	var baseStructIdentifier ast.Expression
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

// The expression a record update copies from: `b` in `P { b | x: 1 }`, and since 09/19
// any postfix form — a call, a field, an index — so this collects it like any other
// expression. Read by the labelled field rather than by position: the CST fallback
// recurses into a node's first named child, which happened to be the base and would go
// on happening to be it until the rule gained a child.
func collectBaseStruct(structUpdateNode *sitter.Node, ctx *collector_ctx.Ctx) ast.Expression {
	baseNode := cst.Field(structUpdateNode, "base")
	if baseNode == nil {
		return nil
	}
	return ctx.CollectExpr(baseNode)
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
	firstAt := map[string]*sitter.Node{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() != "struct_field" {
			continue
		}
		field := collectStructInstanceField(child, ctx)
		if field.Value == nil {
			continue // see appendCollected
		}
		if first, seen := firstAt[field.Name]; seen {
			reportDuplicateField(ctx, child, first, field.Name, "is given a value twice in this literal", "a field takes one value")
		} else {
			firstAt[field.Name] = child
		}
		fields = append(fields, field)
	}
	return fields
}

// reportDuplicateField reports lyra-E075 at a field's second occurrence, naming its first.
// Shared by struct literals — every literal form reads its fields here — and struct patterns.
func reportDuplicateField(ctx *collector_ctx.Ctx, second, first *sitter.Node, name, what, rule string) {
	at := ctx.NodeLocation(first)
	ctx.AddErrorCoded(second, diag.SeverityError, diag.CodeDuplicateStructField,
		"field %q %s (first at %d:%d); %s", name, what, at.StartLine, at.StartCol, rule)
}

// ReportDuplicateField is reportDuplicateField for the collector's pattern code.
func ReportDuplicateField(ctx *collector_ctx.Ctx, second, first *sitter.Node, name, what, rule string) {
	reportDuplicateField(ctx, second, first, name, what, rule)
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
		if child.Kind() != "field_value" {
			continue
		}
		if value := CollectExpression(child, ctx); value != nil { // see appendCollected
			fields = append(fields, ast.StructField{Name: "", Value: value})
		}
	}
	return fields
}
