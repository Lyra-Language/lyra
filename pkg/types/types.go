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
	case Int, Int8, Int16, Int32, Int64, UInt, UInt8, UInt16, UInt32, UInt64, Float, Float16, Float32, Float64:
		return true
	default:
		return false
	}
}

type SelfType struct {
	GenericParams []string
}

func (SelfType) typeNode()         {}
func (SelfType) GetName() string   { return "Self" }
func (s SelfType) String() string  { return s.GetName() }

// UnresolvedType represents a type reference that hasn't been resolved yet
type UnresolvedType struct {
	Name string // e.g., "Tree", "Point", "Maybe"
}

func (UnresolvedType) typeNode()             {}
func (u UnresolvedType) GetName() string     { return u.Name }
func (u UnresolvedType) String() string      { return u.GetName() }

type AllocationModifier string

const (
	Stack  AllocationModifier = "stack"
	Heap   AllocationModifier = "heap"
	Shared AllocationModifier = "shared"
)
