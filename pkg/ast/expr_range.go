package ast

type RangeExpr struct {
	ExprBase
	Start       Expression
	End         Expression
	EndOperator string
	Step        Expression
}

func (r *RangeExpr) exprNode() {}

func (r *RangeExpr) GetName() string {
	return "range_expression"
}
