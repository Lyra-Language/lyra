# Lyra — To-Do

What is **open**. Finished work and the reasoning behind it live in
[COMPLETED.md](COMPLETED.md); when an item below says something landed, that is where the
detail is. For what the compiler does *today*, read `CLAUDE.md` — this file is not a
status report.

Tags: **[OPEN]** not started · **[PARTIAL]** landed in part · **[DECIDED]** settled, not
built · **[IDEA]** not committed to · **[ROADMAP]**/**[DEFERRED]** deliberately later.

## Known bugs

None open. `break`/`continue` no longer leak an enclosing statement's pending temporaries
(closed 08/03, measured with LeakSanitizer both ways — 18 bytes before, none after). The
jump records its obligation and `resolveExitReleases` settles it once the function's CFG is
final, releasing only the temporaries whose producing block dominates the jump
(`dominators.go`). See COMPLETED.md.

Two more closed 08/03, both hazard-8 misses found while making a `weak` field
constructible and both wider than the feature that surfaced them: `resolveForLayout` could
not size a `shared` struct holding *any* generic field (`Maybe<i64>`), and
`resolveTypeIfKnown` rejected a return annotation against itself. See COMPLETED.md.

The typechecker's infinite recursion on a definition cycle closed 07/31 — an
in-progress guard in `inferExprType`, which is also what stopped `lyra-lsp` dying
mid-keystroke (see COMPLETED.md).

The lambda-context bug closed 07/31 (a lambda literal now takes its missing
parameter and return annotations from the context it appears in — see COMPLETED.md).

Before that, three closed 07/30 (borrowed-`string` use-after-free, anonymous-tuple literal
width, `i128` multiply link failure on Linux) — see COMPLETED.md.

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
  enforced at the instantiation.
  (A trait-impl method on a generic receiver **does** lower as of 08/03 — see COMPLETED.md
  and `pkg/backend/llvm/README.md`'s trait section. `Maybe<weak T>` parses and lowers as of
  08/03 too; the "does not parse" note that stood here was never true.)
- **[DONE 08/04] A generic body may call another generic at a variable-dependent
  instantiation.** `let get_or<t> = (o: Maybe<t>, d: t) -> t => o.unwrap_or(d)` compiles,
  and so does the free-function analogue. The fix is the one this entry called for:
  compose the caller's bindings into the callee's, done in the **driver**
  (`instantiations.go`) rather than the backend, because the per-specialization ownership
  pass runs off the instantiation set and a specialization discovered after it would fall
  back to the program-wide table — analyzed generically, where a type variable is not
  managed, so a `t = string` body would emit neither retains nor releases.

  The recursive-generic question this entry raised is answered by **bounding type depth**,
  not the count: polymorphic recursion (`f<t>` calling `f<Box<t>>`) is infinite, and what
  grows is the type. A count-only bound does terminate, but only after the set is both
  enormous and individually huge — measured at over a minute and a gigabyte, which is
  indistinguishable from the hang it was meant to prevent. Depth catches it in a few dozen
  cheap steps and reports it as what it is. Recursion at the *same* type is untouched.
  See COMPLETED.md.
- **[DONE 08/01] A binding's written generic parameter list is authoritative** — option (b)
  of the three that were on the table. A signature variable absent from a written list is
  `lyra-E031`; a declared parameter the signature never mentions is `lyra-W013`. The list
  stays **optional** (the lexical rule is unchanged, so `let unbox = (b: Box<t>, fb: t) -> t`
  is still generic with no list); what changed is that a written one must agree with its
  signature. `checker/generic_params.go`, reasoning in that package's README and in
  COMPLETED.md. The three type-variable walkers are now one (`types.CollectTypeVars`).

  - **[OPEN] The same reconciliation for *type* declarations, traits and impls.** A
    `struct`/`data`/named-`tuple` list, a `trait` list, and an `impl` list are each still
    unreconciled with the bodies they parameterize. Lower severity than the binding case was:
    a type declaration's list arity *is* load-bearing (checked against the type arguments at
    instantiation, `backend/generic_types.go`), so a mismatch there tends to surface as an
    arity error rather than as silence. Same pass, same walker; it needs the nominal-type
    question answered per declaration kind (a struct's own fields **are** its signature,
    unlike a signature that merely mentions the struct).

### Modules

Resolution, per-module scoping, `pub`, the implicit prelude, per-module name resolution and
symbol mangling all landed 07/30.

Types and traits got per-module identity 08/01 — `SymbolTable.Types`/`.Traits` are keyed
by the same `declKey` bindings already used, the typechecker's `resolvedTypes` cache and
the backend's `structTypes` registry with them. Two modules may each declare a private
`Point`, and a prelude type shadow no longer reaches another module. See COMPLETED.md.
- Out of scope by decision, none of it changing what a module's source looks like: package
  management, versioning, separate/incremental compilation.

**An import can make an ordinary name unusable, and the prelude cannot.** A `pub`
declaration takes the *bare* program-wide key whichever module made it (`declKeyIn`,
`pkg/ast/symbols/table.go`), so importing a module claims every name it exports. `import
util.seq` (which exports a `map`) plus a perfectly ordinary
`let map = (n: i64) -> i64 => n + 1` is a hard error:

```
shadow.lyra:3:1: error: function "map" is already defined at .../util/seq.lyra:3:20-6:2
```

The example used to be `import std.maybe`, which is no longer a module — the standard
library is one module as of 08/04. The bug is unchanged; only the illustration had to move
to a user module, and that it *can* is the point.

The comparison is what makes it wrong rather than merely strict. The **prelude** — the names
you never asked for — takes the *soft* path: `let unwrap_or = …` warns (`lyra-W012`) and the
user's declaration wins. The names you deliberately imported take the hard one. That is
backwards on both counts: the explicit act is punished, and the implicit one forgiven. A
user's read of that error is that the standard library owns `map` and their program may not
have one, which is not a rule this language means to have.

The mechanism to fix it already exists and is keyed too narrowly: `shadowsPrelude` qualifies
the shadowing declaration and leaves the bare key to the prelude, which is exactly the
"local declaration wins, the other stays reachable" rule wanted here. Two shapes to weigh —
(a) generalize that to any imported module, so a local declaration always wins and the
imported one is reached through its namespace (`seq.map(…)`), which is a form the language
supports for every module; or (b) qualify `pub` keys outright and
teach the bare lookups to consult the importing module's bindings. (a) is a smaller change
and keeps a genuine *cross-module* duplicate — two modules both exporting `map`, neither
importing the other — reportable as it is now.

Worth doing before the standard library grows: every name added under `pub` is currently a
name taken away from anyone who imports that module. It is also the constraint that decides
where a combinator could live at all, back when the library was split (see the 08/02
 discussion of `map`/`filter`); the whole standard library is one module as of 08/04.

**Less pressing since UFCS landed 08/03, and not fixed by it.** The combinators are now
reached as `m.map(f)`, dispatched on the receiver, so the bare name `map` no longer has to
mean one type's version — which was the reason this decided where `map` could live. The
collision itself is untouched: importing a module still claims every top-level name it
exports, so a program that imports one still may not declare its own. What changed is that
the workaround (don't import it) now costs less, which is exactly the kind of relief that
lets a real bug sit for a year. The fix is still (a) or (b) above.

**Receiver-keyed overloading (08/03) removed the other half of the pressure, and again did
not fix this.** Two `self` functions of one name may now be *declared* in one module when
their receiver heads differ, so `Maybe` and `Result` no longer need separate modules merely
to have `unwrap_or` each — which was the standing argument for the split. `std.maybe` was
folded into the prelude on 08/04 and deleted; the whole standard library is now one module,
so the shipped code no longer trips this at all.

**And the way it got tripped on the way there is the sharpest statement of the bug.** Adding
`map` for `Result` to the prelude while `std.maybe` still declared `map` for `Maybe` did not
produce an error — it *silently removed a method*. The prelude keeps bare keys, so its `map`
took the program-wide one; that flipped `shadowsPrelude("std.maybe", "map")` to true, which
pushed `std.maybe`'s `map` to a qualified key; and the UFCS rung consults exactly one
candidate by name, so `m.map(f)` on a `Maybe` found the `Result` overload, failed to match
the receiver, and reported "member access on non-struct type Maybe<i64>". Two features that
each work correctly, composing into a disappearance. Reproduced on the commit *before*
overloading landed, so it is this bug and not that feature.

**[DONE 08/04] UFCS resolves against every declaration of the name the file can reach** —
its own module, the prelude, and each imported module — and picks by receiver, rather than
taking the single `declKey` winner (`ufcsFunction`). That is the cross-module form of the
dispatch overloading already does within a module; the import requirement is unchanged, so
an unimported module's method is still unreachable. A file's own module wins a tie, and a
tie that survives that is reported with a qualifier the reader can type
(`` `dup.map(m, …)` ``) rather than broken by map-iteration order. See COMPLETED.md.

**It does not subsume (a) or (b), and the remaining half is worth stating precisely.** Only
the *method* form resolves this way. The **bare-call** form (`map(m, f)`) still goes through
the scope chain — module → prelude → global — so the prelude's `map` still shadows an
imported module's for a plain call, and two modules exporting one name still collide on the
bare key. The receiver is available at a bare call too (it is argument 0, which is the whole
premise of the desugar), so extending the same candidate gathering to `inferIdentifierCall`
is the natural next step; the key-level fix (a)/(b) is what settles the non-receiver names,
which have no receiver to disambiguate on and so cannot be fixed this way at all.

The LSP resolves a document's whole import graph as of 08/02 (see COMPLETED.md), which leaves
two editor features single-file where the program no longer is:

- **Rename declines a cross-file declaration.** Renaming a prelude function from a use site
  would need every unit's occurrences and a multi-file `WorkspaceEdit`; today
  `resolveRenameAnchor` returns false rather than splicing the new name into this buffer at
  the *other* file's coordinates. Declining is right until the multi-file form exists — the
  alternative is a silent corruption — but the message the user gets is nothing at all.
- **References only searches the open document.** Uses of a name in sibling modules are
  missing from the result, which reads as "no other uses" rather than "not looked".

Both want the same thing: walking every unit's program, which the server now has, keyed by
each node's `Location.File`.

## Constructor syntax — juxtaposition

**[DECIDED 08/02; BUILT]** `Some 42` is back alongside `Some(42)`. **One operand, never
curried** — there is no `Rect 3 4`, because a constructor's positional payload is already a
single anonymous tuple internally (`Rect(f64, f64)` → one `TupleType` param), so `Rect(3, 4)`
re-reads as "Rect applied to the tuple `(3, 4)`": the parens belong to the tuple, not to a
call, and the tree is byte-for-byte what it was.

*Why now, when it was removed 06/18.* That commit is explicit that the machinery existed
"solely to prevent a nullary constructor from greedily consuming the next statement **in the
terminator-less grammar**" — `let c = None` ⏎ `match c {…}` parsing as `None(match …)`.
Statements gained a terminator on 07/31, six weeks later. The sole stated reason expired.
It also closes a real asymmetry: `Some 42` has always been legal in *pattern* position
(`data_pattern` is `Name pattern`), so the two positions disagreed about the language's own
constructor syntax.

### `Some -1` is `Some(-1)` — application, not subtraction

**[DECIDED 08/02]** Application binds tighter than binary operators, and `negation` is in the
operand set, so `Some -1` applies `Some` to `-1`. `Some 42 ?? d` is `(Some 42) ?? d` and
`Some a + b` is `(Some a) + b`, the ML reading.

*Why this is not the Haskell ambiguity.* In Haskell `Some -1` is genuinely ambiguous because
any identifier can be a value, so the subtraction reading has an operand. **Lyra's lexer has
already split the cases**: `identifier` is `/(_[a-zA-Z0-9_]+|[a-z][a-zA-Z0-9_]*)/`
(lowercase-leading) and `const_identifier` is `/[A-Z][A-Z0-9_]*/` (SCREAMING_CASE). A
PascalCase name in expression position is therefore *always* a constructor — never a
variable, never a constant — so the subtraction reading has nothing to bind and `MAX - 1`
(a constant) is untouched arithmetic. The previous incarnation of the rule reached the same
answer, keeping `negation` "so `Err -1` … still work[s]".

- **[OPEN] The residual hazard: an operator overload on a data type.** `-` is overloadable
  (`(_-_)` in a trait), so `Empty - 1` on a `data` type with a `Sub` impl now parses as
  `Empty(-1)` rather than as subtraction. It needs all of: a `-` overload on a *sum* type, a
  **nullary** constructor as the bare left operand, and an operand that is an atomic
  constructor operand. Contrived, and `(Empty) - 1` still says the other thing. Worth a lint
  if it ever bites — a `-` overload on a data type whose nullary constructor appears bare on
  the left — rather than a grammar rule, since the grammar cannot know about the impl.
- **[IDEA] Warn on `Some - 1` written with spaces on both sides.** Same parse as `Some -1`,
  but it *reads* as subtraction. Not built: it has exactly one valid meaning, so a warning
  is arguably noise. Revisit if it confuses anyone.

### The operand must be atomic

A juxtaposed operand is a literal, a name, a nullary constructor, a negated literal, a
struct/array literal, or another application. A **compound** operand — a call, member access,
index, `?`, deref, arithmetic — is parenthesized: `Ok(f(y))`, `Some(a.b)`. Those are the
spellings they already had, so nothing regressed, but `Ok f(y)` is a parse error rather than
sugar.

That is forced, not chosen. **Every postfix form is headed by `_postfix_expr`, which reaches
`parenthesized_expr`**, so admitting any of them as an operand also admits `Some (x)…` while
the parser looks for the `.`/`[`/`?`/`^` — which reopens a third reading of `Some(x)` and
tips the pre-existing parameter-position race, so `(Some(x): Maybe<i64>) -> i64` stops
parsing as a destructured lambda parameter. No conflict entry fixes it; the reading has to
not exist. Found by bisecting the operand set against the corpus — **the "unnecessary
conflict" warnings are unreliable in this region**, so verify against corpus, not warnings.

- **[IDEA] Admit compound operands after all**, if the parameter-position race can be settled
  another way (restructuring `pattern` the way the 07/16 fix did, rather than by conflicts).
  `Ok f(y)` reading as sugar would be nice; it is not worth a fragile parse.

*Cost:* 5,537 → 6,606 states, `parser.c` 9.4 MB → 12.0 MB (+19% / +28%). Juxtaposition is
genuinely expensive in an LR automaton — still an order of magnitude below the 62,663-state
`lambda_expr` incident, but this is now the second-largest single feature in the parser.
Re-measure with `--report-states-for-rule -` before adding anything else here.

*Implementation:* the collector erases the spelling — `collectAppliedConstructorExpr` builds
the **same named `TupleLiteralExpr`** the parenthesized form builds, so the typechecker,
purity, ownership, exhaustiveness and the backend never learn juxtaposition exists. Proof
that the erasure is exact: `Some None` and `Some(None)` fail with the identical
(pre-existing) nested-generic lowering error.

## Array comprehensions

**[DONE 08/04]** `[ x in xs | x % 2 == 0 | x * 2 ]` — generators, optional guards, result —
collects, type-checks and lowers. The grammar had them from the start; nothing else did, so
the expression reported `unknown expression type "array_comp_expr"`.

They matter beyond convenience: **a comprehension is the only way to build an array.** There
is no growth operation, and a spread in an array literal (`[0, ...xs]`) parses but is not
collected, so before this the prelude could not have a `map` for `[]t` at all — the natural
recursive `[head, ...tail]` formulation needs both of the missing pieces. `map` for arrays
is now one line in `std/prelude.lyra`, and a third receiver head beside `Maybe` and
`Result`.

A comprehension is always `[]u`, never `[N]u`, even with no guard: a guard decides at run
time how many elements survive, and adding one to a comprehension should not change its
type. Capacity is the product of the source lengths and the box records the survivor
*count* as its length — the reasoning for over-allocating rather than counting twice or
growing is in `pkg/backend/llvm/array_comp.go`.

Still open, each refused loudly rather than approximated:

- **[OPEN] A range or string source.** `[ x in 1..=10 | x * x ]` is the grammar's own
  example and does not lower. A range needs its iteration count derived from
  start/end/step including the inclusive and negative-step cases; a string yields *runes*,
  whose count is not its byte length, so the capacity rule needs a different answer as well
  as the walk.
- **[OPEN] A generator whose source depends on an earlier generator** —
  `[ row in grid, cell in row | cell ]`. Sources are materialized once before the loops,
  which is what makes the capacity computable; a dependent source would need
  materialization inside the enclosing loop and a capacity that is not known up front.
- **[OPEN] `result_expr` is narrower than an expression.** The grammar admits
  `_math_operand`, a tuple, the struct literals and an array literal — so `[ x in xs |
  "a" ++ b ]` is a *syntax* error. Worth widening in the grammar rather than working around.

**[OPEN] `noalloc` does not see an array allocation** — pre-existing, found here.
`allocContext.allocates` counts only values whose allocation flavor is `shared`, and a
`[]T` box is heap-allocated without being one, so `pure noalloc (…) -> []i64 => [1, 2, 3]`
is accepted. A comprehension inherits the same hole. The fix is for the alloc effect to ask
about the *representation* rather than the flavor.

## Ranges

The three range grammars were unified 08/01 (`rangeBounds`, one `range_end_operator`,
`lyra-E032` for a missing end operator at all three sites, open-ended patterns,
`lyra-E033` for an ill-formed step). See COMPLETED.md. What is left:

- **[OPEN] A `step()` constraint is not enforced against values.** Nothing reads
  `types.StepConstraint` after collection, so `newtype Quarter = f32 where range(0..=100),
  step(0.25)` validates the *step* but still accepts 0.3. Unlike `range(…)`, which the
  value-range pass checks (`lyra-E023`), a step is a divisibility test — cheap for a
  compile-time constant, a runtime check otherwise, which is the decision to make first.
- **[OPEN] Descending ranges have no semantics.** `InvalidStepReason` deliberately does not
  judge a negative step, because the language has never said what `10..=0` or `0..=10:-1`
  means (an expression range has no descending form today). Settle it before anything reads
  the sign — the well-formedness rule is shared by both step spellings, so a guess made in
  one place silently becomes the language's answer in both.
- **[IDEA] Open-ended expression ranges** (`0..`), which need a lazy/infinite iterator. The
  pattern and constraint spellings have open bounds; the expression one deliberately does
  not, and that asymmetry is documented in `tree-sitter-lyra`'s `rangeBounds` rather than
  left to be rediscovered.

## Language design — Pit of Success

Make the safe path the default.

### 1. Must-use `Result`/`Maybe` + `?` propagation

Canonical Result/Maybe identity is settled (a `CanonicalKind` stamp via the
name-independent `@builtin` attribute), and `?` checks the operand's error type against the
enclosing return.

- **[OPEN]** From-style **declared error conversion**, once a conversion trait exists.
  Today `?` is assignability-only.
- **[DONE 08/01] `?` lowers.** It had type-checked and then failed the build
  (`expression lowering not implemented for *ast.TryExpr`), so no program could use the
  language's primary error-propagation operator; found by exercising the prelude, 07/30.
  `pkg/backend/llvm/try.go` lowers it as the match it is — tag test, unwrap on success,
  and on failure rebuild the failure variant **at the enclosing function's return type**
  (the operand and the return are different instantiations, so the union cannot be
  forwarded) and `emitReturn`. See COMPLETED.md; the ownership half is below.
  - **[DONE 08/03] A temporary produced by a *sub-expression* of the operand no longer
    leaks on the propagating path.** `f(g())?`, where `g`'s owned result was consumed by a
    borrowing parameter. The propagating path now releases those temporaries into its own
    block (`releaseTempsOnExit`) instead of holding the whole pending list back. Measured
    both ways with LeakSanitizer on Linux: 19 bytes in 1 allocation before, none after.
    See COMPLETED.md.
- **[DONE 08/03] Shadowing a marked canonical type now explains itself.** `?` on a
  user's own `data Maybe` reported `` `?` operand must be a Result or Maybe, got Maybe ``
  — true, and useless, because it names the answer as the problem. The rule was kept (the
  marker confers the kind; a same-named unmarked type is ordinary) and the message
  replaced: the collector stamps `ShadowedCanonical`, and `?` says whether the shadow
  re-declares the prelude's type or is a different type wearing its name, each with the
  fix that fits.

  **The advice this entry used to recommend does not work**, which is the part worth
  keeping: "mark it `@builtin(Maybe)`" is `lyra-E017` (duplicate claim), because the
  prelude already holds the kind. A program can have exactly one canonical Maybe. The
  shipped message therefore never mentions `@builtin` — it says remove the declaration or
  rename it, and both are covered by tests that run the suggested fix. See COMPLETED.md.
### 2. Checked arithmetic by default; wraparound explicit

Trap-on-overflow covers all integer arithmetic, `wrapping_*`/`saturating_*` are the lowered
escape hatches, and the value-range pass both diagnoses definite faults (`lyra-E020`–`E023`,
`W011`) and elides the traps it can prove unnecessary. That backlog is clear.

- **[OPEN] `checked_*`** — returns `Maybe<T>`. It was blocked on a prelude; the prelude
  landed 07/30 and generic types 07/29, so it is unblocked. Shares the unresolved
  return-type-from-context problem with #5's narrowing conversions.
- **[DONE 08/02] Bitwise and shift operators** — `& | ~ << >>`, prefix `~`, and the five
  compound assignments. An out-of-range shift amount traps
  (`lyra_panic_shift_overflow`), which is the same call div-by-zero makes and for the
  same reason: LLVM's shifts are UB there, so the alternative is a silently
  target-shaped answer. See COMPLETED.md. Two follow-ups:
  - **[DONE 08/02] The value-range pass tracks bitwise results.** `andI`/`orI`/`xorI`/
    `shlI`/`shrI` sit beside `addI`/`subI`/`mulI`, so `(x & 0x0F) + 1` now proves its
    addition safe and drops the trap. Each rule widens rather than guess: `&` needs one
    operand known non-negative (which is the masking case, and holds whatever the sign
    of the masked value), `|`/`~` need both, and the shifts need a bounded count.
    Soundness is checked by exhaustive brute force over every interval of a small
    width. See COMPLETED.md. Still imprecise on purpose: `|`/`~` over a possibly
    negative operand, and `&` where *both* sides may be negative, all widen to ⊤.
  - **[DONE 08/02] A variable shift amount elides its check when the range pass can
    bound it.** `NoShiftOverflow(e)` joins `NoDivZero`/`NoOverflow`; the proof
    obligation mirrors the emitted check exactly (an *unsigned* compare against the
    width, so the count needs a lower bound of 0 and a finite upper below the width).
    A constant in range was already folded at lowering; this covers the variable case,
    e.g. a count refined by `if n < 8`.
  - **[DONE 08/02] `x <<= n` types like `x = x << n`.** `checkAssignToBinding` split
    into `resolveAssignTarget` (the target's existence, mutability and type) plus the
    value check, so the shift path can apply the first and type its count as a count.
    A rejected target still returns its type, because every caller checks the value
    either way — a refused assignment must not hide the errors inside its value.
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

- **[PARTIAL] Effect polymorphism over function-typed parameters.** Both halves landed
  07/31; details in `checker/README.md`, reasoning in COMPLETED.md.
  - The **inferred** half: a function's stored effect is its *base* plus its callback
    parameters, and a call site pays base ∪ the effects of the arguments supplied for them.
    `unwrap_or_else`, `ok_or_else` and every prelude combinator are annotated `pure noalloc` and
    callable from `pure` code, with an impure callback rejected at the call site.
  - The **declared** half: `lambda_type` takes the same `pure`/`det`/`noalloc` modifiers a
    lambda value does (`f: pure () -> t`), carried on `types.LambdaType` and enforced by
    `checkDeclaredCallbackBounds` at *every* call site. A bounded parameter is not
    polymorphic, so its function is pure for every caller. The standard library
    deliberately does not use it: a bound on `unwrap_or_else` would forbid a fallback that
    logs, and the inferred half already keeps pure callers pure.

  What is left:
  - **[OPEN] Callbacks reached through anything but a parameter or a binding** — a struct
    field, a call result, an array element — stay conservative (`AllEffects`). Multi-clause
    lambdas are no longer among them: they are desugared into a single-body match before the
    effect passes run, so their parameters are an ordinary indexed list.
  - **[DONE 07/31] Trait-impl methods** are polymorphic over their callbacks, and a bound
    written in a trait signature (`apply: (Self, pure () -> i64) -> i64`) is enforced at
    call sites. Note the receiver offset: signature parameter 0 is `Self`, which sits
    outside `call.Arguments` (`methodArgumentAt`).
  - **[OPEN] A declared bound is not inferred.** Passing an unconstrained parameter into a
    bounded slot is rejected rather than propagating the requirement outward, so a wrapper
    must declare its own bound by hand. Inferring it (a caller's parameter *becomes*
    bounded because it is forwarded into a bounded slot) is the natural next step and is
    what would make bounds composable without annotation churn.

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
- **[DONE 08/03] An array element carries an allocation or `weak` modifier** —
  `[]shared Node`, `[3]weak Observer`, `[16]stack Vec3`. A `tree-sitter-lyra` change only
  (`_element_type`); **nothing in this repo needed changing**, because the checking had been
  written for a syntax that did not exist — `firstAllocationMismatch` already recursed into
  array elements. See COMPLETED.md.
- **[DONE 08/03] A `weak` field is constructible** — `Maybe<weak T>`, so a cycle back-edge
  is optional and "no back-edge" stays distinct from "the referent is gone". **The premise
  this item was filed under was wrong twice over**, which is the part worth keeping: the
  grammar parses `Maybe<weak Node>` and always did (`parameterized_type`'s arguments are
  `$.type`, which includes `weak_type`), so no `tree-sitter-lyra` change was needed, and the
  real blockers were two missing switch cases with nothing to do with `weak` — a `shared`
  struct holding *any* generic field (`Maybe<i64>`) failed identically. See COMPLETED.md.
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

## Type resolution

- **[OPEN] Fold `resolveType` and `resolveTypeIfKnown` into one walk.** They recurse over
  the same composites and differ only at an unknown *name* — report "unknown type", or hand
  the type back untouched — so the entire recursion is duplicated for a difference that
  lives in one leaf. That duplication has now drifted once (08/03: the twin was missing
  `ParameterizedType` and `*LambdaType`, giving "expected `Maybe<weak Node>`, got
  `Maybe<weak Node>`"), and it is the same shape as the `collectTypeVars` /
  `mentionsTypeVar` split that `types.CollectTypeVars` already fixed by unification —
  where taking the union of the copies turned up composites *neither* had.
  *Shape:* one walk taking the leaf behaviour (a callback, or a `report bool`), with both
  names kept as thin wrappers so no call site changes. Note the two also differ in a second,
  easier-to-miss way — `resolveType` recurses into an alias chain, caches by resolved
  identity, checks visibility, and guards circularity, none of which the twin does — so the
  fold must keep those on the reporting path rather than assume the walks are identical
  apart from the leaf.

## Traits

### [OPEN] `Show` — no value of a generic type can be formatted

There is no way to render a value whose type is a **type variable**. Interpolation and
`print` are checked against `isPrintableType` — string, the integers, the floats, `bool`,
`rune` — because those are exactly the types the backend has a formatter for, and it picks
one per *concrete* type rather than through any signature. A `t` has no representation, so
there is nothing to pick:

```
lyra-E001: cannot interpolate a value of type t (expected a string, an integer, a float, bool, or rune)
```

Found by writing the prelude's `expect` (08/04). The natural first draft reports what it
got — `panic("expected ${value}, got ${v}")` — and none of it is expressible. The shipped
signature takes the message as a `string` instead, which moves the formatting to the caller,
who has the concrete type; that is the conventional shape anyway (it is Rust's `expect`), so
the gap cost nothing there. It will cost something the first time a combinator genuinely
needs to describe its own payload — `assert_eq` is the obvious one, and it needs `Eq` too.

What it takes, and why it is more than declaring a trait:

- a `Show` trait in the prelude, with impls for each printable primitive;
- **`where` bounds enforced at instantiation**, which is its own open item above — they are
  collected today and not checked, so `where t: Show` would not actually constrain anything;
- interpolation and `print` routed through bound dispatch when the operand's type is a
  variable, rather than through the fixed printable set.

Worth deciding alongside `Eq`/`Ord` rather than alone: they share the "core traits the
compiler knows by name" question, which is the same one `@builtin(Maybe)`/`@builtin(Result)`
already answer for types — a marker conferring identity, so the trait is recognised by the
marker and not by being spelled `Show`.

**Not a blocker for equality, oddly.** `!=` on a bare type variable type-checks today
(verified 08/04), so a combinator *can* compare its payload without any bound — which is
either a convenience or a hole depending on what `Eq` is meant to mean, and is worth
settling when that trait is designed.

### Trait machinery

Trait-method lowering landed 07/30: an impl method lowers to a function taking the receiver
first, and dispatch is static. That entry used to end "and a generic impl needs no extra
machinery" — it needed exactly the machinery a generic function needs, built 08/03: one
emitted function per binding set, the body lowered under those bindings, and an ownership
table per specialization. See COMPLETED.md.

- **[PARTIAL] Borrow modifiers on trait signatures.** `ref` and `mut` landed 07/31:
  `bump: (mut Self) -> void` writes through to the caller, and `peek: (ref Self) -> i64`
  borrows without copying. The grammar always accepted them — `trait_method_signature` is an
  aliased `lambda_type` whose `parameter_type` has always carried an optional
  `type_modifier`; what dropped them was `Collector.parseParameterType`, plus the absence of
  a field to hold them (`types.ParameterType.Borrow` now exists beside the allocation
  `Modifier`).

  - **[DONE 08/03] `own` is supported**, on parameters and on the receiver. lyra-E030 is
    retired. The restriction had named its own prerequisite — teach the ownership pass
    about method bodies — and that was the smaller half of what it turned out to be
    guarding; see COMPLETED.md for the two resolution gaps behind it.
  - *Watch for*: the rule that any code rebuilding a `types.ParameterType` field-by-field
    silently drops new fields. Three sites did (`substituteSelf`, the lambda→signature
    conversion in `typechecker_traits.go`, and `lambdaSignature`), and the symptom was a
    `mut` receiver that parsed, type-checked, and quietly wrote to a copy.

## Method syntax for free functions (UFCS)

**[DECIDED 07/31; BUILT 08/03]** UFCS — `x.f(y)` resolving to a free function `f(x, y)` —
**opt-in via a first parameter named `self`**. A function written `(self: Maybe<t>, …)` is
callable both ways; every other function stays call-only. See COMPLETED.md, and
`pkg/analyzer/typechecker/README.md`'s last section for the mechanism.

Two decisions taken at build time, neither of which the design below had settled:

- **An import is required** to call into another module method-style. A file's own module
  and the prelude need none. The alternative — any `pub` function in the program being
  reachable through a value of its type — reads better in the abstract and worse in
  practice: whether your call compiles would depend on whether some *other* file imported
  that module.
- **An `own` receiver is refused**, so a move always looks like a call.

Still open here: **the two spellings are one call, but only the desugared one is what later
passes see.** That is what makes the feature cheap, and it means a diagnostic reported
against an argument index can name a position the reader did not write (argument 1 of
`m.f(x)` is `m`). Nothing does that today — the messages in play name parameters, not
indices — but a future one could, and the fix belongs wherever that message is written.

*Why it earns its place here, rather than being sugar.* A free function's name is a
program-wide land grab today, which is the whole reason the standard library splits
`maybe.map` from `result.map` and why putting either in the prelude claims the name `map`
for one type forever. UFCS disambiguates on the **receiver type** — precisely the axis that
distinguishes them — so both are reachable as `m.map(f)` / `r.map(f)` from free functions in
different modules, with no overloading. The pressure to put combinators in the prelude at all
goes away: a module's functions become reachable through values of its type without an import
binding their names.

It also **routes around trait type-parameter binding**, which is what blocks method-form
combinators today (`typechecker_trait_dispatch.go`: a method returning the impl's element
type "is not yet fully instantiated"). UFCS delivers the same ergonomics without traits, and
is much the cheaper of the two — which is the sequencing argument: do this before anyone
reaches for the trait feature to get `m.unwrap_or(0)`.

*Why opt-in rather than universal.* The author decides what is a receiver, so adding a helper
to a module cannot change what `x.f()` means elsewhere in it; `self` already spells "receiver"
in trait impls, so the language gains no second word for the same idea; LSP completion on `m.`
stays a curated set rather than every function in scope; and nothing existing changes meaning,
so there is no migration. Note that **Odin, which Lyra borrows from elsewhere (`%%`, the
`rune` naming), deliberately rejected UFCS** on the grounds that it obscures where a procedure
comes from — the `self` opt-in is the answer to exactly that objection.

*Resolution order:* struct field → trait method → UFCS → builtin. A real impl beats a free
function, and user code still shadows a compiler builtin, both matching the existing ladder in
`inferMemberCall` (and its mirror, the backend's `lowerBuiltinMethodCall`).

*Open sub-decision:* **whether `own self` may be called method-style.** `x.consume()` moving
`x` is caught by use-after-move (`lyra-E019`), but the receiver syntax hides the move, which
cuts against making costs visible. Leaning toward refusing UFCS for an `own` receiver, so a
move always looks like a call.

*One hazard to build against, not discover.* **The purity pass indexes arguments
positionally** — `callableParams` maps a parameter name to its position and
`checkDeclaredCallbackBounds`/`callEffect` read `call.Arguments[idx]`. At a UFCS call site the
receiver sits *outside* `call.Arguments`, so every index shifts by one: without handling,
`m.unwrap_or_else(f)` checks `f` against the wrong parameter's declared bound — silently, since
both are function-typed. The backend's `lowerDirectCall` argument coercion has the same shift.

*Sequencing (superseded).* This said "after the two open trait gaps above, since UFCS adds a
fourth caller into the same member-call resolution path". It went first instead, and the
reasoning was wrong in a way worth keeping: those gaps are `own` receivers and ownership
analysis of method bodies — orthogonal to the ladder, which UFCS extends by inserting one
rung that returns early. What actually made the order right is that UFCS rides the path that
*works*: a generic free function monomorphizes today, while a generic trait impl's method
does not (it reaches the backend with `Maybe<t>` unspecialized — "match on Maybe<t> not
implemented yet"). Method ergonomics through traits needs that built first; through UFCS it
needed nothing.
