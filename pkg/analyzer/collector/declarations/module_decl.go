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
	return moduleDecl
}
