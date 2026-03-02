package ast

import (
	"fmt"

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

func (r *RangeExpr) Print(indent string) {
	fmt.Printf("%sRangeExpr {\n", indent)
	fmt.Printf("%s  Start: {\n", indent)
	r.Start.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  EndOperator: %s\n", indent, r.EndOperator)
	fmt.Printf("%s  End: {\n", indent)
	r.End.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	if r.Step != nil {
		fmt.Printf("%s  Step: {\n", indent)
		r.Step.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
