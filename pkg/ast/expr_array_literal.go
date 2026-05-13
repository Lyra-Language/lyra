package ast

type ArrayLiteralExpr struct {
	ExprBase
	Elements []Expression
}

func (a *ArrayLiteralExpr) exprNode() {}

func (a *ArrayLiteralExpr) GetName() string {
	return "array_literal_expr"
}
