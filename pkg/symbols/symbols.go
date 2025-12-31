package symbols

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Symbol represents any named entity in the program
type Symbol interface {
	symbolNode()
	GetName() string
	GetLocation() Location
}

// Location tracks where a symbol was defined
type Location struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// TypeSymbol represents a type declaration (struct, data, trait)
type TypeSymbol struct {
	Name          string
	GenericParams []string // e.g., ["t", "u"] for Maybe<t> or Map<k, v>
	Type          types.Type
	Location      Location
	IsPublic      bool
}

func (TypeSymbol) symbolNode()             {}
func (s TypeSymbol) GetName() string       { return s.Name }
func (s TypeSymbol) GetLocation() Location { return s.Location }

// FunctionSymbol represents a function definition
type FunctionSymbol struct {
	Name          string
	GenericParams []string
	Signature     *types.FunctionType // nil if not explicitly typed
	Patterns      []*FunctionPatternSymbol
	Location      Location
	IsPublic      bool
	IsPure        bool
	IsAsync       bool
}

func (FunctionSymbol) symbolNode()             {}
func (s FunctionSymbol) GetName() string       { return s.Name }
func (s FunctionSymbol) GetLocation() Location { return s.Location }

// FunctionPatternSymbol represents a function pattern
type FunctionPatternSymbol struct {
	Parameters []types.NamedParameter
	Guard      *GuardSymbol
	Body       string
	Location   Location
}

func (FunctionPatternSymbol) symbolNode()             {}
func (s FunctionPatternSymbol) GetName() string       { return "function pattern" }
func (s FunctionPatternSymbol) GetLocation() Location { return s.Location }

// GuardSymbol represents a guard
type GuardSymbol struct {
	Expression string
	Location   Location
}

func (GuardSymbol) symbolNode()             {}
func (s GuardSymbol) GetName() string       { return "guard" }
func (s GuardSymbol) GetLocation() Location { return s.Location }

// TraitMethodSymbol represents a trait method signature
type TraitMethodSymbol struct {
	Name      string
	Signature *types.FunctionType
	Location  Location
}

func (TraitMethodSymbol) symbolNode()             {}
func (s TraitMethodSymbol) GetName() string       { return s.Name }
func (s TraitMethodSymbol) GetLocation() Location { return s.Location }

// VariableSymbol represents a let/var/const binding
type VariableSymbol struct {
	Name       string
	Type       types.Type // may be nil if needs inference
	Location   Location
	IsMutable  bool // var vs let
	IsConstant bool // const
}

func (VariableSymbol) symbolNode()             {}
func (s VariableSymbol) GetName() string       { return s.Name }
func (s VariableSymbol) GetLocation() Location { return s.Location }

// TraitSymbol represents a trait declaration
type TraitSymbol struct {
	Name          string
	GenericParams []string
	Bounds        []string // trait bounds like Show + Eq
	Methods       map[string]*TraitMethodSymbol
	Location      Location
	IsPublic      bool
}

func (TraitSymbol) symbolNode()             {}
func (s TraitSymbol) GetName() string       { return s.Name }
func (s TraitSymbol) GetLocation() Location { return s.Location }

// TraitImplSymbol represents a trait implementation
type TraitImplSymbol struct {
	TraitName string
	ForType   types.Type
	Methods   map[string]*TraitMethodImplSymbol
	Location  Location
}

func (TraitImplSymbol) symbolNode() {}
func (s TraitImplSymbol) GetName() string {
	return s.TraitName + " for " + s.ForType.GetName()
}
func (s TraitImplSymbol) GetLocation() Location { return s.Location }

// TraitMethodImplSymbol represents a trait method implementation
type TraitMethodImplSymbol struct {
	Name     string
	Impl     *FunctionPatternSymbol
	Location Location
}

func (TraitMethodImplSymbol) symbolNode()             {}
func (s TraitMethodImplSymbol) GetName() string       { return s.Name }
func (s TraitMethodImplSymbol) GetLocation() Location { return s.Location }
