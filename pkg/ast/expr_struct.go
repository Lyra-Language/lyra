package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type StructInstanceExpr struct {
	ExprBase
	Name        string
	GenericArgs []types.Type
	// The base a record update copies from — `existingPlayer` in
	// `Player { existingPlayer | health: newHealth }`. Any postfix expression, a call
	// included (`Duration { duration() | days: 1 }`), so it is an Expression and not
	// an *IdentifierExpr: it was the latter until 09/19, when the grammar widened.
	BaseStruct Expression
	Fields     []StructField
}

func (s *StructInstanceExpr) GetName() string {
	return s.Name
}

type AnonymousStructInstanceExpr struct {
	ExprBase
	BaseStruct Expression
	Fields     []StructField
}

func (s *AnonymousStructInstanceExpr) GetName() string {
	return "anonymous struct"
}

type StructField struct {
	Name  string
	Value Expression
}
