package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type Expression interface {
	AstNode
	exprNode()
	GetName() string
	GetType() types.Type
}

// Base struct to embed in all expression types
type ExprBase struct {
	AstBase
	Type types.Type
}

func (e *ExprBase) node()                 {}
func (e *ExprBase) exprNode()             {}
func (e *ExprBase) GetLocation() Location { return e.Location }
func (e *ExprBase) GetName() string       { return "" }
func (e *ExprBase) GetType() types.Type   { return e.Type }

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

func (g *GuardExpr) GetType() types.Type {
	return nil
}
