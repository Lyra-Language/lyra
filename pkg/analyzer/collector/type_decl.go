package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectTypeDeclaration(node *sitter.Node) *ast.TypeDeclStmt {
	// type_declaration contains struct_type, data_type, trait_declaration, etc.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "struct_type":
			return c.collectStructTypeDeclaration(child)
		case "data_type":
			return c.collectDataTypeDeclaration(child)
		case "constrained_type":
			return c.collectConstrainedTypeDeclaration(child)
		}
	}
	return nil
}

func (c *Collector) collectAllocationModifier(node *sitter.Node) types.AllocationModifier {
	return types.AllocationModifier(c.nodeText(node))
}
