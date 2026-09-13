# Allocation representation — `stack` vs `shared` (LLVM lowering)

The decision behind `lowerType`. The front end enforces the invariants it relies on:
`AllocationOf`/`WithAllocation`, `lyra-E014` (recursive types need a `shared`
indirection), `lyra-E018` (flavor boundary), and the effect checker's arena discharge.

## Summary

| Flavor | Semantics | LLVM representation |
|---|---|---|
| `stack` (and `Unspecified`) | value, inline | the scalar/aggregate **by value** |
| `shared` | multi-owner, heap | **pointer** to a ref-counted box |
| `[]T` | heap, always | pointer to a dynamic-array box (below), regardless of flavor |
| `string` | fat pointer into a box | STRING_LAYOUT.md |
| closure | `{ i8* fn, i8* env }`, env in a box | README.md, "Closures" |

A `shared T` parameter sees the same box pointer wherever the value was allocated, so the
ABI is uniform.

## The box

```
SharedBox(T)    = { i64 strong, i64 weak, T payload }           ; SharedBoxType
DynArrayBox(T)  = { i64 strong, i64 weak, i64 len, i64 cap, T* elems }   ; DynArrayBoxType
```

- `rcHeaderSize = 16`; the payload is always exactly 16 bytes in (Lyra's max alignment is
  8). Every box kind uses the same two-count header, so header size never depends on type.
- **`PinnedRC`** (all ones, `-1` as i64) marks a box that retain/release ignore — string
  literals today, arena boxes later.
- A `[]T`'s elements live in a separate `malloc`'d buffer (`elems`, may be null at cap 0)
  so `push` can grow without moving the box and dangling aliases. Cost: one extra load
  per element access. An empty `[]` still allocates a box — no null case.
- `managedBox` recovers the box pointer from any managed value (a string subtracts the
  header from `data`; a closure's `env` likewise; `shared`/`[]T` values *are* the box).

## Runtime shims (`runtime.go`)

Emitted **into the module** on first use (`ensureRCRuntime`), over libc `malloc`/`free`,
so `clang out.ll` needs no runtime object. A program that never heaps carries none.

```
lyra_rc_alloc(i64 size) -> i8*          ; strong = 1, weak = 1 (implicit, see below)
lyra_rc_retain(i8* box)                 ; strong += 1          (pinned: no-op)
lyra_rc_release(i8* box, i8* drop_fn)   ; --strong == 0: drop_fn(payload), then drop the implicit weak
lyra_rc_drop_reuse(i8* box) -> i8*      ; unique: return box unfreed; shared: decrement, null; pinned: null
lyra_rc_weak_retain(i8* box)            ; weak += 1
lyra_rc_weak_release(i8* box)           ; --weak == 0: free
lyra_rc_upgrade(i8* box) -> i8*         ; strong != 0: strong += 1, return box; else null
lyra_arena_alloc                        ; reserved name only — arenas are not emitted
```

**The strong owners hold one implicit weak reference**, taken in `lyra_rc_alloc` and
dropped after `drop_fn` returns. A `drop_fn` is arbitrary glue that can, through a cycle,
drop the last real weak reference to the box it is running on; without the implicit weak
that frees the box mid-drop and the outer release frees it again. Do not "simplify" this to
a `weak == 0` test — ASan-confirmed double free.

Pass a drop function to a shim as an `i8*`, never as the `*ir.Func` — opaque pointers hide
the mismatch on macOS; typed-pointer clang (`asan.sh`) rejects it.

## Ownership: where retain/release go

Placement comes from the front-end ownership pass (`pkg/analyzer/ownership`): a **retain**
where a borrowed value flows into an owning position (binding, owned return, `own` arg,
aggregate field), and a **release-after-statement** where an owned temporary flows into a
borrowing one. The backend (`ownership_lower.go`, `lowerExpr`, `emitReturn`):

- releases each managed binding at **scope exit** via a stack of frames; `return` releases
  every live frame before sealing, `break`/`continue` the frames they leave;
- param modes: `own` is released by the callee; bare/`ref`/`mut` are borrows;
- an `if`/`match` yields one merged owned value, released once after the phi;
- releases temporaries per the dominance rules in README.md ("Statement temporaries").

Ownership is **deep**: the question is whether a value transitively owns a reference
(`ownership.OwnsManaged`, which `needsDrop` delegates to). Copying a stack aggregate
retains every managed value it reaches; stack-aggregate bindings are framed and
deep-released. The retain and drop walks are one function (`emitOwnedValue`,
owned_walk.go) so they cannot cover different fields.

### Perceus (direction decided; stages 1–3 built)

`own`/`ref`/`mut` are Perceus's owned/borrowed conventions.

1. **Last use.** The pass marks each eligible binding's final use (ineligible: shadowed,
   parameter, reassigned, referenced in a loop — those keep scope-exit release). An owning
   last use **transfers** (no dup) — only when unconditional; a borrowing last use
   **drops** at that statement.
2. **Fusion.** A transfer retires the binding from its frame at the move
   (`retireManagedSlot`). A drop is emitted by `dropLastUsesInStmt` after the statement, in
   its post-dominating end block, so a conditional last use is freed on every path; a
   sealed statement is skipped and its frame release covers that path. A copy chain
   `a → b → c` is one allocation and one release.
3. **Reuse / FBIP.** `ReuseMatch` marks a `data` match whose scrutinee is an owned `shared`
   binding at its last use, whose arms are a plain tag switch (no guards, no value-testing
   payloads), and where ≥1 arm constructs the same type (`ReuseTarget`). `lowerDataMatch`
   calls `lyra_rc_drop_reuse` once after unboxing and retires the scrutinee's slot; a target
   construction writes into the token or allocates fresh (`lowerBoxSharedReuse`, a phi of
   the two); a non-constructing arm frees the token (`free(NULL)` is fine).
   - `drop_reuse` does **not** drop payload fields: arms read fields straight out of the
     box. `dropReclaimedPayload` drops them at the merge, guarded on a non-null token, from
     the union `unboxSharedData` copied out first.
   - **Arm bindings dup, they do not move** — a moved field would be freed twice when the
     box is shared and survives. Costs refcount traffic, not allocations.
   - A **borrowed** scrutinee is never reused.

**Open (stage 4):** skip stores for fields shared with the matched constructor, a static
uniqueness fast path, token-conditional dup (dup only on the null-token path); reuse
through the guard/value-test ladder; struct/tuple reuse.

## `shared` lowering (`shared.go`)

- **Construction**: build the payload inline, then `lowerBoxShared` allocates and stores
  it. The flavor is read from the construction's recorded type, stamped by the
  typechecker's `propagateExpectedType` from any context (binding, return, argument,
  field, payload, element, arm).
- **Field access / assignment** GEP through the box (`box → payload → field`).
- **`shared` fields** lower to pointers, which is what makes a recursive type finite.
- **`match`**: `unboxSharedData` loads the payload out of the box; an identifier catch-all
  binds the *box pointer*, so `lowerMatchLadder` threads `whole` separately from `scrut`.
  A **nested** `shared data` sub-pattern is a loud error. Test the **Lyra** type before
  unboxing — a raw pointer is also an LLVM pointer.
- **`shared [N]T`** is a box around `[N x T]`; indexing goes through
  `sharedArrayPayloadPtr`.

## Drop glue (`drop.go`)

`void @lyra_drop_T(i8* payload)`, generated once per payload type and cached **before**
its body is built (so a self-referential type emits one function calling itself).

- **Stops at a managed value**: one `lyra_rc_release` and no descent — that box runs its
  own glue. The walk only descends through inline aggregates, so it terminates for the
  same reason `resolveForLayout` does (cycles pass through `shared`).
- `data` payloads switch on the tag; only the live variant is dropped.
- A `[]T`'s glue (`dynArrayDropFn`, via `boxDropFn`) loops over `len` elements, then frees
  the buffer. A looped drop makes static release-site counts meaningless as a leak check;
  use ASan/LSan.
- A payload owning nothing gets no glue and a null `drop_fn`.

## Not done / deferred

- **Interior assignment through a borrowed root's by-value copy** releases nothing (the
  caller owns it); by-reference `mut`/`ref` closed the case that mattered.
- **Atomic refcounts** — not needed while ownership never crosses threads (parallel
  `pure`/`det` work takes borrows).
- **Cycles** leak unless broken with `weak` (`weak.go`); no tracing collector.
- **Arenas** (`with` blocks) — backend errors loudly (`lyra-E050`). Planned: arena boxes
  set `PinnedRC`, bulk-freed at arena drop.
- `byval`/`sret` thresholds for Lyra's own calling convention (C boundary: `pkg/abi`).
