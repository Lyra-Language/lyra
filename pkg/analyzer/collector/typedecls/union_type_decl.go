package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// A `union` declaration. The body reuses the grammar's `struct_member`, so the two
// things a struct field carries and a union member cannot arrive here and are refused
// by name (lyra-E072) rather than silently dropped:
//
//   - **`readonly`** freezes a field after construction, and there is nothing to freeze
//     when every member aliases the same bytes — writing one writes them all, so a
//     frozen member would be a promise the next member's write breaks.
//   - **A default value** presumes one member is the one initialized, which is exactly
//     the fact a union does not record.
//
// Admit-then-report rather than a second grammar rule, on the trade `lyra-E065` and
// `lyra-E067` already make: a typed message naming the rule beats a syntax error
// pointing at whichever token failed to shift, and one member rule cannot drift from
// another that does not exist.
func collectUnionTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	var name string
	var nameLoc ast.Location
	var genericParams []ast.GenericParam
	var members []types.StructField
	var memberDocs map[string]*ast.Doc
	isPublic := cst.Field(node, "visibility") != nil

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "union_name":
			name = ctx.NodeText(child)
			nameLoc = ctx.NodeLocation(child)
		case "generic_parameters":
			genericParams = ctx.CollectGenericParams(child)
		case "union_type_body":
			members = CollectStructFields(child, ctx)
			memberDocs = CollectMemberDocs(child, ctx)
			checkUnionMemberForm(child, ctx)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		NameLocation:  nameLoc,
		GenericParams: genericParams,
		Type: types.UnionType{
			Name:    name,
			Members: members,
		},
		IsPublic:   isPublic,
		MemberDocs: memberDocs,
	}

	if len(members) == 0 {
		ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeMalformedUnion,
			"union %s: a union with no members has no size and nothing to read; "+
				"give it at least one member", name)
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register union type %q: %v", name, err)
	}

	return astNode
}

// checkUnionMemberForm refuses the two `struct_member` forms a union member cannot take.
func checkUnionMemberForm(body *sitter.Node, ctx *collector_ctx.Ctx) {
	for i := uint(0); i < body.ChildCount(); i++ {
		member := body.Child(i)
		if member.Kind() != "struct_member" {
			continue
		}
		if frozen := cst.Field(member, "frozen"); frozen != nil {
			ctx.AddErrorCoded(frozen, diag.SeverityError, diag.CodeMalformedUnion,
				"a union member cannot be `readonly`: every member names the same "+
					"bytes, so writing any other member rewrites this one")
		}
		if def := cst.Field(member, "default_value"); def != nil {
			ctx.AddErrorCoded(def, diag.SeverityError, diag.CodeMalformedUnion,
				"a union member cannot have a default value: a union does not record "+
					"which member is live, so there is no member to default")
		}
	}
}
