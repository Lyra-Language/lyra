package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNamedTupleTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	var allocation types.AllocationModifier
	var elements []types.Type
	isPublic := false

	if allocationNode := node.ChildByFieldName("allocation"); allocationNode != nil {
		allocation = allocModifier(allocationNode, ctx)
	}
	if visibilityNode := node.ChildByFieldName("visibility"); visibilityNode != nil {
		isPublic = true
	}
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		name = ctx.NodeText(nameNode)
	}
	if genericParamsNode := node.ChildByFieldName("generic_parameters"); genericParamsNode != nil {
		genericParams = ctx.CollectGenericParams(genericParamsNode)
	}
	if elementsNode := node.ChildByFieldName("tuple_type_body"); elementsNode != nil {
		elements = CollectTupleTypeBody(elementsNode, ctx)
	}

	return &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		IsPublic:      isPublic,
		Allocation:    allocation,
		Type:          types.TupleType{Name: name, Elements: elements},
	}
}
