package types

import (
	"fmt"
	"strings"
)

type Type interface {
	typeNode()
	IsNumericType() bool
	GetName() string
}

// typeNode is a placeholder for the type node
func (PrimitiveType) typeNode() {}
func (ArrayType) typeNode()     {}
func (FunctionType) typeNode()  {}
func (GenericType) typeNode()   {}
func (StructType) typeNode()    {}
func (DataType) typeNode()      {}
func (MapType) typeNode()       {}
func (TupleType) typeNode()     {}

// IsNumericType checks if the type is a numeric type
func (p PrimitiveType) IsNumericType() bool {
	isInt := p.Name == "Int" || p.Name == "Int8" || p.Name == "Int16" || p.Name == "Int32" || p.Name == "Int64"
	isUInt := p.Name == "UInt" || p.Name == "UInt8" || p.Name == "UInt16" || p.Name == "UInt32" || p.Name == "UInt64"
	isFloat := p.Name == "Float" || p.Name == "Float16" || p.Name == "Float32" || p.Name == "Float64"
	return isInt || isUInt || isFloat
}
func (a ArrayType) IsNumericType() bool {
	return false
}
func (f FunctionType) IsNumericType() bool {
	return false
}
func (g GenericType) IsNumericType() bool {
	return false
}
func (s StructType) IsNumericType() bool {
	return false
}
func (d DataType) IsNumericType() bool {
	return false
}
func (m MapType) IsNumericType() bool {
	return false
}
func (t TupleType) IsNumericType() bool {
	return false
}

// GetName returns the name of the type
func (p PrimitiveType) GetName() string {
	return p.Name
}
func (a ArrayType) GetName() string {
	elementName := "?"
	if a.ElementType != nil {
		elementName = a.ElementType.GetName()
	}
	return fmt.Sprintf("Array<%s>", elementName)
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
func (g GenericType) GetName() string {
	return g.Name
}
func (s StructType) GetName() string {
	return s.Name
}
func (d DataType) GetName() string {
	return d.Name
}
func (m MapType) GetName() string {
	keyName := "?"
	if m.KeyType != nil {
		keyName = m.KeyType.GetName()
	}
	valueName := "?"
	if m.ValueType != nil {
		valueName = m.ValueType.GetName()
	}
	return fmt.Sprintf("Map<%s, %s>", keyName, valueName)
}
func (t TupleType) GetName() string {
	elementNames := make([]string, len(t.Elements))
	for i, element := range t.Elements {
		elementName := "?"
		if element != nil {
			elementName = element.GetName()
		}
		elementNames[i] = elementName
	}
	return fmt.Sprintf("(%s)", strings.Join(elementNames, ", "))
}

// Type definitions
type PrimitiveType struct {
	Name string // Int, Float, Bool, String
}

type ArrayType struct {
	ElementType Type
}

type FunctionType struct {
	ParameterTypes []ParameterType
	ReturnType     Type
}

type ParameterType struct {
	Modifier Modifier
	Type     Type
}

type Modifier string

const (
	Ref Modifier = "ref"
	Mut Modifier = "mut"
	Own Modifier = "own"
)

type GenericType struct {
	Name string // lowercase letter optionally followed by any number of letters or numbers
}

type StructType struct {
	Name   string // uppercase letter optionally followed by any number of letters or numbers
	Fields map[string]Type
}

type MapType struct {
	KeyType   Type
	ValueType Type
}

type TupleType struct {
	Elements []Type
}

type DataType struct {
	Name         string // uppercase letter optionally followed by any number of letters or numbers
	Constructors map[string]DataTypeConstructor
}

// Data constructor can have different shapes
type DataTypeConstructor struct {
	Name   string
	Params []Type          // for Simple(Int) style
	Fields map[string]Type // for Node { left: Tree, value: t } style
}

// UnresolvedType represents a type reference that hasn't been resolved yet
type UnresolvedType struct {
	Name string // e.g., "Tree", "Point", "Maybe"
}

func (UnresolvedType) typeNode()             {}
func (u UnresolvedType) IsNumericType() bool { return false }
func (u UnresolvedType) GetName() string     { return u.Name }
