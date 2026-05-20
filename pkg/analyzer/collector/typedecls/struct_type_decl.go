package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStructTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var genericParams []ast.GenericParam
	var fields []types.StructField
	var derives []string
	isPublic := false
	var allocation types.AllocationModifier

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "attribute_list":
			derives = collectDerives(child, ctx)
		case "allocation_modifier":
			allocation = allocModifier(child, ctx)
		case "visibility":
			isPublic = true
		case "struct_name":
			name = ctx.NodeText(child)
		case "generic_parameters":
			genericParams = ctx.CollectGenericParams(child)
		case "struct_type_body":
			fields = CollectStructFields(child, ctx)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.NamedStructType{
			Name:       name,
			Fields:     fields,
			Allocation: allocation,
		},
		IsPublic: isPublic,
		Derives:  derives,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, collector_ctx.SeverityError, "failed to register struct type %q: %v", name, err)
	}

	return astNode
}
