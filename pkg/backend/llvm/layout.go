package llvm

import (
	lltypes "github.com/llir/llvm/ir/types"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// Type/layout helpers that make ALLOCATION.md and DATA_LAYOUT.md concrete: the
// `stack`-value LLVM type of a primitive, the `shared` box type, the sum-type
// tagged union (all as llir `types.Type`, aliased `lltypes`), and the
// size/alignment engine the union sizing needs.
//
// These assume a typical 64-bit target datalayout (pointer = 8 bytes, natural
// alignment = size). The real backend should eventually query LLVM's own
// datalayout rather than hardcoding this; for now it lets us lay out the
// sum-type payload blob.

const pointerSize = 8

// LLVMPrimitive returns the llir type for a Lyra primitive by value (the `stack`
// representation), and ok=false for one whose representation isn't settled yet
// (string) or that shouldn't survive typechecking (untyped literals are mapped
// to their defaults defensively). char is a Unicode scalar → i32.
func LLVMPrimitive(name types.PrimitiveTypeName) (lltypes.Type, bool) {
	switch name {
	case types.Int8, types.UInt8:
		return lltypes.I8, true
	case types.Int16, types.UInt16:
		return lltypes.I16, true
	case types.Int32, types.UInt32:
		return lltypes.I32, true
	case types.Int64, types.UInt64, types.UntypedInt, types.UntypedSignedInt:
		return lltypes.I64, true
	case types.Float16:
		return lltypes.Half, true
	case types.Float32:
		return lltypes.Float, true
	case types.Float64, types.UntypedFloat:
		return lltypes.Double, true
	case types.Boolean:
		return lltypes.I1, true
	case types.Char:
		return lltypes.I32, true
	}
	// string (and anything unhandled) — representation deferred.
	return nil, false
}

// SharedBoxType returns the llir type of a ref-counted box wrapping payload:
// `{ i64, <payload> }` with the refcount first. A `shared` value is a pointer to
// this box (ALLOCATION.md).
func SharedBoxType(payload lltypes.Type) *lltypes.StructType {
	return lltypes.NewStruct(lltypes.I64, payload)
}

// TagType returns the smallest unsigned integer type that holds numVariants
// distinct tags.
func TagType(numVariants int) *lltypes.IntType {
	return lltypes.NewInt(uint64(tagBits(numVariants)))
}

// DataUnionType returns the llir type of a `data` value's tagged union,
// `{ iTAG, [K x iA] }` (DATA_LAYOUT.md): the tag followed by a payload blob sized
// to the largest variant. The blob's element type iA equals the payload's
// alignment, so llir/LLVM's own struct layout pads the tag and aligns the payload
// — no manual padding needed. An all-nullary `data` (an enum) has no blob and
// lowers to just `{ iTAG }`. ok=false when any variant payload can't be sized yet
// (e.g. a string field, or an un-monomorphized generic).
//
// Alignment of the whole value is returned by SizeAndAlign(dt); apply it at the
// alloca/global.
func DataUnionType(dt types.DataType) (*lltypes.StructType, bool) {
	tag := TagType(len(dt.Constructors))
	payloadSize, payloadAlign, ok := maxVariantPayload(dt)
	if !ok {
		return nil, false
	}
	if payloadSize == 0 {
		return lltypes.NewStruct(tag), true
	}
	elem := lltypes.NewInt(uint64(payloadAlign * 8))
	count := ceilDiv(payloadSize, payloadAlign)
	return lltypes.NewStruct(tag, lltypes.NewArray(uint64(count), elem)), true
}

// SizeAndAlign returns the size and alignment (bytes) of t under the target
// datalayout, ok=false for a type not settled yet (string, dynamic array) or
// that needs monomorphization first (a bare generic parameter). A `shared`-
// flavored value is pointer-sized regardless of its payload — it's a pointer —
// which is where DATA_LAYOUT and ALLOCATION meet (and why a `shared` recursive
// field gives a finite union).
func SizeAndAlign(t types.Type) (size, align int, ok bool) {
	if types.AllocationOf(t) == types.Shared {
		return pointerSize, pointerSize, true
	}
	switch v := t.(type) {
	case types.PrimitiveType:
		return primitiveSizeAndAlign(v.Name)
	case types.NamedStructType:
		return aggregateSizeAndAlign(fieldTypes(v.Fields))
	case types.AnonymousStructType:
		return aggregateSizeAndAlign(fieldTypes(v.Fields))
	case types.TupleType:
		return aggregateSizeAndAlign(v.Elements)
	case types.StaticArrayType:
		es, ea, ok := SizeAndAlign(v.ElementType)
		if !ok {
			return 0, 0, false
		}
		return alignUp(es, ea) * v.Size, ea, true // stride * count
	case types.DataType:
		return dataSizeAndAlign(v)
	}
	return 0, 0, false
}

func primitiveSizeAndAlign(name types.PrimitiveTypeName) (int, int, bool) {
	switch name {
	case types.Int8, types.UInt8, types.Boolean:
		return 1, 1, true
	case types.Int16, types.UInt16, types.Float16:
		return 2, 2, true
	case types.Int32, types.UInt32, types.Float32, types.Char:
		return 4, 4, true
	case types.Int64, types.UInt64, types.Float64,
		types.UntypedInt, types.UntypedSignedInt, types.UntypedFloat:
		return 8, 8, true
	}
	// string — representation deferred.
	return 0, 0, false
}

// aggregateSizeAndAlign lays out fields in order with C-style padding: each field
// starts at the next multiple of its alignment, and the total is rounded up to the
// aggregate's alignment (tail padding). An empty aggregate is {0, 1}.
func aggregateSizeAndAlign(fields []types.Type) (int, int, bool) {
	size, align := 0, 1
	for _, f := range fields {
		fs, fa, ok := SizeAndAlign(f)
		if !ok {
			return 0, 0, false
		}
		size = alignUp(size, fa) + fs
		if fa > align {
			align = fa
		}
	}
	return alignUp(size, align), align, true
}

// dataSizeAndAlign sizes a `data` value as the struct { tag, payload-blob }.
func dataSizeAndAlign(dt types.DataType) (int, int, bool) {
	tagBytes := tagBits(len(dt.Constructors)) / 8
	payloadSize, payloadAlign, ok := maxVariantPayload(dt)
	if !ok {
		return 0, 0, false
	}
	// Layout { tag, [payload] } with the payload aligned to payloadAlign.
	size := alignUp(tagBytes, maxInt(payloadAlign, 1)) + payloadSize
	align := maxInt(tagBytes, payloadAlign)
	return alignUp(size, align), align, true
}

// maxVariantPayload returns the max size and max alignment over all variants'
// payload structs (a variant's payload is the struct of its Params).
func maxVariantPayload(dt types.DataType) (size, align int, ok bool) {
	align = 1
	for _, c := range dt.Constructors {
		ps, pa, ok := aggregateSizeAndAlign(c.Params)
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

// tagBits returns the bit width of the tag integer for numVariants variants.
func tagBits(numVariants int) int {
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

func fieldTypes(fields []types.StructField) []types.Type {
	out := make([]types.Type, len(fields))
	for i, f := range fields {
		out[i] = f.Type
	}
	return out
}

// alignUp rounds n up to the next multiple of a (a is a power of two ≥ 1).
func alignUp(n, a int) int {
	if a <= 1 {
		return n
	}
	return (n + a - 1) / a * a
}

func ceilDiv(n, d int) int { return (n + d - 1) / d }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
