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
