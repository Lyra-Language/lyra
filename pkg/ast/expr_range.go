package ast

import (

	"github.com/Lyra-Language/lyra/pkg/types"
)

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

func (r *RangeExpr) GetType() types.Type {
	stepType := types.Type(nil)
	if r.Step != nil {
		stepType = r.Step.GetType()
	}
	return types.RangeType{
		Start:       r.Start.GetType(),
		End:         r.End.GetType(),
		EndOperator: r.EndOperator,
		Step:        stepType,
	}
}
