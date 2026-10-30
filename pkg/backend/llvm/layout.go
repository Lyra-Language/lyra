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

const pointerSize = types.PointerSize

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
// pointer" { i8* data, i64 byte_len, i64 rune_count } — a pointer to the UTF-8
// bytes (not NUL-terminated), the byte length, and the rune count. Literals point
// into a global constant; a heap string (concatenation, slice, read_line) points
// into a ref-counted box. See STRING_LAYOUT.md.
//
// The rune count rides the value as of 08/12, which is what makes `s.len()` a
// field read instead of an O(n) decode walk — the audit's last standing tension
// (the docs defended rune-count len by endorsing `for i in 0..<s.len() { s[i] }`,
// a loop whose len calls alone once measured 99.7% of `starts_with`'s cost).
// Construction almost always knows the count for free: a literal counts at compile
// time, `++` adds, `slice` subtracts its rune bounds, and only byte sources
// (read_line, interpolation's mixed segments) pay one linear count over bytes they
// just produced. The count is appended as field 2, so every field-0/1 consumer
// kept its index.
func StringLLVMType() *lltypes.StructType {
	return lltypes.NewStruct(lltypes.NewPointer(lltypes.I8), lltypes.I64, lltypes.I64)
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
		fields = append(fields, i64c(-1)) // PinnedRC bit pattern
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

// SizeAndAlign returns the size and alignment (bytes) of t, already resolved: the one
// engine, types.SizeAndAlign, which the front end asks too.
func SizeAndAlign(t types.Type) (size, align int, ok bool) {
	return types.SizeAndAlign(t)
}

func maxVariantPayload(dt types.DataType) (size, align int, ok bool) {
	return types.MaxVariantPayload(dt)
}

func tagBits(numVariants int) int { return types.TagBits(numVariants) }

func fieldTypes(fields []types.StructField) []types.Type { return types.FieldTypes(fields) }

func alignUp(n, a int) int { return types.AlignUp(n, a) }

// bufferStride is the bytes a `[]T` buffer gives each element: its size rounded to its
// alignment, and **never zero**. An element taking no space (`struct Marker {}`, 10/29) has
// every address at the buffer's start, and a buffer of `n × 0` bytes was `malloc(0)` —
// ASan crashed reporting each zero-sized store past it, and growing or shrinking one was
// `realloc(p, 0)`, which may free `p`. One byte each keeps every buffer non-empty and
// costs only arrays of nothing.
func bufferStride(size, align int) int {
	if s := alignUp(size, align); s > 0 {
		return s
	}
	return 1
}

func ceilDiv(n, d int) int { return (n + d - 1) / d }
