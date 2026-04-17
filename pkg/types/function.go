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

func (f FunctionType) IsNumericType() bool {
	return false
}

func (f FunctionType) GetName() string {
	parameterTypes := make([]string, len(f.ParameterTypes))
	for i, parameterType := range f.ParameterTypes {
		parameterTypes[i] = parameterType.GetName()
	}
	returnTypeName := "?"
	if f.ReturnType != nil {
		returnTypeName = f.ReturnType.GetName()
	}
	return fmt.Sprintf("(%s) -> %s", strings.Join(parameterTypes, ", "), returnTypeName)
}

func (f FunctionType) Print(indent string) {
	fmt.Printf("%sFunctionType {\n", indent)
	if len(f.ParameterTypes) > 0 {
		fmt.Printf("%s\tParameterTypes: {\n", indent)
		for _, parameterType := range f.ParameterTypes {
			fmt.Printf("%s\t\t%s\n", indent, parameterType.GetName())
		}
		fmt.Printf("%s\t}\n", indent)
	}
	if f.ReturnType != nil {
		fmt.Printf("%s\tReturnType: %s\n", indent, f.ReturnType.GetName())
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
		return fmt.Sprintf("%s%s", modifier, p.Type.GetName())
	}
	return modifier
}

type Modifier string

const (
	Ref Modifier = "ref"
	Mut Modifier = "mut"
	Own Modifier = "own"
)
