package symbols

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// TraitSymbol represents a trait declaration
// Note: This remains as a symbol since traits are more complex declarations
// TODO: Consider converting to an AST node in the future
type TraitSymbol struct {
	Name          string
	GenericParams []string
	Bounds        []string // trait bounds like Show + Eq
	Methods       map[string]*TraitMethodSymbol
	Location      ast.Location
	IsPublic      bool
}

func (s *TraitSymbol) GetName() string           { return s.Name }
func (s *TraitSymbol) GetLocation() ast.Location { return s.Location }

// TraitMethodSymbol represents a trait method signature
type TraitMethodSymbol struct {
	Name      string
	Signature *types.FunctionType
	Location  ast.Location
}

func (s *TraitMethodSymbol) GetName() string           { return s.Name }
func (s *TraitMethodSymbol) GetLocation() ast.Location { return s.Location }

// TraitImplSymbol represents a trait implementation
type TraitImplSymbol struct {
	TraitName string
	ForType   types.Type
	Methods   map[string]*TraitMethodImplSymbol
	Location  ast.Location
}

func (s *TraitImplSymbol) GetName() string {
	return s.TraitName + " for " + s.ForType.GetName()
}
func (s *TraitImplSymbol) GetLocation() ast.Location { return s.Location }

// TraitMethodImplSymbol represents a trait method implementation
type TraitMethodImplSymbol struct {
	Name     string
	Impl     *ast.FunctionClause // Points to AST node
	Location ast.Location
}

func (s *TraitMethodImplSymbol) GetName() string           { return s.Name }
func (s *TraitMethodImplSymbol) GetLocation() ast.Location { return s.Location }
