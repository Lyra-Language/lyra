package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectStructFields returns struct fields in source declaration order.
func collectStructFields(node *sitter.Node, ctx *collctx.Ctx) []types.StructField {
	var fields []types.StructField
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "struct_member" {
			fieldTypeNode := child.ChildByFieldName("field_type")
			var fieldType types.Type
			if fieldTypeNode != nil {
				fieldType = ctx.ParseType(fieldTypeNode.Child(0))
			}
			fieldName := ctx.NodeText(child.ChildByFieldName("field_name"))
			defaultValue := ctx.CollectExpr(child.ChildByFieldName("default_field_value"))
			fields = append(fields, types.StructField{
				Name:         fieldName,
				Type:         fieldType,
				DefaultValue: defaultValue,
			})
		}
	}
	return fields
}

// collectDataConstructor parses a data_type_constructor node.
func collectDataConstructor(node *sitter.Node, ctx *collctx.Ctx) (string, types.DataTypeConstructor) {
	var name string
	ctor := types.DataTypeConstructor{
		Params: make([]types.Type, 0),
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "data_type_constructor_name":
			name = ctx.NodeText(child)
		case "generic_type", "user_defined_type_name", "signed_integer_type", "string_type", "boolean_type", "float_type":
			ctor.Params = append(ctor.Params, ctx.ParseType(child))
		case "struct_type_body":
			ctor.Fields = collectStructFields(child, ctx)
		}
	}

	ctor.Name = name
	return name, ctor
}

// CollectTupleTypeBody is exported because collector.go calls it from parseAnonymousTupleType.
func CollectTupleTypeBody(node *sitter.Node, ctx *collctx.Ctx) []types.Type {
	elements := make([]types.Type, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "tuple_type_element" {
			elements = append(elements, ctx.ParseType(child.ChildByFieldName("type")))
		}
	}
	return elements
}
