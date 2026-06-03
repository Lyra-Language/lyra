package typechecker

import "github.com/Lyra-Language/lyra/pkg/types"

// isAssignable reports whether a value of type from can be assigned to a slot of type to.
//
// Untyped integer literals (UntypedInt) are assignable to any concrete integer type or
// any concrete float type. Untyped float literals (UntypedFloat) are assignable to any
// concrete float type. All concrete types require exact structural equality.
func isAssignable(from, to types.Type) bool {
	if types.TypesEqual(from, to) {
		return true
	}
	// Constrained types are nominally distinct from their base types at the
	// type-equality level, but a value is always assignable to a constrained
	// type when it satisfies the base type (the constraint check itself is
	// handled separately by checkPatternConstraints etc.).
	if ct, ok := to.(*types.ConstrainedType); ok {
		return isAssignable(from, ct.Type)
	}
	// A constrained type value is assignable to its base type (e.g.
	// `let s: string = someEmail` where Email = string @pattern(...)).
	if ct, ok := from.(*types.ConstrainedType); ok {
		return isAssignable(ct.Type, to)
	}
	// A static array literal is assignable to a dynamic array with a compatible
	// element type. This mirrors how `let xs: []int = [1, 2, 3]` should work:
	// the literal produces StaticArrayType{int,3} which widens to DynamicArrayType{int}.
	if fromSA, ok := from.(types.StaticArrayType); ok {
		// An empty array literal [] (Size==0, ElementType==nil) is assignable to
		// any array type — the element type is vacuously satisfied.
		if fromSA.ElementType == nil {
			switch to := to.(type) {
			case types.DynamicArrayType:
				return true
			case types.StaticArrayType:
				return fromSA.Size == to.Size
			}
		}
		if toDyn, ok := to.(types.DynamicArrayType); ok {
			return isAssignable(fromSA.ElementType, toDyn.ElementType)
		}
		// StaticArrayType → StaticArrayType: sizes must match, elements must be assignable.
		if toSA, ok := to.(types.StaticArrayType); ok {
			return fromSA.Size == toSA.Size && isAssignable(fromSA.ElementType, toSA.ElementType)
		}
	}

	fromP, fromIsPrim := from.(types.PrimitiveType)
	toP, toIsPrim := to.(types.PrimitiveType)
	if !fromIsPrim || !toIsPrim {
		return false
	}
	switch fromP.Name {
	case types.UntypedInt:
		return isAnyConcreteInt(toP.Name) || isAnyConcreteFloat(toP.Name)
	case types.UntypedSignedInt:
		return isAnyConcreteSignedInt(toP.Name) || isAnyConcreteFloat(toP.Name)
	case types.UntypedFloat:
		return isAnyConcreteFloat(toP.Name)
	}
	return false
}

// areEqualityCompatible reports whether two types can be compared with == or !=.
// The rule is symmetric assignability: a == b is valid whenever a can be
// assigned to b's type OR b can be assigned to a's type. This covers:
//   - Same concrete types:            bool == bool, i32 == i32
//   - Untyped widening to concrete:   i32 == 5, f64 == 1.0
//   - Rejects int/float mixing:       5 == 5.0  (UntypedInt ↛ UntypedFloat)
//   - Rejects cross-kind mismatches:  string == 5, bool == 5
func areEqualityCompatible(a, b types.Type) bool {
	return isAssignable(a, b) || isAssignable(b, a)
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
	// UntypedInt + UntypedSignedInt → UntypedSignedInt (signed takes precedence)
	if ap.Name == types.UntypedInt && bp.Name == types.UntypedSignedInt {
		return b
	}
	if bp.Name == types.UntypedInt && ap.Name == types.UntypedSignedInt {
		return a
	}
	if ap.Name == types.UntypedSignedInt && (isAnyConcreteSignedInt(bp.Name) || isFloatType(bp)) {
		return b
	}
	if bp.Name == types.UntypedSignedInt && (isAnyConcreteSignedInt(ap.Name) || isFloatType(ap)) {
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
	return isAnyConcreteInt(p.Name) || p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt
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
	return isAnyConcreteSignedInt(n) || isAnyConcreteUnsignedInt(n)
}

func isAnyConcreteSignedInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
		return true
	}
	return false
}

func isAnyConcreteUnsignedInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.UInt, types.UInt8, types.UInt16, types.UInt32, types.UInt64:
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
