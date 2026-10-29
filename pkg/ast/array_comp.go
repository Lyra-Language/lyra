package ast

import "github.com/Lyra-Language/lyra/pkg/types"

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
	// Type is the clause's written type (`[i: u8 in 0..<10 | …]`), nil when there is none;
	// it means what a loop variable's does (`ForInLoopExpr.KeyType`).
	Type types.Type
}

func (g *Generator) exprNode() {}

func (g *Generator) GetName() string {
	return "generator"
}
