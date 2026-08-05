package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectImportStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ImportStmt {
	importStmt := &ast.ImportStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Path:    []ast.ModuleName{},
	}

	pathNode := cst.Field(node, "path")
	aliasNode := cst.Field(node, "alias")
	membersNode := cst.Field(node, "members")

	if pathNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected module path, got %s", node.Kind())
		return nil
	}

	for i := uint(0); i < pathNode.ChildCount(); i++ {
		child := pathNode.Child(i)
		if child.Kind() == "module_name" {
			importStmt.Path = append(importStmt.Path, ast.ModuleName{Name: ctx.NodeText(child)})
		}
	}

	if aliasNode != nil {
		aliasNameNode := cst.Field(aliasNode, "name")
		if aliasNameNode != nil {
			importStmt.Alias = ctx.NodeText(aliasNameNode)
		}
	}

	if membersNode != nil {
		importStmt.Members = []ast.ImportMember{}
		for i := uint(0); i < membersNode.ChildCount(); i++ {
			child := membersNode.Child(i)
			if child.Kind() == "import_member" {
				importStmt.Members = append(importStmt.Members, CollectImportMember(child, ctx))
			}
		}
	}

	return importStmt
}

func CollectImportMember(node *sitter.Node, ctx *collector_ctx.Ctx) ast.ImportMember {
	nameNode := cst.Field(node, "name")
	if nameNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected import member name, got %s", node.Kind())
		return ast.ImportMember{Name: "", Alias: ""}
	}
	name := ctx.NodeText(nameNode)
	aliasNode := cst.Field(node, "alias")
	alias := ""
	if aliasNode != nil {
		alias = ctx.NodeText(aliasNode)
	}
	return ast.ImportMember{Name: name, Alias: alias, Location: ctx.NodeLocation(node)}
}
