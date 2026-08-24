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

`CheckPurity` returns **two** results — the `pure` violations, and the missing-bound
warnings from `missing_pure_bound.go` (documented below), which ride along because they
read the same fixpoint. It enforces `pure` (lambdas and, since 06/24/26, trait-impl methods): no captured
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

## `missing_pure_bound.go`

`missingPureBounds` is `CheckPurity` inverted, and it is why that function returns two
results rather than one: the enforcement half reads an annotation and checks the body
against it, this half reads the body and asks whether the annotation is missing
(`lyra-W018`). Both consume the one effect fixpoint, so nothing is re-derived — it is a
method on `purityChecker` for exactly that reason.

**The obvious justification for it is false in this compiler, and the code says so.**
"An unmarked callee blocks a `pure` caller" is what a reader expects, and purity here is
*inferred* whole-program: a `pure` function may call an unannotated free function or impl
method whose body the fixpoint found effect-free, and nothing is refused. The missing bound
costs on the **next edit**:

```lyra
let helper = (n: i64) -> i64 => { println("added later"); n * 2 }
let caller = pure (n: i64) -> i64 => helper(n)
```

The `println` is reported at `caller` — the only thing in the program that promised
anything. Write `pure` on `helper` and it is reported at the `println` too, in the function
being edited. So the bound is where the *blame* goes when a body changes, which is
`generic_params.go`'s "the diagnostic lands somewhere else" one rung up.

Scope was chosen against measurements over `std/` and `examples/`, not from taste:

- **`pure` only.** `det` and `noalloc` come off the same fixpoint and were counted on the
  same code: `det` fires on ~1/6 of all functions and `noalloc` on ~2/5, and nearly every
  `det` candidate is a terminal-escape wrapper (`cursor_hide`, `move_to`) that qualifies
  only because `det` permits `EffectOutput` by design. Reporting them buries the `pure`
  half, which fires a handful of times per file and names real pure helpers.
- **Declarations only.** The fixpoint covers every lambda including inline closure
  arguments; `(x) => x * 2` inside an `xs.map(…)` is an expression, not an interface. A
  nested named `let` is excluded on the weaker version of the same point — its callers are
  in the body around it.
- **`main` never warns.** Nothing calls it, so there is no caller for blame to move to.
- **A trait-declared bound counts as annotated.** `effectiveMethodBounds` is shared with
  `checkTraitMethodBounds` so the two cannot disagree; warning at an impl whose trait
  already says `pure` would be advice to restate what is already enforced.

A higher-order function *is* reported: a callback's effects are charged to the call site
that supplies it, so marking one `pure` does not forbid impure callbacks — which is how the
prelude's `map`/`filter`/`flat_map` are `pure noalloc` today.

**Landing it required marking the standard library**, and that is the real cost of the
feature rather than an incidental chore. `std/prelude` was diagnostic-clean before this and
drew 97 warnings after — every one a trait-impl method (`Add::+` and friends per numeric
width, `Show::show`, `Signed::abs`, `Ord::compare`, `Needle::found_at`) — which would have
appeared on *every user compile*, about code the user did not write. They were marked at the
impl rather than by declaring the traits' methods `pure`, which would have been one edit
instead of 97: a bound on the trait binds every implementer, including a user's, and
deciding that no `impl Show for MyType` may ever print is a language decision, not a
cleanup. `std/math`, `std/tui` and `examples/` were marked the same way.

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
(`purity.go`'s `allocContext`/`buildAllocContext`): a value-**producing** expression whose
recorded `TypeTable` type is **heap-represented**. Two ways to be heap-represented, and
they are different questions (`heapRepresented`):

- a **`shared` flavor** — a use-site property, so an annotated binding
  `let n: shared Node = Node{…}` records the flavor onto the construction via `checkVarDecl`
  and `allocContext.allocates` reads it via `AllocationOf`. The producing forms are
  `StructInstanceExpr`/`TupleLiteralExpr`/`DataConstructorExpr`;
- a **dynamic array** (`ArrayLiteralExpr` recorded as `[]T`, `ArrayCompExpr`) — heap-boxed by
  its own nature, since `lowerType` maps `[]T` to a ref-counted box *before* the flavor is
  consulted. Added 08/04, when array `map`/`filter` reached the prelude as comprehensions and
  `noalloc` could be claimed by a function allocating per element. The **same literal** as a
  fixed `[N]T` is stack storage and does not count — the rule reads the recorded type, not
  the syntax.

It is asked of producing forms rather than of every expression on purpose: a `[]T`
*identifier* is heap-represented and allocates nothing, so a type-only rule would charge
every mention of an array to its enclosing function.

**Strings are charged by *form*, not by type** (`allocatesByForm`, 08/04): `StringConcatExpr`
and `InterpolatedStringExpr` each build a fresh ref-counted box, while a string **literal**
interns as a pinned static box and allocates nothing. The type cannot make that distinction
— all three are `string` — which is the exact opposite of the array case, where the type is
what does. The two predicates stay separate for that reason; one covering both would mean
"the type says so" in one case and "the syntax says so" in the other. `allocatesByForm` is
gated on the `TypeTable` anyway, even though it never reads one, so the AST-only
`InferredEffects` keeps its all-or-nothing contract instead of reporting strings alone.

`CheckPurity` is threaded the `TypeTable` and the **captures table** for this; the AST-only
`InferredEffects` helper has neither and so never sets `EffectAlloc`. A **closure
construction is charged by capture** (08/12): a nested lambda that captures heap-boxes its
environment per construction and sets `EffectAlloc`, while a capture-free one is the
backend's shared pinned static and stays free — exact against the shipped lowering, and a
rule Lambda Set Specialization can only loosen (see the closure-lowering entry in
`todo.md`). A `shared` construction in a return/argument position (flavor not yet recorded
on the construction node) is deferred to a future layout/escape pass — see `todo.md`.

**There used to be a `with`-arena discharge here, and removing it (08/13) is the reason
to read this paragraph.** Every expression lexically inside a `with` body was marked
discharged and all three allocation predicates consulted the mark, so wrapping a `shared`
construction in `with a = 42 { … }` switched `noalloc` off — for a statement that has no
lowering, whose arena expression nothing type-checked, and whose canonical `Arena.new(…)`
spelling had been unspellable since `lyra-E035`. That is the `slice` hole's shape a third
time: a bound that silently stops binding. `with` is refused outright now (`lyra-E050`).
Deleting the discharge was **half** the fix — allocation is a use-site property this pass
reads off the `TypeTable`, so a body the typechecker never visited is one whose `shared`
constructions are invisible here regardless; the typechecker checks `with` bodies now,
which needed `WithStmt.Body` to become a `*BlockExpr` (see the E050 entry in
`COMPLETED.md`). If arenas are built, the discharge returns **with an escape analysis** —
a `shared` value built inside a block and returned out escapes, and "everything lexically
inside" was always the approximation standing in for that analysis. An **unresolvable external call** (no local lambda, builtin, or type
conversion) conservatively taints `AllEffects` (`PurityEffects | EffectAlloc`) — everything,
including Alloc, so `noalloc` flags it too (we can't verify it doesn't allocate).
`builtinEffects`: print/println→Output, read→Input, write→Input|Output, `await`→Input,
`random_seed()`→Rand, `wall_clock_nanos()`→Time, **`panic`→None**. Only *ambient* rand/time sources
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
`pure` code at all**, which is the entire prelude combinator layer.

`callableParams` maps the declared parameter names *and* the names a body-level `match` binds
them to (`addMatchAliases`). That second half is not an extra: a **multi-clause function is a
match on its parameters** by the time this runs (`typechecker/multi_clause.go` desugars it, and
clears `LambdaClauses`), so a clause that renames a parameter — `(self: …, predicate: …)`
destructured as `(Some v, pred)` — reaches this pass as an arm binding. Without the aliases the
call through `pred` is an unresolvable callee and the whole function is charged `AllEffects`;
with them it is the parameter it destructures, and its declared bound is enforced under the new
name too. Only a *whole-value* binding aliases the argument — the `v` of `Some v` is the
payload, and charging a call through it against that position would consult the wrong
parameter's bound. See COMPLETED.md, 08/06, for the prelude breakage that found this.

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

## Trait default bodies in the purity pass

`collectMethodImpls` gathers an impl's clauses **and** every trait method's default
(`ast.TraitMethod.DefaultImpl()`), because a default is a body like any other and is
dispatched to exactly as an impl's is. It is taken from that accessor rather than built
here: the effect map is keyed by pointer and the typechecker's resolutions name that same
instance, so a second one would leave every call to a default charged the unresolved-callee
default (`AllEffects`) while the body it actually runs sat in the map unread.

`checkTraitDefaultBounds` then holds each default to the bound its trait declares, as
`checkTraitMethodBounds` does for an impl's clause. **The diagnostic lands on the default**,
which is the body that is wrong, rather than on every impl that inherited it — the default
is the one body the trait's author controls, and blaming implementers would blame code that
wrote nothing. Before defaults were dispatchable the bound was enforced on every *override*
and not on the thing being overridden.

## `array_repeat_alias.go`

`CheckArrayRepeatAliasing(program, symTable, tt)` (`lyra-W019`) warns when `[v; n]` fills its
slots with a value the program can mutate through the reference they all share.
`[[' '; WIDTH]; HEIGHT]` is the shape it exists for: one row referenced HEIGHT times, so every
`grid[py][px] = c` writes the same place and every row prints identically — found in
`examples/mandelbrot.lyra`, where a uniform image read as bad arithmetic and outlived the two
genuine arithmetic bugs it was hiding behind. The semantics are right and unchanged (`[v; n]`
evaluates its value once, which is what makes `[expensive(); 1000]` one call), so the fix is a
diagnostic. A **warning**, on `missing_pure_bound.go`'s reasoning: the code is correct, a
deliberate alias is a real thing to want, and with no `#[allow]`-shaped suppression an error
would leave that intention nothing to write.

**It runs after typechecking, and it has to.** The element's type at *inference* time is not
the type it is lowered at: under a `[][]rune` annotation the inner `[' '; WIDTH]` infers as the
fixed `[WIDTH]rune` — copied per slot, sharing nothing — and only propagation widens it to the
heap-boxed `[]rune` that does. A check inside `inferArrayRepeatType` would have cleared the
exact program that motivated it, so this reads the `TypeTable` for the settled type the backend
will lower. That is the whole reason it is a standalone pass rather than an arm of the
typechecker.

The element predicate is `ownership.SharesMutableState`, deliberately narrower than "managed" —
see that package's README for the two measurements that shaped it. A count folding to 0 or 1
fills no second slot and is silent; a **runtime** count is assumed plural, since `[buf; n]` sized
from a window resize is exactly the case the author cannot see the number for either. The
message names the struct field it reaches the sharing through (`SharedMutablePath`), and nothing
for a tuple or `data` payload, whose own spelling already prints what they hold.

### Unused loop bindings (`lyra-W020`)

`checkLoopBindings` reports a `for-in` binding the body never reads, naming `_` as the fix.
A separate code from `lyra-W003` because the fix differs and the difference is the point: an
unused local can be deleted, a loop binding cannot — the loop still has to iterate.

**It could not have existed before `for _ in` parsed** (08/18); until then the advice would
have named a spelling the parser rejects. The two-name form is where it earns its keep —
`for k, v in xs` reading only `v` is the case `for _, v in xs` exists for. A name already
starting with `_` is exempt, matching the unused-local rule.

Both halves of the pass share `referencedNames`, because "is this name referenced" is one
question about two kinds of declaration; two copies would drift about exactly the cases that
make it conservative (a write counts as a read; the walk descends into nested lambdas, so a
captured name is not reported).

**The warning is reported at `ForInLoopExpr.KeyLocation`/`ValueLocation`, not at the loop.**
Beyond precision, this is load-bearing: the loop node had no Location at all until this
landed, and a diagnostic with a zero Location escapes the driver's per-file filtering — the
two prelude instances appeared on every file compiled, which is how the gap was found.

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
100..<=127 => x + 100 }` on an i8 is a definite overflow and an arm whose pattern can't overlap
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

## Effect inference is one walk, and why that matters

`inferImpurity` runs a fixpoint over an effect *set*: each round asks every callable what
its body does, ORs in anything newly found, and stops when nothing changes. The question
"what does this body do" used to be asked by **two** functions — `lambdaEffects` for a free
function and `methodEffects` for a trait-impl method — at ~200 near-identical lines each.
Both carried comments saying the other had to stay in step. One line did not.

A call resolving to a trait-impl method charged `impureMethods[method]` on the lambda side
and `methodCallEffect(...)` on the method side, and only the second adds the effects of the
arguments supplied for that method's **callback** parameters. So:

```lyra
trait Runner { run: (Self, () -> i64) -> i64 }
impl Runner for Runbox { run = (self, f) => f() + self.n }

let noisy = () -> i64 => { println("noise") 42 }
let mid   = (r: Runbox) -> i64 => r.run(noisy)     // inferred pure — wrong
let outer = pure (r: Runbox) -> i64 => mid(r)      // accepted, and prints
```

`outer` promised purity, checked clean, and printed. The *reporting* walk (`exprVisitor`)
had used `methodCallEffect` all along, so the diagnostic machinery was right and the table
it consults was wrong — which is exactly why nothing caught it, and why a test asserting
"this program is rejected" was the only thing that could have.

Inference is now one walk. `bodyEffects` takes a **`callable`**, the descriptor of what the
two entry points actually differ in: the body's own frame, the capture stack, the `mut`
parameters (nil for a method, which has no modifier syntax yet — and a nil map reads false,
which is precisely what the method walk did by omitting the test), the parameter positions,
where to record an allocation site, how to read a declared bound on a parameter, and how to
walk the body. Everything else is shared, so a divergence now requires editing the one walk
into disagreeing with itself.

The tables the walk reads live in **`inference`**, which `purityChecker` embeds rather than
copying field by field. That is the second half of the same lesson: the reporting walk and
the fixpoint answer the same questions, so they should not be reading two sets of maps.
