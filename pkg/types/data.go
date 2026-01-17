package types

import "fmt"

type DataType struct {
	Name         string // uppercase letter optionally followed by any number of letters or numbers
	Constructors map[string]DataTypeConstructor
}

func (DataType) typeNode() {}

func (d DataType) IsNumericType() bool {
	return false
}
func (d DataType) GetName() string {
	return d.Name
}

func (d DataType) Print(indent string) {
	fmt.Printf("%sDataType(%s) {\n", indent, d.Name)
	for _, constructor := range d.Constructors {
		constructor.Print(indent + "  ")
	}
	fmt.Printf("%s}\n", indent)
}

// Data constructor can have different shapes
type DataTypeConstructor struct {
	Name   string
	Params []Type                 // for Simple(Int) style
	Fields map[string]StructField // for Node { left: Tree, value: t } style
}

func (c DataTypeConstructor) Print(indent string) {
	if c.Params != nil {
		fmt.Printf("%sDataTypeConstructor(%s) (\n", indent, c.Name)
		for _, param := range c.Params {
			param.Print(indent + ", ")
		}
		fmt.Printf("%s)\n", indent)
	}
	if c.Fields != nil {
		fmt.Printf("%sDataTypeConstructor(%s) {\n", indent, c.Name)
		for name, field := range c.Fields {
			fmt.Printf("%s%s: %s\n", indent+"  ", name, field.Type.GetName())
		}
		fmt.Printf("%s)\n", indent)
	}
}
