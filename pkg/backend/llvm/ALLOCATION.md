# Allocation representation — `stack` vs `shared` (LLVM lowering)

How Lyra's two storage flavors lower to LLVM. This is the decision behind
`lowerType`; the front-end already enforces the invariants this design relies on
(`AllocationOf`/`WithAllocation`, the `lyra-E014` recursive-type check, the
`lyra-E018` flavor-boundary check, and the effect checker's arena discharge).

## Summary

| Flavor | Semantics | LLVM representation |
|---|---|---|
| `stack` (and `Unspecified` → stack) | value semantics, inline | the aggregate/scalar **by value** |
| `shared` | multi-owner, heap | **`ptr`** to a ref-counted box `{ i64 rc, payload }` |

A function that takes `shared T` always sees the same `ptr`-to-box representation
regardless of where the value was allocated (global heap or an arena), so the ABI
is uniform.

## `stack` / value types

- LLVM type = the value itself: `i8..i64`/`half`/`float`/`double`/`i1` for
  primitives; `%T = type { … }` for structs; `[N x T]` for static arrays; the
  element struct for tuples.
- Binding/assignment **copies** the value (scalar copy or `memcpy` for
  aggregates). No heap, no refcount, no destructor beyond dropping fields.
- Calls: pass small values directly; large aggregates via `byval` pointer, and
  return them via an `sret` out-parameter (thresholds are a separate ABI detail).

## `shared` types

- LLVM type = `ptr` to `SharedBox(T) = { i64 rc, T payload }` (rc first so
  retain/release never need `T`'s layout).
- **Construct** (a `shared`-flavored value built outside an arena):
  `p = lyra_rc_alloc(sizeof box)`, set `rc = 1`, init `payload`; the value is `p`.
- **Retain** (value-copy of an owning `shared` binding, e.g. `let b = a` with both
  owning): `rc += 1`.
- **Release** (an owning `shared` binding goes out of scope):
  `if (--rc == 0) { drop payload; free(p) }`.
- Ownership modifiers decide whether retain/release fire — they are the whole
  point of `ref`/`mut`/`own`:
  - `own` param/return = **move**: transfer the pointer, **no retain** (the caller
    gives up its reference; use-after-move is guarded by todo #6).
  - `ref`/`mut` param = **borrow**: pass the pointer, **no rc change**; the callee
    must not release it.
  - plain owning copy = **retain**.
- Recursive types: a `shared` self-reference is just a `ptr`, so the box has finite
  size — this is exactly why `lyra-E014` requires a `shared` indirection to break a
  by-value cycle.

## Arena interaction (`with` blocks)

Arena-allocated `shared` values keep the same `ptr`-to-box representation but are
**bulk-freed** when the arena drops, never individually. To keep one ABI while
skipping per-value refcount traffic (the reason arenas exist):

- Reserve a **pinned** sentinel in `rc` (e.g. the high bit set) meaning
  "arena-owned — retain/release are no-ops". Arena construction
  (`lyra_arena_alloc`) sets the pinned sentinel; `retain`/`release` fast-path out
  when it is set. Arena teardown frees the whole block in one shot.

This is the runtime side of the effect checker's arena discharge (a `shared`
construction lexically inside a `with` block doesn't count as an escaping alloc).

## Runtime shims

```
lyra_rc_alloc(i64 size)            -> ptr    ; malloc(size) + rc = 1
lyra_rc_retain(ptr)                          ; rc += 1  (no-op if pinned)
lyra_rc_release(ptr, ptr drop_fn)            ; if --rc == 0: drop_fn(payload); free  (no-op if pinned)
lyra_arena_alloc(ptr arena, i64 size) -> ptr ; bump; rc = pinned
```

**Implemented (`runtime.go`, `ensureRCRuntime`):** `lyra_rc_alloc`,
`lyra_rc_retain`, and `lyra_rc_release` are emitted as **real function
definitions into the module itself**, built on libc `malloc`/`free` (declared as
externs, exactly like `memcmp`/`memcpy`). There is no separate runtime object to
link — `lyrac build`'s single `clang out.ll` stays self-contained. The box header
is a single `i64` refcount (`rcHeaderSize = 8`); the payload starts at
`box + 8` (Lyra's max alignment is 8, so no extra padding). `PinnedRC` is the
all-ones sentinel (`-1` as i64); retain/release compare against it and no-op.
`drop_fn` is called on the payload (`box + 8`) before `free` when the count hits
zero, and skipped when null. The runtime is emitted **lazily** — a program that
never heaps carries none of it.

**First consumer:** string concatenation (`++`, `lowerStringConcat`) allocates a
box via `rcAllocPayload` and `memcpy`s into it (STRING_LAYOUT.md). It does not yet
*release* — heap values currently leak (below). `lyra_arena_alloc` is still just a
reserved name (the `with`-block work). A real bump/pool allocator replaces
`malloc`/`free` later; `drop_fn` may become a per-type generated function.

## Deferred / out of scope for this decision

- **Emitting retain/release at Lyra call/scope sites** — the shims exist and are
  correct, but nothing wires an owning copy to a `retain` or a scope exit to a
  `release` yet (the backend doesn't read `own`/`ref`/`mut` param modes, and
  there's no scope-liveness tracking). So heap values (today: `++` strings)
  allocate and **leak**. This is the next allocation slice; it needs the
  ownership-modifier plumbing plus per-scope release insertion.
- **`shared`-value lowering** — `lowerType` still resolves a `shared T` to its
  by-value struct rather than a `ptr`-to-box; constructing a `shared` value
  (`let n: shared Node = …`) isn't lowered. The box runtime it will use now
  exists.
- **Atomic refcounts** — *not* needed while refcount mutations happen only through
  owning bindings in sequential code and auto-parallelized `pure`/`det` functions
  take **borrows** (`ref`), which touch no refcount. Revisit only when the job
  system lets a `shared` value's *ownership* cross threads.
- **Cycle leaks** — refcounting leaks reference cycles; the intended answer is
  `shared`-only cycles (E014) plus a future `weak` reference, not a tracing
  collector. Deferred.
- **COW**, large-value ABI thresholds, and the representations of `string`,
  dynamic arrays, and `data`/sum types (tagged-union layout) are separate lowering
  decisions, not this item.
