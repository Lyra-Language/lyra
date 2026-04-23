package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectDataTypeDeclaration(node *sitter.Node, ctx *collctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	var allocation types.AllocationModifier
	var constructors []types.DataTypeConstructor
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "allocation_modifier":
			allocation = allocModifier(child, ctx)
		case "visibility":
			isPublic = true
		case "data_type_name":
			name = ctx.NodeText(child)
		case "generic_parameters":
			genericParams = ctx.CollectGenericParams(child)
		case "data_type_constructor":
			_, ctor := collectDataConstructor(child, ctx)
			constructors = append(constructors, ctor)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.DataType{
			Name:         name,
			Constructors: constructors,
			Allocation:   allocation,
		},
		IsPublic: isPublic,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AppendError(err)
	}

	return astNode
}
