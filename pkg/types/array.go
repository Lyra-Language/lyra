package types

import "fmt"

type StaticArrayType struct {
	ElementType Type
	Size        int
	Allocation  AllocationModifier
}

func (StaticArrayType) typeNode() {}

func (a StaticArrayType) GetName() string {
	elementTypeName := "?"
	if a.ElementType != nil {
		elementTypeName = a.ElementType.String()
	}
	return fmt.Sprintf("StaticArray<%s, %d>", elementTypeName, a.Size)
}

func (a StaticArrayType) String() string {
	return a.GetName()
}

type DynamicArrayType struct {
	ElementType Type
	Allocation  AllocationModifier
}

func (DynamicArrayType) typeNode() {}

func (a DynamicArrayType) GetName() string {
	elementName := "?"
	if a.ElementType != nil {
		elementName = a.ElementType.String()
	}
	return fmt.Sprintf("DynamicArray<%s>", elementName)
}

func (a DynamicArrayType) String() string {
	return a.GetName()
}
