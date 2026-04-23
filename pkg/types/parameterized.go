package types

import "fmt"

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
func (p ParameterizedType) Print(indent string) {
	fmt.Printf("%sParameterizedType(%s) {\n", indent, p.Name)
	for _, typeArgument := range p.TypeArguments {
		typeArgument.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
}
