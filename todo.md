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
  (Dates and reasoning: COMPLETED.md, 09/15–09/19.) **Worse than left as written**: in
  `if flag(a, "x") { return Skipped("…long…") }` over budget, the comma-list rule claims
  the *call's* arguments and explodes `flag(` one argument per line (seen 09/27, gen.lyra).
  The same happens to `return if c { f(a, b, c) } else { g }` over budget: the call inside
  the `if` is exploded rather than the `if` being broken (seen 09/28, Sheliak's arith.lyra).
  A long **array of short literals** explodes one element per line (17 numbers became 19
  lines, 09/28, Sheliak's beam.lyra); filling rows, as a hand-wrapped array keeps, reads better.
  A **record-update literal** over budget explodes after the base's `|` but keeps the first
  field on the base's line — `Z80Cpu {⏎ new_z80() | pc: 0x1000,⏎ sp: …` (09/28, Sheliak's
  z80scenarios.lyra); the base belongs on a line of its own, or the fields together.
  A **`for` whose one-line body is over budget** is broken at its binders' comma —
  `for i,⏎ inputs in prefs.gamepads { … }` (10/06, Sheliak's prefs.lyra); the body belongs
  on its own lines.
  **It also collapses deliberate spacing** in a table (`7, 7,  7, 7`, grouping rows), which
  is fine, but then a table written as rows of 8 in one line of 24 reads as noise.

- **A block beginning with a name `data` does not parse.** `if c { data.len() } else { 0 }`
  is a syntax error, `data` being taken for a `data` declaration; the same name works
  anywhere else, including later in a block. Found writing `std.rar` (09/29), whose
  parameters are named `data` throughout.


- **A large struct passed bare is a silent copy.** Sheliak's `pure (bus: GenesisBus, …)`
  helpers copied a multi-kilobyte bus on every memory read — two thirds of its frame time,
  found only by profiling (09/28); `ref` fixed it. A warning for a struct over some size
  passed to a bare parameter the callee never moves or mutates — "take it by `ref`" — would
  have said so at the declaration.

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
  - **Control flow landed 09/24: 111 to 163**, the largest single move. `IfExpr`, the two
    loops with `MathAssignOpExpr`, `VarReassignmentStmt`, `BreakStmt`/`ContinueStmt`,
    `MatchExpr` with `MatchArm` and `GuardExpr` — and **the pattern kinds**, which are
    most of the score: a destructuring `let` and a match arm take the same node, so
    collecting one collected the other. Literal, wildcard, range, tuple, array, or,
    struct (four shapes of field), rest and binding patterns.
    Two things worth keeping: **`break x` records `x` as a label, not a value** (the
    grammar reads a bare name that way), and a **character pattern prints as a quoted
    rune** where every other literal pattern prints its source text — Go's field is `any`
    and a rune goes in as a type whose `String()` the printer finds. Exact for ASCII;
    a non-printable non-ASCII rune would differ, which the code states. (COMPLETED.md.)
  - **What blocks the rest is now a long flat tail**, which is the shape to expect from
    here: **166 goldens use only supported kinds against 163 matching**, the three being
    the orphans below, and no remaining node blocks more than 3. Sole blockers:
    `LambdaClause` 3, `RegexLiteralExpr` 3, then a run of twos — `AnonymousStructType`,
    `LiteralUnionConstraint`, `IfDestructuringStmt`, `AddressOfExpr`, `ComposeExpr`,
    `SizeofExpr`, `VoidType` — and singles after that. `ArrayCompExpr` with `Generator`
    is the largest pair at 4. A slice from here is a themed handful rather than one node.
  - **Three goldens have no test.** `if_then_expr`, `if_then_expr_multiple_lines` and
    `if_then_expr_with_else_if` are referenced from nothing and record an `if/then/end`
    syntax the grammar no longer has — their contents are the identifiers `else` and `end`
    collected as statements. Delete them with whatever next touches that directory.
  - **Locations landed 09/25, and the slice was mostly its oracle.** The goldens cannot
    check a span — `pkg/printer` skips the embedded `AstBase` it lives in — so the first
    thing built was `PrintASTWithLocations` on the Go side and a `--locations` mode here,
    and then **163 of 163** goldens agree node for node. Every payload struct carries a
    `Location` with a zero default; the collector builds a line table once per file
    (`Source`) and finds a byte offset's line by binary search, since scanning per node is
    quadratic in a file this will eventually be run on.
    Three spans are **not** the node's own and had to be learned from the oracle: an
    interpolation's text run and a raw string both take the whole literal's span, and a
    written *type* carries none at all — a type is compared structurally, so the Go
    collector keeps written types' positions in a side table instead.
    It also found a **missing** location in the Go collector (below), which is the only way
    a missing one ever shows up. (COMPLETED.md, 09/25.)
  - **[PARTIAL] Next on this axis: diagnostics.** The first is in (10/07): `lyra-E088`
    from a CST walk (`refused_arguments`), printed by `--diagnostics` and checked against
    the Go collector's list. The rest need a channel inside the collect walk, which an
    immutable `source` refuses — so it is `mut` threading or a walk per rule.
  - The `SymbolTable` is a second axis, deliberately after the AST: the Go collector builds
    both in one walk, and doing the same here before the AST is checked would mean two
    unverified things at once.

- **[PARTIAL] Hot reload that keeps the program's state — `lyrac run --live` (10/20).**
  Built on macOS ([pkg/backend/LIVE.md](pkg/backend/LIVE.md)), with function values through
  slots and new top-level `let`s initialized on apply (step 4, 10/21); next: Linux through
  `./asan.sh` (step 5), whole static archives in generation 0, then Vega on it.
- **[DECIDED, not built] Qualified type names — `ast.Program` in a type position.**

  **The gap.** A plain `import lib` binds a namespace that works for values and not for
  types: `nums.twice(1)` resolves, `(p: nums.Pt)` is a *syntax* error before it is anything
  else. So a module whose surface is mostly types has no namespace form, and every name has
  to be listed — `examples/collector/collect.lyra` imports **60** names from `ast`. UFCS
  already took the pressure off *functions* (that file's treesitter import is down to
  `{ Node }`); types are what is left.

  This is the alternative to a wildcard import, and the reason to prefer it: a wildcard
  removes information the compiler uses. The import list **breaks UFCS ties** (the decision
  below), and `lyra-W004` now reports a name that only a method call reaches — both go
  silent under `import ast.{ * }`, and a collision arrives from a module nobody read. A
  qualified name adds a spelling and takes nothing away: it binds no bare name, so it
  cannot shadow, cannot collide, and never breaks a tie.

  **Syntax.** `<namespace>.<Name>` wherever a type name may be *written* — parameter,
  return, local annotation, struct field, `type` alias, generic argument, constructor
  payload, `impl` target. The namespace is the one an `import` bound (its last path
  component, or its alias), **not** an arbitrary dotted path: an import is what makes a
  module reachable, and `std.temporal.PlainDate` from a file that imports neither would
  resolve a module nobody asked for.

  **Resolution is the value form's, which already exists.** `visibilityIn(module, name)`
  and `LookupTypeIn` are the `In` forms written for exactly this question, and privacy
  stays structural — a private type in another module is still "not yours", not "unknown".
  `checkWrittenTypeNames` asks "was this name admitted?"; for a qualified reference it asks
  "is that module imported?" instead.

  **The grammar's trap.** `user_defined_type_name` is one token reused for declarations
  *and* references through `alias` — `struct_name`, `data_type_name`, `tuple_type_name`,
  `constrained_type_name` are all it. A qualified form must be admitted only where a type
  is **referenced**; admitting it at a declaration would parse `struct a.B { … }`. It also
  has to sit wherever `include/types/allocation.js` admits a type name, or `shared ast.Expr`
  will not parse.

  **Keep `Name` bare in the AST.** `types.UnresolvedType` carries the name the printer
  prints, and 245 goldens print it; the module belongs beside it (as `TypeRefs` already
  carries a key) rather than inside it. That keeps the golden files, the bootstrap's
  differential test and `pkg/docgen`'s signature round-trip unaffected except where they
  should be.

  **Work, in the order the cross-project rule forces:**
  1. `tree-sitter-lyra`: the new rule, at reference positions only. Pushed **before** the
     dependent `lyra` change, and `parser.c` regenerated with it.
  2. Collector: `parseType`'s new case; `TypeRefs` records the module.
  3. Typechecker: resolve through the named module; `checkWrittenTypeNames` asks the
     import question for a qualified ref.
  4. `pkg/docgen`: signatures are re-rendered in source syntax and round-trip through the
     parser (there is a guard test), so the qualified form has to render.
  5. `lyrafmt`: leaf-driven, so likely free — wants a round-trip case, since a hidden token
     between two leaves is the shape that has bitten it before.
  6. **Both editors' queries**: `tree-sitter-lyra/queries/highlights.scm` and
     `lyra-zed-ext/languages/lyra/highlights.scm` are deliberate siblings with different
     capture names, and a new node kind wants both.
  7. The bootstrap's `collect_type`, which the differential test will demand.

  **Open:** whether a bare `import lib.{ Name }` should then warn when the qualified form
  is used everywhere — probably not; the two spellings are a choice, as they are for values.

- **[DECIDED] A method call does not require the method's name in the import; the list
  breaks ties instead.** Gating was measured against this repo — 44 call sites, 13 files,
  mostly accessors on types with no public fields — and rejected: it taxes what the lack of
  field privacy forces and drags names like `day` into the bare scope. (COMPLETED.md, 09/22.)


### Allocation flavor — `lyra-E018` covers all nine positions (09/24)

Found while writing LANGUAGE.md's new **Allocation flavor** section, by running the same
stack→`shared` crossing through every position that can hold a value. One was silently
wrong, six reached the backend, and all of them report now. Kept as a record of the shape,
since the next position added — a `with` binding, a comprehension clause — will inherit the
same exemption unless it goes through `checkStoredFlavor`.

- **[FIXED 09/24] A `stack`-annotated argument into a `shared` parameter segfaulted,
  silently.** The 09/23 borrowed-argument fix added `checkArgumentAllocation`, which fires
  where `firstAllocationMismatch` *exempts* — one side `Unspecified` — so the common
  spelling was caught and the concrete pair was left to `checkAllocationCompat` on the
  `own` path, which a borrow never takes. Writing the flavor out made the program worse
  than leaving it off. **Fixed:** the call site runs `checkAllocationCompat` for every
  parameter, owning or borrowing, which also closes the structural case (an element's
  flavor inside a borrowed array argument). A test asserting the miscompile was clean
  (`TestAlloc_BorrowedParamFlavorMismatch_Ok`) is what had kept it alive — a borrow reads
  the value *through the parameter's representation*, so "references it in place" was
  never true of a function compiled once. (COMPLETED.md, 09/24.)

- **[FIXED 09/24] Six positions reported nothing and failed in the backend** with
  `aggregate element type mismatch` — rule 5 working and rule 14 not — and two of them
  emitted **invalid IR**, reported by clang rather than by `lyrac`. Annotated init, a data
  constructor payload, a struct literal's field value, an array element, a tuple element,
  a reassignment and an interior write. **All nine positions report now, in both
  spellings** (`checkStoredFlavor`).

  The shape behind it was one sentence, and the fix followed it: a value built by a plain
  construction and bound to a name is `Unspecified`, not `Stack`, so
  `firstAllocationMismatch` — which fires only when both sides are concrete — exempted the
  common spelling everywhere, and `checkArgumentAllocation` covered that exempted pair for
  arguments alone.

  What made it more than one predicate: the rule is about **where the value came from**,
  not what its type is. `let s: shared Node = Node { … }` is legal and `= n` is not, for
  the same `Node` either way, so the check walks the expression — per element for a
  container literal, per branch for an `if`/`match`, and stopping at a construction. The
  case that taught it was `examples/collector`: the collector builds an applied
  constructor as a *named tuple literal*, so that one node means both "an anonymous tuple,
  whose elements are each a storing site" and "a constructor applied, which is built
  here". Reading it only the first way refused the bootstrap's own AST.
  (COMPLETED.md, 09/24.)

### Using a released resource was not reported (09/25)

- **[FIXED 09/25]** `close_it(h)` then `read_it(h)` checked clean, as did reading a node
  out of a deleted tree. `CheckUseAfterMove` counted a move only for a **managed** value,
  on the reading that "a non-managed value is copied, so passing it leaves the original
  intact" — true of every struct except one holding a handle to something Lyra does not
  own. A `@must_release` type is consumed by an `own` parameter now, and its message says
  *released* rather than *moved*, because it is a use-after-free rather than a lost
  uniqueness.
- **[FIXED 09/25] A branch that returns no longer joins.** Required by the above and a
  latent fault on its own: the union at an `if`/`match` counted a release down an arm that
  `return`s as having happened on the path past it, and the loop seed counted one as
  reaching the next iteration. `lyrafmt` is written in exactly that shape twice — release,
  report, return — so the fix above reported it twice before this landed. The union was
  "conservative, matching Rust"; this is the half of Rust's rule it was missing, invisible
  while only managed values could move.
- **[FIXED 09/25] `@must_release`'s second argument is reported, not dropped.** It reads
  one release function by design, and silently ignored the rest — so
  `@must_release(free_it, delete)` read as "either discharges it" and neither the warning
  nor the compiler said otherwise.

### A method-style call showed no signature in hover (09/25)

- **[FIXED 09/25]** `twice(a)` hovered as `twice: (i64) -> i64` and `a.twice()` hovered
  empty. `desugarUFCSCall` synthesizes the callee and nothing recorded a type for it, so
  the TypeTable had a hole and every reader of it answered nothing — the LSP had a fallback
  that rendered the doc comment alone, which is why the symptom was "no signature" rather
  than "no tooltip". Fixed where the hole was: the desugaring records
  `lambdaSignature(fn)`. **The spelling the standard library is documented in was the one
  without a signature in the editor.**

### A name used only method-style is now reported (09/25)

- **[FIXED 09/25] `lyra-W004` gained the case the 09/22 decision created.** A method call
  does not require the method's name in the import list, so `import lib.{ f }` beside
  nothing but `x.f()` lists a name doing no work — `import lib` alone compiles. It said
  nothing, because by the time the check walks the tree a UFCS callee *is* an ordinary
  identifier: `desugarUFCSCall` synthesized it at the method name's position, so a name
  alone cannot tell it from a bare use. The typechecker records the callee's **span**
  (`UFCSCallees`), and the check scans twice — once counting every reference, once
  counting only written ones.
- **The guard is the interesting half.** The list is what breaks a tie between two modules
  exporting the same method name, so dropping the name turns a working call into
  `receiver.f is ambiguous`. A second exporter silences the warning, deliberately coarsely
  — any other exporter, not just one whose receiver would clash — since advice the
  compiler then refuses is worse than advice not given.
- **It also unhid a case that was never reachable**: the module-level UFCS note used to
  exempt the whole import statement, so a genuinely unused member sitting beside a
  method-used one was silent. It exempts the statement now, not its members.
- Fires on about a dozen files across `std`, `bindings` and `examples`, each a real
  redundant name — `std/io.lyra`'s `import std.path.{ parent }` beside only
  `path.parent()`, verified to compile as `import std.path`. **Open:** whether to act on
  them, which is a repo-wide edit and a judgement call about how the library reads.

### `module a.b` carried no location (09/25)

- **[FIXED 09/25]** `CollectModuleDeclaration` built its node without an `AstBase`, so the
  declaration's span was zero — the third node found this way in two days, after
  `ForLoopExpr` and the AST-locations slice. Hazard 14's cost applies (a location-less
  diagnostic escapes per-file filtering), and so does a second one this time: the editor's
  new "add an import after the module declaration" put the line *above* it, because a zero
  span sorts before every real one. **A missing location is invisible until something asks
  where the node is.**

### The C-style `for` loop carried no location (09/25)

- **[FIXED 09/25]** `CollectForLoopExpr` built its `ast.ForLoopExpr` with no `ExprBase`, so
  the node's span was zero. The same gap `for/in` had until 08/18 and the same cost: a
  diagnostic against a zero Location prints no `line:col` and escapes the driver's per-file
  filtering (hazard 14), so a warning on a prelude loop lands on every file compiled.
  **Found by the bootstrap** — the Lyra collector set a span, the two printed ASTs
  disagreed, and a *missing* location has no other symptom inside one implementation.

### A `[]shared T` and a `[]T` in one function segfaulted (09/24)

- **[FIXED 09/24]** Both arrays were built and printed correctly; the crash was at scope
  exit. **The drop glue was keyed on the type's rendering**, and `DynamicArray<Node>` is
  what both `[]shared Node` and `[]Node` render as — allocation flavor is deliberately not
  part of a type's identity, which is right for assignability and wrong for a cache whose
  entries are generated functions. One glue was emitted and both releases called it, so the
  plain array's release ran the boxed loop and read an inline `%Node` as a box pointer.
  **Fixed:** `dynArrayDropFn` keys on `dropKey`, which renders an element's flavor
  (`elementDropKey`). The third instance of one mistake in that key — two modules' private
  `Inner`s, every anonymous tuple in a program, now two element flavors — and they share a
  shape: **a key that renders less than the glue reads.** (COMPLETED.md, 09/24.)

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

- **[DONE 10/19] `if let … { } else if … { }` parses**, `else if let` too, and an `if`'s
  `else` may be an `if let`. (COMPLETED.md, 10/19.)
- **[DONE 10/19] A constructor means the type its module reaches** — `TrueColor` in a
  module importing only `project.{ … }` meant another module's `ImportEvent`, the
  alphabetically first; candidates are now ranked by how the module reaches them.
  (COMPLETED.md, 10/19.)
- **[OPEN] A constructor cannot be qualified** — `import_view.Chosen(1)` is a syntax error,
  so a namespace import reaches a module's functions and none of its constructors. It
  belongs beside the qualified type names (Modules and tooling).
- **[DONE 10/19] A constructor is a function value** (`on_change: Width`), and **a lambda
  in a generic struct field is typed by it**. (COMPLETED.md, 10/19.)
- **[OPEN] lui can write `on_change={Width}` through** rather than the typed lambda it
  generates, now that the compiler accepts both; it would also pass a function name (`doc.
  rename`) as itself. Regenerates Vega's and Termina's views.
- **[OPEN] `struct X {}` does not parse**: a type with no fields (a marker implementing a
  trait — a game's music tables) must be `data X = X` instead. Either accept it or say so.

- **[DONE 09/23] A match arm names alternatives with `|`** (`1 | 2`, `"get" | "post"`),
  over literals and ranges. A binding alternative stays refused, deliberately — see
  LANGUAGE.md § patterns. (COMPLETED.md, 09/23.)
- **[DONE 10/19] An `if` or a `match` is an operand**, on either side of arithmetic,
  `++`, a comparison and `&&`/`||`; and an arm's literal must fit the operation's width.
  (COMPLETED.md, 10/19.)
- **[DONE 09/29] `?.`/`?[` are safe navigation**, desugared by the collector (`lyra-E083` on
  a non-Maybe); and **nullary constructors are or-pattern alternatives**. (COMPLETED.md, 09/29.)
- **[OPEN] Or-pattern alternatives that bind nothing but test a payload** (`Some(_) | None`),
  and alternations below the top of a pattern (`Some(1 | 2)`, `(1 | 2, _)`, both refused
  today). Neither needed yet.
- **[DONE 10/08] `loop { … }` and `while cond { … }`**; `for` only iterates, and the
  C-style `for init; cond; post` is gone. (COMPLETED.md, 10/08.)
- **[DONE 10/08] `while let p = v { … }`**, erased by the collector into `loop { if let … }`.
  (COMPLETED.md, 10/08.) Sheliak's three poll loops and lyra-md's link scan use it.
- **[OPEN] A struct pattern as a `for` binding** (`for Pt { x, y } in points`). Its rival was
  the conditional `for` reading the literal as a condition; with that form now `while`, the
  grammar may admit it. Untried; a tuple covers the spelling.
- **[OPEN] A written loop-variable type in the other positions**: a comprehension clause
  (`[i: u8 in 0..<10 | …]`) and the two-variable form's element. `for i: u16 in …` covers
  the one-variable loop (09/29).
- **[OPEN] A private method's hint says "import it"** when its module already is imported
  (`shapes.square(2).hidden()`; `typechecker_ufcs.go`, the `ufcsImported` branches). It
  should say the method is private to its module. Found 10/07 testing `impl` blocks; a
  hand-written private method gets the same message.
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

- **[OPEN] A panic in the standard library names its caller.** `m.unwrap()` on a `None`
  traps `at …/std/prelude/…`, the `panic`'s own line, which says nothing about which
  `unwrap`. A function marked to take its caller's place (Rust's `#[track_caller]`) would fix it.
- **[OPEN] `parse_args` cannot stop at a command.** Every `-`-argument is a flag, so a
  program running a command after its own flags (`termina-ui --print ls -la`, xterm's `-e`)
  hand-rolls the loop. A stop at the first positional, or at `--`, would do.
- **[OPEN] A deflate compressor.** `std.compress` inflates only, so `std.png.write_png` stores
  its pixels uncompressed: an MCP image from Vega is ~1.8 MB of base64 for a 768×432 frame
  that compresses to a tenth of it. Fixed-Huffman deflate with a small hash-chain matcher
  would do.
- **[DONE 10/19] Markup for `bindings.imgui.ui`** — `.lui` files generated into Lyra by
  `tools/lui`, not a Lyra grammar change (COMPLETED.md, 10/19).
- **[DONE 10/21] `.lui`: components called across files** — `<import src="other.lui"/>`
  reads its components, checks calls and imports the module (COMPLETED.md, 10/21).
- **[OPEN] `bindings.imgui.ui`: what the tree lacks.** Keys for repeated labels (ImGui's
  `push_id`), tooltips, tables, child regions, an integer slider, and layout past `Row` and
  `Form`. Grow it as Vega's next dialogs and panels move to markup.
- **[OPEN] A float literal array's element times an f32 is invalid IR** — `let step: f32 =
  2.0; for k in [-1.0, 0.0, 1.0] { total = total + k * step }` typechecks, then clang
  refuses `fmul double %a, %b` with `%b` a float: the elements stay `f64` in the backend
  while the product was taken as `f32`. Either the context settles the literals as `f32`
  or the mix is refused; the backend must not emit it (rule 5). Found by Termina's ☰
  glyph, which types the array meanwhile.
- **[OPEN] A tuple index is not a comprehension's source**: `[c in g.1 | c]` is a syntax
  error where `[c in g.colors | c]` and a constant parse (the grammar's generator value).
  Found by Termina's color groups, which became a struct for it.
- **[OPEN] `std.io`: walking a directory tree.** lyrafmt and lui each walk one by hand
  (`read_dir`, skipping `.` names and symlinked directories). A `std.io` walk, sorted,
  with the caller's filter, would serve both.
- **[OPEN] `bindings.imgui.ui`: install a tree in the Mac's bar.** Vega's `menus.lyra` and
  Termina's `menus/menus.lyra` each walk a menus tree into `bindings.menubar` (items by tag,
  greying synced each frame). One shared function could serve both, and Vega's would then
  read `Shortcut.alt`.
- **[OPEN] `bindings.imgui`: what is still unbound** is listed at the end of `generated.lyra`.
  Next likely: `ImGuiListClipper` (long lists — needs a constructor), drag-and-drop payload
  data (`void *`), and a layout location for `imgui.ini` chosen by the program.
- **[OPEN] `bindings.imgui`: `IsMousePosValid` is unbound** (its `const ImVec2 *` parameter).
  Sulafat tests `get_mouse_pos()` against -FLT_MAX instead; `floor()` on that position traps.
  A hand-written no-argument wrapper would do.
- **[DONE 09/29] `bindings.imgui`: `cancel_quit` and SDL's open/save dialogs**; `std`:
  `string.replace` and `std.json`'s writer. (COMPLETED.md, 09/29.)
- **[OPEN] Defaults in the remaining by-name passes.** A filled-in default
  (`TypeTable.IsDefault`) names its module's top level, but captures, `@must_release` and
  the value-range pass still match its names against the caller's bindings. Each needs a
  probe like `TestMove_ADefaultIsNotTheCallersBinding`, and the skip if it misreads.
- **[OPEN] `bindings.imgui` on Linux is unverified.** `@link` flags are sorted, so `-lSDL3`
  precedes the static `-llyra-imgui`; check the link under `--as-needed`, and that the
  archive needs nothing from libstdc++ there either.

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
- **[OPEN] `W022` reads a computed `match` scrutinee as an acquisition.** `match if c { a }
  else { b } { Some(x) => set_cursor(x) }` warns that `x` is unreleased, though both arms
  are views of bindings released later. `editor.lyra` matches each binding directly.
- **[PARTIAL] SDL3 bindings, driven by examples, towards an NES-style game.** One example
  per rung, binding only what it uses (`bindings/README.md`). Art is PNG via **SDL3_image**
  (`bindings/sdl3_image.lyra`). The rungs live in `examples/SDL3/nes/`.
  1. `screen.lyra` — 256×240 integer-scaled screen, primitives, debug text. **Done 09/25.**
  2. `sprites.lyra` — PNG sprite sheet via SDL3_image, flips, colour mod, nearest scaling.
     **Done 09/25.**
  3. `input.lyra` — NES pad (D-pad, A, B, Start, Select) from keyboard state and gamepads,
     with pressed-this-frame edges. **Done 09/25**, with the shared `console.lyra`.
  4. `sound.lyra` — APU-style pulse/triangle/noise pushed to an audio stream from the loop.
     **Done 09/25.**
  5. `tilemap.lyra` — scrolling tile background, camera, fixed 60 Hz timestep.
     **Done 09/25.** Keyboard only: gamepad tracking belongs in `pad.lyra` before the game.
  6. `game.lyra` — SLIME TRAIL: one level, patrolling slimes, reach the flag. **Done 09/25.**
  7. `editor.lyra` — a level editor, driving every key constant (generated) and the mouse.
     **Done 09/26.** Still unbound: the GPU API, surfaces, joysticks, text input, the
     clipboard, window management beyond fullscreen, file dialogs.
- **[OPEN] raylib gaps:** `SetShaderValue*` uniforms (a `const void *` no Lyra pointer
  reaches); `LoadMaterials` (needs pointer reinterpretation to reach `MemFree`);
  `LoadTextureCubemap`; `ExportImageToMemory` (returns size 0).
- **[OPEN] glTF viewer gaps:** morph targets, `KHR_animation_pointer`, crossfading node
  clips; WebP and KTX2/Basis textures; `KHR_materials_variants`, iridescence, sheen,
  specular, IOR, volume; back-to-front sorting of see-through meshes.

## GUI (`ui/`)

The design is the *Lyra GUI — Design* document; `ui/README.md` maps what is built.

- **[DONE 10/21] Phase 1, the seams** — `ui.draw`'s display list, `ui.tess`, the
  `Platform`/`Renderer` traits and `ui.sdl3` over `SDL_RenderGeometry` (COMPLETED.md, 10/21).
- **[DONE 10/21] Phase 2, text and layout** — `ui.text` (HarfBuzz, a FreeType atlas),
  `ui.style` (themes, the five-layer cascade), `ui.layout` (constraints, `paint`); gate
  `examples/ui/settings.lyra` (COMPLETED.md, 10/21).
- **[OPEN] What a terminal needs of `ui`** — an element the program paints, font faces and
  fallback, raw keys and pointer; designed in `ui/TERMINAL.md` (10/22), for Termina's move
  off ImGui. Steps 1 (`painted`, `PaintDraw`/`PaintText`, `on_size`), 2 (the atlas's pages)
  and 3 (faces, metrics, hinting), 4 (fallback) and 5 (`PaintCells`, `PaintWave`) are built
  (10/22), 6 (raw input) and 7 (`Readable`, `scroll_follow`, `ScrollTo`) 10/23, and step 8's
  first half 10/23: `termina-ui` beside the ImGui app.
- **[OPEN] What a terminal's window needs of `ui`** — popups, menus (drawn, and native on a
  Mac), a tab bar, the window's controls and a drawn title bar, a file dialog, colour; designed
  in `ui/WINDOW.md` (10/23), the rest of Termina's move off ImGui. Steps 1 (popups, the
  select's list on them), 2 (menus drawn), 3 (menus native on a Mac), 4 (the tab bar), 5 (the
  window's controls, a drawn title bar), 6 (a file dialog), 7 (colour) and 8 (lui's elements,
  `Fragment`) built 10/23; next Termina on them (step 9).
- **[OPEN] `ui`: a drawn title bar wants trying on a Linux desktop** — SDL's hit test calling
  `bindings.hittest` for a borderless window (moving it, its edges resizing it) is untested:
  the dummy driver refuses a hit test. And its double click: SDL starts a move at the press, so
  a second click never reaches the program to maximize (`ui/WINDOW.md`'s open question 3).
- **[OPEN] `ui`: a tab's label is clipped bare**, its last letters cut where it shrinks
  ("Docu"); an ellipsis, or a fade at the edge, would say there is more. Text has no
  `text-overflow` yet.
- **[OPEN] `ui` focuses nothing at start, and `App` has no first command.** termina-ui asks
  `FocusKeyed` from its painter's first `on_size`; an `App.start: []Command<e>`, or an
  `autofocus` on an element, would say it directly.
- **[OPEN] A script's key is a press with no release, and its text no key before it**
  (`ui.script`). A program keeping the modifiers as the last key left them sees Ctrl held
  after `Ctrl+r 'echo'`, which no keyboard does. A key token could press and release.
- **[OPEN] What raw input left out** (`ui/TERMINAL.md` §3): a key's repeat flag and physical
  code, the keypad's keys (they type through text), a `cursor` style property (a painter's
  I-beam over its command line), and AccessKit's Terminal role (a painter is a group).
- **[OPEN] `Fonts.cells` looks every cell up each frame**: the faces' character maps and
  advances through HarfBuzz, a few calls a cell — 12,000 cells on a 200×60 screen. A cache by
  face order and character would make a frame of an unchanged screen cost lookups only;
  measure under Termina first.
- **[OPEN] `ui.text`'s hinting is light everywhere unless a program says.** macOS draws
  text unhinted; `ui.text` cannot ask which platform it is on (it calls no platform library),
  so the default belongs to whatever opens the window (`ui.sdl3`, `ui.gpu`'s `open_app`).
- **[OPEN] `ui.text`: left to right, runs by face only.** Fallback is built (10/22): a
  paragraph's runs are cut by the face that has each character. Not by script — a run of one
  face mixing Latin and Hebrew is shaped as one — and no bidi, no colour emoji (FreeType's
  BGRA bitmaps need a colour atlas). Also no text alignment (a line starts at its box's left)
  and no ellipsis.
- **[DONE 10/22] `ui.text`: the atlas's pages** — a page a texture, added when a glyph fits
  in none, the least recently drawn emptied past `ATLAS_PAGES` (COMPLETED.md, 10/22).
- **[OPEN] `ui.layout`: a `Picture` ignores opacity.** `DrawOp.Picture` has no colour to
  fade by; give it one (a tint, as `Glyphs` has) and `paint` can pass the opacity on.
- **[OPEN] `./asan.sh` has no HarfBuzz or FreeType** (`asan.Dockerfile`), so the `ui`
  layout, cascade and offset tests skip there; CI installs both.
- **[DONE 10/21] Phase 3, interaction** — `Node<e>`, `ui.interact` (identity, hit-testing,
  focus, the keyboard, a drawn text field), `ui.app`'s loop, `ui.script`; gate
  `examples/ui/form.lyra` (COMPLETED.md, 10/21).
- **[DONE 10/21] Phase 4, markup and reload** — lui's `ui` target (tokens, global and themed
  sheets, parts, imports across files), commands and subscriptions, `Node.map`, `Host` for
  `lyrac run --live`; gate `examples/ui/components_view.lui` (COMPLETED.md, 10/21).
- **[DONE 10/21] Phase 5, polish** — a layout cache by identity, frames drawn on demand,
  batched geometry calls, and an accessibility tree to VoiceOver through AccessKit
  (COMPLETED.md, 10/21).
- **[DONE 10/21] Flex and grid** — `flex`, `grid`, layout style properties, `<Flex>`/`<Grid>`
  and CSS's properties in lui; demo `examples/ui/layouts_view.lui` (COMPLETED.md, 10/21).
- **[DONE 10/21] Media queries, selector lists, window limits** — `Rule.media`, `@media` in
  lui, the cache keyed on media answers, `App.min_size`/`max_size`, live resize in the Layouts
  demo (COMPLETED.md, 10/21).
- **[DONE 10/21] The GPU renderer and effects** — `ui.gpu` over SDL_GPU (Metal), shadows,
  gradients, filter and backdrop blur, CSS syntax in lui; demo `examples/ui/effects_view.lui`
  (COMPLETED.md, 10/21).
- **[DONE 10/21] Overflow and scrolling** — `overflow_x`/`overflow_y`, overlay scrollbars,
  wheel/drag/keys, a window that scrolls; demo `examples/ui/overflow_view.lui` (COMPLETED.md, 10/21).
- **[OPEN] Scrolling is immediate.** No smooth scrolling, momentum or rubber-banding; a
  trackpad's precise deltas are scaled like a wheel's lines. `ui.sdl3` would need SDL's
  precise wheel values and a per-scroller animation in `Ui`.
- **[OPEN] Scrollbars only overlay.** No classic gutter (`scrollbar-gutter`), no
  `scrollbar-width`, no auto-hide after idle; `ScrollTrack`/`ScrollThumb` style colour and radius only.
- **[OPEN] The window's overflow is inline style**, so it beats any sheet's rule for the root;
  a sheet cannot set the window's overflow. CSS's root-propagation rule would fix it.
- **[OPEN] The cascade parses selectors per element.** `applied` splits and trims every rule's
  selector for every element it styles, so each rule a theme gains slows every layout; parse
  them once into a sheet (by tag and class) and the whole tree gets faster.
- **[DONE 10/22] Transitions** — CSS's `transition` in `Style` and lui, `ui.motion`, paint-only
  properties blended after layout, sizes through the next; the effects demo (COMPLETED.md, 10/22).
- **[OPEN] Animations beyond transitions**: `@keyframes`/`animation`, `transitionend`-style events
  to the program, CSS's reversing shortening (a change back mid-way takes the full duration),
  `font-size` and `line-height`, and transforms (there are none yet) do not transition.
- **[OPEN] Scrollbars and a field's placeholder, caret and selection do not transition**:
  they are styled outside the laid-out elements the walk visits (a slider's parts are inside).
- **[OPEN] An idle `ui` window ignores SIGTERM** until the next event: SDL turns the signal into
  a quit only when it pumps, and the loop is asleep in `SDL_WaitEventTimeout` with no timeout.
  `pkill` left demo windows open; a bounded wait (or SDL's quit-on-signal waking it) would fix it.
- **[OPEN] `ui.gpu` on Linux and Windows.** The shaders are MSL compiled by SDL at run time;
  Vulkan needs SPIR-V and D3D12 DXIL, which want a shader toolchain (SDL_shadercross, or glslc)
  pinned and checked at build time. Until then both fall back to SDL's 2D renderer.
- **[OPEN] Effects not built**: `text-shadow`, `opacity` as a group (an offscreen layer — today
  it multiplies into each colour), other `filter` functions, conic gradients, a gradient's
  `to corner` by the box's diagonal (taken as 45°), more than eight stops on the GPU
  (resampled).
- **[OPEN] Typechecker: a trait method's signature names types in the caller's scope.**
  Calling `renderer.draw(color, ops)` where `Renderer.draw` takes `ui.draw.Color` failed in a
  module that had not imported `Color` ("cannot assign ui.draw.Color to Color"); importing it
  works around it.
- **[OPEN] `ui.gpu` prepares a frame in ~2 ms for 8,760 instances** (400 labelled boxes): 80
  floats an instance, pushed one at a time. A packed instance or a typed buffer would cut it;
  only worth it when a screen's frame misses its refresh.
- **[DONE 10/21] Live resize is the framework's** — `Platform.on_live_resize`, `ui.app`'s
  `follow_resizes` over a `shared` running window, for `run` and for a program's own loop
  (COMPLETED.md, 10/21).
- **[OPEN] `@media` lacks range syntax and more features**: `(width >= 600)`, `aspect-ratio`,
  `resolution`, `prefers-reduced-motion`, and a `not` on one condition alone.
- **[OPEN] Grid: no named areas, no negative lines, no `justify-content`/`align-content`.**
  `grid-template-areas`, `grid-area: name`, lines counted from the end (`-1`) and packing
  the tracks themselves are not built; tracks always stretch (CSS's `normal`).
- **[OPEN] Flex and grid: no margins and no percentages but `flex-basis`.** CSS's auto
  margins (pushing an item to the end) and `width: 50%` would each need a length type in
  `Style`; `align-self: baseline` reads only a row's first line.
- **[OPEN] Layout: intrinsic widths are measured afresh each time** — a flex item's or grid
  item's min- and max-content, outside the cache. A container unchanged is reused whole, but
  one that changes re-measures every item; memoize by identity if a large grid shows it.
- **[OPEN] `order` reorders Tab's focus too**: the laid-out children are in `order`'s order,
  and `focus_order` walks them. CSS keeps the document's order for focus.
- **[OPEN] A GPU renderer is not needed yet.** Measured 10/21 (Apple Silicon Mac, SDL's
  Metal renderer, 1200×900): 400 labelled boxes (800 ops) take 7.2 ms to lay out fresh and
  0.31 ms cached, 1.9 ms to paint, 1.6 ms to tessellate and submit (0.6 ms of it
  tessellating); 100 boxes 2.1 / 0.09 / 0.54 / 0.39 ms. Geometry is not the cost; revisit for
  rounded clips (a stencil) or when a screen's draw passes ~4 ms.
- **[OPEN] AccessKit on Linux and Windows.** `bindings/accesskit` builds the macOS adapter
  only; elsewhere the stub, and `ui.sdl3` tells nobody. The same release has the Unix
  (AT-SPI) and Windows (UIA) adapters; the shim needs their window handles and, on Unix,
  root-window bounds.
- **[OPEN] `ui.access`'s actions are unchecked against VoiceOver.** VoiceOver reads the form
  and its highlight fits each control (10/21, once bounds went over in pixels); pressing,
  setting and stepping from VoiceOver are tested headless only.
- **[DONE 10/22] `ui.access`: a slider's value set as text** — read with the prelude's new
  `parse_f64` (COMPLETED.md, 10/22).
- **[OPEN] Backend: a private generic struct as another module's type argument cannot be laid
  out.** `HashMap<string, Cached<e>>` in `ui.layout` failed with "cannot lay out data type
  Maybe" while `Cached` was private; it is `pub` meanwhile (its doc says why).
- **[OPEN] Typechecker: a generic call has no context from a field assignment or a generic
  struct literal's field.** `self.cache = hashmap_new()` and `Host { cache: layout_cache() }`
  need `::<…>`, though the field's type settles both.
- **[OPEN] `ui.app`: commands run where the host runs.** A file read holds the loop until it
  ends; async I/O wants a worker and results the platform posts (an `InputEvent` carrying an
  id, since it cannot carry the program's event type).
- **[DONE 10/23] lui's `ui` target's menus, tab bar, swatch, window buttons and `Fragment`**
  (`ui/WINDOW.md` step 8, COMPLETED.md 10/23).
- **[OPEN] Backend: a string pattern holding an escape** (`"\\"` as a `match` arm) is refused
  ("escaped string patterns not implemented yet"); lui writes an `if` instead.
- **[OPEN] Typechecker: an expected type does not reach a method's receiver** —
  `let xs: []Node<Ev> = [...[x in ys | [text("a")]].flatten()]` cannot infer `text`'s event,
  while `flatten([…])`, the same function called plainly, can.
- **[OPEN] lui: an import's module is its path from the importing file**, `/` as `.` — right
  when both are under the program's root as written, else `module="…"`. Imports are one level:
  an imported file's own imports are not read.
- **[OPEN] Typechecker: a generic struct's field defaulted to `None`** (`shown: Maybe<Laid<e>>
  = None`) is E073; `Host.shown` is a `[]Laid<e>` of at most one meanwhile.
- **[OPEN] Typechecker: an untyped lambda as a data constructor's payload** (`Scope((_) =>
  sheet)` for `Scope((Theme) -> Sheet)`) is not elaborated from the payload's type, as an
  argument's is; its parameters must be annotated.
- **[DONE 10/22] `ui`: states as pseudo-classes** — `:hover`, `:active`, `:focus`,
  `:focus-visible`, `:focus-within`, `:checked`, `:disabled`, `:enabled`, class-level
  specificity; the class form dropped (COMPLETED.md, 10/22).
- **[OPEN] A generic struct's lambda default has one type for every instantiation.** A field
  default is the declaration's own AST node (`applyDefaultFields`), so `on_edit: (string) -> e =
  (_: string) => panic("…")` lowers with no return type, and with `-> e` written it is not
  specialized ("cannot assign (string) -> e to (string) -> Ev"). The `[]` case was fixed 10/21;
  a lambda needs the default instantiated (or elaborated) per literal.
- **[DONE 10/22] `ui`: a select** (HTML's `<select>`, `select`/`SelectSpec`, lui's
  `<Select>`) — its list floats over the page (`Laid.floats`) (COMPLETED.md, 10/22).
- **[DONE 10/23] `ui`: a select's list scrolls and flips** — placed as a popup's content is
  (`ui/WINDOW.md` step 1, COMPLETED.md 10/23).
- **[OPEN] `ui`: a swatch is heard as a button** labelled with its colour's text; AccessKit's
  colour well role (and its value) would say what it is. `AccessRole` has no such role yet.
- **[OPEN] `ui`: AccessKit is not told a select is expanded, nor what a popup or a menu is** —
  an open popup's content is heard as its elements, with no dialog role around them, and a
  menu bar, its menus and items as buttons, not AccessKit's menu roles; a tab list's selected
  tab is not said to be (AccessKit's `selected`, which `AccessNode` has no field for).
- **[OPEN] `ui`: a disabled control is not told to assistive technology** (AccessKit's
  `disabled` flag), nor disabled through its ancestors (CSS's disabled `fieldset`).
- **[OPEN] `ui.interact`: a caret moves by rune, not grapheme cluster.** `é` written as `e`
  and a combining accent, or an emoji sequence, takes several Left presses; needs UAX #29.
- **[OPEN] `ui.interact`: a field's caret does not blink**, and typing is not coalesced into
  one undo step a word. Blinking wants phase 4's timers.
- **[OPEN] `ui.layout`: `Node<void>` does not compile.** A payload instantiated at `void` in a
  constructor with other fields lowers as `{ i1, void }` (`Checkable(bool, e)`); `NoEvent`
  stands in. A void field should take no space, as a lone void payload already does.
- **[OPEN] Typechecker: a nullary constructor in a struct literal beside a field mentioning
  its variable** — `Nd { k: L, xs: xs }` for `struct Nd<e> { k: K<e>, xs: []e }` is E073 at
  `L`, though the return type `Nd<e>` settles it; `let k: K<e> = L` first works.
- **[OPEN] Typechecker: a generic call's element in an array beside typed siblings has no
  context.** `column([box(1.0), sized(None, Some(2.0))])`, `box` typed `Node<Ev>`, cannot infer
  `sized`'s `e`: elements are inferred one by one, before the array's type is joined.
- **[OPEN] lyrafmt is not idempotent on a long struct literal holding a lambda**: `App { view:
  (f: Form) -> Node<Ev> => view(theme, f), update: update, background: … }` past 90 columns is
  re-indented differently on a second `-w`, so `--check` fails right after a format.
- **[OPEN] `ui.sdl3`'s platform half is untested.** Nothing headless sends it input: bind
  `SDL_PushEvent` and script events, as `std.game`'s `--hold` scripts a pad.
- **[OPEN] A rounded clip is square on SDL's renderer** (`PushClip`'s radii are ignored):
  it needs a stencil (an SDL_GPU backend) or content drawn to a texture and masked.
- **[OPEN] A constructor resolved by its declaration, not its name.** Then `KeyDown as
  SdlKeyDown` could stand beside another module's `KeyDown`, which is refused since 10/21
  (`constructorAliasCollisions`) rather than silently confused.
- **[OPEN] An install carries `ui/`** beside `std/` and `bindings/` once a shipped program
  uses it — Vega's release copies those two into its lyra.
- **[OPEN] Vega: `bindings.sdl3.Event` grew `TextInput`, `WindowResized` and
  `WindowScaleChanged`.** A `match` over it with no wildcard arm (none in this repo) is no
  longer exhaustive.

## Array comprehensions

- **[OPEN] A comprehension clause whose source depends on an earlier one** (`[row in grid,
  cell in row | cell]`). Sources are materialized before the loops to compute capacity;
  this needs materialization inside the enclosing loop. Refused loudly, with the
  comprehension-over-a-comprehension workaround in the message (confirmed 09/22). Called a
  *clause* here because a bare "generator" names a `gen` function — the 09/17 convention
  CLAUDE.md states and the backend's own message already follows.
- **[DONE 09/29] A field, an element or a parenthesized expression is a comprehension
  source.** (COMPLETED.md, 09/29.)
- **[DONE 10/19] A constant is a comprehension source** — `[p in PAIRS | p.1]` was a
  syntax error at `in`: an all-caps name lexes as a `const_identifier`, which the clause's
  source list lacked. (COMPLETED.md, 10/19.)
- **[OPEN] A guard is only a comparison, a call or a name** — `[p in pairs | p.1 | p.0]`
  (a `bool` field as the guard) reads as the single result `p.1 | p.0`, a bitwise-or, and
  is refused as one; `p.1 == true` works. A field, an element or a tuple position could be
  a guard too.
- **[DONE 10/19] A later `let … else` no longer hides an earlier binding of its name**, a
  plain destructuring neither, and a destructured name is rebound like a `let`'s.
  (COMPLETED.md, 10/19.)
- **[DONE 10/19] `if` is an operand** — see Language surface. (COMPLETED.md, 10/19.)
- **[DONE 10/19] A field refuses the wrong array** — `h.items = [true]` (or a
  comprehension of `bool`s) into a `[]Item` field passed `lyrac check` and failed in the
  backend; the check now runs before the field's type is pushed into the value.
  (COMPLETED.md, 10/19.)

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
- **[IDEA] A type parameter shared by a module's functions.** Sheliak's 68000 core has ~40
  functions that each begin `<b> where b: Bus = (cpu: mut Cpu, bus: mut b, …)`: the bus is
  generic for monomorphised speed, and nothing lets a module say it once. A module- or
  block-level `where` (or trait objects, at a dispatch cost) would remove the repetition.
- **[IDEA] Borrow modes through function types.** `(mut i64) -> void` is spellable but a
  call through a function value passes by value, so a by-reference function cannot be a
  value (`lyra-E082`). Honouring the type's modes at an indirect call would lift it.
- **[DEFERRED] (c) Exclusivity (`mut` XOR alias)** until the job system or resizable
  interior borrows force it. Leaning toward statement-scoped projections over a static
  container freeze.
- **[OPEN] (e) Observable aliasing.** `var b = a; b[0] = 99` changes `a` silently for `[]T`
  and structs holding one — even an immutable `let` global's, through a `var` set to it.
  `pure` now sees through such a local (10/19); the language rule itself is unchanged.
  Warning on every copy is too noisy; copy-on-write
  (`lyra_rc_drop_reuse` is already the uniqueness test) would make `xs[i] = v` allocate
  under `noalloc`, so it needs (c) first. Check whatever lands for (c) against this.

## Wider integers

- **[IDEA] Arbitrary precision as a stdlib `BigInt`**, never a primitive (it breaks
  trap-on-overflow). Zig-style arbitrary widths are skipped absent a concrete use.

## Foreign functions — `extern`

- **[OPEN] Returning a C function pointer, or passing a closure to C.** Both need a way to
  call a bare code address; the `void *` context parameter covers captures meanwhile. A
  closure could cross as that pair — a trampoline and its environment — where C takes a
  `void *userdata` beside the pointer (who keeps the environment alive is the question);
  never as a bare pointer, which needs code made at run time, and executable memory the
  hardened runtime refuses without an entitlement.
- **[PARTIAL] `@link` search paths, static archives and macOS frameworks.** Search paths
  landed 09/26 as `@link(…, pkg: "…")`, asked of pkg-config. Still the build system's
  problem: a library with no pkg-config package, static archives, and macOS frameworks —
  where a manifest would start to earn its keep.
- Non-LP64 targets are unsupported by stated assumption; `CLong`/`CULong` is the grep
  target for a port.
- **[OPEN] A buffer C keeps past the call is freed at its last Lyra use.** `data()` on an
  array handed to `SDL_SetPointerProperty` was freed before `SDL_CreateProcessWithProperties`
  read it (a segfault; `bindings/sdl3/process.lyra` reads the array again after). Wants a
  spelled keep-alive, or a check that an `unsafe` pointer's array outlives the block.

## Const generics — a value as a type parameter

**[DONE 09/30, layer 0]** `let f<const N: i64> = (xs: ref [N]t) -> …`: integer value
parameters over `[N]T` in function signatures, N solved from the argument by the same
unifier as type variables (an `types.ArraySize` in the bindings map), one specialization
per size, a generic passing its own size on. LANGUAGE.md § Generic parameter lists.

- **[OPEN] `N` as a value** in the body (`for i in 0..<N`) — `xs.len()` covers it today.
- **[OPEN] A `const` parameter on a type or trait** (`struct Ring<t, const N: i64>`), and
  `fixed<I, F>` / `simd<T, N>` over it.
- **[OPEN] Arithmetic in a type position** (`[N + 1]t`) — a solver; and constraints on a
  value parameter, which wait on CTFE.
- **[OPEN] A turbofish value** (`f::<64>(…)`): the turbofish takes types today.
- **[OPEN] A named `const` as an array type's size** (`[MAX_BYTES]u8`): `lyra-E031` takes it
  for an undeclared `const` parameter, so only a literal or a parameter sizes one. Found
  10/14 by `std.chips.effects`, whose buffers are written `[8]u8` and `[128]u8` beside their constants.
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

## The 68000 — Lyra for the Genesis

The route to Vega's engine (a Lyra game compiled for the console): LLVM's experimental M68k
backend, built by `vega/tools/llvm-m68k.sh`. **Probed 09/29**: the IR `lyrac --emit-llvm`
writes compiles with `llc -mtriple=m68k-unknown-elf -mcpu=M68000 -code-model=medium
-relocation-model=static` — the whole prelude, 64-bit and float included — and an `i16`
`pure noalloc` function comes out tight: each overflow check is one `bvs`, as the 68000
sets V on every add. What stands between that and a ROM:

- **[DONE 09/30] A target in `lyrac`**: `lyra.toml`'s `target = "genesis"` is read by
  `lyrac` and the LSP (09/29); `lyrac build` makes a cartridge (09/30: `pkg/rom` links,
  `runtime/genesis` starts it, `pointer_at` and volatile access reach the hardware).
- **[OPEN] Sizes are still `i64`** (next item) — the reason a game's `[]T` could not work
  even with an allocator. Fixed arrays and scalars are what a Genesis program has today.
- **[PARTIAL] A Genesis layer in Lyra**: `std.genesis.vdp`, `.pad` and `.sprites` (09/30)
  run `examples/genesis/walker.lyra`. Next: DMA (needs a pointer's address as an integer),
  planes and scrolling (name tables, a tilemap loader), the window, 6-button pads, and the
  sound chips; then the engine layer over them (actors, animation from Vega's frames).
- **[DONE 09/30] Interrupt handlers in Lyra**: `@interrupt(vblank)` / `@interrupt(hblank)`;
  a `var` the handler writes is volatile everywhere. Still open: variables written by a
  function the handler *calls* (not seen — read them with `read_volatile`), masking
  interrupts around a critical section (`move #$2700, sr` from Lyra — `vdp.hold_vblank`
  covers the VDP's writes against the vertical blank, 10/06, but not an hblank handler's),
  and the level-2 external interrupt (the pads' TH line).
- **[OPEN] The LSP does not watch `lyra.toml`**: editing it takes effect on the next edit of
  a source file. Register a `workspace/didChangeWatchedFiles` watcher and re-publish.
- **[OPEN] A top-level initializer is not target-checked** (`let table = [1, 2]` at module
  level allocates at start-up): CheckTarget walks lambdas and methods only.
- **[OPEN] Sizes are `i64`**: array lengths, capacities and RC counts, and `malloc(i64)` is
  called with two longs. On a 16/32-bit CPU with 64 KB of RAM a length wants the target's
  word — a `usize`-like width chosen per target, which `SizeAndAlign` and the runtime's
  layouts must follow.
- **[OPEN] The runtime's host dependencies**: `main`'s `program_args` wrapper allocates, and
  panic writes with `write`/`exit`. A console needs its own entry and a panic that shows
  something and halts; `malloc`/`memcpy`/`memcmp` either come from a tiny runtime or are
  refused by a console profile (`noalloc` on the frame loop).
- **[OPEN] Compiler helpers**: 64-bit multiply/divide and soft float (`__muldi3`,
  `__muldf3`, …) — compiler-rt's generic C builds with the M68k clang and is permissive.
- **[OPEN] The M68k backend spills a loop's values to the stack** (10/06): the collision
  search reloads its bounds with `movem` each pass, ~400 cycles a collider, and a function's
  prologue saves eight registers. Probe `llc`'s register allocators and `i16` bounds before
  writing more Genesis loops around it; COMPLETED 10/06 has the profile.
- **[IDEA] A profiler in Sheliak**: master clocks by opcode address, named from the program's
  symbols — the throwaway build that found the 10/06 and 10/15 hot spots. `pkg/rom` would
  need to keep local symbols (it maps globals only) for the ROM's own map; 10/15 read them
  with `llvm-nm` from the program's object, offset by `main`'s address.
- **[OPEN] Untyped loop variables default to `i64`** (09/29) — emulated, and slow, on the
  68000. A console profile should steer to `i16`/`i32`, or make the default per target.
- **[OPEN] Memory-mapped I/O needs volatile reads and writes** (the VDP ports, the pads):
  LLVM may otherwise merge or drop them.
- **[OPEN] `data` is a keyword where a call is**: `let data = …` declares, but `data()` and
  `data.f()` do not parse, so a hardware helper named for the VDP's data port has to be
  called something else. Either reserve it everywhere or accept it as a name in expressions.
- **[OPEN] LLVM `main` turns a volatile zero store into `clr`** (16c337766166, 08/25), which on
  a 68000 reads the destination first — wrong for a volatile store, and fatal on the VDP's
  control port. `tools/llvm-m68k.sh` pins e7dd336e0f78, before it (and before the later
  MOVEM/PHI and MOVX fixes). Report upstream, then move the pin.
- **[OPEN] Report the small-code-model store upstream**: `counter = v` in C becomes
  `move.l (4,%sp), (counter,%pc)`, a PC-relative destination, under LLVM 22.1.8.
- **[OPEN] Report the dropped stack index upstream**: `(stack object + index)` was
  addressed as the object alone (`matchAddressBase` set an index beside a frame-index
  base, which `hasIndexReg` does not see). Patched locally by
  `tools/llvm-m68k-patches/0001`; check `main` for the same code before reporting.

- **[OPEN] Patch 0004 upstream**: reported as llvm/llvm-project#232869 (10/16, the fix
  suggested in it). Drop the patch once a fix lands past the pin; a pull request with an
  `llc` lit test is the next step if nobody takes it.
- **[OPEN] Report the `fastcc` demoted-return mismatch upstream**: a function returning an
  aggregate too big for registers, made `fastcc` by the optimizer, read the hidden pointer
  from `d0` while its caller passed it in `a0` (10/15). Lyra moves every aggregate through
  memory on the 68000 since (COMPLETED.md 10/15); a reduced IR case would settle it.
- **[OPEN] `std.chips.music` costs a fifth of a frame on the 68000**: the heroes' extended
  theme 0.18 of a frame an update (0.29 before steady channels, COMPLETED 10/16). What is left
  is flat across the channel loop's code — a bool tested in five instructions, overflow checks
  through the status register, spills — and `MUSIC_STARTS[order * 13 + channel]`, which LLVM
  widens to an `i64` multiply (`__muldi3`) for its bounds check.
- **[DONE 10/16] A Genesis program inlined less crashed**: a `fastcc` struct return popped a
  pointer passed in a register (`tools/llvm-m68k-patches/0004`, COMPLETED 10/16).
- **[DONE 10/16] clang segfaulted compiling a copy of Sheliak**: a global's 320 KB
  initializer built on the stack and stored as one value (COMPLETED 10/16).
- **[OPEN] `-Os` fails for the Genesis**: this LLVM's `opt` refuses `default<Os>` ("use O2 with
  the optsize attribute"); the backend would mark every function `optsize` instead.
- **[DONE 10/16] Same-named types in two modules confused method calls**: refused as
  ambiguous, and generic methods sharing one body (COMPLETED 10/16).
- **[OPEN] The Genesis DAC's pitch on hardware**: the Z80 driver plays 0.5% flat in Sheliak
  (the bus held while the frame is queued), and a real Z80's bank-window reads wait about
  3 T-states more, which Sheliak does not model — about 1% more. Measure on a console, then
  correct `Z80_RATE` or model the wait in Sheliak.
- **[IDEA] The DAC past 64 KB and past 11.5 kHz**: the driver counts a sample in 16 bits and
  plays at a fixed 11,473 bytes a second, so a long voice clip is cut and a 32 kHz drum loses
  its top. A count of 24 bits, and a faster loop with the queue every other byte.

## Large aggregates

- **[DONE 10/11] A large aggregate returned by value** — through an `sret` slot now, with
  `memcpy`/`memset` for large copies and zero-fills (COMPLETED.md 10/11, `sret.go`).
- **[DONE 10/11] A large aggregate passed by value** — a pointer to a copy the caller makes
  as the argument is made (COMPLETED.md 10/11, `sret.go`).
- **[DONE 10/11] `sret` on the 68000** — not the M68k LLVM: a libc `memcpy` declared with an
  `i64` length, read as 0 by a 32-bit `size_t`. LLVM's intrinsics now (COMPLETED.md 10/11).

## Building for other platforms

`std.game` runs on every platform SDL3 does; `lyrac` builds for its host alone (Vega's modern
target, 10/11). In order:

- **[DONE 10/12] A target descriptor and `--target`** — `pkg/target`'s platforms; iOS, the
  Simulator and Linux built from a Mac and the suite run on each (COMPLETED.md 10/12). Left
  for the platforms that need them: `<root>/lib/<platform>/` is searched but nothing builds
  into it yet, and the bindings' C halves are built for the machine alone.
- **[OPEN] iOS** — SDL3 for iOS links (`tools/sdl3-ios.sh`), and a program over it is an app
  started through `SDL_RunApp`, playing in the Simulator frame for frame as on the Mac, full
  screen with an on-screen pad, and signed for and run on an iPhone (10/12: installed,
  launched and stopped by `devicectl`; a device needs Developer Mode on), and Vega builds and
  runs one there (File > Platform; `lyrac run --progress` ends `100% running`, and a killed
  run ends its app). Left: the pad and leaving the app checked by hand on a device, and
  `bindings.menubar`/`imgui` built per platform.
- **[OPEN] Android** — `aarch64-linux-android` is AAPCS64 too; the NDK's clang, a `.so` run by
  SDL's Java activity, an APK.
- **[OPEN] Windows** — `abi.Win64` (1/2/4/8-byte aggregates as integers, else by pointer, no
  `byval`), then the POSIX pieces the runtime and std call (`opendir`, termios, `getentropy`,
  `clock_gettime`, `localtime_r`, std.io's `open`/`mkdir`/`readlink`).
- **[OPEN] The web** — the backend's 64-bit pointers and `i64` `size_t` (`layout.go`'s
  `pointerSize`, the libc declarations, `coro.size.i64`) need a per-target width first.

## Target introspection

- **[IDEA] `size_of::<t>()` / `align_of::<t>()`** (plus stride) — answerable from
  `SizeAndAlign`, needing only a value-argument-less turbofish call shape.
- Target predicates (`is_x86`, `simd_width_of`) wait for real cross-compilation; `lyrac`
  has no notion of a target.

## Constraints are ordinary Lyra

**[IDEA]** A constraint becomes a `pure det` predicate with the value as `self`
(`type Lane = i64 where power_of(2)`), and the keywords are rewritten in the prelude:
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
- **[DONE 09/28] A written trait name needs its import.** Impl heads, bounds, supertraits
  and `Trait::method` resolved an unimported public trait, and W004 counted none of them as
  a use; a private trait now says E028. (COMPLETED.md, 09/28.)
- **[DONE 09/28] `Result<void, E>` has an `Ok`: `Ok()`.** A constructor applied to
  nothing builds a void payload; constructor arity is checked at last. (COMPLETED.md.)
- **[DONE 09/28] `lyra-W018` no longer advises `pure` on a trait against its signature.**
  Silent for a `mut`-parameter method; impl-only advice when a sibling impl has effects.
- **[DONE 09/28] A constructor wrapping a partially-solved one takes its context.**
  `Some(Ok(5))` under `Maybe<Result<i64, string>>` is matched argument-wise and narrowed.
- **[DONE 09/28] A construction with no context is lyra-E073**, naming the open parameter;
  a constructor turbofish (`Ok::<i64, string>(5)`) is read at last.
- **[DONE 09/28] Branches that each solve half are joined** (`if`/`match`,
  `typechecker/join_partial.go`).
- **[DONE 09/28] …at any depth**: `Some(Ok(1))` beside `Some(Err("x"))` merges through
  the payloads (`settleOpenArguments`).
- **[DONE 09/28] `if let … else …` is a value where a block's value is used**, as the
  `match` it means; any other statement tail there is refused by the typechecker.
- **[OPEN] An unannotated function ending in `if let … else …` infers `void`.** Only a
  declared return makes the tail a value, so a callback's statement-shaped `if let` keeps
  its meaning; decide whether inference should read it as a value, as it reads `if`.
- **[DONE 09/28] Bound-call effects group impls by trait identity**, not name: no false
  E007 from another module's same-named trait, and an impl written through an alias counts.
- **[OPEN] A partial ordering for floats.** A second `PartialOrd`-style type vs a widened
  `Ordering`; deferred until something needs it. A bit-pattern `total_cmp` for sorting
  floats is also unbuilt (`sort_by` with a comparator works meanwhile).
