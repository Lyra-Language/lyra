package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectLambdaExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	if node.ChildCount() < 1 {
		ctx.AddError(node, diag.SeverityError, "collectLambdaExpr: node has no children")
		return nil
	}
	isUnsafe := node.ChildByFieldName("is_unsafe") != nil
	isPure := node.ChildByFieldName("is_pure") != nil
	isDet := node.ChildByFieldName("is_det") != nil
	isNoAlloc := node.ChildByFieldName("is_noalloc") != nil
	isAsync := node.ChildByFieldName("is_async") != nil
	isGenerator := node.ChildByFieldName("is_gen") != nil

	parametersNode := node.ChildByFieldName("parameters")
	if parametersNode == nil {
		ctx.AddError(node, diag.SeverityError, "collectLambdaExpr: lambda expression missing parameters")
		return nil
	}
	funcScope := ctx.PushFunctionScope()
	defer ctx.PopScope()

	parameters := collectParameters(parametersNode, ctx)
	for i := range parameters {
		pName := parameters[i].GetName()
		if existing, alreadyDeclared := ctx.LookupCurrentScope(pName); alreadyDeclared {
			ctx.AddError(parametersNode, diag.SeverityError,
				"parameter %s is already declared in this scope (first declared at %s)",
				pName, existing.GetLocation().Pretty())
		} else if err := ctx.RegisterParameter(&parameters[i]); err != nil {
			ctx.AddError(parametersNode, diag.SeverityError, "failed to register parameter %q: %v", pName, err)
		}
	}

	bodyNode := node.ChildByFieldName("body")
	var body = ast.Expression(nil)
	var lambdaClauses = []ast.LambdaClause(nil)
	if bodyNode != nil {
		body = ctx.CollectExpr(bodyNode)
	} else {
		lambdClauseNodes := node.ChildByFieldName("lambda_clauses")
		if lambdClauseNodes == nil {
			ctx.AddError(node, diag.SeverityError, "collectLambdaExpr: lambda expression must have either a body or lambda clauses")
			return nil
		}
		lambdaClauses = collectLambdaClauses(lambdClauseNodes, ctx)
	}
	returnTypeNode := node.ChildByFieldName("return_type")
	var returnType types.ReturnType
	if returnTypeNode != nil {
		returnType = collectReturnType(returnTypeNode, ctx)
	}

	lambda := &ast.LambdaExpr{
		ExprBase:      ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		IsUnsafe:      isUnsafe,
		IsPure:        isPure,
		IsDet:         isDet,
		IsNoAlloc:     isNoAlloc,
		IsAsync:       isAsync,
		IsGenerator:   isGenerator,
		Parameters:    parameters,
		Body:          body,
		LambdaClauses: lambdaClauses,
		ReturnType:    returnType,
	}
	// Record the lambda's parameter/function scope so passes keyed on the node
	// (the typechecker's enterScope, and purity's scope resolution) can recover
	// it without re-walking. The body block records its own child scope.
	ctx.RecordScope(lambda, funcScope)
	return lambda
}

func collectLambdaClauses(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.LambdaClause {
	clauses := []ast.LambdaClause{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "lambda_clause" {
			clauses = append(clauses, *CollectLambdaClause(child, ctx))
		}
	}
	return clauses
}

func CollectLambdaClause(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.LambdaClause {
	patterns := collectPatternParameters(node.ChildByFieldName("parameters"), ctx)
	var guard *ast.GuardExpr
	if guardNode := node.ChildByFieldName("guard"); guardNode != nil {
		guard = collectGuard(guardNode, ctx)
	}
	body := ctx.CollectExpr(node.ChildByFieldName("body"))
	return &ast.LambdaClause{
		AstBase:  ast.AstBase{Location: ctx.NodeLocation(node)},
		Patterns: patterns,
		Guard:    guard,
		Body:     body,
	}
}

func collectPatternParameters(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Pattern {
	patterns := []ast.Pattern{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		// tree-sitter inlines the `pattern` rule, so the concrete child kind
		// is used directly (e.g. "identifier") instead of a "pattern" wrapper.
		p := ctx.CollectPattern(child)
		if p != nil {
			patterns = append(patterns, p)
		}
	}
	return patterns
}

func collectParameters(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Parameter {
	parameters := []ast.Parameter{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "parameter" {
			parameters = append(parameters, collectParameter(child, ctx))
		}
	}
	return parameters
}

func collectParameter(node *sitter.Node, ctx *collector_ctx.Ctx) ast.Parameter {
	patternNode := node.ChildByFieldName("pattern")
	if patternNode == nil {
		ctx.AddError(node, diag.SeverityError, "parameter node missing pattern")
		return ast.Parameter{}
	}
	pattern := ctx.CollectPattern(patternNode)
	typeModifierNode := node.ChildByFieldName("type_modifier")
	var typeModifier string
	if typeModifierNode != nil {
		typeModifier = ctx.NodeText(typeModifierNode)
	}
	typeNode := node.ChildByFieldName("type")
	var paramType types.Type
	if typeNode != nil {
		paramType = ctx.ParseType(typeNode)
	}
	var defaultValue ast.Expression
	if defaultValueNode := node.ChildByFieldName("default_value"); defaultValueNode != nil {
		defaultValue = ctx.CollectExpr(defaultValueNode)
	}
	return ast.Parameter{
		AstBase:      ast.AstBase{Location: ctx.NodeLocation(node)},
		Pattern:      pattern,
		TypeModifier: types.TypeModifier(typeModifier),
		Type:         paramType,
		DefaultValue: defaultValue,
	}
}

func collectReturnType(node *sitter.Node, ctx *collector_ctx.Ctx) types.ReturnType {
	typeModifierNode := node.ChildByFieldName("type_modifier")
	var typeModifier string
	if typeModifierNode != nil {
		typeModifier = ctx.NodeText(typeModifierNode)
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		ctx.AddError(node, diag.SeverityError, "return type node missing type")
		return types.ReturnType{}
	}
	return types.ReturnType{
		Type:         ctx.ParseType(typeNode),
		TypeModifier: types.TypeModifier(typeModifier),
	}
}
