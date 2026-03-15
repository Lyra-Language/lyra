package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStructTypeDeclaration(node *sitter.Node, ctx *collctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	var fields []types.StructField
	isPublic := false
	var allocation types.AllocationModifier

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "allocation_modifier":
			allocation = allocModifier(child, ctx)
		case "visibility":
			isPublic = true
		case "struct_name":
			name = ctx.NodeText(child)
		case "generic_parameters":
			genericParams = collectGenericParams(child, ctx)
		case "struct_type_body":
			fields = collectStructFields(child, ctx)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.StructType{
			Name:       name,
			Fields:     fields,
			Allocation: allocation,
		},
		IsPublic: isPublic,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AppendError(err)
	}

	return astNode
}
