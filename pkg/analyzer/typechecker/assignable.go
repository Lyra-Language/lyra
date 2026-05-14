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
		return isAnyInt(toP.Name)
	case types.UntypedFloat:
		return isAnyFloat(toP.Name)
	}
	return false
}

// numericResultType returns the result type of a binary operation on two numeric types.
// UntypedInt/UntypedFloat widens to the concrete type when mixed with one.
// Returns nil if the operands are from different numeric families or are otherwise incompatible.
func numericResultType(a, b types.Type) types.Type {
	if types.TypesEqual(a, b) {
		return a
	}
	ap, aIsPrim := a.(types.PrimitiveType)
	bp, bIsPrim := b.(types.PrimitiveType)
	if !aIsPrim || !bIsPrim {
		return nil
	}
	if ap.Name == types.UntypedInt && isAnyInt(bp.Name) {
		return b
	}
	if bp.Name == types.UntypedInt && isAnyInt(ap.Name) {
		return a
	}
	if ap.Name == types.UntypedFloat && isAnyFloat(bp.Name) {
		return b
	}
	if bp.Name == types.UntypedFloat && isAnyFloat(ap.Name) {
		return a
	}
	return nil
}

func isAnyInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.UInt, types.UInt8, types.UInt16, types.UInt32, types.UInt64:
		return true
	}
	return false
}

func isAnyFloat(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Float16, types.Float32, types.Float64:
		return true
	}
	return false
}
