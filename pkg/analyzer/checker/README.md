# `pkg/analyzer/checker` — standalone AST passes

Standalone AST-level semantic passes. Most (e.g. `use_before_declaration.go`, `shadowing.go`,
`unused_variables.go`) run after collection but before typechecking and only need the AST.
**`purity.go` is the exception** — `CheckPurity(program, methodTable)` takes the typechecker's
`*typetable.MethodTable` (nil-safe) so a pure function/method calling a trait method can be
checked against the method it actually dispatches to; it must run *after* `typechecker.Check`,
not before (see `cmd/lyra-lsp/main.go`'s ordering).


## `use_before_declaration.go`

`CheckUseBeforeDeclaration(program) []UseBeforeDeclarationError`
  Two-pass algorithm: collect all names declared directly in a block, then walk in order
flagging any use of a not-yet-seen name. A lambda body is checked with its **parameters
pre-seeded as already in scope** (`checkStatementsInScope`, 07/30) — a parameter is declared by
the signature, so a `let` of the same name *shadows* it rather than using it early. Without
that, `(s: string) => { let s = s ++ "!"  s }` was flagged, which mattered because shadowing is
the replacement for reassigning a borrowed parameter (`lyra-E025`).

## `purity.go`

`CheckPurity` enforces `pure` (lambdas and, since 06/24/26, trait-impl methods): no captured
mutation, no calls to non-pure functions/methods, no `await`. Both `CheckPurity` and the
call-site "non-pure method" check consult `inferImpurity`'s bottom-up fixpoint (not just the
explicit `pure` flag) for whether a callee is actually pure — it runs over free functions and
trait-impl methods jointly (`collectMethodImpls`), since either can call the other.
`InferredPureFunctions(program)` separately exposes that result by name for top-level functions
only (a name-keyed map can't disambiguate methods across impls). Method-to-method calls are now
tracked: `checkTraitImplMethodBody` (`typechecker_traits.go`) type-checks each impl method body
— verifying it against the trait's declared return type (Self and the trait's own params
substituted, mirroring `checkLambdaBody`) and populating `MethodTable` with any `.`-calls
inside, so `methodEffects` finds them in the fixpoint.

## `effects.go`

`checker.Effect`, a bitmask generalizing the old impure/pure bool (`EffectMut`,
`EffectInput`/`EffectOutput` — the split of the old `EffectIO`, kept as their `Input|Output`
alias — `EffectAlloc`, `EffectRand`, `EffectTime`; `EffectNone` = pure). `inferImpurity`
accumulates this per function/method (set-monotonic fixpoint) instead of a bool;
`InferredEffects(program)` exposes it by name. Two named-bound masks over the row:
**`PurityEffects = Mut|Input|Output|Rand|Time`** (everything but Alloc — `pure` and
`InferredPureFunctions` are defined against it, so `EffectAlloc` is *orthogonal*, a `pure`
function may allocate) and **`DetEffects = Input|Rand|Time`** (⊆ PurityEffects, so `pure` ⟹
`det`; `det` forbids only the non-determinism sources, permitting Mut/Alloc/Output — the
input-vs-output IO split is what lets `det` allow logging). `EffectAlloc` detection
(`purity.go`'s `allocContext`/`buildAllocContext`): a construction expression
(`StructInstanceExpr`/`TupleLiteralExpr`/`DataConstructorExpr`) whose recorded `TypeTable` type
is `shared` — i.e. the value is *used* as `shared` (allocation is a use-site flavor, so an
annotated binding `let n: shared Node = Node{…}` records the flavor onto the construction via
`checkVarDecl`, and `allocContext.allocates` reads it via `AllocationOf`), *unless* lexically
inside a `with`-arena block (a hard-coded discharge — Lyra has no general effect handlers).
`CheckPurity` is threaded the `TypeTable` for this; the AST-only `InferredEffects` helper has no
`TypeTable` and so never sets `EffectAlloc`. A `shared` construction in a return/argument
position (flavor not yet recorded on the construction node), implicit allocation (dynamic
arrays/strings, escaping closures), and precise arena escape are deferred to a future
layout/escape pass. An **unresolvable external call** (no local lambda, builtin, or type
conversion) conservatively taints `AllEffects` (`PurityEffects | EffectAlloc`) — everything,
including Alloc, so `noalloc` flags it too (we can't verify it doesn't allocate).
`builtinEffects`: print/println→Output, read→Input, write→Input|Output, `await`→Input,
`Random.global()`→Rand, `wallClock()`→Time, **`panic`→None**. Only *ambient* rand/time sources
carry the bit — a threaded RNG's `rng.next()` or a passed-in `tick` (reached through a local
binding) is ordinary `mut`/`own` data, which is what lets `det` permit seeded randomness and
sim-time. User surface is the `pure`/`det`/`noalloc` ladder — see `todo.md` FP/Imperative #5.

**`panic` is EffectNone**, so it is callable from `pure`, `det` and `noalloc` alike. It writes
to stderr and exits, which argues for Output — but *every integer operation in this language
can already panic*: `a + b` traps on overflow, `xs[i]` out of bounds, a non-exhaustive match on
fallthrough, all from inside `pure` functions, all writing the same message to the same fd and
exiting with the same code. Classifying the explicit form as impure while the implicit ones are
free would make `pure` mean "cannot panic *on purpose*". The rule taken instead: purity is about
what a function returns and mutates, not whether it terminates. (Koka, which tracks `exn`/`div`
as effects in their own right, takes the other road — worth revisiting if a catchable panic or a
totality guarantee is ever wanted, since both need that bit.)

**Effect polymorphism over function-typed parameters.** A higher-order function's effects are
not a property of the function alone — what `unwrap_or_else(m, f)` does depends entirely on
`f`. A function's stored effect is therefore its **base** (what its own body does) plus its
**callback parameters** (`callableParams` — the function-typed ones it calls, tracked in the
same fixpoint as the effects, since finding one changes a caller's effect and finding an effect
can reveal one a round later). A call site pays base ∪ the effects of the arguments actually
supplied for those parameters (`callEffect`/`argumentEffect`), so one definition gives
`unwrap_or_else(m, () -> i64 => 0)` pure and `unwrap_or_else(m, () -> i64 => log())` impure.

Before this, a call through a parameter hit the unresolvable-callee branch and tainted
`AllEffects`, which spread to every caller: **no callback-taking function was callable from
`pure` code at all**, which is the entire std.maybe/std.result combinator layer.

Two consequences that are the point rather than side effects:

- **An annotation constrains a function's own body.** `pure` on a higher-order function claims
  "contributes no effects of its own", not "no effect can occur through me" — the second is not
  the function's to promise without constraining its callback — that is what the declared half
  below (`f: pure () -> t`) is for. This is what lets the prelude annotate
  `unwrap_or_else` `pure noalloc` without constraining its callers; a caller passing an impure callback is still rejected, at the
  call site, with the diagnostic naming the **argument** rather than the innocent callee.
- **A callback passed onward stays polymorphic**: `(f) => or_else(m, f)` is polymorphic in `f`
  too, so combinators built from combinators are not poisoned by the hand-off.

**Trait-impl methods are polymorphic too.** `methodEffects` returns a base effect plus callback
parameters exactly as `lambdaEffects` does, and `methodCallEffect` charges a call site for the
arguments it supplies. Their parameter *types* live only in the trait declaration (an impl binds
patterns, not typed parameters), so `collectMethodSignatures` maps each impl method to its
declared signature — which is also what makes a bound written in a trait signature
(`apply: (Self, pure () -> i64) -> i64`) enforceable, via `signatureBound`.

**The receiver offset is the hazard in that path.** A trait signature counts `Self` as parameter
0, but `x.foo(a)` puts the receiver *outside* `call.Arguments`, so signature index i is
`Arguments[i-1]` (`methodArgumentAt`). Reading `Arguments[i]` instead checks every callback
against the argument one place to its right — silently, because two function-typed arguments
type-check against each other's parameters perfectly well. A test with two callbacks in
different positions is what makes that observable, and there is one.

Deliberately still conservative: a callback reached through a struct field, a call result or an
array element, and multi-clause lambdas (per-clause patterns give no index to match an argument
against).

**The declared half: `f: pure () -> t`.** A parameter's *type* may carry the same
`pure`/`det`/`noalloc` modifiers a lambda value does (`tree-sitter-lyra`'s `lambda_type`),
collected onto `types.LambdaType`'s `IsPure`/`IsDet`/`IsNoAlloc`. A parameter carrying one is
**not** effect-polymorphic: what calling it can do is known from the signature, so
`declaredBound`/`boundEffect` charge exactly what the bound still permits and the enclosing
function is pure *for every caller* rather than at the call sites that happen to pass a pure
callback. That is the claim a signature could not make before, since purity was not part of a
function type at all.

`checkDeclaredCallbackBounds` enforces it at **every** call site, not only inside `pure`
functions: the bound is a property of the callee's signature, so an impure program may not
quietly hand an impure callback to a `pure`-declared slot. What it compares is the argument's
**inferred** effect, not its annotation — requiring the word `pure` on every lambda literal a
program writes would cost more than the bound is worth, and inference is precisely what this
pass has and the typechecker does not. That is also why `isAssignable` deliberately lets two
function types differing only in bounds through in either direction: a shape mismatch there
would report "cannot assign `() -> i64` to `pure () -> i64`", which explains nothing, instead
of "this argument mutates state outside itself". `TypesEqual` *does* distinguish them, so
identity questions (trait-signature matching) still see two types.

A bound composes through a forward — a constrained parameter satisfies a constrained slot from
its own declared type, since a parameter has no body to inspect — and an **unconstrained one
cannot**: it promises nothing, so `strict(g)` with `g: () -> i64` is rejected with a message
saying to declare it. A bound the compiler cannot check is not a bound.

Note what this does *not* change: an unbounded parameter keeps the inferred behaviour, so
adding the declared half did not make every callback-taking function strict. The standard
library deliberately leaves its combinators unbounded — `unwrap_or_else` with a `pure` bound
would forbid a logging fallback, which is a legitimate use — and relies on the inferred half
to keep pure callers pure.

**A namespace-qualified callee resolves through its last segment** (`resolveCallee`). `maybe.map`
had no resolution at all, so *every* cross-module call from a `pure` function was reported
impure — module paths are merged into one program before this pass, and a `pub` name is
program-wide unique, so the last segment is the same lambda. The fallback is taken only when the
object segment names no binding, mirroring the backend's `namespaceCallee`: with a local `math`
in scope, `math.double` is a field read, and resolving it to another module's `double` would
attribute the wrong body's effects.

**Resolution order: scope, then the builtin table** — in `isImpureCallee` and in both
`lambdaEffects`/`methodEffects` call cases, matching how the typechecker resolves a call
(`print`/`println`/`panic` are consulted only when scope resolution misses). Consulting the
table first classified a *user's own* function by the builtin's entry: a user `print` that was
pure got reported impure, and — once `panic` was in the table as EffectNone — a user `panic`
that mutated would have been waved through. The name is not the callee.

## `try_outside_result.go`

`CheckTryOutsideResult(program, symTable)` (`lyra-E008`) flags a `?` whose nearest enclosing
function doesn't return a canonical Result/Maybe. It reads the same canonical identity the
typechecker uses via `canonicalKindOfName` (the read side of the collector's
`resolveCanonicalTypes` stamp), so the context check and the typechecker's operand/kind checks
agree — a function returning a same-named-but-differently-shaped `data Result` is *not* a valid
`?` context.

## `effect_bounds.go`

`CheckEffectBounds(program)` (`lyra-E015`) errors when a lambda, a trait-impl method, or a
**trait method declaration** (`trait X { pure det foo: … }`) carries both `pure` and `det`: two
rungs of the same correctness axis (`pure` ⊆ `det`), so annotating both is contradictory.
`noalloc` is an orthogonal resource bound and is never flagged. AST-only, wired into the LSP
before typechecking. **`det`/`noalloc` enforcement** lives in `purity.go`'s
`checkBoundedEffects` (run inside `CheckPurity`, `lyra-E016`): it checks each callable's full
inferred (transitive) effect set — `det` against `DetEffects`, `noalloc` against `EffectAlloc` —
reporting once at the callable's location (`pure` keeps its fine-grained per-op walk,
`lyra-E007`). **Per-trait-method bounds are a contract:** a trait method may be declared
`pure`/`det`/`noalloc` (`TraitMethod.IsPure/IsDet/IsNoAlloc`); `checkTraitMethodBounds`
(`purity.go`) checks each impl of it against the *effective* bound — the impl method's own
annotation OR the trait's — so an impl of a `pure`-declared method is enforced pure even without
its own `pure` marker, and a `where t: Show` bound to a `pure`-method trait carries that
guarantee.

## `use_after_move.go`

`CheckUseAfterMove(program, symTable, tt)` (`lyra-E019`) flags reading a binding after its value
was moved into an `own` parameter — the definite-move analysis of `todo.md` FP/Imperative #6 /
borrow-model #8(a). A **move** is exactly one thing: a bare identifier naming a *managed* value
(string or `shared`, via `ownership.IsManaged`) passed to an `own` param. An `own` scalar or
stack aggregate is copied by value, and a field argument (`p.name`) would be a *partial* move
(its own design question), so neither counts. The analysis is flow-sensitive per function body
over a name→move-site map: an `if`/`match` analyzes each branch from the join point and takes
the **union** (moved in either branch → moved after, matching Rust); a loop body is seeded with
every move found anywhere inside it, so a move on one iteration is visible to the next
iteration's reads (the message says so); a `let`/`var` declaration or reassignment of the name
clears the move. Control-flow nodes are handled explicitly and everything else routes children
back through the same walker via `ast.WalkStmt`/`WalkExpr` with pruning, so straight-line
coverage is total without enumerating every node kind. Conservative in the report-nothing
direction — an unresolvable callee (a method call, a call through a local) records no move, so a
hard error can't fire on a shape the analysis doesn't model; reports dedupe by (binding, move
site). Runs **after** typechecking (it needs the `TypeTable` to identify managed values),
alongside `CheckPurity`. **Note on framing:** this is not a memory-safety fix — the ownership
pass retains a managed value flowing into a non-last-use `own` argument, so use-after-move is
safe today (ASan-verified). It enforces the `own` contract and surfaces the otherwise-silent
reuse/FBIP **perf cliff** (the defensive retain leaves rc = 2, so `lyra_rc_drop_reuse` reports
the box shared and reuse stops firing), and is the prerequisite for dropping that retain to make
`own` a true move. Trait-impl method bodies (and trait default methods) are covered, each
analyzed from a fresh state so a move in one method can't flag a read in the next. Not covered:
moves through a variable captured by a nested lambda, which is analyzed independently (whether a
closure body runs once, later, or never is exactly what captures make uncertain, so threading
the outer state in would risk false positives).

## `captured_assignment.go`

`CheckCapturedAssignment(program, capturesTable)` (`lyra-E024`) rejects a lambda that assigns to
a binding it captured. A closure captures **by value** (see `pkg/analyzer/captures`), so the
write reaches the closure's own copy and nothing else: `var n = 5; let bump = () -> i64 => { n =
n + 1  n }` leaves the outer `n` at 5. Compiling that silently is the same failure the by-value
`mut` parameter had — a write that vanishes with no diagnostic either way — and the other
reading (capture by reference, so the write lands) is unavailable without escape analysis
proving the frame outlives the closure, where being wrong is a dangling pointer rather than a
lost update. Covers all three assignment forms (`n = …`, `n += …`, and a path write like `p.x =
…`, walked down to its root binding), and fires only for a name the capture pass recorded, so a
lambda writing to its own local or parameter is untouched. Runs after the capture pass in the
driver.

## `inert_borrow_modifier.go`

`CheckInertBorrowModifiers(program)` (`lyra-W010`) warns when an `own`/`ref`/`mut` modifier sits
on a parameter whose type is a copied scalar primitive (a numeric type, `bool`, or `rune`).
Those modifiers are conventions over a *reference* — `own` transfers ownership, `ref`/`mut`
borrow — but a scalar is passed by value with no interior to borrow and no reference to
transfer, so the modifier collapses to a plain parameter and only misleads a reader into
expecting move/borrow semantics (the same inert-annotation class as an unused param, or
`pure`+`det`). Since 07/29 the predicate lives in `types.IsCopiedScalar` and is **shared with
the backend's by-reference `mut` lowering** (`paramIsByRef`): a `mut` this warning calls inert
must stay by value, and one it stays silent about must be passed by reference — reading one
predicate is what keeps those two from drifting into a silent miscompile. A **warning**, not an
error (decided 07/21 over allow-silently and forbid): the code is correct, and hard-erroring
would wall off two real cases — a generic `own t` (which must stay uniform; at monomorphization
it may be managed) and a scalar-repr newtype used for API intent. Excluded: `string` (a
`PrimitiveType` but managed, so `own string` is a real move — via `types.IsString`); generic
type params (a `GenericType`, not a `PrimitiveType`); aggregates (can own managed fields).
Scoped to `LambdaExpr.Parameters` (every free function + nested lambda). AST-only; wired into
the driver's lint block.

## `type_names.go`

`CheckTypeNames(program)` (`lyra-W009`) warns when a **struct** is declared with an
all-uppercase (SCREAMING_CASE) name: such a name matches the `const_identifier` lexer rule
(`[A-Z][A-Z0-9_]*`) instead of `user_defined_type_name`, so a struct literal `NAME { … }` won't
parse — the struct can be referenced but never *constructed*, and the failure otherwise surfaces
far away as a confusing "undefined symbol" at the use site. A warning (not an error) since the
type is still usable by reference; scoped to structs only (a `data` type constructs via its
constructors and a named tuple via `Name(…)`, so a SCREAMING_CASE name works there). Returns
`[]diag.Diagnostic`; wired into the driver alongside the other lint passes.

## `generic_params.go`

`CheckGenericParams(program)` reconciles a binding's **written** generic parameter list against
the type variables its signature actually mentions, in both directions: a signature variable
missing from the list is an error (`lyra-E031`), a declared parameter the signature never
mentions is a warning (`lyra-W013`). AST-only, and wired in *before* typechecking — reporting at
the declaration is the whole point.

**The list stays optional; written, it is authoritative.** Type variables are lexical (a
lowercase type name is a variable wherever it appears), so `let unbox = (b: Box<t>, fb: t) -> t`
is generic with no list at all and stays legal — that follows from the lexical rule. What did
not follow is the list being *unchecked when written*: before this pass, `let mismatch<t> = (a:
u) -> u => a` compiled and ran, declaring `t` while being generic in `u`.

*The hazard is a typo, and it is a Pit-of-Success inversion.* A misspelled lowercase type name
does not fail — it silently becomes a *new* type variable, and the function becomes generic in
something its author never meant. The signature still type-checks; what changes is that callers
must now solve a variable that should have been a fixed type, so the diagnostic (if any) lands
at the call site, or the error surfaces only in the backend. That is how the prelude's
`ok`/`err` shipped without their `<t, e>` and drew no diagnostic at all. Uppercase names never
had the hole: an unknown one is an `UnresolvedType`, reported at the declaration.

**Why an error and not just a warning** (option (b) of the three in `todo.md`, over (a) warn on
both and (c) require the list outright). A warning also gives the typo somewhere to be caught;
what only an error buys is that a bound cannot be quietly inert. The list is the only place a
bound can be written (`<t: Show>`), so a list that need not agree with its signature means a
constraint can silently constrain nothing — which is what makes this worth settling *before*
bound enforcement rather than after: an unenforced bound and a bound on the wrong variable look
identical from outside, and only one of them stops being a problem when enforcement lands. (c)
was rejected as the least ML-ish of the three, buying little over (b).

The `where`-clause half is enforced in the collector, not here — `Collector.MergeWhereConstraints`
merges a `where u: Show` into the matching list entry, and *discarded* a name that matched
nothing, so by the time there is an AST the constraint no longer exists to check. It now reports
the same `lyra-E031` at the point the name is still visible.

**The variable walk is `types.CollectTypeVars`, shared not copied.** This pass is the third
consumer of "which variables does this signature mention?", after the typechecker's
`lambdaTypeVars` and the backend's `mentionsTypeVar` — and those two had already drifted (the
backend's was missing `ParameterizedType`, the 07/30 build failure). Adding a third switch was
the one thing `todo.md` asked whoever took this not to do, so all three now call one walker in
`pkg/types/typevars.go`; hazard 8 in `lyra/CLAUDE.md` covers the class. Unifying them turned up
two composites *neither* copy had, `AnonymousStructType` and `RangeType`.

Deliberately not walked: nominal types (`NamedStructType`, `DataType`). A `struct Box<t>` binds
its own `t`, so a function taking a `Box<i64>` mentions no variable of its own — descending into
the declaration would report `t` as a use and make every function touching a generic type
spuriously generic.

**Not covered** (unchanged behaviour, same defect class): the generic parameter lists on *type*
declarations, traits, and impls. Those are reconciled by nothing today either; the arity of a
type declaration's list is at least load-bearing at instantiation, which a binding's is not.

## `range_analysis.go`

`CheckIntegerRanges(program, tt)` (`lyra-E020` / `lyra-E021` / `lyra-E022` / `lyra-E023` /
`lyra-W011`), a **flow-sensitive value-range (interval) analysis** over each function body. It
tracks each integer variable's interval `[lo, hi]` at every program point (a `rangeEnv` map + a
`reachable` flag, cloned per branch, unioned at joins) and reports a **definite integer
overflow** (`lyra-E020` — an `+`/`-`/`*`/unary-`-` whose operand ranges prove the result can't
fit its type on *any* path — `if x > 100 { x + 100 }` on an i8, which the literal-only
`checkIntegerLiteralRange` can't catch because it needs the branch refinement), a **definite
divide-by-zero** (`lyra-E021` — a `/`/`%`/`%%` whose *identifier* divisor is proven `[0,0]` on a
reachable path, `if b == 0 { a / b }`; a literal/folded-constant `5 / 0` stays the typechecker's
own constant-fold check, so E021 adds only the non-constant flow-proven case, avoiding a double
report), a **definite out-of-bounds index** (`lyra-E022` — an `xs[i]` whose index range is
*non-singleton* and entirely outside `[-size, size)`, `if i >= size { xs[i] }`; a single
constant index stays the typechecker's own range check, and a non-singleton range guarantees
that check — which resolves only a single constant — didn't fire, so again no double report), a
**range-constraint violation** (`lyra-E023` — a non-constant value proven entirely outside a
range-constrained newtype's range, `if x > 100 { let p: Percent = x }`, via
`checkConstraintViolation`; the flow-sensitive twin of the typechecker's constant-value
`checkRangeConstraints`, scoped to an *identifier* value assigned to an annotated `let` — the
typechecker stamps the target `*ConstrainedType` onto the value node, and folds the literal case
itself, so no double report), and an **always-true/false integer comparison** (`lyra-W011`).
Runs **after** typechecking (needs the TypeTable for each expression's width/signedness). **Zero
false positives is the bar:** anything not precisely trackable *widens* to ⊤ (the type's full
range), which can only miss a diagnostic, never invent one — an absent variable is ⊤; a
float-adapted int literal (`let a: f64 = 5` records the literal at the float type) is untracked,
since an integer interval built from a float's source text can be *wrong*, not just imprecise
(f32 rounds 16777217); interval math that would overflow int64 is ⊤ (`addI`/`subI`/`mulI`/`negI`
are all int64-overflow-guarded, so i8..u32 are precise and i64 is mostly ⊤; **u64 is tracked
with a `+∞` upper sentinel** since its true max 2^64-1 doesn't fit int64 — the exact lower bound
of 0 is load-bearing (`x < 0` always-false, a refined `x >= size` index → E022, a proven-below-0
subtraction → E020 underflow) while the fake upper only ever causes conservative untracking;
`compareConst` has sentinel guards so `x > MaxInt64` on a u64 is *not* wrongly folded to
always-false); a C-style **`for` loop is analyzed with a widening/narrowing fixpoint**
(`evalForLoop`) so its counter is tracked *precisely* inside the body (`for var i: u8 = 0; i <
3; i += 1` → i ∈ [0,2]): the body is analyzed *silently* (the `rangeChecker.silent` flag gates
`report`+safe-marking) to find the loop-head invariant — widen unstable bounds to ±∞ for fast
convergence, then narrow them back with the guard — then once *loudly* with that invariant, so
diagnostics/elision key off the precise ranges; an accumulator (no bounding guard) still widens
to ⊤, the after-loop state havocs (sound under `break`); a **`for … in` over a numeric range**
binds its loop variable to the range interval (`forInRangeKey` — the for-in analogue of the
counter, no fixpoint since the range gives the bounds directly), but only when the range is
*provably non-empty* (`start.hi < end.lo` for `..<`) so a body diagnostic is genuinely definite
rather than a maybe-empty false positive — a maybe-empty / stepped / two-variable / non-range
iterable still havocs. A contradictory branch refinement marks that branch **unreachable**
(diagnostics suppressed); on block exit the env keeps every change to a pre-existing outer
variable — a reassignment **and** a *havoc* (a name a loop / untrackable reassignment deleted →
⊤) — and drops the block's own local declarations (a shadowed outer name restored to its
pre-block value). It's built from the block's *inner* (post-body) env, **not** the pre-block
snapshot: the latter (copying-forward only names still present) silently reverted a havoc done
inside a nested block/branch, which then unsoundly elided a downstream safety check on the stale
interval — a miscompile, fixed 07/27 (`TestRange_Safety_HavocInNestedBlockNotElided`). **Branch
refinement** (`refine`, pure — no diagnostics, since `eval` already emitted them once for the
condition) narrows a variable against a comparison with a *pure* constant side (literal /
negated literal / another tracked variable, on either operand), threads `&&` into the
then-branch and `||` into the else, and swaps for `!`. **Match-arm refinement**
(`evalMatch`/`refineScrutinee`) is the `match` analogue: when the scrutinee is a tracked integer
variable, each arm narrows it to the values its pattern matches (a literal or a numeric range
via `patternInterval`, mirroring the typechecker's exhaustiveness reader), so `match x {
100..=127 => x + 100 }` on an i8 is a definite overflow and an arm whose pattern can't overlap
the scrutinee's range is unreachable/skipped; a catch-all/identifier arm refines nothing. A
*possible* overflow (`a + b` on two full-range i8s) is deliberately left to the runtime trap. A
compound assign (`v += k`) is typed `void`, so its overflow bound is read off the RHS (which
carries the target's propagated width). **Diagnostics-first by design**: validating the engine
as a front-end diagnostic before trusting it to *remove* a runtime safety check, where an
unsound narrowing would be a miscompile. **Trap elision landed on top**: `CheckIntegerRanges`
also returns a **`SafetyTable`** — the operations the pass proved can't hit a given runtime trap
on any path — stored on `driver.Result.RangeSafety`, so the backend emits the plain instruction
/ bare load instead of the checked form. Four facts, keyed by the AST expression node (a pointer
match — both passes walk one `*ast.Program`): **`NoOverflow`** (`+`/`-`/`*` (and `+=`-style)
whose result fits its type, from `checkArith`'s "result entirely within the type" branch →
`applyIntMathOp` drops the `with.overflow`+trap, via `emitWrappingOp`); **`NoDivZero`** /
**`NoDivOverflow`** (from `checkDivision`, which also emits E021: a `/`/`%`/`%%` with a
provably-nonzero divisor / no signed `INT_MIN÷-1` → `emitCheckedDivOp` drops the divide-by-zero
/ overflow guard; unsigned division is always `NoDivOverflow`); and **`IndexInBounds`** (from
`evalIndex`, which also emits E022: `xs[i]` with the index provably in `[0, size)` →
`lowerIndexExpr` drops the bounds trap *and* the negative-from-end adjustment; a loop counter is
proven via the widening fixpoint, a refined param via branch refinement). Sound by construction
(only *proven*-safe ops are present; a nil table / absent entry reports false, so a real fault
is never elided). The analysis's original deferred list is now cleared — diagnostics + elision
across overflow / divide-by-zero / bounds, all integer widths (i8–u64), `if`-branch +
`match`-arm + C-style-loop + for-in-range refinement, and `RangeConstraint` enforcement both
ways (the typechecker's `range_constraint.go` for a constant value, this pass's
`checkConstraintViolation` for a flow-proven one). A remaining precision item: a
*variable-length* for-in range (`for i in 0..<n`) isn't provably non-empty, so it still havocs
(sound).


## Purity — current state

The FP/Imperative purity work (`pkg/analyzer/checker/purity.go`) is the active area now. Purity
inference (bottom-up, no `pure` keyword required) covers both free functions and trait-impl
methods via one joint fixpoint (`inferImpurity`), including method-to-method call chains:
`checkTraitImpl` (`typechecker_traits.go`) now calls `checkTraitImplMethodBody` on each impl
method body, which sets up a param scope from the trait signature, checks the body against the
declared return type, and infers the body so any `.`-call inside is dispatched into
`MethodTable` — making the purity fixpoint's `methodTable.Get` lookups in `methodEffects`
produce correct results for method-to-method chains. **Phase 2 landed for lambdas + free
functions (07/10/26):** the purity checker now *consumes* the collector's `ScopeTable` rather
than re-walking the AST — `scopeFrames.forLambda` (`purity.go`) flattens a lambda's recorded
`ScopeFunction` subtree (pruning at nested function scopes) into the per-lambda `scopeBindings`
frame, deriving mutability from declaring nodes and reconciling two collector quirks
(`with`-arena handles read mutable, `for … in` loop vars skipped) for bit-for-bit fidelity.
`CheckPurity`/`InferredEffects`/`InferredPureFunctions` take a `*symbols.ScopeTable`
accordingly. **Still open:** impurity of imported functions; and the *trait-method* path
(`directScopeBindingsForClause`) still re-walks — method clauses have no recorded scope
(`CollectLambdaClause` pushes none), so converting them needs a collector change reconciled with
`checkTraitImplMethodBody`. See `todo.md`'s FP/Imperative #3.
