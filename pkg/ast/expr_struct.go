package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type StructInstanceExpr struct {
	ExprBase
	Name        string
	GenericArgs []types.Type
	BaseStruct  *IdentifierExpr // For record update syntax, the base struct being updated (e.g. `existingPlayer` in `Player { existingPlayer | health: newHealth }`)
	Fields      []StructField
}

func (s *StructInstanceExpr) GetName() string {
	return s.Name
}

type AnonymousStructInstanceExpr struct {
	ExprBase
	Fields []StructField
}

func (s *AnonymousStructInstanceExpr) GetName() string {
	return "anonymous struct"
}

type StructField struct {
	Name  string
	Value Expression
}
