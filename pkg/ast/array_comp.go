package ast

type ArrayCompExpr struct {
	ExprBase
	Generators []Generator
	Guards     []Expression
	Result     Expression
}

func (a *ArrayCompExpr) exprNode() {}

func (a *ArrayCompExpr) GetName() string {
	return "array_comp_expr"
}

type Generator struct {
	ExprBase
	Value      Expression
	Identifier string
}

func (g *Generator) exprNode() {}

func (g *Generator) GetName() string {
	return "generator"
}
