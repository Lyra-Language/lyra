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
	fmt.Printf("%sStaticArrayType {\n", indent)
	fmt.Printf("%s\tSize: %d\n", indent, a.Size)
	if a.ElementType != nil {
		fmt.Printf("%s\tElementType: {\n", indent)
		a.ElementType.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	if a.Allocation != "" {
		fmt.Printf("%s\tAllocation: %s\n", indent, a.Allocation)
	}
	fmt.Printf("%s}\n", indent)
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
	fmt.Printf("%s\tElementType: {\n", indent)
	a.ElementType.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
}
