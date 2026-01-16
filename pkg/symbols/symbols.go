package symbols

import (
	"fmt"

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
	Clauses       []*FunctionClauseSymbol
	Location      Location
	IsPublic      bool
	IsPure        bool
	IsAsync       bool
}

func (FunctionSymbol) symbolNode()             {}
func (s FunctionSymbol) GetName() string       { return s.Name }
func (s FunctionSymbol) GetLocation() Location { return s.Location }

// FunctionClauseSymbol represents a function clause
type FunctionClauseSymbol struct {
	ParameterPatterns []Pattern
	Guard             *GuardSymbol
	Body              *FunctionBodySymbol
	Location          Location
}

func (FunctionClauseSymbol) symbolNode()             {}
func (s FunctionClauseSymbol) GetName() string       { return "function clause" }
func (s FunctionClauseSymbol) GetLocation() Location { return s.Location }

type Pattern interface {
	isPattern()
	GetName() string
}

type IdentifierPattern struct {
	Name string
}

func (IdentifierPattern) isPattern()        {}
func (p IdentifierPattern) GetName() string { return p.Name }

type LiteralPattern struct {
	Value any
}

func (LiteralPattern) isPattern()        {}
func (p LiteralPattern) GetName() string { return fmt.Sprintf("%v", p.Value) }

// TODO: add other patterns

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
	Name           string
	Type           types.Type // may be nil if needs inference
	InitExpression *ExpressionSymbol
	Location       Location
	IsMutable      bool // var vs let
	IsConstant     bool // const
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
	Impl     *FunctionClauseSymbol
	Location Location
}

func (TraitMethodImplSymbol) symbolNode()             {}
func (s TraitMethodImplSymbol) GetName() string       { return s.Name }
func (s TraitMethodImplSymbol) GetLocation() Location { return s.Location }

// FunctionBodySymbol represents a function body
type FunctionBodySymbol struct {
	Block      *BlockSymbol
	Expression *ExpressionSymbol
	Location   Location
}

func (FunctionBodySymbol) symbolNode()             {}
func (s FunctionBodySymbol) GetName() string       { return "function body" }
func (s FunctionBodySymbol) GetLocation() Location { return s.Location }

// BlockSymbol represents a block of code
type BlockSymbol struct {
	Statements []*StatementSymbol
	Location   Location
}

func (BlockSymbol) symbolNode()             {}
func (s BlockSymbol) GetName() string       { return "block" }
func (s BlockSymbol) GetLocation() Location { return s.Location }

// ExpressionSymbol represents an expression
type ExpressionSymbol struct {
	Type     types.Type
	Location Location
}

func (ExpressionSymbol) symbolNode()             {}
func (s ExpressionSymbol) GetName() string       { return "expression" }
func (s ExpressionSymbol) GetLocation() Location { return s.Location }

// StatementSymbol represents a statement
type StatementSymbol struct {
	Expression *ExpressionSymbol // nil if not an expression statement
	Location   Location
}

func (StatementSymbol) symbolNode()             {}
func (s StatementSymbol) GetName() string       { return "statement" }
func (s StatementSymbol) GetLocation() Location { return s.Location }
