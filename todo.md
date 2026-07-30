# Lyra — To-Do

What is **open**. Finished work and the reasoning behind it live in
[COMPLETED.md](COMPLETED.md); when an item below says something landed, that is where the
detail is. For what the compiler does *today*, read `CLAUDE.md` — this file is not a
status report.

Tags: **[OPEN]** not started · **[PARTIAL]** landed in part · **[DECIDED]** settled, not
built · **[IDEA]** not committed to · **[ROADMAP]**/**[DEFERRED]** deliberately later.

## Known bugs

None open. The last three closed 07/30 (borrowed-`string` use-after-free, anonymous-tuple
literal width, `i128` multiply link failure on Linux) — see COMPLETED.md.

## In progress

### Backend — LLVM IR

Everything through closures, generics, strings, arrays, `match`, and interior assignment
lowers; `CLAUDE.md`'s `pkg/backend/llvm` section is the current inventory. Settled specs:
`ALLOCATION.md` (`stack` = inline, `shared` = ptr to a ref-counted box), `DATA_LAYOUT.md`
(tagged union), `STRING_LAYOUT.md`, `SIMD.md`. What is left:

- **[PARTIAL] Perceus reference counting** (PLDI 2021; the Koka/Lean technique). Stages 1–3
  are in — last-use precision, dup/drop fusion, and reuse/FBIP on `shared` values (a
  recursive `map` rebuilds every cell with zero allocation).
  - **Stage 4, reuse specialization** — token-conditional dup, a static-uniqueness fast
    path, skipping shared-field stores. Deferred on purpose: it buys refcount traffic, not
    allocations, and carries real double-free risk. Best done after a dynamic-array growth
    operation exists.
  - **Also open:** hoisting a *conditional* last use (one inside a branch still falls back
    to the scope frame); reuse through the ladder path (guards / value-testing payloads);
    struct and tuple reuse.
  - Committed consequences: `weak` must be real before shared object graphs (rc has no
    cycle collector); drop timing is last-use, so no user-observable finalizers unless
    separately decided; a checked FBIP annotation (Koka's `fip`) follows if the uniqueness
    cliff bites.
- **[PARTIAL] Closure lowering is tiered** — dev = uniform boxed closures (landed), release
  = **Lambda Set Specialization** (PLDI 2023). LSS is still to build, gated on the generics
  monomorphizer as decided: one specializer, two parameter axes. The tiers are semantically
  identical, so `noalloc` is defined against the *release* lowering. Hot-reload note:
  body-only edits keep lambda sets stable; adding/removing a lambda or changing captures
  means a full rebuild even in dev.
- **[OPEN] Generic types** — `where` bounds on a type parameter are collected but not
  enforced at the instantiation. `Maybe<weak T>` does not parse (a grammar change). A
  trait-impl *method* on a generic receiver does not lower — though neither does one on a
  non-generic receiver, so that is a trait gap, not a generics one.

### Modules

Resolution, per-module scoping, `pub`, the implicit prelude, per-module name resolution and
symbol mangling all landed 07/30.

- **[OPEN] Types and traits share one program-wide namespace.** Two modules cannot declare
  unrelated same-named types, and a user type shadowing a prelude type replaces it for the
  whole program. Both need the same thing: per-module type *identity* end to end — mangled
  type symbols plus a location-aware `LookupType`. `SymbolTable.Types` is keyed by bare
  name today, and so is the backend's registry of emitted LLVM struct types, which resolves
  a type reference carrying no location to say who is asking.
- Out of scope by decision, none of it changing what a module's source looks like: package
  management, versioning, separate/incremental compilation.

## Language design — Pit of Success

Make the safe path the default.

### 1. Must-use `Result`/`Maybe` + `?` propagation

Canonical Result/Maybe identity is settled (a `CanonicalKind` stamp via the
name-independent `@builtin` attribute), and `?` checks the operand's error type against the
enclosing return.

- **[OPEN]** From-style **declared error conversion**, once a conversion trait exists.
  Today `?` is assignability-only.
- **[OPEN] `?` does not lower** — `expression lowering not implemented for *ast.TryExpr`.
  It type-checks (including the enclosing-return check, `lyra-E008`) and then fails the
  build, so no program can actually use it. Found by exercising the prelude, 07/30.
- **[OPEN] Shadowing a marked canonical type gives a useless diagnostic.** Now that
  `std/prelude.lyra` marks its types `@builtin(Maybe)`/`@builtin(Result)`, the marker
  claims the kind and `resolveCanonicalTypes` leaves a same-named *unmarked* type "an
  ordinary type" — correct in itself. But a user's `data Maybe` shadows the prelude's
  **program-wide** (types share one namespace), so the reachable outcome is a program
  whose `?` reports `` `?` operand must be a Result or Maybe, got Maybe ``. The rule is
  right and the message is indefensible. Options: have a shadowing declaration inherit
  the kind it shadows, or say what is actually wrong — "`Maybe` here is your own
  declaration, not the prelude's canonical one; mark it `@builtin(Maybe)` or rename it".
  Note the fallback's own comment still reads "which is every program today (there is no
  prelude)"; that premise is what changed.
### 2. Checked arithmetic by default; wraparound explicit

Trap-on-overflow covers all integer arithmetic, `wrapping_*`/`saturating_*` are the lowered
escape hatches, and the value-range pass both diagnoses definite faults (`lyra-E020`–`E023`,
`W011`) and elides the traps it can prove unnecessary. That backlog is clear.

- **[OPEN] `checked_*`** — returns `Maybe<T>`. It was blocked on a prelude; the prelude
  landed 07/30 and generic types 07/29, so it is unblocked. Shares the unresolved
  return-type-from-context problem with #5's narrowing conversions.
- **[IDEA] Type-level overflow policy on a `newtype`** — an overflow behaviour
  (`wrapping`/`saturating`) as a new constraint kind in the existing
  `newtype N = Base where …` grammar, so arithmetic on `N` uses that policy instead of the
  checked default.
  - *Why:* for types whose wrapping is definitional — hash/checksum accumulators, PRNG
    state, ring counters, hardware registers — the per-op `wrapping_*` methods are noise and
    a footgun; one missed call is a spurious trap or a bug.
  - *Why it does not contradict "wraparound explicit":* `newtype` is nominal, so the policy
    is opted into at the boundary (`Wrapping8(x)`, or an annotation) — locality lives at the
    conversion site rather than at every operation.
  - *What justifies it over the existing methods:* a `newtype` already carries a `range`, so
    `saturating` can clamp to the **domain** (`newtype Volume = u8 where range(0..=100),
    saturating`) — something `saturating_add`, which is full-width only, cannot express.
  - *Open decisions:* (a) precedence — an explicit `.wrapping_add()` on a `saturating`
    newtype should override the type default, keeping the escape hatch meaningful;
    (b) mixing — `wrappingVal + plainU8` forces a conversion (nominal), sidestepping "whose
    policy wins"; (c) range-saturating semantics — does every intermediate clamp, or only
    bind/store (`(x+60)+60` for `0..=100`)?
  - *Sequencing:* full-width wrap/saturate is one native op or an `llvm.*.sat` intrinsic;
    arbitrary-range saturation is compare+select after each op. **Wrapping-only** is the
    cheap, unambiguous first slice.

### 5. Lossy conversions must be loud

Widening is settled and narrowing already hard-errors.

- **[DECIDED]** Narrowing gets named methods — `truncate` / `saturate` / `narrow`, not a
  cast keyword. The builtin-method registry to host them exists.
- **[OPEN]** Their return type is the narrower target with no argument, so they need
  context-directed return-type inference (or a turbofish). Same blocker as `checked_*`.

### 8. Consistency cleanups

Settled: keep `data` / `struct` / named `tuple` / anonymous tuple — they sit at different
points on "does this grouping need a name and named fields?", not redundant. Rule of thumb:
sum → `data` (inline record for one-off named payloads; promote to a `struct` when the
payload earns a name); product → `struct` (named) / named `tuple` (positional nominal) /
anonymous tuple (ad hoc). Nothing open.

## Functional / imperative blend

**Model:** purity = no observable effect crossing the function boundary; local mutation of
owned values is fine. `ref`/`mut`/`own` tell the checker whether a mutation escapes. Payoff:
license to memoize, reorder, auto-parallelize.

The original item numbers (#1–#8) are kept on the bullets below, since code comments and
`CLAUDE.md` cite them; a number that no longer appears here belongs to a finished item and
is findable in COMPLETED.md.

Landed: the purity pass and bottom-up purity inference (`lyra-E007`); the three-level
binding ladder `let` / `let mut` / `var`; `ref`/`mut`/`own` parameter modifiers with
`lyra-W010` for an inert one; the `pure` ⊆ `det` ⊆ unannotated ladder plus orthogonal
`noalloc` (`lyra-E015`/`E016`); the use-after-move check for `own` (`lyra-E019`); and the
whole allocation-flavor axis — `stack`/`shared` compatibility (`lyra-E018`), recursive-type
well-formedness (`lyra-E014`), `shared`/dynamic arrays, for-in across arrays/ranges/strings,
interior assignment, and deep retain-on-copy.

- **[OPEN] (#3) Purity inference phase 2 for trait-method clauses.** Lambdas and free functions
  read the collector's `ScopeTable`; method clauses still re-walk the AST, because
  `CollectLambdaClause` records no scope. Needs a collector change reconciled with
  `checkTraitImplMethodBody`.
- **[OPEN] (#4) `ref`/`mut`/`own` outside parameter position**, and driving move/copy/borrow
  semantics from them.
- **[OPEN] (#5) Allocation, remaining pieces:** a `shared` construction in a bare
  argument/return position is not stamped with its flavor (only annotated bindings and
  `shared` payload args are); a *nested* `shared data` sub-pattern — destructuring a tail
  through its own box — errors loudly; dynamic-array **growth** (no grow op exists in the
  language yet); construction-site `shared T {…}` syntax; implicit-alloc / escape analysis;
  atomic refcounts (deferred to the job system).
- **[OPEN] A `weak` field is unconstructible.** A field must be initialized and there is no
  empty weak, so the cycle-breaking use needs `Maybe<weak T>` or a nullable weak. Generics
  are no longer the blocker — but `Maybe<weak Node>` **does not parse**: the grammar will
  not take a `weak` type inside type arguments, so this needs a `tree-sitter-lyra` change
  (push the grammar first, then `lyra`).
- **[DECIDED 07/11] Command-line args are ambient, not a `main` parameter.** `main` stays
  parameter-less always (one uniform entry-point shape); args are read through a builtin
  accessor (`CommandLine.args()`) tagged `EffectInput` — the same ambient-effect pattern as
  `Random.global()` / `wallClock()`, so it composes with the `pure`/`det` ladder for free,
  including for a callee other than `main` that wants args. Matches Rust/Go/Zig/Swift over
  Java/C#. Not implemented; it is a convention recorded to prevent later signature churn.
- **[ROADMAP] (#7) Explicit SIMD** — `simd<T,N>` → LLVM `<N x T>`, for determinism and games.
  Layer 1 is the primitive vector type; layer 2 is a data-parallel map over `pure`/`det`
  component arrays (the auto-parallel payoff). SoA-for-components, distinct from `[N]T`.
  Sequenced after the scalar backend; spec in `pkg/backend/llvm/SIMD.md`.

### Borrow model (#8) — targeted checks, not a Rust borrow checker

**[DECIDED 07/18]** Refcounting already carries memory safety, so the compiler closes only
the holes RC leaves. No lifetime annotations, ever. Use-after-move on `own` (a) is done, and
by-reference `mut`/`ref` parameters (d) landed 07/29.

- **[OPEN] (b) Borrows are second-class.** A `ref`/`mut` value may be read and passed down
  as a borrow, but never stored in a field, captured by an escaping closure, or returned —
  except the blessed **borrow-from-self accessor** (`(self: ref T) -> ref F`, whose result
  is treated as a borrow of the receiver). That is Rust's elision rule #3 as the *only*
  legal borrow-return form, which is what removes the need for lifetime syntax. Ambiguous
  cases (a borrow returned from a multi-`ref`-param function) are rejected: return `shared`
  or restructure.
- **[DEFERRED] (c) Exclusivity (`mut` XOR alias)** until dynamic arrays or the job system
  force it, and scoped there. Interior borrows into resizable containers are the one
  use-after-free RC does not cover. Leaning toward **statement-scoped projections**
  (Hylo/Swift copy-out/write-back subscripts — no holdable element reference, in-place via
  the optimizer) over a static container freeze. Parallel `ref` borrows get their
  no-mutation guarantee from `pure`/`det`, not a new checker.

## Wider integers — `i128`/`u128`

**[DECIDED 07/25; MVP LANDED 07/27]** Add `i128`/`u128`; do **not** add arbitrary-precision
bignums or Zig-style arbitrary fixed widths. These read as one question but are three, with
nothing in common in cost or fit.

- **`i128`/`u128` — yes.** The most on-brand numeric addition: fixed-width and identical on
  every target, exactly the determinism thesis that removed `int`/`uint`. LLVM lowers them
  natively and the checked-overflow model extends by width for free.
- **Arbitrary precision — no, not as a primitive; revisit as a stdlib `BigInt`.** It breaks
  two load-bearing decisions at once: fixed-width primitives, and trap-on-overflow (a bignum
  *grows* instead of trapping, so it cannot be what plain `i64 + i64` does). No systems
  language ships this as a builtin (Rust `num-bigint`, C++ Boost, Go `math/big`). Lyra is
  well placed to do the same later — a bignum is
  just another managed, ref-counted value, so the runtime shape is already solved; what is
  missing is a stdlib to host it, plus a small-int tagged-inline optimization before it is
  fast enough to want.
- **Zig-style arbitrary widths (`i7`, `u3`, `i256`) — skip** unless a concrete use case
  (bit-level wire protocols, packed hardware registers) shows up. Non-power-of-two widths
  force load/store legalization and mask/`sext`/`zext` sequences everywhere, and Lyra's
  primitives are *named constants* (`primitives.go`), not a numeric width field — `iN` would
  mean reworking that representation entirely.

Types, checked arithmetic, division via the builtins library, `match`, conversions and
`print` all landed. One gap remains:

- **[OPEN] >64-bit literals.** `IntegerLiteralExpr.Value` is a Go `int64` and
  `numeric_literals.go` parses via `strconv.ParseInt(…, 64)`, so a true `i128` literal
  cannot be written — a 128-bit constant is reached today via arithmetic or an `i128(x)`
  conversion. Closing it means widening the literal node to a `big.Int` or hi/lo pair, which
  threads through the collector, the printer and golden output, and every `Value`/`Unsigned`
  reader. **And with it:** compile-time folding is `int64`-bound
  (`typechecker/overflow.go`'s `extractIntLiteralValue`), so correct `i128` folding needs
  128-bit constant arithmetic. The value-range pass needs no change — it already widens
  `i128`/`u128` to ⊤, which is sound.

## Traits

Trait-method lowering landed 07/30: an impl method lowers to a function taking the receiver
first, dispatch is static, and a generic impl needs no extra machinery.

- **[OPEN] Trait signatures carry no borrow modifier** — the grammar has nowhere to write
  one — so every parameter including the receiver is by value. If that changes,
  `traitMethodLambda` is the line that must carry it, or the call site and the body will
  disagree about who owns the receiver.
