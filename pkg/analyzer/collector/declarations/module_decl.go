package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectModuleDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ModuleDeclStmt {
	moduleDecl := &ast.ModuleDeclStmt{
		Path: make([]ast.ModuleName, 0),
	}
	pathNode := node.ChildByFieldName("path")
	if pathNode == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "Expected module path, got %s", node.Kind())
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
