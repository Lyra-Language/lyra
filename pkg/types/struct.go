package types

type NamedStructType struct {
	Name          string // uppercase letter optionally followed by any number of letters or numbers
	Fields        []StructField
	GenericParams []GenericType
	Allocation    AllocationModifier
}

func (NamedStructType) typeNode() {}

func (s NamedStructType) GetName() string {
	return s.Name
}

func (s NamedStructType) String() string {
	return s.GetName()
}

type StructField struct {
	Name         string
	Type         Type
	DefaultValue any
}

type AnonymousStructType struct {
	Fields []StructField
}

func (AnonymousStructType) typeNode()        {}
func (AnonymousStructType) GetName() string  { return "" }
func (a AnonymousStructType) String() string { return "struct" }
