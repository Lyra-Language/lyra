package types

import "fmt"

type StructType struct {
	Name          string // uppercase letter optionally followed by any number of letters or numbers
	Fields        []StructField
	GenericParams []GenericType
	Allocation    AllocationModifier
}

func (StructType) typeNode() {}

func (s StructType) GetName() string {
	return s.Name
}

func (s StructType) String() string {
	return s.GetName()
}

func (s StructType) Print(indent string) {
	fmt.Printf("%sStructType(%s) {\n", indent, s.Name)
	if s.Allocation != "" {
		fmt.Printf("%s  Allocation: %s\n", indent, s.Allocation)
	}
	if len(s.GenericParams) > 0 {
		fmt.Printf("%s  GenericParams: %s\n", indent, s.GenericParams)
	}
	fmt.Printf("%s  Fields: {\n", indent)
	for _, field := range s.Fields {
		field.Print(indent + "    ")
	}
	fmt.Printf("%s}\n", indent+"  ")
	fmt.Printf("%s}\n", indent)
}

type StructField struct {
	Name         string
	Type         Type
	DefaultValue any
}

func (s StructField) Print(indent string) {
	fmt.Printf("%sStructField(%s: %s)\n", indent, s.Name, s.Type.String())
	if s.DefaultValue != nil {
		fmt.Printf("%s  DefaultValue: %v\n", indent, s.DefaultValue)
	}
}
