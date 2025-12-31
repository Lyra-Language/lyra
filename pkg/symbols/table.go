package symbols

import "fmt"

// Scope represents a lexical scope
type Scope struct {
	Parent   *Scope
	Children []*Scope
	Symbols  map[string]Symbol
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
		Symbols:  make(map[string]Symbol),
		Kind:     kind,
	}
	if parent != nil {
		parent.Children = append(parent.Children, s)
	}
	return s
}

// Define adds a symbol to the current scope
func (s *Scope) Define(sym Symbol) error {
	name := sym.GetName()
	if existing, exists := s.Symbols[name]; exists {
		return fmt.Errorf("symbol %q already defined at %v", name, existing.GetLocation())
	}
	s.Symbols[name] = sym
	return nil
}

// Lookup searches for a symbol in this scope and parent scopes
func (s *Scope) Lookup(name string) (Symbol, bool) {
	if sym, ok := s.Symbols[name]; ok {
		return sym, true
	}
	if s.Parent != nil {
		return s.Parent.Lookup(name)
	}
	return nil, false
}

// LookupLocal only searches the current scope (no parents)
func (s *Scope) LookupLocal(name string) (Symbol, bool) {
	sym, ok := s.Symbols[name]
	return sym, ok
}

// SymbolTable is the top-level container for all symbols
type SymbolTable struct {
	GlobalScope *Scope

	// Quick lookup tables for types and functions
	Types      map[string]*TypeSymbol
	Functions  map[string]*FunctionSymbol
	Traits     map[string]*TraitSymbol
	TraitImpls []*TraitImplSymbol // list because keys are complex
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		GlobalScope: NewScope(nil, ScopeGlobal),
		Types:       make(map[string]*TypeSymbol),
		Functions:   make(map[string]*FunctionSymbol),
		Traits:      make(map[string]*TraitSymbol),
		TraitImpls:  make([]*TraitImplSymbol, 0),
	}
}

// RegisterType adds a type to both the scope and quick lookup
func (st *SymbolTable) RegisterType(sym *TypeSymbol) error {
	if err := st.GlobalScope.Define(sym); err != nil {
		return err
	}
	st.Types[sym.Name] = sym
	return nil
}

// RegisterFunction adds a function to both the scope and quick lookup
func (st *SymbolTable) RegisterFunction(sym *FunctionSymbol) error {
	if err := st.GlobalScope.Define(sym); err != nil {
		return err
	}
	st.Functions[sym.Name] = sym
	return nil
}

// RegisterTrait adds a trait to both the scope and quick lookup
func (st *SymbolTable) RegisterTrait(sym *TraitSymbol) error {
	if err := st.GlobalScope.Define(sym); err != nil {
		return err
	}
	st.Traits[sym.Name] = sym
	return nil
}

// RegisterTraitImpl adds a trait implementation
func (st *SymbolTable) RegisterTraitImpl(sym *TraitImplSymbol) error {
	if err := st.GlobalScope.Define(sym); err != nil {
		return err
	}
	st.TraitImpls = append(st.TraitImpls, sym)
	return nil
}
