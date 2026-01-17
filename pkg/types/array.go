package types

import "fmt"

type ArrayType struct {
	ElementType Type
}

func (ArrayType) typeNode() {}

func (a ArrayType) IsNumericType() bool {
	return false
}

func (a ArrayType) GetName() string {
	elementName := "?"
	if a.ElementType != nil {
		elementName = a.ElementType.GetName()
	}
	return fmt.Sprintf("Array<%s>", elementName)
}

func (a ArrayType) Print(indent string) {
	fmt.Printf("%sArrayType(%s)\n", indent, a.GetName())
}
