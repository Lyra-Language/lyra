package ast

type DataConstructorExpr struct {
	ExprBase
	Constructor string
	Value       Expression
}

func (d *DataConstructorExpr) GetName() string { return d.Constructor }
