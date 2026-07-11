package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/types"
)

// Runtime shim ABI for `shared` (ref-counted) values — see ALLOCATION.md. A
// `shared` value is a pointer to a box `{ i64 rc, payload }`; the backend emits
// calls to these entry points and links a small runtime that provides them.
const (
	// ShimRCAlloc(size i64) -> ptr : malloc a box of `size` bytes, rc = 1.
	ShimRCAlloc = "lyra_rc_alloc"
	// ShimRCRetain(box ptr) : rc += 1 (no-op when the box is pinned).
	ShimRCRetain = "lyra_rc_retain"
	// ShimRCRelease(box ptr, drop_fn ptr) : if --rc == 0 { drop_fn(payload); free } (no-op when pinned).
	ShimRCRelease = "lyra_rc_release"
	// ShimArenaAlloc(arena ptr, size i64) -> ptr : bump-allocate a box in the
	// arena with rc = PinnedRC, so retain/release no-op and the arena bulk-frees.
	ShimArenaAlloc = "lyra_arena_alloc"
)

// PinnedRC is the refcount sentinel for arena-owned boxes: a box whose rc equals
// it is never individually retained/released or freed (the arena frees it in
// bulk). Max u64 (all bits set) keeps the check a single compare and can't
// collide with a real count.
const PinnedRC uint64 = 1<<64 - 1

// declareRuntime adds `declare` entries for the runtime shims to the module. A
// Func with no basic blocks prints as a `declare`. Unused declarations are
// harmless (nothing links until a call is emitted), so it is safe to add them
// unconditionally.
func declareRuntime(m *ir.Module) {
	i8ptr := types.NewPointer(types.I8)
	m.NewFunc(ShimRCAlloc, i8ptr, ir.NewParam("", types.I64))
	m.NewFunc(ShimRCRetain, types.Void, ir.NewParam("", i8ptr))
	m.NewFunc(ShimRCRelease, types.Void, ir.NewParam("", i8ptr), ir.NewParam("", i8ptr))
	m.NewFunc(ShimArenaAlloc, i8ptr, ir.NewParam("", i8ptr), ir.NewParam("", types.I64))
}
