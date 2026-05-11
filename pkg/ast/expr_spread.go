package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type SpreadExpr struct {
	ExprBase
	Name string
}

func (e *SpreadExpr) GetType() types.Type {
	return nil
}

func (e *SpreadExpr) String() string {
	return "..." + e.Name
}