package ast

import "github.com/Lyra-Language/lyra/pkg/types"

type AwaitExpr struct {
	ExprBase
	Operand Expression
}

func (e *AwaitExpr) GetName() string {
	return "await " + e.Operand.GetName()
}

type NegationExpr struct {
	ExprBase
	Operand Expression
}

func (e *NegationExpr) GetName() string { return "negation_expr" }

type AddressOfExpr struct {
	ExprBase
	Operand Expression
	IsMut   bool
}

func (e *AddressOfExpr) GetName() string { return "address_of_expr" }

type DerefExpr struct {
	ExprBase
	Operand Expression
}

func (e *DerefExpr) GetName() string { return "deref_expr" }

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
