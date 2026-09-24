# Lyra — To-Do

What is **open**. Finished work and its reasoning live in [COMPLETED.md](COMPLETED.md); what
the compiler does today is in `CLAUDE.md`.

Tags: **[OPEN]** not started · **[PARTIAL]** landed in part · **[DECIDED]** settled, not
built · **[IDEA]** not committed to · **[ROADMAP]**/**[DEFERRED]** deliberately later.

**Entries rot: re-run an open entry's own reproduction before acting on it.** Reconciled
against the compiler on **09/22**, which is the date to trust an unedited entry from. That
pass found one entry describing the *opposite* of the behaviour — it claimed a callback
through an array element was charged conservatively when it was not charged at all, which
was a hole in `pure` that the entry's wording had been hiding — three entries describing
work already finished, and one using a word the codebase had renamed. This line is a rule,
not a formality.

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
  and a `=>` inside a `(`, which belongs to a lambda passed as an argument.
  (Dates and reasoning: COMPLETED.md, 09/15–09/19.)

- **[PARTIAL] The collector in Lyra — the bootstrap proper.** The Go collector is ~7.8k
  lines over a ~5.6k-line AST, so this is sliced rather than attempted.
  - **Slice 1 landed 09/22: the CST accessors a collector needs**, which are not the ones a
    formatter needs. `lyrafmt` walks to leaves by index and never asks what a child *is*;
    a collector asks almost nothing else. `bindings/treesitter` gained `field` (a `Maybe`,
    so C's null node cannot be used as one — hazard 2 made unrepresentable),
    `named_child`/`named_child_count`, `is_missing`, and `text(node, bytes)` taking the
    source's **bytes**, since every offset tree-sitter reports counts bytes while a Lyra
    `string` slices by rune.
  - **Slice 2 landed 09/22: the AST in Lyra (`examples/collector/ast.lyra`) and a printer
    matching `pkg/printer`'s format** (`print.lyra`), for the subset the smallest goldens
    use — `VarDeclStmt` over integer, character, string and lambda literals. Six goldens
    match byte for byte from hand-built trees, there being no collector yet, which pins the
    *format* before slice 3 has to debug a walk at the same time. The AST mirrors the Go
    type and field names, so the **245 goldens are the oracle**. Lyra has no reflection, so
    where Go walks fields generically the printer names them one at a time: alphabetical
    order and the two omission rules are written per node, and a field added to the AST and
    forgotten there shows up as a golden that stops matching.
  - **Its modules are siblings** (`ast`, `printer`, `collect`), as every example's are.
    They were dotted paths for a few hours so a test could import them from a temp
    directory, which resolved only when the module root was the repo root — the editor's
    is `build/`, so Zed showed 11 errors on files the command line called clean.
    `TestRepo_EveryLyraFileChecksUnderBothRoots` is the guard.
  - **Slice 3 landed 09/22: the CST→AST walk** (`collect.lyra`) and `collector.lyra`, which
    prints a file's AST in the goldens' format. **13 of the 238 goldens whose sources can
    be recovered now match from source**, hand-built trees gone. A kind outside the subset
    is skipped rather than guessed, so growth fails by printing too little — visible in a
    diff — rather than by inventing a node. Grow it by making more goldens match; the next
    ones want `\x`/`\u` escapes, lambda parameters and bodies, and the other statement
    kinds.
  - **Slice 4 landed 09/23: the expression tree.** `IdentifierExpr`, `MathBinaryOpExpr`,
    `ExpressionStmt`, and a lambda's real shape — `Parameter` with `IdentifierPattern` and
    `PrimitiveType`, and `ReturnType`. **16 of 238 goldens** match from source, up from 13;
    the count moves slowly because most of the rest want `BlockExpr` and `FunctionCallExpr`,
    but the recursive tree behind them is now in place. Every field holding an expression is
    `shared`, which is the only way a tree is finitely sized — and writing it found two
    compiler bugs and tripped over a third (above).
  - **Slice 5 landed 09/23: calls, blocks and the postfix family.** `FunctionCallExpr`,
    `BlockExpr`, `MemberExpr`, `IndexExpr`, `TupleIndexExpr`. **20 of 238** goldens match,
    up from 16 — and the first two of those five scored *nothing* on their own, which is
    the thing to read correctly: they left **92 goldens blocked by exactly one missing
    node**, and the postfix family cashed four of them in. The next cheap ones are
    `TryExpr`, `RangeExpr` and `NegationExpr`; the expensive ones by count are
    `TypeDeclStmt` (29), `BooleanBinaryOpExpr` (27) and `DestructuringDeclStmt` (24).
  - **The Go collector is now an oracle directly**, not only through its stored goldens
    (`TestCollector_AgreesWithTheGoCollector`): both collectors run on the same source and
    their printed ASTs are compared. The goldens are a curated set, and a slice can add a
    construct none of them exercises *in isolation* — which is exactly what calls and
    blocks were.
  - **Slice 6 landed 09/23: `BooleanBinaryOpExpr`, `NegationExpr`, `TryExpr`, `RangeExpr`.**
    **33 of 238**, up from 20 — the payoff slice 5's measurement predicted, since these are
    small nodes sitting on the tree it built. **85 goldens are still blocked by exactly one
    node**, so the next slice should score similarly. By count the remaining ones are
    `TypeDeclStmt` (29), `DestructuringDeclStmt` (24), `InterpolatedStringExpr` (22), then
    the written-type kinds — `GenericType`, `UnresolvedType`, `ParameterType` (14 each),
    which travel together and are one slice rather than three.
  - **Slice 7 landed 09/24: type declarations.** `TypeDeclStmt` over the three forms
    (`NamedStructType`, `TupleType`, `DataType`) with generic parameters, plus the written
    types they hold — `UnresolvedType` and `GenericType`. **41 of 238**, up from 33. The
    tail of this domain is a sub-domain of its own: `ConstrainedType` blocks 11 of the
    remaining declaration goldens and brings the newtype constraint expressions with it.
    Bigger wins elsewhere: `DestructuringDeclStmt` (24) and `InterpolatedStringExpr` (22).
  - **Slice 8 landed 09/24: destructuring, interpolation and newtypes.**
    `DestructuringDeclStmt` with `DataPattern`, `InterpolatedStringExpr`, `ConstrainedType`.
    **50 of 238**, up from 41. Still uncollected and worth naming rather than discovering:
    a newtype's `where` constraints, and the pattern kinds beyond an identifier and a data
    pattern — **tuple, array and struct patterns**, which is what stops
    `let Some (Ok v) = m` from agreeing with the Go collector.
  - **The harvest, 09/24: 50 goldens to 75, with no new node kind.** Measuring which
    goldens use only *kinds* already built found 78 against a test listing 50 — the other
    28 were blocked by a **field** on a node already collected, which the kind-by-kind
    count cannot see. Optional chaining and const names (separate CST kinds, not flags),
    `pure`/`async` (on the declaration in the juxtaposed spelling, on the lambda in the
    other), a parameter's default, a declaration's generic parameters and its written type
    annotation, the numeric escapes and raw strings. Three needed no code: the golden was
    hand-edited and the whitespace-insensitive comparison in
    `pkg/analyzer/collector/tests` had been absorbing it — it compares bytes now.
    Two more came from the differential test: `bool`'s AST name is `boolean`, and a
    `ReturnType`'s label renders a tuple's elements rather than its `Name` field.
    (COMPLETED.md, 09/24.)
  - **The literal family landed 09/24: 75 goldens to 99**, the best-scoring slice so far
    and the one the harvest's measurement predicted. `FloatLiteralExpr`,
    `StringConcatExpr`, `TupleLiteralExpr`, `ArrayLiteralExpr`, `ArrayRepeatExpr` — five
    small nodes on the tree slices 4 to 6 built, which is the shape that pays.
    A float travels as a **number**: Lyra's formatting is Go's `%v` (both shortest
    round-trip), and the parse is exact in the range every written literal lives in
    (Clinger's fast path). Past it the last digits can differ, and a mantissa beyond an
    i64 used to *trap* — one bad literal took the whole file down rather than collecting
    imprecisely, the compiler's own hazard 3 — so digits past capacity now move into the
    exponent. A literal too large for an `f64` collects as zero, agreeing with the Go
    collector's placeholder on the tree though not yet on the diagnostic.
    (COMPLETED.md, 09/24.)
  - **The second harvest pass, 09/24: 99 to 101, and the field-level gap is closed.**
    Run because the first pass's lesson is that a finished node still hides fields. Two
    goldens used only kinds already built and still differed: a `const` is the same
    declaration under another node kind (`const_declaration`, whose `keyword` field
    carries the word), and a **call** can carry type arguments at its callee
    (`map::<i64, i64>(…)`) — the field `TupleLiteral` already had. **Every golden whose
    kinds are collected and whose source can be recovered now matches**: 104 use only
    supported kinds, 101 match, and the three-golden difference is the orphans below.
    So the next slice is a *kind* slice again, with nothing field-level hiding under it.
  - **Struct instances landed 09/24: 101 to 111** — every stored golden that builds a
    struct. Four shapes behind one node (named fields, the positional shorthand, a record
    update, and the anonymous literal, which is a second *node kind* over the same
    fields), plus `SpreadExpr`, met first as a field's value. Two names worth knowing:
    the field is `GenericArgs` here where a call and a tuple literal spell it
    `GenericArguments`, and the instance's field node prints as `StructField` — the same
    name a *declaration's* field prints under, with different fields. (COMPLETED.md.)
  - **What blocks the rest, measured rather than guessed.** Sole blockers, by goldens:
    `ForInLoopExpr` 5, `IfExpr` 5, `TuplePattern` 4, `ArrayPattern` 3, `RegexLiteralExpr`
    3, then a tail of twos. Travelling together: `ArrayCompExpr` with `Generator` (4),
    `ForLoopExpr` with `MathAssignOpExpr` (4). **114 use only supported kinds against 111
    matching**, and the three are the orphans below — so nothing field-level is hiding.
    What is left is the structural slice: control flow (`IfExpr`, `MatchExpr`, the two
    loops) and the pattern kinds it travels with.
  - **Three goldens have no test.** `if_then_expr`, `if_then_expr_multiple_lines` and
    `if_then_expr_with_else_if` are referenced from nothing and record an `if/then/end`
    syntax the grammar no longer has — their contents are the identifiers `else` and `end`
    collected as statements. Delete them with whatever next touches that directory.
  - **The `Location` every node carries is not modelled yet**, and the goldens do not show
    it (`pkg/printer` omits `print:"-"` fields, which is what `NameLocation` is). It has to
    arrive before anything reports a diagnostic, and choosing when is a slice of its own.
  - The `SymbolTable` is a second axis, deliberately after the AST: the Go collector builds
    both in one walk, and doing the same here before the AST is checked would mean two
    unverified things at once.

- **[DECIDED] A method call does not require the method's name in the import; the list
  breaks ties instead.** Gating was measured against this repo — 44 call sites, 13 files,
  mostly accessors on types with no public fields — and rejected: it taxes what the lack of
  field privacy forces and drags names like `day` into the bare scope. (COMPLETED.md, 09/22.)


### Recursive types — two bugs the bootstrap's AST walked into (09/22)

An expression tree is the first program here that needs a type containing itself, and
neither way of writing an **optional child** works. `shared` on a plain field is fine
(`struct BinOp { left: shared Expr }` builds and runs); the hole is `Maybe` over one.

- **[FIXED 09/23] A cycle through a generic type's argument was not caught by `lyra-E014`,
  and crashed the compiler.** `struct Lambda { body: Maybe<Expr> }` where `Expr` holds a
  `Lambda` is accepted at declaration and stack-overflows as soon as a value is built:
  `ownership.ownsManaged` → `eachComponent` → `resolveNamedType`, forever. E014 catches
  every direct shape (struct→struct, data→struct→data), and `ownership.go`'s own comment
  states the invariant it is relying on — "a recursive type's cycle must pass through a
  `shared` field (lyra-E014), which is managed, so the recursion returns". The cycle
  through `Maybe<…>` slips past, so the pass runs on a type it assumes cannot exist. A
  cycle through `[]Expr` is *correctly* accepted: an array is boxed, so the type is
  finitely sized. **Fixed:** `collectByValueNames` walks a parameterized type's arguments
  (its closing comment had listed the kind among those "bounded by construction"), and the
  driver skips the ownership passes when the program already has errors — they feed only
  the backend, which never runs on one, and the crash had been swallowing the diagnostic
  that was already in hand. (COMPLETED.md, 09/23.)

  ```lyra
  data Expr = Lit(i64) | Lam(Lambda)
  struct Lambda { body: Maybe<Expr> }
  let main = () -> u8 => {
    let e = Lam(Lambda { body: Some(Lit(1)) })
    0
  }
  ```

- **[FIXED 09/23] A `shared` value passed to a plain parameter was not checked, and the
  callee misread it.** `describe(b.left)` where `left: shared Expr` and `describe` takes
  `(e: Expr)` compiles, then traps at runtime (`match not exhaustive`): the callee reads
  the pointer as an inline value. `lyra-E018` covers this crossing at annotated init,
  reassignment, interior writes, **`own` arguments** and non-borrow returns — a *borrowed*
  argument is the gap, and it is the common spelling. Silent wrong behaviour rather than a
  loud error, so it is the worse kind. Pre-existing (reproduced with a plain `shared`
  struct field, no `Maybe` involved), found 09/23 beside the two below.

- **[FIXED 09/23] `Maybe<shared T>` did not lower.** `aggregate element type mismatch: cannot
  store %Expr into { i64, i64, %Expr }*`, as a struct field and as a data payload alike.
  The backend refusing loudly is rule 5 working, but it leaves an optional child with **no
  spelling at all**: `Maybe<Expr>` crashes the front end and `Maybe<shared Expr>` stops at
  the backend, while `[]Expr` and `[]shared Expr` both build and run. Blocks the
  bootstrap's slice 4, whose `Lambda` needs an optional body and return type. **Fixed:**
  `stampPayloadAllocation` — a context's *allocation* now reaches a construction's payload
  even where its *type* is settled, which the type-directed push declines by design.

## Language surface

- **[DONE 09/23] A match arm names alternatives with `|`** (`1 | 2`, `"get" | "post"`),
  over literals and ranges. A binding alternative stays refused, deliberately — see
  LANGUAGE.md § patterns. (COMPLETED.md, 09/23.)
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
  (`join`, `parent`, `base`, `stem`, `extension`, `with_extension`, `normalize`),
  `last_index` in the prelude, `std.io.create_dir_all`, and a `Set` for the link check.
  **Assets landed 09/21** — everything that is not a page is copied where it stands, byte
  for byte through `std.io.copy_file`. **Directory links and the executable bit landed
  09/22**: `../guide/` resolves to that directory's `index.md` and is reported when there
  is none (it was silently *skipped* before, not reported broken as this entry claimed),
  an author's own `index.md` now survives the generated listing, and `copy_file` carries
  the executable bit through `std.io.is_executable`. The rest of the mode still does not
  travel: reading it means `stat`, whose `st_mode` offset differs by platform and
  architecture, so it wants a builtin like `dir_names` rather than a wider guess in
  `std.io`.
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
  09/19. The **default events file landed 09/22** with `std.env` under it
  (`XDG_CONFIG_HOME`, then `$HOME/.config`), so what is left of that note is editing.
  **Zone rules landed 09/22** (slice one): `TimeZone` over the system's IANA database, with
  the offset, abbreviation and daylight-ness at an instant, checked against Go's `time` at
  ±1s around every transition 1965–2035 in nine zones. **`Instant` and `ZonedDateTime`
  landed 09/22** (slice two), and the **disambiguation** rules and DST-aware arithmetic the
  same day (slice three), checked against Python's `zoneinfo` at every transition of six
  zones. Next: the POSIX footer rule for instants past the table's end (~2037), and then
  the recurring-schedule tool the whole thing was for. **That tool landed 09/22**
  (`examples/schedule/when.lyra`) and the design held: it needed nothing that was not
  already there. **The POSIX footer landed the same day**, so an instant past the table's
  end (~2037) follows the zone's stated rule rather than freezing at the last offset —
  checked against Go at every transition to 2100. `parse_zoned_date_time` and
  `parse_instant` close the round trip 09/22 — what `Show` writes is now readable back.
  What is left for zones: leap seconds, whose records are skipped. **Custom formatting and parsing are deliberately absent**, as they are from
  Temporal, which leaves them to `Intl`: a pattern language wants a program driving it, and
  the one that would is a log analyser.

- **[IDEA] Big-endian byte reads want a home.** The TZif parser has private `read_int` over
  `[]u8`; a second binary format would copy it. `std.bytes` when there is one.
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

- **[OPEN] A comprehension clause whose source depends on an earlier one** (`[row in grid,
  cell in row | cell]`). Sources are materialized before the loops to compute capacity;
  this needs materialization inside the enclosing loop. Refused loudly, with the
  comprehension-over-a-comprehension workaround in the message (confirmed 09/22). Called a
  *clause* here because a bare "generator" names a `gen` function — the 09/17 convention
  CLAUDE.md states and the backend's own message already follows.

## Lazy sequences — `gen` and `Seq<t>`

- **[OPEN] Shapes still refused:** a plain function returning a sequence from a block body;
  a lambda literal inside a `gen` used as a value; a `mut` parameter on one; `yield from`
  over an array, string or range.

## Ranges

- **[IDEA] Open-ended expression ranges (`0..`)**, now possible over `Seq`. Patterns and
  constraints already allow open bounds; the expression form deliberately does not yet.

- **[DONE 09/23] A range pattern is written in its scrutinee's units.** `'0'..<='9'` on a
  rune; numeric bounds there are refused. (COMPLETED.md, 09/23.)

## Pit of Success

- **[IDEA] Overflow policy on a `newtype`** (`where wrapping` / `saturating`), so a hash
  accumulator need not spell `wrapping_*` per op and `saturating` can clamp to a `range`.
  Open: explicit-method precedence, and whether saturation clamps every intermediate or
  only at store. Wrapping-only is the cheap first slice.
- **[DECIDED] Narrowing gets named methods** (`truncate`/`saturate`/`narrow`). **[OPEN]**
  they need return-type-from-context inference or a turbofish.

## Functional / imperative blend

Item numbers (#3–#8) are cited from code comments.

- **[PARTIAL] Only parameters and bindings are effect-polymorphic.** A callback reached
  through a **struct field** is charged `AllEffects` — conservative, so a *pure* callback
  in a field is refused too, which is the remaining gap. Reaching one through an array
  element, another call's result, or a lambda literal in call position was charged
  **nothing** until 09/22, which was a hole in `pure` rather than conservatism: the effect
  ran. Now charged (a literal by its own body, the rest `AllEffects`). This entry used to
  claim all three were already `AllEffects`, which is how the hole survived being read.
  (COMPLETED.md, 09/22.)
- **[OPEN] A declared callback bound is not inferred.** Forwarding an unconstrained
  parameter into a bounded slot is rejected instead of propagating the bound outward.
- **[IDEA] A suppression syntax** (`#[allow]`-like). Becomes a gap once a second
  opinionated lint lands.
- **[IDEA] `det`/`noalloc` missing-bound warnings behind an opt-in flag**; measured too
  noisy to default on.
- **[OPEN] (#3) Purity inference phase 2 for trait-method clauses.** Method clauses re-walk
  the AST because `CollectLambdaClause` records no scope.
- **[PARTIAL] Impurity of an imported function.** Scoped and mostly answered 09/22: the
  answer is **both** — inference across the merged program for free functions, and a
  declared bound at the boundary for trait methods, which a bound call now believes instead
  of joining over every impl. (COMPLETED.md, 09/22.)
- **[DONE 09/23] W018 on a trait method advises the trait.** An impl's own bound says
  nothing about its siblings, so only the trait's is what a `where t: Trait` call can rely
  on — and an impl inherits it, so the trait is the one action that covers every impl.
  (COMPLETED.md, 09/23.)
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
- **[DONE 09/23] Overlapping impls are ranked.** A more specific target wins by
  subsumption; incomparable targets stay ambiguous and ranking never reaches across traits.
  Identical targets are still `lyra-E037`. (COMPLETED.md, 09/23.)
- **[OPEN] A partial ordering for floats.** A second `PartialOrd`-style type vs a widened
  `Ordering`; deferred until something needs it. A bit-pattern `total_cmp` for sorting
  floats is also unbuilt (`sort_by` with a comparator works meanwhile).
