package ast

type NullCoalescingExpr struct {
	ExprBase
	Optional Expression
	Default  Expression
}

func (e *NullCoalescingExpr) exprNode() {}

func (e *NullCoalescingExpr) GetName() string {
	return "null_coalescing"
}
