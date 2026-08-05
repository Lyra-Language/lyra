package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectTypeAliasDeclaration lowers `type Op = ((i64, i64)) -> i64`.
//
// **The whole of transparency is the `Type` field below: the alias registers the
// aliased type *itself*, with no wrapper.** Nothing downstream then needs to know
// aliases exist — `resolveType`'s UnresolvedType case looks a name up with
// LookupType and returns the declaration's type, so `Op` expands to the function
// type at every annotation site, and assignability, inference and the backend all
// see a type they already handle. Compare `newtype`, which registers a
// `types.ConstrainedType` wrapper precisely because it is a *distinct* type with
// its own identity and constraints.
//
// The cost of that simplicity is that the alias name is gone by the time anything
// reports a type: a mismatch on an `Op` parameter names the function type, not
// `Op`. That is the correct trade for a transparent alias — the name is a spelling,
// not an identity, and a diagnostic that said `Op` would be claiming an identity
// the type does not have — but it is the thing to revisit if the messages read
// badly in practice.
func collectTypeAliasDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	nameNode := cst.Field(node, "name")
	typeNode := cst.Field(node, "type")
	if nameNode == nil || typeNode == nil {
		// Both are required by the grammar, so this is only reachable from a partial
		// parse. Bail rather than register a half-built type: a nil `Type` in the
		// symbol table would surface much later, as an unexplained nil dereference in
		// whichever pass first resolved the name.
		ctx.AddError(node, diag.SeverityError, "malformed type alias")
		return nil
	}

	name := ctx.NodeText(nameNode)
	astNode := &ast.TypeDeclStmt{
		AstBase:      ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:         name,
		NameLocation: ctx.NodeLocation(nameNode),
		Type:         ctx.ParseType(typeNode),
		IsPublic:     cst.Field(node, "visibility") != nil,
		IsAlias:      true,
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register type alias %q: %v", name, err)
	}

	return astNode
}
