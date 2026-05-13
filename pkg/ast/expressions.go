package ast

import (
	"fmt"
)

type Expression interface {
	AstNode
	exprNode()
	GetName() string
}

// Base struct to embed in all expression types
type ExprBase struct {
	AstBase
}

func (e *ExprBase) node()                 {}
func (e *ExprBase) exprNode()             {}
func (e *ExprBase) GetLocation() Location { return e.Location }
func (e *ExprBase) GetName() string       { return "" }

type IdentifierExpr struct {
	ExprBase
	Name    string
	IsConst bool
}

func (i *IdentifierExpr) GetName() string {
	return i.Name
}

type GuardExpr struct {
	ExprBase
	Condition Expression
}

func (g *GuardExpr) GetName() string {
	return fmt.Sprintf("guard %s", g.Condition.GetName())
}
