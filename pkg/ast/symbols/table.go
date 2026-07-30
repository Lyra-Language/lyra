package symbols

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Scope represents a lexical scope
type Scope struct {
	Parent   *Scope
	Children []*Scope
	Symbols  map[string]ast.Named // Variables and other named entities
	Kind     ScopeKind
}

type ScopeKind int

const (
	ScopeGlobal ScopeKind = iota
	ScopeModule
	ScopeFunction
	ScopeBlock
	ScopeLoop
)

func NewScope(parent *Scope, kind ScopeKind) *Scope {
	s := &Scope{
		Parent:   parent,
		Children: []*Scope{},
		Symbols:  make(map[string]ast.Named),
		Kind:     kind,
	}
	if parent != nil {
		parent.Children = append(parent.Children, s)
	}
	return s
}

// Define adds a named AST node to the current scope
func (s *Scope) Define(node ast.Named) error {
	name := node.GetName()
	if existing, exists := s.Symbols[name]; exists {
		existingLoc := existing.GetLocation()
		return fmt.Errorf("symbol %q already defined at %s", name, describeLocation(existingLoc))
	}
	s.Symbols[name] = node
	return nil
}

// Lookup searches for a symbol in this scope and parent scopes
func (s *Scope) Lookup(name string) (ast.Named, bool) {
	if sym, ok := s.Symbols[name]; ok {
		return sym, true
	}
	if s.Parent != nil {
		return s.Parent.Lookup(name)
	}
	return nil, false
}

// LookupLocal only searches the current scope (no parents)
func (s *Scope) LookupLocal(name string) (ast.Named, bool) {
	sym, ok := s.Symbols[name]
	return sym, ok
}

// SymbolTable is the top-level container for all symbols
// It provides quick lookups by name, pointing directly to AST nodes
type SymbolTable struct {
	GlobalScope  *Scope
	CurrentScope *Scope

	// Quick lookup tables - these point to AST nodes directly
	Types     map[string]*ast.TypeDeclStmt
	Functions map[string]*ast.LambdaExpr
	Traits    map[string]*ast.TraitDeclStmt

	// ModuleOf records which module declared each top-level name, and ModuleOfFile
	// which module each source file belongs to. Together they answer the two questions
	// namespacing needs: "does `math.double` really name something in util.math?" and
	// "which module is this reference being made from?" — the latter recovered from a
	// node's Location.File, so no pass has to thread a module context through itself.
	ModuleOf     map[string]string
	ModuleOfFile map[string]string

	// Imports maps a module path to the imports its files declare. A plain
	// `import a.b` binds a *namespace* under the last segment (or its `as` alias);
	// `.{ X, Y as Z }` binds the listed names directly.
	Imports map[string][]Import

	// PureFuncs maps the name of each function declared with the `pure`
	// keyword to its lambda expression. Populated during collection; used
	// by the purity checker to know which functions are explicitly pure
	// (and which are implicitly pure by default). PureFuncs is a subset
	// of Functions.
	PureFuncs map[string]*ast.LambdaExpr
}

func NewSymbolTable() *SymbolTable {
	st := &SymbolTable{
		GlobalScope:  NewScope(nil, ScopeGlobal),
		Types:        make(map[string]*ast.TypeDeclStmt),
		Functions:    make(map[string]*ast.LambdaExpr),
		Traits:       make(map[string]*ast.TraitDeclStmt),
		PureFuncs:    make(map[string]*ast.LambdaExpr),
		ModuleOf:     make(map[string]string),
		ModuleOfFile: make(map[string]string),
		Imports:      make(map[string][]Import),
	}
	st.CurrentScope = st.GlobalScope
	return st
}

// LookupType, LookupTrait and LookupFunction are the **only** ways a pass should
// resolve a top-level name to its declaration.
//
// They exist as a choke point rather than as sugar over the maps. Those maps are keyed
// by bare name and are read from seven packages; with modules, answering "which
// declaration does this name mean" depends on *which module is asking*, and a lookup
// scattered across dozens of call sites cannot be taught that. Routing every read
// through here means module resolution is a change in one place — the same reason
// recordedType, stripNewtype and slotIsOwning exist.
//
// All three are nil-receiver-safe, so a consumer without a symbol table sees "not
// found" rather than crashing.
func (st *SymbolTable) LookupType(name string) (*ast.TypeDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Types[name]
	return decl, ok
}

func (st *SymbolTable) LookupTrait(name string) (*ast.TraitDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Traits[name]
	return decl, ok
}

func (st *SymbolTable) LookupFunction(name string) (*ast.LambdaExpr, bool) {
	if st == nil {
		return nil, false
	}
	fn, ok := st.Functions[name]
	return fn, ok
}

func (st *SymbolTable) PushScope(kind ScopeKind) *Scope {
	st.CurrentScope = NewScope(st.CurrentScope, kind)
	return st.CurrentScope
}

func (st *SymbolTable) PopScope() {
	if st.CurrentScope.Parent != nil {
		st.CurrentScope = st.CurrentScope.Parent
	}
}

// RegisterType adds a type declaration to the symbol table
func (st *SymbolTable) RegisterType(node *ast.TypeDeclStmt) error {
	if err := st.GlobalScope.Define(node); err != nil {
		return err
	}
	st.Types[node.Name] = node
	return nil
}

// RegisterTrait adds a trait declaration to the symbol table.
// Returns an error if a trait with the same name is already registered.
func (st *SymbolTable) RegisterTrait(node *ast.TraitDeclStmt) error {
	if _, exists := st.Traits[node.Name]; exists {
		return fmt.Errorf("trait %q already defined", node.Name)
	}
	st.Traits[node.Name] = node
	return nil
}

// RegisterFunction adds a function to the symbol table. If node is declared
// `pure`, it is also recorded in PureFuncs (a subset of Functions).
//
// A duplicate is an error, as it already is for a type or a trait. It used to
// overwrite silently, which was harmless while a program was a single file — a
// redeclaration within one file is caught earlier, by the scope's own Define — but with
// modules it meant two modules could each define `helper` and the program would build,
// calling whichever happened to be registered last. A silently wrong program, chosen by
// collection order.
func (st *SymbolTable) RegisterFunction(name string, node *ast.LambdaExpr) error {
	if existing, exists := st.Functions[name]; exists && existing != node {
		return fmt.Errorf("function %q is already defined at %s", name, describeLocation(existing.GetLocation()))
	}
	st.Functions[name] = node
	if node.IsPure {
		st.PureFuncs[name] = node
	}
	return nil
}

// describeLocation renders a location for an "already defined" message, naming the file
// when there is one. Without it, two colliding modules both report a bare line:col —
// and the line:col of the *other* module's declaration is worse than useless, since it
// reads as a position in the file being reported on.
func describeLocation(loc ast.Location) string {
	if loc.File != "" {
		return fmt.Sprintf("%s:%s", loc.File, loc.Pretty())
	}
	return loc.Pretty()
}

// RegisterVariable adds a variable to the current scope
func (st *SymbolTable) RegisterVariable(node *ast.VarDeclStmt) error {
	if st.CurrentScope == nil {
		return fmt.Errorf("no current scope to register variable %s", node.Name)
	}
	return st.CurrentScope.Define(node)
}

// RedefineVariable replaces an existing binding under the same name in the
// current scope. It is used for same-scope sequential rebinding (e.g.
// `let x = parse(x)`), where the most recent declaration must win so that
// later references resolve to it.
func (st *SymbolTable) RedefineVariable(node *ast.VarDeclStmt) {
	if st.CurrentScope != nil {
		st.CurrentScope.Symbols[node.Name] = node
	}
}

// RegisterDestructuredName binds a single name introduced by a destructuring
// declaration (e.g. `x` in `let (x, y) = pair`) to that declaration in the
// current scope. The owning DestructuringDeclStmt is stored — rather than the
// leaf pattern — so consumers can recover the binding's mutability (`var` /
// `let mut`). Insertion is direct and last-wins (mirroring RedefineVariable) so
// same-scope rebinding does not error here; the collector emits duplicate
// diagnostics separately.
func (st *SymbolTable) RegisterDestructuredName(name string, decl *ast.DestructuringDeclStmt) {
	if st.CurrentScope != nil {
		st.CurrentScope.Symbols[name] = decl
	}
}

// RegisterParameter adds a lambda/function parameter to the current scope
func (st *SymbolTable) RegisterParameter(node *ast.Parameter) error {
	if st.CurrentScope == nil {
		return fmt.Errorf("no current scope to register parameter %s", node.GetName())
	}
	return st.CurrentScope.Define(node)
}

// ScopeTable maps scope-creating AST nodes (BlockExpr, LambdaExpr, etc.)
// to the Scope the collector pushed when it entered them.
// Populated during collection; used by the type-checker to enter the
// correct child scope when traversing a nested block or function body.
type ScopeTable struct {
	entries map[ast.AstNode]*Scope
}

func NewScopeTable() *ScopeTable {
	return &ScopeTable{entries: make(map[ast.AstNode]*Scope)}
}

func (st *ScopeTable) Set(node ast.AstNode, scope *Scope) {
	st.entries[node] = scope
}

func (st *ScopeTable) Get(node ast.AstNode) (*Scope, bool) {
	s, ok := st.entries[node]
	return s, ok
}

// Import is one resolved import as seen from the module that declared it.
//
// Namespace and selective imports are the two shapes the grammar allows, and they bind
// different things: a plain `import util.math` (or `... as m`) makes the module
// reachable under a name, so uses read `math.double(…)`; `import util.math.{ double }`
// puts the listed names in scope directly. Alias holds whichever name the reference
// site will use.
type Import struct {
	Path    string            // the module being imported, e.g. "util.math"
	Alias   string            // namespace name it binds; empty for a selective import
	Members map[string]string // local name → name in the imported module
	Loc     ast.Location
}

// IsNamespace reports whether this import binds a namespace rather than naming members.
func (i Import) IsNamespace() bool { return i.Alias != "" }

// ImportsFor returns the imports visible in the file at path.
func (st *SymbolTable) ImportsFor(file string) []Import {
	if st == nil {
		return nil
	}
	return st.Imports[st.ModuleOfFile[file]]
}

// NamespaceImport returns the import a namespace name refers to, as seen from file.
func (st *SymbolTable) NamespaceImport(file, name string) (Import, bool) {
	for _, imp := range st.ImportsFor(file) {
		if imp.IsNamespace() && imp.Alias == name {
			return imp, true
		}
	}
	return Import{}, false
}

// DeclaringModule reports which module declared a top-level name.
func (st *SymbolTable) DeclaringModule(name string) string {
	if st == nil {
		return ""
	}
	return st.ModuleOf[name]
}

// BindingOf returns the top-level `let`/`var` declaration that bound name.
//
// SymbolTable.Functions holds the *lambda*, but `pub` is a property of the binding
// that names it, so exporting a function can only be answered from the declaration.
func (st *SymbolTable) BindingOf(name string) (*ast.VarDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	sym, ok := st.GlobalScope.LookupLocal(name)
	if !ok {
		return nil, false
	}
	decl, ok := sym.(*ast.VarDeclStmt)
	return decl, ok
}
