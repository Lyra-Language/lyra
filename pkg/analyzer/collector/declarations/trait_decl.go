package declarations

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectTraitDeclaration(node *sitter.Node, ctx *collctx.Ctx) *ast.TraitDeclStmt {
	visibilityNode := node.ChildByFieldName("visibility")
	isPublic := visibilityNode != nil
	nameNode := node.ChildByFieldName("name")
	name := ctx.NodeText(nameNode)

	genericParamsNode := node.ChildByFieldName("generic_parameters")
	genericParams := make([]string, 0)
	if genericParamsNode != nil {
		genericParams = ctx.CollectGenericParams(genericParamsNode)
	}

	boundsNode := node.ChildByFieldName("trait_bounds")
	bounds := make([]string, 0)
	if boundsNode != nil {
		bounds = collectBounds(boundsNode, ctx)
	}

	genericParameterConstraintsNode := node.ChildByFieldName("trait_generic_parameter_constraints")
	genericParameterConstraints := make([]ast.TraitGenericParameterConstraint, 0)
	if genericParameterConstraintsNode != nil {
		genericParameterConstraints = collectGenericParameterConstraints(genericParameterConstraintsNode, ctx)
	}

	methodsNode := node.ChildByFieldName("methods")
	if methodsNode == nil {
		ctx.AddError(node, collctx.SeverityError, "trait declaration is missing methods node")
		return nil
	}
	methods := collectMethods(methodsNode, ctx)

	return &ast.TraitDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Name: name,
		GenericParams: genericParams,
		GenericParameterConstraints: genericParameterConstraints,
		Bounds: bounds,
		Methods: methods,
		IsPublic: isPublic,
	}
}

func collectBounds(node *sitter.Node, ctx *collctx.Ctx) []string {
	bounds := make([]string, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "trait_name" {
			bounds = append(bounds, ctx.NodeText(child))
		}
	}
	return bounds
}

func collectGenericParameterConstraints(node *sitter.Node, ctx *collctx.Ctx) []ast.TraitGenericParameterConstraint {
	constraints := make([]ast.TraitGenericParameterConstraint, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "trait_generic_parameter_constraint" {
			constraints = append(constraints, collectTraitGenericParameterConstraint(child, ctx))
		}
	}
	return constraints
}

func collectTraitGenericParameterConstraint(node *sitter.Node, ctx *collctx.Ctx) ast.TraitGenericParameterConstraint {
	genericTypeNode := node.ChildByFieldName("generic_type")
	if genericTypeNode == nil {
		ctx.AddError(node, collctx.SeverityError, "trait generic parameter constraint is missing generic type node")
		return ast.TraitGenericParameterConstraint{}
	}
	name := ctx.NodeText(genericTypeNode)
	traitBoundsNode := node.ChildByFieldName("trait_bounds")
	if traitBoundsNode == nil {
		ctx.AddError(node, collctx.SeverityError, "trait generic parameter constraint is missing trait bounds node")
		return ast.TraitGenericParameterConstraint{}
	}
	constraints := collectBounds(traitBoundsNode, ctx)
	return ast.TraitGenericParameterConstraint{
		Name: name,
		Constraints: constraints,
	}
}
func collectMethods(node *sitter.Node, ctx *collctx.Ctx) []ast.TraitMethod {
	methods := make([]ast.TraitMethod, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "trait_method" {
			methods = append(methods, collectTraitMethodDeclaration(child, ctx))
		}
	}
	return methods
}

func collectTraitMethodDeclaration(node *sitter.Node, ctx *collctx.Ctx) ast.TraitMethod {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		ctx.AddError(node, collctx.SeverityError, "trait method is missing name node")
		return ast.TraitMethod{}
	}
	name := collectMethodName(nameNode, ctx)
	signatureNode := node.ChildByFieldName("signature")
	if signatureNode == nil {
		ctx.AddError(node, collctx.SeverityError, "trait method is missing signature node")
		return ast.TraitMethod{}
	}
	signature := ctx.ParseFunctionType(signatureNode)
	if signature == nil {
		ctx.AddError(node, collctx.SeverityError, "could not parse trait method signature")
		return ast.TraitMethod{}
	}
	return ast.TraitMethod{
		Name: name,
		Signature: signature,
	}
}

func collectMethodName(node *sitter.Node, ctx *collctx.Ctx) ast.MethodName {
	name := ctx.NodeText(node)
	switch node.Kind() {
	case "identifier":
		return ast.NewMethodNameIdentifier(name)
	case "unary_operator":
		opNode := node.NamedChild(0)
		op := ctx.NodeText(opNode)
		if opNode.Kind() == "prefix_operator" {
			// trim the underscore
			op = strings.TrimSuffix(op, "_")
			return ast.NewMethodNamePrefix(ast.PrefixOperator(op))
		}
		if opNode.Kind() == "suffix_operator" {
			// trim the underscore
			op = strings.TrimPrefix(op, "_")
			return ast.NewMethodNameSuffix(ast.SuffixOperator(op))
		}
		ctx.AddError(node, collctx.SeverityError, "could not collect method name: unknown unary operator node kind: %s", opNode.Kind())
		return ast.MethodName{}
	case "binary_operator":
		// trim the parens and the underscores
		name = strings.TrimPrefix(name, "(")
		name = strings.TrimSuffix(name, ")")
		name = strings.Trim(name, "_")
		return ast.NewMethodNameBinary(ast.BinaryOperator(name))
	default:
		ctx.AddError(node, collctx.SeverityError, "could not collect method name: unknown node kind: %s", node.Kind())
		return ast.MethodName{}
	}
}