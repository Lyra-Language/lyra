# `pkg/backend/llvm` — the LLVM IR backend

Companion specs: `ALLOCATION.md` (boxes, refcount runtime, ownership, Perceus, drop glue),
`DATA_LAYOUT.md` (tagged unions), `STRING_LAYOUT.md`, `SIMD.md` (roadmap). FFI and
`extern` implementation notes: `../FFI.md`.

**Standing rule: a form that does not lower yet is a loud error, never wrong code.** That
includes repeating a front-end check, and never letting llir panic (`ir.NewStore`,
`NewInsertValue`) or clang reject a module (`joinPhi` checks phi incomings first). Any site
building an instruction from another pass's value owes that check.

## Overview

Built on `github.com/llir/llvm` v0.3.6 (pure Go, emits **typed** pointers such as `i8*`).
`llvm.New()` returns a `*Backend`; `Emit` builds an `ir.Module`.

- **`@main` is always `i32`** at the ABI level whatever Lyra's `u8`/`void` entry returns;
  the body value is coerced (`coerceIntWidth`) and zero-extended into it (`entryABI`).
- **Integer arithmetic is checked** (`trap.go`, via `emitTrapIf`): `+ - *` use
  `llvm.{s,u}{add,sub,mul}.with.overflow`; `/ % %%` guard zero and signed `INT_MIN / -1`;
  unary `-` guards `-INT_MIN`. `%` truncates, `%%` floors.
- **Elision**: a check is dropped when value-range analysis proved it cannot fire
  (`res.RangeSafety`: `NoOverflow` → `emitWrappingOp`, `NoDivZero`/`NoDivOverflow` in
  `emitCheckedDivOp`, `IndexInBounds` in `lowerIndexExpr`). That is only sound if the
  range pass models every write to the variable — a stale interval is a **missing trap**
  (lyra/CLAUDE.md rule 18).
- **Traps** are noreturn runtime functions writing `lyra: …` to stderr and `exit(101)`:
  `lyra_panic_overflow`, `_divide_by_zero`, `_index_out_of_bounds`,
  `_string_index_out_of_bounds`, `_array_slice_out_of_range`, `_string_slice_out_of_range`,
  `_shift_overflow`, `_range_step`, `_float_to_int`, `_constraint`, `_match_failed`,
  `_interior_nul`, `_negative_length`, `_negative_byte_len`, and `lyra_panic` (user
  `panic`).
- **Top-level `const`** has no storage: `Emit` records them in `l.consts` and an
  identifier read that misses `l.locals` inlines the value expression. A local `const` is
  an ordinary alloca.
- **Locals** are entry-block `alloca` + store/load (mem2reg builds SSA). `l.locals` maps
  name → slot pointer; a by-ref parameter's slot is an `ir.Param`, so read a slot's pointee
  type with `slotElemType`, never `slot.(*ir.InstAlloca)`.

## Statements, control flow and functions

### Block termination

`return`, `break`, `continue`, `panic` and a fully-diverging `match`/`if` seal a block
mid-stream. So:

- `lowerBlockStmts` (value-optional) stops once `block.Term != nil`; `lowerBlock` requires
  a value; `lowerForEffect` is for loop bodies and one-armed `if`.
- **Every fall-through `br` at a join and every phi incoming is guarded on
  `Term == nil`.**
- `if` branches and match arms go through `lowerBranchValue` (value-optional). Four sites
  lower an arm body — `lowerMatchLadder`, the `data` tag switch, two in `match_array.go` —
  keep them in step.
- `matchMerge.value()` distinguishes a **void** merge (reached, no value) from one **no arm
  reached**, which is sealed with `unreachable`. `lowerIf` does the same for an unreachable
  merge. `lowerVarDecl` refuses binding a void initializer with an error naming the
  binding.

### `panic`, `never` and `diverged`

`lowerPanicCall` emits `call @lyra_panic(i8*, i64)` + `unreachable` and returns **nil with
the block sealed**. The message is a (pointer, length) pair because it is a runtime
string; the shim writes prefix, message and newline as three `write`s (no allocation on a
failure path).

**Every consumer of a lowered operand must test `diverged(v, block)`** (nil value *and*
sealed block — several lowerings legitimately return nil and continue). Checked in
`lowerVarDecl`, `lowerVarReassignment`, `lowerCallArgs` (the one argument loop for direct
and indirect calls), `lowerNumericConversion`, array-literal elements, and
`coerceAggregateElem` (the aggregate funnel, which errors on nil). A missed site
serializes a nil operand and crashes at `m.String()` with no useful trace. A tuple
containing `never` fails as `unknown type: never`.

### `?` (`try.go`)

A match in disguise that seals the failure path and returns the success block.

- **The error is rebuilt, not forwarded**: `Result<i64,string>` and `Result<bool,string>`
  are different unions. The payload is extracted and a fresh `Err`/`None` built at
  `l.retLyra` (unlowered, stored unsubstituted, read through `applyTypeSubst` so a generic
  specialization rebuilds at its instantiation).
- Shape recognized by constructor names (`canonicalTryShape`), which
  `collector/canonical.go` pins for `@builtin` Result/Maybe.
- **Refcount**: a borrowed operand (binding) keeps its reference, so the propagated payload
  is duplicated; an owned temporary's reference moves into the rebuilt error. Inverting
  either is a leak or a use-after-free (`TestExec_TryBorrowedOperand`,
  `TestASan_TryManagedPayload`).
- **A conversion** (`MethodTable.TryConversion`, `impl From<E1> for E2`) is called on the
  extracted error in `lowerTryPropagate`: its result is a fresh +1, so nothing is duplicated
  and the operand's own temporary is released like any other (`transferred` is cleared).
- **The error block releases the statement's temps itself** (`releaseTempsOnExit`) and
  raises `pendingBase` so `emitReturn`'s flush skips them — flushing from there would
  release in the producing block, *before* the tag test, on the success path too.
  `releaseTempsOnExit` does **not** truncate the pending list (the success path still
  needs its flush) and only releases temps produced in the branching block (dominance);
  conditional sub-expression temps leak, the safe direction.

### `??` (`null_coalescing.go`)

`match a { Some(v) => v, None => b }`: the default is a **lazy arm** (`m ?? panic(…)` is
meaningful), both arms feed one phi. The Some payload has no node to mark, so its +1
(`deepRetain`, duplicate never move) is emitted here. Non-Maybe left operands are
`lyra-E049` in the front end; the backend's refusal stays as a defense.

### Statement temporaries (`flushStmtTemps`, `dominators.go`)

An owned temporary is released after its statement:

- If its production block **dominates the statement's end block**, release there.
- Otherwise (produced inside an `&&`/`if`/`match` branch) release **where control leaves
  the region its production block dominates** (`regionExits`) — never in the production
  block itself, which may precede later uses in the same branch.
- The dominator tree is **rooted at the statement's start block** (`newDomTreeFrom`), not
  the function entry: an enclosing block may still be unsealed, which makes everything look
  unreachable and every dominance answer false — here false means an early free.
- An exit that is also a back-edge into the region leaks; `break`/`continue` blocks are
  left to exit releases. Temps of an enclosing in-flight statement are protected by
  `pendingBase`.

**Exit releases**: `break`/`continue` owe the pending temps of every statement they skip.
Dominance is only final once the CFG is, so `lowerBreak`/`lowerContinue` record the
obligation (`recordExitReleases`) and `resolveExitReleases` emits it after the body.
Appending to a sealed block is safe (llir prints `Insts` before `Term`). `loopCtx.tempBase`
bounds which temps are the jump's — ones below it belong to a statement enclosing the loop
and are released after it.

**`lowerMatch` owns its scrutinee's temps**: it lowers the scrutinee once for all five
shapes and releases its temps at the **merge**, raising `pendingBase` while arms lower.
**Do not use dominance here** — a `data` match emits its `switch` after the arms, so the
entry is unsealed mid-lowering and every query is false.

**Branchless builtins**: `read_line`, `<=>`, `compare_bytes_at`, `byte_offset` emit no
branches at the call site, so an owned result behaves like an ordinary call's.

### Loops and ranges (`control_flow.go`)

- Three `for` forms (infinite, condition, C-style) with labeled `break`/`continue` via the
  `l.loops []loopCtx` stack. A `loopCtx` carries frame and temp depths.
- **`for x in xs`** over `[N]T`, `shared [N]T` or `[]T` is an index-counter loop; the loop
  variable **borrows** the element (not framed). `for i, x in xs` binds the index too
  (collector: `Key` = index, `Value` = element; single-variable form puts the element in
  `Key`).
- **`for c in s`** walks runes (`lowerForInString`, `lyra_utf8_decode`), advancing by each
  rune's byte length.
- **Ranges** (`lowerForInRange`): the counter is the loop variable, width from the first
  concrete bound (else i64). **The advance is guarded** so `0..<=MAX` visits MAX and exits
  instead of wrapping, and a big step cannot leap an exclusive end. A runtime step ≤ 0
  traps (`lyra_panic_range_step`). Direction comes from the operator only
  (`types.RangeDescends`); `rangeLoopPredicate` picks from direction × inclusivity ×
  signedness.

### Scoping

`l.locals` is snapshotted by `pushLocalScope`, which returns the restore, at: each block,
each loop, each `if let` branch, and **each match arm (reset per arm, not just restored
after)** — otherwise an arm reads an earlier arm's shadowing slot. Scoping is independent
of ownership frames, which track slots.

### Functions

Two passes in `Emit`: `declareFunction` for every function, then `defineFunction`. Per-body
state resets in `beginFunction` (`locals`, `loops`, `retType`, `retSigned`, `retLyra`,
`entryABI`). **`emitReturn` is the one return path** (coercion, main's u8→i32, frame
releases, `inlineRet` for sequences). Void bodies lower for effect and return through
`emitReturn(nil)` so temps flush.

`bindParameters` binds parameters for every function shape — plain, specialization, lifted
closure (param 0 is the environment, hence its offset), trait method.

Front-end desugars mean these never reach here: default arguments
(`typechecker/default_args.go`; `lowerDirectCall` still checks arity) and multi-clause
functions (`typechecker/multi_clause.go`).

**Emitted symbols**: user functions are `lyra.<module>.<name>` (`userSymbol`),
specializations likewise (`lyra.identity$i64`). `main` keeps its name; runtime symbols use
`lyra_`, which a user symbol (dot after `lyra`) cannot spell. Extern symbols are the C name
(see `../FFI.md`). Trait methods are `Type$Trait$method`, generic types `Box$i64`, both
module-qualified when private.

### Operators

- **`!x`** is `xor i1 x, true` (`lowerNotBooleanExpr`). The grammar's `_not_operand` keeps
  `!a && b` as `(!a) && b`; `TestExec_NotBindsTighterThanAnd`/`…Or` assert on a side effect.
- **`<=>`** (`lowerSpaceship`) yields `Ordering`, branchless: two `select`s and an
  `insertvalue` of the tag on an undef union. Predicates follow the **operand's
  signedness**; tags come from `findConstructor`. Floats are refused in the typechecker.
- **`& | ~`** are `and`/`or`/`xor`; prefix `~x` is `xor x, -1`. None trap.
- **Shifts** (`emitShiftOp`): the count is checked *before* being coerced to the value's
  width (coercing first could truncate an out-of-range count into range); the check is one
  **unsigned** compare against the width (catches negatives) → `lyra_panic_shift_overflow`;
  a constant in-range count (`constShiftInRange`) emits no check. `>>` is `ashr` for signed,
  `lshr` for unsigned (`TestExec_ShiftRightSignedness`). Range-based elision for a variable
  count is not wired.
- **`icmpPred(rel, signed)`** is the one place signedness picks an integer predicate.

## Closures (`closures.go`)

A function value is `{ i8* fn, i8* env }` for every function type — named function,
captureless lambda or closure — so no per-call-site specialization.

- `fn` is the lifted body, `ret (i8* env, params...)`. `env` points at the **payload** of a
  ref-counted box whose first payload slot is the capture-set's drop glue, so
  `managedBox` and retain/release work unchanged. Releases go through one trampoline,
  `lyra_closure_env_drop`, which reads the glue from the environment — a release site only
  knows the static function type.
- Every nested lambda is lifted to `@lyra_closure_N` (`collectNestedLambdas`,
  `declareClosure`/`defineClosure`), all declared before any body, **bodies lowered last**,
  never re-entrantly.
- **Inside a generic body there is one closure per specialization**: `l.closures` is keyed
  by `closureKey{lambda, spec}`; `collectNestedLambdas` skips generic bodies and
  `declareSpecialization` emits their closures under the substitution. A capture's type is
  read only through **`capturesOf`**, which substitutes (the third type funnel beside
  `lowerType` and `recordedType`).
- **A trait-method body is the other generic body**: `collectNestedLambdas` skips trait
  declarations and impls too, and `traitMethod` declares the body's closures under the
  resolution's bindings and `SpecKey` (`definePendingTraitMethods` defines them after the
  body). A lambda there may mention the impl's variable (`(y: t) -> t` in `impl … for
  Box<t>`) or a default's own (`(x) => true` at `x: a`), which the program-wide walk
  declared once with no substitution — *"type variable t has no concrete type here"*.
- **`declareClosure`/`defineClosure` run from top-level loops, so they must
  `enterModuleOf` themselves.** Rule for any new such path: if it lowers a signature or
  body from a top-level loop, it enters its own module.
- A named function used as a value gets a thunk `@name.closure` that ignores `env`; direct
  calls keep the plain signature. A captureless closure shares the pinned static
  `.closure.empty_env` (null drop fn) — no allocation.
- Captures are **by value**, each managed one retained into the environment by `buildEnv`
  (the ownership pass never sees capture reads). The body binds them as **borrows**.
- `lowerIndirectCall` unpacks the pair, bitcasts `fn` to the callee's static `LambdaType`,
  and passes `env` first. Reached from a local, a parameter, a struct field holding a
  function (told apart from a builtin method by looking the field up on the struct), or any
  callee expression.
- Loud errors: a `mut`/`ref` or default parameter on a lambda used as a value (a function
  type carries neither).

## Types

### Literals

`literalIntType` reads the recorded width (fallback i64); a literal the typechecker placed
in a float context lowers as a float (`literalRecordedFloatType`). `floatConst` **rounds** a
literal to its target width before handing it to llir, which would otherwise truncate the
mantissa (`let x: f32 = 0.1` held a different number). A comparison width mismatch is a
defensive loud error.

### Type declarations

`Emit` runs `lowerTypeDeclarations` (empty named placeholders for every decl) then
`lowerTypeDefinitions` (fill fields), so order is irrelevant and forward references work.
`l.structTypes` is keyed by the declaration's **type key**, not `TupleType.GetName()`.

- Fields: stack → by value; **`shared` → box pointer**; **`weak` → `i8*`** (the box; takes
  the weak half of the protocol, `weak.go`). Both make recursive types finite.
- `data` → `DataUnionType` (DATA_LAYOUT.md).
- `resolveForLayout` deep-resolves `UnresolvedType` leaves and normalizes
  `ParameterizedType` through `resolveInstantiation`, short-circuiting at `shared` (which
  is also why it terminates). **While resolving a declaration's fields it resolves from the
  declaration's location** (`resolvingFrom`/`declLocOf`), not `l.currentLoc`.
- `resolveInstantiation` is the choke point that turns a `ParameterizedType` into the shape
  it denotes, so downstream switches need no generic case; it must handle every shape a
  parameterized type can expand to, including a newtype wrapper.
- Unions: `union.go`, see `../FFI.md`.

### Per-module type identity (`type_identity.go`)

A type name is not program-wide. `l.structTypes` is keyed through `l.typeKey(name)`,
matching the symbol table's `<module>::<name>` keys; `lookupTypeDecl(name)` is the backend's
`LookupTypeFrom` (a bare `LookupType` cannot see private declarations).

- **The asking location is ambient**: `l.currentLoc`, set by `enterModuleOf` per type
  declaration/definition, inside `declareFunctionAs`/`defineFunctionInto` (so trait methods
  and specializations get it), `lowerEntry`, closures, and `planExtern`.
- The emitted type name keeps the declared spelling when the key equals it; qualified keys
  are mangled (`main__Box$i64`).
- **An instantiation carries the site it was requested from**: a specialization enters the
  *generic's* module, but a type argument may be private to the caller's.
  `lookupNamedType` falls back to the site's key after the current module's.
- **A resolved nominal type whose name is not program-wide carries its declaration key**
  (`NamedStructType.Key` et al., set by `SymbolTable.KeyAmbiguousTypes`): `lowerType` and
  `resolveForLayout` look it up by that key first, and `types.IdentityString` spells it
  module-qualified in instantiation keys and symbols (`Opt$two_Point`). A program-wide type
  carries none and keeps its bare spelling, so an unresolved reference to it names the same
  instantiation a resolved one does.
- **Bugs here only reproduce with a `module` header**; the suite prepends `module main`.

### `data` construction

See DATA_LAYOUT.md. A capitalized call the typechecker resolved to a constructor records a
`DataType` and goes to `lowerDataConstruction`. `lowerTupleLiteralExpr`,
`lowerDataConstruction` and `lowerStructInstanceExpr` all pass elements through
`coerceAggregateElem`. A `shared` value is built inline then boxed (`lowerBoxShared`).
Loud error: an inline-record data constructor; record-update syntax; a struct field
omitted in favour of a default.

### `newtype`

Emits nothing — a newtype is its base at run time. Transparency runs through two choke
points: **`lowerType`** (annotation types) and **`recordedType`** (every read of the
TypeTable — never call `TypeTable.Get` directly), both via `stripNewtype`, which resolves a
name only far enough to see a newtype and leaves other `UnresolvedType`s alone
(`lookupNamedType`/`namedStructFields`/`resolveDataType` depend on them). The same strip
runs at managed boundaries — `resolveNamedType`, `boxDropFn`, `dropFuncFor`/`retainFuncFor`
(one glue for newtype and base), `deepRetain`/`deepRelease`, and `lvalueAddress`
(normalizes `lvalueLoc.ty`). So a newtype over a managed base is managed.

### Tuples and structs

First-class values: `insertvalue` to build, `extractvalue` to read. Anonymous tuples build a
structural type on the fly (`lowerAnonymousTupleType`). Struct literals may list fields out
of order; values are keyed by name and built in declared order. Field position comes from
`namedStructFields`, which resolves `UnresolvedType` (how a field typed as another struct is
recorded).

## Arrays

### Fixed `[N]T` (`arrays.go`)

- `[N x T]` aggregate; literal elements coerced to the element type.
- **Constant index** → `extractvalue` (range-checked at compile time). **Runtime index** →
  widen to i64 by signedness, one **unsigned** `>= size` compare (negatives become huge) →
  trap, then GEP+load. Negative indices trap; `from_end(k)` (`lowerArrayFromEnd`) is
  `len - k` with the same one-compare trick.
- A local/param array is indexed through its slot (`arrayLValue` — must address a by-ref
  parameter in place); other values are materialized to a temp.
- `shared [N]T` — see ALLOCATION.md.
- `[v; n]` evaluates `v` once and retains a managed element once per slot beyond the first.
  Up to `repeatUnrollLimit` (64) it is an `insertvalue` chain; above it `fillFixedArray` fills a
  stack slot in a counted loop and loads it, and an all-zero `v` is one `zeroinitializer` store.
  The dynamic path (`lowerDynArrayRepeat`) has the same limit.

### Dynamic `[]T` (`dynarray.go`)

Box layout and drop glue in ALLOCATION.md. `lowerDynArrayConstruction` sizes to the
literal; `lowerDynArrayIndex` always bounds-checks (range analysis does not track dynamic
lengths). `len()` (`lowerArrayLen`) is the constant size or the box's `len` field.

`push` grows by doubling and stores unconditionally. `[...xs, v]` sizes once from operand
lengths; spreading a `shared` array is a loud error.

### Comprehensions (`array_comp.go`)

`[ x in xs | guard | result ]` allocates once and fills at a running count.

- **Capacity = product of source lengths**, allocated up front. Running the clauses twice
  would evaluate guards (which may call functions) twice; growing would realloc on the
  common mapping case.
- **The capacity must bound the loop by construction**: an array's bound is its length; a
  string's is its **byte** length (≥ rune count); a range's count is `ceil(span/step)`
  computed once, clamped at zero, divisor made safe before `sdiv`, and **the loop is
  driven by that count**, so a degenerate range is empty.
- Each source is a `compSource` that emits its own loop (arrays/ranges share
  `countedSource`; strings use a byte cursor).
- **`shrinkCompBuffer`** returns unused capacity when a guard or string source could
  undershoot, conditional on `count < capacity`. The new size is clamped to one element
  (`realloc(p, 0)` may free and return null; the buffer pointer is non-null by
  construction), a failed shrink keeps the old buffer via `select`, and `cap` is rewritten
  with the buffer.
- Loud error: a comprehension clause whose source depends on an earlier clause.

### `match` on `[]T` (`match_array.go`)

An if-else ladder: each arm is a length test (`== fixedCount`, `>=` with a rest), then
element tests in a block reached only after the length matched. Element bindings and a
bare `[...rest]` are borrows. **`[head, ...tail]`** binds a *fresh* box
(`bindTailSubArray`): sized at run time, suffix copied with each managed element retained,
bound in an **arm-scoped frame** (an unmatched arm's slot is uninitialized). A guard's false
edge gets its own block that releases that frame first.

Loud errors: rest not last, nested non-scalar element patterns, destructuring an array in a
`let`/parameter.

## Assignment through a place (`lvalue.go`)

**`lvalueAddress`** is the one recursive address computation: identifier → slot; `.field`
→ GEP (through the box for `shared`, `memberFieldAddress`); `[i]` → bounds-checked element
GEP (fixed through storage, `shared`/dynamic through the box); `p^` → the pointer value.
Hops nest in any order.

**Managed targets**: the new value (+1 from ownership) is computed first, then the old one
released, then stored — so `xs[i] = xs[i] ++ y` is safe. `releaseOldTarget` releases only
when the slot genuinely owns its value:

- the final hop crossed a ref-counted box (`lvalueLoc.viaBox`), or
- the root is an owning binding (`lvalueRootIsOwning`/`slotIsFramed`: local `let`/`var`,
  `own` param), or
- the root is a by-reference `mut` parameter (the slot is the caller's storage).

`p^ = v` releases too (every `&mut`-able root owns its value). Match-arm and `if let`
bindings are borrows and immutable. Optional member assignment (`p?.x = v`) is a loud error.

### `mut`/`ref` parameters are by reference (`paramIsByRef`)

`lowerParameter` emits a pointer; `defineFunction` binds the incoming pointer directly as
the slot (no copy); the call site passes `argumentAddress`. A `ref` argument may be a
temporary, which `argumentAddress` spills to an entry-block alloca. `own` stays by value. A
`mut`/`ref` on a **copied scalar** stays by value (`types.IsCopiedScalar`, shared with
`lyra-W010`). The ownership pass treats both as borrows — the callee releases nothing. The
typechecker requires a `mut` argument to be a mutable lvalue (`checkMutArgument`) and
exclusive within the call (`checkExclusiveMutableBorrow`).

## Pattern matching

`patternMatcher` returns the `aggPatternTest`/`aggPatternBind` pair for one pattern; every
destructuring form uses it, so a pattern means the same thing everywhere.

- **`data`** (`lowerDataMatch`): tag `switch`, one block per arm; payload reinterpreted and
  bound (`bindDataPatternPayload`, flat or single-tuple forms). Falls back to
  `lowerMatchLadder` when an arm tests a payload value (`dataMatchHasPayloadTest`) or has a
  guard (`matchHasGuard`) — a switch cannot route one tag to two arms.
- **Scalars** (`lowerScalarMatch`, a ladder over `lowerMatchLadder`): integers/bools
  (`icmp eq`, ranges with the scrutinee's signedness), floats (`fcmp oeq`, ordered range
  compares; needs a wildcard; literal arms warn `lyra-W008`), strings (equality or regex
  DFA), runes (pre-decoded `ast.RunePatternValue`).
- **Structs/tuples** (`lowerStructMatch`/`lowerTupleMatch`): ladder with recursive
  `aggPattern*`; nested struct/tuple/`data` sub-patterns. A nested `data` sub-pattern tests
  its tag and reinterprets via `extractDataPayload`; reading a wrong-variant payload is
  harmless because the ANDed tag test already failed. The collector rewrites `Pt { x, y }`
  to a named `StructPattern` (`reclassifyStructPatterns`).
- **Guards** (`lowerGuardedArmBody`): evaluated after binding; false goes to the next arm.
- **Fall-through traps** (`sealMatchFallthrough`, `lyra_panic_match_failed`) on every
  ladder and the `data` default — exhaustiveness is only a warning for most scrutinee
  kinds.
- **`shared` scrutinees** unbox first; Perceus reuse — ALLOCATION.md.
- **Destructuring statements** (`destructuring.go`): `let (a, b) = v` requires an
  irrefutable pattern; `if let` binds inside a branch scope (`pushLocalScope`);
  `let … else` binds into the continuation and its else must diverge (`lyra-E074`; the
  backend check stays). A destructuring `let`'s names **own** their values (retained and
  framed); arm and `if let` bindings borrow.
- **Destructuring parameters** are irrefutable (value-testing sub-patterns refused).
  Bound names borrow; an `own` parameter frames the *whole* aggregate so unnamed managed
  fields are freed. `mut` cannot be destructured; `ref` can.
- Loud errors: nested `shared data` sub-patterns, escaped string patterns, array patterns
  outside `match`.

## Floats

`FloatLiteralExpr` at `literalFloatType`; `fadd/fsub/fmul/fdiv`; `frem` for `%` and
`lowerFlooredFRem` for `%%`; `fneg`; `fcmp` with ordered predicates except `!=` (`une`, so
`NaN != x`). Conversions: `sitofp`/`uitofp` by source signedness, `fpext`, and narrowing
(rounds to nearest). Binary op lowering dispatches on the lowered operand's LLVM type.

`floor`/`ceil`/`round` (`rounding.go`, via `lowerBuiltinMethodCall`) call
`llvm.<op>.<width>` (cached in `l.floatMathFuncs`) then `fptosi` to i64, with a range
and NaN check first (`lyra_panic_float_to_int`) — `fptosi` out of range is poison.

The float-math builtins share that lowering and answer the **receiver's width** instead:
`log`, `log2`, `log10`, `sqrt`, `exp`, `exp2`, `sin`, `cos` as `llvm.<op>.<width>`, and
`pow` as `llvm.pow.<width>`. `tan`, `asin`, `acos`, `atan` and `atan2` are emitted as
**direct libm calls** — their LLVM intrinsics only arrived in 19/20 and the emitted IR must
parse under the clang-15 the ASan container pins — which is also why `lyrac build` links
`-lm`. libm has no half-precision entry points, so an `f16` receiver on that path is widened
to `f32` and rounded back (`emitLibmMathCall`). Which list a name is in is private to
`rounding.go`; raising the clang floor moves names between them and changes nothing else.

## Strings

Layout, the NUL invariant, literals, equality, `++` and `slice`: STRING_LAYOUT.md.

### Indexing

`lyra_str_rune_offset(data, byteLen, idx, allowEnd)` (`strRuneOffsetFunc`) is **the one
definition of where rune k begins**, returning a byte offset or -1 so each caller raises its
own trap. `allowEnd` admits `idx == runeCount` (slice bounds, `byte_offset`) and must not be
set for indexing.

- `s[i]` (`lowerStringIndex`) decodes the rune at that offset: O(i). Negative traps.
- `s.from_end(k)` walks **backwards over bytes**, skipping continuation bytes: O(k), no
  decoding.
- `slice` resolves each bound separately and compares the resolved *offsets*.
- `s.byte_offset(i) -> Maybe<i64>` (`lowerStringByteOffset`) exposes the walk, branchless;
  end position is `Some(byte_len)`.

### Byte-level primitives

`s.byte_len()` is the field. `s.compare_bytes_at(offset, other)` compares **exactly
`other`'s length** at a byte offset, so `== 0` is a prefix test. It is **branchless and
total**: offset clamped before the GEP, length clamped to what `s` has, and a shortfall or
out-of-range offset forces a negative result. A byte mismatch decides before a shortfall
(`"hello".compare_bytes_at(4, "lo")` is positive). Byte prefixes equal rune prefixes
because UTF-8 is prefix-free.

### Interpolation

`lowerInterpolatedString` formats each segment with `formatForPrint` and concatenates into
one fresh box — an owned result like `++`. The collector reconstructs literal chunks from
the **raw source between** interpolation nodes, not `string_content` text (authoritative
regardless of scanner whitespace handling).

### `decode_utf8` / `encode_utf8`

`++`'s shape: allocate, memcpy, fat pointer. `byteBufferOf` gets the bytes (`[]u8`: load the
buffer and len; `[N]u8`: `argumentAddress`, spilling a non-lvalue). The rune count is walked
with `lyra_utf8_count`. `encode_utf8` is `dynArrayAlloc` plus a memcpy into the buffer. No
ownership entry needed: the builtin's recorded signature makes `isOwnedReturn` answer owned.
(`calleeIsOwningBuiltin` is for builtins called as **free functions** with no signature —
it must name every one, e.g. `read_line`, `program_arg`.)

## Builtins with runtime shims

All shims are emitted into the module lazily. Libc functions the compiler calls are declared
through `declareLibc(name, ret, params…)`, which reports whether it created the declaration
(so attributes like `noreturn` on `exit` or variadic `snprintf` are set once). `declareLibc`
and extern declarations share one symbol space — see `../FFI.md`.

### `print` / `println` (`print.go`)

Intercepted in `lowerFunctionCallExpr` *after* user-function lookup (a user binding
shadows). `formatForPrint` renders to bytes, written with `write(1, …)`; `println` adds a
`"\n"` write. string → its own bytes; int → `snprintf` `%lld`/`%llu` into an entry-block
buffer; bool → `select` of interned literals; rune → `lyra_rune_to_utf8` (no surrogate
check). The ownership pass treats print as borrowing (`calleeIsBorrowingBuiltin`).

**Floats round-trip** (`floatToStrFunc`, `lyra_f{16,32,64}_to_str`): format at increasing
precision and `strtod` each candidate until it reads back equal, topping out at 17/9/5
significant digits. **Compare at the value's own width** (narrow an f32 candidate back to
`float`). Shortest within the ladder, not provably minimal (Ryu is the upgrade).

### `read_line` (`input.go`)

`lyra_read_line` returns `Maybe<string>`:

- **`getchar`, not `getline`/`fgets`** — those need `stdin`, whose symbol is
  platform-dependent (`__stdinp` vs `stdin`).
- Reads straight into a ref-counted box, `realloc`ing as it grows (reserving the NUL byte).
- **Returns the union itself** — no branches at the call site.
- Strips `\n` and a trailing `\r`; EOF with nothing read is `None`.
- Needs `calleeIsOwningBuiltin`: the unresolved-callee default treats a result as borrowed,
  which leaks (`TestExec_ReadLineUnderASan` under LSan).

### `random_seed` (`random.go`)

`getentropy` (macOS and glibc 2.25+; no `FILE*`). The slot is pre-filled with `time(NULL)`
**before** the call — POSIX leaves the buffer unspecified on failure. Not a security
primitive. The generator itself is prelude Lyra.

### `wall_clock_nanos` (`clock.go`)

`clock_gettime(CLOCK_REALTIME = 0, …)` into a `[2 x i64]` `timespec` (same on both targets),
**zeroed before the call**. `sec * 1e9 + nsec` uses plain `mul`/`add`.

### Terminal (`tui.go`)

One of the two files that consult `runtime.GOOS` (here for `TIOCGWINSZ`); sound only
because `lyrac` compiles for its host. It never indexes a platform struct — `struct
termios` is carried as an oversized opaque buffer — which is the difference from the other
one.

### Directories (`dirent.go`)

The second `runtime.GOOS` consumer, and the one that *must* know a field offset: `d_name`
sits at 21 on macOS and 19 on Linux, and `readdir` is `readdir$INODE64` on x86_64 macOS
(where the bare name links against the legacy 32-bit-inode entry point and reads the wrong
field). Both measured 09/17; an unknown host is refused rather than guessed. The shim
`lyra_read_dir` answers the canonical `Maybe<[]string>` in one call, copying each name into
a fresh box as `program_arg` copies argv — `readdir` reuses its buffer. `std.io`'s
`read_dir` is the Lyra half.

## Traits and generics

### Trait methods (`traits.go`)

A trait-impl method is an **ordinary function taking the receiver first**; dispatch is fully
static (no vtables). The symbol names type, trait and method.

- **Emitted lazily at first call**, where the receiver-substituted signature exists.
  `typetable.Resolution` carries impl, signature and `Bindings`; the backend never
  re-derives `Self` substitution.
- **Monomorphized per binding set**: `Resolution.SpecKey()` names the specialization for
  the symbol, the method cache and the ownership table (`driver.OwnershipByMethod`) —
  ownership differs by type argument on the *same* AST node. The body lowers under
  `pushTypeSubst`, so `lowerType`/`recordedType` make it concrete.
- **Bodies are queued** (`pendingTraitMethod` carries bindings and spec key), never lowered
  re-entrantly; declare-before-define lets recursion terminate.
- The synthesized `*ast.LambdaExpr` (trait signature types, impl clause names/body, each
  parameter's `Borrow` copied) goes through `defineFunctionInto`, the path plain functions
  use. `own` on a trait method parameter is `lyra-E030`.
- Refused loudly: a generic body calling a generic *impl method* at a type-variable
  instantiation ("type variable t has no concrete type here").

### Generic calling generic

The typechecker records `expect<t=t>` (a template). `specializations()` lowers only
`Instantiations.Concrete()`; `specializedFuncFor` composes the active `typeSubst` into the
callee's bindings. The driver (`driver/instantiations.go`) closes the set — seeding trait
bodies from `MethodTable.Specializations()` too — **before** the per-specialization
ownership pass, or a late specialization falls back to the generically-analyzed table and
emits no retains/releases. Polymorphic recursion is refused, bounded on type depth.
A `where`-bound call's candidates for a specialization only the closure discovers (reached
through a second generic) are published by the typechecker when the driver asks
(`candidatePublisher`: `PublishCandidatesForInstantiation` / `PublishCandidatesForMethod`);
the closure re-scans `Specializations()` to a fixpoint so those candidates' bodies are composed.

A **trait method** resolved inside a generic body is the same template (`impl Get for Box<t>`
reached from `g<u>` binds `t = u`): `traitMethod` composes it with `l.typeSubst`
(`Resolution.Composed`), and the driver's closure composes resolutions and operator resolutions
found in each concrete body with the same helper, so the SpecKey has an ownership table.

Local generics (a generic `let` inside a function): one closure per instantiation,
`local_generic.go`.

### Bound dispatch

A call under a `where` bound (and every `self` call in a trait default) is resolved
abstractly by the typechecker, which publishes one candidate per implementing type:

- **Look up with `candidateKey(expr)`**, not `recordedType(expr).String()` — the latter is
  module-qualified (`main__Box$i64`) and misses, only in programs with a `module` header.
- **Read borrow modes with `methodParams(call, res)`**, not `methodParamModes(call)` — a
  bound call has no resolution entry, so modes come back nil and a `mut` receiver is passed
  by value into a method expecting a pointer (a wild load).

- **A method generic in its own variables** (`mapv: (Self, (i64) -> b) -> b`): apply
  `res.WithMethodVars(MethodTable.BoundMethodVars(call), l.typeSubst, types.Substitute)` to the
  candidate before `traitMethod`. The solution is in the enclosing body's vocabulary (`b = u`);
  the driver closes the set with the same call, so the SpecKey has an ownership table.

`Trait::method(receiver, …)` (`lowerTraitPathCall`) is not a bound call; the receiver is
argument 0, so arguments and parameters are index-aligned. A receiver-less method
(`zero: () -> Self`) whose `Self` solved to a bound variable *is* one: no resolution is
recorded, and the candidate is keyed by `MethodTable.BoundSelf(call)` under the active
substitution, there being no receiver expression to read a type from.

## Raw pointers (`pointers.go`)

A raw pointer is an LLVM pointer; nothing owns or is refcounted.

- **`&x` uses `argumentAddress`, never `lowerExpr`** (which would address a copy).
- **`^T` lowers to a pointer to its pointee, not `i8*`** — llir type-checks stores against
  the element type. `^T` and `^mut T` lower identically.
- **`p.offset(n)`** is a GEP over the pointee type with the index widened to i64. No bounds
  check.
- **`nullptr`** takes its pointer type from the TypeTable, never `NullPtrExpr.GetType`
  (typed-pointer clang distinguishes `i8* null` from `i64* null`).
- **Pointer `==` is keyed on the Lyra type** (`isRawPointerExpr`), never the LLVM type —
  a `shared` aggregate is also an LLVM pointer and compares by value
  (`TestExec_SharedAggregateEquality`, `TestExec_NullPtr`).
- **An `unsafe` block lowers through `lowerBlockStmts`**, since it may have no value.

## Foreign functions (`extern.go`, `abi_lower.go`)

Details in `../FFI.md`. Essentials for this package:

- An extern is `ExternDeclStmt.Func()` declared via `declareFunctionAs` and called through the
  ordinary path. **The symbol is the C name** (`@symbol` or the name as written), not
  `userSymbol`.
- `l.externs` is keyed by the **C symbol**: several declarations collapse to one `declare`;
  disagreeing signatures are refused. It shares a symbol space with `declareLibc`, which
  cannot return an error and records conflicts in `l.symbolConflict` for `emitModule` to
  fail on.
- No ownership crosses; `mut`/`ref` is refused on extern parameters.
- **Variadic**: `Sig.Variadic` on the declaration is all LLVM needs; `lowerDirectCall`'s
  arity check becomes a floor; `promoteVariadicArg` emits the typechecker's recorded
  promotion (`TypeTable.VariadicPromotion`), taking signedness from the Lyra type.
- **Aggregates by value**: `pkg/abi` classifies, `abi_lower.go` coerces through memory.
  `planExtern` must `enterModuleOf` the extern first. `pushExternSignature` makes
  `lowerType` read function types as C function pointers for one declaration.
- `@link` reaches `lyrac` via `driver.Result.Links`.
- Tests: `llvm_extern_test.go` (libc/libm), `llvm_ffi_fixture_test.go` +
  `testdata/ffi_fixture.c`. Expected values come from `testdata/ffi_oracle.c`, never from
  Lyra's own output; the compile cache is salted with the fixture bytes
  (`compileCachedSalted`); they use `emitWithPrelude` because they import `std.ffi`.

## Lazy sequences (`seq_lower.go`, `seq_coro.go`)

- **Consumed in place, a `Seq<t>` has no representation**: a `gen` producer is lowered at
  its consumer (`for-in` or comprehension) with each `yield` running the consumer's body; a
  terminal is an inlined call. `mentionsSeq` skips declaring/defining such functions;
  `lowerFunctionCallExpr` inlines the call before consulting specialization/overload tables.
  A `Seq` reaching `lowerType` otherwise is `errSeqNotLowered`.
- Bodies run in a **`seqEnv`**, captured at the consumer and reinstalled at each yield.
  Anything a body's lowering depends on beyond the shared frame/temp stacks belongs there
  (`pendingBase` included).
- **`emitReturn` honours `inlineRet`**: a `return` in an inlined body stores and branches,
  releasing only frames above the inline base. Nothing else may emit `ret`.
- A consumer's `loopCtx` carries the consumer's depths, so its `break` releases what the
  producer holds.
- **Held as a value**, a sequence is a box around an LLVM coroutine handle, managed as a
  `shared` box with glue `lyra_seq_drop`; the promise is `{ i1 has, t value }` read through
  `llvm.coro.promise`. Coroutine bodies are emitted after all other functions
  (`defineSeqCoroutines`). `yieldValueTo`: a handler means inline, none inside a coroutine
  means suspend. Managed parameters are retained on entry and framed.
- **`CheckCoroutineSupport`** refuses clang older than 15; `lyrac` errors by name,
  `compileCached` skips. Any new consumer of emitted IR must ask it.
- Open gaps: `todo.md`, "Lazy sequences".

## Shared walks and helpers

**`emitOwnedValue(block, v, t, retainWalk|dropWalk)`** (`owned_walk.go`) is the single
retain/drop walk; the mode only changes the managed leaf. `emitRetainValue`/`emitDropValue`
are its names at call sites. A copy must retain exactly what its death releases, so these
must not diverge again. **`emitEqValue` stays separate**: equality descends *into* managed
values and returns a value.

| helper | purpose |
|---|---|
| `makeString(block, data, byteLen, runeCount)` | every string construction |
| `icmpPred(rel, signed)` | (relation, signedness) → integer predicate |
| `declareLibc(name, ret, params…)` | libc declarations; reports creation |
| `privateConst(name, init)` | private immutable globals |
| `lowerTypeList(ts)` / `fieldTypes(fields)` | Lyra types → LLVM field list |
| `diverged(v, block)` | did this operand diverge |
| `recordedType(expr)` | every TypeTable read (substitutes, strips newtypes) |

`layout.go` holds the type toolkit (`LLVMPrimitive`, `IsSignedInt`, `SharedBoxType`,
`DynArrayBoxType`, `TagType`, `DataUnionType`, `StringLLVMType`) and `SizeAndAlign`.
`pointerSize` is where LP64 is assumed.

## Behavioural tests

`buildAndRun*` helpers compile emitted IR with clang and run it.

**ASan only works because the harness adds `sanitize_address`** to every `define`
(`instrumentForASan`, `llvm_ownership_test.go`). Generated `.ll` has no function attributes,
so without it the pass instruments nothing: the runtime still intercepts bad `free()`s, but
no load or store is checked and use-after-free runs clean. It is a text rewrite in the
harness on purpose — ordinary builds must not carry it.

- **Run ASan binaries through `buildAndRunASan`/`buildAndRunASanWithPrelude`**, never a bare
  `exec.Command`: `asanOptions` enables LeakSanitizer where it exists (Linux, CI,
  `./asan.sh`) and disables it on macOS. `buildAndRunLSanWithPrelude` skips off Linux for
  tests whose point is "does not leak".
- **`./asan.sh` before pushing memory-model work**: Debian's clang-15 uses typed pointers
  and rejects IR type mismatches Apple clang's opaque pointers cannot represent.
- **Path-sensitive conservation check** (`conservation_check_test.go`): from each
  `lyra_rc_alloc`, follows the box through bitcasts/GEPs/phis/aggregates/slots and reports a
  reachable `ret` with it neither released nor escaped. Tuned for **no false positives**
  (anything unmodelled marks it escaped). `Backend.emitModule` hands it the real
  `*ir.Module`. Two guards: a hand-built leaky module it must flag, and a per-program
  assertion that at least one allocation was path-checked. (Match names with `Ident()`
  carefully — it includes the `@` sigil.)
- Programs are prefixed with `module main`, since several bug classes only appear with a
  module header.

**Speed.** macOS security-assesses each newly created executable on first exec (~200 ms,
serialized system-wide), so:

- **Every test calls `t.Parallel()`**, table cases as parallel subtests. ASan executions are
  capped by `asanRunSlots` (concurrent shadow-memory mapping fails sporadically).
- **Binaries are cached** (`exec_cache_test.go`): `compileCached` keys on
  SHA-256(clang version | GOOS/GOARCH | flags | IR) in `~/Library/Caches/lyra-llvm-tests`
  (`os.UserCacheDir()`). Warm run ~2 s; cold ~2 min. Entries unused for 30 days are pruned;
  deleting the directory forces a full recompile.
