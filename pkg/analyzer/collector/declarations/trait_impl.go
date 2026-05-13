package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectTraitImplementation(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TraitImplStmt {
	traitNameNode, ok := ctx.MustField(node, "trait_name")
	if !ok {
		return nil
	}
	traitName := ctx.NodeText(traitNameNode)

	genericParamsNode := node.ChildByFieldName("generic_parameters")
	genericParams := []ast.GenericParam{}
	if genericParamsNode != nil {
		genericParams = ctx.CollectGenericParams(genericParamsNode)
	}

	typeNode, ok := ctx.MustField(node, "type")
	if !ok {
		return nil
	}

	astType := ctx.ParseType(typeNode)
	if astType == nil {
		ctx.AddError(node, collector_ctx.SeverityError, "could not parse trait implementation type")
		return nil
	}

	constraintsNode := node.ChildByFieldName("constraints")
	constraints := []ast.TraitImplConstraint{}
	if constraintsNode != nil {
		constraints = collectTraitImplConstraints(constraintsNode, ctx)
	}

	methodsNode, ok := ctx.MustField(node, "methods")
	if !ok {
		return nil
	}
	methods := collectTraitMethodImpls(methodsNode, ctx)

	return &ast.TraitImplStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		TraitName:     traitName,
		GenericParams: genericParams,
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
	traitBoundsNode, ok := ctx.MustField(node, "trait_bounds")
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
			methods = append(methods, collectTraitMethodImpl(child, ctx))
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
		Name:   methodName,
		Clause: clause,
	}
}
