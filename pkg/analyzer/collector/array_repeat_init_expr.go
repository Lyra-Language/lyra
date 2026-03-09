package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectArrayRepeatInitExpr(node *sitter.Node) *ast.ArrayRepeatExpr {
	return &ast.ArrayRepeatExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: c.nodeLocation(node)},
			Type:    nil, // Type will be resolved during type checking
		},
		Value: c.collectExpression(node.ChildByFieldName("value")),
		Count: c.collectExpression(node.ChildByFieldName("count")),
	}
}
