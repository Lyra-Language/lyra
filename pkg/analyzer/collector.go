package analyzer

/*
Collector walks the STree and builds the symbol table
This is a top-down approach, starting from the program node and walking down the AST.
The symbol table is built as we go, and we can use the symbol table to check the program.
The symbol table is used to check the program for errors.
*/

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Collector walks the AST and builds the symbol table
type Collector struct {
	source []byte
	table  *symbols.SymbolTable
	errors []error
}

func NewCollector(source []byte) *Collector {
	return &Collector{
		source: source,
		table:  symbols.NewSymbolTable(),
		errors: make([]error, 0),
	}
}

// Collect walks the entire AST and returns the populated symbol table
func (c *Collector) Collect(root *sitter.Node) (*symbols.SymbolTable, []error) {
	c.walkProgram(root)
	return c.table, c.errors
}

func (c *Collector) walkProgram(node *sitter.Node) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "type_declaration":
			c.collectTypeDeclaration(child)
		case "expression":
			c.collectExpression(child)
		case "statement":
			c.collectStatement(child)
		// Handle concrete statement types directly (due to supertypes)
		case "function_definition":
			c.collectFunctionDef(child)
		case "declaration":
			c.collectDeclaration(child)
		case "const_declaration":
			// handle top-level const declarations if needed
		}
	}
}

func (c *Collector) collectTypeDeclaration(node *sitter.Node) {
	// type_declaration contains struct_type, data_type, trait_declaration, etc.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "struct_type":
			c.collectStructType(child)
		case "data_type":
			c.collectDataType(child)
		case "trait_declaration":
			c.collectTraitDeclaration(child)
		case "trait_implementation":
			c.collectTraitImpl(child)
		}
	}
}

func (c *Collector) collectStructType(node *sitter.Node) {
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

	sym := &symbols.TypeSymbol{
		Name:          name,
		GenericParams: genericParams,
		Type: types.StructType{
			Name:   name,
			Fields: fields,
		},
		Location: c.nodeLocation(node),
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(sym); err != nil {
		c.errors = append(c.errors, err)
	}
}

func (c *Collector) collectDataType(node *sitter.Node) {
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

	sym := &symbols.TypeSymbol{
		Name:          name,
		GenericParams: genericParams,
		Type: types.DataType{
			Name:         name,
			Constructors: constructors,
		},
		Location: c.nodeLocation(node),
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(sym); err != nil {
		c.errors = append(c.errors, err)
	}
}

func (c *Collector) collectExpression(node *sitter.Node) {

}

func (c *Collector) collectDeclaration(node *sitter.Node) {
	var keyword string
	var name string
	var varType types.Type
	var initExpr *symbols.ExpressionSymbol

	keyword = c.nodeText(node.ChildByFieldName("keyword"))
	name = c.nodeText(node.ChildByFieldName("name"))
	typeAnnotation := node.ChildByFieldName("type_annotation")
	if typeAnnotation != nil {
		varType = c.parseType(typeAnnotation.ChildByFieldName("type"))
	}
	initExpr = c.collectExpressionSymbol(node.ChildByFieldName("value"))

	sym := &symbols.VariableSymbol{
		Name:           name,
		Type:           varType,
		InitExpression: initExpr,
		Location:       c.nodeLocation(node),
		IsMutable:      keyword == "var",
		IsConstant:     keyword == "const",
	}

	if err := c.table.GlobalScope.Define(sym); err != nil {
		c.errors = append(c.errors, err)
	}
}

func (c *Collector) collectStatement(node *sitter.Node) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "function_definition":
			c.collectFunctionDef(child)
		case "declaration", "const_declaration":
			c.collectDeclaration(child)
		}
	}
}

func (c *Collector) collectFunctionDef(node *sitter.Node) {
	var name string
	var genericParams []string
	var signature *types.FunctionType
	var functionClauses []*symbols.FunctionClauseSymbol
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
			functionClauses = append(functionClauses, c.collectFunctionClauseSymbol(child))
		}
	}

	sym := &symbols.FunctionSymbol{
		Name:          name,
		GenericParams: genericParams,
		Signature:     signature,
		Location:      c.nodeLocation(node),
		IsPublic:      isPublic,
		IsPure:        isPure,
		IsAsync:       isAsync,
		Clauses:       functionClauses,
	}

	if err := c.table.RegisterFunction(sym); err != nil {
		c.errors = append(c.errors, err)
	}
}

// Helper methods

func (c *Collector) nodeText(node *sitter.Node) string {
	return string(c.source[node.StartByte():node.EndByte()])
}

func (c *Collector) nodeLocation(node *sitter.Node) symbols.Location {
	start := node.StartPosition()
	end := node.EndPosition()
	return symbols.Location{
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
	// Walk through the struct body and extract field_name -> field_type pairs
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
			// Simple constructor like Some(t) or Leaf(Int)
			ctor.Params = append(ctor.Params, c.parseType(child))
		case "struct_type_body":
			// Record-style constructor like Node { left: Tree, value: t }
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

// parseType converts a type AST node to a types.Type
func (c *Collector) parseType(node *sitter.Node) types.Type {
	switch node.Kind() {
	case "signed_integer_type", "unsigned_integer_type":
		return types.PrimitiveType{Name: c.nodeText(node)}
	case "float_type":
		return types.PrimitiveType{Name: c.nodeText(node)}
	case "string_type":
		return types.PrimitiveType{Name: "Str"}
	case "boolean_type":
		return types.PrimitiveType{Name: "Bool"}
	case "generic_type":
		return types.GenericType{Name: c.nodeText(node)}
	case "array_type":
		// []t format
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.IsNamed() {
				return types.ArrayType{ElementType: c.parseType(child)}
			}
		}
	case "user_defined_type_name":
		return types.UnresolvedType{Name: c.nodeText(node)}
	case "function_type":
		return c.parseFunctionType(node)
	case "map_type":
		return c.parseMapType(node)
	case "field_type":
		// field_type wraps the actual type
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.IsNamed() {
				return c.parseType(child)
			}
		}
	}
	return nil
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
		// Throw an error
		panic(fmt.Sprintf("parseParameterType: type node is nil for node: %v", node))
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
	var methods = make(map[string]*symbols.TraitMethodImplSymbol)

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
	var methodClause *symbols.FunctionClauseSymbol

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "method_name":
			methodName = c.nodeText(child)
		case "function_clause":
			methodClause = c.collectFunctionClauseSymbol(child)
		}
	}
	return &symbols.TraitMethodImplSymbol{
		Name:     methodName,
		Impl:     methodClause,
		Location: c.nodeLocation(node),
	}
}

func (c *Collector) collectFunctionClauseSymbol(node *sitter.Node) *symbols.FunctionClauseSymbol {
	var parameterPatterns []symbols.Pattern
	var guard *symbols.GuardSymbol
	var body *symbols.FunctionBodySymbol

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "parameter_list":
			parameterPatterns = c.collectParameterPatterns(child)
		case "guard":
			guard = c.collectGuardSymbol(child)
		case "body":
			body = c.collectFunctionClauseBody(child)
		}
	}
	return &symbols.FunctionClauseSymbol{
		ParameterPatterns: parameterPatterns,
		Guard:             guard,
		Body:              body,
		Location:          c.nodeLocation(node),
	}
}

func (c *Collector) collectParameterPatterns(node *sitter.Node) []symbols.Pattern {
	parameterPatterns := make([]symbols.Pattern, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter" {
			parameterPatterns = append(parameterPatterns, c.collectPattern(child))
		}
	}
	return parameterPatterns
}

func (c *Collector) collectPattern(node *sitter.Node) symbols.Pattern {
	pattern := node.ChildByFieldName("pattern")
	if pattern != nil {
		switch pattern.Kind() {
		case "identifier":
			return symbols.IdentifierPattern{Name: c.nodeText(pattern)}
		case "literal_pattern":
			return symbols.LiteralPattern{Value: c.nodeText(pattern)}
		}
	}
	return nil
}

func (c *Collector) collectGuardSymbol(node *sitter.Node) *symbols.GuardSymbol {
	var expression string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "expression" {
			expression = c.nodeText(child)
		}
	}
	return &symbols.GuardSymbol{Expression: expression, Location: c.nodeLocation(node)}
}

func (c *Collector) collectFunctionClauseBody(node *sitter.Node) *symbols.FunctionBodySymbol {
	body := &symbols.FunctionBodySymbol{
		Location:   c.nodeLocation(node),
		Block:      nil,
		Expression: nil,
	}
	block := node.ChildByFieldName("block")
	if block != nil {
		body.Block = c.collectFunctionClauseBlock(block)
	}
	expression := node.ChildByFieldName("expression")
	if expression != nil {
		body.Expression = c.collectExpressionSymbol(expression)
	}
	return body
}

func (c *Collector) collectFunctionClauseBlock(node *sitter.Node) *symbols.BlockSymbol {
	var statements []*symbols.StatementSymbol
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "statement" {
			statements = append(statements, c.collectStatementSymbol(child))
		}
	}
	return &symbols.BlockSymbol{Statements: statements, Location: c.nodeLocation(node)}
}

func (c *Collector) collectExpressionSymbol(node *sitter.Node) *symbols.ExpressionSymbol {
	switch node.Kind() {
	case "integer":
		return &symbols.ExpressionSymbol{Type: types.PrimitiveType{Name: "Int"}, Location: c.nodeLocation(node)}
	case "float":
		return &symbols.ExpressionSymbol{Type: types.PrimitiveType{Name: "Float"}, Location: c.nodeLocation(node)}
	case "string":
		return &symbols.ExpressionSymbol{Type: types.PrimitiveType{Name: "Str"}, Location: c.nodeLocation(node)}
	case "boolean":
		return &symbols.ExpressionSymbol{Type: types.PrimitiveType{Name: "Bool"}, Location: c.nodeLocation(node)}
	}
	return nil
}

func (c *Collector) collectStatementSymbol(node *sitter.Node) *symbols.StatementSymbol {
	statement := &symbols.StatementSymbol{
		Expression: nil,
		Location:   c.nodeLocation(node),
	}
	expression := node.ChildByFieldName("expression")
	if expression != nil {
		statement.Expression = c.collectExpressionSymbol(expression)
	}
	return statement
}
