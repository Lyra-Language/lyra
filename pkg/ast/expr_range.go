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
	fmt.Printf("%s\tStart: {\n", indent)
	r.Start.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tEndOperator: %s\n", indent, r.EndOperator)
	fmt.Printf("%s\tEnd: {\n", indent)
	r.End.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	if r.Step != nil {
		fmt.Printf("%s\tStep: {\n", indent)
		r.Step.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
