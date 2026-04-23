package ast

import (

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

type MathBinaryOp string

const (
	MathBinaryOpAdd MathBinaryOp = "+"
	MathBinaryOpSub MathBinaryOp = "-"
	MathBinaryOpMul MathBinaryOp = "*"
	MathBinaryOpDiv MathBinaryOp = "/"
)
