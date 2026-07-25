package expressions

import (
	"reflect"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectBlockExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.BlockExpr {
	if node == nil {
		return nil
	}
	scope := ctx.PushBlockScope()
	defer ctx.PopScope()
	statements := []ast.Statement{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			continue
		}
		// A named child that isn't a statement — a trailing `comment`/`doc_comment`
		// most commonly — collects to nil; appending it would leave a nil (or the
		// value expr) *not* last, so the block's value (its final statement) would be
		// this nil and the whole block would miscompile. Guard the same way the
		// top-level program collection does (untyped and typed nils alike).
		if stmt := ctx.CollectStatement(child); !isNilStmt(stmt) {
			statements = append(statements, stmt)
		}
	}
	block := &ast.BlockExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Statements: statements,
	}
	ctx.ScopeTable.Set(block, scope)
	return block
}

// isNilStmt reports whether a collected statement is nil — an untyped nil, or a
// typed nil pointer wrapped in the interface (which a non-statement child such as
// a trailing comment collects to). Mirrors the top-level program collector's guard.
func isNilStmt(s ast.Statement) bool {
	if s == nil {
		return true
	}
	rv := reflect.ValueOf(s)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}
