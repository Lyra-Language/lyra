package ast

import "github.com/Lyra-Language/lyra/pkg/types"

type AwaitExpr struct {
	ExprBase
	Operand Expression
}

func (e *AwaitExpr) GetName() string {
	return "await " + e.Operand.GetName()
}

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
