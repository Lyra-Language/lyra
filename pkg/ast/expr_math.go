package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type MathBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator MathBinaryOp
	Right    Expression
}

func (m *MathBinaryOpExpr) GetName() string {
	return "math_binary_op_expr"
}

func (m *MathBinaryOpExpr) GetType() types.Type {
	leftType := m.Left.GetType()
	rightType := m.Right.GetType()
	if types.TypesEqual(leftType, types.PrimitiveType{Name: types.Int}) && types.TypesEqual(rightType, types.PrimitiveType{Name: types.Int}) {
		return types.PrimitiveType{Name: types.Int}
	}
	if types.TypesEqual(leftType, types.PrimitiveType{Name: types.Float}) && types.TypesEqual(rightType, types.PrimitiveType{Name: types.Float}) {
		return types.PrimitiveType{Name: types.Float}
	}
	return types.Type(nil)
}

func (m *MathBinaryOpExpr) Print(indent string) {
	fmt.Printf("%sMathBinaryOpExpr(%s) {\n", indent, m.GetName())
	fmt.Printf("%s  Left: {\n", indent)
	m.Left.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Operator: %s\n", indent, m.Operator)
	fmt.Printf("%s  Right: {\n", indent)
	m.Right.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s}\n", indent)
}

type MathBinaryOp string

const (
	MathBinaryOpAdd MathBinaryOp = "+"
	MathBinaryOpSub MathBinaryOp = "-"
	MathBinaryOpMul MathBinaryOp = "*"
	MathBinaryOpDiv MathBinaryOp = "/"
)
