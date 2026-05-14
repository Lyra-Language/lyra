package typechecker

import "github.com/Lyra-Language/lyra/pkg/types"

// isAssignable reports whether a value of type from can be assigned to a slot of type to.
//
// Untyped integer literals (UntypedInt) are assignable to any concrete integer type.
// Untyped float literals (UntypedFloat) are assignable to any concrete float type.
// All concrete types require exact structural equality.
func isAssignable(from, to types.Type) bool {
	if types.TypesEqual(from, to) {
		return true
	}
	fromP, fromIsPrim := from.(types.PrimitiveType)
	toP, toIsPrim := to.(types.PrimitiveType)
	if !fromIsPrim || !toIsPrim {
		return false
	}
	switch fromP.Name {
	case types.UntypedInt:
		return isAnyConcreteInt(toP.Name) || isAnyConcreteFloat(toP.Name)
	case types.UntypedFloat:
		return isAnyConcreteFloat(toP.Name)
	}
	return false
}

// numericResultType returns the result type of a binary operation on two numeric types.
// UntypedInt/UntypedFloat widens to the concrete type when mixed with one.
// Returns nil if the operands are from different concrete numeric types
func numericResultType(a, b types.Type) types.Type {
	if types.TypesEqual(a, b) {
		return a
	}
	ap, aIsPrim := a.(types.PrimitiveType)
	bp, bIsPrim := b.(types.PrimitiveType)
	if !aIsPrim || !bIsPrim {
		return nil
	}
	if ap.Name == types.UntypedInt && (isAnyConcreteInt(bp.Name) || isFloatType(bp)) {
		return b
	}
	if bp.Name == types.UntypedInt && (isAnyConcreteInt(ap.Name) || isFloatType(ap)) {
		return a
	}
	if ap.Name == types.UntypedFloat && isFloatType(bp) {
		return b
	}
	if bp.Name == types.UntypedFloat && isFloatType(ap) {
		return a
	}
	return nil
}

// numericPrimitiveByName maps a Lyra type keyword to its PrimitiveType, for the
// numeric types that are valid targets of an explicit type conversion. Returns
// (t, true) if name is a known concrete numeric primitive, (nil, false) otherwise.
func numericPrimitiveByName(name string) (types.Type, bool) {
	switch types.PrimitiveTypeName(name) {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.UInt, types.UInt8, types.UInt16, types.UInt32, types.UInt64,
		types.Float16, types.Float32, types.Float64:
		return types.PrimitiveType{Name: types.PrimitiveTypeName(name)}, true
	}
	return nil, false
}

func isIntType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	return isAnyConcreteInt(p.Name) || p.Name == types.UntypedInt
}

func isFloatType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	return isAnyConcreteFloat(p.Name) || p.Name == types.UntypedFloat
}

// floatPrecision returns the relative precision rank of a concrete float type
// (higher = more precise). Returns 0 for non-float or untyped float types.
func floatPrecision(t types.Type) int {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return 0
	}
	switch p.Name {
	case types.Float16:
		return 1
	case types.Float32:
		return 2
	case types.Float64:
		return 3
	}
	return 0
}

func isAnyConcreteInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.UInt, types.UInt8, types.UInt16, types.UInt32, types.UInt64:
		return true
	}
	return false
}

func isAnyConcreteFloat(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Float16, types.Float32, types.Float64:
		return true
	}
	return false
}
