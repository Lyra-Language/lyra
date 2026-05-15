package ast

type NotBooleanExpr struct {
	ExprBase
	Expression Expression
}

func (n *NotBooleanExpr) GetName() string {
	return "not_boolean_expr"
}

type BooleanBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator BooleanBinaryOp
	Right    Expression
}

func (b *BooleanBinaryOpExpr) GetName() string {
	return "boolean_binary_op_expr"
}

type BooleanBinaryOp string

const (
	BooleanBinaryOpLT  BooleanBinaryOp = "<"
	BooleanBinaryOpLTE BooleanBinaryOp = "<="
	BooleanBinaryOpGT  BooleanBinaryOp = ">"
	BooleanBinaryOpGTE BooleanBinaryOp = ">="
	BooleanBinaryOpEq  BooleanBinaryOp = "=="
	BooleanBinaryOpNEq BooleanBinaryOp = "!="
	BooleanBinaryOpAnd BooleanBinaryOp = "&&"
	BooleanBinaryOpOr  BooleanBinaryOp = "||"
)
