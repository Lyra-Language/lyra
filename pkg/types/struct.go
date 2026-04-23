package types

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

type StructField struct {
	Name         string
	Type         Type
	DefaultValue any
}
