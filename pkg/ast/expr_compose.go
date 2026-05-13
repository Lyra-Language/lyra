package ast

type ComposeExpr struct {
	ExprBase
	Left     Expression
	Operator string
	Right    Expression
}

func (c *ComposeExpr) GetName() string { return "compose_expr" }
