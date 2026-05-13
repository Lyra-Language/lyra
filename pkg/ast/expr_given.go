package ast

type GivenExpr struct {
	ExprBase
	Body     Expression
	Bindings []Statement
}

func (g *GivenExpr) GetName() string { return "given_expr" }
