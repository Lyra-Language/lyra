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
	// A data-type value is assignable to the same nominal type whether the slot
	// is written as a bare name, with generic arguments (`Maybe<i64>`), or as
	// another reference to the data type. The checker does not instantiate
	// generics, so nominal types unify by head name. This is required because a
	// constructor application like `Some(42)` infers to the bare `Maybe`
	// DataType while annotations are usually written `Maybe<T>`.
	if nominalDataMatch(from, to) {
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

// firstAllocationMismatch decides whether owning a value of type from into a
// slot of type to crosses a storage-flavor boundary that must be spelled
// explicitly. It walks from and to in parallel — the top-level flavor first,
// then structurally into array element types and tuple element types — and
// returns the first concrete, differing flavor pair it finds (found=true), or
// found=false when the two are flavor-compatible throughout.
//
// Allocation (stack vs shared) is NOT part of nominal identity — isAssignable
// and TypesEqual ignore it, so the structural check is unchanged. This is a
// separate axis, applied only at *owning* sites (a binding's initializer, a
// reassignment, an interior lvalue write, an `own` argument, an owned return):
// storing a `shared` value where a `stack` value is expected (or vice versa)
// changes representation and so is an explicit operation, never a silent
// coercion.
//
// The rule is deliberately conservative: it flags only a concrete, differing
// pair. An Unspecified flavor on either side means "inherit from context" (an
// unannotated binding, a plain type reference), which is polymorphic and stays
// compatible with either flavor — this keeps all unannotated code, and the
// common "construct a literal, bind it `shared`" pattern, error-free. The
// structural recursion catches an element-level boundary such as a `stack`
// element assigned into a `[N]shared` slot, which the top-level flavors (both
// the array's own, here Unspecified) would miss; returning the offending pair
// lets the diagnostic name the actual flavors that clashed, even several levels
// down.
func firstAllocationMismatch(from, to types.Type) (types.AllocationModifier, types.AllocationModifier, bool) {
	fromA := types.AllocationOf(from)
	toA := types.AllocationOf(to)
	if fromA != types.Unspecified && toA != types.Unspecified && fromA != toA {
		return fromA, toA, true
	}
	switch f := from.(type) {
	case types.StaticArrayType:
		switch t := to.(type) {
		case types.StaticArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		case types.DynamicArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		}
	case types.DynamicArrayType:
		switch t := to.(type) {
		case types.DynamicArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		case types.StaticArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		}
	case types.TupleType:
		if t, ok := to.(types.TupleType); ok && len(f.Elements) == len(t.Elements) {
			for i := range f.Elements {
				if fa, ta, ok := firstAllocationMismatch(f.Elements[i], t.Elements[i]); ok {
					return fa, ta, true
				}
			}
		}
	}
	return "", "", false
}

// elementAllocationMismatch recurses into a pair of container element types,
// guarding the nil element that an empty array literal ([]) carries.
func elementAllocationMismatch(from, to types.Type) (types.AllocationModifier, types.AllocationModifier, bool) {
	if from == nil || to == nil {
		return "", "", false
	}
	return firstAllocationMismatch(from, to)
}

// paramOwnsArgument reports whether passing an argument to a parameter with this
// mode transfers ownership, so the argument's allocation flavor must match the
// parameter's declared flavor (allocationCompatible). Borrowed parameters
// (bare / `ref` / `mut`) are allocation-polymorphic — the callee references the
// caller's value in place, whatever its flavor — so only `own`, which adopts the
// value into the callee's own storage, is subject to the flavor check. This is
// FP/Imperative todo #5 Decision (b): "owned params carry a flavor; borrowed
// params are allocation-polymorphic."
func paramOwnsArgument(mod types.TypeModifier) bool {
	return mod == types.Own
}

// isOwnedReturn reports whether a function's return value is owned out to the
// caller — so its allocation flavor must match the declared return type — rather
// than borrowed. A `ref`/`mut` return is a borrow (allocation-polymorphic); a
// bare or `own` return transfers ownership. Mirror of paramOwnsArgument for the
// return position (Decision (b) applies symmetrically to owned returns).
func isOwnedReturn(mod types.TypeModifier) bool {
	return mod != types.Ref && mod != types.Mut
}

// nominalDataMatch reports whether from and to denote the same nominal data
// type by head name, ignoring generic arguments. At least one side must be a
// concrete DataType; the other may be a DataType, a ParameterizedType
// (`Maybe<i64>`), or an UnresolvedType naming the same data type. Used so a
// constructor application (which infers to the bare DataType) is accepted
// against a written instantiation of that type.
func nominalDataMatch(from, to types.Type) bool {
	if name, ok := dataTypeName(from); ok {
		if other, ok := nominalName(to); ok {
			return name == other
		}
	}
	if name, ok := dataTypeName(to); ok {
		if other, ok := nominalName(from); ok {
			return name == other
		}
	}
	return false
}

// dataTypeName returns the name of t when it is a concrete DataType.
func dataTypeName(t types.Type) (string, bool) {
	if dt, ok := t.(types.DataType); ok {
		return dt.Name, true
	}
	return "", false
}

// nominalName returns the head name of any nominal type reference: a DataType,
// a ParameterizedType, or an UnresolvedType.
func nominalName(t types.Type) (string, bool) {
	switch v := t.(type) {
	case types.DataType:
		return v.Name, true
	case types.ParameterizedType:
		return v.Name, true
	case types.UnresolvedType:
		return v.Name, true
	}
	return "", false
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
	case types.Int8, types.Int16, types.Int32, types.Int64,
		types.UInt8, types.UInt16, types.UInt32, types.UInt64,
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
	case types.Int8, types.Int16, types.Int32, types.Int64:
		return true
	}
	return false
}

func isAnyConcreteUnsignedInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.UInt8, types.UInt16, types.UInt32, types.UInt64:
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
