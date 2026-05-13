package ast

type YieldExpr struct {
	ExprBase
	Value Expression
}

func (y *YieldExpr) GetName() string { return "yield_expr" }

type YieldFromExpr struct {
	ExprBase
	Generator Expression
}

func (y *YieldFromExpr) GetName() string { return "yield_from_expr" }
