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
		// **The declaration's own span**, which it carried none of until 09/25 — the
		// third node found this way in two days, after `LoopExpr` and the AST's
		// locations slice. A zero Location prints no `line:col` and escapes the driver's
		// per-file filtering (hazard 14), and it also makes the node invisible to
		// anything asking *where* it is: the editor's "add an import after the module
		// declaration" put the line above it instead.
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Path:    []ast.ModuleName{},
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
	moduleDecl.Links, moduleDecl.Packages = collectModuleLinks(node, ctx)
	return moduleDecl
}

// collectModuleLinks reads `@link("SDL3", pkg: "sdl3")` off the module header: the
// libraries to link, and the pkg-config packages that say where they are.
//
// **`@link` is the only attribute a module takes**, and `@symbol` is refused here by
// name rather than ignored: a symbol is one declaration's C name and a module has no
// single one, so admitting it silently would let a reader believe every extern below
// had been renamed. That is the standing rule on this surface — an attribute that
// parses and is read by nobody costs more than an absent one.
func collectModuleLinks(node *sitter.Node, ctx *collector_ctx.Ctx) ([]string, []string) {
	attrs := cst.Field(node, "attributes")
	if attrs == nil {
		return nil, nil
	}
	var links, packages []string
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
		libs, pkgs := readLinkArgs(attr, ctx)
		links = append(links, libs...)
		packages = append(packages, pkgs...)
	}
	return links, packages
}

// readLinkArgs reads one `@link(…)`: its positional strings are libraries, as the linker
// names them (`"SDL3"` becomes `-lSDL3`), and a `pkg: "sdl3"` names the pkg-config package
// that says *where* the library is — which `lyrac` asks, so a Homebrew or /usr/local
// install links with no `LIBRARY_PATH`. Shared by the module header and the extern, which
// take the same `@link`.
//
// `pkg` is the one named argument; any other is refused by name rather than ignored.
func readLinkArgs(attr *sitter.Node, ctx *collector_ctx.Ctx) ([]string, []string) {
	args := cst.Field(attr, "args")
	if args == nil {
		ctx.AddError(attr, diag.SeverityError,
			"`@link` needs the library to link, as a string: `@link(\"m\")`")
		return nil, nil
	}
	var libs, pkgs []string
	for j := uint(0); j < args.NamedChildCount(); j++ {
		arg := args.NamedChild(j)
		if arg.Kind() == "attribute_named_arg" {
			nameNode := cst.Field(arg, "name")
			value := cst.Field(arg, "value")
			if nameNode == nil || value == nil {
				continue
			}
			if name := ctx.NodeText(nameNode); name != "pkg" {
				ctx.AddError(arg, diag.SeverityError,
					"`@link` has one named argument, `pkg:` (the pkg-config package); `%s:` is not it", name)
				continue
			}
			if value.Kind() != "string_literal" {
				ctx.AddError(value, diag.SeverityError,
					"`pkg:` names a pkg-config package as a string: `@link(\"SDL3\", pkg: \"sdl3\")`")
				continue
			}
			pkgs = append(pkgs, stringLiteralText(value, ctx))
			continue
		}
		if arg.Kind() != "string_literal" {
			ctx.AddError(arg, diag.SeverityError,
				"`@link` takes a library name as a string: `@link(\"m\")`")
			continue
		}
		// The name as the linker takes it, not a flag: `@link("m")` becomes `-lm`.
		// Reading the literal's text rather than evaluating it is what keeps an
		// attribute argument data — see the grammar note on why it is a plain string.
		libs = append(libs, stringLiteralText(arg, ctx))
	}
	if len(libs) == 0 && len(pkgs) > 0 {
		ctx.AddError(attr, diag.SeverityError,
			"`@link` names the library itself as well as its package: `@link(\"SDL3\", pkg: \"sdl3\")`")
	}
	return libs, pkgs
}
