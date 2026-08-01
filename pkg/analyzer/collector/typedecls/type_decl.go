package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "named_tuple_type":
			return collectNamedTupleTypeDeclaration(child, ctx)
		case "struct_type":
			return collectStructTypeDeclaration(child, ctx)
		case "data_type":
			return collectDataTypeDeclaration(child, ctx)
		case "constrained_type":
			return collectConstrainedTypeDeclaration(child, ctx)
		case "type_alias":
			return collectTypeAliasDeclaration(child, ctx)
		}
	}
	return nil
}
