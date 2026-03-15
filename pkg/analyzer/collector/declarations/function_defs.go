package declarations

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectFunctionDefinition(node *sitter.Node, ctx *collctx.Ctx) *ast.FunctionDefStmt {
	var name string
	var genericParams []string
	var signature *types.FunctionType
	var clauses []*ast.FunctionClause
	isPublic := false
	isPure := false
	isAsync := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "function_signature":
			name, genericParams, signature, isPure, isAsync = collectFunctionSignature(child, ctx)
		case "function_clause":
			clauses = append(clauses, collectFunctionClause(child, ctx))
		case "function_clause_list":
			for j := uint(0); j < child.ChildCount(); j++ {
				if child.Child(j).Kind() == "function_clause" {
					clauses = append(clauses, collectFunctionClause(child.Child(j), ctx))
				}
			}
		}
	}

	astNode := &ast.FunctionDefStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Signature:     signature,
		Clauses:       clauses,
		IsPublic:      isPublic,
		IsPure:        isPure,
		IsAsync:       isAsync,
	}

	if err := ctx.RegisterFunction(astNode); err != nil {
		ctx.AppendError(err)
	}

	return astNode
}

func collectFunctionSignature(node *sitter.Node, ctx *collctx.Ctx) (name string, genericParams []string, sig *types.FunctionType, isPure, isAsync bool) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		text := ctx.NodeText(child)
		switch child.Kind() {
		case "identifier":
			name = text
		case "generic_parameters":
			genericParams = ctx.CollectGenericParams(child)
		case "function_type":
			sig = parseFunctionType(child, ctx)
		default:
			switch text {
			case "pure":
				isPure = true
			case "async":
				isAsync = true
			}
		}
	}
	return name, genericParams, sig, isPure, isAsync
}

func parseFunctionType(node *sitter.Node, ctx *collctx.Ctx) *types.FunctionType {
	ft := &types.FunctionType{
		ParameterTypes: make([]types.ParameterType, 0),
	}

	parameterTypes := node.ChildByFieldName("parameter_types")
	if parameterTypes != nil {
		for i := uint(0); i < parameterTypes.ChildCount(); i++ {
			child := parameterTypes.Child(i)
			if child.Kind() == "parameter_type" {
				ft.ParameterTypes = append(ft.ParameterTypes, parseParameterType(child, ctx))
			}
		}
	}
	if returnType := node.ChildByFieldName("return_type"); returnType != nil {
		ft.ReturnType = ctx.ParseType(returnType)
	}

	return ft
}

func parseParameterType(node *sitter.Node, ctx *collctx.Ctx) types.ParameterType {
	modifier := types.Modifier("")
	if modifierNode := node.ChildByFieldName("modifier"); modifierNode != nil {
		modifier = types.Modifier(ctx.NodeText(modifierNode))
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		ctx.AppendError(fmt.Errorf("parameter type is missing type node"))
		return types.ParameterType{}
	}
	return types.ParameterType{
		Modifier: modifier,
		Type:     ctx.ParseType(typeNode),
	}
}

func collectFunctionClause(node *sitter.Node, ctx *collctx.Ctx) *ast.FunctionClause {
	var parameters []ast.Parameter
	var guard *ast.GuardExpr
	var body ast.Expression

	parameterListNode := node.ChildByFieldName("parameters")
	if parameterListNode != nil {
		parameters = collectFunctionParameters(parameterListNode, ctx)
	}
	guardNode := node.ChildByFieldName("guard")
	if guardNode != nil {
		guardExpressionNode := guardNode.ChildByFieldName("guard_expression")
		if guardExpressionNode == nil {
			ctx.AppendError(fmt.Errorf("guard expression is missing"))
		} else {
			guard = &ast.GuardExpr{
				ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(guardNode)}},
				Condition: expressions.CollectExpression(guardExpressionNode, ctx),
			}
		}
	}
	bodyNode := node.ChildByFieldName("body")
	if bodyNode != nil {
		body = expressions.CollectExpression(bodyNode, ctx)
	}

	return &ast.FunctionClause{
		AstBase:    ast.AstBase{Location: ctx.NodeLocation(node)},
		Parameters: parameters,
		Guard:      guard,
		Body:       body,
	}
}

func collectFunctionParameters(node *sitter.Node, ctx *collctx.Ctx) []ast.Parameter {
	parameters := make([]ast.Parameter, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter" {
			parameters = append(parameters, collectParameter(child, ctx))
		}
	}
	return parameters
}

func collectParameter(node *sitter.Node, ctx *collctx.Ctx) ast.Parameter {
	var pattern ast.Pattern
	if patternNode := node.ChildByFieldName("pattern"); patternNode != nil {
		pattern = ctx.CollectPattern(patternNode)
	} else {
		ctx.AppendError(fmt.Errorf("parameter is missing pattern node"))
	}
	defaultValue := ast.Expression(nil)
	if defaultValueNode := node.ChildByFieldName("default_value"); defaultValueNode != nil {
		if defaultValueExpressionNode := defaultValueNode.ChildByFieldName("expression"); defaultValueExpressionNode != nil {
			defaultValue = expressions.CollectExpression(defaultValueExpressionNode, ctx)
		} else {
			ctx.AppendError(fmt.Errorf("parameter default value is missing expression"))
		}
	}
	return ast.Parameter{Pattern: pattern, DefaultValue: defaultValue}
}
