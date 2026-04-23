package types

import (
	"fmt"
	"strings"
)

type FunctionType struct {
	ParameterTypes []ParameterType
	ReturnType     Type
	IsAsync        bool
	IsPure         bool
}

func (FunctionType) typeNode() {}

func (f FunctionType) GetName() string {
	parameterTypes := make([]string, len(f.ParameterTypes))
	for i, parameterType := range f.ParameterTypes {
		parameterTypes[i] = parameterType.GetName()
	}
	returnTypeName := "?"
	if f.ReturnType != nil {
		returnTypeName = f.ReturnType.String()
	}
	return fmt.Sprintf("(%s) -> %s", strings.Join(parameterTypes, ", "), returnTypeName)
}

func (f FunctionType) String() string {
	return f.GetName()
}

func (f FunctionType) Print(indent string) {
	fmt.Printf("%sFunctionType {\n", indent)
	if len(f.ParameterTypes) > 0 {
		fmt.Printf("%s\tParameterTypes: {\n", indent)
		for _, parameterType := range f.ParameterTypes {
			parameterType.Print(indent + "\t\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	if f.ReturnType != nil {
		fmt.Printf("%s\tReturnType: %s\n", indent, f.ReturnType.String())
	}
	if f.IsAsync {
		fmt.Printf("%s\tIsAsync: true\n", indent)
	}
	if f.IsPure {
		fmt.Printf("%s\tIsPure: true\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type ParameterType struct {
	Modifier     Modifier
	Type         Type
	DefaultValue any
}

func (p ParameterType) GetName() string {
	modifier := ""
	if p.Modifier != "" {
		modifier = string(p.Modifier) + " "
	}
	if p.Type != nil {
		return fmt.Sprintf("%s%s", modifier, p.Type.String())
	}
	return modifier
}

func (p ParameterType) Print(indent string) {
	fmt.Printf("%sParameterType {\n", indent)
	if p.Modifier != "" {
		fmt.Printf("%s\tModifier: %s\n", indent, p.Modifier)
	}
	fmt.Printf("%s\tType: {\n", indent)
	p.Type.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	if p.DefaultValue != nil {
		fmt.Printf("%s\tDefaultValue: %v\n", indent, p.DefaultValue)
	}
	fmt.Printf("%s}\n", indent)
}

type Modifier string

const (
	Ref Modifier = "ref"
	Mut Modifier = "mut"
	Own Modifier = "own"
)
