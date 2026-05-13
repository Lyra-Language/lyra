package ast

type UnsafeBlockExpr struct {
	ExprBase
	Body BlockExpr
}

func (u *UnsafeBlockExpr) GetName() string { return "unsafe_block" }
