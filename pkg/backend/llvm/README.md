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
`lowerCallArgs`, `lowerNumericConversion`, and the array-literal element
loop; each was a nil dereference (a Go crash out of `lyrac`, not a loud error) before.
The call-argument check lives in `lowerCallArgs` because it once lived in the *direct*
call's own loop: `lowerIndirectCall` had a second argument loop with no guard, so
`f(panic(…))` through a function value still crashed until 08/05. One loop now serves
both, and it is also where by-reference `mut`/`ref` arguments are handled.
Positions with no guard fail loudly instead: a tuple literal with a panicking element
reports `unknown type: never`, because the *type* recorded for the tuple contains `never`
and no `diverged` check can rescue that — the tuple is uninhabited and the honest fix is a
typechecker diagnostic, not a lowering.

### The `?` operator lowers

`try.go`. `x?` is a `match` in disguise and lowers as one — test the operand's tag, unwrap
the payload on success, propagate the failure variant otherwise — so it joins `return` and
`panic` as a construct that seals a block mid-stream, and returns the *success* block as
the one control continues in (like `if` and `match`).

**The error arm is the part that is not a plain match, and it is why this is a lowering
rather than a desugaring.** The propagated value has a different LLVM type from the
operand: `?` on a `Result<i64, string>` inside a `-> Result<bool, string>` function cannot
forward the operand's union, because those are two distinct monomorphizations. So the error
payload is *extracted* and a fresh `Err` built around it at the enclosing function's return
type. That type is `l.retLyra` (set by `beginFunction`, unlowered) — `retType` alone cannot
say which constructor to build or what the payload's Lyra type is. It is stored
unsubstituted and read through `applyTypeSubst`, so a `?` inside a generic specialization
rebuilds at that instantiation's type.

The Result/Maybe shape is recognized by **constructor name** (`canonicalTryShape`), which is
not a guess: `collector/canonical.go`'s `canonicalShape` pins a canonical Result to exactly
`Ok(_) | Err(_)` and a Maybe to `Some(_) | None`, and refuses the `@builtin` marker to any
declaration that does not match. The *type* name is deliberately free, which is why this
reads the variants rather than the name — and why it does not add a third copy of the
canonical-kind name resolution the collector stamps and the front end reads.

**Refcounting: the propagated payload is duplicated or moved depending on how the operand's
own reference is disposed of**, and both mistakes are real bugs in opposite directions. A
borrowed operand (a binding) still owns its copy and will release it — at scope exit, or via
the `releaseAllManagedFrames` this very return runs — so the rebuilt error needs a reference
of its own. An owned temporary is not released on this path, so its reference is what the
error carries away and a dup there would leak. Inverting the two makes the borrowed case
print freed memory and trips ASan, which is what `TestExec_TryBorrowedOperand` and
`TestASan_TryManagedPayload` exist to catch.

**The propagating return releases the statement's dead temporaries itself, rather than
letting `emitReturn` flush them.** That flush releases each pending temp *in the block that
produced it*, and the operand's producing block is the one before the branch — so flushing
from the error block would put a release ahead of that block's terminator, freeing the
operand ahead of the tag test that reads it, on the **success** path too. So the error block
emits its own releases (`releaseTempsOnExit`) and then raises `pendingBase` to keep the
return's flush off them.

`releaseTempsOnExit` differs from `flushStmtTemps` in the one way that matters here: **it
does not truncate the pending list.** The propagating path is one exit from a statement that
still has another, and the success path must still reach the statement's own flush —
truncating would move the release rather than add one, leaking on every non-exiting path
instead. The operand's own temp is excluded, its reference having transferred into the
rebuilt error.

**What it releases is bounded by dominance**: only temps produced in the block the branch
was emitted from, which is the error block's predecessor and so certainly live there. A temp
produced inside a conditional sub-expression (an `&&` right operand, a match arm) is not
dominated by that block, and releasing it would touch a value undefined on the path the
branch did not take; those still leak, the safe direction.

### `??` lowers (08/13)

`null_coalescing.go`. `a ?? b` is `?`'s value-position sibling — the same match in
disguise (`match a { Some(v) => v, None => b }`) with everything that made `?` a hard
lowering removed: nothing leaves the expression, so there is no rebuild at another type,
no early return, and both arms feed one phi under the ordinary merge rules. It had
type-checked and failed to lower since it was collected — the 07/30 `?` shape.

The default is **lazy** — an arm, not an operand — which is what makes
`m ?? panic("missing")` meaningful and rules out lowering both sides eagerly and
selecting. Ownership follows the match rules exactly (ownership.go's NullCoalescingExpr
case): optional borrowed as a scrutinee, default a conditional arm coerced to owned by
its own node's marks, merged value a uniformly-owned temp released once by the enclosing
statement's flush. The Some payload is the one value with no node of its own to mark, so
its +1 (deepRetain, duplicate-never-move) is emitted in the lowering directly — `?`'s
failure-rewrap arrangement. The typechecker's half is `propagateLiteralType` on the
default against the unified type, because the phi requires the arms to agree
(`m ?? 7` on a `Maybe<u8>` lowers the 7 at u8; `?? 300` is refused).

A left operand that is not a canonical Maybe is refused at check time (`lyra-E049`;
a warning as lyra-W007 until 08/13), so a build never reaches the backend with one —
the backend's own loud refusal (rule 5) survives as a broken-guarantee defense, the
same posture as `?`'s shape checks.

`break`/`continue` leaked the same way and are fixed differently, because the producing
block dominates a `break` without being its predecessor — no block-equality test reaches it.
See **Exit releases** below.

### Exit releases (`dominators.go`, `resolveExitReleases`)

A `break` or `continue` leaves every statement between it and the loop without reaching
their flushes, so it owes those statements' pending temporaries a release. It may only
release the ones live where it stands: an SSA value has one defining block, so it is
available at the jump exactly when that block **dominates** the jump's block. Releasing a
non-dominated one frees a value the taken path never produced — a double free, where
skipping merely leaks.

Dominance is a property of the finished CFG, and the CFG does not exist while the jump is
being lowered; an edge added later can only *remove* dominators, so computing it early could
report a dominance a later edge invalidates — the unsound direction. So `lowerBreak`/
`lowerContinue` record the obligation (`recordExitReleases`) and `resolveExitReleases` emits
it after the body, against a tree that can no longer change. Appending to the sealed jump
block is safe: llir keeps `Insts` and `Term` apart and prints instructions first, so the
release lands before the jump, and every release is straight-line so no block is split.

`loopCtx.tempBase` bounds which temporaries are the jump's to release — the mirror of
`frameDepth`. One recorded below it belongs to a statement enclosing the whole loop, whose
flush still runs after the loop exits; releasing it at the jump too would double-free.

Note the dominance check has never been observed to reject a temporary: the candidates that
would need it are released in their own block by the statement flush before a sibling jump
is lowered. It is kept as a guarantee rather than a demonstrated filter — see COMPLETED.md
(08/03) for why that trade was taken rather than relying on the structural argument.

### Logical not lowers

`!x` is `xor i1 x, true` (`lowerNotBooleanExpr`, `arithmetic.go`). It type-checked long before
it lowered — reaching the backend as "expression lowering not implemented for
`*ast.NotBooleanExpr`", so no program using `!` could be built at all.

That is also why a **precedence** bug rode along with it undetected: `!`'s grammar operand was
`$.expression`, so `!a && b` parsed as `!(a && b)` — the opposite grouping from every C-family
language, and silent, since both readings are well-typed `bool`. Narrowing the operand to a
postfix expression (`_not_operand`, tree-sitter-lyra) fixed it; `PREC.UNARY` on the rule could
not, because a precedence does not stop a wider operand rule from absorbing more. Pinned by
`TestExec_NotBindsTighterThanAnd`/`...Or`, which assert on an observable **side effect** rather
than the result: both readings usually agree on the value and differ only in whether the right
operand is evaluated.

### The `<=>` three-way comparison lowers (08/06)

`a <=> b` yields the prelude's `Ordering` (`Less | Equal | Greater`), not a bool — the one
operator on `BooleanBinaryOpExpr` whose result is not `i1`, so it leaves
`lowerBooleanBinaryOpExpr` before the icmp-predicate table (`lowerSpaceship`).

**Branchless.** All three variants are nullary, so the values differ in exactly one field —
the tag — and two `select`s plus an `insertvalue` on an undef union produce it. The payload
blob is left undef, which DATA_LAYOUT.md already specifies for a nullary variant. No new
blocks, so the call site returns the block it was given: a branching call site returns a
*merge* block, which the old `flushStmtTemps` handled as though it were conditional, and
that is the bug `read_line` hit (its dominance test since 08/07 gets the merge block right,
but keeping the lowering branchless is still the cheaper shape). `Ordering` owns nothing so it could not bite here, and the shape is pinned
by a test on the emitted IR anyway.

Two things that would each be a silent wrong answer rather than a crash: the predicates
follow the **operand's signedness** (`u8(200) <=> u8(1)` is Greater; read as signed, 200 is
-56 and it flips), and the tags come from `findConstructor` rather than a hard-coded 0/1/2,
so reordering the prelude's variants cannot quietly miscompile every three-way match.

Integers and runes only — **floats are refused in the typechecker**, because NaN is neither
less than, equal to nor greater than anything and a three-way answer has to pick one. See
`todo.md` for the partial-ordering question that decides.

### Bitwise and shift operators lower

`arithmetic.go`. `&`/`|`/`~` are a plain `and`/`or`/`xor`; prefix `~x` is `xor x, -1` (LLVM
has no complement instruction, and `-1` is an all-ones mask at any width). None of them can
trap — every bit pattern has a complement, at every width and either signedness.

Shifts carry three things the others do not (`emitShiftOp`):

- **Width.** The count is typed independently of the shifted value, so `u8 << i64` is
  well-typed while LLVM requires both `shl` operands to share a type. The count is coerced
  to the shifted width — *after* the check, deliberately, since coercing first could
  truncate an out-of-range count into range and hide exactly what is being checked.
- **Signedness of `>>`.** Signed shifts arithmetically (`ashr`, sign-filling), unsigned
  logically (`lshr`, zero-filling). This is the only place the two spellings of `>>` differ,
  and getting it backwards is a wrong answer rather than a crash —
  `TestExec_ShiftRightSignedness` pins both directions with values that disagree.
- **The amount check.** `shl`/`lshr`/`ashr` are UB once the amount reaches the operand's
  width, so an out-of-range count branches to `lyra_panic_shift_overflow` — the same
  treatment div-by-zero gets, for the same reason. The comparison is **unsigned**, which
  catches a negative count in the same instruction. A compile-time constant already in range
  (`constShiftInRange`) emits no check at all, covering `x << 3`; range-analysis elision for
  a *variable* count is not wired up yet (todo.md).

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
entry through `emitReturn(nil)` so owned temporaries flush).
**Destructuring params lower** — see [below](#destructuring-parameters-lower).
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
(recorded in `lowerer.structTypes`, keyed by the declaration's **type key** — see
Per-module type identity below — and **not** by `TupleType.GetName()`, which renders the full
`Point(i32, i32)` shape), then
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

`resolveForLayout` also normalizes a **`ParameterizedType`** through `resolveInstantiation`
(generic_types.go) before laying it out, so a generic used *by value* inside another type —
`struct Node { parent: Maybe<weak Node> }`, a payload, an array element — is sized as the
concrete aggregate its instantiation denotes. Without it the instantiation reached
`SizeAndAlign` as a shape none of its cases match and boxing the enclosing value failed with
"cannot size a `shared T` payload yet" (08/03). A `shared` instantiation short-circuits
first, for the same reason and with the same finiteness argument as the `UnresolvedType` arm.
This is the case `resolveInstantiation`'s own comment anticipated: every site reading an
aggregate's shape switches on NamedStructType / TupleType / DataType, and a
ParameterizedType matches none of them, so it is normalized once at the accessor rather than
cased for at each site.

### Per-module type identity

A type name is **not** program-wide. Two modules may each declare a private `Point`, and a
module may declare its own `Maybe` over the prelude's; the symbol table settles which
declaration a name means with `declKey` (bare when exported, `<module>::<name>` when private or
prelude-shadowing), and `l.structTypes` is keyed the same way through `l.typeKey(name)`. Keyed
by bare name it was not: two same-named private structs emitted a single `%Point` carrying the
*union* of both field lists, which clang rejected as a redefinition — loudly, but only at
`clang` rather than as a Lyra diagnostic.

**The asking position is ambient, not threaded** (`type_identity.go`). Unlike the function path's
`funcKey(name, loc)`, a resolved `NamedStructType` reaching `lowerType` carries only a name — the
location it was written at is long gone — and `lowerType` is reached from essentially every
expression. What the lowerer does have is the item it is currently lowering, which belongs to
exactly one module: `l.currentLoc` is set by `enterModuleOf` per type declaration/definition and
inside `declareFunctionAs`/`defineFunctionInto`, plus `lowerEntry` for `main`. Those two shared
bodies are deliberately where it sits rather than `declareFunction`/`defineFunction`, because a
**trait method** and a **generic specialization** lower through them too and would otherwise
resolve names against whichever module was lowered last.

It is an `ast.Location` rather than a module path so the backend deals in the same currency as
every other caller (`LookupTypeFrom`, `ownership.OwnsManaged`) and the symbol table keeps sole
authority over the file → module step. `lookupTypeDecl(name)` is the backend's `LookupTypeFrom`;
a bare `SymbolTable.LookupType` is wrong here because it cannot see a private declaration at all.
The emitted type *name* keeps the declared spelling whenever the key equals it — so ordinary IR
is byte-for-byte what it was — and is mangled only when qualified. A generic instantiation's
symbol (`Box$i64`) is qualified the same way, which is a separate path and would otherwise still
have collided.

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
`lowerTupleLiteralExpr` and `lowerDataConstruction` — and, since 08/05, `lowerStructInstanceExpr`,
the third way into an aggregate, which had been left out — run each lowered element through
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
by its signedness, then a single unsigned `>= size` compare (valid range [0, size); a negative
sign-extends to a large unsigned value, so one compare catches both ends) branches to the
`lyra_panic_index_out_of_bounds` trap (Pit-of-Success, like checked arithmetic) before a
`getelementptr`+`load`. A negative index **traps** — it counted from the end until 08/12;
`xs.from_end(k)` is the explicit end-relative accessor now (`lowerArrayFromEnd`: the element
at `len - k`, the same one-compare trick on `len - k >= len`). A constant index is range-checked at compile time
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

### Array comprehensions lower (`array_comp.go`, 08/04)

`[ x in xs | guard | result ]` allocates one box and fills it: the generators become nested
counter loops, the guards a short-circuiting conjunction, and the surviving results are
stored at a running count which becomes the box's `len` at the end. **This is the only way
to build an array** — there is no growth operation and no collected spread — so it is what
makes `map`/`filter` for `[]t` writable in the prelude.

**Capacity is the product of the source lengths**, allocated before anything is known about
how many elements survive a guard. The three options were to run the generators twice (once
to count, once to fill), to grow the box as it fills, or to over-allocate once and record
the real length. Running twice is *wrong* rather than slow — a guard may call a function,
and evaluating it twice per element makes the call count a detail of the lowering. Growing
needs a reallocation primitive the language does not have. Over-allocating costs memory only
on a filtering comprehension, and keeps every guard evaluated exactly once.

**Arrays, ranges and strings** are all sources. Each is a `compSource` that **emits its own
loop** rather than exposing an index: the index model fits an array and a range and not a
string, since UTF-8 is variable width and its walk is a byte cursor whose advance is
whatever the decoder just consumed. Arrays and ranges share `countedSource` (run *n* times,
bind `at(i)`); a string gets its own cursor loop.

**The capacity must bound the loop by construction, not by agreement** — writing past the
box is memory corruption, not a wrong answer. An array's bound is its length. A string's is
its **byte** length, which bounds the rune count because no encoded rune is shorter than a
byte. A range's count is derived once (`ceil(span/step)`, clamped at zero, with the divisor
made safe before the division since `sdiv` by zero is undefined) and the loop is then driven
by that count rather than re-testing `i < end` — so a degenerate range yields an empty array
instead of a fill loop racing past the allocation.

Since 08/04 a range also has a **direction**, taken from its end operator (`..>` / `..>=`,
`types.RangeDescends`), and the span is measured along it: a descending range spans
`start - end`. Direction never comes from which bound is larger — `5..<1` is an ascending
range that is empty, and must produce nothing rather than quietly counting down. `for-in`
gets the same information through `rangeLoopPredicate`, which picks one of eight
comparisons from direction × inclusivity × signedness, and subtracts when descending.

Deferred, loud error: a generator whose source **depends on an earlier generator**
(`[ row in grid, cell in row | cell ]`) — sources are materialized once before the loops,
which is exactly what makes the capacity computable.

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
START..<END` (also `..<=` inclusive, and an optional `:step`) lowers to a counter loop
(`lowerForInRange`: `i = START; while i </<= END { body; i += step }`) — the counter *is* the
loop variable, its width the first concrete-integer bound's type (else i64, matching
`iterableElementType`). The advance is **guarded** (08/12): the counter moves only when it can
move by `step` and stay inside the range, so an inclusive `..<=` to the counter type's max
terminates after visiting it instead of wrapping past it and looping forever, and a large step
cannot leap an exclusive end back into range. A **runtime** step of zero or less traps
(`lyra_panic_range_step`) on the shift-amount ladder — a constant one is refused at check time. A **string** iterable `for c in s` walks the string's
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
string is UTF-8, so runes aren't randomly addressable — the position comes from
`lyra_str_rune_offset` and this decodes the one rune there. A non-negative `i` costs O(i)
(prefer `for c in s` for a full traversal); out of range traps out-of-bounds, with the string
trap rather than the array one. Also deferred: growth (no grow op exists yet), and `match` on
a `shared` array.

**A negative index traps, and `s.from_end(k)` is the end-relative accessor** (08/12; a
negative counted from the end from 08/08 until then, and `slice` took negative bounds the
same way — both removed, because the most common off-by-one got a valid read of the wrong
element in a trap-over-silent-wrongness language). The accessor keeps the whole performance
story (measured after the change: `from_end(1)` ×2000 on an ~1800-rune string is 2 µs where
`s[s.len() - 1]` ×2000 is 6082 µs): finding the k-th rune from the end is a **byte** walk
that skips continuation bytes (`10xxxxxx`) until it reaches a lead byte — well-defined
because UTF-8 is
self-synchronizing, and costing O(k) with *no decoding*. So it is not sugar over an existing
spelling, it removes an O(n) tax that had no workaround: `s[s.len() - 1]` is two full decode
walks (`len()` is O(n), then `s[i]` is O(i)), measured at **34272 µs against 18 µs** for
`s[-1]` over 2000 reads of a 2000-rune string.

**`s.byte_offset(i) -> Maybe<i64>`** (`lowerStringByteOffset`) exposes that same walk to
Lyra as the rune→byte conversion the language otherwise cannot perform. It is what makes
"does `sep` occur at rune i" cheap —
`s.compare_bytes_at(s.byte_offset(i).unwrap_or(-1), sep) == 0` — and the two compose
without a `match` precisely because `compare_bytes_at` is **total**: `unwrap_or(-1)` hands
it an offset it already answers negative for, which is the payoff for having made it total
rather than trapping. It maps *positions*, so `allowEnd` is set and the end position is
`Some(byte_len)` rather than `None` — slice's rule rather than indexing's, since a bound
may name the end (`s.slice(a, n)`), and the asymmetry with the trapping `s[n]` is
deliberate. Branchless, built the way `checked_*` builds its Maybe. Without it, a prelude
`split` could only ask that question by allocating (`slice(i-m, i) == sep`) or by scanning
to the end of the string (`index(sep, i-m) == i-m`), either of which makes it quadratic.

`lyra_str_rune_offset(data, byteLen, idx, allowEnd)` (`strRuneOffsetFunc`) is the one
definition of "where does rune k begin", returning a byte offset or -1, and all three
callers go through it. Both callers used to
carry their own copy of the forward walk — the same question answered twice, which is why a
negative bound could not be added to one without being added to the other by hand (rule 8).
It returns -1 rather than trapping so each caller raises the panic that fits it, the index
and slice traps saying different things. `allowEnd` admits `idx == runeCount`, whose offset
is the byte length: slice needs it (an exclusive end, and `s.slice(n, n)` is "") and indexing
must not have it. Two consequences for `slice`: it resolves each bound with its own call, so
it is O(start) + O(end) where the old single pass was O(end) — that pass advanced a rune
counter forward and no value of it means "third from the end" — and the ordering check
compares the resolved *offsets*, since comparing the written bounds would reject
`slice(1, -1)` on `1 > -1`.

### Byte-level string primitives

**`s.byte_len()`** (`lowerStringByteLen`) is the fat pointer's own length field, O(1), where
`len()` is the rune-count field beside it (both O(1) since 08/12). **`s.compare_bytes_at(offset, other)`**
(`lowerStringCompareBytesAt`) is `compare_bytes` at a byte offset, comparing **exactly
`other`'s length** rather than the rest of `s` — which is what makes `== 0` a *prefix* test
instead of an equality test, and is the only semantic difference between the two.

They are the primitives the prelude's `starts_with`/`ends_with` are one line each on top of
(and `contains`/`split` will be a loop over). **Byte-level is not an approximation:** UTF-8
is prefix-free and self-synchronizing, so for a well-formed string a byte-prefix is exactly a
rune-prefix and a byte-suffix exactly a rune-suffix — the same property `impl Ord for string`
leans on when it answers a rune question with one memcmp. Exposing a byte length is a
deliberate, narrow crack in "runes are the language, bytes are the representation"; `len()`
remains what ordinary code wants, because it is the one that agrees with `s[i]`.

The reason they exist is measured rather than assumed. The rune-indexed prelude versions were
O(m²) for a prefix and O(n·m) for a suffix (`s[i]` is O(i)), and paid an O(n) `len()` before
comparing anything — so `s.starts_with("--")` on a 2000-rune string decoded the whole string
to answer a question about two bytes, with the length calls alone measuring **99.7%** of the
cost. Building on `slice` + `compare_bytes` instead fixes the quadratic term but allocates
(losing `noalloc`) and is still O(n), since `slice` walks runes to find the offset. These are
O(m) with no decoding and no allocation: 19.9 ms → 19 µs on that case, 112 ms → 4 µs for a
400-rune needle.

`compare_bytes_at` is **branchless and total**. Every out-of-range case folds into a select —
the offset is clamped before it reaches the GEP, the memcmp length is clamped to what `s`
actually has, and a shortfall (or an offset that was out of range at all) forces a negative
result. No trap, so no caller has to guard; branchless for the reason `read_line` and `<=>`
are, since a call site ending in a merge block is not a case `flushStmtTemps` handles. One
ordering rule is easy to guess backwards: **a byte mismatch decides before a shortfall**
(memcmp's own order), so `"hello".compare_bytes_at(4, "lo")` is *positive* — `'o' > 'l'` —
rather than negative for the missing byte. Short-sorts-first settles only a range that matched
as far as it went, and every predicate built on this asks `== 0`, where the distinction cannot
arise.

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

### A match may be a statement (08/06)

An arm body is **value-optional**, exactly as an `if` branch is: it goes through
`lowerBranchValue`, so a block body whose last statement is an assignment simply comes back
with no value. Until 08/06 the arm bodies went through `lowerExpr`, which routes a block to
`lowerBlock` and *requires* a value — so `match m { Some v => { x = v; }, None => { x = 0; } }`
failed with "block has no value", and every statement-position match had to be rewritten as an
`if`/`else` chain. `if` never had the problem, because it had been using the value-optional
helper all along; the two helpers answer the same question and only one of them was reaching
the arms.

**Four sites lower an arm body** — the shared ladder (`lowerMatchLadder`: scalar, struct,
tuple), the `data` tag switch, and two helpers in the array match — so a partial fix leaves the
identical source failing under a different scrutinee, which is the remote symptom rule 8
describes.

`matchMerge` carries the bookkeeping: a **reaching** arm with no value sets `void`, and
`value()` then yields nothing rather than a phi over a nil operand. That is a different case
from the one it already handled — a **diverged** arm has a sealed block and contributes no edge
at all, whereas a void arm reaches the merge and control continues past the match. Arms may
mix: one ending in an expression beside one ending in an assignment makes the match a statement
and discards the stray value.

The consumer side needed a guard for the same reason. `lowerVarDecl` knew about a *diverging*
initializer but not a *void* one, so `let r = if c { x = 1 } else { x = 2 }` dereferenced a nil
`init.Type()` and **crashed the compiler** — a pre-existing violation of the never-panic
invariant, now an error naming the binding. Binding a void expression is not rejected by the
typechecker (only warned as unused), so the backend is where it is caught.

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

### Destructuring parameters lower

**Destructuring parameters lower** (07/31, `destructuring.go`) — `let sum = ((a, b): (i64,
i64)) -> i64 => a + b`, `({ x, y }: Pt)`. This is the *fourth* destructuring form and the last
one that did not compile, and it goes through the same `patternMatcher` the three statements do
for the same reason: one meaning of "this pattern against this value", not two.

It is the **irrefutable** form — a parameter has no failure path, since there is no `else` and a
function cannot decline to be called — and that is checked rather than assumed, because the
typechecker admits a value-testing sub-pattern here (`((1, b): (i64, i64))`, `(Just(v): Opt)`)
and the backend has nowhere to put the failing edge. Refused with a message, as
`lowerDestructuringDecl` refuses `let Some(v) = m`.

One helper, `bindParameters`, binds parameters for **every** shape of function — plain,
generic specialization, lifted closure (whose `ir.Param` slot 0 is the environment, hence its
`offset`), and trait-impl method, which reaches it because `Resolution.Lambda` synthesizes a
`LambdaExpr` whose parameters are the impl's clause patterns. The two loops it replaced had
already been copied once.

The ownership rule is the one the other pattern forms use: **bound names are borrows**, not
framed for release, because they are field copies out of a value someone else owns. For a bare
or `ref` parameter that owner is the caller. For an `own` one it is this function, so the
*whole* incoming aggregate is framed — one release that `drop.go` walks into each managed
field, rather than one per bound name, which is also what frees a managed field the pattern
never named (`({ age }: own Person)` must still free `name`). A field that escapes the callee
is retained on the way out, since a pattern name has no declaration inside the function and so
is never last-use-eligible. Both directions are ASan-covered in
`llvm_destructuring_param_test.go`, along with the refcount shape itself.

Two refusals. A **`mut`** parameter cannot be destructured: the bindings would be copies, so a
write could not reach the caller, which is the entire content of `mut` — a borrow that silently
is not one. **`ref`** *is* supported (the pointee is loaded and destructured from there): it is
read-only, so copying fields out changes nothing observable, and a destructuring parameter asked
for the fields by value in the first place. And an **array** pattern parameter fails with the
same message a `let [a, b] = arr` does, since static-array patterns are unimplemented
everywhere, not specifically here.

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
(`lowerMatchLadder`) — a pattern with no literal sub-pattern matches unconditionally and
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
`dataMatchHasPayloadTest` and falls back to the shared if-else ladder (`lowerMatchLadder`
with `aggPatternTest`/`aggPatternBind` closures over the `data` type) instead of the `switch` —
each arm's condition is the tag check ANDed with its payload tests, first-match-wins; the
no-payload-test case keeps the compact `switch`.

### Match arm guards lower

**Match arm guards lower** (`Some(x) if x > 0`): a guard is a boolean test evaluated *after* the
pattern matches and its variables are bound, so it may reference them — `lowerGuardedArmBody`
cond-branches on the guard to the arm body (true) or to the next arm (false, exactly like a
failed pattern test). It plugs into every ladder — `lowerMatchLadder`, which `lowerScalarMatch`
delegates to, and the array one — so a guarded arm never seals the ladder (it can fall
through), and a `data` match with any guard
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
pointer* (its declared type), so `lowerMatchLadder` threads that `whole` value separately
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
**`x.floor()`/`.ceil()`/`.round()`** (`rounding.go`'s `lowerFloatMathMethod`, reached from
`builtin_methods.go`'s `lowerBuiltinMethodCall`, dispatched from
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
after its statement, each in a block that follows all of its uses (`flushStmtTemps`): the test
is **dominance** — a temp whose production block dominates the statement-*end* block is freed
there, so it survives a later argument whose branch moves control onward before the call
(releasing it in its production block was a use-after-free), while a temp built in an
`&&`/`if` branch does *not* dominate the end block and is freed in its own, the only block
that produced it. That test used to be `p.block == start`, a proxy that held only while every
other block was conditional; `slice`/`read_line`/`<=>` branch unconditionally, so their
continuation blocks broke it and two `slice`s in one expression freed the first result before
allocating the second (08/07); temps of an enclosing statement
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
`lyra_f{16,32,64}_to_str`, the shortest-round-trip formatter below; **bool** → a pointer/length `select` between interned
`"true"`/`"false"`; **rune** → UTF-8 encoded into a 4-byte buffer by the lazily-defined runtime
`lyra_rune_to_utf8` (1–4 bytes by magnitude, no surrogate check — matching rune's
unvalidated-code-point contract). snprintf formats into memory (not stdio), so numeric output
stays in program order with the raw string/bool/rune writes. `print` returns void — its value is
discarded — and the ownership pass treats it as **borrowing** its argument
(`calleeIsBorrowingBuiltin`), so a heap temporary argument (`print("a" ++ b)`) is released after
the call rather than leaked. Aggregates aren't printable (no Show/Display trait yet).

**A printed float reads back as the same value** (08/13). It was `snprintf("%g")` until
then — six significant digits, so `0.1 + 0.2` printed `0.3` and `1234567890.0` printed
`1.23457e+09`, each a different number from the one held, with nothing to say so.
`floatToStrFunc` emits one formatter per printed width that renders at increasing
precision and `strtod`s each candidate, stopping at the first that comes back equal; the
ladder's top rung is the width's IEEE round-trip guarantee (17/9/5 significant digits),
so it always terminates faithfully, and `%g`'s trailing-zero stripping means the bottom
rung usually answers in one iteration. **The comparison is at the value's own width** —
an f32 candidate narrows back to `float` first, because 0.1f32 widened to a double is
0.10000000149011612 and a double-width check would print all of it. Shortest within the
ladder, not provably minimal; Ryu is the upgrade.

Two things depend on that being right. `floatConst` (arithmetic.go) **rounds** a literal
to its target width before handing it to llir, which otherwise truncates the mantissa
when emitting a narrower type — `let x: f32 = 0.1` shipped as `0x3FB9999980000000`, one
ULP below 0.1f32, so the program held a number its source did not name. That bug was
invisible while printing was lossy (both constants printed `0.1` at six digits), which is
the case for round-tripping output in general: a lossy printer hides other faults. And a
float literal in a *comparison* takes the operand's width (also 08/13): `x == 0.1` against
an f32 emitted a **double** constant into a `float` compare and clang rejected the module,
because the imprecision warning sat on an `else if` where the width propagation belonged —
so the operators it warned about were exactly the ones that never propagated. Both fixes
meet in one constant: `float 0x3FB99999A0000000` is the right type from the second and the
right value from the first.

`layout.go` provides the llir type toolkit — `LLVMPrimitive`, `IsSignedInt`,
`IsNumericConversionTarget`, `SharedBoxType`, `TagType`, `DataUnionType` (all returning `llir`
`types.Type`) and the `SizeAndAlign` datalayout engine — that `lowerType` dispatches over. The
builtin overflow-arithmetic methods (`typechecker/builtins.go`), the `stack`/`shared`
representation (ALLOCATION.md), and the `data` tagged-union layout (DATA_LAYOUT.md) are the
settled lowering decisions; SIMD is a roadmap (SIMD.md).

### `read_line` lowers (`input.go`, 08/05)

**`read_line() -> Maybe<string>`** is the program's only console input, and the only builtin
that *returns* an owned managed value. It is intercepted in `lowerFunctionCallExpr` beside
`print`/`panic`, after the user-function lookup, so a user binding shadows it.

The runtime shim (`ensureReadLineRuntime`, `lyra_read_line`) is emitted into the module like
the refcount shims, so `clang out.ll` stays self-contained. Three decisions in it are
load-bearing:

- **It reads with `getchar`**, not `getline`/`fgets`. Those need the `stdin` global, whose
  *symbol* is platform-dependent (`__stdinp` on macOS, `stdin` on glibc) — a host conditional
  in otherwise target-independent IR. `getchar` needs no such global.
- **It reads straight into a ref-counted box** (header + capacity, `realloc`'d as it grows),
  so what comes back is an ordinary owned heap string that release and drop glue already
  understand — no scratch buffer, no copy, and no "string that came from input" special case.
- **It returns the `Maybe` union itself**, doing the null test and both constructions
  internally, so the *call site emits no branches*. That was not tidiness: `flushStmtTemps`
  used to release a temporary at the statement's end block only when it was produced in the
  statement's *start* block, and otherwise **in its own production block** (a temp produced
  inside a branch is undefined on the other path). A merge block is neither, so an earlier
  version that branched at the call site had the owned `Maybe<string>` released *in that merge
  block* — before the `match` consuming it ran its switch, printing a line of blanks instead
  of the input. That flush asks dominance now (08/07), so a merge block would no longer break
  it; the branchless call is kept because an owned builtin result behaving exactly like an
  ordinary function call is the cheaper thing to reason about.

The line terminator is consumed and not returned, and a trailing `\r` goes with it. EOF with
bytes already read is a final unterminated line; EOF with nothing read is `None` — which is
the whole reason the return type is `Maybe<string>` rather than `string`, since a bare string
cannot tell EOF from a blank line and the natural read loop then never terminates.

The ownership pass needs `calleeIsOwningBuiltin` for this: a builtin has neither a
`LambdaExpr` nor a `LambdaType`, so it falls to the unresolved-callee default, which treats a
**result** as borrowed. That default is leak-safe for arguments and the wrong direction for a
result — the consuming site retains a +1 value (a leak) and a discarded call is never released
at all. Verified by reverting the rule under LeakSanitizer on Linux: 848 bytes across 5
allocations, one per line read (`TestExec_ReadLineUnderASan`).

Parsing is deliberately **not** here: `parse_i64` is written in Lyra in `std/prelude.lyra`.
The line has to come from libc and Lyra has no FFI, so input genuinely cannot be expressed in
the language; parsing can, and anything that can belongs in the prelude where it is readable
and replaceable.

### `random_seed` lowers (`random.go`, 08/05)

**`random_seed() -> u64`** returns one word of OS entropy, and is the *only* part of
randomness in the backend. The generator — `Rng`, `next_u64`, `below`, `between`,
`random_below` — is ordinary Lyra in `std/prelude.lyra`, because a PRNG is arithmetic and
arithmetic is expressible; asking the OS for entropy is not.

The shim uses **`getentropy`**: available on both targets (macOS 10.12+, glibc 2.25+),
unlike `getrandom` (Linux-only) and `arc4random_buf` (glibc 2.36+), and it needs no `FILE*`,
so it avoids the platform-dependent `stdin` symbol problem that shaped `read_line`. The slot
is filled with `time(NULL)` **before** the getentropy call rather than after a failure test:
POSIX leaves the buffer unspecified on failure, so testing the return value and then reading
an untouched buffer would seed from an uninitialized stack word. The fallback is weak by
design (one-second resolution) and is not a security primitive.

The result is a `u64` — a plain scalar owning nothing — so unlike `read_line` there is no
ownership question and nothing for the temp machinery to do.

**Keeping the *seed* as the primitive is what makes `det` usable with randomness.** A seeded
generator is arithmetic over its own state, so `rng.below(100)` carries only `EffectMut`,
which `det` permits; the Rand bit is charged exactly at the point of asking for a seed nobody
supplied. Had the builtin been `random_below`, every draw would be non-deterministic and
`det` code could not draw at all. None of that is written down as a rule — it falls out of
bottom-up effect inference over the prelude.

### `wall_clock_nanos` lowers (`clock.go`, 08/06)

**`wall_clock_nanos() -> i64`** is `clock_gettime(CLOCK_REALTIME, …)` and nothing else — the
same division of labour `random_seed` has: asking the OS what time it is cannot be expressed
in Lyra, while seconds, elapsed durations and formatting are arithmetic and belong in the
prelude.

It existed as an entry naming nothing until 08/06 — `wallClock` in `checker/effects.go`'s
`builtinEffects`, tagged `EffectTime`, with no typechecker signature and no lowering. That is
the `Random.global()` shape, and implementing it was chosen over deleting the entry because
deleting would have left `EffectTime` a bit nothing in the language could set.

`struct timespec` is two 64-bit words on both targets, so it is built as `[2 x i64]` rather
than a named struct, and **CLOCK_REALTIME is 0 on both**. The slot is **zeroed before the
call**, for the reason `random_seed` writes its `time(NULL)` fallback first: POSIX leaves the
struct unspecified on failure, so a program ignoring the return value would read
uninitialized stack to decide a timestamp. The product `sec * 1e9` uses plain `mul`/`add` —
this is runtime support below the language's checked arithmetic, and no epoch second a
`clock_gettime` can report overflows it.

The result is an `i64` (signed, because the useful operation on two instants is subtraction)
— a plain scalar owning nothing, so like `random_seed` there is nothing for the temp
machinery to do.

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
signature exists (`impl Show<t> for Box<t>` has no single type until a receiver picks one).
`typetable.Resolution` carries the impl and that signature alongside the method, so the backend
never re-derives Self substitution; duplicating it would be a second implementation of "what is
this method's type", free to disagree with the one that type-checked the call.

### A generic body may call another generic (08/04)

`unwrap<t>` written as `expect(self, "…")` — a generic calling a generic at a
*variable-dependent* instantiation — used to be refused with `type variable "t" has no
concrete type here`, because substitutions were not composed: the typechecker records that
inner call as `expect<t=t>`, where the right-hand `t` is the **caller's** variable, and
that is a template rather than a specialization.

Two halves, and the interesting one is not in this package:

- **Here:** `specializations()` emits only `Instantiations.Concrete()`, so a template is
  never lowered, and `specializedFuncFor` composes the lowerer's active `typeSubst` into
  the callee's bindings before keying — lowering `unwrap<t=i64>`, the call resolves to
  `expect<t=i64>`. Outside a generic body the substitution is empty and composition is the
  identity, so an ordinary call is unchanged.
- **In the driver** (`driver/instantiations.go`): the set of specializations is closed
  under that same composition *before* the per-instantiation ownership pass runs. That
  ordering is the whole reason it does not live here. `OwnershipBySpec` is built from the
  instantiation set, so a specialization discovered later would find no table of its own
  and fall back to the program-wide one — analyzed generically, where a type variable is
  not reference-counted, so a `t = string` body would emit neither retains nor releases.
  That is the double free this package already fixed once (see `ownership()`).

Polymorphic recursion (`f<t>` calling `f<Box<t>>`) is infinite and is refused, bounded on
type **depth** rather than count — what diverges is the type, and a count-only bound
terminates only after the set is both enormous and individually huge.

`substituteTypeVars` is now `types.Substitute`: the driver needs the same walk, and a
switch over composite types that exists twice drifts (hazard 8). Moving it turned up
`*LambdaType`, which this package's copy never handled — invisible until something
substituted into a signature carrying a callback, which is exactly a generic combinator.

### A generic impl is monomorphized, one function per binding set (08/03)

The paragraph above used to end "which is why a generic impl needs no extra machinery". It
needed exactly the machinery a generic *function* needs, and the absence showed up two ways.
A body that touched the impl's type variable could not lower at all —
`match on Maybe<t> not implemented yet`, `field access on non-struct type Box<t>` — because
the body was emitted with `t` still abstract. And a body that did *not* touch it lowered once
and was called with every receiver type:

```llvm
define i64 @Box$Sized$size(%Box$i64 %self)
  call i64 @Box$Sized$size(%Box$i64 %4)
  call i64 @Box$Sized$size(%Box$boolean %6)   ; ← the same function
```

Apple clang accepts that (opaque pointers make the two function types indistinguishable), so
it stood as a silent miscompile rather than a build failure — the class of bug `asan.sh` and
its typed-pointer clang exist to catch.

Dispatch had been computing the bindings all along, to check the impl's `where` bounds; they
now travel on the resolution as `Bindings`, and `Resolution.SpecKey()` names the specialization
for all three consumers that must agree — the emitted symbol, the method cache, and the
ownership table. The body is then lowered exactly as a generic function's is: `pushTypeSubst`
with the bindings, so `lowerType` and `recordedType` — the two accessors every type already
funnels through — make the whole body concrete without rewriting a node.

**The substitution has to survive the queue.** Bodies are deferred (see below), so a
substitution pushed while declaring is long gone by the time the body is lowered;
`pendingTraitMethod` carries the bindings and the spec key, and the define loop installs both.

**Ownership is per specialization too**, and this is where "no extra machinery" was most
expensive: `pkg/analyzer/ownership` walks top-level declarations, which impl methods are not,
so **no method body had ever been analyzed** — generic or otherwise, a method holding a
`string` emitted neither retains nor releases. `driver.OwnershipByMethod` now holds one table
per `SpecKey`, built by the same `ownership.AnalyzeLambda` the generic-function path uses.
Whether a value is reference-counted is a property of the type *argument*, so the answer at
`t = string` (retain the returned payload) is not the answer at `t = i64` (do nothing), and
the tables cannot be merged — they are keyed by AST node, and it is the *same* node.

**Not composed, and deliberately so.** A generic body calling a generic impl method
(`getOr<t>` calling `o.unwrap(d)` on an `Opt<t>`) is refused with "type variable t has no
concrete type here", exactly as a generic function calling another generic function already
was. Both are the same limitation, and both fail loudly rather than emitting a body at the
wrong instantiation.

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

**Borrow modifiers travel with the parameter** — `Resolution.Lambda` copies each signature
parameter's `Borrow` onto the synthesized `ast.Parameter`, which is what makes a `ref Self` a
pointer on both sides. This paragraph used to say a trait signature carried no modifier and
that everything was by value; `ref`/`mut` landed 07/31, and the line it warned about is the
one now doing the carrying. `own` is still rejected (lyra-E030) — see `todo.md`, where the
ownership prerequisite it named is now built.

### Bound dispatch: two tables, and which to ask

A **bound-dispatched** call (`v.show()` under `where t: Show`, and every call on `self`
inside a trait default) is resolved twice. The typechecker resolves it *abstractly* — the
receiver is a type variable there, so no impl can be named — and publishes one concrete
candidate per implementing type; the concrete answer arrives here, where a specialization
has fixed the variable. Two accessors exist because both halves are easy to skip, and
skipping either is silent:

- **`candidateKey(expr)`, not `recordedType(expr).String()`, for the lookup.** The
  candidate tables are keyed in the *typechecker's* spelling of a type (`Box<i64>`, and
  the mono key `Box$i64`); `recordedType` takes one further step this package needs
  everywhere else, normalizing a `ParameterizedType` to the emitted instantiation's named
  type — whose name carries the declaring module's key, `main__Box$i64`. Asked under
  that, a generic impl's candidate is never found and the call fails to lower, **in any
  program that declares a `module` and in none that does not**.
- **`methodParams(call, res)`, not `methodParamModes(call)`, for the borrow modes.** A
  bound call has no entry in the *resolution* table — that is the point of the candidate
  table — so reading modes from it returns nil and every operand goes by value. A `mut`
  receiver's emitted method takes a pointer, so it is handed a struct: not a mismatch
  anything can diagnose, a wild load.

`Trait::method(receiver, …)` is lowered by `lowerTraitPathCall` and is *not* a bound call
— dispatch records its full resolution like a `.`-call's. The one difference is the
operand layout: the receiver is argument 0 rather than a separate expression, so
arguments and signature parameters are index-aligned where a `.`-call offsets by one.

## Raw pointers (`pointers.go`)

`&x`, `&mut x`, `p^`, `p^ = v` and `unsafe { … }`. **Every one is something this package
already did**, which is why the feature is small here after being large in the front end:
a raw pointer *is* an LLVM pointer, so `&x` is `argumentAddress` — the address a `mut`
parameter is already passed by — `p^` is a load, `p^ = v` a store, and an `unsafe` block
is its body. No ownership, no refcounting and no drop glue: a raw pointer does not own
what it points at.

Three things to keep:

- **`argumentAddress`, never `lowerExpr`, for the operand of `&`.** `lowerExpr` yields the
  *value*, and storing it into a fresh slot hands out the address of a copy. The
  typechecker has already refused a non-lvalue operand (`lyra-E059`), so the fallback path
  in `argumentAddress` is unreachable from here — and the error if it is reached is a hard
  one, per rule 5.
- **`^T` lowers to a pointer to its pointee, not `i8*`.** llir type-checks a store against
  the destination's element type, so an opaque pointer panics on every write before clang
  sees it. `^T` and `^mut T` lower identically; mutability is a front-end rule
  (`lyra-E061`), not a machine distinction. A named pointee is safe to name mid-layout,
  since every type is *declared* before any is defined.
- **`p.offset(n)` is a `getelementptr` with the pointee's type**, which is what makes
  "in elements" LLVM's own meaning rather than a scaling written here — on a `^i64` a
  byte-offset lowering would answer garbage from inside the first element instead of
  failing. The index is widened to i64 rather than assumed to be one: an untyped literal
  reaches here at whatever width propagation left it, and a GEP index of the wrong width
  is a module clang refuses. No bounds check, because there is nothing to check against;
  `std.ffi`'s CBuffer is where a length joins a pointer and the check becomes possible,
  in ordinary Lyra.
- **An `unsafe` block goes through `lowerBlockStmts`, not `lowerBlock`.** The latter
  insists the block has a value, and `unsafe { p^ = v }` ends in an assignment and
  produces none. `lowerForEffect` unwraps one to its body for the same reason a plain
  block is unwrapped there.

### Text and bytes (`decode_utf8` / `encode_utf8`)

`lowerStringConcat`'s shape — allocate a ref-counted box, memcpy in, hand back a fat
pointer — with two differences. The bytes come from an array rather than from two strings,
which is what `byteBufferOf` is for: a `[]u8` is a box holding a pointer, so its buffer is
a load and its length a field read, while a `[N]u8` *is* its elements and has no address
until `argumentAddress` takes one (spilling a temporary to a slot when the receiver is not
storage). And the rune count has to be **walked**: concatenation can add its operands'
counts because joining cannot split or merge a rune, but a byte buffer carries none, so
this is the third caller of `lyra_utf8_count` beside `read_line` and interpolation's
formatted segments.

`encode_utf8` is the same two allocations a `[]u8` always costs — `dynArrayAlloc`'s fixed
box plus a malloc'd buffer — with the string's payload memcpy'd into the buffer. Element
type i8 and stride 1, so the byte length *is* the element count and no scaling appears.

Ownership needs nothing from either: the typechecker records the builtin's signature on
the MemberExpr, whose return carries no `ref`/`mut`, so `isOwnedReturn` already answers
*owned* — and the `[]u8` gets `lyra_drop_1_DynamicArray_u8_` like any other. That is why
`slice` has no entry in `calleeIsOwningBuiltin` either: that list is for builtins called
as *free functions* (`read_line`), which have no signature to read.

## Foreign functions (`extern.go`)

`extern name: (…) -> T`, and a call to one. **Almost nothing is here, and that is the
design working**: an extern is a signature standing in for a body someone else supplies,
which is a shape this package already emits — a function declared before any body exists so
a call can reference it. `ExternDeclStmt.Func()` is that body-less function, so declaring
one is `declareFunctionAs` and calling one is the ordinary call path, found under the same
`funcKey` any other function is. Nothing downstream knows an extern exists.

Three things are decisions rather than details:

- **The symbol is the name as written**, not `userSymbol`'s `lyra.<module>.<name>`. A
  foreign symbol is the linker's, and mangling it would name a function nobody defines —
  failing as a link error about a symbol the source never mentions.
- **Two declarations of one foreign name are one `declare`** (`l.externs`, keyed by the C
  symbol). They have to be: each `extern` is private to its module — there is no
  `pub extern`, and `declIsPublic` says so — but the symbol they name is global, so two
  modules both declaring `strlen`, which is what two libraries using `strlen` looks like,
  would otherwise emit two `declare`s of one name. What *is* refused is two declarations
  that **disagree** about the signature: only one can describe the function that gets
  linked, so emitting either silently picks a winner (rule 5).
- **No ownership crosses.** Every parameter and result is FFI-safe by `lyra-E063` —
  scalars, `^T`, `void` — so none is reference-counted and there is no retain, release or
  drop glue to emit. The front-end rule is what makes this file short. It is also why
  `mut`/`ref` is refused on an extern's parameter: `paramIsByRef` is *Lyra's* convention,
  and at the boundary it is either inert (a `mut` scalar still goes by value) or an ABI
  mismatch (`mut ^i64` reads as a pointer and passes an `i64**`).

`@link("z")` reaches the link line through `driver.Result.Links` and `lyrac`'s
`linkFlags` — and through every "compile with" hint, which must name the same libraries as
the build it stands in for.

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

## `owned_walk.go` — the retain/drop walk, written once

`emitRetainValue` and `emitDropValue` are the two halves of one invariant: a copy of an
aggregate must add a reference to **exactly** the managed values its death removes one
from. Miss a field on the retain side and it leaks; miss it on the drop side and it is
freed while a copy still points at it.

They were ~150 duplicated lines each — identical down to the tag switch in the `data` arm —
and the history is what the duplication cost: both lacked `AnonymousStructType` until 08/08
(`{ m: string }` leaked one reference per value) and both lacked `ParameterizedType` until
08/07. Note *both* each time. Two copies agreeing and being wrong together is the failure a
side-by-side reading cannot find, which is why the durable fix is one walk rather than a
test that the two match.

`emitOwnedValue(block, v, t, retainWalk|dropWalk)` is that walk. The `ownedWalk` constant
selects what happens at a managed **leaf** — the single place a retain and a release
genuinely differ — and everything above it (which fields exist, which variant is live, how
a newtype resolves, how an instantiation substitutes) is shared. `emitRetainValue` and
`emitDropValue` remain as the names every call site already uses.

**`emitEqValue` is not folded in with them**, though it has the same arms in the same order.
Retain and drop stop *at* a managed value, because its own box owns whatever it holds;
equality descends *into* one to compare it, and it returns a value rather than performing an
effect. Nothing breaks if it visits a different set of fields than these two do, and that is
precisely what makes it a different walk instead of a third copy of this one.
