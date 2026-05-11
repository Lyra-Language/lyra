package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type StructInstance struct {
	ExprBase
	Name        string
	GenericArgs []types.Type
	BaseStruct  *IdentifierExpr // For record update syntax, the base struct being updated (e.g. `existingPlayer` in `Player { existingPlayer | health: newHealth }`)
	Fields      []StructField
}

func (s *StructInstance) GetName() string {
	return s.Name
}

func (s *StructInstance) GetType() types.Type {
	return nil
}

type StructField struct {
	Name  string
	Value Expression
}
