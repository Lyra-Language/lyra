package types

import (
	"fmt"
)

type ConstrainedType struct {
	Name        string
	Type        Type
	Constraints []Constraint
}

func (c ConstrainedType) typeNode()           {}
func (c ConstrainedType) IsNumericType() bool { return false }
func (c ConstrainedType) GetName() string     { return c.Name }
func (c ConstrainedType) Print(indent string) {
	fmt.Printf("%sConstrainedType(%s) {\n", indent, c.Name)
	for _, constraint := range c.Constraints {
		constraint.Print(indent + "  ")
	}
}

type Constraint interface {
	constraintNode()
	Print(indent string)
}

type RangeConstraint struct {
	Start      MathConstraintExpr
	Comparator string
	End        MathConstraintExpr
}

func (r RangeConstraint) constraintNode() {}
func (r RangeConstraint) Print(indent string) {
	fmt.Printf("%sRangeConstraint {\n", indent)
	if r.Start != nil {
		fmt.Printf("%s  Start: {\n", indent)
		r.Start.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
	fmt.Printf("%s  Comparator: %s\n", indent, r.Comparator)
	if r.End != nil {
		fmt.Printf("%s  End: {\n", indent)
		r.End.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type MathConstraintExpr interface {
	mathConstraintExprNode()
	GetName() string
	Print(indent string)
}

// MathConstraintLiteralExpr implements MathConstraintExpr
type MathConstraintLiteralExpr struct {
	Value any
	Type  Type
}

func (m *MathConstraintLiteralExpr) mathConstraintExprNode() {}

func (m *MathConstraintLiteralExpr) GetName() string {
	return fmt.Sprintf("%v", m.Value)
}

func (m *MathConstraintLiteralExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintLiteralExpr(%s)\n", indent, m.Value)
	if m.Type != nil {
		fmt.Printf("%s  Type: {\n", indent)
		m.Type.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
}

// MathConstraintIdentifierExpr implements MathConstraintExpr
type MathConstraintIdentifierExpr struct {
	Name    string
	Type    Type
	IsConst bool
}

func (m *MathConstraintIdentifierExpr) mathConstraintExprNode() {}

func (m *MathConstraintIdentifierExpr) GetName() string {
	return m.Name
}

func (m *MathConstraintIdentifierExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintIdentifierExpr(%s)\n", indent, m.Name)
	if m.Type != nil {
		fmt.Printf("%s  Type: {\n", indent)
		m.Type.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
}

// MathConstraintBinaryOpExpr implements MathConstraintExpr
type MathConstraintBinaryOpExpr struct {
	Left     MathConstraintExpr
	Operator MathConstraintBinaryOp
	Right    MathConstraintExpr
}

func (m *MathConstraintBinaryOpExpr) mathConstraintExprNode() {}

func (m *MathConstraintBinaryOpExpr) GetName() string {
	return fmt.Sprintf("%s %s %s", m.Left.GetName(), m.Operator, m.Right.GetName())
}

func (m *MathConstraintBinaryOpExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintBinaryOpExpr(%s)\n", indent, m.GetName())
	fmt.Printf("%s  Left: {\n", indent)
	m.Left.Print(indent + "    ")
	fmt.Printf("%s  Operator: %s\n", indent, m.Operator)
	fmt.Printf("%s  Right: {\n", indent)
	m.Right.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}

type MathConstraintBinaryOp string

const (
	MathConstraintBinaryOpAdd MathConstraintBinaryOp = "+"
	MathConstraintBinaryOpSub MathConstraintBinaryOp = "-"
	MathConstraintBinaryOpMul MathConstraintBinaryOp = "*"
	MathConstraintBinaryOpDiv MathConstraintBinaryOp = "/"
)

// MathConstraintNegationExpr implements MathConstraintExpr
type MathConstraintNegationExpr struct {
	Operand MathConstraintExpr
}

func (m *MathConstraintNegationExpr) mathConstraintExprNode() {}

func (m *MathConstraintNegationExpr) GetName() string {
	return fmt.Sprintf("-%s", m.Operand.GetName())
}

func (m *MathConstraintNegationExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintNegationExpr(%s)\n", indent, m.GetName())
	fmt.Printf("%s  Operand: {\n", indent)
	m.Operand.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}
