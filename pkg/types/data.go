package types

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

// DataTypeConstructor can have different shapes
type DataTypeConstructor struct {
	Name   string
	Params []Type        // for Simple(int) style
	Fields []StructField // for Node { left: Tree, value: t } style
}
