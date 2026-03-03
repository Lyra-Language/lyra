package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectStructTypeDeclaration(node *sitter.Node) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	fields := make(map[string]types.StructField)
	isPublic := false
	var allocation types.AllocationModifier

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "allocation_modifier":
			allocation = c.collectAllocationModifier(child)
		case "visibility":
			isPublic = true
		case "struct_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "struct_type_body":
			fields = c.collectStructFields(child)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.StructType{
			Name:       name,
			Fields:     fields,
			Allocation: allocation,
		},
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}
