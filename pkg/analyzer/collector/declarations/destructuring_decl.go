package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectDestructuringDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.DestructuringDeclStmt {
	keyword := ctx.NodeText(node.ChildByFieldName("keyword"))
	pattern := ctx.ParseDestructuringPattern(node.ChildByFieldName("pattern"))
	typeAnnotationNode := node.ChildByFieldName("type_annotation")
	var typeAnnotation types.Type = nil
	if typeAnnotationNode != nil {
		typeAnnotation = ctx.ParseType(typeAnnotationNode)
	}
	value := ctx.CollectExpr(node.ChildByFieldName("value"))

	return &ast.DestructuringDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Keyword: keyword,
		Pattern: pattern,
		Type:    typeAnnotation,
		Value:   value,
	}
}
