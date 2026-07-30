package collector

/*
Collector walks the tree-sitter CST and builds an AST representation of the program.
It also populates a symbol table for quick name lookups.
The AST nodes serve as the source of truth - the symbol table just indexes them.
*/

import (
	"reflect"
	"strconv"
	"strings"

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
// It is AddFile + Finish for the single-file case.
func (c *Collector) Collect(root *sitter.Node) (*ast.Program, *symbols.SymbolTable, *symbols.ScopeTable, []error) {
	c.AddFile(root, c.source, "", "")
	return c.Finish()
}

// AddFile walks one source file into the accumulating program, and may be called
// repeatedly — once per module — before Finish.
//
// The split exists because only the *walk* is per-file: every pass in Finish reaches
// across the whole program. reclassifyStructPatterns has to know every declared struct,
// resolveCanonicalTypes every declared type, and registerTopLevelFunctions every
// top-level binding — and with modules those can come from a file that has not been
// walked yet. Running them per file would make the result depend on collection order.
//
// source is swapped in alongside the tree because every text and location read goes
// through it (ctx.NodeText / ctx.NodeLocation); a stale source would slice the wrong
// bytes for the file being walked.
func (c *Collector) AddFile(root *sitter.Node, source []byte, file, modulePath string) {
	c.source = source
	c.ctx.Source = source
	c.ctx.File = file
	c.ctx.Module = modulePath
	c.table.ModuleOfFile[file] = modulePath
	// Walk the file inside its own module scope, so *every* scope the walk creates —
	// a function's parameter scope, a block, a match arm — is parented under it. This
	// is the keystone of per-module resolution: without it a module scope is a sibling
	// nothing looks into, and a function body's chain runs straight to the global
	// scope, so a module could not even see its own private declarations.
	outer := c.table.CurrentScope
	c.table.CurrentScope = c.table.ModuleScopeFor(modulePath)
	before := len(c.ast.Statements)
	c.walkProgram(root)
	c.table.CurrentScope = outer
	c.recordModuleBindings(modulePath, c.ast.Statements[before:])
}

// recordModuleBindings notes which module owns each top-level name this file declared,
// and the imports it made. Both are recorded per file rather than derived later because
// this is the only point where "these statements came from that module" is known —
// afterwards the statements are merged into one program.
func (c *Collector) recordModuleBindings(modulePath string, stmts []ast.AstNode) {
	moduleScope := c.table.ModuleScopeFor(modulePath)
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			c.table.Imports[modulePath] = append(c.table.Imports[modulePath], importBinding(s))
		case *ast.TypeDeclStmt:
			c.noteDeclared(s.Name, modulePath)
			c.defineInModule(moduleScope, s)
		case *ast.TraitDeclStmt:
			c.noteDeclared(s.Name, modulePath)
			c.defineInModule(moduleScope, s)
		case *ast.VarDeclStmt:
			c.noteDeclared(s.Name, modulePath)
			c.defineInModule(moduleScope, s)
			c.exportToGlobal(modulePath, s, s.IsPublic)
		}
	}
}

// exportToGlobal publishes a `pub` declaration into the scope other modules fall
// through to, so a reference from another module resolves to it.
//
// This is the half that makes privacy mean something: a declaration always lands in its
// own module's scope, and only an exported one *also* lands here. A private name is
// therefore invisible outside its module — and, because it never competes for the
// shared name, two modules may each declare one.
//
// The **prelude** exports into a scope of its own rather than the global one. Both are
// on every module's chain, but the prelude's sits nearer, so a module's own declaration
// of a prelude name shadows it *there* while every other module still reaches the
// prelude's — which is the whole difference between an ambient name and a name burned
// into the one global namespace. Exporting it globally instead put it in direct
// competition with user declarations, and whichever won, won for the entire program.
//
// The entry module exports nothing: nothing can import it, so `pub` on its declarations
// would name a boundary that does not exist.
func (c *Collector) exportToGlobal(modulePath string, decl ast.Named, isPublic bool) {
	if modulePath == "" || !isPublic {
		return
	}
	if modulePath == c.table.PreludeModule {
		// A duplicate here can only be the prelude declaring one name twice, which its
		// own module scope has already reported.
		_ = c.table.PreludeScope.Define(decl)
		return
	}
	if err := c.table.GlobalScope.Define(decl); err != nil {
		// Two modules exporting the same name: a bare reference could mean either, so
		// it is a genuine clash rather than something privacy can resolve.
		c.errors = append(c.errors, diag.Diagnostic{
			Location: decl.GetLocation(),
			Severity: diag.SeverityError,
			Message:  err.Error(),
		})
	}
}

// defineInModule adds a top-level declaration to its module's own scope, alongside the
// global registration that still drives lookup (see ModuleScopeFor).
//
// A duplicate here is not reported: it can only be a name the global registration has
// already rejected, and reporting it twice would turn one mistake into two diagnostics.
func (c *Collector) defineInModule(scope *symbols.Scope, decl ast.Named) {
	if scope == nil {
		return
	}
	_ = scope.Define(decl)
}

// noteDeclared records which module declared a name, and separately remembers the
// prelude's names. The two are not the same map: ModuleOf is last-writer-wins, so a
// user module declaring a prelude name overwrites it, while the prelude set must
// survive that in order to recognise the shadow.
func (c *Collector) noteDeclared(name, modulePath string) {
	c.table.ModuleOf[name] = modulePath
	if modulePath != "" && modulePath == c.table.PreludeModule {
		c.table.PreludeNames[name] = true
	}
}

// importBinding converts a collected import into the symbol table's form, applying the
// binding rule: a plain `import a.b` (or `as x`) binds a namespace under the last
// segment or the alias, while `.{ X, Y as Z }` binds the listed names directly.
func importBinding(s *ast.ImportStmt) symbols.Import {
	imp := symbols.Import{Path: joinModulePath(s.Path), Loc: s.GetLocation()}
	if len(s.Members) > 0 {
		imp.Members = make(map[string]string, len(s.Members))
		for _, m := range s.Members {
			local := m.Name
			if m.Alias != "" {
				local = m.Alias
			}
			imp.Members[local] = m.Name
		}
		return imp
	}
	imp.Alias = s.Alias
	if imp.Alias == "" && len(s.Path) > 0 {
		imp.Alias = s.Path[len(s.Path)-1].Name
	}
	return imp
}

func joinModulePath(parts []ast.ModuleName) string {
	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = p.Name
	}
	return strings.Join(names, ".")
}

// SetPreludeModule names the implicitly-imported module, so registration can tell a
// user declaration taking a prelude name (allowed, warned) from two user modules
// clashing (an error).
func (c *Collector) SetPreludeModule(path string) {
	c.table.PreludeModule = path
}

// Finish runs the whole-program passes and returns the merged result.
func (c *Collector) Finish() (*ast.Program, *symbols.SymbolTable, *symbols.ScopeTable, []error) {
	c.registerTopLevelFunctions()
	c.reclassifyStructPatterns()
	c.reclassifyConstructorExprs()
	c.resolveCanonicalTypes()
	return c.ast, c.table, c.scopeTable, c.errors
}

// reclassifyStructPatterns rewrites every `Pt { … }` pattern whose name is a
// declared struct type from a DataPattern into a (named) StructPattern. The
// grammar can't tell `Pt { x, y }` (a struct pattern) from `Node { l, r }` (a
// data-constructor inline-record pattern) — they're identical shapes, both
// parsed as a DataPattern with an inner StructPattern payload — so the split is
// semantic and runs here, after walkProgram has registered every type in the
// symbol table (so a forward-referenced struct still resolves). A name that
// isn't a struct type (a data constructor, or unknown) is left as a DataPattern.
func (c *Collector) reclassifyStructPatterns() {
	onStmt := func(stmt ast.Statement) bool {
		switch s := stmt.(type) {
		case *ast.DestructuringDeclStmt:
			s.Pattern = c.reclassifyPattern(s.Pattern)
		case *ast.IfDestructuringStmt:
			s.DestructuringStatement.Pattern = c.reclassifyPattern(s.DestructuringStatement.Pattern)
		case *ast.ElseDestructuringStmt:
			s.DestructuringStatement.Pattern = c.reclassifyPattern(s.DestructuringStatement.Pattern)
		}
		return true
	}
	onExpr := func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.MatchExpr:
			for i := range e.MatchArms {
				e.MatchArms[i].Pattern = c.reclassifyPattern(e.MatchArms[i].Pattern)
			}
		case *ast.LambdaExpr:
			for i := range e.Parameters {
				e.Parameters[i].Pattern = c.reclassifyPattern(e.Parameters[i].Pattern)
			}
			for ci := range e.LambdaClauses {
				for pi := range e.LambdaClauses[ci].Patterns {
					e.LambdaClauses[ci].Patterns[pi] = c.reclassifyPattern(e.LambdaClauses[ci].Patterns[pi])
				}
			}
		}
		return true
	}
	for _, node := range c.ast.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, onStmt, onExpr)
		}
	}
}

// reclassifyPattern converts a `Pt { … }` DataPattern into a named StructPattern
// when Pt is a declared struct type, recursing into sub-patterns first so nested
// occurrences (a struct pattern inside a tuple/struct/data pattern) are rewritten
// too. Any other pattern is returned unchanged (after recursion).
func (c *Collector) reclassifyPattern(pat ast.Pattern) ast.Pattern {
	if pat == nil {
		return nil
	}
	switch p := pat.(type) {
	case *ast.DataPattern:
		p.Pattern = c.reclassifyPattern(p.Pattern)
		if decl, ok := c.table.LookupType(p.Name); ok {
			if _, isStruct := decl.Type.(types.NamedStructType); isStruct {
				if sp, ok := p.Pattern.(*ast.StructPattern); ok {
					sp.Name = p.Name
					return sp
				}
			}
		}
		return p
	case *ast.StructPattern:
		for i := range p.Fields {
			p.Fields[i].Pattern = c.reclassifyPattern(p.Fields[i].Pattern)
		}
		return p
	case *ast.TuplePattern:
		for i := range p.Elements {
			p.Elements[i] = c.reclassifyPattern(p.Elements[i])
		}
		return p
	case *ast.ArrayPattern:
		for i := range p.Elements {
			p.Elements[i] = c.reclassifyPattern(p.Elements[i])
		}
		return p
	case *ast.BindingPattern:
		p.Pattern = c.reclassifyPattern(p.Pattern)
		return p
	}
	return pat
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
			// The error is surfaced rather than dropped: with modules it is how two
			// modules each defining `helper` are caught, instead of the program
			// building against whichever was registered last.
			if err := c.table.RegisterFunction(vd.Name, lam); err != nil {
				c.errors = append(c.errors, diag.Diagnostic{
					Location: vd.GetLocation(),
					Severity: diag.SeverityError,
					Message:  err.Error(),
				})
			}
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

func (c *Collector) PushLoopScope() *symbols.Scope {
	return c.table.PushScope(symbols.ScopeLoop)
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
	case "rune_type":
		return types.PrimitiveType{Name: types.Rune}
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
	case "weak_type":
		return c.parseWeakType(node)
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

// parseWeakType collects a `weak T` node into a types.WeakType wrapping the
// inner type (the `inner_type` grammar field, a `_non_allocated_type`). weak is a
// non-owning reference used to break `shared` reference cycles; the inner is
// parsed normally (a named type stays an UnresolvedType for the typechecker to
// resolve).
func (c *Collector) parseWeakType(node *sitter.Node) types.Type {
	innerNode := node.ChildByFieldName("inner_type")
	if innerNode == nil {
		c.addError(node, CollectorErrorSeverityError, "parseWeakType: missing inner type")
		return nil
	}
	return types.WeakType{Inner: c.parseType(innerNode)}
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
		// A char-literal pattern ('a') stores its decoded code point, reusing the
		// expression collector so escape handling (\n, \x41, \U…) stays in one
		// place; every other literal keeps its raw source text.
		if child := patternNode.Child(0); child != nil && child.Kind() == "char_literal" {
			if ch, ok := c.CollectExpr(child).(*ast.CharacterLiteralExpr); ok {
				return &ast.LiteralPattern{
					PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
					Value:       ast.RunePatternValue(ch.Value),
				}
			}
		}
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
// raw text and strip the surrounding `r"..."` delimiters — mirroring how
// regex_literal expressions are collected.
func (c *Collector) collectRegexPattern(patternNode *sitter.Node, loc ast.Location) ast.Pattern {
	raw := c.ctx.NodeText(patternNode) // e.g. r"[0-9]+"
	if len(raw) < 3 || raw[:2] != `r"` || raw[len(raw)-1] != '"' {
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
	// An empty `[]` pattern (no elements) is valid — the base case of a list match
	// (`match xs { [] => …, [a, ...rest] => … }`).
	return &ast.ArrayPattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: loc}},
		Elements:    c.collectPatternElements(patternNode),
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
