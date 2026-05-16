package types

import (
	"fmt"
)

type Type interface {
	typeNode()
	fmt.Stringer
	GetName() string
}

// IsNumeric reports whether t is a primitive numeric type.
func IsNumeric(t Type) bool {
	primitive, ok := t.(PrimitiveType)
	if !ok {
		return false
	}
	switch primitive.Name {
	case Int, Int8, Int16, Int32, Int64, UInt, UInt8, UInt16, UInt32, UInt64,
		Float16, Float32, Float64, UntypedInt, UntypedSignedInt, UntypedFloat:
		return true
	default:
		return false
	}
}

func IsString(t Type) bool {
	primitive, ok := t.(PrimitiveType)
	if !ok {
		return false
	}
	return primitive.Name == String
}

func IsBoolean(t Type) bool {
	primitive, ok := t.(PrimitiveType)
	if !ok {
		return false
	}
	return primitive.Name == Boolean
}

type SelfType struct {
	GenericParams []string
}

func (SelfType) typeNode()        {}
func (SelfType) GetName() string  { return "Self" }
func (s SelfType) String() string { return s.GetName() }

type VoidType struct{}

func (VoidType) typeNode()       {}
func (VoidType) GetName() string { return "Void" }
func (VoidType) String() string  { return VoidType{}.GetName() }

// UnresolvedType represents a type reference that hasn't been resolved yet
type UnresolvedType struct {
	Name string // e.g., "Tree", "Point", "Maybe"
}

func (UnresolvedType) typeNode()         {}
func (u UnresolvedType) GetName() string { return u.Name }
func (u UnresolvedType) String() string  { return u.GetName() }

type AllocationModifier string

const (
	None   AllocationModifier = "none"
	Stack  AllocationModifier = "stack"
	Shared AllocationModifier = "shared"
)

type TypeModifier string

const (
	Mut TypeModifier = "mut"
	Ref TypeModifier = "ref"
)

type ReturnType struct {
	Type         Type
	TypeModifier TypeModifier
}

func (r ReturnType) GetName() string {
	if r.TypeModifier != "" {
		return fmt.Sprintf("%s %s", r.TypeModifier, r.Type.GetName())
	}
	return r.Type.GetName()
}
func (r ReturnType) String() string {
	return r.GetName()
}
