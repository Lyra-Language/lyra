package types

// The size and alignment a value takes by value — the one engine both the backend's
// layout (sum-type payloads, unions, captures) and the front end's questions about cost
// ask (lyra-W031, a large struct passed by value). It moved here from the backend
// (10/30) so the typechecker could ask it without importing LLVM.
//
// It assumes a typical 64-bit datalayout (pointer = 8 bytes, natural alignment = size).

// PointerSize is the bytes a pointer takes.
const PointerSize = 8

// A Resolver says what a type written somewhere means, for layout: the type, and the
// Resolver its own components are written under — a struct's fields are resolved from its
// declaration, not from the site that named it. A nil Resolver means every type met is
// already resolved.
type Resolver func(Type) (Type, Resolver)

// SizeAndAlign returns the size and alignment (bytes) of t, already resolved, and
// ok=false for a type not settled yet (a name, an un-monomorphized generic). A `shared`
// value is pointer-sized whatever its payload — it's a pointer — which is where
// DATA_LAYOUT and ALLOCATION meet (and why a `shared` recursive field gives a finite
// union).
func SizeAndAlign(t Type) (size, align int, ok bool) {
	return SizeAndAlignResolving(t, nil)
}

// SizeAndAlignResolving is SizeAndAlign over a type as written, asking resolve what each
// type it meets means.
func SizeAndAlignResolving(t Type, resolve Resolver) (size, align int, ok bool) {
	return layoutOf(t, resolve, 0)
}

// maxLayoutDepth bounds the walk: a type containing itself by value is lyra-E014's to
// refuse, and a program holding one may still be asked about.
const maxLayoutDepth = 64

func layoutOf(t Type, resolve Resolver, depth int) (size, align int, ok bool) {
	if depth > maxLayoutDepth || t == nil {
		return 0, 0, false
	}
	inner := resolve
	if resolve != nil {
		t, inner = resolve(t)
		if t == nil {
			return 0, 0, false
		}
	}
	if AllocationOf(t) == Shared {
		return PointerSize, PointerSize, true
	}
	if _, ok := t.(WeakType); ok {
		return PointerSize, PointerSize, true // a weak reference is a non-owning pointer
	}
	if IsSeq(t) {
		return PointerSize, PointerSize, true // a sequence value is a pointer to its box (seq_coro.go)
	}
	if _, ok := t.(DynamicArrayType); ok {
		return PointerSize, PointerSize, true // a `[]T` value is a box pointer (dynarray.go)
	}
	if _, ok := t.(*LambdaType); ok {
		// A function value is the closure fat pointer `{ i8* fn, i8* env }` (closures.go).
		// Missing until 09/13, so no closure could capture another: `let f = …` then
		// `() -> i64 => f(1)` failed to build with "cannot size captured binding f" — found
		// when a lambda first needed to capture a local generic's closure values.
		return 2 * PointerSize, PointerSize, true
	}
	switch v := t.(type) {
	case PrimitiveType:
		return primitiveLayout(v.Name)
	case VoidType:
		// A `void` payload — `Ok()` of a `Result<void, e>` — holds nothing, so it takes no
		// room; the variant is its tag alone, as a nullary constructor's is.
		return 0, 1, true
	case RawPointerType:
		// A raw pointer is a machine pointer and nothing else — no header, no length,
		// no ownership. Missing until 08/22, and the symptom was remote from the cause
		// in the usual way: not "a pointer has no size" but *any aggregate containing
		// one* failing to size, so `std.ffi`'s own `CBuffer` could not be captured by a
		// closure ("cannot size captured binding") or held in a `[]T` ("cannot size
		// dynamic array element type"). Found by a closure taking a `^u8` — which is
		// what every scoped-callback FFI shape is.
		return PointerSize, PointerSize, true
	case NamedStructType:
		return aggregateLayout(FieldTypes(v.Fields), inner, depth)
	case UnionType:
		return unionLayout(v, inner, depth)
	case AnonymousStructType:
		return aggregateLayout(FieldTypes(v.Fields), inner, depth)
	case TupleType:
		return aggregateLayout(v.Elements, inner, depth)
	case StaticArrayType:
		es, ea, ok := layoutOf(v.ElementType, inner, depth+1)
		if !ok {
			return 0, 0, false
		}
		return AlignUp(es, ea) * v.Size, ea, true // stride * count
	case DataType:
		return dataLayout(v, inner, depth)
	}
	return 0, 0, false
}

func primitiveLayout(name PrimitiveTypeName) (int, int, bool) {
	switch name {
	case Int8, UInt8, Boolean:
		return 1, 1, true
	case Int16, UInt16, Float16:
		return 2, 2, true
	case Int32, UInt32, Float32, Rune:
		return 4, 4, true
	case Int64, UInt64, Float64, UntypedInt, UntypedSignedInt, UntypedFloat:
		return 8, 8, true
	case Int128, UInt128:
		return 16, 16, true // i128 is 16/16 on the mainstream 64-bit ABI
	case String:
		// Fat pointer { i8*, i64, i64 }: three pointer-sized words (data, byte
		// length, rune count). See StringLLVMType.
		return PointerSize * 3, PointerSize, true
	}
	return 0, 0, false
}

// aggregateLayout lays out fields in order with C-style padding: each field starts at the
// next multiple of its alignment, and the total is rounded up to the aggregate's
// alignment (tail padding). An empty aggregate is {0, 1}.
func aggregateLayout(fields []Type, resolve Resolver, depth int) (int, int, bool) {
	size, align := 0, 1
	for _, f := range fields {
		fs, fa, ok := layoutOf(f, resolve, depth+1)
		if !ok {
			return 0, 0, false
		}
		size = AlignUp(size, fa) + fs
		if fa > align {
			align = fa
		}
	}
	return AlignUp(size, align), align, true
}

// unionLayout sizes a C union: **max over members, not a sum**, with every member at
// offset 0.
//
// This is the one function that has to know a union is not a struct, and it is where
// getting it wrong is worst: a too-small union links cleanly and lets the C side write
// past the end of a Lyra stack slot, which is a stack smash rather than a type error.
// `TestExec_UnionLayoutMatchesC` compares every number here against C's own `sizeof`
// and `_Alignof` through the vendored fixture, and the SDL3 test against a header
// nobody wrote for Lyra.
//
// The trailing `AlignUp` is what makes `SDL_Event` 128 rather than 125: C rounds a
// union's size up to its alignment so an array of them is correctly strided.
func unionLayout(ut UnionType, resolve Resolver, depth int) (int, int, bool) {
	size, align := 0, 1
	for _, m := range ut.Members {
		ms, ma, ok := layoutOf(m.Type, resolve, depth+1)
		if !ok {
			return 0, 0, false
		}
		if ms > size {
			size = ms
		}
		if ma > align {
			align = ma
		}
	}
	return AlignUp(size, align), align, true
}

// dataLayout sizes a `data` value as the struct { tag, payload-blob }.
func dataLayout(dt DataType, resolve Resolver, depth int) (int, int, bool) {
	tagBytes := TagBits(len(dt.Constructors)) / 8
	payloadSize, payloadAlign, ok := variantPayloadLayout(dt, resolve, depth)
	if !ok {
		return 0, 0, false
	}
	// Layout { tag, [payload] } with the payload aligned to payloadAlign.
	size := AlignUp(tagBytes, max(payloadAlign, 1)) + payloadSize
	align := max(tagBytes, payloadAlign)
	return AlignUp(size, align), align, true
}

// MaxVariantPayload returns the max size and max alignment over all of dt's variants'
// payload structs (a variant's payload is the struct of its flat field types).
func MaxVariantPayload(dt DataType) (size, align int, ok bool) {
	return variantPayloadLayout(dt, nil, 0)
}

func variantPayloadLayout(dt DataType, resolve Resolver, depth int) (size, align int, ok bool) {
	align = 1
	for _, c := range dt.Constructors {
		ps, pa, ok := aggregateLayout(c.FieldTypes(), resolve, depth)
		if !ok {
			return 0, 0, false
		}
		if ps > size {
			size = ps
		}
		if pa > align {
			align = pa
		}
	}
	return size, align, true
}

// TagBits returns the bit width of the tag integer for numVariants variants.
func TagBits(numVariants int) int {
	switch {
	case numVariants <= 1<<8:
		return 8
	case numVariants <= 1<<16:
		return 16
	case numVariants <= 1<<32:
		return 32
	default:
		return 64
	}
}

// FieldTypes is the type of each field, in order.
func FieldTypes(fields []StructField) []Type {
	out := make([]Type, len(fields))
	for i, f := range fields {
		out[i] = f.Type
	}
	return out
}

// AlignUp rounds n up to the next multiple of a (a is a power of two ≥ 1).
func AlignUp(n, a int) int {
	if a <= 1 {
		return n
	}
	return (n + a - 1) / a * a
}
