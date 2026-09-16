# `pkg/analyzer/typechecker` — inference and checking

Walks the collected AST, infers and verifies types, and writes results into a `TypeTable`.
Language semantics are in [LANGUAGE.md](../../../LANGUAGE.md); this file covers the
implementation.

- **Entry:** `typechecker.New(symTable, scopeTable, typeTable)` → `tc.Check(program) []TypeError`.
- **`TypeError`**: `Message`, `Location`, `Severity` (`SeverityError`/`SeverityWarning`);
  `TypeError.Diagnostic()` maps it to `diag.Diagnostic`.

| File | Concern |
|---|---|
| `typechecker.go` | core, var decls, expressions |
| `typechecker_control_flow.go` | if/match, exhaustiveness |
| `typechecker_functions.go` | lambda/call/member-call dispatch, `inferPrintCall`, return-type-directed `Trait::method(…)` |
| `typechecker_try.go` | `?`: kind and error-type rules, the `From` conversion lookup, the context pushed through it |
| `typechecker_trait_dispatch.go` | `resolveTraitMethod`, `closeOverSupertraits` |
| `typechecker_traits.go` | impl conformance, `checkTraitImplMethodBody` |
| `typechecker_trait_default.go` | trait default methods |
| `typechecker_ufcs.go` / `typechecker_overload.go` | UFCS, receiver-keyed overloading |
| `typechecker_on_demand.go` | on-demand return inference for destructures |
| `instantiate.go` | generic call instantiation |
| `contextual_lambda.go` | lambda parameter/return elaboration |
| `multi_clause.go` / `default_args.go` | clause and default-argument desugars |
| `exhaustiveness.go` / `pattern_literals.go` | tuple pattern matrix, pattern literal range (`lyra-E048`) |
| `builtins.go` | builtin methods and functions |
| `range_constraint.go` | `RangeConstraint` enforcement (`lyra-E023`) |
| `assignable.go` | `isAssignable`, `assignableValue`, `effectiveType` |
| `errors.go` | error helpers |

## Pushing a context down: `propagateExpectedType(expr, concrete)`

The **one** walk that pushes a context type onto a value — literal width *and* `shared`
flavor together. Bottom-up inference leaves untyped literals as `untyped_int`/`untyped_float`;
this narrows them.

- **Recurses** through width-preserving arithmetic (`+ - * / % %%`, unary `-`), `if`/`match`/
  block result positions, anonymous tuple literals element-wise, and array elements. **Stops**
  at identifiers, calls and conversions (`i8(x)` begins a new width).
- **Re-records a composite literal's own node type** (anonymous tuple, array) at the context's
  widths — the backend builds the aggregate from the node's recorded type, so narrowing only
  the leaves emits invalid IR (visible only to `./asan.sh`'s typed-pointer clang).
- **Narrows a leaf only if the value fits**; otherwise leaves it untyped so
  `checkIntegerLiteralRange` reports it once.
- **Int literal in a float context** is recorded at the float type.
- **Signed minimum as negated literal** (`-128` on i8): `NegationExpr` narrows the operand via
  `signedTypeMinMagnitude` (`overflow.go`).
- **A newtype context propagates its base.**
- **Flavor**: stamps the context's `shared` flavor onto every *construction* leaf reached
  (`stampSharedConstruction`/`WithAllocation`) — never onto identifiers/calls, which carry
  their own. An element flavor (`[]shared T`) reaches elements through the ordinary recursion.
  Never split this back into separate width/flavor walks: the pairing drifted before
  (segfault).
- `propagateOperandType` skips an operand already of the result type (avoids quadratic
  re-descent on long `a + b + … + z` chains).

**Context sites:** annotated `let` (`checkVarDecl`), `MathBinaryOp` result
(`inferMathBinaryExpr`), comparisons (`propagateComparisonWidth`, operands' common type),
reassignment (`checkVarReassignment`), return bodies (`checkLambdaBody`/`checkBlockReturn`),
call arguments (`inferLambdaCall`), named-tuple elements, struct fields, data-constructor
payloads (`DataTypeConstructor.FieldTypes()`), and match arms against the arms' common type
(`checkMatchExpr`). `if` must join and push back like `match` — the two are a tested pair.

## Tuples

- `inferTupleLiteralExpr`: `TupleLiteralExpr` covers `(1, 2)` and `Point(1, 2)`. Three cases:
  a data-constructor name (`findDataTypeByConstructor`); `Name == "?"` = anonymous tuple, leaves
  **left untyped** for context (`promoteToDefault` has a `TupleType` case); any other name = a
  **named tuple** (nominal), delegated to `inferNamedTupleLiteralExpr`, which validates against
  the declaration and propagates each declared element type. Named tuples don't yet infer
  generic params positionally without a turbofish.
- `types.TypesEqual`: named tuple compares by name (`!types.IsAnonymousTupleName`), anonymous
  structurally.
- `inferTupleIndexExprType`: `pair.0`, resolves via `resolveType`, bounds-checks.

## Resolving declared types

- **Resolve a declared type where it is read out** — a call's return type (`resolveTypeIfKnown`)
  and a struct field's type (`inferMemberExprType`). A raw `UnresolvedType` compares unequal to
  the resolved one; tell-tale: *"cannot assign Point to Point"*.
- `resolveType` (reports) and `resolveTypeIfKnown` (quiet) both delegate to
  **`resolveTypeWith(t, loc, leaf)`**, which owns composite recursion. Only the leaves differ:
  the reporting leaf also follows alias chains, caches, checks visibility, guards circularity.
  Covered by `tests/named_type_in_composite_test.go`.
- `stripNewtypeResolving` is the one way to see through a newtype (resolve between strips).

## `propagateInstantiation(expr, want)`

Pushes a context's `ParameterizedType` onto construction leaves (through match/if/block arms).
A construction is an instantiation only if it solves **every** parameter itself (`None`,
`Ok(v)` don't); this supplies the rest from context. Called from annotated `let`, the three
return-body sites, concrete call arguments, and generic call arguments (`instantiate.go` —
what makes `unwrap_or(None, 42)` work).

- **Checks, doesn't assume**: each payload element is re-checked under the substitution; on
  mismatch the node stays bare so it can't lower as that instantiation.
- **Guards**: node must be *open*, same declaration, same arity.
- **Open** (`stampableDataType`): the bare declaration, or an instantiation reached only by a
  guess (`markDefaultedConstruction`, predicate `payloadIsAGuess`): a defaulted untyped
  literal, an array literal recorded fixed, an anonymous tuple holding a guess. Everything else
  is closed — overriding a program-determined instantiation would let a real mismatch through.
- **`fieldTakesWidthFromSolve`**: a type-variable field takes width from the substitution and may
  defer; a concrete field (`Wrapped(u8)`) must be narrowed on the spot. **A narrowed literal is
  range-checked against its new type, on both paths** — `stampDataConstruction` for a solved
  payload and `inferTupleLiteralExpr` for a concrete declared one. The concrete side was missing
  it until 09/16, so `Wrapped(300)` truncated to 44 in silence while the generic spelling of the
  same constructor reported; assignability cannot answer it, since after narrowing the payload
  really is a `u8` holding 300.
- Covers data constructors, generic structs and named tuples. A bare `DataType` is assignable to
  any instantiation while bare struct/tuple types are not, so every context site goes through
  **`contextualType`**: propagate *before* the assignability check, re-read the record, and
  report whether a diagnostic was already emitted.
- **Arrays** (`propagateArrayInstantiation`): recurses per element, then re-records the literal's
  type. Runs only when the element type contains an instantiation, and re-records only if every
  element now reads as the context's element type.
- **Partly solved struct literal** (`structShapeForInstantiation`): open when some type argument
  is still a bare generic declaration. Not `instantiationIsSettled`, which asks about untyped
  literals.

## Statements and assignment

`checkNode` / `checkVarDecl` / `checkVarReassignment` / `checkExpressionStmt`.

- **Assignment to a parameter**: `checkAssignToBinding` resolves `tc.paramTypes` *before* scope
  lookup, then shares `checkAssignedValue` with the variable path. Only `own`/`mut` parameters
  may be reassigned (`lyra-E025`); the same rule and code cover pattern bindings (own wording).
  Shadowing (`let s = s ++ "!"`) is the replacement, which is why use-before-declaration seeds
  parameters (`checkStatementsInScope`).
- **Sequential rebind** `let x = x + 1`: the collector's `RedefineVariable` overwrites the
  binding and records `VarDeclStmt.Shadows`; `checkVarDecl` sets `tc.currentVarDecl` so the
  `IdentifierExpr` case redirects to `.Shadows`. Without it the RHS type is nil.
- `checkIfDestructuringStmt` (names scoped to `Then`, scope keyed on the `*IfDestructuringStmt`)
  / `checkElseDestructuringStmt` (names persist in the enclosing scope); both reuse
  `checkDestructuringDecl`.
- **Destructuring parameters** bind in `withParamScope` via `walkDestructuredPattern` against the
  annotation. Unannotated ones stay undefined, except in a trait-impl method
  (`checkTraitImplMethodBody`), where the trait signature supplies the type.

### Storing a value into a slot

Five positions: annotated `let`, destructuring value, reassignment, member/index assignment,
`return`. They share only `checkStorable` (assignable + allocation agreement); everything else
differs deliberately — do not fold into one options-bag function. `checkLValueAssignment` does
no literal propagation (open question, commented).

- `shown`: the target as written (`cannot assign string to Alias`, not the resolved `i64`).
- `subject`: binding-name prefix (`x: cannot assign …`), empty for lvalue paths.

## Trait dispatch: `resolveTraitMethod(receiverType, methodName, requiredTrait)`

Finds every impl whose target matches (`implTargetMatches`) and provides the method; several
matches with no `requiredTrait` is the ambiguity `Trait::method(...)` resolves. Drives
`inferMemberCall`'s fallback and `inferTraitMethodPathCall`; records into `tc.MethodTable()`.
`resolveTraitMethodNamed` is the shared scan behind `.method()`, operators, `==`/`<`
(`dispatchEq`/`dispatchOrdCompare`).

- **Generic impls**: lowercase `GenericType`s in the target unify with the receiver
  (`unifyGenericTarget`) with binding consistency; `Self` is substituted with the receiver.
- **Bounded impls**: `checkImplConstraints` checks each `where` bound via `typeImplementsTrait`
  (single level — a satisfying impl's own bounds are not re-verified).
- **Generic field access**: `resolveGenericAggregate` substitutes type arguments into
  struct fields/tuple elements/data payloads (`substituteGenerics` → `types.Substitute`). Field
  lookup only; dispatch keeps the `ParameterizedType`. Param names come from
  `decl.GenericParams`, since `NamedStructType.GenericParams` is not populated.
- **Bound dispatch** (`dispatchViaGenericBound`): a call on a bare type parameter resolves through
  `tc.genericBounds`, typed against the trait signature with `Self` = the parameter. Recorded as a
  `BoundMethodRef` (`SetBound`); purity joins over all impls. Candidates per implementing type are
  published via `SetBoundCandidates`; a generic impl's candidate at a concrete type is published
  where one is seen (`checkGenericBounds`, `publishImplBodyCandidates`,
  `publishDefaultBodyCandidates`), and for a specialization the driver composes through
  `PublishCandidatesForInstantiation` / `PublishCandidatesForMethod` (`publish_composed.go`).
- **A trait method's own type variables** (`b` of `mapv: (Self, (i64) -> b) -> b`) are solved by
  `solveArgumentTypeVars`, the generic-call solver: `solveMethodTypeVars` at a concrete dispatch
  (the receiver seeds it, for `Self<a>`), `solveBoundMethodTypeVars` through a bound (the receiver
  seeds it too, so a default body's `self: Self<a>` solves `map`'s `a'`), which records
  the solution under the trait's names (`SetBoundMethodVars`) for the driver and backend to compose
  per specialization. A method variable clashing with an impl's (or a bound receiver's) is primed
  (`methodSignatureRenamedAway`); `Resolution.MethodVarNames` carries the rename.
- **Trait type parameters** (`impl Get<t> for Box<t>`): `TraitImplStmt.TraitArgs` maps trait
  params to impl args, applied by `substituteSigGenerics`. `TraitArgs` is collected with
  `FieldNameForChild` iteration; `impl.GenericParams` stays empty; bounds are in
  `impl.Constraints`.
- **Supertraits**: `closeOverSupertraits` expands at the two writers of `tc.genericBounds`
  (`pushGenericBounds`, `checkTraitImpl`); cycle-safe by visited set.

**Cost:** `boundCandidatesByType` looks quadratic but filters by trait name before the expensive
compare (indexing by trait measured at zero gain). `implTargetMatches` must not allocate
`bindings` on the no-match path. Benchmark with `BenchmarkDispatch_*` in `pkg/driver` — it must
use generic bodies with `where` bounds; concrete callers measure nothing.

## Trait default methods (`typechecker_trait_default.go`)

- `checkTraitDefaultMethods` runs once per default with `self: types.GenericType{"Self"}` and
  `tc.genericBounds["Self"]` = the declaring trait closed over supertraits, then calls
  `checkTraitImplMethodBody` (same path as an impl clause). Backend needs nothing.
- Must run **after** `tc.traitImpls` is collected, or candidate sets are empty and the body won't
  lower.
- `resolveTraitMethodNamed` tries impl clauses first, `defaultMatch` last. `Self` **joins** the
  impl's own bindings (`t→i64` and `Self→Box<i64>`).
- `publishDefaultBodyCandidates` mirrors `publishImplBodyCandidates`, with the same re-entry
  guard.
- Inside a default (`tc.currentDefaultTrait`), a missing method names the trait/supertrait fix,
  not `where Self: …` (unwritable).
- It is a setup pass, so it must install the module scope itself (`moduleScopeOf`).

## Builtins (`builtins.go`)

- **Methods**: `builtinMethodSignature(recv, name)` returns a receiver-specialized
  `*LambdaType` (call args only). Consulted **last** in `inferMemberCall`, so user fields, impls
  and UFCS shadow builtins. Includes overflow arithmetic (lowered in `backend/llvm/wrapping.go`),
  `floatRoundingOps` (`floor`/`ceil`/`round` → `i64`, `rounding.go`), `len` on arrays
  (`lowerArrayLen`). `SetBuiltinMethod(call, allocates)` records the resolution for purity.
  Overflow builtins are refused on a newtype receiver (`lyra-E043`).
- **Functions** (`isBuiltinPrintFn`/`isPrintableType`, `inferPrintCall`): resolved in
  `inferIdentifierCall` only after scope misses, so user `print` shadows. `print`/`println` are
  polymorphic over printable scalars and settle an untyped argument via
  `propagateExpectedType(arg, promoteToDefault(argType))`. Effects live in
  `checker/effects.go`'s `builtinEffects`.

### Untyped literal receivers

`inferMemberCall` promotes an untyped receiver for *matching*, but writing it to the node is
deferred: `pinReceiver` is called by the rung taken, and the UFCS rung calls it only when the
candidate's `self` mentions no type variable (otherwise `200.min(w)` on u8 binds `t = i64`). A
deferred receiver is `Arguments[0]` and adopts by the untyped-argument rule.

## Newtype constraints

- `checkPatternConstraints`: a string literal against a `PatternConstraint`; `regexPatternBody`
  strips `r"…"`. Regex syntax validation happens once, here.
- `checkRangeConstraints` (`range_constraint.go`, `lyra-E023`): a compile-time numeric constant
  (literal, negation, folded arithmetic) against a `RangeConstraint`; bounds folded by
  `foldConstraintInt`/`Float` (unfoldable bound → unenforced side).
- Flow-proven identifier values are `checker`'s `checkConstraintViolation` (same code, no
  double report). Sites that can't settle statically are published for a runtime trap.
- `checkIntegerLiteralRange` checks a newtype against its base **only when it has no
  `range(…)`** — otherwise `checkRangeConstraints` owns the report.

## Match exhaustiveness (`typechecker_control_flow.go`)

`*MatchIsExhaustive` functions; tests in `tests/match_expr_*.go`. Code `lyra-E009`.

- **Severity**: closed scrutinee (`bool`, `data`) → error; open (numbers, strings, runes, arrays,
  tuples, structs) → warning. The backend traps on fall-through.
- **Struct**: exhaustive when an unguarded arm is irrefutable (`patternIsIrrefutable`/
  `aggregateMatchIsExhaustive`), mirroring the backend's `aggPatternTest` nil condition.
- **Tuple and `data`** (`exhaustiveness.go`): Maranget pattern matrix — when column 0's rows
  name every constructor, specialize by each and recurse; otherwise the default matrix (which is
  also what terminates on a recursive type). Enumerable columns: `data`, `bool`, and a tuple or
  struct as one constructor. An uninterpretable pattern drops its row (over-warns, never goes
  quiet). Guarded arms never count. A tuple match needs it because every multi-clause function
  desugars to one; a `data` match (`dataMatchCoverage`) reports constructors no arm names apart
  from those named for only some payloads.
- **Array**: a union over lengths — `[e1..en]` covers n, `[…, ...rest]` covers ≥ n; only arms
  with all-irrefutable elements contribute.
- **Rune**: char-literal arms plus a required catch-all (`checkRuneMatchArm`).
- **Kind dispatch strips newtypes** (`types.StripNewtype` in `checkMatchExpr`); data/tuple/struct
  branches keep the unstripped type (E041 makes that sufficient).
- **Pattern literals are value-checked first** (`pattern_literals.go`, `lyra-E048`), width and
  newtype range, since patterns lower at the scrutinee's width. It is a deliberate separate mirror
  of `walkDestructuredPattern` (whose errors `withPatternBindings` discards); unpairable → skipped.
  An exclusive range end checks bound − 1.
- **Nested kinds are the walk's**: an arm check is one level deep, so once it passes
  `checkNestedArmPattern` runs `walkDestructuredPattern` for its errors — every scalar leaf goes
  through `checkScalarPattern`, the same per-kind checkers. Positions (tuple rest included) come
  from `ast.MatchPositions`, the one pairing rule every pass shares.

## Generic functions

A lowercase type name is a `types.GenericType`; uppercase is `UnresolvedType`.

**Instantiation** (`instantiate.go`): unify each parameter against the argument
(`unifyGenericTarget` — one unifier shared with trait dispatch), check against the substituted
signature, return the substituted return type.

- Arity first. An untyped literal argument settles to its default before binding (a type
  variable decides widths in codegen). Every variable must be solved — a return-only one is
  reported at the call unless a context or turbofish binds it.
- **Array literal against `[]t`** (`arrayLiteralAsDeclared`): shape read from the declaration.
  Against a bare `t`, a fixed-array literal speaks last and adopts a binding it can be built as.
- **Elements take element context before joining** (`elementTakesContext`): array/repeat/tuple
  literals and constructions, never scalar leaves. Refused payloads go in `contextRefused` so
  they aren't reported twice.
- **An array literal's flavor is its spelling** (`[…]`/`[v; n]` → `[]T`, `#[…]`/`#[v; n]` →
  `[N]T`, the collector's `Fixed`). The walk narrows elements and re-records a literal **in its
  own flavor only**; a context of the other flavor is `lyra-E079`'s (`reportArrayLiteralFlavor`,
  from `contextualType`, `elementTakesContext`, both constructor stamps, generic calls and union
  members). Nothing chooses a flavor from context.

**`isAssignable` vs `assignableValue`**: `isAssignable` is types only and never crosses array
flavors; within `[]T` only unsettled elements (none, untyped leaves) may differ
(`unsettledElementAssignable`). `assignableValue` adds the expression-dependent narrowing — a
literal's untyped elements take the target's (`literalTakesShape`, walking expression and type
together through array literals, repeats, branches, newtype targets, tuple elements), within one
flavor. Never convert a *built* array (hidden allocation; a binding reaching a `[]T` slot
segfaulted). Value-vs-type sites use the second, type-vs-type the first.

**Function types**: `unifyGenericTarget` and `substituteGenerics` handle `*types.LambdaType`;
parameters unify in the same direction as the return (pattern unification, not subtyping).
Substitution returns a **copy** — `LambdaType` is held by pointer and shared.

**Generic aggregate inference**: struct type arguments are solved from field values by
`unifyGenericTarget` (so `inner: Maybe<t>` pins `t`), and declared field types are substituted
structurally. `mentionsGenericParam` walks the type, so a partly substituted `Maybe<t>` counts as
incomplete; `propagateInstantiation` re-checks what it defers.

**Unbounded type variables** support only pass/return/store; operators need a `where` bound.

### Monomorphization (backend)

- `backend/llvm/monomorphize.go`: one function per instantiation (`identity$i64`), keyed by
  `Key()`/`SpecKey()`; bare generic never emitted. **By substitution, not AST cloning**: the
  substitution is consulted by `lowerType` and `recordedType`. `defineFunctionInto` is shared with
  plain functions.
- **Ownership runs per instantiation** (`ownership.AnalyzeLambda`, `driver.Result.OwnershipBySpec`)
  — a correctness requirement (generically, `t` isn't managed → double free at `t = string`).
  Tables are keyed by AST node and cannot be merged. Backend reads via `l.ownership()`; the pass
  substitutes in `analyzer.typeOf`.

## Generic types (`Box<t>`, `Maybe<t>`, `List<t>`)

- **Front end**: a construction evaluates to a `ParameterizedType` (`parameterizedResult`). Structs
  infer from field values; data constructors and named tuples solve positionally
  (`solveDataTypeVars`). A partly solved substitution does **not** become an instantiation.
  `resolveGenericAggregate` serves all three shapes. `ParameterizedType.String()` renders `Box<i64>`.
- **Backend** (`generic_types.go`): one LLVM type per instantiation (`%Box$i64`, named by
  `typetable.TypeSymbol`), materialized **lazily** in `lowerType`. The generic declaration
  registers nothing (`lowerTypeDecl` skips it). Declare-then-define: the placeholder is registered
  before fields lower so a recursive `shared List<t>` tail terminates. `resolveInstantiation` is
  the choke point (applied in `recordedType` beside the newtype strip) — it needs an arm for every
  shape a parameterized type can expand to, including `*ConstrainedType`.
- **Managed type arguments**: `ownership.OwnsManaged` → `parameterizedOwnsManaged` substitutes and
  re-asks, so pass and backend use one predicate (mismatch was a double free). macOS ASan missed
  it; `TestEmit_GenericManagedMatchesConcrete` compares retain/drop counts to a concrete twin.
- Open: a `where` bound on a generic type's parameter is not enforced at instantiation.

## Contextual typing for lambdas (`contextual_lambda.go`)

- `elaborateLambda` fills missing parameter/return annotations **on the AST node**, before the
  body is inferred. Only fills blanks; explicit annotations win and are still checked. Wired at
  an annotated binding (lambda branch of `checkVarDecl`), direct call arguments, generic call
  arguments.
- **Return inference** (`checkLambdaBody` → `inferLambdaReturnType`) writes the body's type onto
  `ReturnType`. Refused with an explicit `return`. `ast.LambdaExpr.ReturnTypeInferred` exists only
  for `ResolveEntryPoint` (`let main = () => { 0 }` stays void) — nothing else reads it.
- **Generic path ordering**: `solveTypeVars` defers *incomplete* lambdas (`needsContextualTypes`);
  fully annotated ones are not deferred. A type still mentioning an unsolved variable is never
  planted (`isConcreteEnoughToElaborate`), and `inferGenericCall` elaborates again after
  `instantiateSignature`.
- **`plantableVars(subst)`**: variables mentioned by the substitution's *values* — the caller's own
  type parameters (e.g. `t` inside `sort<t>`) — may be planted. A variable solved only by the
  lambda's body (`u` in `map`) appears in no value and stays blank.

## Multi-clause functions (`multi_clause.go`)

`desugarClauses` in `checkLambdaBody` (front end, because the backend reads types by node
identity) rewrites clauses into `match (p0, p1) { … }`.

- One parameter is matched directly, not as a 1-tuple.
- Clauses are consumed (`LambdaClauses = nil`) so bodies aren't checked twice.
- Arity checked here with counts named.
- No matching clause traps (sealed fall-through, exit 101).
- A clause may rename a parameter; downstream name-keyed passes must use `addMatchAliases`.

## Default arguments (`default_args.go`)

- `applyDefaultArguments` appends declaration defaults for omitted trailing arguments before
  arity/generic handling. Idempotent. The appended expression is the **same AST node** as the
  declaration's default (shared across call sites; sound because its type is the parameter's).
- `checkDefaultsAreTrailing` rejects a defaulted parameter before an undefaulted one.
- A default on a lambda used as a *value* is refused — `LambdaType` records that a default exists,
  not what it is.

## On-demand return inference (`typechecker_on_demand.go`)

A destructure (`let (w, h) = viewport()`) is the one position that can't defer a callee's type, so
`forceCheckDestructureCallee` checks that declaration now and `checkDestructuringDecl` retries.

- **Memoized** by `checkedDecls` (else duplicate diagnostics).
- **Cycle-guarded** by `inferringRet`; fallback `lyra-E058` asks for an annotation.
- **Checked at top level** (`atTopLevel`): parameter scope, enclosing return, `where` bounds and
  impl/trait context are cleared and restored — `withParamScope` copies enclosing params, which
  would otherwise leak the caller's names in (false accept).
- Only destructures trigger it; no eager inference elsewhere.

## UFCS (`typechecker_ufcs.go`)

`m.unwrap_or(0)` → `unwrap_or(m, 0)` when the function's first parameter is named `self`. Ladder in
`inferMemberCall`: **field → trait method → UFCS → builtin**.

- **Desugared in place** (`desugarUFCSCall`): receiver becomes `Arguments[0]`, callee an
  `IdentifierExpr`. Downstream passes never see UFCS; both spellings emit identical IR. Required,
  not a shortcut: purity indexes arguments positionally (`checker/ufcs_bounds_test.go` guards it).
  Idempotent; pre-typecheck passes see the un-desugared form.
- **Type-parameter receivers**: `callViaUFCS` in the generic-receiver branch, below the `where`
  bound. `receiverAccepts` unifies the candidate's `self` with the *candidate's* variables as
  wildcards, so only functions generic in their receiver match.
- **Candidates gathered by name** (`ufcsFunction` over `SymbolTable.FunctionsNamed`) and filtered:
  takes `self`, reachable (`ufcsImportedIn`), accepts the receiver. Own module wins a tie; a
  remaining tie is reported with a typeable qualifier. Resolving through one key silently hides
  candidates in other modules.
- The `self` test is on the declared parameter, never on `LambdaClauses` (consumed by the desugar).
- **Import required** (own module and prelude exempt). `UFCSModules()` and `DispatchedTraits()`
  (from `MethodTable.DispatchedImpls()`, keyed by trait name) feed the unused-import check so it
  doesn't advise deleting a needed import.
- **`own` receiver refused** with its own error (not "has no method").
- `UFCSCallable` is exported for LSP completion.

## Receiver-keyed overloading (`typechecker_overload.go`)

One name may be declared several times in a module when every declaration takes `self` and the
receiver heads (`types.HeadName`) differ.

- Resolution is `receiverAccepts`, asked from exactly two sites: `inferOverloadedCall` (bare call)
  and the UFCS rung (before desugaring).
- Overlap is refused at the declaration (`ast.OverloadableWith`). A `self: t` has no head and can't
  be an overload member.
- **An overloaded name is absent from `SymbolTable.Functions`**; the set is in `OverloadSets` and a
  scope holds an `ast.OverloadSet`, so a pass asserting `*VarDeclStmt` fails instead of guessing.
- **The resolved callee is published** (`TypeTable.SetCallee`); consumers read it first, then fall
  back to lookup.
- Using an overloaded name as a value is an error.
- **Bare calls fall back like method calls** (`receiverFallback`, `bareCalleeFor`): scope chain
  first; only if the hit takes a `self` it doesn't accept are candidates gathered. So every user
  function is recorded by declaration (`recordByDecl`) for the backend.
