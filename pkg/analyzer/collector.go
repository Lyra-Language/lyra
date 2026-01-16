package analyzer

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
		case "statement":
			stmt = c.collectStatement(child)
		}

		if stmt != nil {
			c.ast.Statements = append(c.ast.Statements, stmt)
		}
	}
}

func (c *Collector) collectStatement(node *sitter.Node) ast.AstNode {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "function_definition":
			return c.collectFunctionDef(child)
		case "declaration", "const_declaration":
			return c.collectVariableDeclaration(child)
		case "expression_statement":
			return c.collectExpressionStatement(child)
		}
	}
	return nil
}

func (c *Collector) collectExpressionStatement(node *sitter.Node) *ast.ExpressionStmt {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			expr := c.collectExpression(child)
			if expr != nil {
				return &ast.ExpressionStmt{
					AstBase:    ast.AstBase{Location: c.nodeLocation(node)},
					Expression: expr,
				}
			}
		}
	}
	return nil
}

func (c *Collector) collectTypeDeclaration(node *sitter.Node) *ast.TypeDeclarationStmt {
	// type_declaration contains struct_type, data_type, trait_declaration, etc.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "struct_type":
			return c.collectStructType(child)
		case "data_type":
			return c.collectDataType(child)
		case "trait_declaration":
			c.collectTraitDeclaration(child)
			return nil // Traits stay as symbols for now
		case "trait_implementation":
			c.collectTraitImpl(child)
			return nil // Trait impls stay as symbols for now
		}
	}
	return nil
}

func (c *Collector) collectStructType(node *sitter.Node) *ast.TypeDeclarationStmt {
	var name string
	var genericParams []string
	fields := make(map[string]types.Type)
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "struct_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "struct_type_body":
			fields = c.collectStructFields(child)
		}
	}

	astNode := &ast.TypeDeclarationStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.StructType{
			Name:   name,
			Fields: fields,
		},
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

func (c *Collector) collectDataType(node *sitter.Node) *ast.TypeDeclarationStmt {
	var name string
	var genericParams []string
	constructors := make(map[string]types.DataTypeConstructor)
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "data_type_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "data_type_constructor":
			ctorName, ctor := c.collectDataConstructor(child)
			constructors[ctorName] = ctor
		}
	}

	astNode := &ast.TypeDeclarationStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.DataType{
			Name:         name,
			Constructors: constructors,
		},
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

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

func (c *Collector) collectExpression(node *sitter.Node) ast.Expression {
	if node == nil {
		return nil
	}

	loc := c.nodeLocation(node)

	switch node.Kind() {
	case "integer", "integer_literal":
		value, _ := strconv.ParseInt(c.nodeText(node), 10, 64)
		return &ast.IntegerLiteral{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    value,
		}

	case "float", "float_literal":
		value, _ := strconv.ParseFloat(c.nodeText(node), 64)
		return &ast.FloatLiteral{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    value,
		}

	case "string", "string_literal":
		return &ast.StringLiteral{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    c.nodeText(node),
		}

	case "boolean", "boolean_literal":
		value := c.nodeText(node) == "true"
		return &ast.BooleanLiteral{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    value,
		}

	case "identifier":
		return &ast.Identifier{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Name:     c.nodeText(node),
		}
	}

	// For wrapper nodes, recurse into the first named child
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return c.collectExpression(child)
		}
	}

	return nil
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

func (c *Collector) collectStructFields(node *sitter.Node) map[string]types.Type {
	fields := make(map[string]types.Type)
	var currentFieldName string
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "field_name":
			currentFieldName = c.nodeText(child)
		case "field_type":
			if currentFieldName != "" {
				fields[currentFieldName] = c.parseType(child)
				currentFieldName = ""
			}
		}
	}
	return fields
}

func (c *Collector) collectDataConstructor(node *sitter.Node) (string, types.DataTypeConstructor) {
	var name string
	ctor := types.DataTypeConstructor{
		Params: make([]types.Type, 0),
		Fields: make(map[string]types.Type),
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

func (c *Collector) parseType(node *sitter.Node) types.Type {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "signed_integer_type", "unsigned_integer_type":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.nodeText(node))}
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
		return c.parseArrayType(node)
	case "map_type":
		return c.parseMapType(node)
	}
	c.errors = append(c.errors, fmt.Errorf("parseType: unknown type node kind: %s", node.Kind()))
	return nil
}

func (c *Collector) parseArrayType(node *sitter.Node) types.Type {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			return types.ArrayType{ElementType: c.parseType(child)}
		}
	}
	return types.ArrayType{}
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

func (c *Collector) parseMapType(node *sitter.Node) types.Type {
	mt := types.MapType{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "key_type":
			for j := uint(0); j < child.ChildCount(); j++ {
				if child.Child(j).IsNamed() {
					mt.KeyType = c.parseType(child.Child(j))
				}
			}
		case "value_type":
			for j := uint(0); j < child.ChildCount(); j++ {
				if child.Child(j).IsNamed() {
					mt.ValueType = c.parseType(child.Child(j))
				}
			}
		}
	}
	return mt
}

func (c *Collector) collectParameterPatterns(node *sitter.Node) []ast.Pattern {
	patterns := make([]ast.Pattern, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter" {
			patterns = append(patterns, c.collectPattern(child))
		}
	}
	return patterns
}

func (c *Collector) collectPattern(node *sitter.Node) ast.Pattern {
	pattern := node.ChildByFieldName("pattern")
	if pattern != nil {
		loc := c.nodeLocation(pattern)
		switch pattern.Kind() {
		case "identifier":
			return &ast.IdentifierPattern{
				PatternBase: ast.PatternBase{Location: loc},
				Name:        c.nodeText(pattern),
			}
		case "literal_pattern":
			return &ast.LiteralPattern{
				PatternBase: ast.PatternBase{Location: loc},
				Value:       c.nodeText(pattern),
			}
		}
	}
	return nil
}

// Trait handling (keeping symbols for now since traits are more complex)

func (c *Collector) collectTraitDeclaration(node *sitter.Node) *symbols.TraitSymbol {
	var name string
	var genericParams []string
	methods := make(map[string]*symbols.TraitMethodSymbol)
	var isPublic bool

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "trait_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "trait_method":
			method := c.collectTraitMethod(child)
			methods[method.Name] = method
		}
	}

	symbol := &symbols.TraitSymbol{
		Name:          name,
		GenericParams: genericParams,
		Methods:       methods,
		Location:      c.nodeLocation(node),
		IsPublic:      isPublic,
	}

	if err := c.table.RegisterTrait(symbol); err != nil {
		c.errors = append(c.errors, err)
	}
	return symbol
}

func (c *Collector) collectTraitMethod(node *sitter.Node) *symbols.TraitMethodSymbol {
	var methodName string
	var methodType *types.FunctionType

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "method_name":
			methodName = c.nodeText(child)
		case "function_type":
			methodType = c.parseFunctionType(child)
		}
	}
	return &symbols.TraitMethodSymbol{
		Name:      methodName,
		Signature: methodType,
		Location:  c.nodeLocation(node),
	}
}

func (c *Collector) collectTraitImpl(node *sitter.Node) *symbols.TraitImplSymbol {
	var traitName string
	var forType types.Type
	methods := make(map[string]*symbols.TraitMethodImplSymbol)

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "trait_name":
			traitName = c.nodeText(child)
		case "type":
			forType = c.parseType(child)
		case "trait_method_implementation":
			method := c.collectTraitMethodImpl(child)
			methods[method.Name] = method
		}
	}

	sym := &symbols.TraitImplSymbol{
		TraitName: traitName,
		ForType:   forType,
		Methods:   methods,
		Location:  c.nodeLocation(node),
	}

	if err := c.table.RegisterTraitImpl(sym); err != nil {
		c.errors = append(c.errors, err)
	}
	return sym
}

func (c *Collector) collectTraitMethodImpl(node *sitter.Node) *symbols.TraitMethodImplSymbol {
	var methodName string
	var methodClause *ast.FunctionClause

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "method_name":
			methodName = c.nodeText(child)
		case "function_clause":
			methodClause = c.collectFunctionClause(child)
		}
	}
	return &symbols.TraitMethodImplSymbol{
		Name:     methodName,
		Impl:     methodClause,
		Location: c.nodeLocation(node),
	}
}
