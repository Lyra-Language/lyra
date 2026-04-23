package ast

import (

	"github.com/Lyra-Language/lyra/pkg/types"
)

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

// TODO: implement this correctly
func (a *ArrayCompExpr) GetType() types.Type {
	return nil
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

func (g *Generator) GetType() types.Type {
	return nil
}
