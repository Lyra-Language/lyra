package types

import (
	"fmt"
	"strings"
)

type TupleType struct {
	Name     string // uppercase letter optionally followed by any number of letters or numbers
	Elements []Type
}

func (TupleType) typeNode() {}

func (t TupleType) GetName() string {
	elementNames := make([]string, len(t.Elements))
	for i, element := range t.Elements {
		elementName := "?"
		if element != nil {
			elementName = element.String()
		}
		elementNames[i] = elementName
	}
	name := "AnonymousTuple"
	if t.Name != "" {
		name = t.Name
	}
	return fmt.Sprintf("%s(%s)", name, strings.Join(elementNames, ", "))
}

func (t TupleType) String() string {
	return t.GetName()
}

func (t TupleType) Print(indent string) {
	fmt.Printf("%sTupleType(%s) {\n", indent, t.Name)
	fmt.Printf("%s\tElements: {\n", indent)
	for _, element := range t.Elements {
		element.Print(indent + "\t\t")
	}
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}
