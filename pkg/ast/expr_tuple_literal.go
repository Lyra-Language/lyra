package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type TupleLiteralExpr struct {
	ExprBase
	Elements         []Expression
	GenericArguments []types.Type
	Name             string
}

func (t *TupleLiteralExpr) exprNode() {}

func (t *TupleLiteralExpr) GetName() string {
	return "tuple_literal_expr"
}
