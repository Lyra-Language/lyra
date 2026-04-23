package ast

import "fmt"

type IfThenExpr struct {
	ExprBase
	Condition Expression
	Then      Expression
	Else      Expression // nil if no else
}

func (i *IfThenExpr) GetName() string {
	return fmt.Sprintf("if %s then %s else %s", i.Condition.GetName(), i.Then.GetName(), i.Else.GetName())
}

type IfBlockExpr struct {
	ExprBase
	Condition Expression
	Then      Expression
	Else      Expression // nil if no else
}

func (i *IfBlockExpr) GetName() string {
	return fmt.Sprintf("if %s { %s } else { %s }", i.Condition.GetName(), i.Then.GetName(), i.Else.GetName())
}
