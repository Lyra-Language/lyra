package collector

/*
Collector walks the tree-sitter CST and builds an AST representation of the program.
It also populates a symbol table for quick name lookups.
The AST nodes serve as the source of truth - the symbol table just indexes them.
*/

import (
	"fmt"
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Collector walks the CST and builds an AST + symbol table
type Collector struct {
	source []byte
	table  *symbols.SymbolTable
	ast    *ast.Program
	errors []error
}

func NewCollector(source []byte) *Collector {
	return &Collector{
		source: source,
		table:  symbols.NewSymbolTable(),
		ast:    &ast.Program{},
		errors: make([]error, 0),
	}
}

// Collect walks the entire tree and returns the AST, symbol table, and any errors
func (c *Collector) Collect(root *sitter.Node) (*ast.Program, *symbols.SymbolTable, []error) {
	c.walkProgram(root)
	return c.ast, c.table, c.errors
}

func (c *Collector) walkProgram(node *sitter.Node) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		var stmt ast.AstNode

		switch child.Kind() {
		case "type_declaration":
			stmt = c.collectTypeDeclaration(child)
		case "function_definition":
			stmt = c.collectFunctionDef(child)
		case "declaration", "const_declaration":
			stmt = c.collectVariableDeclaration(child)
		case "expression_statement":
			stmt = c.collectExpressionStatement(child)
		}

		if stmt != nil {
			c.ast.Statements = append(c.ast.Statements, stmt)
		}
	}
}

// Helper methods

func (c *Collector) nodeText(node *sitter.Node) string {
	return string(c.source[node.StartByte():node.EndByte()])
}

func (c *Collector) nodeLocation(node *sitter.Node) ast.Location {
	start := node.StartPosition()
	end := node.EndPosition()
	return ast.Location{
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}

func (c *Collector) collectGenericParams(node *sitter.Node) []string {
	params := make([]string, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "generic_type" {
			params = append(params, c.nodeText(child))
		}
	}
	return params
}

func (c *Collector) collectDataConstructor(node *sitter.Node) (string, types.DataTypeConstructor) {
	var name string
	ctor := types.DataTypeConstructor{
		Params: make([]types.Type, 0),
		Fields: make(map[string]types.StructField),
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "data_type_constructor_name":
			name = c.nodeText(child)
		case "generic_type", "user_defined_type_name", "signed_integer_type", "string_type", "boolean_type", "float_type":
			ctor.Params = append(ctor.Params, c.parseType(child))
		case "struct_type_body":
			ctor.Fields = c.collectStructFields(child)
		}
	}

	ctor.Name = name
	return name, ctor
}

func (c *Collector) collectStructFields(node *sitter.Node) map[string]types.StructField {
	fields := make(map[string]types.StructField)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "struct_member" {
			field_type_node := child.ChildByFieldName("field_type")
			var field_type types.Type
			if field_type_node != nil {
				field_type = c.parseType(field_type_node.Child(0))
			}
			field_name := c.nodeText(child.ChildByFieldName("field_name"))
			default_value := c.collectExpression(child.ChildByFieldName("default_field_value"))
			fields[field_name] = types.StructField{
				Name:         field_name,
				Type:         field_type,
				DefaultValue: default_value,
			}
		}
	}
	return fields
}

func (c *Collector) collectFunctionSignature(node *sitter.Node) (name string, genericParams []string, sig *types.FunctionType, isPure, isAsync bool) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		text := c.nodeText(child)
		switch child.Kind() {
		case "identifier":
			name = text
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "function_type":
			sig = c.parseFunctionType(child)
		default:
			switch text {
			case "pure":
				isPure = true
			case "async":
				isAsync = true
			}
		}
	}
	return name, genericParams, sig, isPure, isAsync
}

// allocation is only used for array types
func (c *Collector) parseType(node *sitter.Node, allocation ...types.AllocationModifier) types.Type {
	var alloc types.AllocationModifier
	if len(allocation) > 0 {
		alloc = allocation[0]
	}
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "integer":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.nodeText(node.ChildByFieldName("type")))}
	case "signed_integer_type", "unsigned_integer_type":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.nodeText(node))}
	case "float":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.nodeText(node.ChildByFieldName("type")))}
	case "float_type":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.nodeText(node))}
	case "string_type":
		return types.PrimitiveType{Name: types.String}
	case "boolean_type":
		return types.PrimitiveType{Name: types.Bool}
	case "user_defined_type_name":
		return types.UnresolvedType{Name: c.nodeText(node)}
	case "generic_type":
		return types.GenericType{Name: c.nodeText(node)}
	case "array_type":
		return c.parseArrayType(node, alloc)
	case "constrained_type":
		return c.parseConstrainedType(node)
	case "allocated_type":
		return c.parseAllocatedType(node)
	}
	c.errors = append(c.errors, fmt.Errorf("parseType: unknown type node kind: %s", node.Kind()))
	return nil
}

func (c *Collector) parseArrayType(node *sitter.Node, allocation types.AllocationModifier) types.Type {
	typeNode := node.ChildByFieldName("element_type")
	if typeNode == nil {
		c.errors = append(c.errors, fmt.Errorf("parseArrayType: element type node is nil"))
		return nil
	}

	elementType := c.parseType(typeNode)
	if elementType == nil {
		c.errors = append(c.errors, fmt.Errorf("parseArrayType: element type is nil"))
		return nil
	}

	sizeNode := node.ChildByFieldName("size")
	if sizeNode != nil {
		sizeString := c.nodeText(sizeNode)
		sizeInt, err := strconv.ParseInt(sizeString, 10, 64)
		if err != nil {
			c.errors = append(c.errors, fmt.Errorf("parseArrayType: invalid size: %s", sizeString))
			return nil
		}
		return types.StaticArrayType{ElementType: elementType, Size: int(sizeInt), Allocation: allocation}
	}

	return types.DynamicArrayType{ElementType: elementType, Allocation: allocation}
}

func (c *Collector) parseConstrainedType(node *sitter.Node) types.Type {
	constraints := make([]types.Constraint, 0)
	constraintsNode := node.ChildByFieldName("constraints")
	if constraintsNode != nil {
		constraints = c.collectConstraints(constraintsNode)
	}
	return &types.ConstrainedType{
		Name:        c.nodeText(node.ChildByFieldName("name")),
		Type:        c.parseType(node.ChildByFieldName("type")),
		Constraints: constraints,
	}
}

func (c *Collector) parseFunctionType(node *sitter.Node) *types.FunctionType {
	ft := &types.FunctionType{
		ParameterTypes: make([]types.ParameterType, 0),
	}

	parameterTypes := node.ChildByFieldName("parameter_types")
	if parameterTypes != nil {
		for i := uint(0); i < parameterTypes.ChildCount(); i++ {
			child := parameterTypes.Child(i)
			if child.Kind() == "parameter_type" {
				ft.ParameterTypes = append(ft.ParameterTypes, c.parseParameterType(child))
			}
		}
	}
	returnType := node.ChildByFieldName("return_type")
	if returnType != nil {
		ft.ReturnType = c.parseType(returnType)
	}

	return ft
}

func (c *Collector) parseParameterType(node *sitter.Node) types.ParameterType {
	modifier := types.Modifier("")
	modifier_node := node.ChildByFieldName("modifier")
	if modifier_node != nil {
		modifier = types.Modifier(c.nodeText(modifier_node))
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		c.errors = append(c.errors, fmt.Errorf("parseParameterType: type node is nil"))
		return types.ParameterType{}
	}
	return types.ParameterType{
		Modifier: modifier,
		Type:     c.parseType(typeNode),
	}
}

func (c *Collector) collectPattern(patternNode *sitter.Node) ast.Pattern {
	loc := c.nodeLocation(patternNode)
	switch patternNode.Kind() {
	case "identifier":
		return &ast.IdentifierPattern{
			PatternBase: ast.PatternBase{Location: loc},
			Name:        c.nodeText(patternNode),
		}
	case "literal_pattern":
		return &ast.LiteralPattern{
			PatternBase: ast.PatternBase{Location: loc},
			Value:       c.nodeText(patternNode),
		}
	}
	c.errors = append(c.errors, fmt.Errorf("collectPattern: unknown pattern node kind: %s", patternNode.Kind()))
	return nil
}

func (c *Collector) parseAllocatedType(node *sitter.Node) types.Type {
	allocation := c.collectAllocationModifier(node.ChildByFieldName("allocation"))
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		c.errors = append(c.errors, fmt.Errorf("parseAllocatedType: type node is nil"))
		return nil
	}
	return c.parseType(typeNode, allocation)
}
