package ast

import "fmt"

type IfExpr struct {
	ExprBase
	Condition Expression
	Then      Expression // nil if no then
	Else      Expression // nil if no else
}

func (i *IfExpr) GetName() string {
	return "if_expr"
}

type MatchExpr struct {
	ExprBase
	Scrutinee Expression
	MatchArms []MatchArm
}

type MatchArm struct {
	Pattern Pattern
	Guard   *GuardExpr // nil when the source omits `if guard { ... }`
	Body    Expression
}

func (m *MatchArm) GetName() string {
	if m.Guard != nil {
		return fmt.Sprintf("match %s if %s { %s }", m.Pattern.GetName(), m.Guard.GetName(), m.Body.GetName())
	}
	return fmt.Sprintf("match %s { %s }", m.Pattern.GetName(), m.Body.GetName())
}

type BlockExpr struct {
	ExprBase
	Statements []Statement
}

func (b *BlockExpr) exprNode() {}
func (b *BlockExpr) GetName() string {
	return "block_expr"
}
