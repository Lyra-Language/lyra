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
		Children: make([]*Scope, 0),
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
		return fmt.Errorf("symbol %q already defined at %v", name, existing.GetLocation())
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
	GlobalScope *Scope

	// Quick lookup tables - these point to AST nodes directly
	Types      map[string]*ast.TypeDeclarationStmt
	Functions  map[string]*ast.FunctionDefStmt
	Traits     map[string]*TraitSymbol      // Keep for now - traits are more complex
	TraitImpls []*TraitImplSymbol           // Keep for now - complex keys
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		GlobalScope: NewScope(nil, ScopeGlobal),
		Types:       make(map[string]*ast.TypeDeclarationStmt),
		Functions:   make(map[string]*ast.FunctionDefStmt),
		Traits:      make(map[string]*TraitSymbol),
		TraitImpls:  make([]*TraitImplSymbol, 0),
	}
}

// RegisterType adds a type declaration to the symbol table
func (st *SymbolTable) RegisterType(node *ast.TypeDeclarationStmt) error {
	if err := st.GlobalScope.Define(node); err != nil {
		return err
	}
	st.Types[node.Name] = node
	return nil
}

// RegisterFunction adds a function to the symbol table
func (st *SymbolTable) RegisterFunction(node *ast.FunctionDefStmt) error {
	if err := st.GlobalScope.Define(node); err != nil {
		return err
	}
	st.Functions[node.Name] = node
	return nil
}

// RegisterVariable adds a variable to the current scope
func (st *SymbolTable) RegisterVariable(node *ast.VariableDeclarationStmt) error {
	return st.GlobalScope.Define(node)
}

// RegisterTrait adds a trait to both the scope and quick lookup
func (st *SymbolTable) RegisterTrait(sym *TraitSymbol) error {
	// Traits still use the old symbol type for now
	st.Traits[sym.Name] = sym
	return nil
}

// RegisterTraitImpl adds a trait implementation
func (st *SymbolTable) RegisterTraitImpl(sym *TraitImplSymbol) error {
	st.TraitImpls = append(st.TraitImpls, sym)
	return nil
}
