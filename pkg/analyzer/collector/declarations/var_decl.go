package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

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
	genericParameters := []ast.GenericParam{}
	if genericParametersNode != nil {
		genericParameters = ctx.CollectGenericParams(genericParametersNode)
	}
	if whereNode := node.ChildByFieldName("generic_parameter_constraints"); whereNode != nil {
		genericParameters = ctx.MergeWhereConstraints(genericParameters, whereNode)
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

	astNode := &ast.VarDeclStmt{
		AstBase:                 ast.AstBase{Location: ctx.NodeLocation(node)},
		BindingKind:             kind,
		Name:                    name,
		GenericParams: genericParameters,
		Type:                    varType,
		Value:                   initExpr,
	}

	if err := ctx.RegisterVariable(astNode); err != nil {
		ctx.AddError(node, collector_ctx.SeverityError, "failed to register variable %q: %v", name, err)
	}

	return astNode
}
