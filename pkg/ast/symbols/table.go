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
		return fmt.Errorf("symbol %q already defined at %s", name, existingLoc.Pretty())
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
}

func NewSymbolTable() *SymbolTable {
	st := &SymbolTable{
		GlobalScope: NewScope(nil, ScopeGlobal),
		Types:       make(map[string]*ast.TypeDeclStmt),
		Functions:   make(map[string]*ast.LambdaExpr),
		Traits:      make(map[string]*ast.TraitDeclStmt),
	}
	st.CurrentScope = st.GlobalScope
	return st
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

// RegisterFunction adds a function to the symbol table
func (st *SymbolTable) RegisterFunction(name string, node *ast.LambdaExpr) error {
	st.Functions[name] = node
	return nil
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
