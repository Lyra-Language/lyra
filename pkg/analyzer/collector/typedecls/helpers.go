package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectStructFields returns struct fields in source declaration order,
// emitting an error for any duplicate field name.
func CollectStructFields(node *sitter.Node, ctx *collector_ctx.Ctx) []types.StructField {
	var fields []types.StructField
	seen := map[string]ast.Location{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "struct_member" {
			fieldTypeNode := child.ChildByFieldName("field_type")
			var fieldType types.Type
			if fieldTypeNode != nil {
				fieldType = ctx.ParseType(fieldTypeNode.Child(0))
			}
			fieldNameNode := child.ChildByFieldName("field_name")
			fieldName := ctx.NodeText(fieldNameNode)
			defaultValue := ctx.CollectExpr(child.ChildByFieldName("default_value"))
			frozen := child.ChildByFieldName("frozen") != nil
			if prevLoc, dup := seen[fieldName]; dup {
				ctx.AddErrorRelated(child, diag.SeverityError,
					[]diag.RelatedInformation{{Location: prevLoc, Message: "previously declared here"}},
					"duplicate field %q in struct", fieldName)
			} else {
				seen[fieldName] = ctx.NodeLocation(child)
				fields = append(fields, types.StructField{
					Name:         fieldName,
					Type:         fieldType,
					Frozen:       frozen,
					DefaultValue: defaultValue,
				})
			}
		}
	}
	return fields
}

// collectDataConstructor parses a data_type_constructor node.
func collectDataConstructor(node *sitter.Node, ctx *collector_ctx.Ctx) (string, types.DataTypeConstructor) {
	var name string
	ctor := types.DataTypeConstructor{
		Params: []types.Type{},
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "data_type_constructor_name":
			name = ctx.NodeText(child)
		case "generic_type", "user_defined_type_name", "signed_integer_type", "string_type",
			"boolean_type", "float_type", "anonymous_struct_type":
			ctor.Params = append(ctor.Params, ctx.ParseType(child))
		}
	}

	ctor.Name = name
	return name, ctor
}

// CollectTupleTypeBody is exported because collector.go calls it from parseAnonymousTupleType.
func CollectTupleTypeBody(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Type {
	elements := []types.Type{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "tuple_type_element" {
			elements = append(elements, ctx.ParseType(child.ChildByFieldName("type")))
		}
	}
	return elements
}
