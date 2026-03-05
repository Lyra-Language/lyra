package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectDataTypeDeclaration(node *sitter.Node) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	var allocation types.AllocationModifier
	var constructors []types.DataTypeConstructor
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "allocation_modifier":
			allocation = c.collectAllocationModifier(child)
		case "visibility":
			isPublic = true
		case "data_type_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "data_type_constructor":
			_, ctor := c.collectDataConstructor(child)
			constructors = append(constructors, ctor)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.DataType{
			Name:         name,
			Constructors: constructors,
			Allocation:   allocation,
		},
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}
