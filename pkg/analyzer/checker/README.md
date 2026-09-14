# `pkg/analyzer/checker` — standalone AST passes

Semantic passes outside the typechecker. Language semantics are in
[LANGUAGE.md](../../../LANGUAGE.md).

- **Pre-typecheck, AST-only**: `use_before_declaration.go`, `shadowing.go`, `unused_variables.go`,
  `effect_bounds.go`, `generic_params.go`, `type_names.go`, `inert_borrow_modifier.go`.
- **Post-typecheck** (need `TypeTable`/`MethodTable`): `purity.go`, `use_after_move.go`,
  `range_analysis.go`, `array_repeat_alias.go`, must-release, let-else divergence. A pass lives
  post-typecheck when it needs the *settled* type the backend lowers, not the inferred one.
- **Every pass returns `[]diag.Diagnostic`**, carrying its own severity; the driver loops over
  them. `typechecker.TypeError` is the one exception (`TypeError.Diagnostic()` bridges it).

## `use_before_declaration.go`

`CheckUseBeforeDeclaration(program)`: collect a block's names, then walk in order flagging early
uses. Lambda bodies are checked with **parameters pre-seeded** (`checkStatementsInScope`), so
`let s = s ++ "!"` shadows rather than uses early.

## `purity.go`

`CheckPurity` must run **after** `typechecker.Check` (see `cmd/lyra-lsp/main.go`). It takes the
`*typetable.MethodTable` (nil-safe), the `TypeTable`, the captures table, the `*symbols.ScopeTable`,
and the **`*symbols.SymbolTable`** — required, because an impl inherits its trait's bound and the
trait must be resolved as the impl's module sees it (`LookupTraitFrom`, rule 4). Never build a
local `map[traitName]`.

Returns two results: `pure` violations (`lyra-E007`) and missing-bound warnings (`lyra-W018`,
`missing_pure_bound.go`). `InferredEffects`/`InferredPureFunctions` expose the fixpoint by name
(top-level functions only).

### One walk

- `inferImpurity` is a set-monotonic fixpoint over free functions **and** trait-impl methods
  jointly (`collectMethodImpls`), recomputing every callable each round (no "already impure"
  early-out).
- **`bodyEffects` over a `callable`** is the *only* body walk. `callable` describes what differs:
  frame, capture stack, `mut` params (nil for methods), parameter positions, allocation-site
  recording, declared-bound reading, body walker. Enforcement re-runs the same walk with
  `callable.reportPure` set, so the arm that charges a bit words the diagnostic. Do not
  reintroduce a separate reporting mirror (the old split inferred a pure-through-trait-method
  call wrong).
- `exprVisitor` is orchestration only: find callables, check `det`/`noalloc` against the inferred
  row, re-run the walk for `pure` bodies, check declared callback bounds.
- Reporting-only arm: a nested lambda's parameter defaults are held to the enclosing bound at the
  definition (inference bills the call sites via the default-args desugar).
- Tables live in embedded **`inference`**, shared by fixpoint and enforcement.
- Method-to-method chains work because `checkTraitImplMethodBody` populates `MethodTable` inside
  impl bodies.

### Memoized per-callable inputs

| fact | cached as |
|---|---|
| body scope frame | `frames.forLambda` |
| `mut` parameters | `frames.mutBorrowsFor` |
| parameter positions (incl. match aliases) | `frames.paramsFor` |
| method frame / params | `frames.forMethod` / `methodParamsFor` |

**The maps are shared: nothing may write to one after its builder** (`buildLambdaFrame`,
`addScopeSymbols`, `directScopeBindingsForClause`, `addMatchAliases`). `funcScope.isLocal` tests
key presence, not value. `TestPurity_IsIdempotent` is the standing check.

Lambda scopes come from the collector's `ScopeTable` (`scopeFrames.forLambda`, pruned at nested
function scopes). Trait-method clauses still re-walk (`directScopeBindingsForClause`) because
`CollectLambdaClause` pushes no scope. Impurity of imported functions is open (`todo.md`).

### Resolving callees

- **Scope first, then the builtin table** (`isImpureCallee` and the walk's call cases) — a user
  `print`/`panic` is the user's function.
- **Namespace-qualified callee** (`resolveCallee`): `maybe.map` resolves through its last segment,
  only when the object segment names no binding (mirrors backend `namespaceCallee`).
- **Unresolvable callee** → `AllEffects` (`PurityEffects | EffectAlloc`).
- **Callee the typechecker refused** (`typetable.SetUnresolvedCallee`) → charged nothing,
  reported nowhere (no cascade). Don't re-derive this locally: "resolves nowhere here" also matches
  merely-unseeable callees.
- Overloaded/UFCS callees: read `TypeTable.Callee` first.

## `effects.go`

`checker.Effect` bitmask: `EffectMut`, `EffectInput`, `EffectOutput` (`EffectIO` = both),
`EffectAlloc`, `EffectRand`, `EffectTime`; `EffectNone` = pure.

- **`PurityEffects = Mut|Input|Output|Rand|Time`** — Alloc is orthogonal to `pure`.
- **`DetEffects = Input|Rand|Time`** ⊆ PurityEffects.
- **`builtinEffects`**: `print`/`println`/`set_raw_mode` → Output; `read_line`/`read_key`/
  `terminal_size`/`wait_for_key_ms`/`await` → Input; `random_seed` → Rand; `wall_clock_nanos` →
  Time; **`panic` → None** (every integer op can already trap from `pure` code). Only *ambient*
  sources carry Rand/Time; a threaded RNG is ordinary `mut` data. Every new builtin needs an entry.

### Allocation (`allocContext`/`buildAllocContext`)

Asked of value-**producing** forms only (a `[]T` identifier allocates nothing):

- **`heapRepresented`** (by recorded type): a `shared` construction (`AllocationOf`;
  `StructInstanceExpr`/`TupleLiteralExpr`/`DataConstructorExpr`), or a dynamic array
  (`ArrayLiteralExpr`/`ArrayRepeatExpr` recorded as `[]T`, `ArrayCompExpr`). A fixed `[N]T` literal
  does not count.
- **`allocatesByForm`** (by syntax): `StringConcatExpr`, `InterpolatedStringExpr`; string literals
  are static. Kept separate from the type rule. Gated on having a `TypeTable` so AST-only
  `InferredEffects` never sets Alloc.
- **Closures**: a capturing nested lambda allocates; a capture-free one is a static.
- **Builtin methods**: `SetBuiltinMethod(call, allocates)` from the typechecker (e.g. `slice`).
- `shared` constructions in return/argument position without a recorded flavor are deferred
  (`todo.md`). `with` is refused (`lyra-E050`); any future arena discharge needs escape analysis.

### Effect polymorphism over callbacks

- A function's stored effect = **base** + **callback parameters** (`callableParams`, tracked in the
  same fixpoint). A call pays base ∪ supplied arguments' effects (`callEffect`/`argumentEffect`).
- An annotation constrains the function's **own** body; an impure callback is rejected at the call
  site, naming the argument. Callbacks passed onward stay polymorphic.
- `callableParams` also maps names a body-level `match` binds them to (`addMatchAliases`) —
  required because multi-clause functions are desugared matches that may rename parameters. Only
  whole-value bindings alias (not `Some v`'s payload).
- **Trait-impl methods**: `methodEffects`/`methodCallEffect`; parameter types come from the trait
  declaration via `collectMethodSignatures`; bounds via `signatureBound`.
- **Receiver offset hazard**: trait signature index i is `Arguments[i-1]` (`methodArgumentAt`).
  Off-by-one is silent; a two-callback test guards it.
- Still conservative (charged `AllEffects`): callbacks via struct field, call result, array element.

### Declared callback bounds (`f: pure () -> t`)

- `types.LambdaType.IsPure/IsDet/IsNoAlloc`. A bounded parameter is **not** polymorphic:
  `declaredBound`/`boundEffect` charge exactly what the bound permits.
- `checkDeclaredCallbackBounds` runs at **every** call site and compares the argument's
  **inferred** effect (lambda literals need no annotation).
- `isAssignable` ignores bound differences between function types (so the message is about
  effects); `TypesEqual` distinguishes them.
- A bounded parameter forwarded to a bounded slot satisfies it from its declared type; an
  unbounded one cannot.
- The standard library leaves combinators unbounded on purpose.

### `det` / `noalloc` / trait-method bounds

- `checkBoundedEffects` (`lyra-E016`): whole inferred set — `det` vs `DetEffects`, `noalloc` vs
  `EffectAlloc` — reported once at the callable. `pure` keeps per-op reporting (`lyra-E007`).
- `checkTraitMethodBounds`: an impl is checked against its own annotation OR the trait's
  (`effectiveMethodBounds`, shared with `missingPureBounds`).
- `checkTraitDefaultBounds`: a default body is held to its trait's bound, reported **on the
  default**. `collectMethodImpls` takes defaults from `ast.TraitMethod.DefaultImpl()` — the
  pointer-keyed single instance; a copy would leave calls charged `AllEffects`.

## `missing_pure_bound.go` (`lyra-W018`)

`missingPureBounds`, a method on `purityChecker` reading the same fixpoint: a declaration with no
observable effect that doesn't say `pure`.

- `pure` only (`det`/`noalloc` measured too noisy).
- Top-level declarations and impl methods only; not inline closures or nested named `let`s.
- Never `main`.
- A trait-declared bound counts as written (`effectiveMethodBounds`).
- Higher-order functions *are* reported (callback effects are charged at call sites).
- Standard library impl methods are marked `pure` at the **impl**, not the trait (a trait bound
  would bind user impls).

## `effect_bounds.go` (`lyra-E015`)

`CheckEffectBounds(program)`: `pure` + `det` together on a lambda, impl method or trait method
declaration. `noalloc` is never flagged. AST-only, pre-typecheck.

## `try_outside_result.go` (`lyra-E008`)

`CheckTryOutsideResult(program, symTable)`: `?` whose enclosing function doesn't return a canonical
Result/Maybe, via `canonicalKindOfName` (read side of the collector's `resolveCanonicalTypes`). A
same-named user `data Result` is not canonical.

## `use_after_move.go` (`lyra-E019`)

`CheckUseAfterMove(program, symTable, tt)`, post-typecheck.

- A **move** is only a bare identifier of a managed value (`ownership.IsManaged`) passed to an `own`
  parameter. Scalars/stack aggregates copy; field arguments (partial moves) don't count.
- Flow-sensitive: `if`/`match` branches **union**; a loop body is seeded with every move inside it;
  a declaration or reassignment clears. Control flow is explicit, everything else via
  `ast.WalkStmt`/`WalkExpr` with pruning.
- **`let … else` is not a branch** (`letElse`): the payload binds in the enclosing scope; the else
  diverges.
- Conservative toward silence: unresolvable callees record no move. Reports dedupe by
  (binding, move site).
- Impl and default method bodies each start fresh. Not covered: moves through a nested lambda's
  captures.
- Not a memory-safety fix (ownership retains defensively); it enforces `own` and exposes the
  reuse perf cliff.

## `captured_assignment.go` (`lyra-E024`)

`CheckCapturedAssignment(program, capturesTable)`: a lambda writing a binding it captured (by
value, so the write is lost). Covers `n = …`, `n += …`, path writes (walked to the root), and
`&mut` on a captured binding (own message: take the pointer outside the closure). Only names the
capture pass recorded. Runs after captures.

## `inert_borrow_modifier.go` (`lyra-W010`)

`CheckInertBorrowModifiers(program)`: `own`/`ref`/`mut` on a copied scalar (numeric, `bool`,
`rune`). Predicate is **`types.IsCopiedScalar`, shared with the backend's `paramIsByRef`** — must
stay one predicate or `mut` miscompiles. Warning, not error (generic `own t`, scalar newtypes).
Excludes `string` (`types.IsString`), `GenericType`, aggregates. Scoped to
`LambdaExpr.Parameters`.

## `array_repeat_alias.go` (`lyra-W019`)

`CheckArrayRepeatAliasing(program, symTable, tt)`: `[v; n]` whose slots share mutable state.

- **Must run post-typecheck**: under `[][]rune` the inner repeat *infers* as fixed `[W]rune` and only
  propagation widens it to a shared `[]rune`.
- Predicate `ownership.SharesMutableState`; message path from `SharedMutablePath` (struct fields
  only).
- A count folding to 0 or 1 is silent; a runtime count is assumed plural.

## Unused loop bindings (`lyra-W020`)

`checkLoopBindings`: a `for-in` binding never read; fix is `_`. Separate from `lyra-W003` because a
loop binding can't be deleted. `_`-prefixed names exempt. Shares `referencedNames` with the
unused-local check (writes count as reads; walks into nested lambdas). **Reported at
`ForInLoopExpr.KeyLocation`/`ValueLocation`** — a zero Location escapes per-file filtering.

## `type_names.go` (`lyra-W009`)

`CheckTypeNames(program)`: a SCREAMING_CASE **struct** name lexes as `const_identifier`, so
`NAME { … }` can't parse. Structs only (data/named tuples construct by call).

## `generic_params.go` (`lyra-E031`, `lyra-W013`)

`CheckGenericParams(program)`, AST-only, pre-typecheck. A written generic list must agree with the
signature's variables: a mentioned variable missing from the list is `lyra-E031`, a listed one
never mentioned is `lyra-W013`. The list stays optional. Catches typo'd lowercase type names
(which silently become new variables) and bounds on the wrong variable.

- The `where` half is in the collector (`Collector.MergeWhereConstraints` reports `lyra-E031`).
- Variable walk is **`types.CollectTypeVars`**, shared with the typechecker (`lambdaTypeVars`) and
  backend (`mentionsTypeVar`). Nominal types (`NamedStructType`, `DataType`) are not descended.
- **Type declarations** (`checkTypeDeclGenericParams`): every variable the body mentions must be
  listed (E031, list or not; an alias gets its own message); an unused parameter is a phantom
  type and draws nothing. `collectDeclBodyTypeVars` descends one level into the declaration's
  own struct/union/data body, then uses `CollectTypeVars`.
- **Traits** (`checkTraitGenericParams`): a parameter no method signature mentions is W013; a
  method's own variables are not checked (no method-level list exists). Impls have no list.

## `range_analysis.go`

`CheckIntegerRanges(program, tt)`, post-typecheck, flow-sensitive interval analysis
(`rangeEnv` + `reachable`, cloned per branch, unioned at joins).

| Code | Definite fault | Avoids double report with |
|---|---|---|
| `lyra-E020` | overflow of `+ - *`, unary `-` | `checkIntegerLiteralRange` (literals) |
| `lyra-E021` | divide-by-zero with an *identifier* divisor proven `[0,0]` | typechecker constant fold |
| `lyra-E022` | out-of-bounds index with a *non-singleton* range | typechecker constant index check |
| `lyra-E023` | identifier assigned to a range newtype, proven outside (`checkConstraintViolation`) | `checkRangeConstraints` |
| `lyra-W011` | always-true/false integer comparison | — |

**Zero false positives**: anything imprecise widens to ⊤.

- Absent variable → ⊤. A float-adapted int literal is untracked (float source can be *wrong*).
- Interval math overflowing int64 → ⊤ (`addI`/`subI`/`mulI`/`negI` guarded). **u64 uses a `+∞`
  upper sentinel**; `compareConst` has sentinel guards. i128/u128 are ⊤.
- **C-style `for`** (`evalForLoop`): widening/narrowing fixpoint, body analyzed silently
  (`rangeChecker.silent`) then once loudly. After-loop state havocs.
- **`for … in` range** (`forInRangeKey`): binds the interval only when provably non-empty; stepped,
  two-variable, variable-length, or maybe-empty ranges havoc.
- **Block exit** builds from the *inner* env: keeps reassignments **and havocs** of outer names,
  drops locals, restores shadowed names (a pre-block snapshot reverted havocs — a miscompile;
  `TestRange_Safety_HavocInNestedBlockNotElided`).
- **`refine`** (pure, no diagnostics): comparison against a constant or tracked variable, `&&` into
  then, `||` into else, `!` swaps. Contradiction → unreachable.
- **`evalMatch`/`refineScrutinee`**: arms narrow a tracked scrutinee via `patternInterval`; a
  non-overlapping arm is unreachable.
- Compound assign is `void`-typed; its bound comes from the RHS's propagated width.
- Possible (not definite) overflow is left to the runtime trap.

**Trap elision**: also returns a **`SafetyTable`** (`driver.Result.RangeSafety`), keyed by AST node:

| Fact | Source | Backend effect |
|---|---|---|
| `NoOverflow` | `checkArith` | `applyIntMathOp` → `emitWrappingOp` |
| `NoDivZero` / `NoDivOverflow` | `checkDivision` | `emitCheckedDivOp` drops guards |
| `IndexInBounds` | `evalIndex` | `lowerIndexExpr` drops bounds trap |

A nil table / absent entry means "not safe". **Soundness requires seeing every write to a tracked
variable** — a stale interval is a missing trap in safe code (rule 18):

- **`&mut x` anywhere in a function untracks `x` there** (`mutAddressTaken` in `analyzeBody`;
  `tracked` is the single read and answers ⊤). Flow-insensitive on purpose. `&x` keeps tracking.
- Any new way to write a binding must be modelled or untrack the name.
