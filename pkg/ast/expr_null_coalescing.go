package ast

import (

	"github.com/Lyra-Language/lyra/pkg/types"
)

type NullCoalescingExpr struct {
	ExprBase
	Optional Expression
	Default  Expression
}

func (e *NullCoalescingExpr) exprNode() {}

func (e *NullCoalescingExpr) GetName() string {
	return "null_coalescing"
}

func (e *NullCoalescingExpr) GetType() types.Type {
	return nil
}
