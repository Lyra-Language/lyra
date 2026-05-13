package ast

type MathBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator MathBinaryOp
	Right    Expression
}

func (m *MathBinaryOpExpr) GetName() string {
	return "math_binary_op_expr"
}

type MathBinaryOp string

const (
	MathBinaryOpAdd       MathBinaryOp = "+"
	MathBinaryOpSub       MathBinaryOp = "-"
	MathBinaryOpMul       MathBinaryOp = "*"
	MathBinaryOpDiv       MathBinaryOp = "/"
	MathBinaryOpMod       MathBinaryOp = "%"
	MathBinaryOpRemainder MathBinaryOp = "%%"
)

type MathAssignOpExpr struct {
	ExprBase
	Left     IdentifierExpr
	Operator MathAssignOp
	Right    Expression
}

func (m *MathAssignOpExpr) GetName() string {
	return "math_assign_op_expr"
}

type MathAssignOp string

const (
	MathAssignOpAdd       MathAssignOp = "+="
	MathAssignOpSub       MathAssignOp = "-="
	MathAssignOpMul       MathAssignOp = "*="
	MathAssignOpDiv       MathAssignOp = "/="
	MathAssignOpMod       MathAssignOp = "%="
	MathAssignOpRemainder MathAssignOp = "%%="
)
