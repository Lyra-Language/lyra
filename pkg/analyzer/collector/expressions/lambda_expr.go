package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectLambdaExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.LambdaExpr {
	if node.ChildCount() < 1 {
		ctx.AddError(node, collctx.SeverityError, "collectLambdaExpr: node has no children")
		return nil
	}
	isUnsafe := node.ChildByFieldName("is_unsafe") != nil
	isPure := node.ChildByFieldName("is_pure") != nil
	isAsync := node.ChildByFieldName("is_async") != nil
	isGenerator := node.ChildByFieldName("is_generator") != nil
	isRecursive := node.ChildByFieldName("is_recursive") != nil

	parametersNode := node.ChildByFieldName("parameters")
	if parametersNode == nil {
		ctx.AddError(node, collctx.SeverityError, "collectLambdaExpr: lambda expression missing parameters")
		return nil
	}
	parameters := collectParameters(parametersNode, ctx)

	bodyNode := node.ChildByFieldName("body")
	var body = ast.Expression(nil)
	var lambdaClauses = []ast.LambdaClause(nil)
	if bodyNode != nil {
		body = ctx.CollectExpr(bodyNode)
	} else {
		lambdClauseNodes := node.ChildByFieldName("lambda_clauses")
		if lambdClauseNodes == nil {
			ctx.AddError(node, collctx.SeverityError, "collectLambdaExpr: lambda expression must have either a body or lambda clauses")
			return nil
		}
		lambdaClauses = collectLambdaClauses(lambdClauseNodes, ctx)
	}
	returnTypeNode := node.ChildByFieldName("return_type")
	var returnType types.ReturnType
	if returnTypeNode != nil {
		returnType = collectReturnType(returnTypeNode, ctx)
	}

	return &ast.LambdaExpr{
		ExprBase:      ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		IsUnsafe:      isUnsafe,
		IsPure:        isPure,
		IsAsync:       isAsync,
		IsGenerator:   isGenerator,
		IsRecursive:   isRecursive,
		Parameters:    parameters,
		Body:          body,
		LambdaClauses: lambdaClauses,
		ReturnType:    returnType,
	}
}

func collectLambdaClauses(node *sitter.Node, ctx *collctx.Ctx) []ast.LambdaClause {
	clauses := []ast.LambdaClause{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "lambda_clause" {
			clauses = append(clauses, *CollectLambdaClause(child, ctx))
		}
	}
	return clauses
}

func CollectLambdaClause(node *sitter.Node, ctx *collctx.Ctx) *ast.LambdaClause {
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

func collectPatternParameters(node *sitter.Node, ctx *collctx.Ctx) []ast.Pattern {
	patterns := []ast.Pattern{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "pattern" {
			patterns = append(patterns, ctx.CollectPattern(child))
		}
	}
	return patterns
}

func collectParameters(node *sitter.Node, ctx *collctx.Ctx) []ast.Parameter {
	parameters := []ast.Parameter{}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "parameter" {
			parameters = append(parameters, collectParameter(child, ctx))
		}
	}
	return parameters
}

func collectParameter(node *sitter.Node, ctx *collctx.Ctx) ast.Parameter {
	patternNode := node.ChildByFieldName("pattern")
	if patternNode == nil {
		ctx.AddError(node, collctx.SeverityError, "parameter node missing pattern")
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
		Pattern:      pattern,
		TypeModifier: types.TypeModifier(typeModifier),
		Type:         paramType,
		DefaultValue: defaultValue,
	}
}

func collectReturnType(node *sitter.Node, ctx *collctx.Ctx) types.ReturnType {
	typeModifierNode := node.ChildByFieldName("type_modifier")
	var typeModifier string
	if typeModifierNode != nil {
		typeModifier = ctx.NodeText(typeModifierNode)
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		ctx.AddError(node, collctx.SeverityError, "return type node missing type")
		return types.ReturnType{}
	}
	return types.ReturnType{
		Type:         ctx.ParseType(typeNode),
		TypeModifier: types.TypeModifier(typeModifier),
	}
}
