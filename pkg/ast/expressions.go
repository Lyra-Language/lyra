package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type Expression interface {
	exprNode()
	GetName() string
	Print(indent string)
}

// Base struct to embed in all expression types
type ExprBase struct {
	AstBase
	Type types.Type
}

func (e *ExprBase) exprNode()             {}
func (e *ExprBase) GetLocation() Location { return e.Location }
func (e *ExprBase) GetName() string       { return "" }
func (e *ExprBase) Print(indent string)   {}

type IdentifierExpr struct {
	ExprBase
	Name string
}

func (i *IdentifierExpr) GetName() string {
	return i.Name
}

func (i *IdentifierExpr) Print(indent string) {
	fmt.Printf("%sIdentifierExpr(%s)\n", indent, i.Name)
}

type GuardExpr struct {
	ExprBase
	Condition Expression
}

func (g *GuardExpr) GetName() string {
	return fmt.Sprintf("guard %s", g.Condition.GetName())
}

func (g *GuardExpr) Print(indent string) {
	fmt.Printf("%sGuardExpr(%s)\n", indent, g.Condition.GetName())
	fmt.Printf("%s  Condition: {\n", indent)
	g.Condition.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}
