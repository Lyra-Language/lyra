package analyzer

/*
Collector walks the STree and builds the symbol table
This is a top-down approach, starting from the program node and walking down the AST.
The symbol table is built as we go, and we can use the symbol table to check the program.
The symbol table is used to check the program for errors.
*/

import (
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
		case "statement":
			c.collectStatement(child)
		// Handle concrete statement types directly (due to supertypes)
		case "function_definition":
			c.collectFunctionDef(child)
		case "declaration":
			// handle top-level declarations if needed
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

func (c *Collector) collectStatement(node *sitter.Node) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "function_definition":
			c.collectFunctionDef(child)
		case "declaration", "const_declaration":
			// For now, skip local declarations (they need scope tracking)
			// We'll add these when we walk function bodies
		}
	}
}

func (c *Collector) collectFunctionDef(node *sitter.Node) {
	var name string
	var genericParams []string
	var signature *types.FunctionType
	var patterns []*symbols.FunctionPatternSymbol
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
		case "function_pattern":
			patterns = append(patterns, c.collectFunctionPatternSymbol(child))
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
		Patterns:      patterns,
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
	return
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
		Parameters: make([]types.NamedParameter, 0),
	}

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "function_type_parameter_list":
			for j := uint(0); j < child.ChildCount(); j++ {
				param := child.Child(j)
				if param.Kind() == "parameter_type" {
					for k := uint(0); k < param.ChildCount(); k++ {
						typeNode := param.Child(k)
						if typeNode.IsNamed() {
							ft.Parameters = append(
								ft.Parameters,
								types.NamedParameter{Name: c.nodeText(typeNode), Type: c.parseType(typeNode)})
						}
					}
				}
			}
		default:
			// Return type comes after the parameter list
			if child.IsNamed() && child.Kind() != "function_type_parameter_list" {
				ft.ReturnType = c.parseType(child)
			}
		}
	}

	return ft
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
	var methodPattern *symbols.FunctionPatternSymbol

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "method_name":
			methodName = c.nodeText(child)
		case "function_pattern":
			methodPattern = c.collectFunctionPatternSymbol(child)
		}
	}
	return &symbols.TraitMethodImplSymbol{
		Name:     methodName,
		Impl:     methodPattern,
		Location: c.nodeLocation(node),
	}
}

func (c *Collector) collectFunctionPatternSymbol(node *sitter.Node) *symbols.FunctionPatternSymbol {
	var parameters []types.NamedParameter
	var guard *symbols.GuardSymbol
	var body string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "parameter_list":
			parameters = c.collectNamedParameters(child)
		case "guard":
			guard = c.collectGuardSymbol(child)
		case "body":
			body = c.nodeText(child)
		}
	}
	return &symbols.FunctionPatternSymbol{
		Parameters: parameters,
		Guard:      guard,
		Body:       body,
		Location:   c.nodeLocation(node),
	}
}

func (c *Collector) collectNamedParameters(node *sitter.Node) []types.NamedParameter {
	parameters := make([]types.NamedParameter, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter" {
			parameters = append(parameters, c.collectNamedParameter(child))
		}
	}
	return parameters
}

func (c *Collector) collectNamedParameter(node *sitter.Node) types.NamedParameter {
	var name string
	var typeNode *sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter_name" {
			name = c.nodeText(child)
		} else if child.Kind() == "parameter_type" {
			typeNode = child
		}
	}

	if typeNode == nil {
		return types.NamedParameter{Name: name, Type: nil}
	}

	return types.NamedParameter{Name: name, Type: c.parseType(typeNode)}
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
