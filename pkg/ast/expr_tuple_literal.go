package ast

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type TupleLiteralExpr struct {
	ExprBase
	Elements []Expression
	Name     string
}

func (t *TupleLiteralExpr) exprNode() {}

func (t *TupleLiteralExpr) GetName() string {
	elementNames := make([]string, len(t.Elements))
	for i, element := range t.Elements {
		elementNames[i] = element.GetName()
	}
	return fmt.Sprintf("tuple_literal(Name: %s, Elements: %s)", t.Name, strings.Join(elementNames, ", "))
}

func (t *TupleLiteralExpr) GetType() types.Type {
	if t.ExprBase.Type != nil {
		return t.ExprBase.Type
	}
	elements := make([]types.Type, len(t.Elements))
	for i, element := range t.Elements {
		elements[i] = element.GetType()
	}
	return types.TupleType{Name: t.Name, Elements: elements}
}

func (t *TupleLiteralExpr) Print(indent string) {
	fmt.Printf("%sTupleLiteralExpr(Name: %s) {\n", indent, t.Name)
	for _, element := range t.Elements {
		element.Print(indent + "    ")
	}
	fmt.Printf("%s}\n", indent)
}
