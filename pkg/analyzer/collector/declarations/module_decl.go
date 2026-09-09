package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectModuleDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ModuleDeclStmt {
	moduleDecl := &ast.ModuleDeclStmt{
		Path: []ast.ModuleName{},
	}
	pathNode := cst.Field(node, "path")
	if pathNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected module path, got %s", node.Kind())
		return nil
	}
	for i := uint(0); i < pathNode.ChildCount(); i++ {
		child := pathNode.Child(i)
		if child.Kind() == "module_name" {
			moduleDecl.Path = append(moduleDecl.Path, ast.ModuleName{Name: ctx.NodeText(child)})
		}
	}
	moduleDecl.Links = collectModuleLinks(node, ctx)
	return moduleDecl
}

// collectModuleLinks reads `@link("SDL3")` off the module header.
//
// **`@link` is the only attribute a module takes**, and `@symbol` is refused here by
// name rather than ignored: a symbol is one declaration's C name and a module has no
// single one, so admitting it silently would let a reader believe every extern below
// had been renamed. That is the standing rule on this surface — an attribute that
// parses and is read by nobody costs more than an absent one.
func collectModuleLinks(node *sitter.Node, ctx *collector_ctx.Ctx) []string {
	attrs := cst.Field(node, "attributes")
	if attrs == nil {
		return nil
	}
	var links []string
	for i := uint(0); i < attrs.NamedChildCount(); i++ {
		attr := attrs.NamedChild(i)
		nameNode := cst.Field(attr, "name")
		if nameNode == nil {
			continue
		}
		if name := ctx.NodeText(nameNode); name != "link" {
			ctx.AddError(attr, diag.SeverityError,
				"unknown attribute `@%s` on a module; the only one is `@link(\"name\")`",
				name)
			continue
		}
		args := cst.Field(attr, "args")
		if args == nil {
			ctx.AddError(attr, diag.SeverityError,
				"`@link` needs the library to link, as a string: `@link(\"m\")`")
			continue
		}
		for j := uint(0); j < args.NamedChildCount(); j++ {
			arg := args.NamedChild(j)
			if arg.Kind() != "string_literal" {
				ctx.AddError(arg, diag.SeverityError,
					"`@link` takes a library name as a string: `@link(\"m\")`")
				continue
			}
			links = append(links, stringLiteralText(arg, ctx))
		}
	}
	return links
}
