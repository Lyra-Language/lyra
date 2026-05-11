package declarations

import (
	"reflect"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func isNilExpression(expr ast.Expression) bool {
	if expr == nil {
		return true
	}
	v := reflect.ValueOf(expr)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}

func bindingKind(keyword string, ctx *collector_ctx.Ctx) ast.BindingKind {
	switch keyword {
	case "let":
		return ast.BindingLet
	case "var":
		return ast.BindingVar
	case "const":
		return ast.BindingConst
	default:
		ctx.AddError(nil, collector_ctx.SeverityError, "invalid binding kind: %s", keyword)
		return ast.BindingUnknown
	}
}

func CollectVariableDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.VarDeclStmt {
	kind := bindingKind(ctx.NodeText(node.ChildByFieldName("keyword")), ctx)
	name := ctx.NodeText(node.ChildByFieldName("name"))
	genericParametersNode := node.ChildByFieldName("generic_parameters")
	genericParameters := make([]string, 0)
	if genericParametersNode != nil {
		genericParameters = ctx.CollectGenericParams(genericParametersNode)
	}
	genericParamConstraintsNode := node.ChildByFieldName("generic_parameter_constraints")
	genericParamConstraints := make([]ast.GenericParameterConstraint, 0)
	if genericParamConstraintsNode != nil {
		genericParamConstraints = ctx.CollectGenericParameterConstraints(genericParamConstraintsNode)
	}

	var varType types.Type
	if typeAnnotation := node.ChildByFieldName("type_annotation"); typeAnnotation != nil {
		varType = ctx.ParseType(typeAnnotation.ChildByFieldName("type"))
	}

	valueNode := node.ChildByFieldName("value")
	var initExpr ast.Expression = nil
	if valueNode != nil {
		initExpr = ctx.CollectExpr(valueNode)
	}

	// CollectExpr can return a typed-nil expression when collection fails.
	// Normalize that here so later type inference does not panic on GetType().
	if isNilExpression(initExpr) {
		initExpr = nil
	}

	// Infer variable type from initializer only when no type annotation was given
	if varType == nil && initExpr != nil {
		if arrayType, ok := initExpr.GetType().(types.StaticArrayType); ok {
			varType = arrayType
		} else if tupleType, ok := initExpr.GetType().(types.TupleType); ok {
			varType = tupleType
		} else if rangeType, ok := initExpr.GetType().(types.RangeType); ok {
			varType = rangeType
		}
	}

	// When variable has explicit tuple type and initializer is a tuple literal, propagate type to the literal
	if tupleType, ok := varType.(types.TupleType); ok {
		if tupleLit, ok := initExpr.(*ast.TupleLiteralExpr); ok {
			tupleLit.ExprBase.Type = tupleType
		}
	}

	astNode := &ast.VarDeclStmt{
		AstBase:                 ast.AstBase{Location: ctx.NodeLocation(node)},
		BindingKind:             kind,
		Name:                    name,
		GenericParams:           genericParameters,
		GenericParamConstraints: genericParamConstraints,
		Type:                    varType,
		Value:                   initExpr,
	}

	if err := ctx.RegisterVariable(astNode); err != nil {
		ctx.AddError(node, collector_ctx.SeverityError, "failed to register variable %q: %v", name, err)
	}

	return astNode
}
