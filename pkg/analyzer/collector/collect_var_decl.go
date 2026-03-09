package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectVariableDeclaration(node *sitter.Node) *ast.VarDeclStmt {
	keyword := c.nodeText(node.ChildByFieldName("keyword"))
	name := c.nodeText(node.ChildByFieldName("name"))

	var varType types.Type
	if typeAnnotation := node.ChildByFieldName("type_annotation"); typeAnnotation != nil {
		varType = c.parseType(typeAnnotation.ChildByFieldName("type"))
	}

	valueNode := node.ChildByFieldName("value")
	var initExpr ast.Expression = nil
	if valueNode != nil {
		initExpr = c.collectExpression(valueNode)
	}

	// Infer variable type from initializer only when no type annotation was given
	if varType == nil && initExpr != nil {
		if arrayType, ok := initExpr.GetType().(types.StaticArrayType); ok {
			varType = arrayType
		} else if tupleType, ok := initExpr.GetType().(types.TupleType); ok {
			varType = tupleType
		} else if rangeType, ok := initExpr.GetType().(types.RangeType); ok {
			varType = rangeType
		}
	}

	// When variable has explicit tuple type and initializer is a tuple literal, propagate type to the literal
	if tupleType, ok := varType.(types.TupleType); ok {
		if tupleLit, ok := initExpr.(*ast.TupleLiteralExpr); ok {
			tupleLit.ExprBase.Type = tupleType
		}
	}

	astNode := &ast.VarDeclStmt{
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
