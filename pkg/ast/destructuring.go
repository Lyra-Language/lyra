package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type DestructuringDeclaration struct {
	AstBase
	Keyword string
	Pattern Pattern
	Type    types.Type
	Value   Expression
}

func (d *DestructuringDeclaration) GetName() string { return d.Pattern.GetName() }
func (d *DestructuringDeclaration) Print(indent string) {
	fmt.Printf("%sDestructuringDeclaration(%s) {\n", indent, d.Keyword)
	d.Pattern.Print(indent + "\t")
	fmt.Printf("\n%s\tValue: {\n", indent)
	d.Value.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	if d.Type != nil {
		fmt.Printf("%s\tType: {\n", indent)
		d.Type.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
