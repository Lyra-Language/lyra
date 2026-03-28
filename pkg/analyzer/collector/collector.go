package collector

/*
Collector walks the tree-sitter CST and builds an AST representation of the program.
It also populates a symbol table for quick name lookups.
The AST nodes serve as the source of truth - the symbol table just indexes them.
*/

import (
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/declarations"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/typedecls"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Re-export error types from collctx so callers don't need to import both packages.
type CollectorError = collctx.CollectorError
type CollectorErrorSeverity = collctx.ErrorSeverity

const (
	CollectorErrorSeverityError   = collctx.SeverityError
	CollectorErrorSeverityWarning = collctx.SeverityWarning
	CollectorErrorSeverityInfo    = collctx.SeverityInfo
)

// Collector walks the CST and builds an AST + symbol table.
type Collector struct {
	source []byte
	table  *symbols.SymbolTable
	ast    *ast.Program
	errors []error
	ctx    *collctx.Ctx
}

func NewCollector(source []byte) *Collector {
	c := &Collector{
		source: source,
		table:  symbols.NewSymbolTable(),
		ast:    &ast.Program{},
		errors: make([]error, 0),
	}
	c.ctx = &collctx.Ctx{
		Source:                    source,
		Errors:                    &c.errors,
		CollectExpr:               c.collectExpression,
		CollectStatement:          c.collectStatement,
		ParseDestructuringPattern: c.ParseDestructuringPattern,
		CollectPattern:            c.collectPattern,
		ParseType:                 func(node *sitter.Node) types.Type { return c.parseType(node) },
		RegisterType:              c.table.RegisterType,
		RegisterFunction:          c.table.RegisterFunction,
		RegisterVariable:          c.table.RegisterVariable,
		CollectGenericParams:      c.collectGenericParams,
	}
	return c
}

// Collect walks the entire tree and returns the AST, symbol table, and any errors.
func (c *Collector) Collect(root *sitter.Node) (*ast.Program, *symbols.SymbolTable, []error) {
	c.walkProgram(root)
	return c.ast, c.table, c.errors
}

func (c *Collector) walkProgram(node *sitter.Node) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)

		stmt := c.collectStatement(child)

		if stmt != nil {
			c.ast.Statements = append(c.ast.Statements, stmt)
		}
	}
}

// collectExpression is the thin wrapper wired into ctx.CollectExpr.
func (c *Collector) collectExpression(node *sitter.Node) ast.Expression {
	return expressions.CollectExpression(node, c.ctx)
}

func (c *Collector) collectStatement(node *sitter.Node) ast.Statement {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "type_declaration":
		return typedecls.CollectTypeDeclaration(node, c.ctx)
	case "function_definition":
		return declarations.CollectFunctionDefinition(node, c.ctx)
	case "declaration", "const_declaration":
		return declarations.CollectVariableDeclaration(node, c.ctx)
	case "destructuring_declaration":
		return declarations.CollectDestructuringDeclaration(node, c.ctx)
	case "if_destructuring_declaration":
		return declarations.CollectDestructuringIfStatement(node, c.ctx)
	case "expression_statement":
		return expressions.CollectExpressionStatement(node, c.ctx)
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

func (c *Collector) addError(node *sitter.Node, severity CollectorErrorSeverity, format string, args ...any) {
	c.ctx.AddError(node, severity, format, args...)
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
	case "signed_integer_type", "unsigned_integer_type", "float_type":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.nodeText(node))}
	case "string_type":
		return types.PrimitiveType{Name: types.String}
	case "boolean_type":
		return types.PrimitiveType{Name: types.Bool}
	case "user_defined_type_name":
		return types.UnresolvedType{Name: c.nodeText(node)}
	case "generic_type":
		return types.GenericType{Name: c.nodeText(node)}
	case "parameterized_type":
		return c.parseParameterizedType(node)
	case "array_type":
		return c.parseArrayType(node, alloc)
	case "constrained_type":
		return c.parseConstrainedType(node)
	case "allocated_type":
		return c.parseAllocatedType(node)
	case "anonymous_tuple_type":
		return c.parseAnonymousTupleType(node)
	}
	c.addError(node, CollectorErrorSeverityError, "parseType: unknown type node kind: %s", node.Kind())
	return nil
}

func (c *Collector) parseParameterizedType(node *sitter.Node) types.Type {
	name := c.nodeText(node.ChildByFieldName("name"))
	typeArgumentsNode := node.ChildByFieldName("type_arguments")
	if typeArgumentsNode == nil {
		c.addError(node, CollectorErrorSeverityError, "parseParameterizedType: type arguments node is nil")
		return nil
	}
	typeArguments := make([]types.Type, 0)
	for i := uint(0); i < typeArgumentsNode.ChildCount(); i++ {
		child := typeArgumentsNode.Child(i)
		if child.IsNamed() {
			typeArguments = append(typeArguments, c.parseType(child))
		}
	}
	if len(typeArguments) == 0 {
		return nil
	}
	return types.ParameterizedType{Name: name, TypeArguments: typeArguments}
}

func (c *Collector) parseAnonymousTupleType(node *sitter.Node) types.Type {
	if node.Child(0).Kind() == "tuple_type_body" {
		elements := typedecls.CollectTupleTypeBody(node.Child(0), c.ctx)
		return types.TupleType{Name: "?", Elements: elements}
	}
	c.addError(node, CollectorErrorSeverityError, "parseAnonymousTupleType: unknown type node kind: %s", node.Child(0).Kind())
	return nil
}

func (c *Collector) parseArrayType(node *sitter.Node, allocation types.AllocationModifier) types.Type {
	typeNode := node.ChildByFieldName("element_type")
	if typeNode == nil {
		c.addError(node, CollectorErrorSeverityError, "parseArrayType: element type node is nil")
		return nil
	}

	elementType := c.parseType(typeNode)
	if elementType == nil {
		c.addError(node, CollectorErrorSeverityError, "parseArrayType: element type is nil")
		return nil
	}

	sizeNode := node.ChildByFieldName("size")
	if sizeNode != nil {
		sizeString := c.nodeText(sizeNode)
		sizeInt, err := strconv.ParseInt(sizeString, 10, 64)
		if err != nil {
			c.addError(node, CollectorErrorSeverityError, "parseArrayType: invalid size: %s", sizeString)
			return nil
		}
		return types.StaticArrayType{ElementType: elementType, Size: int(sizeInt), Allocation: allocation}
	}

	return types.DynamicArrayType{ElementType: elementType, Allocation: allocation}
}

func (c *Collector) parseConstrainedType(node *sitter.Node) types.Type {
	constraints := make([]types.Constraint, 0)
	if constraintsNode := node.ChildByFieldName("constraints"); constraintsNode != nil {
		constraints = typedecls.CollectConstraints(constraintsNode, c.ctx)
	}
	return &types.ConstrainedType{
		Name:        c.nodeText(node.ChildByFieldName("name")),
		Type:        c.parseType(node.ChildByFieldName("type")),
		Constraints: constraints,
	}
}

func (c *Collector) parseAllocatedType(node *sitter.Node) types.Type {
	allocationNode := node.ChildByFieldName("allocation")
	allocation := types.AllocationModifier(c.nodeText(allocationNode))
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		c.addError(node, CollectorErrorSeverityError, "parseAllocatedType: type node is nil")
		return nil
	}
	return c.parseType(typeNode, allocation)
}

func (c *Collector) ParseDestructuringPattern(patternNode *sitter.Node) ast.Pattern {
	return c.collectPattern(patternNode.Child(0))
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
	case "tuple_pattern":
		return c.collectTuplePattern(patternNode)
	case "array_pattern":
		return c.collectArrayPattern(patternNode)
	case "struct_pattern":
		return c.collectStructPattern(patternNode)
	case "data_pattern":
		return c.collectDataPattern(patternNode)
	}
	c.addError(patternNode, CollectorErrorSeverityError, "collectPattern: unknown pattern node kind: %s", patternNode.Kind())
	return nil
}

func (c *Collector) collectTuplePattern(patternNode *sitter.Node) ast.Pattern {
	loc := c.nodeLocation(patternNode)
	return &ast.TuplePattern{
		PatternBase: ast.PatternBase{Location: loc},
		Elements:    c.collectTuplePatternElements(patternNode),
	}
}

func (c *Collector) collectTuplePatternElements(patternNode *sitter.Node) []ast.Pattern {
	elements := make([]ast.Pattern, 0)
	for i := uint(0); i < patternNode.ChildCount(); i++ {
		child := patternNode.Child(i)
		element := c.collectPatternElement(child)
		if element != nil {
			elements = append(elements, element)
		}
	}
	return elements
}

func (c *Collector) collectArrayPattern(patternNode *sitter.Node) ast.Pattern {
	loc := c.nodeLocation(patternNode)
	elements := c.collectArrayPatternElements(patternNode)
	if len(elements) == 0 {
		c.addError(patternNode, CollectorErrorSeverityError, "collectArrayPattern: no elements in array pattern")
		return nil
	}
	return &ast.ArrayPattern{
		PatternBase: ast.PatternBase{Location: loc},
		Elements:    elements,
	}
}

func (c *Collector) collectArrayPatternElements(patternNode *sitter.Node) []ast.Pattern {
	elements := make([]ast.Pattern, 0)
	for i := uint(0); i < patternNode.ChildCount(); i++ {
		child := patternNode.Child(i)
		element := c.collectPatternElement(child)
		if element != nil {
			elements = append(elements, element)
		}
	}
	return elements
}

func (c *Collector) collectPatternElement(node *sitter.Node) ast.Pattern {
	switch node.Kind() {
	case "identifier", "literal_pattern", "pattern":
		return c.collectPattern(node)
	case "rest_pattern":
		return c.collectRestPattern(node)
	case "wildcard_pattern":
		return c.collectWildcardPattern(node)
	}
	return nil
}

func (c *Collector) collectStructPattern(node *sitter.Node) ast.Pattern {
	loc := c.nodeLocation(node)
	fields := c.collectStructPatternFields(node)
	return &ast.StructPattern{
		PatternBase: ast.PatternBase{Location: loc},
		Fields:      fields,
	}
}

func (c *Collector) collectStructPatternFields(node *sitter.Node) []ast.StructPatternField {
	fields := make([]ast.StructPatternField, 0)
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		field := c.collectStructPatternField(child)
		if field != nil {
			fields = append(fields, *field)
		}
	}
	return fields
}

func (c *Collector) collectStructPatternField(node *sitter.Node) *ast.StructPatternField {
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
			Name:        c.nodeText(nameNode),
			Pattern:     nil,
		}
	}
	structFieldRenameNode := node.ChildByFieldName("struct_field_rename")
	if structFieldRenameNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
			Name:        c.nodeText(structFieldRenameNode.ChildByFieldName("new_name")),
			Pattern:     nil,
		}
	}
	structFieldWithPatternNode := node.ChildByFieldName("struct_field_with_pattern")
	if structFieldWithPatternNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
			Name:        c.nodeText(structFieldWithPatternNode.ChildByFieldName("name")),
			Pattern:     c.collectPattern(structFieldWithPatternNode.ChildByFieldName("pattern")),
		}
	}
	restPatternNode := node.ChildByFieldName("rest_pattern")
	if restPatternNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
			Name:        "...",
			Pattern:     c.collectRestPattern(restPatternNode),
		}
	}
	wildcardPatternNode := node.ChildByFieldName("wildcard_pattern")
	if wildcardPatternNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
			Name:        "_",
			Pattern:     c.collectWildcardPattern(wildcardPatternNode),
		}
	}

	return nil
}

func (c *Collector) collectDataPattern(node *sitter.Node) *ast.DataPattern {
	loc := c.nodeLocation(node)
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectDataPattern: name node is nil")
		return nil
	}
	patternNode := node.ChildByFieldName("pattern")
	var pattern ast.Pattern
	if patternNode != nil {
		pattern = c.collectPattern(patternNode)
	}
	return &ast.DataPattern{
		PatternBase: ast.PatternBase{Location: loc},
		Name:        c.nodeText(nameNode),
		Pattern:     pattern,
	}
}

func (c *Collector) collectRestPattern(node *sitter.Node) ast.Pattern {
	return &ast.RestPattern{
		PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
		Identifier:  c.nodeText(node.ChildByFieldName("identifier")),
	}
}

func (c *Collector) collectWildcardPattern(node *sitter.Node) ast.Pattern {
	return &ast.WildcardPattern{
		PatternBase: ast.PatternBase{Location: c.nodeLocation(node)},
	}
}
