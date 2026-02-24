package collector

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectVariableDeclaration(node *sitter.Node) *ast.VarDeclStmt {
	keyword := c.nodeText(node.ChildByFieldName("keyword"))
	name := c.nodeText(node.ChildByFieldName("name"))

	var varType types.Type
	if typeAnnotation := node.ChildByFieldName("type_annotation"); typeAnnotation != nil {
		annotationText := strings.TrimSpace(c.nodeText(typeAnnotation))
		if strings.HasPrefix(annotationText, ":") {
			typeStr := strings.TrimSpace(annotationText[1:])
			if len(typeStr) >= 2 && typeStr[0] == '(' && typeStr[len(typeStr)-1] == ')' {
				varType = c.parseTupleTypeFromString(typeStr)
			}
		}
		if varType == nil {
			typeNode := typeAnnotation.ChildByFieldName("type")
			if typeNode == nil {
				for i := uint(0); i < typeAnnotation.ChildCount(); i++ {
					child := typeAnnotation.Child(i)
					if child.IsNamed() {
						text := c.nodeText(child)
						if len(text) >= 2 && text[0] == '(' && text[len(text)-1] == ')' {
							typeNode = child
							break
						}
					}
				}
			}
			varType = c.parseType(typeNode)
			if varType == nil {
				typeStr := strings.TrimSpace(annotationText[1:])
				if len(typeStr) >= 2 && typeStr[0] == '(' && typeStr[len(typeStr)-1] == ')' {
					varType = c.parseTupleTypeFromString(typeStr)
				}
			}
		}
		if varType == nil {
			// Type annotation may span only ":" when type didn't parse; get type from declaration text (between ":" and "=")
			declText := c.nodeText(node)
			if eq := strings.Index(declText, "="); eq > 0 {
				afterColon := declText
				if colon := strings.Index(declText, ":"); colon >= 0 && colon < eq {
					afterColon = declText[colon+1 : eq]
				}
				typeStr := strings.TrimSpace(afterColon)
				if len(typeStr) >= 2 && typeStr[0] == '(' && typeStr[len(typeStr)-1] == ')' {
					varType = c.parseTupleTypeFromString(typeStr)
				}
			}
		}
	}
	// When type_annotation is missing (e.g. type didn't parse), try to get tuple type from declaration text
	if varType == nil {
		declText := c.nodeText(node)
		if eq := strings.Index(declText, "="); eq > 0 {
			if colon := strings.Index(declText, ":"); colon >= 0 && colon < eq {
				typeStr := strings.TrimSpace(declText[colon+1 : eq])
				if len(typeStr) >= 2 && typeStr[0] == '(' && typeStr[len(typeStr)-1] == ')' {
					varType = c.parseTupleTypeFromString(typeStr)
				}
			}
		}
	}

	initExpr := c.collectExpression(node.ChildByFieldName("value"))

	// Infer variable type from initializer only when no type annotation was given
	if varType == nil {
		if arrayType, ok := initExpr.GetType().(types.StaticArrayType); ok {
			varType = arrayType
		} else if tupleType, ok := initExpr.GetType().(types.TupleType); ok {
			varType = tupleType
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
