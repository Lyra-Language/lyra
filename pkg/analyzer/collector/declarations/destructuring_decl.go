package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
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
	keyword := ctx.NodeText(cst.Field(node, "keyword"))
	// A bare-name binding (`if let s = w`) takes the *identifier* branch of
	// `declaration`, which has a `name` field and no `pattern` one — the branch that
	// exists so a plain `let` isn't ambiguous with a function definition. Reading the
	// absent pattern field panicked the collector on a perfectly parseable program
	// (cst.Field returns a nil node, and ParseDestructuringPattern indexed
	// straight into it). Synthesize the equivalent identifier pattern instead: a
	// name is a pattern that binds it, so every downstream pass sees one shape.
	patternNode := cst.Field(node, "pattern")
	var pattern ast.Pattern
	switch {
	case patternNode != nil:
		pattern = ctx.ParseDestructuringPattern(patternNode)
	default:
		nameNode := cst.Field(node, "name")
		if nameNode == nil {
			ctx.AddError(node, diag.SeverityError, "expected a name or pattern to bind")
			return nil
		}
		pattern = &ast.IdentifierPattern{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(nameNode)}},
			Name:        ctx.NodeText(nameNode),
		}
	}
	var typeAnnotation types.Type = nil
	if typeAnnotationNode := cst.Field(node, "type_annotation"); typeAnnotationNode != nil {
		typeAnnotation = ctx.ParseType(cst.Field(typeAnnotationNode, "type"))
	}
	value := ctx.CollectExpr(cst.Field(node, "value"))

	return &ast.DestructuringDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Keyword: keyword,
		IsMut:   cst.Field(node, "mutability") != nil,
		Pattern: pattern,
		Type:    typeAnnotation,
		Value:   value,
	}
}
