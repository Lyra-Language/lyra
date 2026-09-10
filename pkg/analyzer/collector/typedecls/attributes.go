package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
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

// CollectMustRelease extracts the single argument of a `@must_release(f)` attribute —
// the function that discharges the obligation a value of this type carries — together
// with that argument's own span. Returns ("", zero) when the attribute is absent.
//
// The name is taken as **text and not resolved here**, which is rule 4's constraint
// rather than laziness: which declaration a bare name means depends on the module
// asking, and the collector is still building the table that answers. `checker`'s
// must-release pass resolves it with LookupFunctionFrom from the declaration's own
// location, so an unexported release function in a binding module is found and a
// same-named function in another module is not.
//
// Only the first argument counts. A release is one call — a type needing two is a
// type wanting a wrapper.
//
// An attribute with no argument names no function and is reported **here**, returning
// "" — so a caller can read "" as "this type carries no obligation" with no second
// state to disambiguate. Encoding "present but empty" as a non-zero location beside an
// empty name would be a convention every reader has to know and one reader will miss.
func CollectMustRelease(attrList *sitter.Node, ctx *collector_ctx.Ctx) (string, ast.Location) {
	for i := uint(0); i < attrList.ChildCount(); i++ {
		child := attrList.Child(i)
		if child.Kind() != "attribute" {
			continue
		}
		nameNode := cst.Field(child, "name")
		if nameNode == nil || ctx.NodeText(nameNode) != "must_release" {
			continue
		}
		argsNode := cst.Field(child, "args")
		if argsNode == nil {
			ctx.AddError(child, diag.SeverityError, "the `@must_release` attribute needs the "+
				"name of the function that releases this type, as in `@must_release(unload_sound)`")
			return "", ast.Location{}
		}
		for j := uint(0); j < argsNode.ChildCount(); j++ {
			if argsNode.FieldNameForChild(uint32(j)) == "value" {
				arg := argsNode.Child(j)
				return ctx.NodeText(arg), ctx.NodeLocation(arg)
			}
		}
	}
	return "", ast.Location{}
}
