package ast

import "fmt"

type MathBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator MathBinaryOp
	Right    Expression
}

func (m *MathBinaryOpExpr) GetName() string {
	return fmt.Sprintf("%s %s %s", m.Left.GetName(), m.Operator, m.Right.GetName())
}

func (m *MathBinaryOpExpr) Print(indent string) {
	fmt.Printf("%sMathBinaryOpExpr(%s)\n", indent, m.GetName())
	fmt.Printf("%s  Left: {\n", indent)
	m.Left.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Operator: %s\n", indent, m.Operator)
	fmt.Printf("%s  Right: {\n", indent)
	m.Right.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}

type MathBinaryOp string

const (
	MathBinaryOpAdd MathBinaryOp = "+"
	MathBinaryOpSub MathBinaryOp = "-"
	MathBinaryOpMul MathBinaryOp = "*"
	MathBinaryOpDiv MathBinaryOp = "/"
)
