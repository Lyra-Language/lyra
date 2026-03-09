package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectIdentifierExpr(node *sitter.Node, isConst bool, loc ast.Location) *ast.IdentifierExpr {
	return &ast.IdentifierExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Name:     c.nodeText(node),
		IsConst:  isConst,
	}
}
