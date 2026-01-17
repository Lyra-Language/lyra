package types

import "fmt"

type StructType struct {
	Name   string // uppercase letter optionally followed by any number of letters or numbers
	Fields map[string]StructField
}

func (StructType) typeNode() {}

func (s StructType) IsNumericType() bool {
	return false
}

func (s StructType) GetName() string {
	return s.Name
}

func (s StructType) Print(indent string) {
	fmt.Printf("%sStructType(%s) {\n", indent, s.Name)
	for _, field := range s.Fields {
		field.Print(indent + "  ")
	}
	fmt.Printf("%s}\n", indent)
}

type StructField struct {
	Name         string
	Type         Type
	DefaultValue any
}

func (s StructField) Print(indent string) {
	fmt.Printf("%sStructField(%s: %s)\n", indent, s.Name, s.Type.GetName())
	if s.DefaultValue != nil {
		fmt.Printf("%s  DefaultValue: %v\n", indent, s.DefaultValue)
	}
}
