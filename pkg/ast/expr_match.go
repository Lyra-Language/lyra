package ast

import (
	"fmt"
)

type MatchExpr struct {
	ExprBase
	Value     Expression
	MatchArms []MatchArm
}

func (m *MatchExpr) Print(indent string) {
	fmt.Printf("%sMatchExpr {\n", indent)
	fmt.Printf("%s\tValue: {\n", indent)
	m.Value.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tMatchArms: {\n", indent)
	for _, matchArm := range m.MatchArms {
		matchArm.Print(indent + "\t\t")
	}
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
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

func (m *MatchArm) Print(indent string) {
	fmt.Printf("%sMatchArm {\n", indent)
	m.Pattern.Print(indent + "\t")
	fmt.Printf("\n")
	if m.Guard != nil {
		fmt.Printf("%s\tGuard: {\n", indent)
		m.Guard.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s\tBody: {\n", indent)
	m.Body.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}
