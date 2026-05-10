package ast

type AwaitExpr struct {
	ExprBase
	Operand Expression
}

func (e *AwaitExpr) GetName() string {
	return "await " + e.Operand.GetName()
}