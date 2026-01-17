package types

import (
	"fmt"
	"strings"
)

type FunctionType struct {
	ParameterTypes []ParameterType
	ReturnType     Type
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
	fmt.Printf("%sFunctionType(%s)\n", indent, f.GetName())
}

type ParameterType struct {
	Modifier Modifier
	Type     Type
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
