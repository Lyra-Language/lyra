package declarations

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectTraitDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TraitDeclStmt {
	visibilityNode := cst.Field(node, "visibility")
	isPublic := visibilityNode != nil
	nameNode, ok := ctx.MustField(node, "name")
	if !ok {
		return nil
	}
	name := ctx.NodeText(nameNode)

	genericParamsNode := cst.Field(node, "generic_parameters")
	genericParams := []ast.GenericParam{}
	if genericParamsNode != nil {
		genericParams = ctx.CollectGenericParams(genericParamsNode)
	}

	boundsNode := cst.Field(node, "trait_bounds")
	bounds := []string{}
	if boundsNode != nil {
		bounds = ctx.CollectBounds(boundsNode)
	}

	if whereNode := cst.Field(node, "generic_parameter_constraints"); whereNode != nil {
		genericParams = ctx.MergeWhereConstraints(genericParams, whereNode)
	}

	methodsNode, ok := ctx.MustField(node, "methods")
	if !ok {
		return nil
	}
	methods := collectMethods(methodsNode, ctx)

	stmt := &ast.TraitDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		NameLocation:  ctx.NodeLocation(nameNode),
		GenericParams: genericParams,
		Bounds:        bounds,
		Methods:       methods,
		IsPublic:      isPublic,
	}
	if err := ctx.RegisterTrait(stmt); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register trait %q: %v", name, err)
	}
	return stmt
}

func collectMethods(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.TraitMethod {
	methods := []ast.TraitMethod{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "trait_method" {
			methods = append(methods, collectTraitMethodDeclaration(child, ctx))
		}
	}
	return methods
}

func collectTraitMethodDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) ast.TraitMethod {
	nameNode, ok := ctx.MustField(node, "name")
	if !ok {
		ctx.AddError(node, diag.SeverityError, "trait method declaration is missing a name")
		return ast.TraitMethod{}
	}
	name := collectMethodName(nameNode, ctx)
	signatureNode, ok := ctx.MustField(node, "signature")
	if !ok {
		ctx.AddError(node, diag.SeverityError, "trait method declaration is missing a signature")
		return ast.TraitMethod{}
	}
	signature := ctx.ParseLambdaType(signatureNode)
	if signature == nil {
		ctx.AddError(node, diag.SeverityError, "could not parse trait method signature")
		return ast.TraitMethod{}
	}
	defaultMethodNode := cst.Field(node, "default")
	defaultMethod := (*ast.LambdaClause)(nil)
	if defaultMethodNode != nil {
		defaultMethodBodyNode := cst.Field(defaultMethodNode, "body")
		if defaultMethodBodyNode == nil {
			ctx.AddError(node, diag.SeverityError, "default method implementation is missing a body")
			return ast.TraitMethod{}
		}
		defaultMethod = expressions.CollectLambdaClause(defaultMethodBodyNode, ctx)
	}
	return ast.TraitMethod{
		Name:          name,
		Signature:     signature,
		DefaultMethod: defaultMethod,
		IsPure:        cst.Field(node, "is_pure") != nil,
		IsDet:         cst.Field(node, "is_det") != nil,
		IsNoAlloc:     cst.Field(node, "is_noalloc") != nil,
	}
}

func collectMethodName(node *sitter.Node, ctx *collector_ctx.Ctx) ast.MethodName {
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
		ctx.AddError(node, diag.SeverityError, "could not collect method name: unknown unary operator node kind: %s", opNode.Kind())
		return ast.MethodName{}
	case "binary_operator":
		// trim the parens and the underscores
		name = strings.TrimPrefix(name, "(")
		name = strings.TrimSuffix(name, ")")
		name = strings.Trim(name, "_")
		return ast.NewMethodNameBinary(ast.BinaryOperator(name))
	default:
		ctx.AddError(node, diag.SeverityError, "could not collect method name: unknown node kind: %s", node.Kind())
		return ast.MethodName{}
	}
}
