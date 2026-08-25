package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectTraitImplementation(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TraitImplStmt {
	traitNameNode, ok := ctx.MustField(node, "trait_name")
	if !ok {
		return nil
	}
	traitName := ctx.NodeText(traitNameNode)
	// The impl's *trait* — its target type goes through ParseType below and records
	// itself, but the trait name is read straight from its field, so `Shown` in
	// `impl Shown for Point` had no span while `Point` did.
	ctx.RecordTypeRef(traitName, ctx.NodeLocation(traitNameNode))

	genericParams := []ast.GenericParam{}
	// The `<…>` after the trait name is the trait's argument list, whose grammar
	// (`field("generic_parameters", seq("<", commaSep1($.type), ">"))`) labels
	// every child — the `<`/`>` tokens and each type — with the same field name,
	// so cst.Field would return only the `<`. Iterate with
	// FieldNameForChild and parse the named (type) children into trait args, used
	// to bind the trait's own type parameters at dispatch.
	var traitArgs []types.Type
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.FieldNameForChild(uint32(i)) != "generic_parameters" {
			continue
		}
		child := node.Child(i)
		if child.IsNamed() && !cst.IsComment(child) {
			if arg := ctx.ParseType(child); arg != nil {
				traitArgs = append(traitArgs, arg)
			}
		}
	}

	typeNode, ok := ctx.MustField(node, "type")
	if !ok {
		return nil
	}

	astType := ctx.ParseType(typeNode)
	if astType == nil {
		ctx.AddError(node, diag.SeverityError, "could not parse trait implementation type")
		return nil
	}

	constraintsNode := cst.Field(node, "constraints")
	constraints := []ast.TraitImplConstraint{}
	if constraintsNode != nil {
		constraints = collectTraitImplConstraints(constraintsNode, ctx)
	}

	methods := []ast.TraitMethodImpl{}
	if methodsNode := cst.Field(node, "methods"); methodsNode != nil {
		methods = collectTraitMethodImpls(methodsNode, ctx)
	}

	return &ast.TraitImplStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		TraitName:     traitName,
		GenericParams: genericParams,
		TraitArgs:     traitArgs,
		Type:          astType,
		Constraints:   constraints,
		Methods:       methods,
	}
}

func collectTraitImplConstraints(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.TraitImplConstraint {
	constraints := []ast.TraitImplConstraint{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "impl_constraint" {
			constraints = append(constraints, collectTraitImplConstraint(child, ctx))
		}
	}
	return constraints
}

func collectTraitImplConstraint(node *sitter.Node, ctx *collector_ctx.Ctx) ast.TraitImplConstraint {
	genericTypeNode, ok := ctx.MustField(node, "generic_type")
	if !ok {
		return ast.TraitImplConstraint{}
	}
	genericType := ctx.NodeText(genericTypeNode)
	traitBoundsNode, ok := ctx.MustField(node, "trait_impl_bounds")
	if !ok {
		return ast.TraitImplConstraint{}
	}
	traitBounds := ctx.CollectBounds(traitBoundsNode)
	return ast.TraitImplConstraint{
		GenericType: genericType,
		TraitBounds: traitBounds,
	}
}

func collectTraitMethodImpls(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.TraitMethodImpl {
	methods := []ast.TraitMethodImpl{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "trait_method_implementation" {
			method := collectTraitMethodImpl(child, ctx)
			method.Doc = ctx.DocFor(child)
			methods = append(methods, method)
		}
	}
	return methods
}

func collectTraitMethodImpl(node *sitter.Node, ctx *collector_ctx.Ctx) ast.TraitMethodImpl {
	methodNameNode, ok := ctx.MustField(node, "method_name")
	if !ok {
		return ast.TraitMethodImpl{}
	}
	methodName := collectMethodName(methodNameNode.NamedChild(0), ctx)
	clauseNode, ok := ctx.MustField(node, "method_clause")
	if !ok {
		return ast.TraitMethodImpl{}
	}
	clause := *expressions.CollectLambdaClause(clauseNode, ctx)
	return ast.TraitMethodImpl{
		Name:      methodName,
		IsPure:    cst.Field(node, "is_pure") != nil,
		IsDet:     cst.Field(node, "is_det") != nil,
		IsNoAlloc: cst.Field(node, "is_noalloc") != nil,
		Clause:    clause,
	}
}
