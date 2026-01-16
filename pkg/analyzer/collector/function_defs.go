package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectFunctionDef(node *sitter.Node) *ast.FunctionDefStmt {
	var name string
	var genericParams []string
	var signature *types.FunctionType
	var clauses []*ast.FunctionClause
	isPublic := false
	isPure := false
	isAsync := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "function_signature":
			name, genericParams, signature, isPure, isAsync = c.collectFunctionSignature(child)
		case "function_clause":
			clauses = append(clauses, c.collectFunctionClause(child))
		}
	}

	astNode := &ast.FunctionDefStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Signature:     signature,
		Clauses:       clauses,
		IsPublic:      isPublic,
		IsPure:        isPure,
		IsAsync:       isAsync,
	}

	if err := c.table.RegisterFunction(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

func (c *Collector) collectFunctionClause(node *sitter.Node) *ast.FunctionClause {
	var parameters []ast.Pattern
	var guard ast.Expression
	var body ast.Expression

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "parameter_list":
			parameters = c.collectParameterPatterns(child)
		case "guard":
			guard = c.collectExpression(child)
		case "body":
			body = c.collectExpression(child)
		}
	}

	return &ast.FunctionClause{
		AstBase:    ast.AstBase{Location: c.nodeLocation(node)},
		Parameters: parameters,
		Guard:      guard,
		Body:       body,
	}
}
