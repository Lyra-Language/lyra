# `pkg/analyzer/ownership` — retain/release placement

Computes where the backend must **retain** and **release** reference-counted ("managed") values so
each is freed exactly once (Perceus — see
[`ALLOCATION.md`](../../backend/llvm/ALLOCATION.md)). Produces no diagnostics; the backend
consumes the `Table`.

- `ownership.Analyze(program, symTable, typeTable) *Table` runs after typechecking.
- `ownership.AnalyzeLambda(lam, symTable, tt, subst)` re-runs per **generic instantiation** with
  type arguments substituted — managed-ness depends on the type argument.
- Trait-method bodies are analyzed per specialization (`driver.OwnershipByMethod`, keyed by
  `Resolution.SpecKey`).

## What is managed

`IsManaged`: `string`, `[]T`, any function value (a closure environment is a box), any `shared`
value, `weak T` (weak count only), a held `Seq` — read through a newtype's base (`types.StripNewtype`).

`OwnsManaged(t, symTable)` is the **deep** question — does the value transitively own a reference —
and every owning-position decision uses it, so `struct Person { name: string }` owns. It covers
generic instantiations by substituted contents (`parameterizedOwnsManaged`: `Box<string>` owns,
`Box<i64>` doesn't) and a `*ConstrainedType` arriving as an `UnresolvedType`. The backend's
`needsDrop` delegates to it, so minting and releasing cannot drift.

Aggregate fields are released by the backend's **drop glue** (`drop.go`) and copies go through
**retain glue** (`retain.go`); for this pass, an aggregate field and an aggregate copy are both
owning positions.

## The table

A binding / `own` parameter holds one owning reference. The pass records:

- **`Retain[expr]`** — a borrowed value (identifier, field, index, `pair.0`, `p^`) flowing into an
  owning position (binding init, owned `return`, `own` arg, aggregate field). For a plain binding
  only when not its last use; a container element or loop-body read always dups.
- **`ReleaseTemp[expr]`** — an owned temporary (`++` result, owned call result, `if`/`match` merged
  value, tuple/struct/array/repeat/comprehension literal) flowing into a
  borrowing position (`==`, match scrutinee, `++` operand, discarded statement, borrowed arg or
  receiver) — released after the statement. **Every owned producer needs this arm**; a missing one
  is a leak only LeakSanitizer reports.
- **`LastUseTransfer` / `LastUseDrop`** — Perceus last use. `computeLastUse` finds an eligible
  binding's final textual reference (shadowed, parameter, reassigned, loop-referenced and
  address-taken names are ineligible). An owning last use *transfers* only if unconditional; a
  borrowing one *drops* there. The backend fuses both (`retireManagedSlot`,
  `dropLastUsesInStmt`); the managed frame is the leak-safe backstop.
- **`ReuseMatch[m]` / `ReuseTarget[c]`** — FBIP reuse: a `match` on an owned binding
  (`computeOwnedLastRef`) at its last use, of a `shared data` type, with a plain tag switch
  (`plainTagSwitch`) and ≥1 arm constructing the same type. A borrowed scrutinee is never a reuse
  source. Backend: `lyra_rc_drop_reuse`, `lowerBoxSharedReuse`, `lowerDataMatch`,
  `dropReclaimedPayload` (old payload dropped at the merge, not at reclaim).

Rules:

- An `if`/`match` produces **one merged owned value**; its release is one drop of the phi.
- A field bound by a `match` arm is **duplicated, never moved** (the box drops its own fields).
- Modes mirror the typechecker (`paramOwnsArgument`/`isOwnedReturn`): only `own` params consume;
  bare/`ref`/`mut` borrow; bare/`own` returns transfer.
- **Safety bias**: uncertain cases transfer to the scope-exit frame (a leak), never release early.
  **This only holds for nodes the pass visits** — skipping a node misses a retain, which dangles.
  When adding an expression kind, recurse into every sub-expression (arithmetic and `?` were both
  skipped once, both use-after-frees).
- `TryExpr`: operand borrowed like a scrutinee, payload duplicated; the propagating re-wrap is
  retained by the backend (`try.go`).
- A nested **lambda** is an owned producer whose body is analyzed as its own function; its
  **captures are not analyzed at the creation site** (the backend mints the environment's +1).
- An **indirect call** reads conventions from the callee's `LambdaType` (`calleeLambdaType`):
  params borrow, result follows the declared return.
- **`yield e` borrows `e`.**

## Method calls

A `.`-call's parameter modes **and result ownership** come from the trait's declared signature via
the `MethodTable` (`methodSignature`); a bound-dispatched call with no resolution falls back to the
declaring trait's signature.

- **The receiver is signature parameter 0; arguments start at 1.** The offset is written out, since
  an off-by-one is a double free or leak, not a type error.
- **The receiver is transferred when parameter 0 is `own`** — it is not in `e.Arguments`.
- **A borrowed receiver that is a temporary is walked** (`mk().len()` must release). A receiver
  **rooted at a binding** (`xs.len()`, `h.xs.len()`) is deliberately **not** walked
  (`rootedAtBinding`): recording a use moves Perceus's drop (`n.weak()` then failing, `xs.data()`
  freeing under its pointer).
- `checker/use_after_move.go` must resolve method callees too, or `own` through a method goes
  unchecked.

## The expression switch's `default:`

`analyzer.expr` records nothing for an unhandled kind, which is **not** safe (a missed retain
dangles). Borrow-only kinds (result is a number, bool or raw pointer) share one case walking
children with `ast.WalkExprChildren`; `ArrayCompExpr` has a real arm (owning, analyzed under
`conditional`). Still in `default`, deliberately:

- `DataConstructorExpr` — always nullary, already a fresh +1.
- `AwaitExpr`, `ComposeExpr` — no backend case; each owes an arm in the change that lowers it.
- `YieldFromExpr` — its operand is a sequence consumed in place; nothing to release.

A shared traversal can say what a node's children are; only a per-kind arm can say whether a
position **owns**.

## Taking an address pins the binding

`unsafe { … }` is `a.block(e.Body, needOwned)`, paired with `noteAddressTaken`: an address-taken
binding is excluded from last use in **both** `computeLastUse` and `computeOwnedLastRef`, pinning
the **root** of the place (`&xs[0]` pins `xs`). A raw pointer keeps storage alive without counting
as a reference, so otherwise `CBuffer { ptr: unsafe { &xs[0] }, len: 3 }` frees `xs`.

Remove the pin → use-after-free. Remove the arm → double free for a value shared out of the block
(`TestExec_UnsafeBlockRetainsWhatItShares`).

## `SharesMutableState` and the shared fold

`SharesMutableState(t, symTable, loc)` asks whether copying a value's shared reference is
**observable** (used by `lyra-W019`, `checker/array_repeat_alias.go`). Yes for `[]T`, a `shared`
aggregate with a writable field, or any struct/tuple/`data`/`[N]T`/instantiation containing one; no
for a string (immutable). `readonly` does **not** stop it; a `shared` scalar does not share.
`SharedMutablePath` is the implementation and returns the field-name path for the diagnostic.

`OwnsManaged` and `SharedMutablePath` share `eachComponent` (component plus field name, `""` when
positional). The generic-instantiation arm (`instantiateDecl`) is **not** shared:
`SharedMutablePath` needs a `seen` set because it descends past a `shared` value with no writable
field, while `OwnsManaged` stops at `shared`/`weak` (which lyra-E014 guarantees break every cycle).
