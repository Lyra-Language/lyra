package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectNamedTupleTypeDeclaration(node *sitter.Node) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	var allocation types.AllocationModifier
	var elements []types.Type
	isPublic := false

	allocationNode := node.ChildByFieldName("allocation")
	if allocationNode != nil {
		allocation = c.collectAllocationModifier(allocationNode)
	}
	visibilityNode := node.ChildByFieldName("visibility")
	if visibilityNode != nil {
		isPublic = true
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		name = c.nodeText(nameNode)
	}
	genericParamsNode := node.ChildByFieldName("generic_parameters")
	if genericParamsNode != nil {
		genericParams = c.collectGenericParams(genericParamsNode)
	}
	elementsNode := node.ChildByFieldName("tuple_type_body")
	if elementsNode != nil {
		elements = c.collectTupleTypeBody(elementsNode)
	}
	return &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		IsPublic:      isPublic,
		Allocation:    allocation,
		Type:          types.TupleType{Name: name, Elements: elements},
	}
}

func (c *Collector) collectTupleTypeBody(node *sitter.Node) []types.Type {
	elements := make([]types.Type, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "tuple_type_element" {
			elements = append(elements, c.parseType(child.ChildByFieldName("type")))
		}
	}
	return elements
}
