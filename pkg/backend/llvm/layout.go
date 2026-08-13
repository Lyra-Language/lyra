package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

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
	case types.Int128, types.UInt128:
		return lltypes.I128, true
	case types.Float16:
		return lltypes.Half, true
	case types.Float32:
		return lltypes.Float, true
	case types.Float64, types.UntypedFloat:
		return lltypes.Double, true
	case types.Boolean:
		return lltypes.I1, true
	case types.Rune:
		return lltypes.I32, true
	case types.String:
		return StringLLVMType(), true
	}
	// anything unhandled — representation deferred.
	return nil, false
}

// StringLLVMType is the LLVM representation of a Lyra `string`: an immutable "fat
// pointer" { i8* data, i64 len } — a pointer to the UTF-8 bytes plus a byte
// length (not NUL-terminated). Literals point into a global constant; a future
// heap string (concatenation) points into a ref-counted box. See STRING_LAYOUT.md.
func StringLLVMType() *lltypes.StructType {
	return lltypes.NewStruct(lltypes.NewPointer(lltypes.I8), lltypes.I64)
}

// IsNumericConversionTarget reports whether name is one of the eleven
// concrete numeric primitives that a Lyra type-conversion call (`i8(x)`,
// `f32(x)`, …) may target — Lyra's one conversion syntax (Pit-of-Success #5).
// Mirrors the typechecker's numericPrimitiveByName exactly (bool/char/string
// are not conversion targets this way, even though LLVMPrimitive maps them to
// an LLVM type for other purposes).
func IsNumericConversionTarget(name types.PrimitiveTypeName) bool {
	switch name {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int128,
		types.UInt8, types.UInt16, types.UInt32, types.UInt64, types.UInt128,
		types.Float16, types.Float32, types.Float64,
		// `rune` converts to and from the integer types (it lowers as an i32 code
		// point), so `rune(n)` dispatches here rather than being read as a call to
		// an undefined function. The typechecker has already restricted the pairing
		// to rune↔integer.
		types.Rune:
		return true
	default:
		return false
	}
}

// conversionTargetName is types.ConversionTargetName — the one shared answer to
// "is this callee a conversion?", kept as a local name so call sites read the same
// as before it was hoisted.
func conversionTargetName(callee string) (types.PrimitiveTypeName, bool) {
	return types.ConversionTargetName(callee)
}

// IsSignedInt reports whether name is a signed integer type. LLVM's integer
// types carry no signedness (i8 alone doesn't say signed-or-unsigned) —
// signedness lives in the *operation*, not the type — so LLVMPrimitive can't
// tell a caller whether to emit sdiv/srem (signed) or udiv/urem (unsigned), or
// sext vs zext on a widening conversion. Look up signedness here, from the
// original Lyra type, before choosing the instruction.
//
// UntypedInt/UntypedSignedInt are treated as signed, matching the i64
// LLVMPrimitive promotes them to: by the time codegen sees an untyped literal,
// something upstream failed to resolve it (promoteToDefault should already
// have run), so this defaults to the same "i64 signed" reading rather than
// silently picking unsigned.
//
// False for unsigned ints, floats, bool, char, and string/regex (i.e. anything
// that isn't a signed integer) — call only after establishing the operand is
// an integer (e.g. via LLVMPrimitive's ok result together with a concrete-int
// check).
func IsSignedInt(name types.PrimitiveTypeName) bool {
	switch name {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int128,
		types.UntypedInt, types.UntypedSignedInt,
		// A `rune` lowers as a signed i32 code point (Go's rune is int32), so a
		// widening conversion sign-extends and an ordering comparison uses the
		// signed predicate. Code points are non-negative in practice, making the
		// choice unobservable in range — but it must match the i32 representation.
		types.Rune:
		return true
	default:
		return false
	}
}

// floatIntrinsicSuffix reports the LLVM intrinsic name suffix (e.g.
// `llvm.floor.f64`) for a concrete Lyra float type. Lyra's PrimitiveTypeName
// constants for floats are already spelled "f16"/"f32"/"f64" (pkg/types), so
// this is a direct name check rather than a width lookup through the
// already-lowered LLVM type.
func floatIntrinsicSuffix(name types.PrimitiveTypeName) (string, bool) {
	switch name {
	case types.Float16, types.Float32, types.Float64:
		return string(name), true
	}
	return "", false
}

// Box field indices. Every ref-counted box begins with the same header
// (runtime.go): the strong count, then the weak count, then its payload. These are
// named because a GEP index is a bare integer — nothing type-checks that field 2
// is the payload, so a layout change with the indices spelled inline would compile
// silently and read the wrong memory. Every box access goes through the helpers
// below, so the layout is stated once.
const (
	boxStrongField  = 0 // i64: owning references; the value dies at 0
	boxWeakField    = 1 // i64: non-owning references; the memory is freed at 0
	boxPayloadField = 2 // the value itself
)

// Dynamic-array box field indices, past the shared header: a `[]T` box is
// `{ strong, weak, len, cap, T* }`, so its payload (at boxPayloadField) is the
// `{ len, cap, T* }` triple the drop glue receives.
//
// **The elements live in a separate buffer, and that indirection is what makes `[]T`
// growable.** They used to sit inline in a `[0 x T]` tail, which is one allocation and
// one less load per access — and cannot grow: a `[]T` *value* is the box pointer, so
// moving the elements moves the box, and every other binding holding that pointer is
// left dangling. Aliasing is observable (`let b = a; a[0] = 9` is visible through `b`),
// so that is a use-after-free rather than a semantic choice. With the elements behind a
// pointer the box address never changes, so every alias sees a `push` — which is the
// reference semantics `[]T` already had for element assignment.
const (
	dynArrayLenField   = boxPayloadField     // i64: the element count
	dynArrayCapField   = boxPayloadField + 1 // i64: elements the buffer can hold
	dynArrayElemsField = boxPayloadField + 2 // T*: the element buffer (malloc'd, may be null at cap 0)
)

// SharedBoxType returns the llir type of a ref-counted box wrapping payload:
// `{ i64 strong, i64 weak, <payload> }`. A `shared` value is a pointer to this box
// (ALLOCATION.md).
func SharedBoxType(payload lltypes.Type) *lltypes.StructType {
	return lltypes.NewStruct(lltypes.I64, lltypes.I64, payload)
}

// pinnedBoxConstant builds a **pinned static box** around a constant payload: a
// global with the layout a heap box has, but every count set to the pinned
// sentinel so retain and release are no-ops on it (runtime.go, PinnedRC). That is
// what lets a compile-time value — a string literal's bytes, the shared empty
// closure environment — flow through the ownership model as an ordinary managed
// value at no allocation cost, and it is why the counts are set here rather than
// at each site: a site that spelled the header inline would silently be missing a
// count word after a layout change.
func pinnedBoxConstant(payload constant.Constant) (*constant.Struct, *lltypes.StructType) {
	boxTy := SharedBoxType(payload.Type())
	fields := make([]constant.Constant, 0, len(boxTy.Fields))
	for range len(boxTy.Fields) - 1 { // every count word
		fields = append(fields, constant.NewInt(lltypes.I64, -1)) // PinnedRC bit pattern
	}
	fields = append(fields, payload)
	return constant.NewStruct(boxTy, fields...), boxTy
}

// boxPayloadPtr GEPs to a box's payload — the one place that knows where a
// payload sits relative to the header.
func boxPayloadPtr(block *ir.Block, boxTy *lltypes.StructType, box value.Value) value.Value {
	return block.NewGetElementPtr(boxTy, box, i32c(0), i32c(boxPayloadField))
}

// dynArrayBoxSize is a `[]T` box in bytes, and unlike the old inline layout it does
// not depend on the element count: the header, the length, the capacity and the buffer
// pointer. The elements are elsewhere.
const dynArrayBoxSize = rcHeaderSize + 8 + 8 + 8

// DynArrayBoxType returns the box layout of a dynamic array `[]T`:
// `{ i64 strong, i64 weak, i64 len, i64 cap, T* elems }` — the box header, the element
// count, the buffer's capacity, and a pointer to the elements. A `[]T` value is a `ptr`
// to this box, so it reuses the shared-value refcount machinery unchanged: the header is
// at the same offset for every box kind. See dynarray.go.
func DynArrayBoxType(elem lltypes.Type) *lltypes.StructType {
	return lltypes.NewStruct(lltypes.I64, lltypes.I64, lltypes.I64, lltypes.I64, lltypes.NewPointer(elem))
}

// dynArrayLenPtr / dynArrayCapPtr / dynArrayElemsPtr GEP to a dynamic array box's
// scalar fields; dynArrayElemPtr goes one step further to an element.
func dynArrayLenPtr(block *ir.Block, boxTy *lltypes.StructType, box value.Value) value.Value {
	return block.NewGetElementPtr(boxTy, box, i32c(0), i32c(dynArrayLenField))
}

func dynArrayCapPtr(block *ir.Block, boxTy *lltypes.StructType, box value.Value) value.Value {
	return block.NewGetElementPtr(boxTy, box, i32c(0), i32c(dynArrayCapField))
}

func dynArrayElemsPtr(block *ir.Block, boxTy *lltypes.StructType, box value.Value) value.Value {
	return block.NewGetElementPtr(boxTy, box, i32c(0), i32c(dynArrayElemsField))
}

// dynArrayElemPtr GEPs to element `idx`. It costs a **load** the inline layout did not:
// the buffer pointer has to be read out of the box before it can be indexed. That is the
// standing price of growability, and it is the same one every growable reference
// container pays. LLVM hoists the load out of a read-only loop; a loop containing a
// `push` is the case it cannot, since the push may store a new buffer pointer.
func dynArrayElemPtr(block *ir.Block, boxTy *lltypes.StructType, box, idx value.Value) value.Value {
	elemTy := boxTy.Fields[dynArrayElemsField].(*lltypes.PointerType).ElemType
	elems := block.NewLoad(lltypes.NewPointer(elemTy), dynArrayElemsPtr(block, boxTy, box))
	return block.NewGetElementPtr(elemTy, elems, idx)
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
	if _, ok := t.(types.WeakType); ok {
		return pointerSize, pointerSize, true // a weak reference is a non-owning pointer
	}
	if _, ok := t.(types.DynamicArrayType); ok {
		return pointerSize, pointerSize, true // a `[]T` value is a box pointer (dynarray.go)
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
	case types.Int32, types.UInt32, types.Float32, types.Rune:
		return 4, 4, true
	case types.Int64, types.UInt64, types.Float64,
		types.UntypedInt, types.UntypedSignedInt, types.UntypedFloat:
		return 8, 8, true
	case types.Int128, types.UInt128:
		return 16, 16, true // i128 is 16/16 on the mainstream 64-bit ABI
	case types.String:
		// Fat pointer { i8*, i64 }: two pointer-sized words. See StringLLVMType.
		return pointerSize * 2, pointerSize, true
	}
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
// payload structs (a variant's payload is the struct of its flat field types).
func maxVariantPayload(dt types.DataType) (size, align int, ok bool) {
	align = 1
	for _, c := range dt.Constructors {
		ps, pa, ok := aggregateSizeAndAlign(c.FieldTypes())
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
