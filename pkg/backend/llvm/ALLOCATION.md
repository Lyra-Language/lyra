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

## Ownership: emitting retain/release (implemented for strings)

The retain/release *placement* is driven by a front-end **ownership pass**
(`pkg/analyzer/ownership`), which computes — for each managed value — the two
context-dependent adjustments the backend can't see locally: a **retain** when a
borrowed value flows into an owning position (a binding, an owned `return`, an
`own` argument), and a **release-after-statement** when an owned temporary flows
into a borrowing position (a comparison operand, a match scrutinee, a `++`
operand, a discarded statement, a borrowed argument). The backend
(`ownership_lower.go` + `lowerExpr`/`emitReturn`) then:

- releases each managed binding at its **scope exit** via a stack of scope frames
  (a `return` releases every live frame before it seals; break/continue paths
  conservatively leak — never a double free);
- honors param modes: an **`own`** managed param is released by the callee at its
  exit; **bare/`ref`/`mut`** are borrows the caller still owns;
- makes an `if`/`match` produce one merged owned value (each branch coerced to
  +1) released once at the phi, never per-branch;
- releases each temporary in the block it was produced in, so a temp built inside
  an `&&`/`if` branch is freed there (dominating its uses), not at a merge block.

This is live for **strings** today (uniform boxed representation makes retain/
release total — a literal's box is pinned, a `++` box is heap; STRING_LAYOUT.md).
Verified memory-safe under AddressSanitizer.

**[DECIDED 07/17] Direction: Perceus** (PLDI 2021 "Perceus: Garbage Free
Reference Counting with Reuse" — the Koka/Lean technique), evolving from
scope-exit toward last-use dup/drop, then drop specialization + dup/drop fusion,
then reuse analysis (in-place update of unique `shared` values — FBIP;
`is-unique` is `rc == 1`, which a `PinnedRC` arena box correctly fails), then
reuse specialization. `own`/`ref`/`mut` are exactly Perceus's owned/borrowed
calling conventions.

**[IN PROGRESS] Stage 1 — last-use precision (scalars).** The pass
(`pkg/analyzer/ownership`) now computes the *last use* of each eligible managed
binding (final textual reference; sound over-approximation — a binding that is
shadowed, a parameter, reassigned, or referenced inside a loop is ineligible and
keeps scope-exit release). At a last use it emits:

- **transfer** (owning position, e.g. `let b = a`, `return a`, an `own` arg) —
  the reference *moves* to the consumer, so **no dup** (the garbage-free win over
  the old always-dup-then-scope-drop). Only applied to an *unconditional* use
  (not inside an `if`/`match` branch), since a conditional transfer would leak on
  the path that skips it;
- **drop** (borrowing last use) — the binding is released at that statement
  rather than at scope exit.

The break/continue leak is also closed (the loop's managed frames are released on
those edges).

**[DONE] Stage 2 — dup/drop fusion.** Both a last-use transfer and a last-use drop
are now *fused* (no scope-exit release, no sentinel, no residual no-op):

- A **transfer** moves the reference at the use, so the backend retires the
  binding from its frame *immediately at the move* (`retireManagedSlot`).
- A **drop** is emitted by `dropLastUsesInStmt`: after each statement,
  `lowerBlockStmts` walks it for last-use-borrow nodes and, for each binding
  declared in the current scope, releases it and retires the slot — in the
  statement's end block, which **post-dominates** the statement's internal
  branches, so a *conditional* last use is freed correctly on every path. Placing
  drops at statement boundaries (not via a cross-statement pending list) is what
  avoids the "steal" hazard where an early return in the same statement would grab
  a drop belonging to the fall-through: a sealed statement is skipped, so its
  bindings are freed by the seal's frame release on that path, and the
  fall-through frees at the statement boundary — exactly once each.

A copy chain `a → b → c` now emits **one** allocation and **one** release, no
retains. The frame is still the leak-safe backstop for anything not fused
(ineligible/loop-referenced bindings, or a statement that sealed). Verified
memory-safe under AddressSanitizer, with static release==allocation conservation
checks (macOS ASan can't see leaks).

**Still open:** managed values inside aggregates (still leak — see below).

## `shared`-value lowering (implemented)

A `shared T` lowers to a **pointer to `SharedBox(T) = { i64 rc, T }`** (`lowerType`;
`shared.go`), so it slots into the same runtime + ownership machinery as strings:

- **Construction** (`lowerStructInstanceExpr`, `lowerDataConstruction`): a
  `shared`-flavored construction builds the inline payload, then `lowerBoxShared`
  allocates `header + sizeof(payload)` via `lyra_rc_alloc` (rc = 1) and stores the
  payload; the value is the box pointer. The flavor is read from the construction's
  recorded type, which the typechecker stamps `Shared` for the initializer of a
  `shared`-annotated binding (and, transitively, for a `shared` payload argument —
  so a recursive `Cons(1, Nil)` boxes the nested `Nil`).
- **Field access** (`lowerMemberExpr`): a `shared` object is a box pointer, so a
  field is read by `getelementptr` through the box (`box → payload → field`) + load,
  rather than `extractvalue` on an inline value.
- **`shared` fields** lower to pointers automatically (`lowerType`), which is also
  what makes a recursive `shared` field pointer-sized and finite (matching
  `SizeAndAlign` and the E014 check).
- **Ownership**: `IsManaged` now covers `shared` (`AllocationOf == Shared`), so a
  `shared` binding gets the full retain / release / last-use / transfer / drop
  treatment. Retain/release dispatch on the value's representation
  (`lowerManagedRetain`/`Release`): a string recovers its box via `stringBox`; a
  `shared` value *is* the box pointer. Verified memory-safe under AddressSanitizer
  with release==allocation conservation.

**`match` on a `shared` aggregate** is wired (`unboxSharedData`, `shared.go`): a
`shared` data/struct/tuple scrutinee is a box pointer, so the match loads the
inline payload out of the box (`box → field 1`) and the existing tag/pattern
machinery runs on that first-class value; an identifier catch-all still binds the
*box pointer* (its declared type), so the union and the whole value are threaded
separately (`lowerAggregateMatch`'s `whole` param). The box's own last-use release
is the ordinary managed-binding drop — reading through it consumes no reference.
This is the prerequisite for Perceus reuse/FBIP on `shared` values (you can't
reuse a box you can't destructure). A **nested** `shared data` sub-pattern
(destructuring a tail through *its* box, not just the top-level scrutinee) is not
handled and errors loudly.

Not yet done: `shared` arrays; `shared` construction in a bare argument/return
position (the flavor isn't stamped on the node there — only annotated bindings and
`shared` payload args get it); recursive **release** of a `shared`/string value
stored in an aggregate field (release passes a null `drop_fn`, so those leak — the
same aggregate-drop deferral below).

## Deferred / out of scope for this decision

- **Managed values inside aggregates** — a string or `shared` value stored in a
  struct/tuple/`data` field is conservatively *transferred* into the aggregate and
  then **leaks** (per-type aggregate drop isn't implemented — release uses a null
  `drop_fn`). Safe (never a double free), but not yet reclaimed. break/continue
  paths also leak the current iteration's bindings.
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
