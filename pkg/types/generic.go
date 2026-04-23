package types

type GenericType struct {
	Name string // lowercase letter optionally followed by any number of letters or numbers
}

func (GenericType) typeNode() {}

func (g GenericType) GetName() string {
	return g.Name
}

func (g GenericType) String() string {
	return g.GetName()
}
