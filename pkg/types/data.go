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

type DataTypeConstructor struct {
	Name   string
	Params []Type
}
