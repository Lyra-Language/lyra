package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectAwaitExpr(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.AwaitExpr {
	return &ast.AwaitExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:    CollectExpression(node.ChildByFieldName("operand"), ctx),
	}
}