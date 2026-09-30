package types

import "fmt"

type StaticArrayType struct {
	ElementType Type
	Size        int
	Allocation  AllocationModifier
	// SizeVar is a `const` generic parameter the size is written as — the `N` of
	// `[N]t` in `let first<t, const N: i64> = (xs: ref [N]t) -> t` — and Size means
	// nothing while it is set. A binding of N (an ArraySize) replaces it
	// (Substitute), so a specialization's arrays all have sizes.
	SizeVar string
}

func (StaticArrayType) typeNode() {}

func (a StaticArrayType) GetName() string {
	elementTypeName := "?"
	if a.ElementType != nil {
		elementTypeName = a.ElementType.String()
	}
	if a.SizeVar != "" {
		return fmt.Sprintf("StaticArray<%s, %s>", elementTypeName, a.SizeVar)
	}
	return fmt.Sprintf("StaticArray<%s, %d>", elementTypeName, a.Size)
}

// SameArraySize reports whether two fixed arrays have the same size: the same number,
// or the same `const` parameter.
func SameArraySize(a, b StaticArrayType) bool {
	return a.Size == b.Size && a.SizeVar == b.SizeVar
}

// ArraySize is what a `const` generic parameter is bound to: an array size, solved from
// an argument's type (`[64]u32` binds N to 64). It sits in the same bindings map as a type
// variable's binding, so substitution, instantiation keys and specialization carry it
// with no second map; it is never the type of a value.
type ArraySize struct {
	Size int
	// Var is set when the size is another function's `const` parameter — a generic
	// calling a generic with its own `[M]t` binds N to M, which the caller's
	// specialization then replaces with a number (Substitute).
	Var string
}

func (ArraySize) typeNode() {}
func (s ArraySize) GetName() string {
	if s.Var != "" {
		return s.Var
	}
	return fmt.Sprintf("%d", s.Size)
}
func (s ArraySize) String() string { return s.GetName() }

func (a StaticArrayType) String() string {
	return a.GetName()
}

type DynamicArrayType struct {
	ElementType Type
	Allocation  AllocationModifier
}

func (DynamicArrayType) typeNode() {}

func (a DynamicArrayType) GetName() string {
	elementName := "?"
	if a.ElementType != nil {
		elementName = a.ElementType.String()
	}
	return fmt.Sprintf("DynamicArray<%s>", elementName)
}

func (a DynamicArrayType) String() string {
	return a.GetName()
}
