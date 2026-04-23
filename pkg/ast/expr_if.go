package ast

type IfExpr struct {
	ExprBase
	Condition Expression
	Then      Expression // nil if no then
	Else      Expression // nil if no else
}

func (i *IfExpr) GetName() string {
	return "if_expr"
}
