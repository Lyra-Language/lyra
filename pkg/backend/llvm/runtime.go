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
	// ShimRCWeakRetain(box ptr) : weak += 1 (no-op when pinned). Taken when a
	// `weak` reference is created — it keeps the box's *memory* alive without
	// keeping the value alive.
	ShimRCWeakRetain = "lyra_rc_weak_retain"
	// ShimRCWeakRelease(box ptr) : weak -= 1; free the box when both counts are 0
	// (no-op when pinned). The mirror of weak_retain, for a weak reference's death.
	ShimRCWeakRelease = "lyra_rc_weak_release"
	// ShimRCUpgrade(box ptr) -> ptr : the weak→strong upgrade. If the value is still
	// alive (strong != 0) increment strong and return the box — a new owning
	// reference; otherwise return null. This is the *only* way to read through a
	// weak reference, which is what makes a dangling read unexpressible.
	ShimRCUpgrade = "lyra_rc_upgrade"
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

// rcHeaderSize is the box header size in bytes: two i64 counts — strong, then
// weak — preceding the payload. Lyra's maximum scalar alignment is 8 (i64/f64/ptr)
// and aggregates align to ≤ 8, so a multiple-of-8 header places the payload at its
// natural alignment with no padding; the payload always starts exactly
// rcHeaderSize bytes into the box, whatever the box kind.
//
// **Why two counts, uniformly.** A `weak` reference must be able to ask "is the
// referent still alive?" without dereferencing freed memory, so a box's *memory*
// has to outlive its strong count: the value dies (drop glue runs) at strong 0,
// and the memory is freed only once weak also reaches 0. Two rejected
// alternatives, recorded because the choice costs 8 bytes per heap value:
// giving only weakly-referenced types a wide header makes the header size
// type-dependent — the same silent split-by-representation this backend has been
// bitten by twice (by-value `mut` params, newtype managed-ness) — and packing two
// 32-bit counts into one word puts mask/shift arithmetic in the hottest runtime
// functions plus an unenforced overflow assumption. Uniform is boring and
// obviously correct; revisit with measurements, not before.
const rcHeaderSize = 16

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
	pinnedBits := PinnedRC            // via a var: int64(constant) would overflow at compile time
	pinned := i64c(int64(pinnedBits)) // all-ones bit pattern == -1
	one := i64c(1)
	zero := i64c(0)

	l.malloc = l.module.NewFunc("malloc", i8ptr, ir.NewParam("", lltypes.I64))
	l.free = l.module.NewFunc("free", lltypes.Void, ir.NewParam("", i8ptr))

	// i8* @lyra_rc_alloc(i64 %size): p = malloc(size); *(i64*)p = 1; ret p.
	{
		size := ir.NewParam("size", lltypes.I64)
		fn := l.module.NewFunc(ShimRCAlloc, i8ptr, size)
		b := fn.NewBlock("entry")
		box := b.NewCall(l.malloc, size)
		counts := b.NewBitCast(box, i64ptr)
		b.NewStore(one, counts) // strong = 1
		// weak = 1, not 0: the strong owners collectively hold **one implicit weak
		// reference**, dropped by lyra_rc_release after the payload's drop_fn has run.
		// It is what makes the box's memory survive its own drop glue — see the
		// re-entrancy note on lyra_rc_release.
		b.NewStore(one, weakCountPtr(b, box, i64ptr))
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

		payload := callDrop.NewGetElementPtr(lltypes.I8, box, i64c(rcHeaderSize))
		dropTy := lltypes.NewPointer(lltypes.NewFunc(lltypes.Void, i8ptr))
		callDrop.NewCall(callDrop.NewBitCast(drop, dropTy), payload)
		callDrop.NewBr(doFree)

		// The value is dead, but its *memory* must survive while any weak reference
		// can still ask whether it is. That is done by **dropping the implicit weak
		// reference** the strong side took at allocation, rather than by testing the
		// weak count — and the difference is not cosmetic.
		//
		// Testing it here is a re-entrancy bug: `drop_fn` above runs arbitrary user
		// glue, and that glue can drop the last weak reference to **this same box**
		// through a cycle — a `Node` whose child holds `Maybe<weak Node>` back at it.
		// lyra_rc_weak_release then sees weak == 0 with strong already 0 and frees the
		// memory, and this function frees it a second time on the way out. Holding one
		// implicit weak for the strong side makes that impossible: while drop_fn runs,
		// the count cannot reach zero, so nothing can free the box out from under it.
		// Rust's Arc does exactly this, for exactly this reason.
		wPtr := doFree.NewBitCast(weakCountPtr(doFree, box, i64ptr), i64ptr)
		w1 := doFree.NewSub(doFree.NewLoad(lltypes.I64, wPtr), one)
		doFree.NewStore(w1, wPtr)
		reallyFree := fn.NewBlock("really_free")
		doFree.NewCondBr(doFree.NewICmp(enum.IPredEQ, w1, zero), reallyFree, done)
		reallyFree.NewCall(l.free, box)
		reallyFree.NewBr(done)

		done.NewRet(nil)
		l.rcRelease = fn
	}

	// i8* @lyra_rc_drop_reuse(i8* %box): the Perceus drop-reuse.
	//   pinned  → ret null      (arena-owned; never reclaimed here)
	//   rc == 1 → ret box       (unique; reclaim the shell, leave rc = 1, don't free)
	//   else    → rc -= 1; ret null   (shared; decrement, can't reuse)
	//
	// It deliberately does NOT drop the box's payload fields, even in the unique
	// branch — the caller must, and only *after* the match arms have duplicated the
	// fields they bind (a bind reads a field out of the box without a reference of its
	// own, so dropping here would free it under the arm). lowerDataMatch does exactly
	// that: it re-checks the token at the merge and drops the old payload there
	// (dropReclaimedPayload). The caller has already copied the union out via
	// unboxSharedData, so the old field values survive the shell being overwritten.
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

	// void @lyra_rc_weak_retain(i8* %box): if not pinned, weak += 1.
	{
		box := ir.NewParam("box", i8ptr)
		fn := l.module.NewFunc(ShimRCWeakRetain, lltypes.Void, box)
		entry := fn.NewBlock("entry")
		inc := fn.NewBlock("inc")
		done := fn.NewBlock("done")
		strong := entry.NewLoad(lltypes.I64, entry.NewBitCast(box, i64ptr))
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, strong, pinned), done, inc)
		wPtr := weakCountPtr(inc, box, i64ptr)
		inc.NewStore(inc.NewAdd(inc.NewLoad(lltypes.I64, wPtr), one), wPtr)
		inc.NewBr(done)
		done.NewRet(nil)
		l.rcWeakRetain = fn
	}

	// void @lyra_rc_weak_release(i8* %box): if not pinned, weak -= 1; when both
	// counts are 0 the memory is unreachable from either kind of reference, so free
	// it. The payload's drop glue already ran when strong hit 0 — a weak reference
	// never owns the value, only the storage — so there is nothing to drop here.
	{
		box := ir.NewParam("box", i8ptr)
		fn := l.module.NewFunc(ShimRCWeakRelease, lltypes.Void, box)
		entry := fn.NewBlock("entry")
		dec := fn.NewBlock("dec")
		maybeFree := fn.NewBlock("maybe_free")
		doFree := fn.NewBlock("free")
		done := fn.NewBlock("done")
		strongPtr := entry.NewBitCast(box, i64ptr)
		strong := entry.NewLoad(lltypes.I64, strongPtr)
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, strong, pinned), done, dec)
		wPtr := weakCountPtr(dec, box, i64ptr)
		w1 := dec.NewSub(dec.NewLoad(lltypes.I64, wPtr), one)
		dec.NewStore(w1, wPtr)
		// No strong check: the strong side holds one implicit weak reference of its
		// own (see lyra_rc_alloc), so weak reaching zero already means every strong
		// owner is gone *and* its drop glue has finished running.
		dec.NewCondBr(dec.NewICmp(enum.IPredEQ, w1, zero), maybeFree, done)
		maybeFree.NewBr(doFree)
		doFree.NewCall(l.free, box)
		doFree.NewBr(done)
		done.NewRet(nil)
		l.rcWeakRelease = fn
	}

	// i8* @lyra_rc_upgrade(i8* %box): the weak→strong upgrade.
	//   strong == 0 → the value is gone; return null.
	//   otherwise   → strong += 1 and return the box, a new owning reference.
	// A pinned box is always alive (an arena frees it in bulk), so it upgrades
	// without touching the count.
	{
		box := ir.NewParam("box", i8ptr)
		fn := l.module.NewFunc(ShimRCUpgrade, i8ptr, box)
		entry := fn.NewBlock("entry")
		alive := fn.NewBlock("alive")
		inc := fn.NewBlock("inc")
		retBox := fn.NewBlock("ret_box")
		retNull := fn.NewBlock("ret_null")
		strongPtr := entry.NewBitCast(box, i64ptr)
		strong := entry.NewLoad(lltypes.I64, strongPtr)
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, strong, pinned), retBox, alive)
		alive.NewCondBr(alive.NewICmp(enum.IPredEQ, strong, zero), retNull, inc)
		inc.NewStore(inc.NewAdd(strong, one), strongPtr)
		inc.NewBr(retBox)
		retBox.NewRet(box)
		retNull.NewRet(constant.NewNull(i8ptr))
		l.rcUpgrade = fn
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
	boxSize := block.NewAdd(payloadSize, i64c(rcHeaderSize))
	box = block.NewCall(l.rcAlloc, boxSize)
	payload = block.NewGetElementPtr(lltypes.I8, box, i64c(rcHeaderSize))
	return box, payload
}

// rcAllocStringPayload allocates a string's payload with **one byte more than its
// length, holding a NUL**.
//
// Every string a Lyra program can name is NUL-terminated past its end, which is what lets
// one be handed to C without copying it (`s.cstring_ptr()`, and `std.ffi`'s
// `with_cstring` over it). The terminator is *past* `byte_len` and no reader consults it,
// so nothing else about the representation changes: the length stays authoritative,
// interior NULs stay legal, and `byte_len` is what every operation still uses.
//
// **One byte per string, and it buys a copy per crossing.** A string handed to C used to
// be encoded into a fresh `[]u8`, scanned, and NUL-appended — an allocation and two passes
// at every call. See STRING_LAYOUT.md for the measurement.
//
// The invariant is "every producer allocates the extra byte and writes it", which holds by
// construction at the four heap sites that call this, plus the literal global
// (pinnedBoxConstant) and read_line (which reserves the byte in its growth test).
// `TestExec_EveryStringProducerIsNULTerminated` is what notices a fifth.
func (l *lowerer) rcAllocStringPayload(block *ir.Block, byteLen value.Value) (box, payload value.Value) {
	box, payload = l.rcAllocPayload(block, block.NewAdd(byteLen, i64c(1)))
	block.NewStore(constant.NewInt(lltypes.I8, 0),
		block.NewGetElementPtr(lltypes.I8, payload, byteLen))
	return box, payload
}

// weakCountPtr GEPs to a box's weak count. Written in raw byte terms (the box
// arrives as an i8* in the runtime shims, where the payload type is unknown) —
// the count words are the first two i64s of every box, whatever it wraps.
func weakCountPtr(b *ir.Block, box value.Value, i64ptr lltypes.Type) value.Value {
	off := i64c(boxWeakField * 8)
	return b.NewBitCast(b.NewGetElementPtr(lltypes.I8, box, off), i64ptr)
}
