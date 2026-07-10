package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectDataTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var nameLoc ast.Location
	var genericParams []ast.GenericParam
	var allocation types.AllocationModifier
	var constructors []types.DataTypeConstructor
	var derives []string
	var builtin string
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "attribute_list":
			derives = collectDerives(child, ctx)
			builtin = collectBuiltin(child, ctx)
		case "allocation_modifier":
			allocation = allocModifier(child, ctx)
		case "visibility":
			isPublic = true
		case "data_type_name":
			name = ctx.NodeText(child)
			nameLoc = ctx.NodeLocation(child)
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
		NameLocation:  nameLoc,
		GenericParams: genericParams,
		Type: types.DataType{
			Name:         name,
			Constructors: constructors,
			Allocation:   allocation,
		},
		IsPublic: isPublic,
		Derives:  derives,
		Builtin:  builtin,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register data type %q: %v", name, err)
	}

	return astNode
}
