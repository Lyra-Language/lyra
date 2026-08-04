package types

import "strconv"

// The *head* of a type — its type constructor, with any arguments dropped: `Maybe<t>`
// and `Maybe<i64>` share the head `Maybe`, `[]t` has the head `[]`, `i64` is its own.
//
// It exists for **receiver-keyed overloading**: two functions of one name in one module
// are allowed only when their `self` parameters have different heads, and that rule is
// what keeps resolution decidable without a specificity ordering. `Maybe<t>` versus
// `Result<t,e>` is two heads and is allowed; `Maybe<i64>` versus `Maybe<string>` is one
// head twice and is not, so no call site ever has to rank two candidates that both
// match. Relaxing that later means adding an ordering, not reinterpreting these names.
//
// **A type variable has no head**, and that is the load-bearing case rather than an
// omission. `self: t` accepts every receiver, so it overlaps with every other candidate
// and cannot be one member of a set — HeadName reports false and the declaration is
// refused with an explanation. The same false is returned for the structural types
// (an anonymous struct, a function) where "the constructor" is not a name a user could
// have meant to dispatch on.
//
// Two properties callers depend on:
//
//   - **Allocation is not part of the head.** `shared Node` and `Node` are one type
//     nominally (TypesEqual ignores Allocation), so they must not be two overloads —
//     nothing at a call site could tell them apart.
//   - **It answers on an unresolved type.** Registration runs in the collector, before
//     the typechecker resolves a written name, so `self: Point` arrives as an
//     UnresolvedType. Within one module a name means one declaration, which is exactly
//     the scope an overload set spans, so the written name is a sound discriminant.

// HeadName returns the name of t's type constructor, and whether t has one.
//
// The returned string is an identity, not a rendering: it is compared against another
// head and embedded in an emitted symbol, so it must be stable and collision-free
// across the type constructors, but it is never shown to a user on its own.
func HeadName(t Type) (string, bool) {
	switch tt := t.(type) {
	case ParameterizedType:
		// `Maybe<i64>` heads as `Maybe`: the arguments are what a *generic* overload
		// binds, not what it dispatches on.
		return nonEmpty(tt.Name)
	case NamedStructType:
		return nonEmpty(tt.Name)
	case DataType:
		return nonEmpty(tt.Name)
	case UnresolvedType:
		// A name the collector has not resolved yet — see the note above.
		return nonEmpty(tt.Name)
	case *ConstrainedType:
		// A newtype is nominally distinct from its base (`newtype Percent = u8` is not
		// a u8 to the typechecker), so it heads as itself. Stripping to the base here
		// would let `Percent` and `u8` collide as one head and refuse a pair of
		// overloads the checker can plainly tell apart.
		if tt == nil {
			return "", false
		}
		return nonEmpty(tt.Name)
	case PrimitiveType:
		return nonEmpty(string(tt.Name))
	case DynamicArrayType:
		return "[]", true
	case StaticArrayType:
		// Every fixed-size array shares one head: the size is a *value*, and two
		// overloads separated by it would be dispatch on a number rather than on a
		// type constructor.
		return "[_]", true
	case TupleType:
		// Arity is genuinely part of the constructor — a 2-tuple and a 3-tuple are
		// different types and unify with nothing in common — so it belongs in the head.
		return "(" + strconv.Itoa(len(tt.Elements)) + ")", true
	case WeakType:
		return "weak", true
	case RawPointerType:
		return "^", true
	case RangeType:
		return "range", true
	}
	// GenericType (a variable — overlaps with everything), the structural types, and
	// void/never/Self. None can discriminate an overload; see the header.
	return "", false
}

func nonEmpty(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	return name, true
}
