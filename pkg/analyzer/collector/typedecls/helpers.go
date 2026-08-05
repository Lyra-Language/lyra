package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
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
			fieldTypeNode := cst.Field(child, "field_type")
			var fieldType types.Type
			if fieldTypeNode != nil {
				fieldType = ctx.ParseType(fieldTypeNode.Child(0))
			}
			fieldNameNode := cst.Field(child, "field_name")
			fieldName := ctx.NodeText(fieldNameNode)
			defaultValue := ctx.CollectExpr(cst.Field(child, "default_value"))
			frozen := cst.Field(child, "frozen") != nil
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

// collectDataConstructor parses a data_type_constructor node. Each payload
// argument is a "param"-field child (the grammar's repeat1(field('param',
// ...)) — see data_type.js), so params are picked out by field name rather
// than by enumerating every type-node kind a payload could be; the previous
// kind-enumeration silently dropped any kind it didn't list (e.g.
// anonymous_tuple_type, for a parenthesized payload like `C (i64, i64)` —
// the project's own recommended positional-multi-value form — came back
// with zero params).
func collectDataConstructor(node *sitter.Node, ctx *collector_ctx.Ctx) (string, types.DataTypeConstructor) {
	var name string
	ctor := types.DataTypeConstructor{
		Params: []types.Type{},
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "data_type_constructor_name" {
			name = ctx.NodeText(child)
			continue
		}
		if node.FieldNameForChild(uint32(i)) == "param" {
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
			elements = append(elements, ctx.ParseType(cst.Field(child, "type")))
		}
	}
	return elements
}
