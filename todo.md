# Lyra — To-Do

What is **open**. Finished work and its reasoning live in [COMPLETED.md](COMPLETED.md); what
the compiler does today is in `CLAUDE.md`.

Tags: **[OPEN]** not started · **[PARTIAL]** landed in part · **[DECIDED]** settled, not
built · **[IDEA]** not committed to · **[ROADMAP]**/**[DEFERRED]** deliberately later.
Entries rot: re-run an open entry's own reproduction before acting on it.

## Known bugs

- **[OPEN] A statement inside an `if` branch sees the enclosing return as its context.** In
  `-> Maybe<i64> => if c { let q = make(); None } else { None }`, `make<t>() -> Maybe<t>`
  solves `t` from the function's return instead of reporting "cannot infer t", as the same
  `let` in a block does.

- **[OPEN] A concrete trait call on a generic impl inside a generic function does not lower.**
  `let g<u> = (b: Box<u>) -> i64 => b.get()` against `impl Get for Box<t>` type-checks, then
  the backend reports `type variable "u" has no concrete type`: a resolution's bindings
  mention the caller's `u` and nothing composes them with the specialization.

## In progress

### Backend — LLVM IR

- **[PARTIAL] Perceus reference counting.** Stages 1–3 (last-use, dup/drop fusion,
  reuse/FBIP on `shared`) are in.
  - Stage 4, reuse specialization (token-conditional dup, static-uniqueness fast path),
    deferred: it saves refcount traffic, not allocations, and carries double-free risk.
  - Hoisting a *conditional* last use; reuse through guards/value-testing payloads; struct
    and tuple reuse.
  - A CFG liveness pass to replace `computeLastUse`'s textual walk (shadowed, reassigned or
    loop-referenced bindings fall back to scope exit). Sound today; settle by measuring a
    loop holding a large `shared` array alive.
- **[PARTIAL] Closure lowering is tiered.** Dev (boxed closures) is in; release = Lambda Set
  Specialization, gated on the monomorphizer. LSS can only loosen `noalloc`'s closure rule,
  never tighten it.

### Traits: a method's own type variables

- **[OPEN] A lambda's array literal takes the context's flavor only for an exact match.**
  Untyped elements (`(x) => [1, 2]` for `[]u8`) and a wrapped literal (`(x) => Some([x])` for
  `Maybe<[]i64>`) still solve the fixed default; see settleArrayLiteralGuess.
- **[OPEN] `Self<…>` beyond an exact match.** Only a target applied to as many distinct
  variables as `Self<…>` has arguments is accepted; `impl Functor for Result<t, e>` (which
  argument?) and a default method writing `Self<…>` are refused until decided.

### Modules and tooling

Package management, versioning and separate compilation are out of scope by decision.

- **[IDEA] A formatter in Lyra (`lyrafmt`) as the self-hosting probe.** Bind libtree-sitter
  over FFI (`TSNode` crosses by value, which `pkg/abi` already supports) and keep the
  grammar as is; the program then needs a tree walk, a byte-buffer string builder,
  `read_file`/`write_file` and `Result` composition — the same shapes a compiler needs, at
  ~2–4k lines. Expected to surface: the generic-impl-in-generic-function bug above,
  `Result.map_err`, `?` not carrying an expected type, `[]T` aliasing in a scope stack, a
  missing `readdir` in `std.io`, and the first compile-time data point for a program larger
  than any example. A real bootstrap would start with the collector after this.


## Language surface

- **[OPEN] Type-namespaced associated functions.** `Rng.seeded(42)` is `lyra-E035`;
  building the feature is a separate decision (`Trait::method` half-exists).
- **[OPEN] Operator overload on a `data` type:** with a `Sub` impl, `Empty - 1` parses as
  `Empty(-1)`. Contrived; if it bites, lint it rather than change the grammar.
- **[IDEA] Warn on `Some - 1` with spaces on both sides**, which reads as subtraction.
- **[IDEA] Admit compound juxtaposed operands** (`Ok f(y)`). Blocked by the
  parameter-position race with destructured lambda parameters; not worth a fragile parse.
  Verify against the corpus, not tree-sitter's "unnecessary conflict" warnings.
- **[IDEA] A `Regex` value type**, literal-constructed and compile-time-compiled like the
  existing DFA positions (`lyra-E052` today). Runtime-built patterns would be a separate
  feature.
- **[IDEA] Producing a raw pointer from an integer** — a separate feature with its own
  safety story.
- **[IDEA] Arenas (`with`)**, refused as `lyra-E050`. If built, the `noalloc` discharge
  needs escape analysis, and a named arena handle mutated from `pure` code needs its own
  purity test.

## Standard library and bindings

- **[IDEA] `Result.map_err`.** `examples/config/config.lyra` is the first program composing
  two error types (`JsonError` into `ConfigError`); it writes a `match` per call site today.
- **[OPEN] No bulk `^u8 → []u8`.** `CBuffer.get(i)` in a loop is the only spelling;
  nothing needs it yet.
- **[OPEN] `@must_release` extensions:** a `newtype` cannot carry the attribute
  (`constrained_type` has no attribute slot); a multi-binding pattern is untracked; `defer`
  would be the natural companion.
- **[OPEN] raylib gaps:** `SetShaderValue*` uniforms (a `const void *` no Lyra pointer
  reaches); `LoadMaterials` (needs pointer reinterpretation to reach `MemFree`);
  `LoadTextureCubemap`; `ExportImageToMemory` (returns size 0).
- **[OPEN] glTF viewer gaps:** morph targets, `KHR_animation_pointer`, crossfading node
  clips; WebP and KTX2/Basis textures; `KHR_materials_variants`, iridescence, sheen,
  specular, IOR, volume; back-to-front sorting of see-through meshes.

## Array comprehensions

- **[OPEN] A generator whose source depends on an earlier one** (`[row in grid, cell in
  row | cell]`). Sources are materialized before the loops to compute capacity; this needs
  materialization inside the enclosing loop.

## Lazy sequences — `gen` and `Seq<t>`

- **[OPEN] Shapes still refused:** a plain function returning a sequence from a block body;
  a lambda literal inside a `gen` used as a value; a `mut` parameter on one; `yield from`
  over an array, string or range.
- **[OPEN] "Generator" means two things** — a comprehension clause and a `gen` function.
  The docs should pick distinct terms.

## Ranges

- **[IDEA] Open-ended expression ranges (`0..`)**, now possible over `Seq`. Patterns and
  constraints already allow open bounds; the expression form deliberately does not yet.

## Pit of Success

- **[OPEN] Declared error conversion for `?`** (From-style), once a conversion trait exists.
  `?` is assignability-only today. Driven by `examples/config/config.lyra` (half 1); `?` knows
  both types, so this needs an impl lookup, not return-type dispatch.
- **[OPEN] `checked_rem`.** A naming decision (`%` vs `%%`), not a lowering one.
- **[IDEA] Overflow policy on a `newtype`** (`where wrapping` / `saturating`), so a hash
  accumulator need not spell `wrapping_*` per op and `saturating` can clamp to a `range`.
  Open: explicit-method precedence, and whether saturation clamps every intermediate or
  only at store. Wrapping-only is the cheap first slice.
- **[DECIDED] Narrowing gets named methods** (`truncate`/`saturate`/`narrow`). **[OPEN]**
  they need return-type-from-context inference or a turbofish.

## Functional / imperative blend

Item numbers (#3–#8) are cited from code comments.

- **[OPEN] Callbacks reached through a struct field, call result or array element** are
  charged `AllEffects`; only parameters and bindings are effect-polymorphic.
- **[OPEN] A declared callback bound is not inferred.** Forwarding an unconstrained
  parameter into a bounded slot is rejected instead of propagating the bound outward.
- **[IDEA] A suppression syntax** (`#[allow]`-like). Becomes a gap once a second
  opinionated lint lands.
- **[IDEA] `det`/`noalloc` missing-bound warnings behind an opt-in flag**; measured too
  noisy to default on.
- **[OPEN] (#3) Purity inference phase 2 for trait-method clauses.** Method clauses re-walk
  the AST because `CollectLambdaClause` records no scope.
- **[OPEN] (#4) `ref`/`mut`/`own` outside parameter position**, driving move/copy/borrow
  semantics.
- **[OPEN] (#5) Allocation, remaining:** a nested `shared data` sub-pattern errors loudly;
  construction-site `shared T {…}` syntax; implicit-allocation escape analysis; atomic
  refcounts (deferred to the job system).
- **[ROADMAP] (#7) Explicit SIMD** — `simd<T,N>` → `<N x T>`, then a data-parallel map over
  `pure`/`det` component arrays. Spec in `pkg/backend/llvm/SIMD.md`; blocked on const
  generics. Write elementwise emission once, parameterized by lane count with 1 legal.
- **[IDEA] Non-copyable types** (`@nocopy`) for handles where a second owner is a bug.
  Under refcounting this must mean "no implicit retain", so only `stack`/unique ownership;
  it implies move-only binding, no outliving capture, and `[v; n]` refused for n > 1.
- **[IDEA] A call-site transfer marker** (`consume(own x)`). `^` is taken; reopen only if
  E019 starts reading as a surprise.
- **[IDEA] `xs.copy()`** — a shallow `[x in self | x]` for `[]t` only, giving W019 and
  aliasing a fix to name. `copy`, not `clone`, since it is not recursive.
- **[IDEA] `[v; n]` evaluates `v` once per slot**, so `[[false; w]; h]` builds fresh rows.
  A `pure noalloc` operand keeps today's lowering; a deliberate alias moves to a
  comprehension; W019 narrows but stays. Measured 08/19: no shipped code uses an effectful
  operand.

### Borrow model (#8) — targeted checks, not a Rust borrow checker

**[DECIDED]** RC carries memory safety; close only the holes it leaves, with no lifetime
annotations.

- **[OPEN] (b) Borrows are second-class**: never stored, captured by an escaping closure or
  returned, except the borrow-from-self accessor `(self: ref T) -> ref F`.
- **[DEFERRED] (c) Exclusivity (`mut` XOR alias)** until the job system or resizable
  interior borrows force it. Leaning toward statement-scoped projections over a static
  container freeze.
- **[OPEN] (e) Observable aliasing.** `var b = a; b[0] = 99` changes `a` silently for `[]T`
  and structs holding one. Warning on every copy is too noisy; copy-on-write
  (`lyra_rc_drop_reuse` is already the uniqueness test) would make `xs[i] = v` allocate
  under `noalloc`, so it needs (c) first. Check whatever lands for (c) against this.

## Wider integers

- **[IDEA] Arbitrary precision as a stdlib `BigInt`**, never a primitive (it breaks
  trap-on-overflow). Zig-style arbitrary widths are skipped absent a concrete use.

## Foreign functions — `extern`

- **[OPEN] Returning a C function pointer, or passing a closure to C.** Both need a way to
  call a bare code address; the `void *` context parameter covers captures meanwhile.
- **[OPEN] `@link` search paths, static archives and macOS frameworks** are the build
  system's problem for now; that is where a manifest would start to earn its keep.
- Non-LP64 targets are unsupported by stated assumption; `CLong`/`CULong` is the grep
  target for a port.

## Const generics — a value as a type parameter

**[OPEN]** `fixed<I, F>`, `simd<T, N>` and any function generic over `[N]T`'s size all need
a compile-time value parameter: `let sum<t, const N: i64> = (xs: [N]t) -> t`. The use-site
`[N]t` already parses; the declaration list, `ast.GenericParam`,
`types.StaticArrayType.Size` (an `int` today) and the instantiation key must change.

- Layer 0: integers only, value parameters over `[N]T` in function signatures, no
  arithmetic on parameters in type position (that is a solver), `N` inferred by
  unification and positional in the turbofish.
- A value parameter takes a constraint, not a trait bound, and can only be checked at
  compile time (waits on CTFE).
- Before building, sweep whether the array helpers that need `noalloc` actually exist.
- **[IDEA]** a named turbofish form `sum::<i64, N = 3>`, purely additive.

## Compile-time function evaluation

**[IDEA]** Today only literal arithmetic and const chains fold. The gate would be the
existing inferred `pure det`, with no new syntax; results must be emittable constants.

- The risk is a second evaluator disagreeing with the backend: overflow (a diagnostic at
  compile time), float width and rounding, and termination (a step budget).
- Ladder: a `const` from a `pure det` call returning a scalar/string → aggregate results →
  computed generic parameters.
- Driver: user constraint predicates, or a library needing to ship a precomputed table.

## Target introspection

- **[IDEA] `size_of::<t>()` / `align_of::<t>()`** (plus stride) — answerable from
  `SizeAndAlign`, needing only a value-argument-less turbofish call shape.
- Target predicates (`is_x86`, `simd_width_of`) wait for real cross-compilation; `lyrac`
  has no notion of a target.

## Constraints are ordinary Lyra

**[IDEA]** A constraint becomes a `pure det` predicate with the value as `self`
(`newtype Lane = i64 where power_of(2)`), and the keywords are rewritten in the prelude:
`range` → `at_least`/`at_most` (retiring E034), `values` → `one_of([...])`, `step` →
`congruent(offset, n)`. `pattern` stays a builtin, declared with `@builtin(Pattern)`.

- Do all of them, or a user's own `step` is silently read as the builtin.
- Phase 1: prelude forms marked `@builtin`, same diagnostics; user predicates checked at
  run time only. Phase 2: CTFE adds their static rung.
- Consider deleting `precision(...)`, which nothing reads. Open: how a predicate supplies
  a good message. Multi-value relationships stay out of scope.

## Traits

- **[OPEN] Fixed-point types — `fixed<I, F>` is refused** (`lyra-E055`). First confirm the
  purpose (binary-scaled determinism vs decimal money, which a newtype serves) and what
  arithmetic does to the parameters; blocked on const generics. Add `fixed_point_type` to
  both highlight query files when built.
- **[OPEN] Return-type-directed dispatch.** `trait Zero { zero: () -> Self }` cannot be
  resolved, so `Zero`/`Default` are unwritable; settle where the expected type comes from,
  the no-context case, and E035's interaction. Driven by `examples/config/config.lyra`
  (half 2, `FromJson`), which first needs the next entry.
- **[OPEN] An expected type does not reach through `?`.** `let v: i64 = make(1)?` with
  `make<t>(…) -> Result<t, e>` cannot infer `t`; without `?` the annotation infers it.
- **[OPEN] Overlapping impls** (`impl Show for Box<t>` beside `Box<i64>`) are not ranked;
  only identical targets are refused (`lyra-E037`).
- **[OPEN] A partial ordering for floats.** A second `PartialOrd`-style type vs a widened
  `Ordering`; deferred until something needs it. A bit-pattern `total_cmp` for sorting
  floats is also unbuilt (`sort_by` with a comparator works meanwhile).
