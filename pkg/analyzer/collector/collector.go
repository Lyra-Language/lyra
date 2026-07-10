package collector

/*
Collector walks the tree-sitter CST and builds an AST representation of the program.
It also populates a symbol table for quick name lookups.
The AST nodes serve as the source of truth - the symbol table just indexes them.
*/

import (
	"reflect"
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/declarations"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/statements"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/typedecls"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

var _ collector_ctx.Collector = (*Collector)(nil)

const (
	CollectorErrorSeverityError   = diag.SeverityError
	CollectorErrorSeverityWarning = diag.SeverityWarning
	CollectorErrorSeverityInfo    = diag.SeverityInfo
)

// Collector walks the CST and builds an AST + symbol table.
type Collector struct {
	source     []byte
	table      *symbols.SymbolTable
	scopeTable *symbols.ScopeTable
	ast        *ast.Program
	errors     []error
	ctx        *collector_ctx.Ctx
}

func NewCollector(source []byte) *Collector {
	c := &Collector{
		source:     source,
		table:      symbols.NewSymbolTable(),
		scopeTable: symbols.NewScopeTable(),
		ast:        &ast.Program{},
		errors:     []error{},
	}
	c.ctx = collector_ctx.NewCtx(source, c, &c.errors)
	c.ctx.ScopeTable = c.scopeTable
	return c
}

// Collect walks the entire tree and returns the AST, symbol table, scope table, and any errors.
func (c *Collector) Collect(root *sitter.Node) (*ast.Program, *symbols.SymbolTable, *symbols.ScopeTable, []error) {
	c.walkProgram(root)
	c.registerTopLevelFunctions()
	c.resolveCanonicalTypes()
	return c.ast, c.table, c.scopeTable, c.errors
}

// registerTopLevelFunctions populates SymbolTable.Functions (and its
// PureFuncs subset) for every top-level `let`/`var name = <lambda>` binding.
// Only top-level bindings are registered here — Functions/PureFuncs are flat
// maps keyed by name, so a nested same-named binding in a different scope
// would silently collide; scope-aware resolution for non-top-level functions
// is handled separately (see checker/purity.go's capture-stack walk).
func (c *Collector) registerTopLevelFunctions() {
	for _, stmt := range c.ast.Statements {
		vd, ok := stmt.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		if lam, ok := vd.Value.(*ast.LambdaExpr); ok {
			c.table.RegisterFunction(vd.Name, lam)
		}
	}
}

func (c *Collector) RecordScope(node ast.AstNode, scope *symbols.Scope) {
	c.scopeTable.Set(node, scope)
}

func (c *Collector) walkProgram(node *sitter.Node) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)

		stmt := c.CollectStatement(child)

		// Guard against both untyped nils and typed nils. The latter arise when a
		// sub-function returns a concrete nil pointer (e.g. nil *ast.ExpressionStmt)
		// which Go wraps into a non-nil ast.Statement interface value.
		if stmt != nil && !isTypedNil(stmt) {
			c.ast.Statements = append(c.ast.Statements, stmt)
		}
	}
}

// isTypedNil reports whether v is a non-nil interface wrapping a nil pointer.
func isTypedNil(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}

func (c *Collector) CollectExpr(node *sitter.Node) ast.Expression {
	return expressions.CollectExpression(node, c.ctx)
}

func (c *Collector) CollectStatement(node *sitter.Node) ast.Statement {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "module_declaration":
		return declarations.CollectModuleDeclaration(node, c.ctx)
	case "import_statement":
		return statements.CollectImportStatement(node, c.ctx)
	case "type_declaration":
		return typedecls.CollectTypeDeclaration(node, c.ctx)
	case "trait_declaration":
		return declarations.CollectTraitDeclaration(node, c.ctx)
	case "trait_implementation":
		return declarations.CollectTraitImplementation(node, c.ctx)
	case "declaration", "const_declaration", "for_initial_expr":
		return declarations.CollectVariableDeclaration(node, c.ctx)
	case "destructuring_if_declaration":
		return declarations.CollectDestructuringIfStatement(node, c.ctx)
	case "destructuring_else_declaration":
		return declarations.CollectDestructuringElseStatement(node, c.ctx)
	case "expression_statement":
		return expressions.CollectExpressionStatement(node, c.ctx)
	case "with_statement":
		return statements.CollectWithStatement(node, c.ctx)
	case "var_reassignment":
		return statements.CollectVarReassignmentStmt(node, c.ctx)
	case "deref_assignment":
		return statements.CollectDerefAssignmentStmt(node, c.ctx)
	case "member_assignment", "index_assignment":
		return statements.CollectLValueAssignmentStmt(node, c.ctx)
	case "break_statement":
		return statements.CollectBreakStatement(node, c.ctx)
	case "continue_statement":
		return statements.CollectContinueStatement(node, c.ctx)
	case "return_statement":
		return statements.CollectReturnStatement(node, c.ctx)
	}
	return nil
}

// Helper methods

func (c *Collector) addError(node *sitter.Node, severity diag.Severity, format string, args ...any) {
	c.ctx.AddError(node, severity, format, args...)
}

func (c *Collector) CollectGenericParams(node *sitter.Node) []ast.GenericParam {
	params := []ast.GenericParam{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() != "generic_parameter" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}
		p := ast.GenericParam{Name: c.ctx.NodeText(nameNode)}
		if boundsNode := child.ChildByFieldName("bounds"); boundsNode != nil {
			p.Constraints = c.CollectBounds(boundsNode)
		}
		params = append(params, p)
	}
	return params
}

func (c *Collector) MergeWhereConstraints(params []ast.GenericParam, whereNode *sitter.Node) []ast.GenericParam {
	nameToIdx := make(map[string]int, len(params))
	for i, p := range params {
		nameToIdx[p.Name] = i
	}
	for i := uint(0); i < whereNode.NamedChildCount(); i++ {
		child := whereNode.NamedChild(i)
		if child.Kind() != "generic_parameter_constraint" {
			continue
		}
		nameNode, ok := c.ctx.MustField(child, "generic_type")
		if !ok {
			continue
		}
		boundsNode, ok := c.ctx.MustField(child, "generic_bounds")
		if !ok {
			continue
		}
		name := c.ctx.NodeText(nameNode)
		bounds := c.CollectBounds(boundsNode)
		if idx, ok := nameToIdx[name]; ok {
			params[idx].Constraints = append(params[idx].Constraints, bounds...)
		}
	}
	return params
}

func (c *Collector) CollectBounds(node *sitter.Node) []string {
	bounds := []string{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "trait_name" {
			bounds = append(bounds, c.ctx.NodeText(child))
		}
	}
	return bounds
}

func (c *Collector) ParseType(node *sitter.Node) types.Type {
	return c.parseType(node)
}

func (c *Collector) ParseLambdaType(node *sitter.Node) *types.LambdaType {
	return c.parseLambdaType(node)
}

func (c *Collector) RegisterType(stmt *ast.TypeDeclStmt) error {
	return c.table.RegisterType(stmt)
}

func (c *Collector) RegisterTrait(stmt *ast.TraitDeclStmt) error {
	return c.table.RegisterTrait(stmt)
}

func (c *Collector) RegisterFunction(name string, stmt *ast.LambdaExpr) error {
	return c.table.RegisterFunction(name, stmt)
}

// LookupCurrentScope returns the symbol registered under name in the current
// (innermost) scope only, without walking parent scopes.
func (c *Collector) LookupCurrentScope(name string) (ast.Named, bool) {
	return c.table.CurrentScope.LookupLocal(name)
}

func (c *Collector) RegisterVariable(stmt *ast.VarDeclStmt) error {
	return c.table.RegisterVariable(stmt)
}

// RedefineVariable replaces an existing same-scope binding with stmt, used for
// same-scope sequential rebinding so that later references resolve to the newest
// declaration.
func (c *Collector) RedefineVariable(stmt *ast.VarDeclStmt) {
	c.table.RedefineVariable(stmt)
}

func (c *Collector) RegisterDestructuredName(name string, decl *ast.DestructuringDeclStmt) {
	c.table.RegisterDestructuredName(name, decl)
}

func (c *Collector) RegisterParameter(p *ast.Parameter) error {
	return c.table.RegisterParameter(p)
}

func (c *Collector) PushBlockScope() *symbols.Scope {
	return c.table.PushScope(symbols.ScopeBlock)
}

func (c *Collector) PushFunctionScope() *symbols.Scope {
	return c.table.PushScope(symbols.ScopeFunction)
}

func (c *Collector) PushLoopScope() {
	c.table.PushScope(symbols.ScopeLoop)
}

func (c *Collector) PopScope() {
	c.table.PopScope()
}

// allocation is only used for array types
func (c *Collector) parseType(node *sitter.Node) types.Type {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "signed_integer_type", "unsigned_integer_type", "float_type":
		return types.PrimitiveType{Name: types.PrimitiveTypeName(c.ctx.NodeText(node))}
	case "string_type":
		return types.PrimitiveType{Name: types.String}
	case "boolean_type":
		return types.PrimitiveType{Name: types.Boolean}
	case "user_defined_type_name":
		return types.UnresolvedType{Name: c.ctx.NodeText(node)}
	case "generic_type":
		return types.GenericType{Name: c.ctx.NodeText(node)}
	case "parameterized_type":
		return c.parseParameterizedType(node)
	case "array_type":
		return c.parseArrayType(node, types.Unspecified)
	case "constrained_type":
		return c.parseConstrainedType(node)
	case "allocated_type":
		return c.parseAllocatedType(node)
	case "anonymous_tuple_type":
		return c.parseAnonymousTupleType(node)
	case "anonymous_struct_type":
		return types.AnonymousStructType{Fields: typedecls.CollectStructFields(node, c.ctx)}
	case "lambda_type":
		return c.parseLambdaType(node)
	case "self_type":
		return c.parseSelfType(node)
	case "raw_pointer_type":
		return c.parseRawPointerType(node)
	case "void_type":
		return types.VoidType{}
	case "fixed_point_type":
		return c.parseFixedPointType(node)
	}
	c.addError(node, CollectorErrorSeverityError, "parseType: unknown type node kind: %s", node.Kind())
	return nil
}

func (c *Collector) parseParameterizedType(node *sitter.Node) types.Type {
	name := c.ctx.NodeText(node.ChildByFieldName("name"))
	// The grammar applies the "type_arguments" field to each type in the
	// comma-separated list (field("type_arguments", commaSep1($.type))), so the
	// args are sibling fields on this node, not children of one container.
	cursor := node.Walk()
	defer cursor.Close()
	// commaSep1 puts the field on the whole list, so the separating commas are
	// also tagged "type_arguments"; skip those unnamed nodes.
	argNodes := node.ChildrenByFieldName("type_arguments", cursor)
	typeArguments := make([]types.Type, 0, len(argNodes))
	for i := range argNodes {
		if argNodes[i].IsNamed() {
			typeArguments = append(typeArguments, c.parseType(&argNodes[i]))
		}
	}
	if len(typeArguments) == 0 {
		c.addError(node, CollectorErrorSeverityError, "parseParameterizedType: no type arguments")
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

func (c *Collector) parseLambdaType(node *sitter.Node) *types.LambdaType {
	return &types.LambdaType{
		Parameters: c.parseParameterTypes(node.ChildByFieldName("parameter_types")),
		ReturnType: types.ReturnType{Type: c.parseType(node.ChildByFieldName("return_type"))},
	}
}

func (c *Collector) parseParameterTypes(node *sitter.Node) []types.ParameterType {
	parameterTypes := []types.ParameterType{}
	if node == nil {
		// `parameter_types` is an optional field (lambda_type.js): a
		// zero-parameter lambda type like `() -> string` omits it entirely,
		// so ChildByFieldName returns nil here — not an error, just no
		// parameters. Calling a *sitter.Node accessor (ChildCount, Child, …)
		// on a nil node hangs inside the go-tree-sitter CGO binding instead
		// of panicking, so this guard is load-bearing, not defensive fluff.
		return parameterTypes
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "parameter_type" {
			parameterTypes = append(parameterTypes, c.parseParameterType(child))
		}
	}
	return parameterTypes
}

func (c *Collector) parseParameterType(node *sitter.Node) types.ParameterType {
	return types.ParameterType{
		Type: c.parseType(node.ChildByFieldName("type")),
	}
}

func (c *Collector) parseSelfType(node *sitter.Node) types.Type {
	genericParamsNode := node.ChildByFieldName("generic_parameters")
	var names []string
	if genericParamsNode != nil {
		for _, p := range c.CollectGenericParams(genericParamsNode) {
			names = append(names, p.Name)
		}
	}
	return types.SelfType{GenericParams: names}
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
		sizeString := c.ctx.NodeText(sizeNode)
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
	constraints := []types.Constraint{}
	if constraintsNode := node.ChildByFieldName("constraints"); constraintsNode != nil {
		constraints = typedecls.CollectConstraints(constraintsNode, c.ctx)
	}
	return &types.ConstrainedType{
		Name:        c.ctx.NodeText(node.ChildByFieldName("name")),
		Type:        c.parseType(node.ChildByFieldName("type")),
		Constraints: constraints,
	}
}

func (c *Collector) parseRawPointerType(node *sitter.Node) types.Type {
	pointeeNode := node.ChildByFieldName("pointee")
	if pointeeNode == nil {
		c.addError(node, CollectorErrorSeverityError, "parseRawPointerType: missing pointee")
		return nil
	}
	return types.RawPointerType{
		Pointee: c.parseType(pointeeNode),
		IsMut:   node.ChildByFieldName("is_mut") != nil,
	}
}

func (c *Collector) parseFixedPointType(node *sitter.Node) types.Type {
	intBits, fracBits := 0, 0
	idx := 0
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() != "integer_literal" {
			continue
		}
		val, _ := strconv.Atoi(c.ctx.NodeText(child))
		if idx == 0 {
			intBits = val
		} else {
			fracBits = val
		}
		idx++
	}
	return types.FixedPointType{IntegerBits: intBits, FractionalBits: fracBits}
}

func (c *Collector) parseAllocatedType(node *sitter.Node) types.Type {
	allocationNode := node.ChildByFieldName("allocation")
	allocation := types.AllocationModifier(c.ctx.NodeText(allocationNode))
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		c.addError(node, CollectorErrorSeverityError, "parseAllocatedType: type node is nil")
		return nil
	}
	// Arrays are built with the modifier directly (it becomes part of the array
	// type at construction). All other types are parsed normally and then have
	// the modifier overlaid via WithAllocation, which handles UnresolvedType,
	// ParameterizedType, NamedStructType, etc., and is a no-op for types that
	// cannot carry a flavor (primitives, generics, lambdas).
	if typeNode.Kind() == "array_type" {
		return c.parseArrayType(typeNode, allocation)
	}
	return types.WithAllocation(c.parseType(typeNode), allocation)
}

func (c *Collector) ParseDestructuringPattern(patternNode *sitter.Node) ast.Pattern {
	return c.CollectPattern(patternNode.Child(0))
}

func (c *Collector) CollectPattern(patternNode *sitter.Node) ast.Pattern {
	loc := c.ctx.NodeLocation(patternNode)
	switch patternNode.Kind() {
	case "identifier":
		return &ast.IdentifierPattern{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
			Name:        c.ctx.NodeText(patternNode),
		}
	case "literal_pattern":
		return &ast.LiteralPattern{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
			Value:       c.ctx.NodeText(patternNode),
		}
	case "regex_pattern":
		return c.collectRegexPattern(patternNode, loc)
	case "tuple_pattern":
		return c.collectTuplePattern(patternNode)
	case "array_pattern":
		return c.collectArrayPattern(patternNode)
	case "struct_pattern":
		return c.collectStructPattern(patternNode)
	case "data_pattern":
		return c.collectDataPattern(patternNode)
	case "range_pattern":
		return c.collectRangePattern(patternNode)
	case "binding_pattern":
		return c.collectBindingPattern(patternNode)
	case "wildcard_pattern":
		return c.collectWildcardPattern(patternNode)
	}
	c.addError(patternNode, CollectorErrorSeverityError, "collectPattern: unknown pattern node kind: %s", patternNode.Kind())
	return nil
}

// collectRegexPattern lowers a `regex_pattern` grammar node into a
// RegexPattern AST node. The grammar wraps a regex_literal, so we read the
// raw text and strip the surrounding `r/.../` delimiters — mirroring how
// regex_literal expressions are collected.
func (c *Collector) collectRegexPattern(patternNode *sitter.Node, loc ast.Location) ast.Pattern {
	raw := c.ctx.NodeText(patternNode) // e.g. r/[0-9]+/
	if len(raw) < 3 || raw[:2] != "r/" || raw[len(raw)-1] != '/' {
		c.addError(patternNode, CollectorErrorSeverityError, "collectRegexPattern: malformed regex literal %q", raw)
		return nil
	}
	return &ast.RegexPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
		Pattern:     raw[2 : len(raw)-1],
	}
}

func (c *Collector) collectTuplePattern(patternNode *sitter.Node) ast.Pattern {
	loc := c.ctx.NodeLocation(patternNode)
	return &ast.TuplePattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
		Elements:    c.collectPatternElements(patternNode),
	}
}

func (c *Collector) collectArrayPattern(patternNode *sitter.Node) ast.Pattern {
	loc := c.ctx.NodeLocation(patternNode)
	elements := c.collectPatternElements(patternNode)
	if len(elements) == 0 {
		c.addError(patternNode, CollectorErrorSeverityError, "collectArrayPattern: no elements in array pattern")
		return nil
	}
	return &ast.ArrayPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
		Elements:    elements,
	}
}

func (c *Collector) collectPatternElements(patternNode *sitter.Node) []ast.Pattern {
	elements := []ast.Pattern{}
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
	case "rest_pattern":
		return c.collectRestPattern(node)
	case "identifier", "literal_pattern", "pattern",
		"tuple_pattern", "array_pattern", "struct_pattern", "data_pattern",
		"range_pattern", "wildcard_pattern", "binding_pattern":
		return c.CollectPattern(node)
	}
	return nil
}

func (c *Collector) collectStructPattern(node *sitter.Node) ast.Pattern {
	loc := c.ctx.NodeLocation(node)
	fields := c.collectStructPatternFields(node)
	return &ast.StructPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
		Fields:      fields,
	}
}

func (c *Collector) collectStructPatternFields(node *sitter.Node) []ast.StructPatternField {
	fields := []ast.StructPatternField{}
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
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
			Name:        c.ctx.NodeText(nameNode),
			Pattern:     nil,
		}
	}
	structFieldRenameNode := node.ChildByFieldName("struct_field_rename")
	if structFieldRenameNode != nil {
		// `{ oldName: newName }`: Name is the *struct's* field (what binding.go's
		// field lookups match against — same convention as the shorthand and
		// with-pattern cases above), and the local bound name is represented as
		// an ordinary identifier sub-pattern, exactly like the with-pattern case
		// (`{ oldName: somePattern }`) just below.
		newNameNode := structFieldRenameNode.ChildByFieldName("new_name")
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
			Name:        c.ctx.NodeText(structFieldRenameNode.ChildByFieldName("name")),
			Pattern: &ast.IdentifierPattern{
				PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(newNameNode)}},
				Name:        c.ctx.NodeText(newNameNode),
			},
		}
	}
	structFieldWithPatternNode := node.ChildByFieldName("struct_field_with_pattern")
	if structFieldWithPatternNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
			Name:        c.ctx.NodeText(structFieldWithPatternNode.ChildByFieldName("name")),
			Pattern:     c.CollectPattern(structFieldWithPatternNode.ChildByFieldName("pattern")),
		}
	}
	restPatternNode := node.ChildByFieldName("rest_pattern")
	if restPatternNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
			Name:        "...",
			Pattern:     c.collectRestPattern(restPatternNode),
		}
	}
	wildcardPatternNode := node.ChildByFieldName("wildcard_pattern")
	if wildcardPatternNode != nil {
		return &ast.StructPatternField{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
			Name:        "_",
			Pattern:     c.collectWildcardPattern(wildcardPatternNode),
		}
	}

	return nil
}

func (c *Collector) collectDataPattern(node *sitter.Node) *ast.DataPattern {
	loc := c.ctx.NodeLocation(node)
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectDataPattern: name node is nil")
		return nil
	}
	patternNode := node.ChildByFieldName("pattern")
	var pattern ast.Pattern
	if patternNode != nil {
		pattern = c.CollectPattern(patternNode)
	}
	return &ast.DataPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
		Name:        c.ctx.NodeText(nameNode),
		Pattern:     pattern,
	}
}

func (c *Collector) collectRangePattern(node *sitter.Node) ast.Pattern {
	endOperatorNode := node.ChildByFieldName("end_operator")
	endOperator := ""
	if endOperatorNode != nil {
		endOperator = c.ctx.NodeText(endOperatorNode)
	}
	return &ast.RangePattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
		Start:       c.CollectExpr(node.ChildByFieldName("start")),
		End:         c.CollectExpr(node.ChildByFieldName("end")),
		EndOperator: endOperator,
	}
}

func (c *Collector) collectRestPattern(node *sitter.Node) ast.Pattern {
	return &ast.RestPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
		Identifier:  c.ctx.NodeText(node.ChildByFieldName("identifier")),
	}
}

func (c *Collector) collectWildcardPattern(node *sitter.Node) ast.Pattern {
	return &ast.WildcardPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
	}
}

func (c *Collector) collectBindingPattern(node *sitter.Node) ast.Pattern {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectBindingPattern: name node is nil")
		return nil
	}
	patternNode := node.ChildByFieldName("pattern")
	if patternNode == nil {
		c.addError(node, CollectorErrorSeverityError, "collectBindingPattern: pattern node is nil")
		return nil
	}
	return &ast.BindingPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: c.ctx.NodeLocation(node)}},
		Name:        c.ctx.NodeText(nameNode),
		Pattern:     c.CollectPattern(patternNode),
	}
}
