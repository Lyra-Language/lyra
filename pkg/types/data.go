package types

import "fmt"

type DataType struct {
	Name         string // uppercase letter optionally followed by any number of letters or numbers
	Constructors []DataTypeConstructor
	Allocation   AllocationModifier
}

func (DataType) typeNode() {}
func (d DataType) GetName() string {
	return d.Name
}

func (d DataType) String() string {
	return d.GetName()
}

func (d DataType) Print(indent string) {
	fmt.Printf("%sDataType(%s) {\n", indent, d.Name)
	for _, constructor := range d.Constructors {
		constructor.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
}

// DataTypeConstructor can have different shapes
type DataTypeConstructor struct {
	Name   string
	Params []Type        // for Simple(int) style
	Fields []StructField // for Node { left: Tree, value: t } style
}

func (c DataTypeConstructor) Print(indent string) {
	fmt.Printf("%sDataTypeConstructor(%s)", indent, c.Name)
	if len(c.Params) > 0 {
		fmt.Printf(" {\n")
		fmt.Printf("%s\tParams: (\n", indent)
		for idx, param := range c.Params {
			param.Print(indent + "\t\t")
			if idx < len(c.Params)-1 {
				fmt.Printf(",\n")
			}
		}
		fmt.Printf("%s\t)\n", indent)
		fmt.Printf("%s}", indent)
	}
	if len(c.Fields) > 0 {
		fmt.Printf(" {\n")
		fmt.Printf("%s\tFields: {\n", indent)
		for _, field := range c.Fields {
			fmt.Printf("%s\t\t%s: %s\n", indent, field.Name, field.Type.String())
		}
		fmt.Printf("%s\t}\n", indent)
		fmt.Printf("%s}", indent)
	}
	fmt.Printf("\n")
}
