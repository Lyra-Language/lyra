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
	case Int8, Int16, Int32, Int64, Int128, UInt8, UInt16, UInt32, UInt64, UInt128,
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

// IsCopiedScalar reports whether t is a scalar passed by value with no interior:
// a numeric primitive, `bool`, or `rune`. `string` is excluded — it is a
// PrimitiveType in the grammar but a managed fat pointer — as is everything
// non-primitive (aggregates, arrays, generic parameters).
//
// This is the "a borrow/ownership modifier does nothing here" predicate. Two
// places must agree on it or they drift into a silent bug, so both read it here:
// the `lyra-W010` inert-modifier warning (checker/inert_borrow_modifier.go) and
// the backend's by-reference `mut` parameter lowering (backend/llvm, paramIsByRef)
// — a `mut` that W010 calls inert must not be lowered as a reference, and one it
// stays silent about must be.
func IsCopiedScalar(t Type) bool {
	if _, ok := t.(PrimitiveType); !ok {
		return false
	}
	return !IsString(t)
}

func IsStaticArray(t Type) bool {
	_, ok := t.(StaticArrayType)
	return ok
}

func IsDynamicArray(t Type) bool {
	_, ok := t.(DynamicArrayType)
	return ok
}

func IsArray(t Type) bool {
	return IsStaticArray(t) || IsDynamicArray(t)
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
func (VoidType) GetName() string { return "void" }
func (VoidType) String() string  { return VoidType{}.GetName() }

// UnresolvedType represents a type reference that hasn't been resolved yet.
// Allocation carries a usage-site modifier (e.g. from `let n: shared Node`) that
// the typechecker applies on top of the declaration's default after resolution.
// Allocation is NOT part of nominal identity — TypesEqual ignores it.
type UnresolvedType struct {
	Name       string             // e.g., "Tree", "Point", "Maybe"
	Allocation AllocationModifier // usage-site override; Unspecified = inherit from decl
}

func (UnresolvedType) typeNode()         {}
func (u UnresolvedType) GetName() string { return u.Name }
func (u UnresolvedType) String() string  { return u.GetName() }

// AllocationModifier is the storage flavor of a value: where/how it lives.
// It is a property carried *alongside* a nominal type, not part of nominal
// identity (TypesEqual ignores it); see AllocationOf and FP/Imperative todo #5.
type AllocationModifier string

const (
	// Unspecified means no modifier was written at this site. It is the zero
	// value, so an un-set Allocation field reads as Unspecified — the canonical
	// "not specified, resolve from the declaration default later" sentinel.
	// (Replaces the old "none" value, which conflated unspecified with stack
	// and was applied inconsistently — only to array types, never to
	// struct/data/tuple, which were left as "".)
	Unspecified AllocationModifier = ""
	// Stack — stack/inline allocation: value semantics, no heap. The language
	// default flavor for a type with no declared allocation.
	Stack AllocationModifier = "stack"
	// Shared — heap, reference-counted (the `shared` modifier). Constructing a
	// Shared value heap-allocates (see checker.EffectAlloc).
	Shared AllocationModifier = "shared"
)

// AllocationOf returns the allocation flavor a type carries, or Unspecified for
// a type that does not (or cannot) carry one: primitives, generics, and lambdas.
// This is the single accessor every pass should use to read a type's flavor, so
// the per-concrete-type location of the field is centralized in one place.
func AllocationOf(t Type) AllocationModifier {
	switch t := t.(type) {
	case NamedStructType:
		return t.Allocation
	case DataType:
		return t.Allocation
	case TupleType:
		return t.Allocation
	case StaticArrayType:
		return t.Allocation
	case DynamicArrayType:
		return t.Allocation
	case ParameterizedType:
		return t.Allocation
	case UnresolvedType:
		return t.Allocation
	default:
		return Unspecified
	}
}

// WithAllocation returns a copy of t with its allocation modifier overridden to
// mod. When mod is Unspecified the original value is returned unchanged —
// Unspecified means "inherit from context / default to stack", so no override.
// Types that cannot carry a flavor (primitives, generics, lambdas) are returned
// unchanged regardless of mod.
func WithAllocation(t Type, mod AllocationModifier) Type {
	if mod == Unspecified {
		return t
	}
	switch t := t.(type) {
	case NamedStructType:
		t.Allocation = mod
		return t
	case DataType:
		t.Allocation = mod
		return t
	case TupleType:
		t.Allocation = mod
		return t
	case StaticArrayType:
		t.Allocation = mod
		return t
	case DynamicArrayType:
		t.Allocation = mod
		return t
	case ParameterizedType:
		t.Allocation = mod
		return t
	case UnresolvedType:
		t.Allocation = mod
		return t
	default:
		return t
	}
}

type TypeModifier string

const (
	Mut TypeModifier = "mut"
	Ref TypeModifier = "ref"
	Own TypeModifier = "own"
)

type ReturnType struct {
	Type         Type
	TypeModifier TypeModifier
}

func (r ReturnType) GetName() string {
	// A nil Type is an un-inferred return (e.g. an unannotated lambda literal
	// `(n: u8) => n`). Render it as "?" so a type String() — often used to build
	// error messages — never panics on the nil dereference.
	typeName := "?"
	if r.Type != nil {
		typeName = r.Type.GetName()
	}
	if r.TypeModifier != "" {
		return fmt.Sprintf("%s %s", r.TypeModifier, typeName)
	}
	return typeName
}
func (r ReturnType) String() string {
	return r.GetName()
}
