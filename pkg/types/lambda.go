package types

import (
	"fmt"
	"strings"
)

type LambdaType struct {
	Parameters []ParameterType
	ReturnType ReturnType
}

func (LambdaType) typeNode() {}

func (t LambdaType) GetName() string {
	paramTypeNames := make([]string, len(t.Parameters))
	for i, param := range t.Parameters {
		paramTypeNames[i] = param.String()
	}
	return fmt.Sprintf("(%s) -> %s", strings.Join(paramTypeNames, ", "), t.ReturnType.String())
}

func (t LambdaType) String() string {
	return t.GetName()
}

type ParameterType struct {
	Modifier     AllocationModifier
	Type         Type
	DefaultValue any
}

func (p ParameterType) GetName() string {
	modifier := ""
	if p.Modifier != AllocationModifier("") {
		modifier = string(p.Modifier) + " "
	}
	if p.Type != nil {
		return fmt.Sprintf("%s%s", modifier, p.Type.String())
	}
	return modifier
}

func (p ParameterType) String() string {
	return p.GetName()
}