package types

import "fmt"

type GenericType struct {
	Name string // lowercase letter optionally followed by any number of letters or numbers
}

func (GenericType) typeNode() {}

func (g GenericType) IsNumericType() bool {
	return false
}

func (g GenericType) GetName() string {
	return g.Name
}

func (g GenericType) Print(indent string) {
	fmt.Printf("%sGenericType(%s)\n", indent, g.Name)
}
