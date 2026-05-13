package typechecker

import "github.com/Lyra-Language/lyra/pkg/types"

// isAssignable reports whether a value of type from can be assigned to a slot of type to.
//
// Untyped integer literals carry type PrimitiveType{Int} and are assignable to any
// concrete integer type. Untyped float literals carry PrimitiveType{Float} and are
// assignable to any concrete float type. Everything else requires exact structural equality.
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
	case types.Int:
		switch toP.Name {
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.UInt, types.UInt8, types.UInt16, types.UInt32, types.UInt64:
			return true
		}
	case types.Float:
		switch toP.Name {
		case types.Float, types.Float16, types.Float32, types.Float64:
			return true
		}
	}
	return false
}
