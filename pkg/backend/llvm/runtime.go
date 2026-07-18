package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// Runtime shim ABI for `shared` (ref-counted) values and heap strings — see
// ALLOCATION.md. A heap value is a pointer to a box `{ i64 rc, payload }`; the
// backend emits calls to these entry points. Unlike an external runtime, the
// bodies are emitted *into the module itself* (ensureRCRuntime), built on libc
// `malloc`/`free` — there is no separate runtime object to link, so `lyrac
// build`'s single `clang out.ll` (and the test harness) stay self-contained,
// exactly as `memcmp`/`memcpy` are pulled straight from libc.
const (
	// ShimRCAlloc(size i64) -> ptr : malloc a box of `size` bytes (header +
	// payload), rc = 1, return the box pointer.
	ShimRCAlloc = "lyra_rc_alloc"
	// ShimRCRetain(box ptr) : rc += 1 (no-op when the box is pinned).
	ShimRCRetain = "lyra_rc_retain"
	// ShimRCRelease(box ptr, drop_fn ptr) : if --rc == 0 { drop_fn(payload); free } (no-op when pinned).
	ShimRCRelease = "lyra_rc_release"
	// ShimRCDropReuse(box ptr) -> ptr : the Perceus drop-reuse. If the box is
	// unique (rc == 1) return it *without freeing* — a reuse token the caller writes
	// a new value into (rc stays 1); if shared (rc > 1) decrement and return null; if
	// pinned (arena) leave it and return null. An unconsumed non-null token is freed
	// by the caller (`free(NULL)` is a valid no-op, so the caller frees unconditionally).
	ShimRCDropReuse = "lyra_rc_drop_reuse"
	// ShimArenaAlloc(arena ptr, size i64) -> ptr : bump-allocate a box in the
	// arena with rc = PinnedRC, so retain/release no-op and the arena bulk-frees.
	// (Arenas are not emitted yet — reserved name for the `with`-block work.)
	ShimArenaAlloc = "lyra_arena_alloc"
)

// PinnedRC is the refcount sentinel for arena-owned boxes: a box whose rc equals
// it is never individually retained/released or freed (the arena frees it in
// bulk). Max u64 (all bits set) keeps the check a single compare and can't
// collide with a real count. As a signed i64 constant this is -1 (the identical
// bit pattern); the compare is bitwise so signedness is irrelevant.
const PinnedRC uint64 = 1<<64 - 1

// rcHeaderSize is the box header size in bytes: a single i64 refcount preceding
// the payload. Lyra's maximum scalar alignment is 8 (i64/f64/ptr) and aggregates
// align to ≤ 8, so an 8-byte header places the payload at its natural alignment
// with no extra padding — the payload always starts exactly rcHeaderSize bytes
// into the box.
const rcHeaderSize = 8

// ensureRCRuntime emits the ref-counted heap runtime into the module the first
// time it's needed, caching the function handles on the lowerer (idempotent).
// The three shims are defined together — they share the box layout and libc
// malloc/free — so retain/release always exist alongside alloc, even in a slice
// that only calls alloc (string concatenation today).
func (l *lowerer) ensureRCRuntime() {
	if l.rcAlloc != nil {
		return
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	i64ptr := lltypes.NewPointer(lltypes.I64)
	pinnedBits := PinnedRC                                   // via a var: int64(constant) would overflow at compile time
	pinned := constant.NewInt(lltypes.I64, int64(pinnedBits)) // all-ones bit pattern == -1
	one := constant.NewInt(lltypes.I64, 1)
	zero := constant.NewInt(lltypes.I64, 0)

	l.malloc = l.module.NewFunc("malloc", i8ptr, ir.NewParam("", lltypes.I64))
	l.free = l.module.NewFunc("free", lltypes.Void, ir.NewParam("", i8ptr))

	// i8* @lyra_rc_alloc(i64 %size): p = malloc(size); *(i64*)p = 1; ret p.
	{
		size := ir.NewParam("size", lltypes.I64)
		fn := l.module.NewFunc(ShimRCAlloc, i8ptr, size)
		b := fn.NewBlock("entry")
		box := b.NewCall(l.malloc, size)
		b.NewStore(one, b.NewBitCast(box, i64ptr))
		b.NewRet(box)
		l.rcAlloc = fn
	}

	// void @lyra_rc_retain(i8* %box): if rc != PINNED, rc += 1.
	{
		box := ir.NewParam("box", i8ptr)
		fn := l.module.NewFunc(ShimRCRetain, lltypes.Void, box)
		entry := fn.NewBlock("entry")
		inc := fn.NewBlock("inc")
		done := fn.NewBlock("done")
		rcPtr := entry.NewBitCast(box, i64ptr)
		rc := entry.NewLoad(lltypes.I64, rcPtr)
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, rc, pinned), done, inc)
		inc.NewStore(inc.NewAdd(rc, one), rcPtr)
		inc.NewBr(done)
		done.NewRet(nil)
		l.rcRetain = fn
	}

	// void @lyra_rc_release(i8* %box, i8* %drop_fn): if rc != PINNED, rc -= 1;
	// when it hits 0, call drop_fn(payload) (if non-null) and free(box).
	{
		box := ir.NewParam("box", i8ptr)
		drop := ir.NewParam("drop_fn", i8ptr)
		fn := l.module.NewFunc(ShimRCRelease, lltypes.Void, box, drop)
		entry := fn.NewBlock("entry")
		dec := fn.NewBlock("dec")
		maybeDrop := fn.NewBlock("maybe_drop")
		callDrop := fn.NewBlock("call_drop")
		doFree := fn.NewBlock("free")
		done := fn.NewBlock("done")

		rcPtr := entry.NewBitCast(box, i64ptr)
		rc := entry.NewLoad(lltypes.I64, rcPtr)
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, rc, pinned), done, dec)

		rc1 := dec.NewSub(rc, one)
		dec.NewStore(rc1, rcPtr)
		dec.NewCondBr(dec.NewICmp(enum.IPredEQ, rc1, zero), maybeDrop, done)

		dropNull := maybeDrop.NewICmp(enum.IPredEQ, drop, constant.NewNull(i8ptr))
		maybeDrop.NewCondBr(dropNull, doFree, callDrop)

		payload := callDrop.NewGetElementPtr(lltypes.I8, box, constant.NewInt(lltypes.I64, rcHeaderSize))
		dropTy := lltypes.NewPointer(lltypes.NewFunc(lltypes.Void, i8ptr))
		callDrop.NewCall(callDrop.NewBitCast(drop, dropTy), payload)
		callDrop.NewBr(doFree)

		doFree.NewCall(l.free, box)
		doFree.NewBr(done)

		done.NewRet(nil)
		l.rcRelease = fn
	}

	// i8* @lyra_rc_drop_reuse(i8* %box): the Perceus drop-reuse.
	//   pinned  → ret null      (arena-owned; never reclaimed here)
	//   rc == 1 → ret box       (unique; reclaim the shell, leave rc = 1, don't free)
	//   else    → rc -= 1; ret null   (shared; decrement, can't reuse)
	// It does NOT drop the box's payload fields (matching lyra_rc_release's null
	// drop_fn today — managed fields inside an aggregate leak conservatively); the
	// caller reads everything it needs out of the box before drop-reuse (the match
	// already copied the union out via unboxSharedData), then overwrites the reused
	// box's payload.
	{
		box := ir.NewParam("box", i8ptr)
		fn := l.module.NewFunc(ShimRCDropReuse, i8ptr, box)
		entry := fn.NewBlock("entry")
		notPinned := fn.NewBlock("not_pinned")
		shared := fn.NewBlock("shared")
		retBox := fn.NewBlock("ret_box")
		retNull := fn.NewBlock("ret_null")

		rcPtr := entry.NewBitCast(box, i64ptr)
		rc := entry.NewLoad(lltypes.I64, rcPtr)
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, rc, pinned), retNull, notPinned)

		notPinned.NewCondBr(notPinned.NewICmp(enum.IPredEQ, rc, one), retBox, shared)

		shared.NewStore(shared.NewSub(rc, one), rcPtr)
		shared.NewBr(retNull)

		retBox.NewRet(box)
		retNull.NewRet(constant.NewNull(i8ptr))
		l.rcDropReuse = fn
	}
}

// rcAllocPayload emits an allocation of `payloadSize` heap bytes as a ref-counted
// box (rc = 1) and returns both the box pointer (for a future retain/release) and
// a pointer to the first payload byte (`box + rcHeaderSize`). `payloadSize` is an
// i64 value — a runtime length, as with string concatenation. Callers own the
// box; releasing it is the ownership story that this slice defers (heap values
// currently leak — see ALLOCATION.md / STRING_LAYOUT.md).
func (l *lowerer) rcAllocPayload(block *ir.Block, payloadSize value.Value) (box, payload value.Value) {
	l.ensureRCRuntime()
	boxSize := block.NewAdd(payloadSize, constant.NewInt(lltypes.I64, rcHeaderSize))
	box = block.NewCall(l.rcAlloc, boxSize)
	payload = block.NewGetElementPtr(lltypes.I8, box, constant.NewInt(lltypes.I64, rcHeaderSize))
	return box, payload
}
