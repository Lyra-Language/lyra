# Lyra — To-Do

What is **open**. Finished work and its reasoning live in [COMPLETED.md](COMPLETED.md); what
the compiler does today is in `CLAUDE.md`.

Tags: **[OPEN]** not started · **[PARTIAL]** landed in part · **[DECIDED]** settled, not
built · **[IDEA]** not committed to · **[ROADMAP]**/**[DEFERRED]** deliberately later.
Entries rot: re-run an open entry's own reproduction before acting on it.

## In progress

### Backend — LLVM IR

- **[PARTIAL] Perceus reference counting.** Stages 1–3 (last-use, dup/drop fusion,
  reuse/FBIP on `shared`) are in.
  - Stage 4, reuse specialization (token-conditional dup, static-uniqueness fast path),
    deferred: it saves refcount traffic, not allocations, and carries double-free risk.
  - Hoisting a *conditional* last use; reuse through guards/value-testing payloads; struct
    and tuple reuse.
  - A CFG liveness pass was the candidate for shadowed, reassigned and loop-referenced
    bindings. Measured and settled 09/15 (COMPLETED.md): the loop case is closed by a
    loop-exit release; a reassignment already frees at the store; shadowing stays at scope
    exit, with nothing measured needing more.
- **[PARTIAL] Closure lowering is tiered.** Dev (boxed closures) is in; release = Lambda Set
  Specialization. The monomorphizer it was gated on exists. LSS can only loosen `noalloc`'s
  closure rule, never tighten it.
  - **Measured 09/15, before building anything.** For a small higher-order function
    (`xs.map((x) => x * k)` in a hot loop) clang at `-O2` inlines `map`, makes the closure
    call direct, and deletes the environment box: the optimized IR and timing are identical
    to a hand-written comprehension. The win is confined to a higher-order function too big
    to inline: the prelude's `sorted()` on 2M `i64` keeps its comparator indirect (7 indirect
    calls survive in the optimized merge sort) and runs 0.21 s against 0.165 s for a copy
    with `compare` inlined by hand, the code LSS would emit — about 20%. The other thing LSS
    buys is a language guarantee that a non-escaping capturing closure allocates nothing, so
    `sort_by` with a capturing comparator could be written inside `noalloc` (lyra-E016
    today). Costs: a lambda-set type analysis through the typechecker and one copy of each
    higher-order function per lambda set. Verdict: not worth it for speed alone; revisit if
    the `noalloc` refusal starts biting or a self-hosting profile shows indirect-call cost.

### Traits: a method's own type variables

- **[DECIDED] `Self<…>` through a `where` bound stays refused.** `Result<_, e>` (a hole
  marks the position) and default methods writing `Self<…>` landed 09/15; a bound receiver
  `t` has no head to apply, and giving it one is higher-kinded type variables, which nothing
  asks for.

### Modules and tooling

Package management, versioning and separate compilation are out of scope by decision.

- **[PARTIAL] A formatter in Lyra (`lyrafmt`) as the self-hosting probe.** Round trip,
  indentation, whitespace, spacing, inline blocks and **line breaking** (09/19: 90 columns,
  comma lists exploded one element per line) are in; the repo is formatted by it, a
  directory argument is every `.lyra` file beneath it, and both editors format through
  `lyra-lsp`. Breaking covers comma lists, **method chains**, **boolean conditions** and
  **the body after `=>`** (09/21). Left as written: a condition a body follows on the same
  line (`if a || b { … }` — moving the body is a rule about `if`, not about conditions),
  and a `=>` inside a `(`, which belongs to a lambda passed as an argument. What is left after that is the bootstrap proper, which starts
  with the collector. (Dates and reasoning: COMPLETED.md, 09/15–09/19.)


## Language surface

- **[OPEN] W012's message is wrong for a real shadow's neighbour.** It says "this
  declaration wins", which is true for a shadow and was printed for an overload until
  09/21; the overload case is now skipped, but the wording still promises a winner where
  the reader may have meant one. It could name what it shadows and what to do about it.
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

- **[PARTIAL] `lyra-md`, a Markdown renderer, and `site`, a directory builder, as the
  standard library's forcing functions.**
  The block level (headings, fenced code, paragraphs, escaping) renders and is pinned by
  `cmd/lyrac/lyra_md_example_test.go`, and **inline spans** (code, emphasis, links,
  images, backslash escapes) landed 09/17 beside it. The block level added `lines`,
  `strip_prefix` and `strip_suffix` to the prelude; **spans added nothing**, and the
  string builder and `replace` this entry predicted were both unnecessary (COMPLETED.md).
  **The site pass landed 09/21** and forced exactly what this entry predicted: `std.path`
  (`child`, `parent`, `base`, `stem`, `extension`, `with_extension`, `normalize`),
  `last_index` in the prelude, `std.io.create_dir_all`, and a `Set` for the link check.
  **Assets landed 09/21** — everything that is not a page is copied where it stands, byte
  for byte through `std.io.copy_file`. Next for it: a link to a *directory* (`../guide/`)
  is reported as broken rather than resolved to its index, and a page's mode is not copied
  with it (`stat` is a struct this module cannot write).
  - The ~90× slowdown this entry recorded on 09/17 was **`split` being quadratic**, not
    anything in the renderer; profiled, fixed the same day, and the renderer went from
    1081 ms to 18 ms over 200 KB (COMPLETED.md).
  - **Measuring here has a trap worth writing down**: a freshly built binary's first run
    costs ~270 ms on this machine before `main` starts, which is most of a small
    measurement. Warm the binary and take the best of several runs, or the numbers are
    about the loader. Localizing by reasoning has a worse one — the scanner looked guilty
    and was innocent; `sample` named the real culprit in one run.

- **[PARTIAL] `std.temporal` and `examples/calendar`**, after the web's Temporal API. The
  month grid and then **events** landed 09/18: `PlainDate`, `PlainTime`, `PlainDateTime`,
  `Duration` with ISO parsing, and `today()`/`now_plain_date_time()` through the local
  offset, in a TUI reading an ISO 8601 events file; `PlainTime` and `Duration` arithmetic
  09/19. Next, if the calendar grows: a default events file (which forces environment
  variables — `std` has none) and editing.
  **Zone rules landed 09/22** (slice one): `TimeZone` over the system's IANA database, with
  the offset, abbreviation and daylight-ness at an instant, checked against Go's `time` at
  ±1s around every transition 1965–2035 in nine zones. Next: `Instant` and `ZonedDateTime`,
  then the **disambiguation** rules (a local time that does not exist, or happens twice),
  then the POSIX footer rule for instants past the table's end (~2037). The app meant to
  drive those is a recurring-schedule tool, which cannot be written without them.

- **[OPEN] `std` cannot read an environment variable.** `std.temporal` hardcodes
  `/usr/share/zoneinfo` because there is no way to consult `TZDIR`, and the calendar still
  has no way to find `$HOME`. One `getenv` extern and a `std.env` would answer both.
- **[IDEA] Big-endian byte reads want a home.** The TZif parser has private `read_int` over
  `[]u8`; a second binary format would copy it. `std.bytes` when there is one.
- **[OPEN] A `mut` parameter that is not the receiver is not an effect.** A function
  writing through one is inferred `pure`, and accepts the annotation: `EffectMut` covers a
  mutated *receiver* only. Found writing `examples/lyra-md/site.lyra`, whose link checker
  took a `mut []string` of reports and was told it had no observable effect; it now returns
  them instead. Either the class widens to any `mut` parameter, or the rule is written down
  as deliberate — a caller cannot tell the two apart today.
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

## Ranges

- **[IDEA] Open-ended expression ranges (`0..`)**, now possible over `Seq`. Patterns and
  constraints already allow open bounds; the expression form deliberately does not yet.

## Pit of Success

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
- **[OPEN] Impurity of an imported function.** `pkg/analyzer/checker/README.md` calls this
  open and points here for it; it had never been written down — the third README→`todo.md`
  reference found dangling on 09/16, after the typechecker's tuple/array narrowing marker
  and its generic-type bound. Scope it before building: what a caller in another module can
  know about an imported function's effects, and whether the answer is inference across the
  merged program or a declared bound at the boundary.
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

## Bounded stack buffers

**[IDEA]** A buffer with a **compile-time capacity and a run-time length** — `ArrayVec`'s
shape — so a `noalloc` function can build up a sequence whose size it does not know until it
runs.

The gap: `#[0; 4]` is a stack value and passes `noalloc`, while `[0; n]` is a heap box and
does not (`lyra-E016`, "a repeat literal builds a `[]T`"). A `noalloc` function that needs a
scratch buffer sized at run time has nowhere to go today.

- **The concrete case already works**, which is why this is an entry about generality rather
  than about a missing capability. A struct over a fixed array with a length beside it —
  `struct Buf { data: [8]i64, len: i64 }` — pushed through `mut self`, compiles and runs
  under `noalloc`. (`pure` is correctly refused: the write escapes to the caller, `lyra-E007`.)
- What is missing is **genericity over the capacity and the element type**: one `Buf<t, N>`
  instead of a hand-written struct per size. That waits on const generics above, and is the
  only reason this is not simply a library.
- Open: standard library or language form. Once `N` is a parameter a struct in
  `std.collections` needs nothing further from the compiler, and a language form would buy
  syntax and a name rather than a capability — so the library is the default answer unless
  something turns up that only the compiler can do.
- Open: whether the bound check folds. `push` tests `len >= N` every call, where a caller
  filling it in a counted loop often cannot overflow; the value-range pass already elides
  array bounds checks it can prove, and this is the same question asked of a field.
- **Not a VLA**, and not a runtime-sized `[N]T`. A size known only at run time is not a
  property of the array but of its type — `types.StaticArrayType.Size` is an `int`, and
  layout, ABI classification and instantiation keys all read it — so that is a dependent
  type rather than a relaxed check. The `alloca` spelling that would fit `noalloc` needs a
  sound escape analysis this language does not have (see Allocation, #5), grows the frame
  per iteration in a loop unless the backend brackets every scope with
  stacksave/stackrestore, and fails by moving the stack pointer past the guard page — a
  wrong answer that does not trap, on data rather than on a coding mistake, which is the one
  outcome the language is built to refuse. A capacity in the type avoids all three: it
  cannot escape any way a `[N]T` cannot, a loop reuses one slot, and overflow is a length
  test that traps.

## Compile-time function evaluation

**[IDEA]** Nothing here evaluates anything, and that is still true after 09/15. What a
`const` accepts has widened three times — conversions, struct literals, and the **float
builtins** (`const PHI = (1 + 5.0.sqrt()) / 2`) — but the walk behind it is structural, and
a `const` is inlined as its value *expression* at every use site and lowered like any other
code. The number comes from the optimizer, not from the compiler. The gate for real
evaluation would be the existing inferred `pure det`, with no new syntax; results must be
emittable constants.

- The risk is a second evaluator disagreeing with the backend: overflow (a diagnostic at
  compile time), float width and rounding, and termination (a step budget).
- **Float rounding now has a measured answer, and it is a split rather than a risk.**
  IEEE 754 *requires* `sqrt` correctly rounded, so every implementation agrees bit for bit;
  no mainstream libm is correctly rounded for `exp`, `log`, `sin` or `pow`. LLVM folds a
  libm call by calling the **host's** libm, so a folded `exp` is the build machine's answer
  and a cross-compile can differ in the last place. An evaluator therefore has to decide
  what it is matching — the host, the target, or a correctly-rounded ideal that agrees with
  neither — and that choice is the design, not a detail of it. `std/math/constants.lyra`
  takes the conservative half today: `SQRT_2` and `PHI` are derived, `E` and the logarithms
  are literals, and the line between them is exactly `sqrt`.
- **The `-O0` gap is the case for building this.** At `-O2` the calls fold and the constant
  costs nothing; at `-O0` nothing folds, and since a `const` is inlined at every *use*, one
  in a loop body is a libm call per iteration. Relying on the optimizer is not the same
  promise as evaluating, and only the second survives `-O0`.
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
- **[OPEN] Overlapping impls** (`impl Show for Box<t>` beside `Box<i64>`) are not ranked;
  only identical targets are refused (`lyra-E037`).
- **[OPEN] A partial ordering for floats.** A second `PartialOrd`-style type vs a widened
  `Ordering`; deferred until something needs it. A bit-pattern `total_cmp` for sorting
  floats is also unbuilt (`sort_by` with a comparator works meanwhile).
