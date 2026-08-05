package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNamedTupleTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var nameLoc ast.Location
	var genericParams []ast.GenericParam
	var elements []types.Type
	isPublic := false

	if visibilityNode := cst.Field(node, "visibility"); visibilityNode != nil {
		isPublic = true
	}
	if nameNode := cst.Field(node, "name"); nameNode != nil {
		name = ctx.NodeText(nameNode)
		nameLoc = ctx.NodeLocation(nameNode)
	}
	if genericParamsNode := cst.Field(node, "generic_parameters"); genericParamsNode != nil {
		genericParams = ctx.CollectGenericParams(genericParamsNode)
	}
	if elementsNode := cst.Field(node, "tuple_type_body"); elementsNode != nil {
		elements = CollectTupleTypeBody(elementsNode, ctx)
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		NameLocation:  nameLoc,
		GenericParams: genericParams,
		IsPublic:      isPublic,
		Type:          types.TupleType{Name: name, Elements: elements},
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register tuple type %q: %v", name, err)
	}

	return astNode
}
