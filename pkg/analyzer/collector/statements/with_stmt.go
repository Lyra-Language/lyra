package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectWithStatement(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.WithStmt {
	withScope := ctx.PushBlockScope()
	defer ctx.PopScope()

	name := ""
	if nameNode := cst.Field(node, "name"); nameNode != nil {
		name = ctx.NodeText(nameNode)
		v := &ast.VarDeclStmt{
			AstBase:     ast.AstBase{Location: ctx.NodeLocation(nameNode)},
			BindingKind: ast.BindingLet,
			Name:        name,
		}
		if err := ctx.RegisterVariable(v); err != nil {
			ctx.AddError(nameNode, diag.SeverityError, "failed to register with binding %q: %v", name, err)
		}
	}

	arenaNode := cst.Field(node, "arena")
	if arenaNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected arena expression in with statement")
		return nil
	}
	arena := ctx.CollectExpr(arenaNode)

	bodyNode := cst.Field(node, "body")
	if bodyNode == nil {
		ctx.AddError(node, diag.SeverityError, "Expected body block in with statement")
		return nil
	}
	body := ctx.CollectExpr(bodyNode)
	bodyBlock, ok := body.(*ast.BlockExpr)
	if !ok {
		ctx.AddError(bodyNode, diag.SeverityError, "Expected block expression for with statement body")
		return nil
	}

	with := &ast.WithStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:    name,
		Arena:   arena,
		Body:    *bodyBlock,
	}
	// The with-binding (`with a = Arena.new(...)`) lives in this scope, above the
	// body block's own child scope.
	ctx.RecordScope(with, withScope)
	return with
}
