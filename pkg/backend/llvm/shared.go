package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// A `shared` value is a pointer to a ref-counted box `{ i64 rc, payload }`
// (ALLOCATION.md, SharedBoxType). Unlike a string — a fat pointer whose box is
// recovered by subtracting the header — a `shared` value *is* the box pointer, so
// retain/release act on it directly. This file boxes a constructed value and
// provides the type-dispatching retain/release the ownership lowering calls.

// isManagedLLVMType reports whether an LLVM-typed value is reference-counted: a
// string fat pointer, or a `shared` box pointer (the only pointer-typed values
// the backend produces — every stack value is inline). Both participate in the
// retain/release / last-use machinery.
func isManagedLLVMType(t lltypes.Type) bool {
	if isStringLLVMType(t) {
		return true
	}
	_, isPtr := t.(*lltypes.PointerType)
	return isPtr
}

// managedBox returns the ref-counted box pointer (as i8*) for a managed value —
// stringBox for a string, the pointer itself for a `shared` box.
func (l *lowerer) managedBox(block *ir.Block, v value.Value) value.Value {
	if isStringLLVMType(v.Type()) {
		return l.stringBox(block, v)
	}
	return block.NewBitCast(v, lltypes.NewPointer(lltypes.I8))
}

// lowerManagedRetain / lowerManagedRelease bump / drop the refcount of any managed
// value (string or `shared`), dispatching on its representation. release passes a
// null drop_fn: a value's nested managed fields (a `shared` field, a string in a
// struct) are not recursively released yet — recursive drop is the aggregate-drop
// follow-on, so those leak conservatively (never a double free).
func (l *lowerer) lowerManagedRetain(block *ir.Block, v value.Value) {
	l.ensureRCRuntime()
	block.NewCall(l.rcRetain, l.managedBox(block, v))
}

func (l *lowerer) lowerManagedRelease(block *ir.Block, v value.Value) {
	l.ensureRCRuntime()
	null := constant.NewNull(lltypes.NewPointer(lltypes.I8))
	block.NewCall(l.rcRelease, l.managedBox(block, v), null)
}

// unboxSharedData loads the inline aggregate value out of a `shared` box pointer
// so a `match` can destructure it. A `shared` aggregate scrutinee (a `data`,
// struct, or tuple value) lowers to a `ptr` to `{ i64 rc, payload }`
// (ALLOCATION.md), whereas the pattern-matching machinery (lowerDataMatch /
// aggPattern*) works on the first-class payload value — so this reads field 1 out
// of the box and returns it. A non-pointer (inline `stack`) scrutinee is returned
// unchanged, so callers can unbox unconditionally. The box's own last-use release
// is handled by the ownership pass as for any managed binding — unboxing only
// reads through it, it does not consume a reference.
func (l *lowerer) unboxSharedData(block *ir.Block, scrut value.Value) (value.Value, error) {
	ptr, ok := scrut.Type().(*lltypes.PointerType)
	if !ok {
		return scrut, nil // inline value — nothing to unbox
	}
	boxTy, ok := ptr.ElemType.(*lltypes.StructType)
	if !ok || len(boxTy.Fields) != 2 {
		return nil, fmt.Errorf("llvm: match scrutinee is a pointer to a non-box type (%s)", scrut.Type())
	}
	payloadPtr := block.NewGetElementPtr(boxTy, scrut,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
	return block.NewLoad(boxTy.Fields[1], payloadPtr), nil
}

// lowerBoxShared heap-allocates a ref-counted box for a `shared` value: it allocs
// `header + sizeof(payload)` bytes (lyra_rc_alloc sets rc = 1), stores the built
// payload into the box's payload field, and returns the typed box pointer — the
// `shared` value's representation. payloadType is the payload's Lyra type, used to
// size the allocation.
func (l *lowerer) lowerBoxShared(block *ir.Block, payload value.Value, payloadType types.Type) (value.Value, error) {
	l.ensureRCRuntime()
	payloadSize, _, ok := SizeAndAlign(payloadType)
	if !ok {
		return nil, fmt.Errorf("llvm: cannot size a `shared %s` payload yet", payloadType)
	}
	boxSize := constant.NewInt(lltypes.I64, int64(rcHeaderSize+payloadSize))
	boxI8 := block.NewCall(l.rcAlloc, boxSize) // i8*, rc already 1
	boxTy := SharedBoxType(payload.Type())     // { i64, payloadLLVM }
	box := block.NewBitCast(boxI8, lltypes.NewPointer(boxTy))
	payloadPtr := block.NewGetElementPtr(boxTy, box,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
	block.NewStore(payload, payloadPtr)
	return box, nil
}
