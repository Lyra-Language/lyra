package types

import "fmt"

type StaticArrayType struct {
	ElementType Type
	Size        int
	Allocation  AllocationModifier
}

func (StaticArrayType) typeNode() {}

func (a StaticArrayType) IsNumericType() bool {
	return false
}

func (a StaticArrayType) GetName() string {
	elementTypeName := "?"
	if a.ElementType != nil {
		elementTypeName = a.ElementType.GetName()
	}
	return fmt.Sprintf("StaticArray<%s, %d>", elementTypeName, a.Size)
}

func (a StaticArrayType) Print(indent string) {
	fmt.Printf("%sStaticArrayType(%s)\n", indent, a.GetName())
	fmt.Printf("%s  ElementType: {\n", indent)
	fmt.Printf("%s  Size: %d\n", indent, a.Size)
	a.ElementType.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}

type DynamicArrayType struct {
	ElementType Type
	Allocation  AllocationModifier
}

func (DynamicArrayType) typeNode() {}

func (a DynamicArrayType) IsNumericType() bool {
	return false
}

func (a DynamicArrayType) GetName() string {
	elementName := "?"
	if a.ElementType != nil {
		elementName = a.ElementType.GetName()
	}
	return fmt.Sprintf("DynamicArray<%s>", elementName)
}

func (a DynamicArrayType) Print(indent string) {
	fmt.Printf("%sDynamicArrayType(%s)\n", indent, a.GetName())
	fmt.Printf("%s  ElementType: {\n", indent)
	a.ElementType.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}
