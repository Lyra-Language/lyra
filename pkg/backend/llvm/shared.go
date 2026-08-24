package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// A `shared` value is a pointer to a ref-counted box `{ i64 rc, payload }`
// (ALLOCATION.md, SharedBoxType). Unlike a string — a fat pointer whose box is
// recovered by subtracting the header — a `shared` value *is* the box pointer, so
// retain/release act on it directly. This file boxes a constructed value and
// provides the type-dispatching retain/release the ownership lowering calls.

// managedBox returns the ref-counted box pointer (as i8*) for a managed value —
// stringBox for a string, the pointer itself for a `shared` box.
func (l *lowerer) managedBox(block *ir.Block, v value.Value) value.Value {
	if isStringLLVMType(v.Type()) {
		return l.stringBox(block, v)
	}
	// A closure's environment pointer sits rcHeaderSize past its box, exactly as a
	// string's data pointer does — that deliberate symmetry is what lets retain and
	// release treat a function value like any other managed value.
	if isClosureLLVMType(v.Type()) {
		return l.closureEnvBox(block, v)
	}
	return block.NewBitCast(v, lltypes.NewPointer(lltypes.I8))
}

// lowerManagedRetain / lowerManagedRelease bump / drop the refcount of any managed
// value (string or `shared`), dispatching on its representation.
//
// release passes the value's **drop glue** (drop.go) as lyra_rc_release's drop_fn,
// so when the box's count reaches zero whatever its payload owns — a string field,
// a `shared` tail — is released too, one box at a time down the structure. lyraType
// is the value's Lyra type, needed to know what the payload owns; a nil/unknown type
// yields a null drop_fn, which only leaks (memory-safe).
func (l *lowerer) lowerManagedRetain(block *ir.Block, v value.Value, lyraType types.Type) {
	l.ensureRCRuntime()
	if isWeakType(l.stripNewtype(lyraType)) {
		// A copy of a weak reference takes another *weak* count: it must keep the
		// box's memory alive without resurrecting the value.
		block.NewCall(l.rcWeakRetain, l.managedBox(block, v))
		return
	}
	block.NewCall(l.rcRetain, l.managedBox(block, v))
}

func (l *lowerer) lowerManagedRelease(block *ir.Block, v value.Value, lyraType types.Type) error {
	l.ensureRCRuntime()
	if isWeakType(l.stripNewtype(lyraType)) {
		// No drop_fn: a weak reference never owned the payload, so there is nothing
		// of the value's to drop — only the storage to give up. The payload's glue
		// already ran when the strong count hit zero.
		block.NewCall(l.rcWeakRelease, l.managedBox(block, v))
		return nil
	}
	drop, err := l.boxDropFn(lyraType)
	if err != nil {
		return err
	}
	block.NewCall(l.rcRelease, l.managedBox(block, v), drop)
	return nil
}

// isWeakType reports whether a Lyra type is a `weak` reference, which takes the
// weak half of the refcount protocol rather than the strong half.
func isWeakType(t types.Type) bool {
	_, ok := t.(types.WeakType)
	return ok
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
	if !ok || len(boxTy.Fields) != boxPayloadField+1 {
		return nil, fmt.Errorf("llvm: match scrutinee is a pointer to a non-box type (%s)", scrut.Type())
	}
	payloadPtr := boxPayloadPtr(block, boxTy, scrut)
	return block.NewLoad(boxTy.Fields[boxPayloadField], payloadPtr), nil
}

// lowerBoxShared heap-allocates a ref-counted box for a `shared` value: it allocs
// `header + sizeof(payload)` bytes (lyra_rc_alloc sets rc = 1), stores the built
// payload into the box's payload field, and returns the typed box pointer — the
// `shared` value's representation. payloadType is the payload's Lyra type, used to
// size the allocation — it is run through resolveForLayout first, since a field
// naming another declared type (`struct Outer { inner: Inner }`) is recorded as a
// bare UnresolvedType that SizeAndAlign can't size on its own (the same treatment
// lowerDataDef gives a variant payload).
func (l *lowerer) lowerBoxShared(block *ir.Block, payload value.Value, payloadType types.Type) (value.Value, error) {
	l.ensureRCRuntime()
	payloadSize, _, ok := SizeAndAlign(l.resolveForLayout(payloadType))
	if !ok {
		return nil, fmt.Errorf("llvm: cannot size a `shared %s` payload yet", payloadType)
	}
	boxSize := constant.NewInt(lltypes.I64, int64(rcHeaderSize+payloadSize))
	boxI8 := block.NewCall(l.rcAlloc, boxSize) // i8*, rc already 1
	boxTy := SharedBoxType(payload.Type())     // { i64, payloadLLVM }
	box := block.NewBitCast(boxI8, lltypes.NewPointer(boxTy))
	payloadPtr := boxPayloadPtr(block, boxTy, box)
	block.NewStore(payload, payloadPtr)
	return box, nil
}

// lowerBoxSharedReuse boxes a `shared` value with a Perceus **reuse token**: when
// the token (an i8* from lyra_rc_drop_reuse) is non-null the value's box was
// reclaimed from a matched-and-dead unique value, so we write the new payload into
// it in place (no allocation); when null we fall back to a fresh lyra_rc_alloc box.
// The choice is a runtime branch (uniqueness isn't statically known — the true
// Perceus mechanism), producing a phi of the two box pointers. payloadType sizes
// the fresh allocation; the reused box is the same `data` type (the analysis only
// pairs same-type constructions), so its layout matches.
func (l *lowerer) lowerBoxSharedReuse(block *ir.Block, payload value.Value, payloadType types.Type, token value.Value) (value.Value, *ir.Block, error) {
	l.ensureRCRuntime()
	boxTy := SharedBoxType(payload.Type()) // { i64, payloadLLVM }
	boxPtrTy := lltypes.NewPointer(boxTy)
	fn := block.Parent

	reuseBlock := fn.NewBlock("")
	allocBlock := fn.NewBlock("")
	merge := fn.NewBlock("")
	isNull := block.NewICmp(enum.IPredEQ, token, constant.NewNull(lltypes.NewPointer(lltypes.I8)))
	block.NewCondBr(isNull, allocBlock, reuseBlock)

	// Reuse: write the new value into the reclaimed box. rc is already 1 (drop-reuse
	// returns a unique box), but set it explicitly so the box is a well-formed fresh
	// value regardless of how it was reclaimed.
	reuseBox := reuseBlock.NewBitCast(token, boxPtrTy)
	rcPtr := reuseBlock.NewGetElementPtr(boxTy, reuseBox,
		i32c(0), i32c(boxStrongField))
	reuseBlock.NewStore(constant.NewInt(lltypes.I64, 1), rcPtr)
	reusePayloadPtr := boxPayloadPtr(reuseBlock, boxTy, reuseBox)
	reuseBlock.NewStore(payload, reusePayloadPtr)
	reuseBlock.NewBr(merge)

	// Alloc: no reclaimable box — allocate a fresh one.
	freshBox, err := l.lowerBoxShared(allocBlock, payload, payloadType)
	if err != nil {
		return nil, nil, err
	}
	allocBlock.NewBr(merge)

	phi := merge.NewPhi(ir.NewIncoming(reuseBox, reuseBlock), ir.NewIncoming(freshBox, allocBlock))
	return phi, merge, nil
}
