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
	// BooleanBinaryOpSpaceship is `<=>`, the three-way comparison. It is grouped
	// with the boolean operators because the grammar puts it among the relational
	// ones and it collects into the same node — but unlike every other operator
	// here its result is **not** `bool`: it is the prelude's `Ordering`
	// (`Less | Equal | Greater`), which is what makes it worth having rather than
	// a slower spelling of `<`.
	BooleanBinaryOpSpaceship BooleanBinaryOp = "<=>"
)
