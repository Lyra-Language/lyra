package types

import (
	"fmt"
	"strings"
)

type TupleType struct {
	Elements []Type
}

func (TupleType) typeNode() {}

func (t TupleType) IsNumericType() bool {
	return false
}

func (t TupleType) GetName() string {
	elementNames := make([]string, len(t.Elements))
	for i, element := range t.Elements {
		elementName := "?"
		if element != nil {
			elementName = element.GetName()
		}
		elementNames[i] = elementName
	}
	return fmt.Sprintf("(%s)", strings.Join(elementNames, ", "))
}

func (t TupleType) Print(indent string) {
	fmt.Printf("%sTupleType(%s)\n", indent, t.GetName())
	for _, element := range t.Elements {
		element.Print(indent + "  ")
	}
}
