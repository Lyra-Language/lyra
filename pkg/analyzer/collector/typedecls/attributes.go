package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectDerives extracts trait names from @derive(...) attributes in an attribute_list node.
func collectDerives(attrList *sitter.Node, ctx *collector_ctx.Ctx) []string {
	var derives []string
	for i := uint(0); i < attrList.ChildCount(); i++ {
		child := attrList.Child(i)
		if child.Kind() != "attribute" {
			continue
		}
		nameNode := cst.Field(child, "name")
		if nameNode == nil || ctx.NodeText(nameNode) != "derive" {
			continue
		}
		argsNode := cst.Field(child, "args")
		if argsNode == nil {
			continue
		}
		for j := uint(0); j < argsNode.ChildCount(); j++ {
			if argsNode.FieldNameForChild(uint32(j)) == "value" {
				derives = append(derives, ctx.NodeText(argsNode.Child(j)))
			}
		}
	}
	return derives
}

// collectBuiltin extracts the single argument of a `@builtin(X)` attribute (e.g.
// "Result"), marking the declaration as a request to be treated as a canonical
// compiler-known type. Returns "" when no `@builtin` attribute is present. Only
// the first argument is used — `@builtin` names one kind. The request is
// validated and resolved later by the canonical-type pass.
// Exported as CollectBuiltin because the *trait* collector needs the identical
// reading (`@builtin(Ord)`), and one attribute reader is the point: two would be
// free to disagree about which argument counts.
func CollectBuiltin(attrList *sitter.Node, ctx *collector_ctx.Ctx) string {
	for i := uint(0); i < attrList.ChildCount(); i++ {
		child := attrList.Child(i)
		if child.Kind() != "attribute" {
			continue
		}
		nameNode := cst.Field(child, "name")
		if nameNode == nil || ctx.NodeText(nameNode) != "builtin" {
			continue
		}
		argsNode := cst.Field(child, "args")
		if argsNode == nil {
			continue
		}
		for j := uint(0); j < argsNode.ChildCount(); j++ {
			if argsNode.FieldNameForChild(uint32(j)) == "value" {
				return ctx.NodeText(argsNode.Child(j))
			}
		}
	}
	return ""
}
