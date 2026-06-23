package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectDestructuringDeclaration collects the `declaration` node shared by
// `if let`/`else` pattern bindings (destructuring_if_stmt.go,
// destructuring_else_stmt.go). It does not register the pattern's bound names
// — those two callers have different binding scopes (if-let's names are local
// to its Then branch; let-else's persist after the statement) and register
// them itself via registerDestructuredNames once it knows which scope applies.
func CollectDestructuringDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.DestructuringDeclStmt {
	keyword := ctx.NodeText(node.ChildByFieldName("keyword"))
	pattern := ctx.ParseDestructuringPattern(node.ChildByFieldName("pattern"))
	var typeAnnotation types.Type = nil
	if typeAnnotationNode := node.ChildByFieldName("type_annotation"); typeAnnotationNode != nil {
		typeAnnotation = ctx.ParseType(typeAnnotationNode.ChildByFieldName("type"))
	}
	value := ctx.CollectExpr(node.ChildByFieldName("value"))

	return &ast.DestructuringDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Keyword: keyword,
		IsMut:   node.ChildByFieldName("mutability") != nil,
		Pattern: pattern,
		Type:    typeAnnotation,
		Value:   value,
	}
}
