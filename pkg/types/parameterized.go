package types

type ParameterizedType struct {
	Name          string
	TypeArguments []Type
}

func (ParameterizedType) typeNode() {}
func (p ParameterizedType) GetName() string {
	return p.Name
}
func (p ParameterizedType) String() string {
	return p.GetName()
}
