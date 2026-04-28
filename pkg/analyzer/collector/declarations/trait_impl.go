package declarations

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectTraitImplementation(node *sitter.Node, ctx *collctx.Ctx) *ast.TraitImplStmt {
	traitNameNode, ok := ctx.MustField(node, "trait_name")
	if !ok {
		return nil
	}
	traitName := ctx.NodeText(traitNameNode)

	genericParamsNode := node.ChildByFieldName("generic_parameters")
	genericParams := make([]string, 0)
	if genericParamsNode != nil {
		genericParams = ctx.CollectGenericParams(genericParamsNode)
	}

	typeNode, ok := ctx.MustField(node, "type")
	if !ok {
		return nil
	}

	astType := ctx.ParseType(typeNode)
	if astType == nil {
		ctx.AddError(node, collctx.SeverityError, "could not parse trait implementation type")
		return nil
	}

	constraintsNode := node.ChildByFieldName("constraints")
	constraints := make([]ast.TraitImplConstraint, 0)
	if constraintsNode != nil {
		constraints = collectTraitImplConstraints(constraintsNode, ctx)
	}

	methodsNode, ok := ctx.MustField(node, "methods")
	if !ok {
		return nil
	}
	methods := collectTraitMethodImpls(methodsNode, ctx)

	return &ast.TraitImplStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		TraitName: traitName,
		GenericParams: genericParams,
		Type: astType,
		Constraints: constraints,
		Methods: methods,
	}
}

func collectTraitImplConstraints(node *sitter.Node, ctx *collctx.Ctx) []ast.TraitImplConstraint {
	constraints := make([]ast.TraitImplConstraint, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "impl_constraint" {
			constraints = append(constraints, collectTraitImplConstraint(child, ctx))
		}
	}
	return constraints
}

func collectTraitImplConstraint(node *sitter.Node, ctx *collctx.Ctx) ast.TraitImplConstraint {
	genericTypeNode, ok := ctx.MustField(node, "generic_type")
	if !ok {
		return ast.TraitImplConstraint{}
	}
	genericType := ctx.NodeText(genericTypeNode)
	traitBoundsNode, ok := ctx.MustField(node, "trait_bounds")
	if !ok {
		return ast.TraitImplConstraint{}
	}
	traitBounds := collectBounds(traitBoundsNode, ctx)
	return ast.TraitImplConstraint{
		GenericType: genericType,
		TraitBounds: traitBounds,
	}
}

func collectTraitMethodImpls(node *sitter.Node, ctx *collctx.Ctx) []ast.TraitMethodImpl {
	methods := make([]ast.TraitMethodImpl, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		fmt.Println("collectTraitMethodImpls", child.Kind())
		if child.Kind() == "trait_method_implementation" {
			methods = append(methods, collectTraitMethodImpl(child, ctx))
		}
	}
	return methods
}

func collectTraitMethodImpl(node *sitter.Node, ctx *collctx.Ctx) ast.TraitMethodImpl {
	methodNameNode, ok := ctx.MustField(node, "method_name")
	if !ok {
		return ast.TraitMethodImpl{}
	}
	methodName := collectMethodName(methodNameNode.NamedChild(0), ctx)
	clauseNode, ok := ctx.MustField(node, "function_clause")
	if !ok {
		return ast.TraitMethodImpl{}
	}
	clause := CollectFunctionClause(clauseNode, ctx)
	if clause == nil {
		ctx.AddError(node, collctx.SeverityError, "could not parse trait method implementation function clause")
		return ast.TraitMethodImpl{}
	}
	return ast.TraitMethodImpl{
		Name: methodName,
		Clause: *clause,
	}
}