# `pkg/backend/llvm` — the LLVM IR backend

Companion specs in this directory: `ALLOCATION.md` (stack vs shared), `DATA_LAYOUT.md`
(tagged unions), `STRING_LAYOUT.md`, `SIMD.md` (roadmap).

The standing rule for everything below: **a form that does not lower yet errors loudly
rather than emitting wrong code.**

## Status

**Early status**, built on `github.com/llir/llvm`
(v0.3.6 — pure Go, builds a structured `ir.Module`; note it emits *typed* pointers like `i8*`,
not opaque `ptr`). `llvm.New()` returns a `*Backend` whose `Emit` builds an `ir.Module` and
defines `@main` — always `i32` at the LLVM/ABI level (the actual C runtime-expected signature,
verified against real clang output) regardless of Lyra's own `u8`/`void` entry-point convention,
with the `u8` body value coerced (`coerceIntWidth`: identity/`trunc`/`sext`/`zext`) and
zero-extended into that `i32` slot. `lowerExpr` (called via `lowerEntry`) covers integer +
float + character literals (a `rune` is an i32 code point), arithmetic (`+ - * / % %% -(unary)` on
ints and floats, incl. Odin-style floored `%%` vs truncated `%`; integer arithmetic is **fully
checked** — Pit-of-Success #2, `trap.go`: `+`/`-`/`*` lower to
`llvm.{s,u}{add,sub,mul}.with.overflow`, `/`/`%`/`%%` guard the divisor against zero and
(signed) against the `INT_MIN / -1` overflow, and unary `-` on a non-literal guards against
`-INT_MIN`; each bad case branches to a noreturn trap — `lyra_panic_overflow` or
`lyra_panic_divide_by_zero` — that writes to stderr and `exit(101)`s via the shared `emitTrapIf`
helper; a runtime array index out of bounds traps the same way via
`lyra_panic_index_out_of_bounds`. Any of these checks is **elided** (plain instruction / bare
load, no trap) when the value-range analysis proved it can't fire (`res.RangeSafety`): the
`+`/`-`/`*` overflow check via `NoOverflow(e)` (→ `emitWrappingOp`), the divide-by-zero /
signed-div-overflow guards via `NoDivZero(e)`/`NoDivOverflow(e)` (in `emitCheckedDivOp`), and
the array bounds trap + negative-index adjustment via `IndexInBounds(e)` (in `lowerIndexExpr`)),
numeric conversions (`i8(x)`, `u32(x)`, … — int↔int, int→float, float widening), comparisons +
`&&`/`||` (int and float), `if`/blocks (incl. one-armed `if`, and a two-armed `if` with void
branches, as statements — `lowerBranchValue` lowers each branch value-optionally, so a diverging
or void branch contributes no phi incoming), **`let`/`var` bindings, identifier reads, `var`
reassignment** (and **top-level `const`** — `Emit` records every top-level `const` declaration
in `l.consts`, and an identifier read that misses `l.locals` inlines the const's value
expression, since a `const` is a compile-time constant with no storage of its own; the value
node carries the typechecker-recorded width, and a const may reference another const; a *local*
const is an ordinary `lowerVarDecl` alloca like a `let`. Top-level consts are
**order-independent** — the typechecker checks them before function bodies (`Check`'s
`isTopLevelConst` pre-pass), so a function may reference a const declared later; a const
referencing *another* const must still precede it (the use-before-declaration check keeps the
const→const order acyclic)), **`for` loops with `break`/`continue`** (`lowerForLoop`: the
cond/body/post/exit CFG; a `loops []loopCtx` stack on the lowerer resolves break/continue
targets, labeled ones by walking the stack), **compound assignment** `MathAssignOpExpr` (`i +=
1` → load/op/store via `lowerMathAssignOp`, reusing the extracted `applyIntMathOp`), and
**user-defined functions** with calls, `return`, and recursion; anything else errors loudly
rather than emitting wrong code.

## Statements, control flow and functions

### Block-termination discipline

**Block-termination discipline:** break/continue are the first constructs that seal a block
mid-stream, so `lowerBlock` is split into a value-optional `lowerBlockStmts` (stops iterating
once `block.Term != nil`) + a value-requiring wrapper + `lowerForEffect` (loop/one-armed-`if`
bodies need no value), and every fall-through `br` at a CFG join is guarded by `end.Term ==
nil`. All three loop forms lower — infinite (`for {}`), condition-only (`for cond {}`), and the
three-clause `for var i = 0; i < n; i += 1`. A `let`/`var` declared inside a loop body **is**
visible there (07/29): both loop nodes hold their body as a `*BlockExpr`, so the block identity
the collector keyed the body's scope on survives into the AST. It used to be a value copy, which
made `enterScope` miss — silently, since a miss just runs in the enclosing scope — and every
body-local read as "undefined identifier". A loop body is also checked *for effect*
(`checkBlockForEffect`), because it has no value: putting its last statement in value position
rejected a one-armed `if` there.

### `panic` and the `never` type

`panic(msg)` is the one trap a program reaches deliberately — the other four
(`lyra_panic_overflow`, `..._divide_by_zero`, `..._index_out_of_bounds`,
`..._match_failed`) are emitted on conditions the compiler checks. It shares their exit
code and their stderr convention, because a panic the programmer wrote and one the
compiler inserted are the same event to whatever is watching the process.

Its runtime differs from the other four in one way: `void @lyra_panic(i8*, i64)` takes the
message as a **(pointer, length) pair** rather than baking a constant in, since a user
message is a runtime `string` (an interpolated one is the case worth having) and that is
exactly the fat pointer of STRING_LAYOUT.md. It writes the `"lyra: panic: "` prefix, the
caller's bytes, and a newline as three separate `write` calls — assembling one buffer would
mean allocating, and a trap has to work when the reason for trapping is that something is
already wrong.

`lowerPanicCall` emits `call` + `unreachable` and returns **no value with the block
sealed**. That is what made `never` (`types.NeverType`, the bottom type — assignable to
everything, so a diverging expression satisfies any context) need almost nothing here: the
block-termination discipline above already guards every fall-through `br` and every phi
incoming on `Term == nil`, for `return`/`break`/`continue`, and a panicking match arm or
`if` branch is that same shape.

What it *did* need is a guard at each site that **consumes** a lowered operand, since those
dereference the value they get back. `diverged(v, block)` (trap.go) is that test — nil value
*and* sealed block, because several lowerings legitimately produce no value while still
reaching the next statement. It is checked in `lowerVarDecl`, `lowerVarReassignment`,
`lowerDirectCall`'s argument loop, `lowerNumericConversion`, and the array-literal element
loop; each was a nil dereference (a Go crash out of `lyrac`, not a loud error) before.
Positions with no guard fail loudly instead: a tuple literal with a panicking element
reports `unknown type: never`, because the *type* recorded for the tuple contains `never`
and no `diverged` check can rescue that — the tuple is uninhabited and the honest fix is a
typechecker diagnostic, not a lowering.

### Locals are lexically scoped

**Locals are lexically scoped** (07/29, `pushLocalScope`): `l.locals` was a single flat
name→slot map for a whole function, so a binding that **shadowed** an outer one clobbered it
permanently (`let n = 100; { let n = 5 }; n` read 5) and every construct that binds a name for
the duration of a sub-tree leaked it into whatever followed. `pushLocalScope` snapshots the
visible bindings and returns the restore, used at four kinds of site: a block
(`lowerBlockStmts`, alongside the managed frame it already pushes), each loop (the loop variable
and a C-style init's counter belong to the loop), and each **match arm** — which needs a *reset
per arm*, not just a restore after the match: an arm that reads an outer binding an earlier
arm's pattern shadowed would otherwise read the earlier arm's slot, never stored to on this
path. The restore installs a fresh copy each call, so repeated resets can't leak writes back
into the snapshot. Name scoping is independent of the ownership bookkeeping, which tracks
*slots* — two same-named managed values in nested scopes are two allocations, each released
once.

### Functions

**Functions** lower in two passes (`Emit`): every user function is `declareFunction`'d before
any body, so a call (from main, between functions, or recursive) resolves against `l.funcs`;
`defineFunction` then lowers each body. `main` stays special (`lowerEntry`, the `i32` ABI). Each
body resets per-function state via `beginFunction` (fresh `locals`/`loops`, plus
`retType`/`retSigned`/`entryABI`); params bind as entry-block allocas keyed by name.
`emitReturn` is the one return path — coerces to the declared width, with main's `entryABI`
doing the u8→i32 slot — shared by explicit `return` (`ReturnStmt`, which seals its block like
break/continue) and the implicit tail return. Call arguments pass un-coerced (the typechecker
propagated each param's width onto its literal args, so `add(200)` already lowers `200` at the
param width).

### Void functions lower

**Void functions lower** (`lowerType` maps `VoidType` → LLVM `void`; `emitReturn` emits `ret
void`, discarding any body value; `defineFunction`/`lowerEntry` lower a void body for *effect*
via `lowerForEffect`, so an empty or non-expression-terminated block is fine, and route the void
entry through `emitReturn(nil)` so owned temporaries flush). Deferred with a loud error: destructuring params.
**Default params no longer reach here** — the typechecker fills every omitted argument from
the declaration (`typechecker/default_args.go`), so a defaulted parameter is an ordinary one
by the time it is lowered, and `lowerDirectCall` guards on the argument count so a call that
somehow was not filled fails loudly instead of emitting a short argument list. **Multi-clause functions no longer reach here**: the typechecker
desugars them into a match on the parameters (`typechecker/multi_clause.go`), so by the time
the backend sees one it is an ordinary lambda whose body is a match.

## Closures

### Closures lower

**Closures lower** (07/29, `closures.go`) — a function value is a boxed closure `{ i8* fn, i8*
env }`, the dev tier of the two decided in `todo.md` (release-tier Lambda Set Specialization
comes with the generics monomorphizer). One representation for every function type, so a `(i64)
-> i64` parameter accepts a named function, a captureless lambda, and a capturing closure alike,
with no per-call-site specialization — which is also what a stable hot-reload ABI needs. `fn` is
the lifted body, always `ret (i8* env, params...)`; `env` points at the *payload* of a
ref-counted box `{ i64 rc, { i8* dropFn, captures... } }`, exactly `rcHeaderSize` past its
header, the same relationship a string's data pointer has to its box — so `managedBox` recovers
it by subtracting the header and retain/release/drop work on a closure with no new machinery
(`IsManaged` covers a `LambdaType`). Every *nested* lambda is lifted to a top-level
`@lyra_closure_N` (`collectNestedLambdas` + `declareClosure`/`defineClosure`), all declared
before any body and their bodies lowered **last** — never re-entrantly at the creation site,
which would mean saving and restoring the whole per-function lowering state mid-expression. A
**named** function used as a value gets a thunk (`@name.closure`) that ignores the environment
and forwards, so a direct call by name keeps its plain signature (every existing call site and
its IR are untouched, and the thunk exists only for functions actually used as values). A
**captureless** closure shares one *pinned* static environment (`.closure.empty_env`, null
dropFn), so a plain function value costs no allocation while still being an ordinary managed
value everywhere else. Captures are **by value**, copied into the environment at creation with
each managed one retained there (`buildEnv` — the ownership pass never sees capture reads, since
it analyzes a nested lambda as a separate function, which is exactly why the +1 is minted in the
backend); the body binds them as **borrows** (unframed, like a match-arm binding), since the
environment owns them. The environment's captures are freed through **one generic trampoline**
(`lyra_closure_env_drop`) that reads the per-capture-set glue out of the environment's first
slot: a release site knows only the static type `(i64) -> i64`, never which lambda produced the
value, so the glue has to travel *with* the value. An indirect call (`lowerIndirectCall`)
unpacks the pair, bitcasts `fn` to the signature the callee's static `LambdaType` gives, and
calls it with the environment first; it is reached from a local binding holding a function, a
function-typed parameter, a struct field that holds one (told apart from a builtin method by
looking the field up on the receiver's struct — a builtin's *recorded* type is a LambdaType
too), and any other callee expression (`fs[1](5)`).

### Deferred, loud errors

**Deferred, loud errors:** a `mut`/`ref` parameter on a lambda used as a value (a function type
carries no borrow mode, so the call site would pass by value while the body expected a pointer —
a disagreement that is a miscompile, not an error) and a lambda with no
return annotation used as a value. (Multi-clause lambdas are desugared away in the front end
before reaching this path.) Locals are modeled as entry-block `alloca` + store/load
(mem2reg builds SSA — no hand-written phi nodes for variables), tracked in `lowerer.locals`
(name → its alloca, a pointer). `lowerVarDecl` allocas in the function's entry block and stores
the initializer; a reassignment stores into the *existing* alloca and leaves the `locals` entry
pointing at that alloca (it must stay the pointer, not the stored value, or the next
`IdentifierExpr` load's `slot.(*ir.InstAlloca)` assertion fails). Alloca type is taken from the
initializer's lowered `.Type()` (so a `let`/`var` doesn't need `lowerType` on its annotation).

## Literals and type declarations

### Integer literals lower at the width the typechecker recorded

**Integer literals lower at the width the typechecker recorded** (`literalIntType` reads
`res.TypeTable`, fallback i64 for an untyped/absent entry; a literal the typechecker adapted to
a **float** context lowers as a float constant of the recorded width instead, via
`literalRecordedFloatType`) — context-directed literal-width inference
(`typechecker.propagateLiteralType`) resolves narrow widths, so `i8(x) < 3` lowers `3` as i8 and
u8 arithmetic happens at u8 (where `+`/`-`/`*` overflow **traps** — see the checked-arithmetic
note above). The comparison width-mismatch guard is now defensive-only: it fires when a literal
too large for its context width was left untyped and fell back to i64 (`i8(x) < 300`), which is
a loud error rather than miscompiled code.

### Type declarations lower before any function

**Type declarations lower before any function** (`Emit` calls `lowerTypeDeclarations` then
`lowerTypeDefinitions`): each top-level `tuple`/`struct` decl becomes a named LLVM struct type
(`tuple Point(i32, i32)` → `%Point = type { i32, i32 }`, `struct Node {…}` → its field types in
order, e.g. bool → i1, f64 → double). Like functions, types lower in **two passes** —
`declareNamedStruct` first registers an *empty* named `NewStruct()` placeholder for every decl
(recorded in `lowerer.structTypes`, keyed by the bare declared name, **not**
`TupleType.GetName()`, which renders the full `Point(i32, i32)` shape), then
`lowerTupleDef`/`lowerStructDef` fill in each one's fields. Declaring all names first makes
declaration order irrelevant and lets a field reference another named type (`struct Line { a:
Point }` → `%Line = type { %Point, %Point }`), including a forward reference. A
`stack`/`Unspecified` field lowers by value; a **`shared` field lowers to a pointer** to its
ref-counted box (`lowerType`, ALLOCATION.md), which is also what makes a recursive `shared`
field pointer-sized and finite. A **`weak` field lowers to an opaque `i8*`** (`WeakType`, the
box pointer), so it likewise makes a recursive type finite (`struct Node { parent: weak Node }`
→ `%Node = { i64, i8* }`). Unlike `shared` it takes the **weak** half of the refcount protocol —
see the `weak` notes above and `weak.go`. A **`data` (sum) decl** lowers the same two-pass way
(`lowerDataDef`) but to its tagged-union layout `%T = { iTAG, [K x iA] }` (DATA_LAYOUT.md, via
`DataUnionType`): a tag sized to the variant count, then a payload blob sized/aligned to the
largest variant — an all-nullary enum is just `{ i8 }`; a recursive type is finite because its
recursive field must be `shared` (a pointer, per lyra-E014). A by-value reference to another
named type in a payload (`data W = Wrap(P)`) is sized by first resolving the reference through
the symbol table (`resolveForLayout`, which deep-rewrites `UnresolvedType` leaves and
short-circuits a `shared` ref — that short-circuit is also what keeps the resolution finite,
since a recursive cycle must pass through a `shared` field).

### data value construction lowers

**`data` value construction lowers** (`lowerDataConstruction`, per DATA_LAYOUT.md): unlike a
tuple/struct it goes through memory, because the payload blob is *reinterpreted* as the active
variant's payload struct — alloca the union, `store` the variant's tag (field 0), and for a
payload variant `getelementptr` the blob (field 1), `bitcast` it to the payload-struct pointer,
and `store` the built payload; the union is then loaded back as a first-class value. The
collector wraps a positional constructor's fields in a single anonymous tuple (`Rect(i64, i64)`
→ one `TupleType` param), so both the typechecker and backend read the flat field types via
`types.DataTypeConstructor.FieldTypes()`. A nullary constructor (`Red`, `Nil` — a
`DataConstructorExpr`) just stores the tag; a positional one (`Cons(1, x)` — a
`TupleLiteralExpr` whose recorded type is a `DataType`) stores the payload too. Both
`lowerTupleLiteralExpr` and `lowerDataConstruction` run each lowered element through
`coerceAggregateElem` before the `insertvalue` — normally the identity (the typechecker already
narrowed the leaves), but a residual int-width mismatch is coerced (trunc/ext) rather than
letting llir *panic* inside `NewInsertValue`, and an irreconcilable mismatch is a loud error; a
well-typed program must never panic the backend. A **`shared`-flavored data value** (or a
`shared` payload field, e.g. a recursive `Cons(1, Nil)`) is boxed: the union is built inline,
then `lowerBoxShared` heap-allocates a `{ i64 rc, union }` box (`lyra_rc_alloc`) and the value
is the box pointer (ALLOCATION.md).

### Deferred, loud error

**Deferred, loud error:** an inline-record data constructor (`Node { … }`, routed through
`lowerStructInstanceExpr`), and a still-unsizeable payload (`string`, un-monomorphized generic).

### newtype declarations lower

**`newtype` declarations lower** (07/29) — by emitting *nothing*. A newtype is nominal to the
typechecker and *is* its base at run time, so `lowerTypeDecl`/`lowerTypeDef` register no LLVM
type for one (deliberately not an alias: a distinct named type per newtype would make every
arithmetic, comparison, and coercion site reconcile two llir types for one machine value, for no
gain). Transparency instead runs through two choke points: **`lowerType`** strips the wrapper
for a type read off an *annotation* (a parameter, return, field, or element written as
`Percent`), and **`recordedType`** — the accessor every lowering decision now reads instead of
`TypeTable.Get` directly — strips it for a type read off the TypeTable. Both go through
`stripNewtype`, which also resolves a type written as a *name* far enough to answer "is this a
newtype?" (a field declared `Email` is recorded as an `UnresolvedType`, so the lookup is the
only way to reach the newtype at all) and leaves every other name untouched, since
`UnresolvedType` is load-bearing for `lookupNamedType`/`namedStructFields`/`resolveDataType`.
The same strip runs at the managed-value boundaries — `resolveNamedType`, `boxDropFn`,
`dropFuncFor`/`retainFuncFor` (so a newtype and its base share one glue rather than generating
two identical copies), `deepRetain`/`deepRelease`, and `lvalueAddress` (which normalizes
`lvalueLoc.ty` in one place: a managed newtype target's overwritten value was never released,
and fixing only the managed test would have released a string fat pointer as if it were a box
pointer). So a newtype over a *managed* base (`newtype Email = string`) is managed — retained on
copy, released on death, moved into an `own` parameter (`lyra-E019`) — exactly as the base is.
`lowerType` (Lyra type → llir type) now covers scalars (`PrimitiveType` → llir)

### and named tuple/struct references

**and named tuple/struct references** — a `TupleType`/`NamedStructType`/`UnresolvedType`
resolves to its registered struct via `lookupNamedType`; note a field naming another declared
type arrives as an `UnresolvedType` (the typechecker doesn't rewrite it), which is why that case
is handled.

## Tuples and structs

### Tuple instances lower

**Tuple instances lower** (`lowerTupleLiteralExpr`/`lowerTupleIndexExpr`): construction
(`Point(3, 4)`, `(1, 2)`) builds a first-class struct value via `insertvalue` (undef + one
insert per element, in declaration order), and positional access (`pair.0`) reads an element
back with `extractvalue` — so a `let`-bound tuple round-trips through the same alloca/store/load
path as a scalar (mem2reg promotes it). Named tuples use their registered struct type; an
anonymous tuple `(1, 2)` builds a structural `NewStruct` on the fly (`lowerAnonymousTupleType`,
since it has no declaration to register). A capitalized call the typechecker resolved to a data
constructor records a `DataType`, not a `TupleType`, and is routed to `lowerDataConstruction`
(the tagged-union path above) rather than building a plain aggregate.

### Struct instances lower

**Struct instances lower** the same way (`lowerStructInstanceExpr`/`lowerMemberExpr`): `Node {
value: 3 }` builds its declared struct via `insertvalue` and `n.value` reads a field via
`extractvalue`. Two struct-specific wrinkles: a struct literal names its fields and may list
them out of declaration order, so values are keyed by name and built in the *declared* order
(`structType.Fields`, also the `insertvalue` index); and field *access* needs the field's
position by name, looked up from the object's declared struct type via `namedStructFields`
(which resolves an `UnresolvedType` — how a field/binding typed as another named struct is
recorded — through `res.SymbolTable.Types`, so nested `line.start.x` works). Deferred with loud
errors: record-update syntax (`P { base | f: v }`), a missing field relying on a default, and an
inline-record data constructor (records the owning `DataType`).

## Arrays

### Fixed-size arrays lower

**Fixed-size arrays lower** (`arrays.go`): a literal `[1, 2, 3]` builds an `[N x T]` aggregate
(`lowerArrayLiteralExpr` — undef + `insertvalue` per element, coerced to the element type;
`lowerType` gained the `StaticArrayType` → `[N x <elem>]` case), and `xs[i]` reads an element
(`lowerIndexExpr`): a compile-time-constant index is a bare `extractvalue` (the typechecker
already range-checked it), while a **runtime index is bounds-checked** — the index widens to i64
by its signedness, a **negative index counts from the end** (Python-style: `i < 0` → `i + size`,
so `-1` is the last element and `-size` the first, via a `select`), then an unsigned `>= size`
compare on the adjusted value (which catches both `i >= size` and `i < -size`, since an
out-of-range negative stays negative → large unsigned) branches to a new
`lyra_panic_index_out_of_bounds` trap (Pit-of-Success, like checked arithmetic) before a
`getelementptr`+`load`. A constant index (positive or negative) is range-checked at compile time
against `[-size, size)` (`resolveConstantInt` folds a `NegationExpr`). A local/param array is
indexed through its own alloca (no copy, `arrayLValue`); any other array *value* is materialized
into a temp first. Arrays flow through `let`/params/args and **returns** (`emitReturn` gained an
`ArrayType` by-value case) as first-class aggregates. The typechecker narrows an
annotated/return element width onto the literal's leaves and re-records it as a concrete `[N x
elem]` (a new `propagateLiteralType` `ArrayLiteralExpr` case — static context only, so a `[]T`
dynamic annotation keeps its `DynamicArrayType`), so `() -> [3]u8 => [4, 5, 6]` builds `[3 x
i8]` matching the signature rather than `[3 x i64]`. A **`shared [N]T`** array lowers too
(`shared`-array support): it's a `ptr` to `{ i64 rc, [N x T] }` (the ordinary `shared`-box
`lowerType` path), so `lowerArrayLiteralExpr` builds the inline `[N x T]` and boxes it via
`lowerBoxShared` when the recorded type is `shared` (the typechecker's `propagateAllocation`
stamps the array-literal node, same as struct/data construction); `lowerIndexExpr` geps through
the box's payload (`sharedArrayPayloadPtr` — box → field 1 → element) for both a constant and a
bounds-checked runtime index, borrowing the box (no reference consumed). The per-type drop glue
gained an array case (`emitDropArray`, `needsDrop`/`emitDropValue` recurse into the element
type; N is constant so element drops are unrolled), and the ownership pass treats array-literal
elements as owning positions (`ArrayLiteralExpr` case, mirroring tuples/structs) so a managed
element transfers into the box rather than being double-freed — ASan-verified with
alloc==release conservation (`llvm_shared_array_test.go`).

### Dynamic arrays

**Dynamic arrays** (`[]T`, `dynarray.go`) lower too, with a distinct shape from a `shared`
value: a `[]T` is a `ptr` to `{ i64 rc, i64 len, [0 x T] }` (`DynArrayBoxType` — rc, element
count, then a flexible `[0 x T]` tail). Modelling it as a single box pointer (not a `{data,len}`
fat pointer) means it reuses the shared-value managed machinery unchanged — `IsManaged` covers
`[]T`, `managedBox` bitcasts the pointer, retain/release/drop act on it like a `shared` value,
and `lowerType` maps `[]T` → the box pointer *before* the `shared`-strip (a dynamic array is
inherently heap-boxed regardless of flavor). `lowerDynArrayConstruction` allocs a box sized to
the literal (`rcHeaderSize + 8 + N*stride`), stores len + elements (empty `[]` still allocs a
len-0 box, so the representation is uniform — no null case); `lowerDynArrayIndex` loads the
runtime len, bounds-checks the negative-from-end index against it, and GEP+loads (always checked
— the value-range pass doesn't track dynamic lengths). By-value flow through
`let`/params/returns is the ordinary pointer path; a `[]T` return-body/arg literal is recorded
as `DynamicArrayType` by `propagateLiteralType` (it re-records the dynamic case, not only the
static one, since only an annotated `let` records the type via `checkVarDecl`).

### Managed element types

**Managed element types** (`[]string`, `[][]i64`) are freed by a per-element-type drop glue
(`dynArrayDropFn`, routed via `boxDropFn`'s `DynamicArrayType` case) — the dynamic-length
counterpart to a `shared [N]T`'s unrolled `emitDropArray`: it takes the box payload `{ i64 len,
[0 x T] }`, loads `len`, and loops releasing each element (`emitDropValue`); elements transfer
into the box at construction (ownership's `ArrayLiteralExpr` case), so the box owns and frees
them exactly once. ASan-verified incl. an aliased-copy retain (rc 1→2→1→0) and
`[]string`/`[][]i64` element drops (the looped drop makes a static release-*count* meaningless —
one site runs `len` times — so the managed-element leak check is structural + ASan).

### Iteration

**Iteration** (`for x in <array>`) lowers via `lowerForInLoop` (`control_flow.go`) — an
index-counter loop (`i = 0; while i < len { x = arr[i]; body; i++ }`) over a fixed-size (`[N]T`,
stack or `shared` — length is the compile-time size) or dynamic (`[]T` — length is the box's
runtime `len`) array, with break/continue; the loop variable **borrows** each element (bound
into `l.locals` but not framed for release — the array still owns it, and for a managed element
frees it when the array dies). The typechecker types the loop variable from the iterable's
element type (`bindForInLoopVars`/`iterableElementType` in `typechecker_control_flow.go` — an
array's element, a string's `rune`, a range's numeric type kept *untyped* when its bounds are
untyped, so `for i in 0..<3 { t: u8 = i }` binds `i` to `t`'s width) — without which a body use
of the loop variable had no recorded type and couldn't lower.

### match on a dynamic []T

**`match` on a dynamic `[]T`** lowers via `lowerArrayMatch`/`lowerArrayPatternMatch`
(`match_array.go`) — an if-else ladder (the array analogue of `lowerScalarMatch`): each `[...]`
arm is a **length test** (`len == fixedCount`, or any length for a lone `[...rest]`) followed by
per-element literal/range tests (reusing `scalarMatchTest`), evaluated in a block reached only
*after* the length matched (so element reads are always in bounds), first-match-wins into a
merge phi. Element bindings (`[a, b]`) and a whole-array `[...rest]` are **borrows** (bound into
`l.locals`, not framed — the scrutinee still owns the storage).

### Deferred, loud errors

**Deferred, loud errors:** a `[head, ...tail]` pattern binding a *tail sub-array* (needs an
allocation + copy), a rest not last, a nested non-scalar element pattern, and a
fixed-size-`[N]T` scrutinee; an `[]` *empty* array pattern lowers (the base case of a list match
— `commaSep` in the `array_pattern` grammar rule + an `[$.array_literal, $.array_pattern]` GLR
conflict, the collector no longer rejecting a zero-element pattern; the backend's `fixedCount=0`
length test already matched `len == 0`).

### .len()

**`.len()`** is a compiler-provided array method (`lowerArrayLen`, `dynarray.go`; registered in
`builtinMethodSignature` for any array receiver → i64): a fixed-size array's is its compile-time
size (constant), a dynamic array's is the runtime `len` field of its box (reading it borrows the
array — no ownership action). The **two-variable form `for i, x in xs`** binds the loop counter
as the index `i` (i64) alongside the element `x` (`lowerForInLoop` — the collector puts the
first name in `Key` = the index, the second in `Value` = the element; the single-variable form
leaves `Value` empty and `Key` is the element). A **numeric range** iterable `for i in
START..<END` (also `..=` inclusive, and an optional `:step`) lowers to a counter loop
(`lowerForInRange`: `i = START; while i </<= END { body; i += step }`) — the counter *is* the
loop variable, its width the first concrete-integer bound's type (else i64, matching
`iterableElementType`), with a plain (wrapping) increment (so an inclusive `..=` to the counter
type's max loops forever — the one edge). A **string** iterable `for c in s` walks the string's
**runes** — UTF-8 decoded (`lowerForInString` + the `lyra_utf8_decode` runtime shim, the inverse
of `lyra_rune_to_utf8`): `bi = 0; while bi < byteLen { c = decode(data, bi); body; bi += n }`,
advancing the byte index by each rune's decoded byte count (so a multibyte character counts
once); the rune loop variable is a plain i32, not a borrow. So all three iterable kinds (array,
range, string) lower.

### Deferred, loud error

**Deferred, loud error:** the two-variable form over a string (`for i, c in s` — the index/rune
pairing isn't defined).

## Assignment through an lvalue

### Array element assignment

**Array element assignment** `xs[i] = v` lowers (`lowerLValueAssignment`, `lvalue.go`): an
`LValueAssignmentStmt` with an `IndexExpr` target computes the element's address — a fixed-size
array through its alloca (or a `shared` array's box payload), a dynamic array through its box
payload — bounds-checks the index (negative-from-end, same trap as a read via the shared
`boundsCheckedIndex`), and stores in place; the typechecker (`checkLValueAssignment`) already
verified the root binding is mutable (`var`/`let mut`/`mut`/`own`).

### String indexing

**String indexing** `s[i]` → the i-th **rune** lowers (`lowerStringIndex`, `strings.go`): a
string is UTF-8, so runes aren't randomly addressable — it walks from the front decoding one
rune per step (via `lyra_utf8_decode`) until it has skipped `i`, then yields that rune. O(i)
(prefer `for c in s` for a full traversal); running off the end before reaching `i` — which
includes any negative index, since there is no from-the-end form for a string — traps
out-of-bounds. Also deferred: growth (no grow op exists yet), and `match` on a `shared` array.

### Struct-field assignment

**Struct-field assignment** `p.x = v` (nested `p.a.b = v`) and **arbitrary mixed paths** —
`grid[i].y = v`, `p.arr[i] = v`, `m[i][j] = v`, `line.start.x = v` — all lower through one
recursive **`lvalueAddress`** (`lvalue.go`): an identifier root → its alloca, a `.field` hop →
gep into the object's stack-struct storage, an `[i]` hop → gep to the array element
(bounds-checked, negative-from-end); a fixed-size array is addressed through its storage, a
`shared`/dynamic array through its box (loaded from the object's slot). Because it recurses on
the object, index and member hops nest in any order. A **managed target** (a
`string`/`shared`/`[]T` array element or struct field) is handled: `lowerLValueAssignment`
releases whatever the slot held before storing the new (+1) value — the ownership pass's
`LValueAssignmentStmt` case gives the RHS its +1, and the new value is computed before the
release so `xs[i] = xs[i] ++ y` is safe (mirrors managed `var` reassignment).

### The release-old is emitted when the slot genuinely owns what it is overwriting

**The release-old is emitted when the slot genuinely owns what it is overwriting**
(`releaseOldTarget`), which holds two ways: the slot is reached through a **ref-counted box**
(`lvalueLoc.viaBox` — each hop records whether it crossed a box and only the *final* hop counts,
since crossing into a box makes everything below it unaliased again), or the path is rooted at
an **owning binding** (one the backend framed — a local `let`/`var` or an `own` param, via
`lvalueRootIsOwning`/`slotIsFramed`). Both are leak-free. A **by-reference `mut` parameter**
root is the third yes (07/29): its slot *is* the caller's storage, so the overwritten value is a
genuine reference to drop rather than a duplicate to dangle — which closes the leak that the old
refusal accepted (while `mut` was passed by value, the parameter copy shared the caller's
reference, so releasing through it would have freed a value the caller still owned). Restricting
the release fixed an ASan-confirmed **use-after-free** (07/28: `let q = p; p.name = …; q.name`,
a `[2]string` element, and the `mut Person` param where the by-value copy is the invisible
alias); deep-retain-on-copy (07/29) then made copies carry their own +1, which is what
re-enabled the owning-root case. Tests: `llvm_managed_assign_test.go`. A **`shared` struct** in
the path is addressed through its box (`memberFieldAddress`'s shared branch: box → payload →
field, reusing `lvalueBoxPtr`), so `p.x = v` / `p.name = v` on a `shared` struct — and stack
fields nested inside one (`ln.start.x`) — work, a managed field of a shared struct being fully
leak-free (the box's drop glue frees the final field). So interior assignment is complete across
stack/shared/dynamic, index/member, and managed/non-managed targets; only an *optional* member
target (`p?.x = v`) remains a loud error.

### mut parameters are passed by reference

**`mut` parameters are passed by reference** (07/29, `paramIsByRef`): `lowerParameter` emits a
pointer to the parameter's type, `defineFunction` binds that incoming pointer *directly* as the
binding's slot (no alloca+store — that copy was the bug), and the call site passes the
argument's address via `lvalueAddress`, so `f(p)`, `f(grid[i])`, and forwarding a by-ref
parameter onward all name the caller's own storage. Until then every parameter was copied, so
interior assignment through a `mut` parameter mutated the callee's private copy and the write
reached the caller **only** when the value was already behind a pointer (`mut shared T`, `mut
[]T`), silently vanishing otherwise (`mut Person`, `mut [2]string`, even `mut Counter { n: u8 }`
— nothing to do with managed values) — a type-dependent split with no diagnostic either way,
contradicting the typechecker's own message that `mut <type>` mutates "the caller's value".
`own` stays by value (it *transfers* — the copy is the point) and **`ref` is by reference too**
(07/29): it is also a borrow, so copying a value to lend it out read-only was pure waste — a
`ref [8]i64` was passed as a 64-byte first-class aggregate at every call, now a pointer. It
can't write, so the callee's view stays read-only; what changes is that it sees the caller's
*live* value rather than a snapshot, observable only when the same binding also reaches a `mut`
parameter of that call — which `checkExclusiveMutableBorrow` now rejects (see below). Unlike
`mut`, a `ref` argument may be a **temporary** (`f(Pt { x: 1 })`, `f("a" ++ "b")`) — lending one
out is legitimate — so `argumentAddress` materializes a non-lvalue into an entry-block alloca
and passes that; an owned temporary is still released after the statement by the ordinary
pending-temp machinery. The ownership pass is untouched: `mut` is still a borrow, so the slot is
**not** framed and the callee releases nothing. Two consequences: `arrayLValue` must address a
by-ref array parameter in place (materializing it into a fresh alloca would reintroduce the
copy), and `slotElemType` replaced the `slot.(*ir.InstAlloca)` assertions at every site that
reads a local's pointee, since a by-ref parameter's slot is an `ir.Param`. A `mut` on a **copied
scalar** stays by value — exactly the case `lyra-W010` calls inert, sharing the one
`types.IsCopiedScalar` predicate so diagnostic and lowering can't drift; a scalar has no
interior, and the only construct that could observe a by-ref scalar (whole-parameter
reassignment, `n = n + 1`) doesn't lower for integers anyway. The typechecker now enforces at
the **call site** that a `mut` argument is an lvalue rooted at a mutable binding
(`checkMutArgument`, sharing `rootBindingIsMutable` with `checkLValueAssignment`), so a
temporary or a `let` binding can no longer be passed where it would be mutated. It also enforces
that a **`mut` borrow is exclusive** (`checkExclusiveMutableBorrow`): within one call, a binding
passed to a `mut` parameter may not be named by any other argument. Since `mut`/`ref` are both
pointers to the caller's storage, two such arguments naming one binding are two views of the
same memory, and what the non-`mut` view observes would depend on statement order inside the
callee — `both(p, p)` with `(a: ref Pt, b: mut Pt)` reads 1 or 99 by where `a.x` sits relative
to `b.x = 99`, and two `mut` parameters let each write clobber the other. Both compiled silently
before. Deliberately narrow (argument *roots* within one call, which is exactly the aliasing
by-reference passing introduces — Lyra has no general borrow checker); two `ref` arguments may
share a binding, and scalars are exempt since they're passed by value.

## Pattern matching

### [head, ...tail] lowers

**`[head, ...tail]` lowers** (07/29): the tail is a **fresh** `[]T` box — the one array binding
that is not a borrow, since the elements it needs are a suffix of a box whose header describes
the *whole* array, so there is no existing storage to alias. `bindTailSubArray`
(`match_array.go`) allocates a box sized at run time (`length - fixedCount`), copies the suffix
in a loop **retaining each managed element** (the tail's drop glue will release them, so the
reference is duplicated rather than moved out of the source), and binds it in an **arm-scoped
managed frame** — the release must sit on the path that did the allocation, since an arm that
never matched has an uninitialized slot (releasing it faults). The length test becomes `>=` for
such an arm. It needed **no ownership-pass change**, which is what it had long been filed as
blocked on: the pass keys managed-ness off the recorded type, and a pattern binding is never
last-use-eligible, so an owning use inside the arm (returning the tail, passing it to an `own`
parameter) records a plain `Retain` — exactly the +1 an escape needs, with the frame release
balancing the box's own reference. Cost is one retain/release pair versus a transfer. A
**guard** is tested after the bindings exist, so a failing guard would skip the body's release
and leak the box; its false edge therefore gets its own block that releases the arm frame before
falling through (the *pattern*'s own failure edges need none — the length and element tests all
branch before anything is allocated).

### The three destructuring *statements* lower

**The three destructuring *statements* lower** (07/29, `destructuring.go`) — `let (a, b) = v`,
`if let pat = v { … } else { … }`, and `let pat = v else { … }`. All type-checked before but
none compiled. They are one mechanism differing only in the non-match path, and all three drive
the **same** pattern machinery `match` is built on (`patternMatcher` returns the
`aggPatternTest`/`aggPatternBind` pair for a single pattern), so a pattern means exactly the
same thing in a match arm and in an `if let` — two implementations of "does this value match
this pattern" would drift. A `shared` scrutinee is unboxed first, as in a match. Being
statements, they need no merge phi, only a control-flow join. Per form: a plain `let` requires
an *irrefutable* pattern (a pattern that imposes a test is a loud error pointing at `let …
else`, rather than binding on a path where the match may not hold); an `if let` binds in the
then-block **inside a branch-scoped local scope** (`pushLocalScope`), which is what scopes the
names to that branch — emitting the bind into the then-block is not on its own enough, since
`l.locals` is a flat map and the binding would stay installed for the code *after* the
statement: with an outer binding of the same name (`let x = 7; if let Some(x) = m { … }; x`) the
trailing read then resolves to the *pattern's* slot, returning the matched payload on the taken
path and uninitialized garbage on the untaken one. Silently and with a wrong value, since the
collector scopes the name to `Then` and only codegen disagreed — the same shape as the match-arm
shadowing bug, fixed 07/29 (`llvm_destructuring_test.go`); a `let … else` binds into the
*continuation* block, sound precisely because the else branch must diverge (a fall-through would
be a use of unbound names, so it is a loud error). Deferred with a loud error: destructuring an
**array** with a pattern, whose length-test-plus-element-tests shape belongs to the array match
driver rather than a single test/bind pair.

### match on a data value lowers

**`match` on a `data` value lowers** (`lowerMatch`/`lowerDataMatch`) — the destructuring
counterpart to construction: store the scrutinee, load its tag, and `switch` to one block per
arm (DATA_LAYOUT.md). A data-pattern arm reinterprets the payload blob as its variant's payload
struct (the same `bitcast`+`load` mirror of construction) and binds each field (`extractvalue` →
alloca → `l.locals`, so the arm body reads them like any local); a `_`/identifier arm is the
switch default (an identifier binds the whole scrutinee). The arms feed a merge phi, so `match`
is a value like `if`; exhaustiveness (lyra-E009) is a hard error for `data`, so a match with no
catch-all gets a default block that traps (`sealMatchFallthrough` — unreachable in a well-typed
program; defense in depth against a gap in that enforcement). The typechecker binds arm pattern
variables so the body type-checks and records their types (`checkMatchExpr` →
`withPatternBindings` → `walkDestructuredPattern`, reusing the `paramTypes` machinery);
`bindDataPatternPayload` accepts both flat (`Rect(w, h)`) and single-tuple-param (`MkPair((x,
y))`) forms.

### match on a bool/integer scalar lowers

**`match` on a bool/integer scalar lowers** too (`lowerScalarMatch`) as an if-else ladder: each
non-catch-all arm is a comparison (`icmp eq` for a literal, `scrut >= lo && scrut </<= hi` for a
range pattern — signed/unsigned predicates from the scrutinee) that cond-brs to the arm body or
the next test, first-match-wins; a `_`/identifier arm ends the ladder (an identifier binds the
scalar value), and the unmatched fall-through **traps** (`sealMatchFallthrough`, `trap.go` —
`lyra: match not exhaustive` on stderr, exit 101, the same discipline as a failed bounds check).
It was a bare `unreachable` until 07/29, which was **undefined behavior** on a reachable edge:
exhaustiveness is only a *warning* for int/string/rune/float/array/tuple/struct scrutinees and
warnings never gate a build, so a fell-through match ran off into UB (SIGTRAP at -O0, arbitrary
under optimization) — and a fully-guarded match reached it deterministically. All four match
fall-through edges (the scalar ladder, the aggregate ladder, the array ladder, and the `data`
tag-switch default) route through the one helper. A pure-literal match would be more compact as
an LLVM `switch`, but the ladder is used uniformly so a range arm fits (the optimizer recovers
the switch).

### match on a struct or tuple value lowers

**`match` on a struct or tuple value lowers** too (`lowerStructMatch`/`lowerTupleMatch`): these
are single-shape aggregates (no tag/switch), so both go through one shared if-else ladder
(`lowerAggregateMatch`) — a pattern with no literal sub-pattern matches unconditionally and
binds by position/name (`extractvalue` → alloca → `l.locals`), a literal sub-pattern (`{ x: 0, y
}`, `(0, b)`) makes the arm conditional, and a `_`/identifier arm is the catch-all. The
per-pattern test and bind are the mutually-recursive `aggPatternTest`/`aggPatternBind`, which
walk a struct/tuple pattern against the (first-class) aggregate value: a literal/range
sub-pattern ANDs an `icmp`/range check (via `scalarMatchTest`), and a **nested struct/tuple
sub-pattern recurses** (`((a, b), c)`, `{ inner: { v } }`, `(Pt { x, y }, c)`) — always safe
with `extractvalue`, no branch, because a single-shape aggregate has no tag. The struct-vs-tuple
difference (named fields vs positional elements) is just which branch of `aggPattern*` runs; the
caller passes them as the ladder's `test`/`bind` closures. A struct pattern may be brace-only
(`{ x, y }`, structural) or **type-named** (`Pt { x, y }`, symmetric with construction) — the
collector's `reclassifyStructPatterns` pass turns the type-named form (which the grammar parses
as a `DataPattern`, since `Pt { … }` and a data variant `Node { … }` are identical shapes) into
a named `StructPattern`, so the backend sees one node type and lowers named and bare
identically; the typechecker verifies a named pattern's type matches the scrutinee. A shorthand
field (`{ x }`) binds `x`, `{ x: a }` renames to `a`.

### Nested data sub-patterns lower

**Nested `data` sub-patterns lower** too, integrated into `aggPatternTest`/`aggPatternBind`: a
`data` sub-pattern's *test* is its tag check (`extractvalue`-the-tag == variant index, ANDed
into the aggregate's condition, so `(c, Some(x))` discriminates on the nested tag), and its
*bind* reinterprets the payload (`extractDataPayload`: store-to-slot + `bitcast` the blob, the
same memory move as `lowerDataMatch`, but on a nested value) and recurses. `bindDataPayload`
(the top-level `data` arm) likewise recurses via `aggPatternBind`, so a payload that is or
contains a struct/tuple is destructured (`Wrapped((a, b))`, `Boxed({ x, y })`).

### Value-testing data payload sub-patterns lower

**Value-testing `data` payload sub-patterns lower** (`Some(0)`, `(c, Some(0))`): a single tag
`switch` routes each tag to exactly one block, so it can't express two arms that share a tag but
test different payloads. Two paths handle this. *Nested in an aggregate* (`(c, Some(0))`):
`aggPatternTest`'s `DataPattern` case ANDs the tag check with a test for each value-testing
payload field — it `extractDataPayload`s the payload and recurses
(`scalarMatchTest`/`aggPatternTest`) branchlessly; when the tag doesn't match, the reinterpreted
payload bits are meaningless but the tag comparison has already forced the AND false, and
reading them is harmless (they stay within the union's stack blob). *Top-level* (`match m {
Some(0) => .., Some(x) => .., None => .. }`): `lowerDataMatch` detects a payload test via
`dataMatchHasPayloadTest` and falls back to the shared if-else ladder (`lowerAggregateMatch`
with `aggPatternTest`/`aggPatternBind` closures over the `data` type) instead of the `switch` —
each arm's condition is the tag check ANDed with its payload tests, first-match-wins; the
no-payload-test case keeps the compact `switch`.

### Match arm guards lower

**Match arm guards lower** (`Some(x) if x > 0`): a guard is a boolean test evaluated *after* the
pattern matches and its variables are bound, so it may reference them — `lowerGuardedArmBody`
cond-branches on the guard to the arm body (true) or to the next arm (false, exactly like a
failed pattern test). It plugs into both ladders (`lowerScalarMatch`, `lowerAggregateMatch`); a
guarded arm never seals the ladder (it can fall through), and a `data` match with any guard
takes the ladder fallback (`matchHasGuard`) rather than the `switch`. The typechecker checks
each guard condition with the pattern's bindings in scope and requires it to be a `bool`
(`checkMatchExpr`); a guarded arm never counts toward exhaustiveness (it may fail), which was
already the case.

### A float scalar match lowers

**A float scalar `match` lowers** through the same `lowerScalarMatch` ladder (the dispatch and
the ladder now accept a float LLVM type, not just an integer one): `scalarMatchTest` delegates a
float scrutinee to `floatScalarMatchTest`, which emits `fcmp oeq` for a literal arm and a
two-sided ordered range check (`fcmp oge`/`olt`/`ole`) for a range arm; an identifier catch-all
binds the float, and guards work unchanged. A float match always needs a wildcard (the reals
can't be enumerated — the typechecker warns otherwise). A float *literal* arm (`1.5 =>`) also
warns (`checkNumericMatchArm`, shared code `lyra-W008` "imprecise float equality"): it lowers to
`fcmp oeq`, the same exact-equality hazard the `==`/`!=` operator warns about (the operator
warning carries the same `lyra-W008`), so a value off by an ULP silently won't match — a range
pattern is the reliable form.

### A string scalar match lowers

**A string scalar `match` lowers** too (same `lowerScalarMatch` ladder): each literal arm is a
byte-equality test (`stringScalarMatchTest` → `lowerStringEquality`), an identifier catch-all
binds the whole fat pointer. A string pattern's source text is raw-quoted (unlike a
`StringLiteralExpr.Value`, which the collector already unescaped), so the quotes are stripped;
an *escaped* pattern (containing a backslash) and a regex pattern (`r/…/`) are deferred with
loud errors.

### A rune scalar match lowers

**A `rune` scalar `match` lowers** through the same ladder: a `rune` is an i32 code point, so a
char-literal arm (`'a' => …`) is an `icmp eq i32` against the **pre-decoded** code point — the
collector stores a char pattern as an `ast.RunePatternValue` (reusing the char-literal
expression decode for escapes), not raw text — and an identifier catch-all binds the rune. Char
*range* patterns (`'a'..'z'`) are deferred (the `range_pattern` grammar bounds are
`_number_literal`-only).

### A shared aggregate scrutinee

**A `shared` aggregate scrutinee** (data/struct/tuple) is a pointer to its ref-counted box, so
the match unboxes it first (`unboxSharedData` — load the inline payload out of `box → field 1`)
and every path above runs on the first-class union; an identifier catch-all binds the *box
pointer* (its declared type), so `lowerAggregateMatch` threads that `whole` value separately
from the unboxed `scrut`, and the box's own drop is the ordinary last-use release (reading
through it consumes no reference). This is the prerequisite for Perceus reuse/FBIP on `shared`
values.

### Perceus reuse / FBIP (stage 3) lowers

**Perceus reuse / FBIP (stage 3) lowers** on top of it: when the ownership pass marks a data
match as a reuse source (owned `shared data` scrutinee at its last use, plain tag switch, ≥1
same-type-constructing arm — `ReuseMatch`/`ReuseTarget`), `lowerDataMatch` reclaims the box once
after unboxing via **`lyra_rc_drop_reuse`** (the box when unique `rc==1`, else null; retiring
the scrutinee's slot to suppress its ordinary drop) and hands the token to the arms — a
reuse-target construction writes the new value into the reclaimed box (`lowerBoxSharedReuse`, a
runtime null-check branch: reclaim vs a fresh `lyra_rc_alloc`), and a non-constructing arm frees
the token (`free(NULL)` is a no-op). Arm-binding *transfer* (`LastUseTransfer` on a field used
exactly once in a consuming match — no dup) is what keeps a reused cell unique so a recursive
`map` reclaims every cell (zero allocation per cell) instead of leaking the tail. A **`shared`
value is returned** as its box pointer (`emitReturn`'s pointer case), and the typechecker's
`propagateAllocation` stamps the arm-tail constructions `shared`. A *borrowed* scrutinee is
never reused.

### Deferred, loud errors

**Deferred, loud errors:** a *nested* `shared data` sub-pattern (destructuring a tail through
*its* box, not just the top-level scrutinee); an array scrutinee (that type doesn't lower at all
yet, so a parameter/value of that type fails first — the match dispatch is never reached).

### Reuse still open

**Reuse still open:** stage 4 specialization (skip shared-field stores + a static-uniqueness
fast path), the ladder-fallback path (guards / value-test payloads), and struct/tuple reuse.

## Floats

### Floats lower

**Floats lower** — a float literal (`lowerExpr`'s `FloatLiteralExpr` case → `constant.NewFloat`
at `literalFloatType`, the recorded width, default f64), float arithmetic (`applyFloatMathOp`:
`fadd`/`fsub`/`fmul`/`fdiv`, `frem` for truncated `%`, and a `select`-based floored-remainder
fixup `lowerFlooredFRem` for `%%` — the float mirror of `lowerFlooredSRem`), `fneg`, float
comparisons (`lowerFloatComparison` → `fcmp`, ordered predicates except `!=`'s unordered `une`
so `NaN != x` holds), int→float and float-widening conversions (`lowerNumericConversion`:
`sitofp`/`uitofp` from the source's signedness, `fpext`/identity via `coerceFloatWidth`), and
float params/returns (`emitReturn` handles a `FloatType` `retType`).
`lowerMathBinaryOpExpr`/`lowerMathAssignOp`/`lowerBooleanBinaryOpExpr` all dispatch on the
already-lowered operand's LLVM type (`*lltypes.FloatType` → the float path, else the int path).
The `i64(x)`-style conversion call still rejects float→int as lossy (no rounding mode implied);
a float reaches an int width (and so the u8 exit code) explicitly, via
**`x.floor()`/`.ceil()`/`.round()`** (`rounding.go`'s `lowerBuiltinMethodCall`, dispatched from
`lowerFunctionCallExpr` when the call's callee is a `*ast.MemberExpr` naming a builtin rather
than a struct field/trait method/user function): the receiver's Lyra float type picks the
matching `llvm.<op>.<width>` intrinsic (`floor`/`ceil`/`round`, half-away-from-zero for `round`
— lazily declared and cached per name in `l.roundingIntrinsics`, the same lazy-declare-and-cache
shape as `memcmpFunc`), then `fptosi` to the builtin's fixed `i64` return type. Out-of-range/NaN
inputs are LLVM's ordinary `fptosi` poison behavior — no range check on the float→int
*conversion* yet (integer arithmetic itself — `+`/`-`/`*` overflow, division/remainder by zero
and `INT_MIN / -1`, and `-INT_MIN` — *is* fully checked; see the checked-arithmetic note near
the top).

## Strings

### Strings lower

**Strings lower** as an immutable fat pointer `{ i8* data, i64 len }` (byte length, not
NUL-terminated — `StringLLVMType`, `SizeAndAlign` = 16/8; full rationale in STRING_LAYOUT.md). A
string literal (`lowerStringConstant`) interns its bytes in a private immutable global `[N x
i8]` and builds the struct from a `getelementptr` to the first byte plus the length — no
allocation (the collector already unescaped `StringLiteralExpr.Value`). `==`/`!=`
(`lowerStringEquality`/`lowerStringComparison`) are branchless: `len_a == len_b &&
memcmp(data_a, data_b, min(len_a, len_b)) == 0` — `memcmp` over the min never reads past either
buffer (memory-safe even when lengths differ) and `n = 0` is a valid no-op; `memcmp` is libc
(declared lazily via `memcmpFunc`, clang links it). Strings have no ordering (`< > …` need
numeric operands). A string is passed/returned by value (the fat pointer is 16 bytes) —
`emitReturn` gained an aggregate-return path, and a `let`-bound string round-trips through
alloca/store/load. `lowerType`/`LLVMPrimitive` map `string` to the struct, so string struct
fields and (once sizeable) payloads flow through too.

### Concatenation ++ lowers

**Concatenation `++` lowers** (`lowerStringConcat`) — the first value this backend
heap-allocates: a concatenated string's bytes don't exist until run time, so it can't point into
a constant global; it allocates a ref-counted box (`rcAllocPayload` → `lyra_rc_alloc`),
`memcpy`s both operands' bytes into the payload, and returns a fat pointer `{ box+8, la+lb }`
into it (`memcpy` of length 0 is a valid no-op, so an empty operand needs no special case; a
chain `a ++ b ++ c` composes left-to-right, a fresh box per step). Heap strings are **freed** by
the ownership model: every string value is a box (a **literal** interns as a *pinned* static box
`{ i64 PinnedRC, [N x i8] }`, `data` at `box+8`, so retain/release no-op on it; a `++` value is
a heap box), making refcounting uniform. `pkg/analyzer/ownership` computes
retains/temp-releases/last-uses; the backend (`ownership_lower.go` + `lowerExpr`/`emitReturn`)
releases each managed binding at its scope exit (a stack of frames; a
`return`/`break`/`continue` releases the frames it exits before sealing) and retires it early at
its **last use** (Perceus stages 1–2, scalars): an owning last use (`let b = a`, `return a`)
*transfers* the reference (no dup) and a borrowing last use is *dropped* at that statement. Both
are **fused** (stage 2, no scope-exit release / no sentinel): a transfer removes the binding
from its frame at the move (`retireManagedSlot`); a drop is emitted by `dropLastUsesInStmt`
after the statement, in the post-dominating end block. A copy chain `a → b → c` compiles to one
allocation and one release. A non-last owning use retains (dups); an owned temporary is released
after its statement, each in a block that follows all of its uses (`flushStmtTemps`): a temp
built in an `&&`/`if` branch is freed there (dominating its uses), while a temp produced on the
statement's *start* block — an earlier call argument, say — is freed at the statement-*end*
block, so it survives a later argument whose branch moves control onward before the call
(releasing it in its production block was a use-after-free); temps of an enclosing statement
still in flight are protected by `pendingBase`; an `if`/`match` is one merged owned value
released once at the phi. `own` string params are released by the callee; bare/`ref`/`mut` are
borrows. The same machinery frees **`shared` values** (`IsManaged` covers them; retain/release
dispatch via `lowerManagedRetain`/`Release` on the value's representation — a string recovers
its box with `stringBox`, a `shared` value *is* the box pointer). Verified memory-safe under
AddressSanitizer with release==allocation conservation, plus a **path-sensitive** conservation
check (07/29, `conservation_check_test.go`): counting allocations against releases is the wrong
granularity for a leak that lives on one edge — the `[head, ...tail]` guard leak had one
allocation and one release, perfectly balanced, with the guard-false edge carrying the box past
its only release, and neither the counts, the behavioral tests, nor ASan (which on macOS reports
use-after-free and double-free, not leaks) saw it. The check walks the CFG of the emitted module
instead: from each `lyra_rc_alloc`, follow the box through bitcasts/GEPs/phis/aggregates and
local slots, and report if a `ret` is reachable with it neither released nor escaped. Tuned for
**no false positives** — any use it doesn't fully model (passing the box to a user function,
storing it through a computed pointer, returning it) marks the value *escaped* and drops it, so
it admits false negatives by design. `Backend.emitModule` exists to hand it the real
`*ir.Module` rather than re-parsed text. Two guards keep it honest: a hand-built leaky module it
must flag (it found nothing at all while matching names with llir's `Ident()`, which prefixes
the `@` sigil — the whole corpus was passing vacuously), and a per-program assertion that at
least one allocation was actually path-checked; managed values inside a `shared` box are freed
by the per-type drop glue (`drop.go`), and — since **deep-retain-on-copy** (07/29) — so are
managed values inside a plain *stack* aggregate: ownership is **deep**, so copying a
struct/tuple/`[N]T` retains each managed value it transitively owns (the per-type **retain
glue**, `retain.go`, the mirror of `drop.go`) and stack-aggregate bindings are framed and
deep-released at scope exit.

### The ref-counted runtime

**The ref-counted runtime** (`runtime.go`, `ensureRCRuntime`) is emitted as *real function
definitions into the module itself* — `lyra_rc_alloc` (malloc + rc=1), `lyra_rc_retain` (rc+=1,
`PinnedRC` no-op), `lyra_rc_release` (rc-=1, `drop_fn(payload)`+`free` at 0, pinned no-op) —
built on libc `malloc`/`free` (declared like `memcmp`/`memcpy`), so `lyrac build`'s single
`clang out.ll` stays self-contained with no runtime object to link. It's emitted **lazily** (a
non-allocating program carries none of it); the box header is a single `i64` refcount
(`rcHeaderSize = 8`, payload at `box+8`).

### ${…} interpolation lowers

**`${…}` interpolation lowers** (`lowerInterpolatedString`, strings.go) — the N-segment
generalization of `++`: each segment is formatted to bytes by the same `formatForPrint`
machinery print uses (a literal chunk is already a string; an interpolated
int/float/bool/rune/string is rendered per its type), and the pieces are concatenated into one
fresh ref-counted box, so the result is an owned heap string like `++` (the ownership pass
already treats an `InterpolatedStringExpr` as an owned producer whose segments are borrowed).
The typechecker (`inferInterpolatedStringExpr`) type-checks each segment as a printable scalar
(the print set) and settles an untyped numeric-literal segment to its default width; the whole
expression is `string`. The **collector** (`expressions/string_literal.go`) reconstructs each
literal chunk from the **raw source between** the interpolation nodes rather than from the
`string_content` node text: tree-sitter, with `/\s/` in `extras`, could strip a content chunk's
*leading* whitespace as token padding, so a chunk that starts with a space (a plain `"  x"`, or
the text right after a `${…}`) would otherwise lose it — slicing the source directly (the
interpolation nodes' byte ranges are exact and start at `$`) recovers every byte and also fixed
the same latent bug in plain string literals (`" "` used to collect as empty). The scanner-side
cause of that padding loss is gone too (07/29: the block-comment scan, which skipped leading
whitespace, no longer runs inside a string — see `tree-sitter-lyra/CLAUDE.md`), so the CST is
now byte-exact on its own; the raw-source re-slice stays as the authoritative path (it is
independent of scanner padding rules) and is what the tests pin. That same scanner fix is what
stops a string containing `/*` from lexing as a **comment** that swallows the following code
(`pkg/analyzer/collector/tests/string_whitespace_test.go`).

### Deferred, loud errors

**Deferred, loud errors:** escaped/regex string patterns.

## `print` / `println`

**`print`/`println` lower** (`print.go`) — the backend's first observable output. They're
compiler-provided builtins **polymorphic over the printable scalar types** (typechecker
`inferPrintCall`/`isPrintableType`: string, any integer/float, bool, rune → void), intercepted
in `lowerFunctionCallExpr` *after* the user-function lookup so a user binding of the same name
shadows the builtin (matching the typechecker's resolution order). Each formats its argument to
bytes (`formatForPrint`) and writes them to stdout via a lazily-declared libc `write(1, data,
len)`; `println` appends a second `write` of one interned `"\n"` byte. Formatting per type:
**string** → the fat pointer's own `{data,len}` (no copy); **int** → libc `snprintf`
`"%lld"`/`"%llu"` (by signedness, widened to i64) into an entry-block stack buffer; **float** →
`snprintf` `"%g"` (promoted to double); **bool** → a pointer/length `select` between interned
`"true"`/`"false"`; **rune** → UTF-8 encoded into a 4-byte buffer by the lazily-defined runtime
`lyra_rune_to_utf8` (1–4 bytes by magnitude, no surrogate check — matching rune's
unvalidated-code-point contract). snprintf formats into memory (not stdio), so numeric output
stays in program order with the raw string/bool/rune writes. `print` returns void — its value is
discarded — and the ownership pass treats it as **borrowing** its argument
(`calleeIsBorrowingBuiltin`), so a heap temporary argument (`print("a" ++ b)`) is released after
the call rather than leaked. Aggregates aren't printable (no Show/Display trait yet); float
formatting is first-cut `%g` (so `1.0` prints as `1`, and shortest-round-trip is a future
refinement). `layout.go` provides the llir type toolkit — `LLVMPrimitive`, `IsSignedInt`,
`IsNumericConversionTarget`, `SharedBoxType`, `TagType`, `DataUnionType` (all returning `llir`
`types.Type`) and the `SizeAndAlign` datalayout engine — that `lowerType` dispatches over. The
builtin overflow-arithmetic methods (`typechecker/builtins.go`), the `stack`/`shared`
representation (ALLOCATION.md), and the `data` tagged-union layout (DATA_LAYOUT.md) are the
settled lowering decisions; SIMD is a roadmap (SIMD.md).

## Emitted symbol names

A top-level user function is emitted as **`lyra.<module>.<name>`** (`userSymbol`), and a generic
specialization the same way (`lyra.identity$i64`). `main` keeps its own name — it is the C entry
point the platform links against — and the runtime's own symbols keep their `lyra_` prefix,
which the dot after `lyra` guarantees can never be spelled by a user symbol.

The prefix fixes a **reachable bug**, not a stylistic one: emitted functions used to take their
source name verbatim, so a program with a function called `malloc`, `write`, `memcmp` or
`lyra_rc_alloc` produced a module clang rejected outright ("invalid redefinition") against libc
or against Lyra's own emitted runtime — and those are names a program has every right to use.
Carrying the module path also makes a symbol unique *across* modules, which is the property
separate compilation and per-module private names will both rest on. Trait methods
(`Box$Show$show`) and generic types (`Box$i64`) were already mangled for their own reasons.

Note this does **not** yet let two modules each declare a private `helper`: the front end still
rejects a duplicate top-level name program-wide, because names share one namespace. Mangling
removes the *backend's* objection; the remaining one is per-module name resolution.

## Trait-method lowering (`traits.go`)

A trait-impl method lowers to an **ordinary function taking the receiver first**, and a method
call to a direct call. Dispatch is entirely **static** — the typechecker already chose the impl
— so there are no vtables and nothing is resolved at run time. Until 07/30 an impl type-checked
and then failed the build with `unsupported method call`, which is why the standard library's
combinators had to be free functions.

Methods are emitted **lazily, at the first call**, like generic types and for the same two
reasons: an uncalled method costs nothing, and the *call site* is where the receiver-substituted
signature exists (`impl Show<t> for Box<t>` has no single type until a receiver picks one). That
is why a **generic impl needs no extra machinery** — dispatch has already substituted `Self`
with the concrete receiver, and `typetable.Resolution` now carries the impl and that signature
alongside the method, so the backend never re-derives Self substitution. Duplicating it would be
a second implementation of "what is this method's type", free to disagree with the one that
type-checked the call.

The synthesized function is built as a real `*ast.LambdaExpr` (trait signature for the types,
impl clause for the parameter names and body) and lowered through `defineFunctionInto` — the
same path a plain function and a generic specialization take, so parameter binding, `own`-param
framing, and the void/typed return split cannot drift between them. The **emitted symbol names
type, trait and method**: neither pair alone is unique, since one type may implement two traits
declaring `show` and one trait may be implemented by many types. Bodies are deferred to a queue
rather than lowered re-entrantly (a method calling another queues a second emission
mid-lowering, and the lowerer's per-function state — locals, loops, managed frames — would be
corrupted), and the declare-before-define split lets a self-recursive method terminate. `self`
has no run-time status: it is the receiver purely because dispatch put its type in the
signature's first position, which is where the call site passes it.

**Open:** a trait signature carries no borrow modifier (the grammar has nowhere to write one),
so every parameter including the receiver is by value; if that changes, `traitMethodLambda` is
the line that must carry it or the call site and body would disagree about who owns the
receiver.

## Behavioural tests

**AddressSanitizer only works because the harness adds the `sanitize_address` function
attribute** to every `define` in the emitted module (`instrumentForASan`,
`llvm_ownership_test.go`). This is not a tuning detail — without it the ASan tests are close to
useless, and silently so. ASan's instrumentation is an LLVM *pass* that rewrites only functions
carrying that attribute; clang attaches it when compiling C/C++, but a generated `.ll` has no
function attributes, so the pass instruments **nothing**. The runtime is still linked, so its
malloc/free interceptors catch an *invalid or double `free()`* — which is why the suite did
catch some faults — but no load or store is checked, so a plain use-after-free read or write
runs clean. Measured directly (07/29): a two-line IR program that frees a pointer then loads
from it exits 0 with no report; the same IR with the attribute reports `heap-use-after-free`
immediately.

This had swallowed real bugs. Three faults the suite was expected to catch went by — a
closure-capture double retain, a release of an uninitialized alloca on a weak upgrade, and a
refcount over-release at a managed generic type argument (which decrements a freed box's count
to −1, writing through freed memory but never calling `free()` twice, so no interceptor fires).
Each was eventually caught by a refcount-count or CFG assertion, or by CI. The pattern was
misread for a while as "macOS ASan is weak"; the actual cause was missing instrumentation, and
it applied **equally on Linux** — enabling the attribute makes macOS catch all three. The
attribute is applied as a text rewrite of the emitted IR rather than emitted by the backend on
purpose: it is a property of how the *test* wants to build the module, not of the program, and
emitting it from `lowerFunction` would put it in every ordinary build.

Turning it on immediately surfaced a real **heap-use-after-free** in `lyra_rc_release` (fixed
07/30): reassigning a *borrowed* `string` parameter released the caller's value, which the
caller then released again. The first diagnosis blamed the by-reference `mut` case; that one was
already correct, and the fault was the **by-value** borrow — see `slotIsOwning`.

Linux runs are available via the workspace's `./asan.sh` (see the workspace `CLAUDE.md`). They
are worth doing before pushing memory-model work for a *different* reason than ASan: Debian's
older clang still uses **typed pointers**, so it rejects an IR type mismatch that modern clang
(Apple clang 21, opaque pointers) cannot even represent — which is how a genuine miscompile was
found where a `(u8, u8)` tuple argument was built at the i64 default width and passed to a `{
i8, i8 }` parameter.

The `buildAndRun*` helpers compile emitted IR with clang and run the binary. Two
pieces of infrastructure keep this package fast — it dominates the suite's wall
time, and both exist because macOS security-assesses every *newly created*
executable on its first exec (syspolicyd/XProtect), serialized system-wide at
~200ms per binary (compilation parallelizes fine; re-exec of an already-assessed
file is ~1ms):

- **Everything is parallel** — every test function calls `t.Parallel()`, and
  table-driven cases run as parallel subtests (`t.Run` + `t.Parallel`). New
  tests should follow suit. ASan binary *executions* are capped by a small
  semaphore (`asanRunSlots`) — many simultaneous ASan startups can sporadically
  fail their shadow-memory mapping.
- **Compiled binaries are cached** (`exec_cache_test.go`): `compileCached` keys
  the binary on SHA-256(clang version | GOOS/GOARCH | flags | IR bytes) in
  `~/Library/Caches/lyra-llvm-tests` (`os.UserCacheDir()`). A warm run of the
  package takes ~2s; a cold run (or one after backend changes that alter most
  programs' IR) pays the serialized assessment (~2min for the full set). Only
  tests whose emitted IR changed recompile. Entries untouched for 30 days are
  pruned at init; deleting the directory just forces a full recompile.
