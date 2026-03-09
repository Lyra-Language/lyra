package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type StructInstance struct {
	ExprBase
	Name        string
	GenericArgs []types.Type
	Fields      []StructField
}

func (s *StructInstance) GetName() string {
	return s.Name
}

func (s *StructInstance) GetType() types.Type {
	return nil
}

func (s *StructInstance) Print(indent string) {
	fmt.Printf("%sStructInstanceExpr(%s) {\n", indent, s.Name)
	if s.GenericArgs != nil {
		fmt.Printf("%s\tGenericArgs: {\n", indent)
		for _, genericArg := range s.GenericArgs {
			genericArg.Print(indent + "\t\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s\tFields: {\n", indent)
	for _, field := range s.Fields {
		field.Print(indent + "\t\t")
	}
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}

type StructField struct {
	Name  string
	Value Expression
}

func (s *StructField) Print(indent string) {
	if s.Name != "" {
		fmt.Printf("%sStructField(%s: %s)\n", indent, s.Name, s.Value.GetName())
	} else {
		fmt.Printf("%sStructFieldShorthand(%s)\n", indent, s.Value.GetName())
	}
}
