package ast

import (
	"fmt"
)

type MatchExpr struct {
	ExprBase
	Value     Expression
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
