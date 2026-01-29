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
	fmt.Printf("%sDataTypeConstructor(%s)", indent, c.Name)
	if len(c.Params) > 0 {
		fmt.Printf(" (\n")
		for idx, param := range c.Params {
			param.Print(indent + "  ")
			if idx < len(c.Params)-1 {
				fmt.Printf(",\n")
			}
		}
		fmt.Printf("%s)\n", indent)
	}
	if len(c.Fields) > 0 {
		fmt.Printf(" {\n")
		for name, field := range c.Fields {
			fmt.Printf("%s%s: %s\n", indent+"  ", name, field.Type.GetName())
		}
		fmt.Printf("%s}\n", indent)
	}
	fmt.Printf("\n")
}
