# `pkg/analyzer/ownership` — retain/release placement

Computes where the backend must **retain** and **release** reference-counted ("managed") values
so each is freed exactly once (evolving toward Perceus — see ALLOCATION.md).
`ownership.Analyze(program, symTable, typeTable) *Table` runs after typechecking, and
`ownership.AnalyzeLambda(lam, symTable, tt, subst)` runs it again per **generic instantiation**
with that instantiation's type arguments substituted — managed-ness is a property of the type
argument, so a generic body analyzed once records decisions that are wrong at a managed
instantiation (see the generic-functions section). It (it reads the TypeTable to identify
managed types) and produces no diagnostics — the backend consumes the `Table`. Managed types are
`string`, `[]T`, any **function value** (a closure's environment is a ref-counted box —
closures.go), and any `shared`-flavored value (`IsManaged`) — plus, for the deep `OwnsManaged`
question, any **instantiation of a generic type** whose *substituted* contents own something
managed (`parameterizedOwnsManaged`: `Box<string>` owns a string, `Box<i64>` owns nothing, and
the declaration alone cannot answer it since its field type is the variable `t`; missing this
case was a double free, see the generic-types section) — read through the *base* of any newtype
(`types.StripNewtype`), since `newtype Email = string` is a string box wearing a name;
`OwnsManaged` has the matching `*ConstrainedType` case for the wrapper that arrives as an
`UnresolvedType` (a field declared `Email`). A nested `*ast.LambdaExpr` is an **owned producer**
(creating a closure allocates its environment) whose *body* is analyzed as a function in its own
right — its own last-use map, its own frames — while its **captures are not analyzed at the
creation site at all**: a capture is a copy, and the backend mints the environment's +1 on each
managed one, so recording a retain here too would double it. An **indirect call** reads its
conventions off the callee's static `LambdaType` (`calleeLambdaType`) rather than falling back
to the unknown-callee defaults: its parameters are borrows (a function type cannot express
`own`), and its result follows the declared return convention — treating a closure's owned
result as borrowed leaked the returned value at every call. **Ownership is deep** (07/29): every
*owning-position* decision uses `OwnsManaged(t, symTable)` — "does this value transitively own
anything refcounted?" — not `IsManaged`, so a `struct Person { name: string }` counts as owning
even though the struct itself is not a box. The backend's `needsDrop` delegates to
`OwnsManaged`, so the pass (which decides where a +1 is minted) and the backend (which decides
where one is released) cannot drift apart. A managed value stored in an aggregate *field* is
released by the backend's per-type **drop glue** (`pkg/backend/llvm/drop.go`) — run as a box's
`drop_fn`, and (since deep-retain-on-copy) also called directly at a stack-aggregate binding's
scope exit; the pass's side of that is simply that an aggregate field is an owning position (the
+1 transfers to the aggregate). **Copying** an aggregate is symmetrically an owning position: it
goes through the mirroring per-type **retain glue** (`pkg/backend/llvm/retain.go`), so a copy
holds its own +1 on each managed value it duplicates. Before that, a stack-aggregate copy was
unretained — not merely a leak but two ASan-confirmed use-after-frees (assignment through one
copy; and reading an aggregate out of a box whose drop glue then freed its fields). The model is
ARC over managed values: a binding / `own` param holds one owning reference, and the pass
computes the context-dependent adjustments the backend can't see locally:
- `Retain[expr]` — a borrowed value (an identifier / field / index read — including a container
  element read via `xs[i]` or `pair.0`, and any managed read reached inside a loop body, both of
  which the pass now walks) flowing into an owning position (a binding init, an owned `return`,
  an `own` arg) → dup to mint a fresh +1. For an ordinary binding read this fires only when it
  is **not** the binding's last use; a container element or a loop-body read is always a dup,
  since the container still owns (and drops) the element and a loop's back-edge re-runs the
  read;
- `ReleaseTemp[expr]` — an owned temporary (a `++` result, an owned call result, or an
  `if`/`match` merged value) flowing into a borrowing position (`==`/`!=`, a match scrutinee, a
  `++` operand, a discarded statement, a borrowed arg) → release after the statement;
- `LastUseTransfer[expr]` / `LastUseDrop[expr]` — **Perceus last-use precision** (stage 1,
  scalars): `computeLastUse` finds each eligible managed binding's final textual reference
  (sound over-approx; a shadowed / parameter / reassigned / loop-referenced name is ineligible
  and keeps scope-exit release). At that last use the reference either *transfers* (owning
  position — no dup; only when the use is unconditional, i.e. not inside a branch, so it happens
  on every path) or *drops* (borrowing last use — released there instead of at scope exit).

An `if`/`match` is treated as producing one merged owned value (each branch coerced to +1) so
its release is a single drop of the phi, never per-branch. Ownership per position mirrors the
typechecker's predicates (`paramOwnsArgument`/`isOwnedReturn`: only `own` params consume;
bare/`ref`/`mut` borrow; bare/`own` returns transfer). **Safety bias:** every uncertain case (an
unresolvable callee's args, a value entering an aggregate, an ineligible or conditional last
use) is biased toward *transfer to the scope-exit frame* (a leak — memory-safe), never toward an
early release (which would double-free/dangle). **That bias only applies to nodes the pass
actually visits** — *skipping* a node is not a conservative choice at all, because a missed
retain at an owning position dangles rather than leaks. The arithmetic forms
(`MathBinaryOpExpr`/`MathAssignOpExpr`/`NegationExpr`) were skipped on the reasoning that
arithmetic has no managed operands, which is true of the operation but not of what sits *inside*
it: `consume(p.name) + 1` passes a managed field to an `own` parameter, and with no retain
recorded the callee freed a box the struct still held (ASan-confirmed use-after-free, fixed
07/29). `TryExpr` was the same omission with the same cause (fixed 08/01): `?` looked like
control flow rather than a value, so it was never visited, and `parse(name)?` left the managed
value *inside* its operand unannotated. It is now modelled as what it is — the operand borrowed
like a match scrutinee, the payload read out of it duplicated in an owning position — and the
propagating path's re-wrap, which has no node of its own to mark, is retained by the backend
(`pkg/backend/llvm/try.go`). When adding an expression kind, recurse into every sub-expression
that can hold a value.
The backend half is `pkg/backend/llvm`'s `ownership_lower.go` (the managed-frame stack) + the
retain/temp-release/last-use hooks in `lowerExpr`/`emitReturn`. Both last-use kinds are
**fused** (stage 2 — no scope-exit release, no sentinel): a **transfer** removes the binding
from its frame at the move (`retireManagedSlot`); a **drop** is emitted by `dropLastUsesInStmt`,
which `lowerBlockStmts` runs after each statement — it walks the statement for last-use-borrow
nodes and releases+retires each current-scope binding in the statement's end block
(post-dominating the statement, so a conditional last use is freed on every path). A statement
that sealed (early return) is skipped, so the seal's frame release frees its bindings on that
path; the frame is the leak-safe backstop for anything not fused.

Correspondingly, a field a `match` arm binds is **duplicated, never moved**: the scrutinee's box
drops its own fields when it dies, so a moved field would be freed twice (most sharply when the
box is shared and survives drop-reuse). Eliding that dup/drop pair when the box is known unique
is Perceus stage 4; it costs refcount traffic, not allocations.

**Reuse / FBIP (stage 3, `shared` values)** — the pass also decides where the backend can
reclaim a matched box in place instead of freeing-then-allocating. `ReuseMatch[m]` marks a
`match` whose scrutinee is an owned binding (`let`/`var` or `own` param, via
`computeOwnedLastRef`) at its **last use**, whose type is a `shared data`, whose arms are a
plain tag switch (`plainTagSwitch` — no guards or value-test payloads, mirroring the backend's
switch path), and where ≥1 arm's tail is a construction of the *same* type; `ReuseTarget[c]`
marks each such construction (a value that consumes the reclaimed box). The scrutinee's ordinary
drop is still marked but the backend retires its slot at the drop-reuse point, suppressing it. A
**borrowed** scrutinee is never a reuse source (the caller still owns the structure). The
backend half is `pkg/backend/llvm`'s `lyra_rc_drop_reuse` (runtime.go), `lowerBoxSharedReuse`
(shared.go), the token threading in `lowerDataMatch`, and `dropReclaimedPayload` — the reclaimed
shell's *old* payload is dropped at the match's merge block, past every arm, guarded on the
token being non-null (dropping it at reclaim time would free a field an arm hasn't duplicated
yet); the typechecker's `propagateAllocation` stamps `shared` onto construction leaves inside
match arms so the arm's value is heap-boxed. See ALLOCATION.md.

## Trait-method parameter modes

A `.`-call's modes come from the **trait's declared signature**, resolved through the
`MethodTable` (`methodSignature`) — an impl binds patterns, not typed parameters, so there is
nowhere else they live. `resolveCallee` returns nil for a method call, so before this every
method argument fell to the conservative transfer: leak-safe, and correct while a trait
signature could not express a mode, but wrong the moment one says `own`.

**The receiver is signature parameter 0 and the arguments start at 1.** Reading the modes at
the wrong offset takes each argument's mode from the parameter to its left, which for `own`
is a double free or a leak rather than a type error — so the offset is written out rather
than folded into a loop index.

**`own` on a trait parameter is rejected by the checker (`lyra-E030`)**, and this pass is
why: it does not analyze trait-method *bodies*, so nothing records that a returned `own`
parameter was transferred rather than dropped. Implemented without that, `take: (Self, own
string) -> string` is a heap-use-after-free (measured under ASan, 07/31). `ref`/`mut` need
nothing from this pass — a borrow is retained and released by nobody — which is exactly why
they are supported and `own` is not. Supporting it means walking each `TraitMethodImpl` as a
function here, with a per-method table for the backend, the way `OwnershipBySpec` works per
instantiation.
