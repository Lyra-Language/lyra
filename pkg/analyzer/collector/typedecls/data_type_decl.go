package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectDataTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var nameLoc ast.Location
	var genericParams []ast.GenericParam
	var constructors []types.DataTypeConstructor
	var derives []string
	var builtin string
	// A data type's constructors sit directly under the declaration node rather than
	// in a body node of their own, so the docs pass runs over the declaration itself.
	memberDocs := CollectMemberDocs(node, ctx)
	isPublic := cst.Field(node, "visibility") != nil

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "attribute_list":
			derives = collectDerives(child, ctx)
			builtin = CollectBuiltin(child, ctx)
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
		},
		IsPublic:   isPublic,
		Derives:    derives,
		Builtin:    builtin,
		MemberDocs: memberDocs,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register data type %q: %v", name, err)
	}

	return astNode
}
