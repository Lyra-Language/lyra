package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStructLiteralExpr(node *sitter.Node, ctx *collctx.Ctx) *ast.StructInstance {
	nameNode := node.ChildByFieldName("struct_name")
	name := "?"
	if nameNode != nil {
		name = ctx.NodeText(nameNode)
	}
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	genericArguments := []types.Type(nil)
	if genericArgumentsNode != nil {
		genericArguments = collectGenericArgs(genericArgumentsNode, ctx)
	}
	structBodyNode := node.ChildByFieldName("struct_body")
	fields := collectStructInstanceFields(structBodyNode, ctx)
	return &ast.StructInstance{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		},
		Name:        name,
		GenericArgs: genericArguments,
		Fields:      fields,
	}
}

func collectStructInstanceFields(node *sitter.Node, ctx *collctx.Ctx) []ast.StructField {
	fields := make([]ast.StructField, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "struct_shorthand" {
			fields = append(fields, collectStructInstanceShorthandField(child, ctx))
		} else {
			fields = append(fields, collectStructInstanceField(child, ctx))
		}
	}
	return fields
}

func collectStructInstanceShorthandField(node *sitter.Node, ctx *collctx.Ctx) ast.StructField {
	return ast.StructField{
		Name:  "",
		Value: CollectExpression(node.ChildByFieldName("field_value"), ctx),
	}
}

func collectStructInstanceField(node *sitter.Node, ctx *collctx.Ctx) ast.StructField {
	return ast.StructField{
		Name:  ctx.NodeText(node.ChildByFieldName("field_name")),
		Value: CollectExpression(node.ChildByFieldName("field_value"), ctx),
	}
}
