package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type TupleLiteralExpr struct {
	ExprBase
	Elements         []Expression
	GenericArguments []types.Type
	Name             string
}

func (t *TupleLiteralExpr) exprNode() {}

func (t *TupleLiteralExpr) GetName() string {
	return "tuple_literal_expr"
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
	if t.GenericArguments != nil {
		fmt.Printf("%s\tGenericArguments: {\n", indent)
		for _, genericArgument := range t.GenericArguments {
			genericArgument.Print(indent + "\t\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s\tElements: {\n", indent)
	for _, element := range t.Elements {
		element.Print(indent + "\t\t")
	}
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}
