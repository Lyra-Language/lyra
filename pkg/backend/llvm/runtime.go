package llvm

// Runtime shim ABI for `shared` (ref-counted) values — see ALLOCATION.md. A
// `shared` value is a `ptr` to a box `{ i64 rc, payload }`; the backend emits
// calls to these entry points and links a small runtime that provides them.
// Opaque pointers throughout; i64 for sizes/counts.
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
const PinnedRC = "18446744073709551615" // 2^64 - 1, as an LLVM i64 literal

// emitRuntimeDeclarations writes the `declare` lines for the runtime shims into
// the module. Unused declarations are harmless (nothing links until a call is
// emitted), so it is safe to include them unconditionally.
func emitRuntimeDeclarations(m *module) {
	m.comment("runtime shims for `shared` values (see ALLOCATION.md)")
	m.line("declare ptr @" + ShimRCAlloc + "(i64)")
	m.line("declare void @" + ShimRCRetain + "(ptr)")
	m.line("declare void @" + ShimRCRelease + "(ptr, ptr)")
	m.line("declare ptr @" + ShimArenaAlloc + "(ptr, i64)")
}
