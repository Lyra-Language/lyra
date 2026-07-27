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
box via `rcAllocPayload` and `memcpy`s into it (STRING_LAYOUT.md). `drop_fn` *is*
now a per-type generated function — the recursive drop glue (`drop.go`, below) that
frees what a dying box's payload owns. `lyra_arena_alloc` is still just a reserved
name (the `with`-block work); a real bump/pool allocator replaces `malloc`/`free`
later.

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

**[DONE] Stage 3 — reuse analysis / FBIP (`shared` values).** When a `match`
destructures an owned `shared data` value at its last use, its box is *reclaimed*
instead of freed, and a same-type construction in an arm writes the new value into
it **in place** — Functional But In Place. This is the true (dynamic) Perceus
mechanism: uniqueness is a runtime test, so aliased graphs stay correct.

- **`lyra_rc_drop_reuse(box) -> ptr`** (runtime.go): unique (`rc == 1`) → returns
  the box (a *reuse token*), rc left at 1, **not** freed; shared (`rc > 1`) →
  decrements and returns null; pinned (arena) → returns null untouched. It
  deliberately does *not* drop the box's payload fields, even when unique: an arm
  binds a field by reading it straight out of the box, taking no reference of its
  own, so dropping here would free a field the arm is about to use. The caller drops
  instead, at the merge past every arm — `dropReclaimedPayload` (match_aggregate.go),
  guarded on the token being non-null, reading the old field values from the union
  `unboxSharedData` copied out before the shell could be overwritten.
- **Reuse-aware construction** (`lowerBoxSharedReuse`, shared.go): a runtime branch
  on the token — non-null → write the new payload into the reclaimed box (rc = 1),
  null → a fresh `lyra_rc_alloc`. A phi of the two box pointers.
- **The pass** (`pkg/analyzer/ownership`): `ReuseMatch` marks a match whose
  scrutinee is an owned binding (`let`/`var` or `own` param) at its last use, whose
  type is a `shared data`, whose arms are a plain tag switch (no guards / value-test
  payloads — the reuse-wired backend path), and where ≥1 arm constructs the same
  type. `ReuseTarget` marks each such arm-tail construction. The backend
  (`lowerDataMatch`) drop-reuses the box once after unboxing, retires the
  scrutinee's slot (suppressing its ordinary drop), and hands the token to the arms:
  a target consumes it (in-place write), a non-constructing arm frees it
  (`free(NULL)` is a no-op, so freeing is unconditional).
- **Arm bindings duplicate, they do not move.** An earlier revision moved a field
  bound exactly once out of a consuming match (no dup), which was sound only because
  the box then abandoned its fields (null `drop_fn`). Now that a box really drops
  what it owns, a moved field would be freed twice — most sharply when the box is
  *shared* (`rc > 1`), where drop-reuse decrements and the box survives still owning
  the field. So a bound field is dup'd and the box's drop releases its own reference.
  This costs refcount traffic, **not allocations**: reuse still reclaims the shell,
  so `map`/`filter`/tree rebuilds remain zero-allocation-per-cell. Eliding the
  dup/drop pair when the box is known unique is stage 4 (below).
- **Return of a `shared` value** (`emitReturn` pointer case) and the typechecker's
  **`propagateAllocation`** (a `shared` return type / annotation stamps the
  construction leaves inside `match`/`if` arms `shared`, so the arm's value is
  heap-boxed) are the two supporting pieces this needed.

Verified memory-safe under AddressSanitizer across the linear in-place update, the
recursive FBIP map, the token-free path (a non-constructing arm), and the safety
boundary — a **borrowed** scrutinee is never reused (the caller still owns the
structure), pinned by both a no-`drop_reuse` IR check and an ASan run.

**Still open:** reuse specialization (stage 4 — skip stores for fields the reused
constructor shares with the matched one, a static uniqueness fast path to drop the
runtime branch, and the token-conditional dup that restores arm-binding *moves*:
dup a bound field only on the null-token path, where the box survived); reuse
through the ladder-fallback path (guards / value-test payloads); and struct/tuple
(non-`data`) reuse.

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

**`shared` arrays** are wired: a `shared [N]T` lowers to a `ptr` to
`{ i64 rc, [N x T] }` (the ordinary `shared`-flavored `lowerType` path), so it
reuses the whole shared-box runtime. `lowerArrayLiteralExpr` builds the inline
`[N x T]` and boxes it (`lowerBoxShared`) when the recorded type is `shared` — the
typechecker's `propagateAllocation` stamps the flavor onto the array-literal node
(same as struct/data construction). `lowerIndexExpr` geps through the box's payload
(`sharedArrayPayloadPtr` — box → field 1 → element), for both a constant and a
bounds-checked runtime index, borrowing the box (no reference consumed). The
per-type drop glue gained an array case (`emitDropArray`), so a `shared [N]String`
frees each element when the box dies (`needsDrop`/`emitDropValue` recurse into the
element type; N is constant, so the element drops are unrolled). The ownership pass
treats an array literal's elements as owning positions (`ArrayLiteralExpr` case,
mirroring tuples/structs), so a managed element transfers its reference into the box
rather than being double-freed by both its binding and the box's drop. Verified
under AddressSanitizer with a static allocations==releases conservation check
(`llvm_shared_array_test.go`).

Not yet done: dynamic arrays (`[]T`); `shared` construction in a bare argument/
return position (the flavor isn't stamped on the node there — only annotated
bindings and `shared` payload args get it); `match` on a `shared` array (the array
type reaches the match-unbox path but the element-wise array pattern isn't lowered).

## Aggregate-field drop: the per-type drop glue (implemented)

A managed value stored *inside* a box — a `string` field, a nested `shared` value,
the tail of a recursive list — is owned by that box and must be released when it
dies. `drop.go` generates, once per payload type and cached,

```
void @lyra_drop_T(i8* payload)
```

which releases every managed reference reachable **by value** from `T`, and
`lowerManagedRelease` passes it as the box's `drop_fn`. So freeing a list frees the
whole spine, one box at a time, instead of freeing the head cell and leaking the
tail.

- **"By value" is the stopping rule.** A managed field is released with a single
  `lyra_rc_release` and never walked into — that box runs its *own* `drop_fn` if and
  when its count reaches zero. The walk therefore only descends through inline
  `stack` aggregates, and a recursive type's cycle must pass through a `shared`
  field (lyra-E014), which is exactly where it stops. Generation is finite for the
  same reason `resolveForLayout` is, and the function is cached *before* its body is
  built so a self-referential type emits one function that calls itself.
- **`data` payloads switch on the tag** (`emitDropData`): only the live variant's
  fields are dropped, so a nullary variant's undefined payload blob is never read.
  A struct/tuple is a single shape, so its fields are read with `extractvalue`, no
  branch.
- **Pay for what you use:** a payload owning nothing generates no glue and keeps a
  null `drop_fn`.
- **The retain side was already right:** the ownership pass has always treated an
  aggregate field as an *owning* position, so a value flowing into one transfers its
  +1 to the aggregate. This closes the other half — releasing it.

Verified under AddressSanitizer, plus a static allocations-vs-releases conservation
check (macOS ASan can't see leaks) — `llvm_aggregate_drop_test.go`.

## Deferred / out of scope for this decision

- **Managed values inside a *stack* aggregate** still leak. The drop glue above
  covers a `shared` (boxed) aggregate, whose death is a refcount reaching zero. A
  stack aggregate is a *value*: `let q = p` copies it and duplicates its field
  references with no retain, so dropping both copies would double-free. Making it
  sound needs deep-retain-on-aggregate-copy in the ownership pass first — then a
  stack aggregate binding can run the same glue at scope exit. Safe today (a leak,
  never a double free).
- **Fields an arm neither binds nor the box drops** — none today: the box drops
  everything it still owns. Precision loss is only the extra dup/drop traffic noted
  under stage 4.
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
