package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectVariableDeclaration(node *sitter.Node) *ast.VariableDeclarationStmt {
	keyword := c.nodeText(node.ChildByFieldName("keyword"))
	name := c.nodeText(node.ChildByFieldName("name"))

	var varType types.Type
	if typeAnnotation := node.ChildByFieldName("type_annotation"); typeAnnotation != nil {
		varType = c.parseType(typeAnnotation.ChildByFieldName("type"))
	}

	initExpr := c.collectExpression(node.ChildByFieldName("value"))

	astNode := &ast.VariableDeclarationStmt{
		AstBase: ast.AstBase{Location: c.nodeLocation(node)},
		Keyword: keyword,
		Name:    name,
		Type:    varType,
		Value:   initExpr,
	}

	if err := c.table.RegisterVariable(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}
