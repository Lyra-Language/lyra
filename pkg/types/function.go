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

// FunctionType uses the pointer convention: it is always constructed as
// *types.FunctionType in the collector and stored as *types.FunctionType in
// AST nodes (ast.FunctionDefStmt.Signature, ast.TraitMethod.Signature).
// Receivers stay as value receivers so both FunctionType and *FunctionType
// satisfy types.Type; code that matches the interface concrete type (notably
// TypesEqual) must use the pointer form *FunctionType.
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
