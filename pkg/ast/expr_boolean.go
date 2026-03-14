package ast

import "fmt"

type BooleanBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator BooleanBinaryOp
	Right    Expression
}

func (b *BooleanBinaryOpExpr) GetName() string {
	return "boolean_binary_op_expr"
}

func (b *BooleanBinaryOpExpr) Print(indent string) {
	fmt.Printf("%sBooleanBinaryOpExpr {\n", indent)
	fmt.Printf("%s\tLeft: {\n", indent)
	b.Left.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tOperator: %s\n", indent, b.Operator)
	fmt.Printf("%s\tRight: {\n", indent)
	b.Right.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
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
