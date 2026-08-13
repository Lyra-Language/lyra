package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStructTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var nameLoc ast.Location
	var genericParams []ast.GenericParam
	var fields []types.StructField
	var derives []string
	var memberDocs map[string]*ast.Doc
	isPublic := cst.Field(node, "visibility") != nil

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "attribute_list":
			derives = collectDerives(child, ctx)
		case "struct_name":
			name = ctx.NodeText(child)
			nameLoc = ctx.NodeLocation(child)
		case "generic_parameters":
			genericParams = ctx.CollectGenericParams(child)
		case "struct_type_body":
			fields = CollectStructFields(child, ctx)
			memberDocs = CollectMemberDocs(child, ctx)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		NameLocation:  nameLoc,
		GenericParams: genericParams,
		Type: types.NamedStructType{
			Name:   name,
			Fields: fields,
		},
		IsPublic:   isPublic,
		Derives:    derives,
		MemberDocs: memberDocs,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register struct type %q: %v", name, err)
	}

	return astNode
}
