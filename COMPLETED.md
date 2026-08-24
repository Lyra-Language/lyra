# Lyra — Completed

The dated record of what has been built and, more to the point, **why it ended up the
way it did**: the constraint that forced a design, the measurement that disproved a
diagnosis, the bug a change turned out to be fixing. Open work lives in
[todo.md](todo.md); this file is the archive it points back to.

Newest first.

## Dated log

### 08/24/26 (6)
**Ten checker passes, ten identical error structs, ten identical driver loops.**

`AwaitOutsideAsyncError`, `BreakContinueOutsideLoopError`, `EffectBoundsError`,
`PurityError`, `RecursiveTypeError`, `ReturnOutsideFunctionError`, `TryOutsideResultError`,
`UnsafeOutsideUnsafeError`, `UseBeforeDeclarationError` and `YieldOutsideGeneratorError`
were all `{Code, Message string; Location ast.Location}` with an identical `Error()`. The
driver unpacked each back into a `diag.Diagnostic` field by field, in ten copies of

```go
for _, e := range checker.CheckX(program) {
    res.err(e.Location, e.Code, e.Message)
}
```

They return `[]diag.Diagnostic` now — which half the package already did — and the ladder is
one loop over a slice of results. The transformation was smaller than it looks: the three
field names already matched `diag.Diagnostic`'s, so each struct literal needed only its type
swapped and a severity added.

**Moving the severity into the pass is what made the loop possible.** The driver used to
stamp `SeverityError` on everything from these passes, which is why `CheckGenericParams` sat
outside the ladder — it alone reports both severities, an undeclared type variable being an
error and a declared-but-unmentioned one a warning. It is in the list now, and a pass that
later wants to warn no longer needs the driver to learn about it.

`typechecker.TypeError` keeps its own type: it carries the typechecker's two-valued
`Severity` rather than `diag`'s, and that mapping is real work rather than a copy. It became
`TypeError.Diagnostic()`, beside the two types it bridges — so a field added to `TypeError`
cannot silently fail to reach the output, which the six-field hand copy in the driver
allowed.

Net −118 lines. Nothing outside the checker package and its own tests referenced any of the
ten type names, which is what made this cheap; the 40 test references needed only their type
annotations changed, since `.Code`, `.Message` and `.Location` read the same on the
replacement.

### 08/24/26 (5)
**The LLVM backend's small duplications.**

Nine near-identical spellings, folded into six helpers, plus four pieces of dead or
superseded code. Individually minor; the reason to do them together is that each was a
place where one question had several answers.

- **`makeString`** — the three insertvalues building a string's `{ptr, byte_len,
  rune_count}` fat pointer, written out at six sites. The shape being repeated is what hid
  the interesting part: the *count* is maintained three different ways under it — a literal
  knows it at compile time, `++` and `slice` derive it arithmetically, and only `read_line`
  and interpolation pay the linear `lyra_utf8_count`. Those differences now sit at the call
  sites instead of being buried in identical scaffolding.
- **`icmpPred(rel, signed)`** — three ladders mapping a relation and a signedness to an
  LLVM predicate, reached from three different starting points: an AST comparison operator,
  a range loop's ascending/descending operator, and a `match` range pattern's
  inclusive/exclusive one. The shared half is the one that must not disagree — a `u64` loop
  bound read with a *signed* predicate compares negative and the loop does not run.
- **`declareLibc`** — eight lazily-declared libc functions, each with its own lowerer field
  and its own `if l.x == nil` guard. One map now. It reports whether *this* call created the
  declaration, which is how `exit` stays `noreturn` and `snprintf` variadic without
  re-stamping the attribute on every lookup.
- **`privateConst`** — five hand-set `Immutable`/`LinkagePrivate` pairs. One of them
  (`constGlobal`, in regex_match.go) said in its own comment that it was "the same shape
  cString uses", which is the tell.
- **`lowerTypeList`** — four copies of "lower each Lyra type into a field list", differing
  only in whether the result appends into an already-registered struct (so a recursive
  reference finds it) or builds a fresh one. `fieldTypesOf`/`anonFieldTypesOf` became
  one-liners over layout.go's `fieldTypes` at the same time.

Removed outright: **`IsNumericConversionTarget`**, dead, and by its own comment "mirrors the
typechecker's numericPrimitiveByName exactly" — an unenforced second copy of a table whose
live answer is `types.ConversionTargetName`; **`isManagedLLVMType`**, dead; **`maxInt`**,
which predates Go's builtin `max`; and **`emitCheckedDiv`** rebuilding INT_MIN with
`big.Int` twenty lines from a call to `intMinConst`.

**Verified by IR identity rather than by tests alone.** Emitting a module that exercises
strings, arrays, `match`, `data`, structs, closures, loops and float printing, before and
after: 32 of 2696 lines differ, and after normalizing SSA numbering the instruction
*multiset* is identical — four `sub`s moved a few slots earlier, where `makeString` now
evaluates its rune-count argument before building the struct. Pure reordering of a
side-effect-free instruction. The full suite and `./asan.sh ./...` are green.

### 08/24/26 (4)
**`declKey` memoization: implemented, measured at zero, reverted.**

`declKeyIn` is on every `Lookup*From` — 54 call sites outside `pkg/ast/symbols`, the
typechecker's hot path, re-run per keystroke by the LSP — and after collection it is a pure
function of `(module, name)`. The audit called for memoizing it, and a profile agreed it was
worth looking at: `declKey` is 1.64% of the pipeline cumulatively.

Built it: a `frozen` flag set by `Collector.Finish` after `PopulateImportScopes` (the last
thing that can change a key, since `shadowsImport` reads the import graph), with a
`map[declKeyQuery]string` behind it and an unfrozen table falling through uncached so a
hand-built table stays correct.

Measured **nothing**. Back to back, −0.3% and −0.4% — inside noise. Hashing a two-string key
costs about what the computation it replaces costs, so 1.64% was the ceiling and the cache
hands it back at the door.

Reverted, because it is not free to carry. The memo is sound only while nothing registers or
imports after the freeze, which is an ordering constraint between `Collector.Finish` and the
symbol table that a later change could break silently — and the failure mode is a wrongly
qualified key, which hides a declaration from every module including its own. A correctness
liability in exchange for a measured zero.

**Four performance findings from the audit have now been measured, and the split is the
thing worth keeping.** The two that paid were both about *allocation*: the purity fixpoint
rebuilding maps every round (−27% on the pass), and `implTargetMatches` allocating a map it
threw away (−25% allocations pipeline-wide). The two that did not were both about *repeated
computation that was already cheap*: indexing `traitImpls` by trait name, and this. Reasoning
about complexity from the shape of the code picked the wrong two; `pprof -list` on the hot
function picked the right ones on the first look, both times.

### 08/24/26 (3)
**Trait dispatch: the quadratic was not the cost, and the map was.**

The audit's finding was that `boundCandidatesByType` asks `resolveTraitMethodNamed` once
per impl of a trait, and each of those scans every impl in the program — O(impls²), with
108 impls shipping in the prelude. The shape is real. **The cost is not**, and measuring it
is what showed why.

A new `BenchmarkDispatch_*` (pkg/driver) puts the pipeline on a program of generic bodies
with `where` bounds, which is where bound dispatch actually fires — a concrete caller of a
generic function pays none of it, since the candidates were published once at the bound call
site. A CPU profile of it puts `boundCandidatesByType` at ~80% of the typechecker.

Indexing `traitImpls` by trait name, so each scan sees only that trait's impls, changed
**nothing**: +1.4%, −0.8%, +1.7% across the three sizes — noise straddling zero. The reason
is visible once the loop is read carefully rather than counted: the trait-name filter was
already applied *before* the expensive work, so the inner scan was doing 108 cheap string
compares and then the same ~15 real comparisons either way. Indexing removed the compares
and nothing else. **The index was written, measured, and reverted**; it is recorded here so
the next reader does not re-derive it.

The real cost was one line. `implTargetMatches` allocated its `bindings` map up front:

```go
bindings := map[string]types.Type{}          // 90ms of this function's 110ms
if types.TypesEqual(implType, receiverType) {
    return bindings, true
}
```

It is called once per impl of the trait being dispatched — on every method call, every
overloaded operator, and every `==`/`<`, since those route through `dispatchEq` /
`dispatchOrdCompare` before the structural rule — and the overwhelming majority of those
calls are a concrete impl that does not match. `runtime.makemap_small` was **82% of
implTargetMatches** and roughly two thirds of `resolveTraitMethodNamed`, all of it discarded
on the next line. Allocating on the paths that can fill it, and returning nil on the one
that cannot:

| | time | allocations | bytes |
|---|---|---|---|
| Dispatch_Large | −5.1% | **−25.6%** | −16.0% |
| Dispatch_Medium | −2.1% | −20.7% | −16.5% |
| Analyze_Large | −2.6% | −13.5% | −8.2% |
| Analyze_WideTypes | −2.1% | −17.1% | −9.3% |

Whole-pipeline numbers, not one pass in isolation — and the pipeline is dominated by parsing
and collection, which is what makes a 25% allocation cut on the analysis side show up as
5% wall clock.

**The lesson worth keeping is the order of operations.** The audit reasoned about complexity
from the shape of two nested loops and named the wrong thing; the profile named the right
one on the first look, and it was not an algorithm at all. A `-list` on the hot function was
worth more than the complexity argument that motivated opening the file.

### 08/24/26 (2)
**The purity fixpoint stopped rebuilding what never changes.**

`inferImpurity` recomputes every callable's effect on every round, and must: a function
found to allocate early can still gain an io bit from a callee resolved later, so a
"skip the ones already impure" early-out would miss it. What it does not have to recompute
is the **inputs** each body walk needs — the scope frame (a walk of the collector's scope
subtree plus two maps), the `mut` parameter set, and the parameter positions including the
body-level match-alias walk. All three are pure functions of the AST and the scope tree,
both immutable once collection has finished, and all three were being rebuilt once per
round per callable, with the reporting walk asking for them a third time.

They are memoized on `scopeFrames` now, along with the per-method equivalents.

**Measured, because the audit's claim deserved a number.** A new
`BenchmarkCheckPurity_*` runs the pass in isolation over the real prelude:

| | before | after |
|---|---|---|
| Small | 0.664 ms, 12,846 allocs | 0.465 ms, 8,292 allocs |
| Medium | 1.086 ms, 20,957 allocs | 0.799 ms, 13,695 allocs |
| Large | 2.532 ms, 46,370 allocs | 1.856 ms, 29,702 allocs |

~27–30% faster, ~35% fewer allocations. **End to end it is 2–4%**, and that gap is the
part worth recording: a CPU profile of `BenchmarkAnalyze_Large` does not show `CheckPurity`
at all — parsing and collection dominate so thoroughly that the pass is below the sampling
threshold. The isolated benchmark exists because of that, in both directions: a regression
here is invisible end-to-end, and an optimization here should not be sold as a pipeline win.

**What makes it safe rather than merely fast** is that nothing writes to a cached map after
it is built. That holds by construction — every write to a frame or parameter map is inside
its builder — and `TestPurity_IsIdempotent` is the standing check, since a mutated map
would make a second run disagree with the first. `TestPurity_DeepChainStillReachesTheFixpoint`
guards the other direction: the *outputs* still change between rounds, so a memo that
captured an effect rather than an input would report nothing on a chain deeper than one
round can settle.

Four map copies went with it. Three call sites built `funcScope.locals` by copying a scope
frame's key set into a fresh all-true map, because `isLocal` tested the mapped value while
the frame's map stores *mutability* under each name. `isLocal` tests key presence now, so
they share the frame's own map; the fourth copy was the per-body `locals` the previous
commit had already removed.

### 08/24/26 (1)
**A `pure` function could print. Effect inference was two walks, and they disagreed.**

`inferImpurity` asks "what does this body do" once per callable per fixpoint round, and the
question had **two** implementations: `lambdaEffects` for a free function and
`methodEffects` for a trait-impl method, ~200 near-identical lines each — the same `onStmt`,
the same twelve-arm `onExpr`, the same six allocation cases. Each carried a comment saying
the other had to stay in step. One line did not:

```go
// lambdaEffects
} else if method, ok := methodTable.Get(ex); ok {
    found |= impureMethods[method]                  // base effect only
// methodEffects
} else if method, ok := methodTable.Get(ex); ok {
    found |= methodCallEffect(method, ex, …)        // base + the callbacks supplied here
```

Only the second adds the effects of the arguments supplied for the method's *callback*
parameters. So a free function passing an impure callback through a trait method was
inferred **pure**:

```lyra
trait Runner { run: (Self, () -> i64) -> i64 }
impl Runner for Runbox { run = (self, f) => f() + self.n }

let noisy = () -> i64 => { println("noise") 42 }
let mid   = (r: Runbox) -> i64 => r.run(noisy)     // inferred pure
let outer = pure (r: Runbox) -> i64 => mid(r)      // checks clean
```

`outer` promises purity, `lyrac check` exits 0, and running it prints `noise`. `mid` even
drew a **lyra-W018** advising that it be marked `pure` — advice that would have made the
program worse.

**Why nothing caught it.** The *reporting* walk (`exprVisitor`) had used `methodCallEffect`
all along, so the diagnostic machinery was correct and the table it consults was wrong.
Every test asserting "this bad program is rejected" passed, because those tests report
against the direct call. The only thing that finds this shape is a test asserting a program
*two levels up* is rejected — which is what now exists, alongside its companion asserting a
**pure** callback still leaves the caller pure, since the wrong over-correction ("a method
with a callback is impure") would poison every `map`-shaped trait method in the language.

**The fix is that inference is one walk.** `bodyEffects` takes a `callable`: the descriptor
of what the two entry points genuinely differ in — the body's own frame, the capture stack,
the `mut` parameters, the parameter positions, where to record an allocation site, how to
read a declared bound, and how to walk the body. Everything else is shared, so a divergence
now requires editing one walk into disagreeing with itself.

Two details worth keeping. The method walk had *no* `mut`-borrow set, because trait methods
have no `mut`/`own`/`ref` modifier syntax yet; a nil map reads false, so passing nil
reproduces the omission exactly rather than by a special case. And the per-body `locals`
map — a copy of the scope frame's key set, rebuilt every round for every callable — is gone:
`callable.declares` tests key presence directly, which is what `locals` was standing in for.

**The nine-value parameter thread is gone with it.** `impureLambdas`, `impureMethods`,
`callbacks`, `methodCallbacks`, `methodTable`, `boundGroups`, `alloc`, `frames` and
`signatures` were threaded through six functions by hand and then copied field by field into
`purityChecker` so the reporting walk could ask the same questions of the same data. They
are one `inference` struct now, which `purityChecker` **embeds** — so `c.impureLambdas`
reads the one table rather than a copy that could go stale, and the field names did not have
to change.

Verified: the whole standard library and every example in `examples/` still check clean
under the stricter inference, which is the check that mattered — the fix charges *more*
effects, so the risk was rejecting valid code rather than accepting invalid code.

### 08/23/26 (2)
**The type-walk and substitution copies outside `pkg/types`.**

Rule 8's "one answer" table exists because a switch over composite types that exists twice
drifts. Six copies were still outside it, and the sweep found one live bug, one latent one,
and one class of bug made structurally impossible.

**Three walkers deleted outright.**

- `ownership.substituteTypeVars` was a stale copy of `types.Substitute` missing
  `RawPointerType`, `*LambdaType`, `*ConstrainedType` and `RangeType`. It is the copy that
  was *missed* when `types.Substitute` was created: the driver's and the backend's were both
  converted to thin wrappers then, with a comment on each saying why. 73 lines gone.
- `collectGenericNames` in trait dispatch was a copy of `types.CollectTypeVars` with no
  `AnonymousStructType`, `WeakType`, `RawPointerType`, `RangeType` or `*ConstrainedType`
  case, **and this one was a live bug**: `unifyGenericTarget` has always had a
  `RawPointerType` arm, so `impl Describe for ^t` *could* unify — but the walk that reads
  the target's implicit variables found none, so `implTargetMatches` returned false one step
  earlier. The symptom named neither switch: `member access on non-struct type ^i64`. This
  is rule 8's "two switches can disagree about a case neither one names", and the fix is a
  one-line call.
- `mentionsGenericParam` was a third subset, missing five composites. It is now
  `CollectTypeVars` ∪ `CollectTypeNames` — both, because a parameter reaches that code under
  two spellings (a resolved `GenericType`, an unresolved `UnresolvedType` of the same name),
  and collecting the nominal names cannot produce a false positive since a type parameter is
  lowercase by lexer rule and a nominal name is not. **No observable behaviour change was
  found** — the old switch's trailing `params[t.GetName()]` fallback was covering for the
  missing arms. Latent drift, removed before it mattered.

**`ast.BindGenericParams`.** Nine sites built the same positional map from a declaration's
`GenericParams` and a `ParameterizedType`'s `TypeArguments` — in the typechecker, the
ownership pass and the backend — and they disagreed about the bounds guard: five tested
`i < len(args)` inline, four indexed straight in and were safe only because of a
`len(…) != len(…)` check several lines earlier. That is the shape that arrives as a panic
once someone moves the earlier check.

**The retain/drop pair is now one walk** (`backend/llvm/owned_walk.go`). `emitRetainValue`
and `emitDropValue` were ~150 duplicated lines each, identical down to the tag-switch in
their `data` arm, differing only in what they do at a managed **leaf**. They are now
`emitOwnedValue(…, retainWalk)` / `(…, dropWalk)`, so a copy and its death cover the same
fields by construction rather than by two switches happening to agree.

That matters more than the line count. The history in those files is that both copies
lacked `AnonymousStructType` (until 08/08) and both lacked `ParameterizedType` (until
08/07) — they were symmetric *in being broken*, which is the failure a side-by-side reading
cannot find. A missing arm can now only be missing from both walks in the sense of being
absent from the language.

**Equality was deliberately left out**, though it reads as a third copy of the same arms in
the same order. It answers a different question with a different stopping rule — retain and
drop stop *at* a managed value, because its own box owns what it holds, while `emitEqValue`
descends *into* one to compare it — and it returns a value rather than performing an effect.
Nothing breaks if it visits a different set of fields than the other two, and that is what
makes it a different walk rather than a third copy. Folding it in would have parameterized
the stopping rule and the return type to share six lines of dispatch.

**`OwnsManaged` and `SharedMutablePath` share their fold** (`eachComponent`), which yields
each structural component with its field name where it has one. The generic-instantiation
arm is *not* in there and each keeps its own: `SharedMutablePath` needs a `seen` cycle guard
because it keeps looking past a shared value with no writable field, and `OwnsManaged` does
not because a recursive type must break its cycle with a `shared`/`weak` field (lyra-E014)
and `IsManaged` answers those before it can recurse. That difference used to be buried in
forty lines of otherwise-identical arms; it is now the only thing not shared, with the
reason written next to it.

Verified with the full suite and `./asan.sh ./...` green end to end — which the previous
commit is what made possible.

### 08/23/26 (1)
**The hand-copied AST walkers, and the four bugs they were hiding.**

Four passes walked the AST through a private copy of `pkg/ast`'s canonical
`walkStmtChildren`/`walkExprChildren` rather than through the walkers themselves. Every
copy had drifted, and none of the drift was visible as drift — each showed up as a feature
quietly not working in one syntax.

**Why the copies existed, and why they did not need to.** Two of them could not use the
canonical walker as it stood: the collector's constructor reclassification needs to
*replace* a slot, which a visitor cannot do, and the LSP's position lookup needs the
innermost node containing a line/col. Both are ordinary uses of a walker once the walker
offers them, so `pkg/ast` gained `RewriteStmt`/`RewriteExpr` (post-order, slot-reassigning)
and the child walkers were exported as `WalkStmtChildren`/`WalkExprChildren` for the
flow-sensitive passes, which had been calling `WalkStmt` on a node and discarding the first
callback with `if child == stmt { return true }` in four places.

**What each copy had lost.**

- **`constructor_reclassify.go`** (~200 lines, now a 20-line callback) had no case for
  `TupleIndexExpr`, `BitwiseNotExpr` or a deref assignment's *target*, so an all-caps data
  constructor beneath one kept its `const_identifier` spelling: `~to_num(N)`,
  `pair_of(S).0` and `pick(N, &mut x, &mut y)^ = 7` each failed with
  `undefined identifier "N"` — a diagnostic naming a constructor the program declares three
  lines up.
- **`cmd/lyra-lsp/hover.go`** (282 lines, now 87) was three switches. The *expression* one
  was registered in `exhaustive_test.go` and so was in step; the *statement* one never was,
  and sat **eight kinds** behind — `WithStmt`, both destructuring-if forms,
  `LValueAssignmentStmt`, `BreakStmt`, `DestructuringDeclStmt`, `TraitDeclStmt` and
  `TraitImplStmt`. The last two mean hover, go-to-definition, references, rename and
  document-highlight all returned nothing anywhere inside an `impl` body or a trait default
  method: every operator overload and every `Show` impl in every program.
- **`ownership.go`**'s expression switch fell to a `default:` that recorded nothing for
  **twelve** kinds, against a comment saying skipping a node is "emphatically not the safe
  default". Ten are now one multi-type borrow-only case over `WalkExprChildren`
  (`!consume(p)`, `&mut p.name`, `q^`, `a..<consume(n)` and a match guard had recorded
  nothing at all), and `ArrayCompExpr` got a real arm.

**The comprehension was an ASan-confirmed double free.** A comprehension builds a fresh
array, so its result is an owning position exactly as an array literal's elements are, and
it runs once per iteration, so an owning read records a *retain* rather than one transfer
of a reference N slots then hold. `[i in 0..<3 | t]` on a heap string aborted under
AddressSanitizer before the arm existed.

**The one that was left in the default, and why.** `unsafe { … }` is its body and nothing
else, so `a.block(e.Body, needOwned)` is the obvious arm. Writing it breaks every FFI test
in the backend suite: in `let buf = CBuffer { ptr: unsafe { &xs[0] }, len: 3 }`, walking
the block makes `&xs[0]` the last *mention* of `xs`, so Perceus records a last-use drop and
frees the array while `buf.ptr` still points into it — `buf.get(i)` reads zeros. Last-use
rests on the premise that a binding's final mention is its final use, and `&x` is exactly
the operation that breaks it: a raw pointer keeps storage alive without counting as a
reference to it. **This is a missing rule, not a missing case** — taking an address must
pin the binding against last-use optimization, the way `loopUsed` already excludes a
loop-referenced one in `computeLastUse`. Until that exists, recording nothing inside an
unsafe block is what makes raw pointers work, conservatively, by deferring every drop to
scope exit. The reasoning is in the `default:` comment so the next person does not
rediscover it through a use-after-free.

**The same reasoning is why the default was not simply made a borrow-walk of children**,
which is the obvious mechanical fix. Borrowing a comprehension's result records a last-use
drop on `[i in 0..<2 | t]`, freeing `t` *inside* the loop while the array keeps both
pointers — turning a latent double free at scope exit into an immediate one. A generic
traversal can say what the children are; only a per-kind arm can say whether a position
owns.

**Two mirrors were retired rather than registered**, which is the better outcome: there is
no longer a second list of node kinds to fall behind. `rewriteExprChildren`/
`rewriteStmtChildren` are registered in their place, since a rewriter is the writing half
of the same traversal and owes a case for exactly what the reader descends into.

### 08/22/26 (6)
**A specialization's type argument resolves in the module that asked for it.**

`let m = Some(card); m.unwrap_or(other)` did not lower when `struct Card` had no `pub`:
`llvm: unknown named type "Card"`. Adding `pub` fixed it, which is exactly what made the
bug look like it was about generics rather than about visibility.

**Two modules, one slot.** `declareSpecialization` enters the module of the *generic
function* before lowering its signature, and monomorphize.go explains why: a named type in
that signature is the callee's own, keyed under the callee's module. But a **type
argument** is the caller's, and a private declaration is keyed `<module>::<name>` (rule 4).
`l.currentLoc` is one value, so entering either module leaves the other's names
unresolvable — and the failure only shows for a *private* type, since a `pub` one has a
bare key that resolves from anywhere.

The fix is to stop asking one location to answer both questions. `Instantiation` now
carries the **site** it was requested from, and `lookupNamedType` tries that module's key
when the current one misses. Second, not first, so a name the callee's own module declares
keeps winning and the change can only turn an error into a success; outside a
specialization the site is the zero Location, which keys as a bare name and finds nothing
the lookup did not already try.

**The composed path needed one more line, and it is the interesting half.** A generic
calling a generic records a *template* — bindings written in the enclosing body's own type
variables — which the driver composes into a real specialization. That inner call sits
inside a library's module, so the template's own site is useless; the concrete types come
from the **outer** instantiation, resolved where *it* was requested. So the site travels
outward with the types it explains: `composed.Site = current.Site`. Without it,
`get_or(Some(card), other)` still failed while the direct call worked.

`Instantiation.Substituted` was also dropping **`Disc`** — the discriminant that tells two
receiver-keyed overloads apart. Composition therefore produced specializations keyed
without it, which is precisely the collision `Disc` was added to prevent (`map<t=i64>`
naming both the `Maybe` overload and the array one, sharing one emitted function). Not
observed in the wild, and fixed in passing while adding `Site` to the same literal.

**What it did *not* fix, found while testing it**: `Some(1).unwrap_or(0)` does not parse at
all. A *binding* receiver works (`let m = Some(1)` then `m.unwrap_or(0)`) and a literal
receiver works (`(1).wrapping_add(2)`), but a **constructor-call** receiver splits at the
underscore — `` `let _or` must be initialized `` — which reads as the juxtaposition rule
taking `Some` as applied to `(1).unwrap` and leaving `_or(0)` to begin a statement. That is
why every program written so far has worked: the idiom that fails is the one nobody had
written down. Confirmed pre-existing against HEAD and recorded as grammar work in
`todo.md`.

### 08/22/26 (5)
**`min`/`max`/`clamp` in the prelude, and the three gaps writing them found.**

Generic over `where t: Ord`, with a `self` receiver so `a.min(b)` and `min(a, b)` are the
same call. Three private copies are gone with them: `std/tui/style.lyra`'s, and one each in
`examples/mandelbrot_tui.lyra` and `examples/tui_viewer.lyra` — which is what the entry
meant by "every program that fits something to a window writes its own".

**The primitive `Ord` impls had to come first**, ten integer widths plus `rune`, and they
are `math.lyra`'s arithmetic impls exactly: they exist **so a bound can be satisfied**, not
so a call can dispatch. `3 < 5` is a machine compare whatever a program declares, so the
body's `self <=> other` is the operator and not recursion. Without them `where t: Ord` is
undemandable of a number, which is to say `min` is unwritable for the types that want it
most.

**Floats are excluded, and that is the recorded decision being respected rather than
re-opened.** `<=>` refuses them because NaN is neither less than, equal to nor greater than
anything and a three-way answer has to pick one; the partial-ordering design is open in
`todo.md` and explicitly *"deferred rather than guessed"*. An `impl Ord for f64` would be
that guess, made once here and inherited by every generic that sorts or ranks. So
`min(1.5, 2.5)` is a compile error, and on concrete floats `if a < b { a } else { b }` is a
machine compare that works today.

**Ties are split**: `min` keeps `self`, `max` takes `other`. On a scalar that is
unobservable, which is exactly why it would be got wrong and never noticed — so it is
asserted on a type ordered by one field while carrying another. The reason is that
`min(a, b)` and `max(a, b)` between them should still name *both* values; had both kept
`self`, the pair would answer `a` twice and `b` would vanish. Rust's `Ord::min`/`max` split
it the same way.

**`clamp` traps on `lo > hi`**, which describes an empty range and so has no nearest value
to answer with. Returning either bound would pick one arbitrarily and hide the mistake that
produced the reversed pair. The message is a constant, so it stays `noalloc`.

### Three gaps, none of them fixed here

Each is recorded in `todo.md` with its repro. None is caused by this change; all three are
what writing an ordinary generic prelude function walks into.

- **An untyped literal argument defaults before the type variable is solved.**
  `count.min(80)` on a `u8` reports *"cannot infer type variable t"*, because
  `solveTypeVars` promotes the literal to `i64` before unifying and the two arguments then
  bind `t` inconsistently. It is documented behaviour with a workaround (`u8(80)`), and on
  `i64` nothing is needed — but `min`/`max`/`clamp` put it in front of every program using
  a narrow width. The mechanism to fix it is *in the same function*: an un-annotated lambda
  is already deferred to a second pass because it cannot be inferred until it knows what is
  expected of it, and an untyped literal is that shape exactly.

- **A method call on a bare type parameter does not reach UFCS.** Inside `where t: Ord`,
  `best.max(x)` asks for a `where` bound that is already written, while `max(best, x)` is
  the same call and compiles. UFCS dispatches on the receiver's type and a type variable
  has none.

- **A prelude generic at a *privately* declared struct does not lower** —
  `m.unwrap_or(other)` on a `Maybe<Card>` dies with `llvm: unknown named type "Card"`, and
  adding `pub` fixes it. `declareSpecialization` enters the module of the generic function
  so its signature's names resolve, but a *type argument* comes from the caller's module
  and is keyed `<module>::<name>` when private, so entering one module cannot serve both.
  The `pub` in this session's tie-breaking test is that bug and is commented as such,
  because a workaround left unlabelled reads as a preference.

### 08/22/26 (4)
**`lyra-E064` — an alias applied to an operand says which spelling was wanted.**

`CULong(n)` reported *"CULong: not a tuple type"*. That is the **parse** being reported
instead of the language: `Name(x)` parses as a tuple literal, so the message named a
construct the author did not write, about a type that is not a tuple and was never going
to be. lyra-E044's own comment records replacing exactly this wording for `newtype` on
08/12 — *"naming the parse rather than the language"* — so the same failure arrived twice
by different routes, which is what makes it a shape rather than a typo.

**Two messages, because there are two fixes**, and that is the whole content of the change:

- Where the alias names a type a conversion can name, a width change is the only thing the
  wrapper could have meant, so the message hands over that spelling — *"an alias is
  transparent, so a u64 value already has type CULong. To convert, write `u64(...)`"*.
- Where it does not — an alias for an array, a function type — there is no conversion to
  offer and none is needed, so it says the operand already has that type and to drop the
  wrapper. Naming `[]i64(…)` would be worse than saying nothing: it does not parse.

`conversionSpellingFor` asks the two functions `inferTypeConversion` itself asks
(`numericPrimitiveByName`, `identityConversionTargetByName`), so a message here cannot
name a spelling the conversion path would then refuse. That is the same one-answer
discipline the rest of the compiler follows, applied to a diagnostic's *advice* rather
than to a check.

The juxtaposed form `CULong 5` reaches the same arm, since the collector erases it into
the same node — asserted, because falling through would produce a second and worse
message for the spelling that is arguably more natural to reach for.

**What it does not touch**, checked rather than assumed: a `newtype` still constructs
(`Cents(150)`), and an alias for an anonymous tuple still accepts `P(1, 2)` and stays
transparent — the result is assignable to `(i64, i64)`, so that spelling is an extra way
to write a tuple rather than a nominal type an alias smuggled in.

### 08/22/26 (3)
**`examples/zlib.lyra` uses `CULong`, and the alias could not reach a pointer until it
did.**

zlib's API is `uLong` — `unsigned long` — in five places: `crc32`'s seed and result, both
`compress`/`uncompress` length parameters, and the `uLongf *destLen` in/out. The file had
them written `u64` with a comment saying so and pointing at a `CULong` that did not exist
yet. It exists, so they are written `CULong`, and the comment now says why rather than
apologising for not.

**The alias is the boundary's, not the program's.** The three Lyra wrappers convert at
their edge — `checksum` answers a `u64(…)` of what `crc32` returned — so the
target-dependent type stops at the wrapper and the rest of the program is in Lyra's fixed
widths. That is the same division `std.ffi` draws everywhere else, and it is what makes a
port local: the sites that have to change are the ones that mention `CULong`.

**And it did not work, which is the part worth recording.** `&mut room` on a
`room: CULong` binding produced `^mut u64`, and the parameter wanted `^mut CULong` —
different types, so the call was rejected. `resolveTypeWith` had no `RawPointerType` case,
so the pointee was left an unresolved *name*; a pointee is invariant, so that is not a
near-miss but a flat rejection.

The function's own doc comment describes this exact failure mode four times over — a
named element in `[3]Node`, a `weak Node` referent, a type argument in `Box<Pt>`, a
function type's parameters — each case added after an unresolved name inside a type made
two spellings of one type compare unequal. Raw pointers landed 08/18 and were never added
to it. It stayed invisible because a raw pointer's pointee is almost always written as a
primitive, where there is no name to resolve.

**Second instance in one day**, after `SizeAndAlign`, so the sweep was worth running rather
than fixing one more. It found no others: the type-variable walks (`Substitute`,
`CollectTypeVars`, `mentionsGenericParam`, ownership's `substituteTypeVars`) already handle
pointers, because generics over `^t` landed *with* the feature on 08/19 — verified by
running a generic whose only type-variable occurrence is inside a pointer. The
container-shaped switches (`elementType`, `isByteArray`, `iterableElementType`) have no
pointer case and are right not to.

So the rule 8 lesson is sharper than "grep for the type kind": the family that matters is
the walks that must reach **every** composite — resolution, layout, substitution,
retain/drop — and not every switch that happens to mention one.

**One rough edge left standing.** An alias has no constructor, so `CULong(n)` reports
*"CULong: not a tuple type"* — the juxtaposition path's message, and nothing a reader can
act on. The spelling is `u64(n)`, and the diagnostic should say so.

### 08/22/26 (2)
**`with_cstring`, `with_cstrings`, `CLong`/`CULong` — `std.ffi` is complete, and two
compiler bugs it walked into.**

Three items, and the interesting part of each is where it landed differently from the plan
`todo.md` recorded.

**`with_cstring` is not marked `unsafe`.** The entry assumed it would be — it hands a raw
pointer to a callback — and writing it the marked way showed why that is wrong. `unsafe`
does not cross a lambda boundary, so a marked `with_cstring` costs an outer block for the
call *and* an inner one for the foreign call inside the callback:

```lyra
unsafe { path.with_cstring((p) => unsafe { open(p, 0) }) }   // marked: two blocks
path.with_cstring((p) => unsafe { open(p, 0) })              // unmarked: one
var buf = path.cstring(); unsafe { open(buf.data(), 0) }     // unscoped: one, plus a rule
```

The unscoped spelling — the one this function exists to replace — costs one block. So
marking it would have made the *safer* shape the more ceremonious one, and a safer shape
that costs more is a shape nobody reaches for. Unmarked it costs exactly what the unscoped
form costs and removes the lifetime obligation, which is the entire trade.

The rule that falls out is statable and now written down: **`unsafe` marks handing a
pointer out to keep (`data`), not lending one for the duration of a call.** It is the same
line `CBuffer.get` sits on — an obligation discharged rather than passed on.

**And the entry's claim for the shape was too strong.** "The only shape where the pointer
cannot outlive the buffer" is not true here: `s.with_cstring((p) => p)` compiles, because
Lyra cannot say that `f` may not keep what it was handed. Closing that needs a lifetime
system. What holds meanwhile is that the escaped pointer cannot be *used* without `unsafe`
at the use site — `p^`, `p.offset(n)` and a foreign call are each marked where they are —
which is where this language marks memory-unsafety everywhere else.

**`with_cstrings` exists because nesting was broken, not because it was ugly.** The entry
worried that a two-string call "nests badly". It did not nest at all: a closure could not
capture a `^u8`, so the nested form failed in the backend. With that fixed it nests fine
and reads two levels deep, which is what the flat form is for — a free function rather
than a method, because neither string is the receiver.

**`CLong`/`CULong` are aliases, not newtypes**, reversing what was recorded, and the
program is the argument. A newtype demands `CLong(n)` at every crossing and `i64(r)` at
every result, on every target, forever — to prevent a mixup that is either harmless (LP64,
where the two coincide) or already caught (LLP64: change the alias to `i32` and every site
passing an `i64` fails to compile, naming `CLong` in the message). The nominal identity is
paid for everywhere and buys nothing the width check does not already give. The precedent
is the prelude's `Index`/`Length`. The rule for adding a third is on the declaration: an
alias earns its place when a C type's width differs across targets Lyra intends to support
— `wchar_t` next, `int` and `size_t` never.

### The two bugs, both one missing case in a switch

**`SizeAndAlign` had no `RawPointerType` case.** The symptom was never "a pointer has no
size" — it was *any aggregate containing one* failing to lay out. `std.ffi`'s own `CBuffer`
could not be captured by a closure (`cannot size captured binding`) or held in a `[]T`
(`cannot size dynamic array element type`), and a bare `^u8` could not be captured at all,
which is what every scoped-callback FFI shape is. Three lines to fix, and it had been there
since raw pointers landed on 08/18 — invisible because the pointer tests all kept their
pointers in locals.

The lesson rule 8 does not yet state: **a new *type* kind pays the same tax as a new
declaration kind.** `pkg/ast/exhaustive_test.go` enumerates declaration kinds and the
switches over them; nothing enumerates the switches over composite *types*, and this is the
family `emitRetainValue`/`emitDropValue` and `mentionsTypeVar` also belong to.

**The unused-import walk was blind to a signature hanging off a declaration** rather than
off a LambdaExpr — an `extern`'s, and a `trait` method's. So `import std.ffi.{ CLong }`
beside `unsafe extern pure labs: (CLong) -> CLong` — the only construct that can use it —
warned that the import was unused. That is advice to delete an import the program cannot
compile without, which is precisely the failure the function's own doc comment describes
having fixed for struct literals and signatures on 08/14. `collectRefsByFile` is now
registered in `declarationConsumers`, the eleventh entry; the test could not have caught it
before, since it cannot find a switch nobody registered. Verified by deleting the case and
watching it fail.

**What is left at the boundary** is no longer a `std.ffi` gap: the extern tests in CI, and
a lifetime system if the `(p) => p` escape ever justifies one.

### 08/22/26
**`xs.data()` — the buffer direction of the C boundary, and the E011 hole it found.**

The last thing `examples/zlib.lyra` spelled by hand. Every "pointer plus a length" call
wants a buffer's base address, and the only way to say it was `&mut xs[0]` — an index the
author does not mean, whose bounds check is the only reason it happens to be safe on an
empty array. `std.ffi` now has the name:

```lyra
pub let data     = unsafe pure noalloc (self: []t)     -> ^t     => { … &self[0] }
pub let data_mut = unsafe pure noalloc (self: mut []t) -> ^mut t => { … &mut self[0] }
```

**It cost the compiler nothing**, which is the outcome the FFI design was aiming at: a
generic function over `[]t` returning `^t`, monomorphized by the machinery that already
existed. Raw-pointer generics landed 08/19 (`^mut T` assignable to `^T`, the unifier's
pointer case), and this is the first thing written on top of them that a program actually
reaches for. No builtin, no backend arm, no new diagnostic — the `random_seed`/`Rng`
division applied once more, at the boundary.

**Two functions rather than one, because `&x` and `&mut x` are two spellings.** Mutability
is written where the pointer is taken, and a method call has nowhere to put the word. The
receiver carries it instead: `data_mut` takes a `mut` receiver, which is the rule
`xs.push(v)` and `xs[i] = v` already obey, so a caller holding an immutable array cannot
reach the writable pointer. The alternative — one function whose result's mutability
follows the receiver's — would make `^T` and `^mut T` depend on how the binding two lines
up was declared, which is precisely the thing the two spellings exist to make local.

**An empty array traps with its own message**, not with the index check's. `&self[0]`
already trapped, so the safety was there; what was wrong was the sentence, which named a
`[0]` the caller never wrote and sent them looking for an index bug. One redundant compare
buys a message that describes what happened. The message is a constant, so `data` stays
`noalloc` — and it is `pure noalloc`, since taking an address is not an effect.

**`unsafe`, and that is where the real finding was.** Marking it follows `cstring_len`:
nothing in the body dereferences anything, and the danger is entirely in what the caller
does next — a pointer into a live `[]T` goes stale on the next `push`, Rust's `as_ptr`
hazard exactly. But writing the first unsafe function with a `self` receiver revealed that
**`lyra-E011` did not apply to the method form at all**:

```lyra
let escapes = (xs: []u8) -> ^u8 => data(xs)     // error: requires an `unsafe` block
let escapes = (xs: []u8) -> ^u8 => xs.data()    // checked clean
```

The UFCS rung desugars the receiver into `Arguments` and then calls `inferLambdaCall`
directly — it is the bare call by that point — but it did not make the check the bare-call
rung makes two hundred lines away. So the method spelling was a way around the keyword.
Latent since UFCS landed 08/03 and unobservable until now: every unsafe declaration in the
language was either an `extern` (whose parameters are C's, never named `self`) or
`cstring_len`, which takes a `p: ^u8`. Nothing could be called method-style, so nothing
demonstrated the hole.

This is hazard 8 in a **resolution ladder** rather than in a switch, and it is worth
naming as such: the ladder's rungs are not cases of one `switch` and no exhaustiveness
test can compare them, but they are still copies of one question — *what does this call
call, and what must be true to call it?* — and a rung that answers the first half without
the second is exactly the silent divergence the rule is about. Both UFCS sites (the plain
one and the newtype fallback) now call `requireUnsafeCall`.

**`examples/zlib.lyra` is written through it**, which is the proof that matters: `crc32`
reads `xs.data()`, `compress`/`uncompress` write through `dst.data_mut()`, and the
round-trip still matches Python's answer byte for byte. One rename went with it — the local
`data` in `main` became `original`, since the flagship FFI example should not shadow the
function it is demonstrating.

**Dynamic arrays only**, and the reason is a feature rather than an oversight: a `[N]T`
carries its size in its type, so it cannot be a generic parameter until const generics
exist. A fixed buffer still writes `&xs[0]`. That is the first concrete thing on the const
generics entry's account rather than a hypothetical one.

### 08/20/26
**The checklist that adding a node kind never had** — `pkg/ast/exhaustive_test.go`.

Rule 8 has said "grep for the switches over it" for months, and it kept not working, because
grepping requires knowing which switches exist and nothing enumerated them. Two node kinds
in three days made the cost concrete: `ExternDeclStmt` missing from **ten** switches over
declaration kinds, and `UnsafeBlockExpr` from the LSP's expression walker.

The test has two halves, and neither is reflection: the question is about *code*, and
reflection can say what fields a node has and never what a switch does with it. So it parses
the switches with `go/parser` and compares case sets.

- **Mirrors.** `walkExprChildren`/`walkStmtChildren` are this package's canonical answer to
  what a node's children are. A registered mirror — today the LSP's `findInChildren` — must
  cover every case they have, in both directions, so either falling behind fails.
- **Declaration consumers.** Eight switches must cover every kind in `declarationKinds`,
  and that list is guarded in turn by the language's own rule: **a statement node with a
  `Doc` field is a declaration**, because documentation attaches to declarations and to
  nothing else. So a new declaration node fails at the list first, with a message saying to
  add it there and then to the consumers.

**An omission is a bug; an exclusion is a claim.** Every excused kind carries its reason in
writing beside it — and the most useful entry is `declIsPublic`'s, which excuses
`TraitImplStmt` and `ModuleDeclStmt` on the grounds that its `default: return true` is right
for them. `ExternDeclStmt` took that same default and was **wrong** to, which is how two
modules each declaring `extern abs` collided on a program-wide name. The test makes the
difference between those two an argument someone has to write down.

**It earned its keep before it was finished.** The mirror half found **thirteen** more
expression kinds missing from the LSP walker — both loop forms, ranges, comprehensions,
`??`, `t.0`, `~x`, compose, guard, the async trio. Loop bodies are not an exotic construct;
that is most code in most programs, and hover, go-to-definition and rename had been silently
dead in all of it. Fixing the loops then exposed a subtler bug in the scope half: iterating
`e.Body.Statements` finds every scope *nested* in a loop and misses the one the loop itself
introduces, so the loop variable was the single name inside a loop that could not be
resolved. `scopeInExpr(e.Body, …)` is the fix — the body block, not its contents.

Both failure modes were verified by breaking the code on purpose: deleting one case from
`findInChildren` fails the mirror test naming that case, and adding a scratch declaration
node fails the completeness test naming the file to edit.

**What it does not do**, said plainly in the file: it cannot find a switch nobody
registered. Adding an entry when you add a *consumer* is still a manual act. What it buys is
that adding a *node kind* is not.

### 08/20/26
**The rule-8 sweep, worked.** All six places `ExternDeclStmt` was missing from a switch
over top-level declaration kinds, plus a seventh found by testing the sixth.

| where | was | now |
|---|---|---|
| `docgen.declFor` | `--private` omitted every extern | its own **Foreign functions** section |
| `documentsymbol` | absent from the outline | a Function, selected at its *name* |
| `workspacesymbols` | not findable by symbol search | indexed, located at its name |
| `semantictokens` | no highlighting (two switches) | a function, at the declaration and every use |
| `rename` | **corrupted the source** | declined, with a reason |
| `definition` | landed on the `@link` line | lands on the name |

**Two of them were not "add the case".**

`rename` was worse than missing. `namedNameLoc` had no extern case, so a rename anchored
on a *usage* fell back to the declaration's **start** — the `@link` or `unsafe` token —
and spliced the new name over the first characters of that line. But the right fix is not
to make renaming work: **an extern's name is the C symbol**, so the other half of the
declaration lives in a library this compiler did not build, and renaming the Lyra side
would emit `declare @newName` for a symbol nobody defines. It is declined, on the rule the
cross-file case already follows — a rename that cannot be carried out completely should
not be carried out partially. If externs ever gain a `@symbol("…")` attribute to decouple
the two names, the check comes out.

`definition` worked in the sense of returning something, and returned the wrong line.
Instead of a case it now calls `namedNameLoc`, which rename.go already owns — one answer
to "where is this declaration's name" rather than a second copy that can drift.

**And the seventh, which is the one that matters.** Testing go-to-definition on an extern
means testing it inside `unsafe { … }`, because a call to an extern requires one — and
nothing worked in there at all. `findExprAtPos` (hover.go), which hover, definition, rename
and document-highlight all start from, had no case for `UnsafeBlockExpr`, `AddressOfExpr`
or `DerefExpr`; `scopeInExpr` (definition.go) had none for `UnsafeBlockExpr` either, so
even a found expression resolved its name in the wrong scope. Since raw pointers and FFI
*both* require the block, that is every editor feature dead across the whole of a
program's unsafe code — since 08/18, unnoticed, because the symptom is an editor doing
nothing rather than doing something wrong.

`DerefAssignmentStmt` was a third: it looked only at the value, so `p` in `p^ = v` — the
pointer being written through, the one name in the statement — could not be resolved.

**What the sweep is evidence for.** Ten switches for `ExternDeclStmt`, four more for
`UnsafeBlockExpr` and its siblings, none of them sharing a package: adding a node kind has
no checklist, and the cost is paid weeks later in features that silently do nothing. The
hazard-8 entry names the sweep now; the honest fix is a test that enumerates the kinds and
fails when a new one appears, which is not written yet.

### 08/19/26
**`std.ffi`'s `cstring` — a C string is a plain `[]u8`.**

**The out direction is a plain `[]u8`** — encode, check for an interior NUL, `push(0)` —
with the pointer taken at the call site. That is option A of four, and the survey is why
it won rather than a preference:

- **The shape it is accused of does not compile.** `&"x".cstring()[0]` is `lyra-E059`,
  *cannot take the address of a temporary*. That is precisely the bug
  `CString::new(…).as_ptr()` produces silently in Rust — common enough that Clippy carries
  a dedicated lint for it — and Lyra's type checker refuses it outright. A wrapper type
  adds nothing there.
- **A struct storing the pointer is worse, and measurably.** A `[]u8`'s elements live
  behind a pointer *in* the box, so growth reallocates them: `CStr { bytes, ptr }` read
  104 before a thousand pushes and 6 after, with the struct holding it perfectly alive.
  Recomputing at the call site cannot fail that way.
- **The wrapper that would genuinely help is not a wrapper.** Every language shipping a
  `CString` also ships a *scoped* form and documents it as the default — Swift's
  `withCString`, Haskell's, C#'s `fixed`. That is worth adding; a name around a `[]u8` is
  not a step toward it.
- **A newtype was tried and is a worse A**: transparency covers methods but not indexing,
  so `cs.len()` resolves and `cs[0]` is *cannot index into type CString*, and getting the
  pointer needs a read-out binding at every call site. Zig's sentinel slices (`[:0]const
  u8`) are what that was reaching for, and they are a type constructor rather than a
  nominal wrapper.

None of the four closes the actual hole — `let escapes = (xs: []u8) -> ^u8 => &xs[0]`
compiles — so this is a convention, and the declaration says so.

**An interior NUL traps** rather than returning a `Maybe`. C cannot represent one, so the
string would arrive truncated at that byte: the silently-wrong answer this language traps
for everywhere else. Rust returns an error here; a trap is the call `split("")` already
makes, for an argument with no meaningful answer rather than for input a program did not
choose.

The backend's `llvm_pointer_offset_test.go` gave up its `std.ffi` half to a new
`llvm_ffi_test.go`, on the lesson from `rounding.go` the same afternoon: a file named for
its first tenant stops describing its contents at the second.

### 08/19/26
**A closure can call a foreign function.**

Testing a scoped `with_cstring(s, f)` turned up
`let f = (p: ^u8) -> u64 => unsafe { strlen(p) }` failing to lower — *no type recorded for
captured binding "strlen"*. `captures.globalNames` switches over the top-level declaration
kinds a lambda body can reach without capturing, and had no case for `ExternDeclStmt`, so
the extern's name was collected as a **free variable**: nothing records a type for a
binding that is not one, and the backend failed on a program the front end had checked
clean. One line.

**That is the fourth switch over top-level declaration kinds to be missing this same
node**, after `declIsPublic`, `attachDoc` and `docOf`, and a sweep found six more (docgen
and five LSP surfaces, all editor-facing rather than miscompiles — listed in `todo.md`).
Ten instances of one omission, in ten files sharing no package, is not ten oversights: it
is that adding a declaration kind has no checklist. Hazard 8 now names the sweep.

`pkg/ast/walk.go` was checked and is correctly silent — it has no extern case either, but
an extern has no child statements or expressions, so there is nothing to descend into.

### 08/19/26
**`s.encode_utf8()` — a string's bytes as a `[]u8`.** `decode_utf8`'s inverse, and a
builtin for the mirror-image reason: **nothing in the language could read a byte out of a
string.** `byte_len` measures, `byte_offset` maps a rune position to a byte one,
`compare_bytes_at` compares — none of them reads, and `s[i]` is a rune. So the bytes a
string already holds were reachable only by re-encoding each rune by hand, which is a
UTF-8 encoder written in user code to recover what was there all along.

**Dynamic rather than `[N]u8`**, because the length is a run-time property of the string
and a fixed size could not be written down — which also makes the result `push`-able, and
that is exactly what a NUL-terminated form needs.

It copies, and here the copy is *load-bearing in the other direction* from `decode_utf8`'s:
that one copies because a box's header sits at its start, so a string cannot point into an
array's buffer; this one copies because a `[]u8` is mutable and a string is not, so sharing
the bytes would make `b[0] = 65` a way to write through an immutable value. A test mutates
and grows the result and checks the source string is untouched.

Two allocations, which is what a `[]u8` always costs — `dynArrayAlloc`'s fixed box plus a
malloc'd buffer — with the payload memcpy'd in. Element type i8 and stride 1, so the byte
length *is* the element count and no scaling appears anywhere.

Ownership needed nothing again, and for the same reason `decode_utf8` did not: the
signature is recorded on the MemberExpr and its return carries no `ref`/`mut`, so
`isOwnedReturn` answers *owned* and the array gets its ordinary drop glue. Confirmed in the
emitted IR rather than assumed — `lyra_rc_release` with `lyra_drop_1_DynamicArray_u8_`.

What this leaves for `CString` is a *shape* decision rather than code: three lines of Lyra
(encode, `push(0)`, `&mut bytes[0]`), but the pointer must not outlive the array and
nothing in the language enforces that, so the type is where the reminder has to live.

### 08/19/26
**`bytes.decode_utf8()` — a string built from bytes.** The gap the zlib example exposed
from the other side: reading a foreign buffer produced a `[]u8`, and turning one into a
`string` had no spelling but `s = s ++ "${rune(b)}"` in a loop.

**It is a builtin because it has to be**, which is the `read_line` rule rather than the
`parse_i64` one: it allocates a ref-counted string box and copies into it, and
concatenation is the *only* string construction Lyra code has. So the loop was one
allocation per rune, each copying everything before it — a shape whose cost is easy to
assert and worth measuring instead:

| bytes | `++` loop | `decode_utf8` |
|---|---|---|
| 50k | 0.06s | 0.02s |
| 100k | 0.24s | 0.02s |
| 200k | 0.70s | 0.02s |
| 400k | 1.44s | 0.02s |

Eight times the input for twenty-four times the time, against no change at all. (The
growth flattens above 100k rather than staying cleanly quadratic — allocator and bandwidth
effects — so the honest claim is *superlinear and unbounded*, not a clean n². The
structural fact is exact either way: n allocations totalling O(n²) bytes.)

**Named for the interpretation, not the destination.** `to_string` on a byte array is
ambiguous with *rendering* it — `[104, 105]` as text is "hi" or "[104, 105]" depending on
what the reader assumed, and both are things programs want. `decode_utf8` says which, and
names the inverse that will read bytes back out.

**It does not validate**, and that is a decision rather than an omission. `read_line`
already builds a string from libc's bytes without validating, and `lyra_utf8_count` counts
non-continuation bytes rather than checking them, so malformed input yields a string whose
rune count disagrees with what it holds. Adding a `Maybe<string>` here would give the
language two different answers to one question while the older one stayed wrong; one
unvalidated answer is worse in isolation and better as a whole, and both are fixed by the
same change to that counter.

**Both array flavours**, because which one a byte buffer is depends on how it was written
rather than on what it holds — `[104, 105]` is a `[2]u8` and the same literal under a
`[]u8` annotation is dynamic. `byteBufferOf` is where they differ: a dynamic array's
buffer is a load and its length a field read, while a fixed one *is* its elements and has
no address until `argumentAddress` takes one.

The copy is not avoidable, and is the reason rather than the cost: a box's header sits at
its start, so a string cannot point into an array's buffer — the same fact that makes
`slice` copy. A test pins the consequence, mutating and growing the array afterwards and
checking the string did not follow.

Ownership needed nothing, which was worth checking rather than assuming (hazard 15). The
typechecker records the builtin's signature on the MemberExpr, and its return carries no
`ref`/`mut`, so `isOwnedReturn` already answers *owned* — which is why `slice` has no
entry in `calleeIsOwningBuiltin` either. That list is for builtins called as free
functions, which have no signature to read.

`std.ffi` gains `CBuffer.decode_utf8()` — the same name for the same operation on the same
bytes, told apart by the receiver — and `examples/zlib.lyra` reads zlib's version string
through it instead of interpolating rune by rune.

### 08/19/26
**`cstring_len` is Lyra, not a binding to `strlen`.** The question that produced it was
whether `unsafe extern pure strlen: (^u8) -> u64` belonged in a shared C-bindings module,
and the answer turned out to be that it should not be an extern at all.

Two facts settle it. An extern **cannot be exported** — there is no `pub extern`, by the
rule landed the same morning — so a bindings module could only ever export Lyra wrappers.
And scanning for a zero byte became **expressible in Lyra** the moment `p.offset(n)` did,
which puts it on the `parse_i64` side of the division this language keeps drawing: the
builtin is only what genuinely cannot be written here. Writing it in Lyra settles two
questions rather than relocating them — whether C's return is `u64` on this target, and
which library a `@link` would name.

It is **`unsafe`**, and the contrast with `CBuffer.get` is the design in miniature.
`cstring_len` *is* the promise — "there is a zero byte somewhere ahead" — and nothing can
check it; `get` needs no marking because that promise was made once, at construction, and
every read after it is checked against the length. A test asserts both directions against
each other.

The cost is real and worth stating: libc's `strlen` is hand-vectorised and this is a byte
at a time. The prelude's `index` is a naive scan on the same reasoning — a real guarantee
wants a `memmem`-shaped builtin, and until there is one, honest and slow beats an ABI
claim nobody verifies.

The general form of the question — a `std.libc` — is refused in `todo.md`, along with the
shape that does work: a per-library binding module, which the design already supports with
nothing added.

**`^mut T` is assignable to `^T`, and a generic over a pointer is callable.** Two separate
gaps, found together because the first one's todo entry said to check what else it reached.

The assignability half is one rule in `isAssignable`: dropping the permission to write is
safe — the two are the same machine value and `^T` can do strictly less — where adding it
is the hole `lyra-E061` exists to close. It is one-directional, the pointee stays
invariant (`^Meters` to `^i64` would let a write through the second land in storage the
first names), and `TypesEqual` still tells them apart, which is the same split the
effect-bound rule draws: identity is a different question from what may be passed.

**The downgrade is real, not cosmetic**, which is what makes it safe rather than merely
convenient: a binding annotated `^T` takes that type, so `down^ = 7` is still refused
however writable the pointer it came from was. It reaches four positions at once — a `let`
annotation, a struct field, an argument and a return — because they are one rule, and it
was refused in all four before.

**Then the part that was hiding behind it.** Checking the argument case turned up
`let first<t> = (p: ^t) -> t` reporting *"cannot infer type variable t from these
arguments"* — for a plain `^u8` as much as a `^mut u8`, so nothing to do with mutability.
`unifyGenericTarget` is a switch with a case per composite kind and had none for
`RawPointerType`, so `^t` never bound anything. Hazard 8, in the switch that decides
exactly this question.

Mutability is deliberately **not** matched there. Unification's job is to solve `t`;
whether the argument may then be passed is assignability's, checked afterwards against the
substituted parameter. Matching `IsMut` in the unifier would refuse `^t` a `^mut u8` before
that rule ever ran, and would refuse it *silently* — as an inference failure naming the
type variable rather than the mismatch it actually is. With the split, `poke(&n, 9)`
against `(p: ^mut t)` reports "cannot assign ^i64 to ^mut i64", in the substituted types.

**And behind *that*, a second walker.** Fixing the unifier got `t` solved and the call
still failed, now against an un-substituted `^t`: the typechecker had its own
`substituteGenerics` beside `types.Substitute`, and only the latter had a raw-pointer
case. The two comments are the whole story — the local copy said it walked "the handful of
compound type shapes a data constructor's payload realistically takes today", and
`types.Substitute` said it exists because "a switch over composite types that exists twice
drifts". So the durable fix rule 8 actually asks for: the local name is now a one-line
call to the real walker, and `types.Substitute` is listed with the other single answers.

Three bugs in one afternoon, each hidden by the one above it: with no unifier case the
call never reached an assignability question, and with no substitution the solved variable
never reached the parameter.

### 08/19/26
**Pointer arithmetic, and `std.ffi`'s first type.** `p.offset(n) -> ^T` is the language's
only pointer arithmetic, and `std.ffi`'s `CBuffer { ptr, len }` is the checked thing built
over it. Together they close the gap the zlib work found the same day: a foreign `char*`
can now be walked, and the walk traps on a bad index. `examples/zlib.lyra` reads zlib's
version string through it.

**`p[i]` was the obvious spelling and is the wrong one**, which is the decision worth
recording. It is `xs[i]`'s spelling with none of `xs[i]`'s bounds check — in a language
whose thesis is that two things behaving differently must not look alike, and inside an
`unsafe` block, where most of the code is ordinary and `p` and `xs` are just names. A
method keeps the rule statable — *pointer arithmetic is a named method, never an
operator*, replacing "there is no pointer arithmetic" — and leaves `^` as the only load,
so `p.offset(3)^` is visibly the two acts it is.

Three sub-decisions, each with a plausible other answer:

- **Elements, not bytes**, which is also the one most easily got wrong and least easily
  noticed: on a `^i64` a byte-offset lowering answers garbage from inside the first
  element rather than failing. It matches the language — a string is rune-indexed so `len`
  and `[i]` agree about the unit — and it is what a `getelementptr` with the pointee's
  type already means, so nothing scales by hand.
- **Mutability propagates.** `^mut T` in, `^mut T` out, or `p.offset(n)^ = v` is
  unwritable and the write direction stays broken. The inverse is what pins it: offsetting
  a `^T` must not launder it into a writable one.
- **Signed**, because a negative offset is meaningful in C and refusing it buys nothing
  where nothing is checked anyway.

**The unsafe-context check went in the typechecker**, beside the unsafe-*call* check that
moved there on 08/18 and for the same reason: it needs the **receiver's type**.
`p.offset(n)` and `xs.offset(n)` are the same three tokens, so the syntactic pass that
owns the rest of lyra-E011 could only refuse both or neither. That is hazard 9 in its
general form — a name does not identify what it names — showing up a third time.

**`CBuffer` is the point of making `offset` a primitive rather than a feature.** A raw
pointer carries no length, so nothing about it can be checked; a pointer *and* a length
can be. So the compiler gets one unsafe primitive and `std/ffi.lyra` gets the trapping
accessor in ordinary Lyra, which puts `unsafe` in one file instead of at every use — the
same division `random_seed`/`Rng` and `read_line`/`parse_i64` already draw. `get` is
`pure noalloc`, the second of which is why its panic message is a constant rather than an
interpolation naming the index.

What it does **not** buy is worth being as explicit about: the pointer can still dangle,
and a `CBuffer` built with a wrong length checks against the wrong number and reads out of
bounds silently. Constructing one is the promise; the checking is what the promise buys.
Hence public fields — a caller who can produce the pointer can already produce a bad
length, so a constructor would add a place to look and no safety.

**One thing found and left alone**: `^mut T` is not assignable to `^T`, so
`CBuffer { ptr: &mut xs[0], … }` is refused and the author writes `&xs[0]`. Dropping
mutability is sound and every language with both spellings allows it, but it is an
assignability change that also reaches argument passing, so it is in `todo.md` rather than
folded into this.

### 08/19/26
**Foreign functions lower, and Lyra calls zlib.** `extern` now goes front to back: the
declaration collects, a call resolves and type-checks against the signature, its effects
come from the bound it asserts, it lowers to a `declare` under the C symbol, and `@link`
reaches the link line. The proof was run against a library nobody wrote for Lyra — zlib's
`crc32` agrees with Python's, and `compress` → `uncompress` round-trips 16 bytes back
identical, with the compressed length arriving through a `^mut u64`.

**The backend half is 100 lines, and that is the design working rather than luck.** An
extern is a signature standing in for a body someone else supplies, which is a shape this
package already emits — a function declared before any body exists so a call can reference
it. `ExternDeclStmt.Func()` is that body-less function, so declaring one is
`declareFunctionAs` and calling one is the ordinary call path, found under the same
`funcKey` any other function is. Nothing downstream of `extern.go` knows an extern exists.
The two prior decisions that bought this were putting `Func()` on the AST node in the first
place (08/18, on `TraitMethod.DefaultImpl()`'s pattern) and refusing non-FFI-safe types at
the signature: every parameter and result is a scalar, a `^T` or `void`, so nothing is
reference-counted and there is no retain, release or drop glue to emit at all.

**Three decisions in the backend, each with an obvious wrong answer:**

- **The symbol is the name as written**, not `userSymbol`'s `lyra.<module>.<name>`. A
  foreign symbol belongs to the linker; mangling it names a function nobody defines, and
  the failure is a link error about a symbol the source never mentions.
- **Two declarations of one foreign name are one `declare`** (`l.externs`, keyed by the C
  symbol). Two `declare`s of one name is invalid IR, and the case is ordinary rather than a
  corner: two libraries both using `strlen` is what a standard library looks like. What
  *is* refused is two declarations that **disagree** about the signature — only one can
  describe the function that gets linked, so emitting either silently picks a winner
  (rule 5). The diagnostic names both files, because two externs of one name are usually
  in two files and a bare `line:col` prints the same position twice.
- **`mut`/`ref` on an extern's parameter is refused** (`lyra-E063`, where the types are
  checked, because it is the same question). `paramIsByRef` is *Lyra's* convention, and at
  the boundary it is one of two bad things: inert on a scalar, which still goes by value,
  so the modifier says something the call does not do; or an ABI mismatch on a pointer,
  where `(mut ^i64)` reads as "a pointer" and would pass an `i64**`. `own` is deliberately
  still accepted — it is the move axis, and moving a copied scalar means nothing either way,
  so refusing it would be a rule with no failure behind it.

**And one bug the work turned up, which would have made FFI unusable in a library.** An
extern has no `pub` — there is no way to export one — but `declIsPublic` returned `true`
for any node it did not recognise, so an extern took the **bare, program-wide key** and two
modules each declaring `extern abs` collided with `function "abs" is already defined`. That
is not an edge case: it is what two libraries both using `strlen` produce. The fix is one
case in `declIsPublic`, and the two halves then compose exactly — the front end keys each
declaration by its module and keeps them apart, the backend keys `l.externs` by the C
symbol and emits one `declare`. The dedup written for the invalid-IR reason turned out to
be what makes the privacy rule work.

**The gap the zlib run found, and it is a language gap rather than an FFI one.** A buffer
goes *out* today — `&mut xs[0]` plus a length is exactly what `compress` takes — but a
`^u8` coming *back* can only be read at offset zero: `p^` is the whole vocabulary a raw
pointer has, `p[i]` is `lyra-E001`, and there is no `offset`. So `zlibVersion()`'s second
byte is unreachable and `CString`'s read direction is unwritable. The spelling is a real
choice (`p[i]` reads naturally and is `xs[i]`'s spelling with none of its bounds check;
`p.offset(n)` is uglier and honest about the arithmetic being a separate act), so it is in
`todo.md` rather than guessed at here.

**Linking rides the declaration.** `@link("z")` on the extern, unioned across every module
in the compile, sorted, deduplicated, and passed as `-l` by `lyrac`'s `linkFlags` — which
every "compile with" hint prints too, since a hint naming fewer libraries than the build it
stands in for is a command that fails at link time on a program that compiles.

**The suite's own extern tests call libc and libm** — `abs`, `sqrt`, `frexp`, `strlen`,
`srand` — rather than a fixture whose both sides we wrote. A fixture proves the IR is
self-consistent; the thing worth proving is that Lyra matches an ABI it did not choose.
They also need no package installed on either platform, which is why zlib stays the manual
proof and the CI story is still open (`-lz` links on macOS and not in the Debian container).
The whole suite passes there too, which for backend work is the check that counts: Debian's
older clang uses typed pointers and rejects function-type mismatches Apple clang's opaque
pointers cannot represent.

---

The design settled on 08/18–08/19 and implemented here, kept for the reasoning behind each
rule rather than for the rules themselves (those are in `CLAUDE.md`):

### The effect of an extern call

An extern has no body, so nothing can be inferred. It carries **`AllEffects` by default**,
matching the unresolved-callee rule the purity pass already uses — the cautious answer is
what you get for free.

**A bound may be written, and writing one is `unsafe`.**

```lyra
extern sqrt: (f64) -> f64                    // legal, and useless: AllEffects
unsafe extern pure sqrt: (f64) -> f64        // the assertion, marked as one
```

The asymmetry is the point: **for Lyra code a bound is a promise the compiler checks; for
an extern it is a promise the compiler records.** Lyra already has a word for an assertion
it cannot verify, and this is one — a wrong `pure` here does not fail locally, it silently
corrupts the effect analysis of every caller, which is a *declaration-time* danger Rust's
"safe to declare, unsafe to call" split has no analogue for. So the keyword marks exactly
the unverifiable claim and nothing more: declaring is safe, narrowing is not.

Calling one still needs an `unsafe` block — `lyra-E011` already implements that for an
`unsafe` function and needs no new rule.

**`noalloc` on an extern means "allocates nothing *Lyra* owns."** `EffectAlloc` tracks the
ref-counted boxes the ownership pass reasons about; a foreign `malloc` is not in that
ledger and this bound does not claim it is. Writing it down matters because the alternative
is a bound that silently stops binding — the shape `pure noalloc … => s.trim()` had.

Without a bound the compiler must assume the callee may do anything, which includes reading
input and mutating through any pointer it was handed. That is the same conservatism
`AllEffects` already encodes, so no new machinery: an extern with no bound is simply a
function `pure`/`det`/`noalloc` cannot call.

### What may cross: FFI-safe types only

An extern signature admits the **scalars**, **`^T`**, and **`void`**. It refuses `string`,
`[]T`, closures, tuples, `data` types, and anything `shared` or `weak`, with a diagnostic
naming what to write instead.

This is `read_line` beside `parse_i64`, one layer up: the compiler takes only what is
genuinely primitive, and everything expressible is ordinary Lyra in the prelude. It also
means **there is no nul-termination policy to get wrong**, because there is no automatic
conversion to have one.

`std.ffi` supplies the ergonomics, written in Lyra:

- **`CString`** — a `[]u8` carrying a trailing NUL, with a `^u8` accessor. A Lyra `string`
  is `{ptr, byte_len, rune_count}` and deliberately **not** NUL-terminated, so a `char*`
  needs a copy; making that copy visible is what lets `noalloc` see it and what stops a
  temporary buffer acquiring an invented lifetime.
- **`xs.data() -> ^T`** — no copy needed. A `[]T`'s elements already live behind a
  contiguous `T*` inside the box (they are not inline, which is what makes `push` safe), so
  the buffer a C function wants is already there.

### Ownership does not cross, in either direction

Lyra never hands a Lyra-allocated buffer to C to keep, and never adopts a C-allocated one
into a `[]T`. Both would require the other side to understand the box header — and a
`[]T`'s header sits at the *start* of its box, which is the same fact that makes `slice`
copy rather than borrow. To give C data it keeps, copy into a C-allocated buffer through an
extern allocator; to take C data, copy into a fresh `[]T`. Both copies are written in Lyra
and visible.

**A `^T` into a live array is valid only until the next mutation**, since `push`
reallocates the element buffer. That is Rust's `as_ptr` hazard exactly, it is not
checkable today, and it is therefore squarely inside `unsafe` — which is where the pointer
that produced it already put the caller.

### Linking — `@link`

**[DECIDED 08/18]** A link requirement rides the `extern` that needs it:

```lyra
@link("m")
unsafe extern pure sqrt: (f64) -> f64
```

`lyrac build` collects the `@link`s of every module in the compile, sorts and deduplicates
them, and passes `-l<name>` for each.

**Neither of the two conventional answers works here.** A CLI flag
(`lyrac build --link m`) does not compose: a *module* wrapping libm would force every
consumer program to know and pass it, which is exactly the failure the module system exists
to prevent — a library's requirements travel with the library. A manifest (`lyra.toml`) is a
package-manager-shaped file introduced for one field, in a compiler that has deliberately
avoided having one: modules resolve by path, `std` is found beside the executable, and the
RC runtime is emitted *into the module* precisely so there is no separate object to link.

So it goes in the source, where attributes already live (`@builtin`, `@derive`, `@packed`,
`@align`).

**Per declaration, not per module.** A `std.math` wrapping twenty libm functions repeats
`@link("m")` twenty times, which is the cost; what it buys is that the requirement is never
separated from the thing requiring it, and that a library can in principle be attributed to
the extern that needed it — so linking only what is reachable stays possible later. A
module-level form could not.

Four smaller rules:

- **The argument is a library name, not a flag.** `@link("m")` becomes `-lm`. Taking a raw
  flag invites `@link("-framework CoreFoundation")` and makes `lyrac` a shell.
- **Sorted and deduplicated.** Deterministic for the reason `Resolution.SpecKey` sorts its
  bindings: a build must not wobble between runs. If link *order* ever matters for a real
  case, that is evidence the flat set is wrong rather than a reason to preserve source
  order.
- **`--emit-llvm`'s hint carries them.** The "compile it with: clang …" line must name every
  `-l` the real build would pass, on the standing rule that both hints carry the
  optimization level — a hint that describes a different build than the one it stands in
  for is worse than no hint.
- **`@link` needs no `unsafe`.** It is not an assertion about behaviour: a wrong library
  name fails loudly at link time. That is the whole contrast with the effect bound, which
  fails *silently* and therefore does need the keyword.

**`-lm` stays unconditional** for now. The float intrinsics (`floor`/`ceil`/`round`, `fmod`)
are the *compiler's* requirement rather than a program's, so they are not a `@link`
anywhere. The tidier shape is one requirement set fed by two sources — source attributes and
the backend's own intrinsics — and it is worth doing only when the second source has more
than one member.

**Linking a library nothing calls is harmless**, as `-lm` already is today.

### Integer widths at the boundary — LP64, fixed, written down

**[DECIDED 08/19]** An extern signature uses Lyra's **fixed** widths. There are no
C-shaped aliases (`c_int`, `c_size_t`) and no target-dependent types.

The question is real: C's scalars are not fixed-width, and Lyra removed `int`/`uint`
*for determinism*, so nothing in the language spells "whatever `long` is here". But the
boundary is not where that gets decided — **the compiler already hardcodes the answer**,
in three places that each found it independently:

- `pkg/backend/llvm/clock.go` builds `struct timespec` as `[2 x i64]`, commented "two
  64-bit words there — `{ time_t tv_sec; long tv_nsec; }`". A C `long` is already written
  as `i64`, in a shipped builtin.
- `layout.go`'s `const pointerSize = 8`, commented "assume a typical 64-bit target
  datalayout". Every union payload and aggregate layout rests on it.
- `i128` is laid out "16/16 on the mainstream 64-bit ABI".

So `extern` inherits a commitment rather than introducing one. Adding aliases would buy
portability no program this compiler can emit could observe, and cost a target-dependent
type mechanism the language has none of — `type` aliases are fixed source and the prelude
is one set of files.

**Where the assumption lives:** `layout.go`'s `pointerSize`. Everything else should
reference it rather than restate it.

### What Windows would actually cost, since LP64 is not the whole story

Worth writing down because the obvious framing is wrong. **Windows x64 is LLP64, not
32-bit**: pointers are 64 bits, so `pointerSize`, the alignment rules and the union layout
engine are all *correct* there. The earlier note that a non-LP64 target "invalidates
pointerSize first" is true of a 32-bit target and false of Windows.

| C type | LP64 | Windows LLP64 |
|---|---|---|
| `long`, `unsigned long` | 64 | **32** |
| `int`, `long long`, `size_t`, pointer, `float`, `double` | same | same |

**`long` is the only divergent type**, and zlib walks straight into it: `uLong` is
`unsigned long`, so `crc32` returns 64 bits here and 32 there. Declared `u64`, a Windows
build would read eight bytes where the ABI passes four — an ABI break, not a compile error.

It is gated behind larger blockers (`tui.go` is termios and `ioctl`, `random_seed` is
`getentropy`, `clock.go` is `clock_gettime` — API ports, not width fixes), but it is on
that list rather than absent from it.

**The mitigation costs one line and needs no new feature.** `lyra-E063` looks *through* a
newtype, so `std.ffi` defines the one divergent type:

```lyra
newtype CLong  = i64
newtype CULong = u64
```

and a signature writes it wherever C writes `long`:

```lyra
@link("z")
unsafe extern pure crc32: (CULong, ^u8, u32) -> CULong
```

That turns "audit every extern" into "change two lines" the day a Windows target is real.
It does not buy automatic correctness — someone still edits and rebuilds — but nothing
short of target-dependent types would, and that is a feature to build when there is a
target to build it for.

### Writing C's types

| C | Lyra | C | Lyra |
|---|---|---|---|
| `char` | `i8` | `long`, `long long` | `i64` (`CLong` for `long`) |
| `unsigned char` | `u8` | `unsigned long` | `u64` (`CULong`) |
| `short` | `i16` | `size_t`, `uintptr_t` | `u64` |
| `int` | `i32` | `float` | `f32` |
| `unsigned int` | `u32` | `double` | `f64` |
| `void` | `void` | `T*`, `void*` | `^T` / `^u8` |

`_Bool` is deliberately absent — `lyra-E063`, and see the FFI-safe section for why.

### 08/18/26
**Raw pointers infer and lower.** `&x`, `&mut x`, `p^`, `p^ = v` and `unsafe { … }` all
work, and `lyra-E011`'s unsafe-context policy is reported again after being withdrawn on
08/13 — its advice was to write an `unsafe` block that was itself an unknown expression,
and a diagnostic whose fix does not compile is worse than none.

**Large in the front end and small in the back**, which is the shape worth recording. A
raw pointer *is* an LLVM pointer: `&x` is `argumentAddress` — the address a `mut`
parameter is already passed by — `p^` is a load, `p^ = v` a store, and an `unsafe` block
is its body. No ownership, no refcounting, no drop glue, because a raw pointer does not
own what it points at. The one thing that would have been wrong is `lowerExpr` on the
operand of `&`, which yields the *value*; storing that into a fresh slot hands out the
address of a copy.

**Mutability is checked twice, and the two questions are not interchangeable.** `&mut x`
asks whether **x** may be mutated — the binding rule every interior mutation obeys, reused
so a `&mut` cannot outrun the assignment rule — while `p^ = v` asks whether **p** is a
`^mut`. A `^mut T` may be copied into a `let` and a `^T` may be taken of a `var`, so
neither answer implies the other, and checking only one lets a program write through a
pointer it was never allowed to take.

**`^T` lowers to a pointer to its pointee, not to an opaque `i8*`.** llir type-checks a
store against the destination's element type, so the opaque form panicked on every write
before clang saw it. The Linux/ASan suite is the check that matters for this: Debian's
older clang uses *typed* pointers and rejects exactly the mismatches Apple clang's opaque
ones make invisible. It passes.

**One bug fell out that had been invisible by construction.** `UnsafeBlockExpr.Body` was a
`BlockExpr` **by value**, so the collector's `Body: *body` stored a *copy* — with a
different address from the node the scope table was keyed on. Every binding declared inside
an `unsafe` block therefore resolved nowhere, and each reference reported "undefined
identifier". It could not have been noticed while the block was refused before anything
looked inside it, which is the recurring shape: an unimplemented feature hides its own
bugs, and both surface the day it is implemented.

Deliberately absent: pointer arithmetic, comparison, null, and any way to make a pointer
other than `&`. A raw pointer addresses a binding that exists; producing one from an
integer is a separate feature with its own safety story, and adding it silently as a
consequence of `^T` being a type would be exactly the phantom surface this history is
about.

### 08/18/26
**An import's member list restricts visibility.** `import std.tui.{ bg }` admitted `grey`,
`rgb`, `bold` and every other `pub` name in the module. The rule in force was "any `pub`
name of any module you imported at all", and the member list drove only the namespace
binding and `lyra-W004`.

**It was architectural rather than a missing check**, which is why the obvious fix does not
work. `exportToGlobal` put every `pub` declaration into one global scope that sat on every
module's parent chain, so *exported* and *visible* were the same thing and no per-reference
check was ever consulted. A checker pass over `collectRefsByFile` was tried on 08/17 and
abandoned: that walk is **syntactic**, with no scope information, so it cannot tell a local
binding from a module member — it reported a callback parameter named `f` in the prelude's
own `array.lyra` as belonging to a user module that happened to declare a top-level `f`.
Over-collection is safe for "is this name mentioned anywhere", which is all the
unused-import warning asks, and unsound for "what does this name resolve to".

So the fix is a scope. The chain is **module → imports → prelude**, and it stops there:

- `ImportScopeFor(module)` holds what that module's imports bring in, filled by
  `PopulateImportScopes` from `Collector.Finish` — the earliest it can run, since a
  module's imports resolve against other modules' exports and exports are recorded per
  file.
- `PreludeScope.Parent` is **nil**. GlobalScope is still written and still read, for a
  different question: it is the program-wide registry that makes two modules exporting one
  name an error, and it is what `ExportingModule` consults to turn "undefined" into a
  useful sentence.

**All three decisions from 08/17 held.** An error, not a warning — nothing in `std/` or
`examples/` relied on the hole, exactly as measured, and the only fixtures that did were
tests written against it. A namespace import admits no bare names, or the two import forms
would mean the same thing. And UFCS stays exempt — structurally rather than by a rule,
since `b.doubled()` resolves against the receiver's type, so a method the receiver
justifies needs no import of the free function.

**A type needed its own gate, and the asymmetry is the thing to remember.** A value
resolves through the scope chain, so the imports scope gates it for free. A type does not:
it goes through the Types/Traits maps keyed by `declKey`, which answers *whose declaration
is this* and says nothing about *who may see it*. So `import lib.{ listed }` still admitted
`lib`'s `Point` — the same hole one level up. `importedAt` closes it, and asks the module of
the declaration's **file** rather than `ModuleOf[name]`, which is last-writer-wins
(invariant 4).

**Two diagnostics had to learn the difference between the two failures.** "Undefined" is
the wrong word for by far the commonest new one — the name exists, it is `pub`, and this
file simply did not ask for it — so every undefined-name site now appends *"module `lib`
exports it, but this file does not import it; add `import lib.{ Point }`"*. And
`reportPrivateType` fired for any name another module declared and this one could not
resolve, so an exported-but-unimported name was reported as **private**, telling an author
to add a `pub` that was already there. Before visibility was restricted the two could not
be told apart there, because an exported type always resolved.

### 08/18/26
**On-demand return inference: a destructure no longer depends on declaration order.**
`let (w, h) = viewport()` with `viewport` declared below its caller and un-annotated was
`lyra-E058`, asking for a return type. A destructure needs the element types where the
pattern is walked — each name's type comes from decomposing the value there and then, and
nothing later revisits it — so it is the one position that cannot defer. Binding the whole
tuple was always fine, and so was a scalar return.

**It was reachable by writing ordinary code in the style this project documents.**
Helpers-below-main puts an un-annotated helper after its caller by construction, which is
what made a diagnostic the wrong answer.

The callee's declaration is now checked at the point the destructure asks for it, once.

**Both things the todo entry said to settle fall out of memoizing the declaration.** A body
checked twice reports its diagnostics twice, so `checkVarDecl` consults `checkedDecls` and
returns — "checked early" and "checked in order" become one event, whichever happens first.
And `inferringRet` breaks a cycle: two un-annotated functions destructuring each other's
results have no fixed point, since computing either return type requires the other. That
case still gets lyra-E058, whose message is now written for it rather than for declaration
order.

**The third thing, which the entry did not anticipate and which is the one that bites.** A
hoisted body has to be checked *as if the pass had reached it in order*, and it is not
enough to swap the module scope. `withParamScope` **copies** an enclosing lambda's
parameters into a nested one's — deliberately and correctly, since a nested lambda is
lexically inside its enclosing one and sees its parameters. A hoisted *top-level* function
is not inside anything, so without clearing that context its body could resolve a name
belonging to the caller's parameters:

```lyra
let outer = (secret: i64) -> i64 => {
  let (a, b) = helper()
  a + b + secret
}
let helper = () => (secret, 2)     // type-checked clean; `secret` is not in scope here
```

A false **accept** — the direction that does not announce itself, and it would have shipped
unnoticed had the probe not gone looking. `atTopLevel` saves and clears the parameter scope,
the enclosing return type and name, the `where` bounds in scope, and the impl/trait context
a method body is checked inside, restoring all of it after. The general rule: **a pass that
hoists work must reset the context that work would have had**, not merely the context it
needs to borrow.

### 08/18/26
**`lyra-W020`: a `for-in` binding the body never reads.** It names `_` as the fix, which is
why it could not have existed a day earlier — the advice would have been to write a
spelling the parser rejects, and silence beats that.

Separate from the unused-*local* warning because the fix is different and the difference is
the whole point: a local can be deleted, a loop binding cannot, since the loop still has to
iterate. The two-name form is where it earns its keep — `for k, v in xs` that reads only
`v` is exactly the case `for _, v in xs` was added for. A name already starting with `_` is
exempt, matching the unused-local rule.

Both halves read one walk (`referencedNames`), because "is this name referenced" is the same
question about two kinds of declaration and two copies would drift about the cases that make
it conservative: a *write* counts as a read, and the walk descends into nested lambdas so a
captured name is not reported.

**Measured before shipping**, as lyra-W018 was: 2 instances in the prelude and 7 across
three examples, every one of them a genuine unused counter. All are now written `_`.

**It surfaced a bug worth more than the warning.** `ForInLoopExpr` carried **no Location** —
the collector never set one — and a diagnostic reported against a zero Location does two
things: it prints with no `line:col`, and it *escapes the driver's per-file filtering*, which
keeps a location-less diagnostic on the grounds that it must be program-level. So the two
prelude warnings appeared on **every file compiled**, attached to whatever the user was
checking. The first sighting was `lyrac check` reporting `loop variable "i"` on a file with
no loop in it. The loop now carries its own span, and `KeyLocation`/`ValueLocation` carry the
bindings', so the warning points at the name it is about. Both are tagged out of the printer,
so no golden moved.

### 08/18/26
**`for _ in 0..<n` parses** — a loop that repeats without naming a counter.

`identifier` is `/(_[a-zA-Z0-9_]+|[a-z][a-zA-Z0-9_]*)/`: a leading underscore needs a
character after it. So `for _i in` worked and a bare `for _ in` was a syntax error, and the
workaround — name the counter and never read it — looked like a style choice rather than a
necessity. That is the same shape `let _ = expr` had before 08/07, and the same reason it
survived.

**`_` is admitted inside the existing alias** rather than as a `wildcard_pattern`
alternative beside it, so `for_variable_or_key` still spans whichever was written and every
consumer keeps reading one node kind — the collector needed no change at all. It sees a
binding whose text is `_`, and that name is *unforgeable*: no identifier can be a bare
underscore, so nothing in the body can refer to it and nested wildcards shadow each other
harmlessly. The property the spelling promises holds by construction rather than by a check,
which is why no pass had to learn about it.

The alternative was a synthesized name per loop (`_for0`), which would have had to be unique
across nesting, would have shown up in diagnostics and debug output, and would have been
forgeable — `_for0` is a legal identifier. Cost: **+1 parser state**, 7,821 → 7,822.

### 08/18/26
**`for d in -1..<=1 { if d != 0 { … } }` compiles.** It was
`lyra-E001: operator !=: incompatible types: integer literal and integer literal` — the
same words twice, which is what an asymmetry between two rules looks like from the outside.

A negative literal is `untyped_signed_int`, a non-negative one is `untyped_int`, so a range
with a negative bound gives its counter the signed untyped type while the `0` it is
compared against has the unsigned one. Equality decides its operands with
**assignability**, ordering with **numericResultType**, and only the second knows that
pair: assignability widens an untyped type to a *concrete* one and says nothing about two
placeholders, neither of which is a slot a value ever lands in. So `d < 0` compiled and
`d != 0` did not.

There was also nothing to write instead. `for d: i64 in …` is a syntax error, so the only
way out was to iterate an annotated array (`let around: [3]i64 = [-1, 0, 1]`), which is
what `examples/life.lyra` does — the neighbour-offset loop is where this was found, and it
is the loop every grid program writes.

**The fix admits that one pair and no more.** Adopting numericResultType for equality was
the first attempt and it broke four tests, correctly: that rule is wider in another
direction — `5 < 5.0` compiles — and equality's refusal of int/float mixing is a written
decision. Putting the rule in `isAssignable` instead would have answered a question none of
its ~14 owning sites asks. So `areEqualityCompatible` takes the pair directly.

Left standing and now in todo.md: ordering accepts `5 < 5.0` while equality refuses
`5 == 5.0`. One of those is wrong, and it is probably ordering.

### 08/18/26
**Three bound-dispatch faults, found landing trait defaults; two of them reproduce with no
default in sight.** All three type-checked cleanly, which is what they have in common: a
call the typechecker resolved *abstractly* — the receiver was a type variable there — and
that only a specialization can name a function for.

**A `where`-bound call on a generic impl target did not lower once the program declared a
`module`.** The candidate table is keyed in the typechecker's spelling of the type
(`Box<i64>`, and the mono key `Box$i64`); the backend asked under the *instantiated*
name, which `instantiationSymbol` prefixes with the declaring module's key —
`main__Box$i64`. So `impl Sized2 for Box<t>` published a candidate nothing looked up, and
the call failed with "no impl of Sized2 for it" on a program the front end had checked
clean. **Drop the `module` line and it ran**, which is why it survived: every reproduction
small enough to paste is a program with no module header, and every real one has a header.

The fix is a second accessor rather than a shared mangler. `candidateKey` is
`recordedType` minus its last step — substitution applied, instantiation normalization
*not* — so the lookup asks in the vocabulary the table was keyed in. The alternative was
to teach the typechecker to predict this package's mangling, which is a second copy of
`instantiationSymbol` free to disagree with the one that emits the symbol. Three lookup
sites moved onto it (the bound method call and both operator-candidate paths).

**A `mut` receiver through a bound was a wild load, not a type mismatch.** A bound call
has no entry in the *resolution* table — its concrete impl comes from the candidate table
at lowering — so `methodParamModes`, which reads that table, returned nil and every
operand went by value. The emitted method takes a pointer for a `mut` receiver, so it was
handed a struct:

```lyra
let twice<t> where t: Bump = (v: mut t, n: i64) -> void => { v.bump(n)  v.bump(n) }
```

segfaulted, and so did the identical call inside a trait default — which is the shape that
found it, since a default body's `self` is a type variable and every call on it is a bound
call. `methodParams` now takes the resolution the caller has in hand and falls back to the
table only for the one path that has none. Not a diagnosable mismatch at any point: the
front end sees a well-typed call, and the IR is well-formed with the wrong ABI.

**`Trait::method(x)` did not lower at all** — for a defaulted method and an ordinary impl
method alike — dying as `no type recorded for the callee of an indirect call`.
`lowerFunctionCallExpr` knows a `MemberExpr` callee and an identifier and nothing else, so
a `TraitMethodPathExpr` fell through to the function-value path, where the callee is the
*name of a trait method*: not a value, and with no recorded type. That made the compiler's
own advice unbuildable, since the ambiguity diagnostic for a name two traits provide says
"use TraitName::method(...) to disambiguate".

Everything it needed was already published — dispatch records the full `Resolution` for
this call exactly as for a `.`-call. The one difference is the operand layout: the receiver
is argument 0 here rather than a separate expression, so arguments and signature parameters
are index-aligned and the loop needs no offset. The stale comment on `methodParamModes`
had said as much for months ("or nil when there is no resolution — a fully-qualified call,
or an unresolved one"), describing a state that stopped being true when dispatch started
recording these.

**Trait default methods are dispatched to.** `trait Named { pure name: (Self) -> string
pure shout: (Self) -> string = (self) => self.name() ++ "!" }` parsed and collected from
the beginning; `c.shout()` reported *"Cat has no field or method"*, and an impl could not
override one either, since nothing looked. The fifth instance of the
surface-nothing-reads shape, after `wallClock`, the `where` bounds, `@derive` and the
operator-named methods.

**`Self` is a type variable, and that is the whole design.** A default body is written
once and runs for every implementing type, which is the definition of a generic function
— so it is checked once with `self: GenericType{"Self"}` bounded by the declaring trait,
and monomorphized per implementing type. Every piece that needs already existed:

- `self.name()` inside the body is a call on a value of type-variable type, which is
  `dispatchViaGenericBound` — it records the abstract resolution and publishes one
  concrete candidate per implementing type;
- the backend substitutes `Resolution.Bindings` through `recordedType`, so the shared body
  lowers at each receiver's own type;
- the ownership pass analyzes that body once per specialization, at those bindings.

**The backend needed no change at all**, which is the strongest evidence the choice was
right. The alternative was to deep-copy the default clause into every impl that lacks it,
so each got its own AST nodes — that needs a full expression/statement cloner this
compiler does not have, and a missing case in one is a silently *shared* subtree with a
miscompile at the end of it. Type variables are the mechanism already in hand for "one
body, many types". The name `Self` is unforgeable: a type variable is lowercase by lexer
rule, so no program can declare one that collides.

**The default is presented as the `TraitMethodImpl` it stands in for**
(`ast.TraitMethod.DefaultImpl()`), because every consumer of an impl method already
handles that shape — dispatch, the MethodTable, the purity fixpoint, the ownership pass,
the backend's emitted-method cache. It is cached **on the AST** rather than per pass, and
that is load-bearing: those consumers key on the pointer, so two passes building their own
would disagree about whether they are looking at the same method, and the body would be
emitted once per call site. One instance per trait *method*, not per impl — the impl is
what `SpecKey` varies over (`Cat$Named$shout` beside `Dog$Named$shout`), so sharing the
instance is exactly what makes the body shared and the specializations distinct.

**Dispatch tries the impl's own clauses first** and falls back to the default only when
they match nothing, which is what makes an override an override rather than an ambiguity —
the same last-rung shape a newtype's method fallback has. `Self` joins the impl's own
bindings rather than replacing them, so a generic impl's variables survive.

**Two things that had to be added beyond dispatch.** The default body's *inner* bound
calls need a candidate published at the concrete receiver
(`publishDefaultBodyCandidates`, `publishImplBodyCandidates` for a default: that one is
driven by the impl's `where` constraints and a default has exactly one bound to walk,
`Self` at its trait). And the **purity pass now holds a default to the bound its trait
declares** — `collectMethodImpls` gathers defaults, `checkTraitDefaultBounds` enforces
them — reported at the default rather than at each impl that inherited it. The bound used
to be enforced on every *override* and not on the thing being overridden, which is the
wrong way round: the default is the one body the trait's author controls.

**One diagnostic had to be written for the new context.** `self.nonexistent()` in a
default reported *"type parameter Self has no method; add a `where Self: Trait` bound"* —
advice naming a clause no program can write, because `Self` is a variable the compiler
introduced rather than one the author wrote. It now names the trait: *"trait Bad does not
declare a method "nonexistent", so a default body cannot call it on `self`"*.

Found along the way, both pre-existing and both now in todo.md: a `where`-bound call on a
**generic impl target** does not lower once the program declares a `module` (the candidate
is keyed `Box$i64`, the backend asks about `main__Box$i64`), and `Trait::method(x)` does
not lower at all — which is the spelling the ambiguous-call diagnostic tells you to reach
for.

### 08/18/26
**`lyra-W019`: `[v; n]` whose slots share one mutable value.** `[[' '; WIDTH]; HEIGHT]`
builds **one** row referenced HEIGHT times, so every `grid[py][px] = c` writes the same
place and every row prints identically. Found 08/14 in `examples/mandelbrot.lyra`, where it
was the third of three bugs and the one that survived fixing the other two — the image
stayed uniform, which reads as an arithmetic mistake rather than an aliasing one.

**The semantics are correct and are not what changed.** `[v; n]` evaluates its value once,
which is what makes `[expensive(); 1000]` one call and `[0; 480000]` a loop rather than
480,000 evaluations; each slot is then an owner of that one value. What was missing is that
nothing said so at the moment it bites, and "n copies" reads as "n *independent* copies" to
almost everyone. A warning rather than an error on lyra-W018's reasoning: the code is
correct, a deliberate alias is a real thing to want, and with no `#[allow]`-shaped
suppression in the language an error would leave that intention nothing to write.

**The predicate is narrower than "managed", and every rung of it was measured** rather than
read off the layout — `[v; n]`, then a write through slot 0:

| element | result | |
|---|---|---|
| `[]i64` | `grid[0][0] = 7` → visible in every slot | **shares** |
| `struct Row { cells: []i64 }` | same, through the field | **shares** |
| `(i64, []i64)`, `Held<[]i64>` | same, through the component | **shares** |
| `string` | `words[0] = "bye"` → only slot 0 | immutable: nothing to mutate *through* |
| `i64`, `[2]i64`, `struct Cell { n: i64 }` | copied per slot | inline storage |
| `shared Cell` | aliases (proven separately) | `[]shared T` does not lower — see todo.md |

So the question is not "is this managed" but **"can the sharers mutate what they share"**,
and the gap between the two is `["hi"; 3]` — correct, unremarkable code, and by far the
commoner spelling. Warning on managed values would have fired mostly on it, which is how a
warning stops being read. That reasoning is now the third of a graded trio in
`pkg/analyzer/ownership`: `IsManaged` (is this a ref-counted reference), `OwnsManaged`
(does copying duplicate one, so refcounting must run), `SharesMutableState` (is the
duplication *observable*).

**Two findings inside the predicate, both from measuring rather than reasoning.**
`readonly` does not stop the sharing, only the direct write: `fs[0].cells[0] = 7` is
lyra-E001, and

```lyra
let mut c = fs[0].cells
c[0] = 7                   // writes every slot's cells
```

is not — so a frozen field whose *type* shares still shares, and honouring `readonly` on
the way down would have been a false negative in exactly the struct-wrapping-an-array shape
the warning exists for. And a `shared` *scalar* does not share: assigning to the binding
rebinds it rather than writing the box, so the test is a writable **field**, not the
allocation flavor.

**It is a standalone post-typecheck pass, and it has to be.** The element's type at
inference time is not the type it is lowered at: under a `[][]rune` annotation the inner
`[' '; WIDTH]` infers as the fixed `[WIDTH]rune` — which shares nothing, being copied per
slot — and only propagation widens it to the heap-boxed `[]rune` that does. A check inside
`inferArrayRepeatType` would have cleared the exact program that motivated it. Reading the
TypeTable is what makes it see the settled type the backend will lower.

The evidence is carried by the same walk that decides: `SharedMutablePath` returns the
chain of **struct field** names down to the sharing part and `SharesMutableState` is its
boolean face, so the message that explains why a `Row` aliases cannot drift from the
predicate that decided it does. No name is appended for a tuple or a `data` payload —
`(i64, []i64)` and `Held<[]i64>` already print what they hold.

**`examples/life.lyra`** — Conway's Game of Life, toroidal, sized to the window — was
written alongside it as the program that walks straight into the shape: a double-buffered
grid rebuilt every generation. It also turned up two gaps of its own; see todo.md.

### 08/17/26
**`lyra-W018`: a function that could be `pure` and does not say so.** The effect row is
already computed for every callable — `pure` is checked *against* it — so "could this have
been marked pure" was a question the compiler could always answer and kept to itself. It is
`CheckPurity`'s own fixpoint read backwards, which is why that function now returns two
results rather than gaining a second pass: an effect inside a `pure` function is an error,
a missing bound is advice, and both come off one analysis.

**The obvious justification is false here, and finding that out changed the feature.** The
first draft's message said an unmarked callee blocks a `pure` caller. It does not: purity is
inferred whole-program, so `pure caller = helper(n)` compiles against an unannotated
`helper`, free function and impl method alike. Nothing is refused, and a warning whose
stated reason is wrong is worse than no warning.

What the bound actually buys shows up on the *next* edit — it decides **where the blame
lands**:

```lyra
let helper = (n: i64) -> i64 => { println("added later"); n * 2 }
let caller = pure (n: i64) -> i64 => helper(n)
```

The `println` is reported at `caller`, the only thing in the program that promised
anything. Mark `helper` and it is reported at the `println` as well — at the edit, in the
function being looked at. That is `lyra-E031`'s diagnostic-lands-somewhere-else failure one
rung up, and it is the whole case for the warning.

**The scope was measured, not chosen.** Counted over `std/` and `examples/`: `pure` fires
0–6 times per file and names real pure helpers (`fit`, `render_row`, `key_name`); `det`
fires 11–14 times and *every* candidate in `tui_viewer` is a terminal-escape wrapper
(`cursor_hide`, `move_to`, `alt_screen_enter`) that qualifies only because `det` permits
`EffectOutput` by design; `noalloc` fires on ~40% of all functions. So only `pure` is
reported — the other two would bury it. Declarations only, never an inline closure
(`(x) => x * 2` inside an `xs.map(…)` is an expression, not an interface); `main` is exempt,
since nothing calls it and there is no caller for blame to move to; and a bound the *trait*
declares counts as annotated, via an `effectiveMethodBounds` helper shared with the
enforcement half so the two cannot drift.

**Landing it cost more than building it, and that is the entry's real content.**
`std/prelude` was clean of every existing diagnostic and drew **97** W018s — all trait-impl
methods: `Add`/`Sub`/`Mul`/`Div` per numeric width, `Show::show`, `Signed::abs`,
`Ord::compare`, `Needle::found_at`. Those would have appeared on *every user compile*, about
code the user did not write, which is the state no shipped library may be in.

They were marked at each impl rather than by declaring the traits' methods `pure` — one edit
instead of 97, and refused deliberately: a bound on a trait binds every implementer,
including a user's, so that spelling decides no `impl Show for MyType` may ever print. That
is a language decision, not a cleanup, and it is left open. `std/math`, `std/tui` and
`examples/` were annotated the same way; the tree is diagnostic-clean again.

Two gaps the work exposed, both in `todo.md`: the language has **no `#[allow]`-shaped
suppression**, which is why this is a warning rather than an error and why a deliberately
unannotated function cannot say so; and `det`/`noalloc` are inferred already and measured
too noisy to default on, so a `--pedantic`-shaped opt-in is what would make them available
without imposing them.

### 08/17/26
**`lyra-E058`: destructuring the result of an inferred, later-declared return.**
`let (w, h) = contain_set(cols, rows)` above an un-annotated `contain_set` reported
nothing at the destructure and `undefined identifier` at every *use* of the names — a
diagnostic pointing at the line after the cause, while the cause was a missing `->` fifty
lines down.

The boundary is narrow and was mapped before fixing: a scalar return is fine, and binding
the whole tuple is fine — both defer the type. **Destructuring is the one position that
needs the element types immediately**, and a later declaration's inferred return type does
not exist yet, so `checkDestructuringDecl` saw a nil type and returned silently.

Two things made it worse than a plain missing error. The collector knows the names
perfectly well, so a second destructure of them in an inner scope warns that it *shadows*
them — which says they exist. And the house style adopted the same day (main first,
helpers below) is exactly the arrangement that puts an un-annotated helper after its
caller, so this is reachable by writing ordinary code in the documented style.

Deliberately narrow: it fires only for a call to a function that genuinely has no return
annotation, so an undefined callee keeps its own diagnostic. A confident message about the
wrong thing is worse than the silence it replaces.

**The diagnostic is the workaround.** Making inference order-independent — infer a
callee's return type on demand when a destructure asks — is open in `todo.md`, with the
two things to settle (a cycle guard for mutually recursive un-annotated functions, and not
double-reporting a body that is later checked normally).

### 08/17/26
**A conversion of a constant operand is constant** (`typechecker_const.go`).
`const Y_LEN = X_LEN * ASPECT * f64(HEIGHT) / f64(WIDTH)` was refused: a conversion is
spelled as a call, so it landed in the non-constant arm.

The refusal came with advice — write the type as an annotation, `const A: u8 = 200`
instead of `const A = u8(200)` — and that advice is fine for a bare literal and **cannot
express a conversion inside a larger expression**. There is no annotation that rescues
`X_LEN * ASPECT * f64(HEIGHT) / f64(WIDTH)`, which is what made it a workaround rather
than an answer, and what forced the derivation to be a runtime `let` in the example that
motivated it.

Accepting it is safe for a reason already in the backend: **a `const` is inlined as its
value expression** and lowered like any other code (llvm.go's identifier arm), so nothing
about what the program computes changes. The check recurses into the operand rather than
accepting outright, which is what keeps `u8(x)` for a runtime `x` refused.

**Removing the special-case diagnostic improved three messages, which was not the goal.**
It fired before the conversion was type-checked, so it masked whatever was genuinely
wrong: `u8(x)` now names the *variable* rather than the conversion, `string(7)` reports
that `string(...)` only reads a value of that type back out, and `i64(2.5)` reports that
it is lossy and names floor/ceil/round. A diagnostic that pre-empts a more specific one is
worth suspecting.

Not extended to the **integer** folder (`FoldBigExpr`), which serves overflow checks and
array sizes: it walks magnitudes, and folding a narrowing conversion as identity there
would defeat the overflow detection it exists for. So `const N = i64(…)` is accepted as a
const and still refused as an array size — reported at the use rather than the
declaration, which is a corner with no motivating case.

### 08/17/26
**`wait_for_key_ms(timeout: i64) -> bool`** — a fourth terminal builtin, closing the one
case the other three could not: `\e` begins every escape sequence, so a decoder must look
at what follows it, and with only a blocking read a lone Escape waited for the user's
*next* keypress. It now reports immediately, and `tui_viewer.lyra` redraws on a resize
with no key pressed (measured: 40x12 to 60x20, nothing sent).

**A bool rather than the timed read the todo entry proposed, and the reason is a counting
argument.** There are three outcomes — a key arrived, nothing yet, input ended — and
`read_key_timeout(ms) -> Maybe<rune>` has two answers, so it must conflate two. Conflating
"nothing yet" with "ended" is exactly the mistake `read_line`'s `Maybe` exists to avoid: it
makes the natural loop spin forever once stdin closes. Splitting the question needs no new
type at all — this answers "is there anything to read", `read_key` then answers "a key, or
the end".

The pairing is a **property of poll rather than a convention**: a closed descriptor reports
readable, so at EOF this returns true and the read that follows returns `None`. Verified
before the design was committed to, not after — a poll of a pipe whose write end is closed
answers POLLIN|POLLHUP and the read then returns 0.

Two smaller decisions. The timeout is **clamped into [0, INT_MAX]** rather than rejected:
`deadline - now()` naturally goes negative once the deadline has passed, and "do not wait"
is the meaning there rather than an error, so this is not the silent reinterpretation the
language usually refuses. And unlike `terminal_size`, this needs **no `runtime.GOOS`** —
`struct pollfd` is `{int, short, short}` and POLLIN/POLLERR/POLLHUP are 0x1/0x8/0x10 on
both targets.

**A fifth member of the "block value vs statement" family fell out of using it.** Writing
the viewer's polling loop produced `if !available { if resized { redraw() } } else { … }`,
refused for wanting an `else` on the inner one-armed `if`. `checkIfExpr` took its
`requireType` flag for the *mismatch* check but still inferred both branches as values.
It now routes them through `checkExprForEffect` in statement context — which subsumes the
hand-rolled else-if special case it replaced, and more evenly: that one propagated
statement context only through a bare `else if`, so a braced `else { if … }` still put its
tail in value position.

With that, the family finally has **one** answer rather than five: `checkExprForEffect` is
what `checkBlockForEffect`, `checkMatchExpr(false)` and `checkIfExpr(false)` all use. That
is hazard 8's durable fix — stop having more than one of it — rather than a fifth copy
that happens to agree today.

**Three `std.tui` demo programs**, and the four bugs writing them turned up.
`examples/tui_palette.lyra` is the style half alone (pipeable, touches no terminal
state), `tui_events.lyra` the input half as a readable transcript, and
`tui_viewer.lyra` a full-screen skeleton — one-write frames, size read per frame,
arrows and mouse both moving a marker. All three were driven through a pty to check
they actually run rather than merely compile.

`tui_events` earns its place by showing *why* the decoder exists: it reads three code
points with the raw builtin first, so an arrow key prints as `<27>`, `'[' (91)`,
`'A' (65)`, and then the same key as `Down`.

**`if b { f() } else { g() }` over two `void` functions did not compile at all.** It
emitted `phi void` and clang refused the module outright — six ordinary lines. `match x
{ 0 => f(), _ => g() }` had the identical bug, found by checking the sibling as this
project's notes say to.

The cause is that **"produced no value" has two spellings and both merges tested only
one**: a builtin hands back a nil, while a call to a *user-defined* `void` function
hands back the `ir.Call`, non-nil with a void type. Both guards' comments already
anticipated void branches; neither anticipated that one of them is not nil. Fixed with a
shared `isVoidResult` in both walks in one change, since a partial fix here is a phi
that is well-formed on one construct and not the other.

**A one-armed `if` was illegal as the tail of a statement-position match arm** —
`match e { Up => { if c { … } }, _ => { } }` refused with "`if` used as a value must
have an `else` branch", naming a value nobody wanted. `checkIfExpr` has taken a
`requireType` flag all along for exactly this; `checkMatchExpr` did not, so its arms
were always checked in value position. It takes one now, and the statement path routes
arm bodies through a new `checkExprForEffect` — the value-optional twin of
`inferExprType`, recursive so a nested statement `match` gets the same treatment.

**That is the fourth member of the "block value vs statement" family**, and the set is
worth reading together: `checkBlockForEffect` (loop bodies, earlier), `checkBlock`'s
double-checked tail (`lyra-W006` on a returned value, 08/15), `checkLValueAssignment`'s
missing context (08/15), and this. Each was a construct that had to decide whether its
tail is a value or a statement, and each got the answer from a different place.

Two smaller things the demos surfaced, both documentation rather than compiler bugs:
`_ => ()` is the **empty tuple**, not void, so mixing it with void arms is a type error
and `_ => { }` is the no-op arm; and a bare assignment is not a valid arm body
(`None => running = false` needs braces). `std.tui`'s own module-doc example had both
wrong, which is the argument for examples that compile.

### 08/15/26
**`std.tui`** — `event.lyra`, `key.lyra`, `mouse.lyra`, `screen.lyra`, `style.lyra`: an
`Event` type and the escape-sequence decoder, named keys, SGR mouse reporting,
alternate-buffer/clear/cursor control, and 256-colour styling. All ordinary Lyra over the
three builtins below, which is the division those were built for.

**The mouse needed nothing from the compiler either**, and that is the part worth
recording. A terminal reports clicks as escape sequences *on stdin*, so enabling is a
`print` and receiving is `read_key` — the same two halves the keyboard uses. Verified
before it was designed rather than after: an injected `\e[<0;10;5M` came back through
`read_key` as `27 91 60 48 59 49 48 59 53 77`, every byte intact.

**SGR mode is a requirement, not a preference, and the reason is an interaction between
two decisions taken independently.** The legacy X10 encoding is `\e[M` plus three *raw
bytes*, each its value plus 32 — and `read_key` answers a code point, so it decodes
UTF-8. A column past 95 therefore produces a byte of 128 or more, which is a UTF-8 lead
byte: measured, `\e[M` at column 200 came back as `27 91 77 32 35137 66`, with the column
*and* the row swallowed into that one bogus code point. SGR (`\e[?1006h`) encodes the
same fields as ASCII digits, so every byte stays below 128 and the decode is exact at any
terminal size. `mouse_enable` always requests it and offers no way not to. This would
have surfaced as a coordinate bug on wide terminals only.

**`next_key` became `next_event`** (and `KeyReader`/`key_reader` became
`EventReader`/`event_reader`) the same day, before anything depended on it. Terminal
input is genuinely *one* stream — keys and mouse reports interleave on one file
descriptor, told apart only by their bytes — so there is one reader and one state
machine, and a `next_key` that could return a click would have been a name that lied.
`Event = Keyboard Key | Mouse MouseEvent`.

Two smaller decisions. Motion reporting is a **separate enable** (`mouse_enable_motion`)
rather than a default, because a drag across the screen produces an event per cell where
a click produces two. And `mouse_disable` must be paired like `alt_screen_leave`, or the
terminal keeps emitting reports into the user's shell, where a click arrives as garbage
text.

**Writing it turned up one thing the builtins cannot do, and it is worth recording as a
correction rather than a footnote.** The claim that three builtins are the whole of what
a TUI needs from the compiler is very nearly true; the exception is a **lone Escape**.
`\e` begins every arrow and navigation key, so a decoder must look at the next code
point to know what it has — and `read_key` blocks, so a bare Escape waits for whatever
is pressed next. The decoder is *correct* under this (it buffers the lookahead, so
nothing is lost and Escape is reported one keypress late rather than wrongly) and arrow
keys are unaffected, their bytes arriving together. Resolving it properly needs a timed
read, whose signature is a real design question — `None` would then mean both "no key
yet" and "end of input", the conflation `read_line`'s `Maybe` exists to avoid — so it is
an open entry in `todo.md` rather than a fourth builtin added in haste.

Two API decisions worth keeping. **Styles return their escape sequence, screen commands
perform theirs**: a colour is part of the text it colours, so it composes into a frame
printed once, while clearing the screen has nothing to compose into and a version
returning a string you must remember to print would be a trap. And **every pair is
width-first** — `move_to(col, row)` matches `terminal_size()`'s (columns, rows) and is
the reverse of the `\e[<row>;<col>H` underneath; one place has to flip, and better there
than at every call site.

**Two compiler bugs fell out of writing it**, both pre-existing and both found only by
using the language rather than testing it:

- **`lyra-W006` fired on a value that was returned** (`typechecker_control_flow.go`).
  `checkBlock` walked *every* statement as a statement and then additionally treated the
  last as the block's value, so the tail was checked both ways and any `Maybe`-returning
  call in the tail of an `if` branch or a **braced** match arm was told to bind or match
  its result. `checkBlockReturn` — the same walk for a function body — had the exemption
  all along, which is exactly why a bare block body was fine and the identical expression
  one brace deeper was not. Hazard 8's "two helpers, one question" shape, and the brace
  was the whole difference: an *unbraced* arm always worked.

- **Assigning to a struct field of type `Maybe<…>` did not lower at all** —
  `llvm: unknown named type "Maybe"` from four lines of Lyra. A nullary construction like
  `None` solves no type parameter, so it stays the bare declaration until a context
  completes it (`propagateInstantiation`), and `checkLValueAssignment` was not one of the
  sites applying that context. The comment sitting there said so — "this path does no
  literal propagation at all … whether it *should* is a separate question" — a deferred
  question that was already a bug. Same failure the return position had before 08/05, one
  statement form over; the fix is the same `contextualType` call, which also has to run
  *before* the assignability check because a partly solved struct is not assignable to an
  instantiation of itself until the context has completed it.

  Worth recording how it was mis-diagnosed: the first repro was cross-module and the
  same-file version appeared to work, so it looked like a module-resolution bug for
  several minutes. The same-file test had only *read* the field, never assigned to it.

**The terminal: `set_raw_mode`, `read_key`, `terminal_size`** (`pkg/backend/llvm/tui.go`).
The three builtins an interactive TUI needs, and the only three — everything layered on
them (escape-sequence decoding, colour, boxes, frame diffing) is expressible in Lyra and
so belongs in `std.tui`, which is the `read_line`/`parse_i64` line exactly.

**Only the input half needed the compiler.** `\e` already reached stdout as byte 27, so
ANSI colour, cursor positioning and the alternate screen buffer were always ordinary
`print` calls. What no amount of prelude code fixes is that `read_line` waits for Enter:
a keypress is not observable until the terminal's line discipline releases it, and
turning that off is `tcsetattr`.

**Two of the three dodge the platform question rather than answering it, and that was
the design work.** `struct termios` genuinely differs between macOS and glibc — 4- versus
8-byte flags, an extra `c_line`, NCCS 20 versus 32 — so masking the raw-mode flags here
would have put *field offsets* in the backend, which is the kind of constant that stays
plausible while being wrong. Going through `cfmakeraw` means the struct is only ever
carried, never indexed, so one over-sized opaque buffer covers both. `struct winsize` is
four `unsigned short` everywhere, so only its ioctl selector was ever in question.

That leaves **one** platform-dependent constant, `TIOCGWINSZ` (0x5413 on Linux,
0x40087468 on macOS — its ioctl numbers encode direction and payload size, Linux's do
not), chosen from `runtime.GOOS` at emission time. This is the first place the backend is
not platform-neutral, which is worth flagging rather than burying: it is sound because
`lyrac` hands its IR to the host's clang with no target flag, so the compiling host *is*
the target, and that holds inside `asan.sh`'s Debian container too.

**`read_key` answers a code point, not a byte**, because `rune` means a code point
everywhere else — `s[i]` and `for c in s` both walk them — so a multi-byte character is
one key rather than two mojibake ones. It reuses `lyra_utf8_decode`, the routine string
iteration already uses. An **escape sequence is deliberately not decoded**: an arrow key
arrives as ESC, `[`, `A` in three reads, and assembling those needs a timeout to tell a
real ESC press from the start of a sequence, a table, and a policy for unknown sequences
— none primitive, all expressible.

**The three do not classify alike, although they look like one feature.** `read_key` and
`terminal_size` are EffectInput; `set_raw_mode` is EffectOutput and therefore `det`-legal,
because it changes the world rather than reading it and does so deterministically. So a
`det` function may enter and leave raw mode and may not read while there — the same pair
of answers `print` gets. `terminal_size` is the one that looks like it should be `det`:
it reads no input and returns the same pair all day, but the window can be resized
between two calls, and a viewer that redraws on resize is built on exactly that.

**The original termios is saved in a module global and restored on disable**, rather than
returned as a token for the caller to hand back. A terminal left raw after exit is
unusable — no echo, no line editing, Ctrl-C dead — so restoring must not depend on the
program having kept a token safe. `terminal_size` likewise never fails, answering 80x24
when there is no window, which is clock.go's rule (a failed syscall leaves a *defined*
value) and keeps a piped run rendering instead of dividing by zero.

**What the suite guards, and what it cannot.** A test process has no controlling
terminal, so the tty-only behaviour is not asserted — but a pipe delivers bytes to `read`
immediately, so the whole of `read_key` except the blocking is exercised for real,
including the multi-byte assembly, the truncated-sequence `None` and the EOF `None`. The
tty half was verified by hand through a pty (`pty.fork`, `TIOCSWINSZ` set to a
distinctive 123x45): the size came back 123x45 in that order, `abcé` arrived as four keys
with no Enter and no echo, and after `set_raw_mode(false)` the terminal echoed again and
`read_line` waited for Enter. Both halves also pass under `asan.sh`, which is what
actually exercises the Linux selector and proves the variadic `ioctl` survives Debian
clang's typed pointers.

### 08/15/26
**A capitalized binding name says so (`lyra-E057`).** `let RAMP = [" ", "."]` reported
`cannot destructure StaticArray<string, 2> with a data pattern`, and then an
`undefined identifier "RAMP"` at every use. Both lines are true and neither is the
mistake.

Capitalization is **syntax** here, not convention: a SCREAMING_CASE name is the grammar's
`const_identifier` (`/[A-Z][A-Z0-9_]*/`) and a capitalized one a constructor, so either in
binding position parses as a *pattern to match*. The value is then destructured against a
constructor that does not exist, and the diagnostic describes that parse instead of the
one-word fix.

Two halves, because the fixes differ and cannot be given uniformly: `const RAMP = …` for
the first spelling, a lowercase initial for the second — `const Foo = 10` is a *syntax*
error, so offering `const` for `Foo` would be advice that does not compile. The check
mirrors the grammar's character class literally rather than approximating it as "all upper
case", for exactly that reason.

Three things kept it narrow. It fires only on a **bare** pattern (`p.Pattern == nil`), so
`let Some(x) = n` is untouched; only when the name owns **no constructor**, so
`let None = 10` keeps the shape mismatch, which is what is genuinely wrong with it; and it
is reported **before the value's type is resolved**, since the mistake does not depend on
it — otherwise `let RAMP = Some(5)` reaches the next arm and is told `RAMP` is not a
constructor of `Maybe`, equally true and equally unhelpful.

The name is bound anyway, so one mistake draws one diagnostic — the same best-effort the
struct-pattern arm beside it already took, its comment already citing spurious
`undefined identifier` cascades as the reason.

Found by writing `examples/mandelbrot.lyra`, which is what the example programs are for:
every constant in that file is `const`, and the shading ramp was the first `let` to be
given a name in the same house style.

### 08/14/26
**A top-level `let`/`var` holding data lowers.** It type-checked clean and died in the
backend as `llvm: unbound identifier "greeting"` — hazard 5 inverted, and a fairly basic
gap: module-level state did not exist, and `const` was the only way to give a module a
string.

Nothing collected it. `const` was recorded for inlining, functions went through
`forEachUserFunction`, and a top-level binding that was *neither* fell through both — so
the failure was not a wrong answer but a missing one, reported at every use rather than at
the declaration.

They now get a module-level slot, zero-initialized, **filled at the top of `main` in
declaration order**. `main` is the one place guaranteed to run before anything reads one,
which avoids `llvm.global_ctors` and the cross-translation-unit ordering it would drag in
for a language with a single entry point. Declaration order is the initialization order, so
a global may read an earlier one — the same order the use-before-declaration checker
already enforces on the way in, which makes this an existing policy honoured rather than a
second one invented.

A managed global is a ref-counted box the global owns for the program's life and never
releases: there is no scope for it to leave. A fixed leak, the trade every language makes
for module-level data, and the reason the ownership pass is not consulted — it reasons
about scopes, and a global has none.

**Four sites asked "where does this name live?", and each answered with `l.locals`
alone** — reading an identifier, assigning, reassigning a whole binding, compound-assigning.
Adding globals meant the same three lines in four files, which is the drift hazard 8 exists
to name, so they go through one `slotFor` instead. The ordering is the part that must not
vary: a local shadows a global, and a fourth site quietly disagreeing would produce a wrong
*value*, not an error. Found by fixing them one at a time as each new failure appeared —
plain reassignment, then compound assignment — which is the shape that says "stop patching
and consolidate".

**Found while probing whether a TUI is possible.** It is, on the display side, with nothing
from the compiler: `\e` and `\x1b` both reach stdout as byte 27, so colours, cursor
positioning and the alternate buffer are ordinary `print` calls. An escape-sequence table
is the natural thing to write as module-level data, which is exactly what did not compile.
What a TUI still lacks is *input* — `read_line` is line-buffered, and raw mode and terminal
size want `tcsetattr`/`ioctl` — which would be three builtins on the `read_line` model
rather than FFI. See `todo.md`.

### 08/14/26
**A fixed-array receiver against a `[]t` combinator names the edit.** `["a", "b"].join("")`
reported *"member access on non-struct type StaticArray<string, 3>"* — the type that failed,
never the one that would work, and nothing about the annotation that fixes it.

Now: *"join takes a dynamic array — annotate the value as `[]string` (a `[3]T` literal is a
fixed array, and widening it would allocate)"*.

**Checked before the overload branch, because it applies to both shapes and is the only one
that names an edit.** An overloaded name has said what it takes since the hint existed
(*"map is overloaded on its receiver and takes DynamicArray<t>, Maybe<t>, Result<t, e>"*) —
true, and it leaves the reader to work out that an annotation is the answer. `map` and
`filter` are what people reach for, so the overloaded shape was the one that most needed it.
A single-declaration name said nothing at all, and now says what it takes.

**The suggested type is defaulted before it is named.** An unannotated `[1, 2, 3]` has
untyped elements, which render as "integer literal" — a phrase, not a type — so the first
version suggested `[]integer literal`, an annotation that does not compile. Same rule the
generated documentation follows: a spelling offered to a reader has to parse. There is a
test asserting the suggestion is `[]i64`, and another that taking the advice compiles, which
is the only one that proves the hint is *correct* rather than merely well-worded.

**The rule itself stays.** Auto-widening at the call is the obvious fix and is wrong: a
`[N]T` is a stack value and a `[]T` a heap box, so widening allocates — silently, at a call
site, where `noalloc` exists to make exactly that visible.

The tests needed moving mid-write, which is worth recording: the first draft lived in the
typechecker's own suite, where `parseCollectAndCheck` has no prelude — so `join` and `map`
are undeclared, the hint has nothing to look up, and every assertion passed against a bare
"member access on non-struct type". They now declare their own receivers to test the
mechanism, with the real prelude covered separately by `checkWithPrelude`.

### 08/14/26
**`join` in the prelude** — `split`'s inverse, and the last of the frame-buffer ergonomics.

Generic over the element rather than taking `[]string`, so a list of anything printable
joins without being converted first: `[1, 2, 3].join(", ")` works, and a `[]string` pays
nothing for the generality because `impl Show for string` returns the value rather than
interpolating it. The separator goes *between* parts, so an empty array joins to `""` and a
single element to itself.

**It is an ergonomic win and not a performance one**, which the doc comment says outright:
each `++` copies everything accumulated, and the language has no way to allocate a string of
a known size and fill it, so this is the same quadratic a hand-written loop is.

**It was 2x worse than the loop until a pair of parentheses**, which is the part worth
keeping. `out ++ sep ++ part` associates left, so it copies the whole accumulator *twice*
per element — once for the separator, once for the part. `out ++ (sep ++ part)` builds the
small piece first and copies the big one once: 825 µs → 323 µs over 600 parts, against
347 µs for the hand-written loop it now matches. Measured rather than assumed, and the
2x would have shipped invisibly.

A linear `join` needs a primitive that does not exist — a builder, or a builtin that sums
the parts' byte lengths, allocates once and memcpys. The precedent is `starts_with`, quadratic
in its natural Lyra form and rewritten onto `byte_len`/`compare_bytes_at` for 19.9 ms → 19 µs.
Left for when something joins enough parts to notice.

**Writing it surfaced a pre-existing gap** (`todo.md`): `["a", "b"].join("")` does not
compile, because an array literal infers a fixed `[2]string` and every prelude combinator
takes `[]T`. `[1, 2, 3].map(f)` fails identically, so it is general rather than about
`join` — and it went unnoticed because every example in the standard library annotates.
Auto-widening at the call is the obvious fix and probably wrong: `[N]T` is a stack value
and `[]T` is a heap box, so it would allocate silently at a call site, which is exactly
what `noalloc` exists to make visible.

### 08/14/26
**An impl's body is optional, braces and all** — `impl Arithmetic for Vec2` — matching the
trait declaration, so an umbrella's pair reads as a pair:

```lyra
trait Arithmetic: Add + Sub + Mul + Div
impl Arithmetic for Vec2
```

The *methods* were already optional; this drops the `{}` left standing around nothing. Same
reasoning as the trait's, and the same reason Rust cannot follow: there is no body to
delimit, and a language with a statement terminator does not need a brace to say where a
declaration ended. The thirteen `impl Arithmetic for <width> {}` lines in the prelude are
what make it worth having rather than merely consistent — all thirteen lost their braces, as
did `Complex`'s.

The ambiguity is the trait's, and the terminator settles it identically: `impl Marker for
Vec2` followed by `{ 1 }` on the next line is an impl plus a block statement. Worth checking
rather than assuming, because this rule carries `prec.right(PREC.TRAIT_IMPL)` and greedy
absorption was the plausible failure. Pinned by a corpus test.

Cost: **−4 states** (7,825 → 7,821). Making a required brace optional shrank the automaton —
the same shape the `for`-condition widening had, a narrower rule replaced by one the parser
was already tracking.

**And the question it was asked in service of is settled**: the umbrella impl stays
*required*. The compiler cannot distinguish a conjunction (`Arithmetic`) from a promise
(`Currency: Add`) — identical in shape, and auto-satisfaction would make every `Add` type a
`Currency`. With no orphan rule nobody is ever blocked from writing the impl, so the cost is
one line, and that line is a checked assertion rather than ceremony: lyra-E040 verifies the
claim where it is made. `todo.md` records what would reopen it.

### 08/14/26
**A NaN prints as `nan` on every platform.** It printed `-nan` on Linux and `nan` on macOS:
glibc renders a NaN whose sign bit is set with the sign, Apple's libc does not, and the
float formatter had been handing the question to libc — its own comment said so, that a NaN
"simply runs to the last rung and prints libc's `nan`".

**This is a determinism bug, not a test bug**, which is why the fix is in the formatter
rather than in the assertion. A program's *output* depended on the platform it was built
for, in the language that removed platform-dependent integer widths to avoid exactly that.
Caught by CI on Linux, four runs after it landed, because every test asserting a printed
NaN had been written on macOS.

**The sign is dropped rather than standardized on.** IEEE leaves the sign of a NaN
unspecified for nearly every operation that produces one — `log(-1)`'s is a property of the
libm implementation, not of the computation — so it carries nothing a program could act on.
An infinity keeps its sign, which does.

Handled before the precision ladder rather than as another rung: a NaN never compares equal
to itself, so it would otherwise run every rung to reach the last one and print whatever
libc said there.

**Verified on Linux before pushing, not after.** `./asan.sh ./...` exists for this and had
not been used all day — the whole suite passes in the Debian container, which is what turns
"I think this is platform-independent" into a fact. Four red CI runs is the cost of not
having asked earlier; the tool was there the entire time.

### 08/14/26
**`[v; n]` accepts a runtime count, building a dynamic array** — the buffer a window resize
or a terminal width sizes, which had no spelling at all: `let buf: []u32 = [0; n]` was a
*syntax* error, and `push` in a loop was the only way to build one.

**The restriction was right for one form and inherited by the other.** A fixed array
carries its length in its type, so `[3]T` cannot depend on a value the compiler has not
got — that is real, and unchanged. A `[]T` carries its length at run time and needs nothing
static, and it had the constant rule only because the two share a spelling.

So the grammar's `array_repeat_count` went from `choice($._number_literal,
$.const_identifier)` to `$.expression`, and the rule moved to the typechecker — which is
the only place it can live, because *which* of the two `[0; n]` builds is decided by the
type it is checked against, and the parser cannot see that. This is the split `rangeBounds`
already documents: the grammar refuses what has no meaning anywhere, the checker refuses
what has a plausible meaning in the wrong place, and gets to name the fix (`lyra-E056`).

**A runtime count infers `[]T` and that is not a preference.** No fixed type can describe
it, so the fall-through is the only inhabitable answer — the same way `[1, 2, 3]` infers
`[3]T` with no context. Each form infers the one type it can have.

Four things it needed beyond the obvious:

- **A second shape in the assignability check.** A constant count infers a fixed array that
  the annotation *widens*; a runtime one infers `[]T` outright, so the pair is
  dynamic-to-dynamic and `arrayWideningPair` — which requires a static source — answers
  false. Without an arm for it the element never narrowed and `() -> []u32 => [0; w * h]`
  reported *"expected DynamicArray<u32>, got DynamicArray<integer literal>"*: an element
  type with nothing to pin it.
- **The E056 report had to come from the context hook**, not from the assignability
  failure. Left there it reads "cannot assign DynamicArray<integer literal> to
  StaticArray<u32, 3>" — two types, neither of which is the problem. The count is.
- **A negative count traps** (`lyra_panic_negative_length`), the same rung a shift amount
  and a range step ride: refused at compile time where it is constant, trapped at the only
  moment it exists where it is not. Zero is fine and yields an empty array.
- **A non-integer count is checked on the runtime path**, which is the only thing that
  checks it — the constant path proves it by folding. This also *improved* an existing
  message: `const K = "no"` used to report "K must be a `const` whose value is a
  compile-time integer", which mis-stated the fault, since K is a const. It now says the
  count must be an integer and got a string.

Cost to the grammar: **zero new states** (7,825 → 7,825) and +31 KB of `parser.c`.
`expression` was already reachable in every neighbouring position, so widening the count
added no automaton the parser was not already carrying.

### 08/14/26
**`[v; n]` is emitted as a loop above 64 elements**, where it used to unroll into n stores
at any size.

One store per slot is better code for `[0; 3]` — no counter, no branch, and the optimizer
sees straight through it — and it is a **compile-time bomb** at any size a frame buffer
reaches, because the IR then grows linearly in n. `[0; 200000]` produced a 43 MB `.ll` file
and clang had not finished with it after five minutes. Nothing diagnosed it: the build
simply never returned, which is the worst shape a performance cliff can have.

Now 56 KB of IR regardless of the count, and that same 480,000-element buffer compiles and
runs in 0.43 s.

Two things the fix had to preserve. **A managed element is retained once per slot beyond
the first** — every slot is an owner, so a count one low is a use-after-free and one high is
a leak — which means the retain loop runs over `[1, n)` while the store loop runs over
`[0, n)`. And the **unrolled path stays** below the threshold, because it is genuinely
better there and every repeat literal written by hand is smaller than 64.

`emitCountedLoop` is a helper rather than a third copy of the pattern: this file already
walks n slots at run time in the array drop glue, and a back edge wired wrong produces IR
that verifies and does not terminate.

Found by measuring frame-buffer strategies rather than by reading — the 5-minute hang was
the first symptom, and it looked like an infinite loop in the *program* until the IR size
gave it away.

### 08/14/26
**`sqrt`**, joining the logarithms — and the two tables that held them became one.

`floatLogOps`/`logIntrinsicOps` were named for what they happened to contain rather than
for the shape they share: one float in, one float of the same width out, lowered to
`llvm.<name>.<width>`. A square root is the same question, so a second pair of tables keyed
the same way is the drift hazard 8 keeps cataloguing. They are `floatUnaryMathOps` and
`floatMathIntrinsicOps` now, and adding `exp` or `sin` later is one line in each and
nothing else — where under the old naming it would have been a third table, or a lie in a
name.

Renamed the day after the logs landed, which is the cheapest this ever gets: a table named
for its contents is only wrong once something else belongs in it, and the moment to fix it
is the moment that happens.

`sqrt(-1)` is a NaN rather than a trap, matching the logarithms and float division. What
the escape-time renderer needs it for is nothing — `log(|z|^2)/2` avoids the root entirely
— but a distance estimator cannot, and neither can any ordinary magnitude: `|z|` for
`3 + 4i` is the test that says 5.

### 08/14/26
**`++` names the fix instead of only stating the rule**, and deliberately still refuses to
convert.

`line ++ shades[i]` — a string and a rune — reported *"operands must be strings, got string
and rune"*, which is true and leaves the author nowhere: nothing in it says a rune can be
rendered, and `show` is not a word anyone guesses from a character.

**The tempting fix is to make `++` accept a rune, and that is the wrong one.** It would be
an implicit conversion in a language that refuses them everywhere else: `let c: Cents =
plain_i64` is lyra-E046, `i64(x)` on a float is refused in favour of `floor`/`ceil`/`round`,
and `string(r)` on a rune is refused *by name*, its message saying that conversion "only
reads a value of that type back out". An operator quietly performing the conversion its own
conversion function declines is two mechanisms disagreeing about the same question — and
the slope has a known bottom: if a rune converts then so does an integer, and `++` becomes
JavaScript's `+`, which is precisely what a separate concatenation operator buys avoiding.

So the message carries the cost, which is the pattern lyra-E046, lyra-E043 and the float→int
rejection all follow: refuse, and name the spelling. It also names *which side* is at fault,
since "operands must be strings" leaves a reader checking both.

`show` rather than a new `to_string`: it is already the language's stringification, ships
for every printable scalar, and a second name for one mechanism is the redundancy this
project keeps deleting. What it is not is discoverable, which is the whole argument for
putting it in the diagnostic.

Four existing tests baselined the old sentence and were updated — the message is the
feature here, so an exact-match baseline is right even though it makes the diff wider.

### 08/14/26
**The logarithms — `log` (natural), `log2`, `log10`** — each answering the receiver's own
width rather than a fixed one, because a log is a float operation whose answer is a float.

**Builtins on the `random_seed` rule, not the `parse_i64` one.** A logarithm is not
expressible in this language: no series, no lookup table, no FFI to reach libm. Parsing and
formatting are arithmetic and belong in the prelude; this cannot, so it is primitive. They
lower to `llvm.log`/`log2`/`log10`, which become the libm calls of the same name — which is
what `lyrac build`'s unconditional `-lm` has been paying for all along.

**All three rather than the natural one alone.** Smooth mandelbrot coloring is
`n + 1 - log2(log(|z|))`, so `log2` is not a convenience: writing it as
`x.log() / 2.0.log()` costs an extra call and loses accuracy at exactly the magnitudes
shading depends on. `log10` comes free from the same intrinsic family, and having the trio
is what makes the bare name's base unambiguous by contrast — `log` is the one with no
subscript, which is `e`. (Rust spells it `ln` for that same ambiguity; the trio answers it
differently.)

**Outside the domain they answer IEEE's value rather than trapping** — `log(0)` is `-inf`,
`log(-1)` is a NaN — which is the choice float division already makes. The trap comes later
and in one place: feeding either to an integer conversion is what fails, which is where
`guardFloatToInt` sits. One check, at the boundary where the value has to become something
a machine integer can hold.

Verified by rendering a Mandelbrot set with smooth shading, which is the point of the
feature and exercises the day's whole stack: `log`/`log2` for the escape value, `to_fixed`
for the readout, the float→int guard on every palette index, and a `pure` inner loop. The
set comes out recognizable and symmetric about the real axis.

The two tests that failed first were both **the test's arithmetic, not the compiler's**:
`log2(ln(16)/2)` is 0.4712 rather than 1, and a `break` before the counter advances puts
the smooth value in [1, 2) rather than [2, 3). Worth recording because the instinct on a red
test is to suspect the code under it, and here the code was right twice.

### 08/14/26
**An out-of-range float→int conversion traps instead of answering a number.** `fptosi` is
**poison** in LLVM for an operand no integer can hold — not a saturating conversion — so
`(1.0e20).floor()` answered 0 under `lyrac run` and `-9223372036854775808` under the test
harness's compilation, which is the tell: the value was whatever the optimizer left behind,
and two optimization levels were free to disagree. A NaN converted the same way.

**It was the one gap in this language's numeric ladder.** Integer overflow traps, an index
out of bounds traps, an out-of-range shift traps, a violated `newtype` constraint traps —
and then the one conversion that cannot represent its input quietly produced a plausible
number. That is the quietest possible failure, because a wrong number reads as arithmetic
rather than as a fault: `to_fixed`'s first draft rendered `1.0e20` as
`9223372036854775807.9223372036854775807` and looked entirely credible doing it.

`guardFloatToInt` traps unless the rounded value is in `[-2^63, 2^63)`. Three details:

- **The upper bound is exclusive.** i64's minimum is exactly -2^63 and representable as a
  float; its *maximum* is 2^63-1, which is not representable in binary64, the nearest float
  above it being 2^63. So an inclusive check against a float spelled
  `9223372036854775807` compares against 2^63 and admits a value one past the end — an
  off-by-one that only exists because the two bounds have different representability, and
  which a test at the boundary is the only way to catch.
- **A NaN traps for free**, because the check is written as "trap unless in range" with
  *ordered* comparisons, false for a NaN. The negation — trap *if* out of range, with
  unordered compares — lets a NaN through to a conversion that is poison for it too. Same
  condition, opposite polarity, one of them wrong.
- **Trapping rather than saturating**, of the two defensible answers (Rust's `as`
  saturates). The rest of the ladder traps, and a saturated coordinate quietly rendering
  the wrong pixel is what this exists to prevent — which is exactly the program that is
  coming.

Cost is a compare and a branch per `floor`/`ceil`/`round`. The value-range pass does not
elide it: everything that pass already elides is integer-valued, so this wants float range
tracking rather than a new consumer of what exists. Left open in `todo.md`.

### 08/14/26
**`to_fixed` — a float rendered with a chosen number of decimal places**
(`std/prelude/format.lyra`), because `print` deliberately has no precision knob and a
status line needs one.

The built-in formatter writes the *shortest rendering that reads back as the same value*,
which is the right default for inspecting a number and the wrong one for a column of them:
`1.0 / 3.0` needs seventeen digits to round-trip, so it prints them, and a small magnitude
switches to scientific notation (`1.234e-06`). `zoom.to_fixed(4)` gives `0.3333` and never
switches notation.

Written in Lyra on the `parse_i64` rule — it is arithmetic over an integer and a string.
The one non-obvious choice is that the **whole and fractional parts are scaled
separately**: multiplying the whole value by `10^places` overflows i64 for anything past
about 9.2e18/10^places, so splitting first bounds the multiplication by `10^places`
however large the number is.

**Its first draft printed a confident wrong answer, and that is the finding worth keeping.**
`1.0e20.to_fixed(2)` rendered as `9223372036854775807.9223372036854775807`, because
`(1.0e20).floor()` does not trap — it answers **0**, LLVM's `fptosi` on an out-of-range
operand being poison. So the language that traps on integer overflow, out-of-bounds
indexing, out-of-range shifts and violated newtype constraints quietly returns zero from
the one conversion that cannot represent its input. `to_fixed` guards its own call and the
underlying hole is filed in `todo.md`; the guard is written as a negated `<` so a NaN takes
the same branch, every comparison with one being false.

**A method now resolves on a bare literal receiver**, which the feature needed and which
turned out to be a gap of its own. `builtinMethodSignature` promotes an untyped literal
internally, so `1.5.floor()` has worked since literals became postfix heads (08/06) — but
trait dispatch and UFCS run *first* and saw `untyped_float`, matching no impl and no
`self: f64` parameter. Every prelude function over a float was therefore unreachable from
the literal a reader would naturally try it on: `(2.5).abs()` and `(1.0 / 3.0).to_fixed(4)`
reported *"float literal has no method"* while the identical call on a `let`-bound `f64`
worked. The receiver is pinned to its default width once, before all three paths, and the
builtin path's local promotion is gone — it was the reason only one path could see through
a literal.

It is the same gap the 08/06 grammar change closed one layer up. That change made a literal
a postfix *head* so `"abc".len()` would parse; this one makes the thing it parses into
resolve.

### 08/14/26
**`Signed` in the prelude, and `Complex` prints `1 - 2i`.** The formatter had no way to
choose the sign, so a negative imaginary part read `1 + -2i`.

**The conventional fix does not work here, and finding out why is the useful part.** Both
`Zero` and `Default` are `() -> Self` — no receiver — and Lyra dispatches on a receiver and
nothing else: `let n: i64 = Zero::zero()` reports *"expected a receiver argument"*, with the
annotation sitting right there. Picking an impl from the expected type is
return-type-directed dispatch, which the language does not have and which `lyra-E035`
already declines from the other side. So the question was never "Zero or Default" — it was
receiver dispatch versus return-type dispatch, and only one of them exists.

`Signed` asks a *value* about itself instead: `is_negative` and `abs`, both receiver
methods, both dispatching through machinery that already works. `abs` is in the trait
because the formatter needs it — printing `1 - 2i` means rendering the magnitude, and
without it the branch produces `1 - -2i`, which is worse than what it replaced.

Two decisions worth keeping:

- **Unsigned integers implement it**, answering `false` and returning `self`. The answers
  are constant, which reads oddly against the name — but excluding them would make every
  generic bounded by `Signed` silently drop half the numeric widths, and a formatter that
  works for `i32` and not `u32` is the worse trade.
- **`abs` documents its trap.** A signed integer at its minimum has a magnitude one larger
  than the type's maximum, so `abs` overflows there for the same reason `-x` does.

The general cost of having no return-type dispatch is recorded in `todo.md` rather than
worked around further: there is no way to name the additive identity of a generic numeric
type, so a generic `sum` has no seed. The trigger to build the mechanism is `From`/`Into`,
where no receiver formulation exists at all — a conversion is inherently directed by what it
produces — and it brings `Zero`, `One` and `Default` with it when it lands.

### 08/14/26
**A generic impl is selectable as a candidate.** One cause behind three symptoms: a bound
dispatch to an operator, a bound dispatch to a method, and a nested generic impl all failed
in the backend with internal messages naming an AST node.

The candidate tables are keyed by each impl's **written** target. That is exactly right for
a concrete impl — `impl Show for i64` keys `i64`, and a specialization looks up `i64` — and
can never match for a generic one: `impl Add for Box<t>` keys the literal string `Box<t>`
while the specialization looks up `Box<i64>`. Every generic impl was therefore invisible to
every bound.

The missing keys are published where a concrete type is first known, which is the
instantiation. `publishCandidatesAt` is the only place both halves of the question are in
hand — the callee's body holds the bound-dispatched sites, and the instantiation fixes the
type — so the match stays in the typechecker, which is what `Resolution` exists to keep.

**Four things it needed, each of which looked like the whole fix until the next appeared:**

- **The monomorphized key.** A generic struct is monomorphized before lowering, so inside a
  specialization the receiver is named `Box$i64`, not `Box<i64>`. The mangling is
  *recursive*: `Box<Box<i64>>` is `Box$Box_i64` and not `Box$Box_i64_`, because the backend
  substitutes inner arguments before naming the outer one. `typetable.MonoTypeKey` lives
  beside `TypeSymbol` and `mangleTypeName`, since a second copy of a naming scheme is a
  silent miss the day any of them changes — and this one differs by a single character.
- **The trait that declares the method, not the one the bound names.** Under
  `where u: Arithmetic` the umbrella declares nothing and `(_+_)` comes from `Add`. Asking
  for it on `Arithmetic` matches no impl, publishes nothing, and is indistinguishable from
  having done nothing at all — which is how it presented: `Box where t: Add` worked and the
  identical `Box where t: Arithmetic` did not.
- **Filtering sites by the supertrait closure**, for the same reason.
- **Recursion into the selected impl's body.** A published candidate may itself select a
  generic impl with bound sites one level down. Both concrete-dispatch paths hook it too,
  operator and method: fixing one leaves the identical failure under the other spelling,
  which is hazard 8's shape yet again.

The recursion carries an in-progress set. It terminates on its own for well-founded types —
each step strips a layer — but a body reaching itself would otherwise spin, and a
typechecker that never returns reads as a frozen editor rather than a crash.

**What it unblocks**: `std.math`'s `Complex<t>` now works through a generic bound and
nested, not only directly. `twice(Complex { re: 1.0, im: 2.0 })` prints `2 + 4i`, and
`Complex<Complex<f64>>` adds componentwise. A generic `escape<t> where t: Arithmetic` — the
shape a mandelbrot renderer wants — is now writable.

Measured: no meaningful change to `BenchmarkAnalyze_*`.

### 08/14/26
**A `where` bound is no longer satisfied by an impl whose own `where` clause excludes the
instantiation.** `typeImplementsTrait` matched an impl by its target's *head* and stopped
there, so `impl Arithmetic for Complex<t> where t: Arithmetic` made **every** `Complex<x>`
satisfy an `Arithmetic` bound — `Complex<string>` included, which type-checked clean and
died in the backend as `llvm: type not found for *ast.IdentifierExpr`. Hazard 5 inverted.

**The single level was deliberate and documented, and the reasoning had a hole.** The old
comment justified it as "the recursive obligation surfaces when that impl is itself
dispatched" — true for an impl with methods, and false for the one that *is* its
constraint. An umbrella impl has no methods, so nothing is ever dispatched and the bound
check is the only place the question is asked. `std/math/complex.lyra` is the first code
to write one, which is what turned a documented limit into a reachable bug: a limit stays
invisible until something stands exactly where it applies.

The fix checks a matched impl's constraints against the bindings the match produced,
carrying the goals already being proved so a chain returning to its own goal answers *no*
instead of looping — and only that branch dies, so a goal provable another way still is.

Two things that were not in the plan:

- **A binding that is itself a type variable is skipped, not failed.** `impl Arith for
  Box<t> where t: Arith` has its supertrait obligation checked with `t` abstract, and
  answering "t does not implement Add" there would make every constrained generic impl an
  error. Whether `t` holds is the *enclosing declaration's* question — the rule
  checkGenericBounds already states for its own type-variable arm, arrived at again from
  the other side.
- **Nesting terminates on its own**, since each step strips a layer of type argument
  (`Box<Box<i64>>` → `Box<i64>` → `i64`). The in-progress set guards the case that does
  not; it is not what makes nesting work, which is the opposite of what the plan assumed.

The diagnostic names the inner failure — *"u is instantiated at `Box<string>`, which does
not implement Arith — string does not implement Arith"* — because the outer type alone says
nothing about which part of it was wrong.

**Verifying it turned up a separate, pre-existing backend gap** (todo.md): an operator impl
whose target is *generic* does not lower through a bound, or nested. `twice(21.0)` and
`twice(Pt {...})` for a non-generic struct both work, so neither the bound nor the operator
is broken on its own — it is monomorphizing `impl Add for Complex<t>` at a concrete `t`.
Direct use (`a + b` on `Complex<f64>`) works, so the new std module is usable, just not yet
through a generic bound.

### 08/14/26
**The prelude implements the arithmetic traits for every numeric primitive**, and impl
coherence stopped keying on the trait's *name*.

Declaring `Add`/`Sub`/`Mul`/`Div`/`Arithmetic` shipped them applicable to nothing: no
primitive implemented them, so `where t: Arithmetic` could not be satisfied by a number,
and almost every generic numeric bound bottoms out in one. The gap surfaced immediately on
the first real customer — `impl Add for Complex<t> where t: Arithmetic` type-checks, and
then `Complex<f64>` fails with *"f64 does not implement Arithmetic"*. A trait that only
user types can satisfy is not much of a trait.

Five impls each for the ten integer widths and the three float widths, on the `Show`
model: ordinary Lyra, no builtin, written out because there is no way to abbreviate it.
`rune` and `string` are excluded — a code point is not a number, and concatenation is `++`.

**`impl Add for f64 { (_+_) = (self, o) => self + o }` is not the recursion it looks
like.** A primitive is never routed through an impl, so the body is the machine add and
nothing can reach the impl from an expression. The rule that makes operator overloading
safe is exactly what makes these impls writable; Rust's core does the same.

**What it exposed is the more interesting half.** `checkImplCoherence` keyed duplicates on
`{impl.TraitName, target}` — the trait's *name* — so the prelude's `impl Add for i64` and
a program's own `impl Add for i64`, over its own `trait Add`, were reported as duplicates.
That refuses a correct program, and it is hazard 9 verbatim: a name does not identify a
declaration. Dispatch already avoids it by filtering candidates on the resolved
declaration, its comment recording that filtering by name is what let a user's own
`trait Ord` be taken for the prelude's — the same lesson, one function over.

It was reachable before any of this, via a user's own `trait Show` against
`std/prelude/show.lyra`'s `impl Show for i64`, and latent only because nothing had written
that. The prelude gaining thirteen more impls is what turned a latent bug into a failing
test — `TestExec_OperatorImplCannotChangePrimitiveArithmetic`, which declares its own
`trait Add` and `impl Add for i64` precisely to assert primitives ignore impls.

**Cost, measured, because the LSP re-analyzes the prelude on every keystroke**:
`BenchmarkAnalyze_Medium` 7.20 ms → 9.98 ms (+39%), Small +31%, Large +30%, WideTypes +15%.
A CPU profile puts the increase in `cgocall` — tree-sitter parsing a longer prelude — with
no dispatch hotspot, so it is linear in source size rather than a lookup blowup. In
absolute terms an analysis is ~10 ms against a typing-latency budget of tens of
milliseconds, which is why this was accepted rather than trimmed; it is worth revisiting if
the prelude keeps growing, and the answer then is caching the prelude's analysis rather
than shipping fewer widths.

### 08/14/26
**`fixed<I, F>` says it is unimplemented instead of failing as a type error**
(`lyra-E055`). The annotation parses and collects into a real `types.FixedPointType`, and
no pass after the collector knows what one is — so the type was **uninhabitable**: `1`,
`1.5`, `f64(1.5)` and `i32(1)` were each refused, and no spelling could construct a value.

The absence itself was fine. What was not is that it presented as an ordinary type error:
*"cannot assign integer literal to `fixed<16,16>`"* reads as a fixable mistake, so an
author tries `1.5`, then `f64(1.5)`, then `i32(1)`, and gets the same sentence with one
noun changed each time. Three plausible attempts to learn what one diagnostic can say —
the lyra-E035/E052 rule, applied to a type rather than to an expression.

Two implementation notes:

- **Reported in `parseFixedPointType`**, the one place the syntax becomes a type, so it is
  one diagnostic per mention at the mention — E035's rule. And it returns **nil** rather
  than the type, which is what keeps it to one: a nil annotation reads as *absent*
  downstream, so `let x: fixed<16,16> = 1` infers `i64` for the binding instead of stacking
  the assignability failure underneath a refusal that already explained it.
- **`parseArrayType` stopped adding a second error** on a nil element. `parseType` returns
  nil in exactly three cases — a nil node, an unrecognized kind, and this — and reports the
  latter two itself, so `[]fixed<16,16>` was answering one mistake with two errors, the
  second phrased as a compiler-internal note (`parseArrayType: element type is nil`).

Not deleted, because the intent is to build it and the syntax is the part worth keeping:
a value-parameterized `fixed<I, F>` commits to binary scaling, which serves *determinism*
(lockstep simulation, replays). Decimal money wants a different type and already has a
better answer (`newtype Cents = i64` with a range constraint), so the grammar has made
that choice already. What it does not settle is what arithmetic does to the parameters —
`fixed<16,16> * fixed<16,16>` wants `fixed<32,32>`, and a static array, the only other
value-parameterized type, never has its size changed by an operator. See `todo.md`.

### 08/14/26
**A supertrait's methods are reachable through a subtrait bound.** `trait B: A` was
*enforced* from 08/07 — `impl B for T` requires an `impl A for T` (`lyra-E040`) — and
then `where t: B` still could not call `t.foo()`, reporting *"type parameter t has no
method `foo`; add a `where t: Trait` bound whose trait declares it"*. The promise was
kept and unusable: a supertrait guaranteed something exists and gave the one place that
needs it no way to reach it.

**The fix is a transitive closure at the point a bound set enters scope, not a rule taught
to each reader.** `tc.genericBounds[param]` held the literal trait names from the `where`
clause, and four sites iterate that list — bound dispatch, the generic-argument check,
operator overloading and the `Show` desugar. `closeOverSupertraits` expands it once, at
the **two** write sites (`pushGenericBounds` for a binding, `checkTraitImpl` for an impl),
and all four readers got it for free. This is hazard 8's shape applied before the fact
rather than after: four copies of "also walk the named traits' own `Bounds`" is four
chances to drift, and a fifth reader would have started wrong.

The two write sites are twins by `pushGenericBounds`'s own comment, which is why both had
to change in one go — a bound that reaches `A`'s methods when written on a binding and
not when written on an impl is a bound that means different things depending on where it
is written.

Three things beyond the closure itself:

- **Forwarding is the other half.** Passing a `where u: B` value to a callee bounded
  `where t: A` was refused, and the diagnostic asked the author to add `where u: A` — a
  bound `B` already guarantees. That path reads the same expanded map, so it came along.
- **The walk carries a visited set.** `trait A: B` alongside `trait B: A` is legal: it
  says the two are always implemented together, which is exactly what E040 then requires
  of every implementer. Assuming a DAG hangs the typechecker, and a compiler that never
  returns reads as an editor that froze, not as a compiler bug — so the cycle has a test.
- **The backend needed nothing.** Dispatch publishes candidates for the trait that
  *declares* the method, so a call through `B` to `A`'s method resolves to `A`'s impls
  like any other. Pinned by `TestExec_BoundDispatchReachesASupertraitMethod` regardless:
  "resolves abstractly" and "calls the right function" are different claims, and only the
  second is the one a user notices.

**A trait's body became optional the same day, which is what makes the shape above
writable.** An umbrella trait — `trait Arithmetic: Add + Sub + Mul + Div`, no methods of
its own — did not parse: `memberList` is built on `commaSep1` and its non-emptiness was
deliberate, its comment naming this exact case (*"which is what makes `trait C {}` a syntax
error rather than a trait with no methods"*). Right when a method-less trait meant nothing,
and supertraits are precisely what stopped that being true.

Three things worth keeping:

- **The body is optional, braces and all** — not the member list, which stays non-empty, so
  the list is *absent* rather than empty and `trait C { , }` is still an error. The bodiless
  spelling (`trait Arithmetic: Add + Mul`) is the one an author reaches for, because there
  is no body to delimit; Rust's mandatory `{}` is an artifact of having no statement
  terminator to end the declaration, and Lyra has one. `impl_methods` had been optional
  since it was written, so `impl Arithmetic for Vec2 {}` had been parsing all along and
  only the *declaration* was unwritable.
- **The optional body creates one ambiguity, and the terminator settles it.** A `{` on the
  following line could have been absorbed as the body; it is not, so `trait Marker` ⏎
  `{ 1 }` is a trait plus a block statement. This is the hazard that stopped the `for`
  condition from taking `$.expression` — there the block reading was genuinely ambiguous,
  here it is not. Pinned by a corpus test, because it is the kind of thing a later change
  inverts silently.
- **The collector reads the field with a nil check, not `MustField`.** An absent list is an
  empty method list, **not** a dropped declaration: `MustField` returns nil, which erases
  the trait and then reports `unknown trait` at every impl of it — a diagnostic pointing
  everywhere except at the declaration that caused it.

Cost: 7,786 → 7,825 states (+0.5%), `parser.c` +41 KB (+0.27%).

**And the prelude's first operator-named methods exposed a docgen bug the same day.** A
method's name on a page came from `GetName()` — the bare `Value` — so `(_/_)` rendered as
`/: (Self, Self) -> Self`, a line that does not compile on a page whose whole contract is
that it is the code to write. `MethodName.Key()` is the source spelling and exists for
exactly this; it also carries *kind*, without which prefix `-` and binary `-` render
identically although they are different methods.

Two things kept it invisible, and the second is the reusable lesson. Every trait method in
the standard library had been an ordinary identifier, where `GetName()` and `Key()` agree —
so the bug needed a new *kind of declaration* to surface, not a new code path. And
`TestSignature_RoundTripsThroughTheParser`, the guard written precisely to catch
unparseable signatures, checks `Decl.Signature` only, while a trait's methods are
`Members`: the guard's own blind spot was the shape of the model rather than the rendering.
Members now round-trip too.

What is deliberately *not* changed is whether the umbrella impl is required at all.
`impl Arithmetic for Vec2 {}` asserts nothing the supertrait checks do not already
establish — but making it optional would let a `where t: Arithmetic` bound be satisfied by
a type that never named the trait, which is a coherence question, not a syntax one, and it
is where E040 currently fires.

### 08/13/26
**`lyrac doc` — the standard library's reference page is now generated.** One Markdown
page per module with Starlight frontmatter, dropped straight into
`lyra-website/src/content/docs/reference/`. `pkg/docgen` splits `Collect` (a model from
the AST) from `RenderMarkdown` (one module as a page), so a terminal or JSON renderer is
a function beside the existing one rather than a second AST walk that can disagree with
it about what a module contains.

**The hard part was not the Markdown — it was that a signature has to be Lyra.** A page is
read as the code to write, so a name on it the parser rejects is a broken promise, and the
compiler's own type rendering is written for *diagnostics*, where a type is described. The
two disagree in ways that are individually small:

| `GetName()` | source | |
|---|---|---|
| `DynamicArray<string>` | `[]string` | `DynamicArray` is not a word in Lyra |
| `boolean` | `bool` | there is no `boolean` keyword |
| `AnonymousTuple(i64, u8)` | `(i64, u8)` | |
| `integer literal` | `i64` | a phrase for a sentence, not a type |
| `Maybe` | `Maybe<t>` | **drops the arguments — a type that exists and is the wrong one** |

`docgen.typeName` renders source syntax and falls through to `String()` for the cases
where the two agree. What made that tractable is
`TestSignature_RoundTripsThroughTheParser`, which feeds every generated signature back
through the real parser — and which caught the one no amount of reading would have:
`(mut self: Rng)`. The borrow modifier binds to the **type**, after the colon
(`self: mut Rng`); the other order is a syntax error and looks entirely plausible on a
page. Four of 72 prelude signatures did not parse before that test existed.

Four decisions, each with an obvious wrong answer:

- **It refuses a program that does not type-check.** Signatures come from resolved types,
  so documenting a broken program prints `?` where resolution failed and publishes it as
  the API.
- **An undocumented public declaration is listed anyway**, with its signature — dropping
  it makes the page silently misrepresent the module's surface, which is worse than a
  visible gap. Coverage prints on *every* run, not only under `--strict`.
- **The prelude needs its own opt-in even under `--deps`**, since it is implicitly
  imported by everything; otherwise every project's docs contain a copy of the standard
  library. It is still documented when it *is* the entry module.
- **An impl's methods are not counted as gaps.** The contract lives on the trait, where a
  doc is required; an impl method's doc says what *this* implementation does differently,
  so having none is usually correct rather than missing.

**Two rendering rules that only show up once a doc is nested in a page.** A doc comment is
written standalone, so its `# Panics` is an h1 — in the middle of a page that breaks the
outline and every table of contents built from it, so bodies are shifted by their depth.
And an untagged fence in a Lyra doc comment is Lyra (rustdoc's rule), which matters because
it is the most common code block in the standard library and the one an author has no
reason to tag; untagged, every example on the page renders unhighlighted. Both go through
`ast.walkDocLines`, the single fence tracker, so no consumer can disagree about whether a
`#` line is a heading or a comment inside an example.

**It also retired the module-summary wart recorded below.** A multi-file module's summary
was its *first file's*, alphabetically — so the standard library described itself as
"Combinators over `[]t`." from `array.lyra`, which is now the page's `description` and
therefore visible. `leadsModule` puts the file named for the module's last path segment
first, and `std/prelude/prelude.lyra` exists to hold the opening paragraph and declares
nothing. A module that does not want one adds no file and the join order is unchanged.

Pages are `std-prelude.md` rather than `std.prelude.md`: a site generator derives its URL
slug from the file name and strips dots, so the dotted form publishes at
`/reference/stdprelude/`. Verified end to end — the site builds, and the page renders with
its sidebar entry, its table of contents, and `# Panics` and `# Examples` as real
subsections.

**Documentation comments mean something.** `///` was lexed as its own token and listed
in `extras` from early on, with corpus tests for it on a declaration and on a struct
field — and *nothing* consumed it. The collector skipped it with every other extra
(`block_expr.go` names `doc_comment` in the child it drops), no AST node had a field for
it, and hover rendered a type and nothing else. It is the shape this file records under
"surfaces nothing reads": parsed, tested, and inert, which costs more than an absent
feature because it looks finished.

`///` now documents the declaration below it, `//!` the module the file belongs to, and
the six attachment sites are exactly the declarations — top-level binding, `type`,
`trait`, `impl`, plus struct fields, data constructors, trait method signatures and impl
methods. The body is Markdown with three recognized headings (`# Examples`, `# Panics`,
`# Errors`). Four decisions are worth the space:

- **Docs attach to declarations, not to types.** A field's doc lives in
  `TypeDeclStmt.MemberDocs`, not in `types.StructField`. `TypesEqual` compares fields
  name-by-name so a prose field would in fact have been safe today — but a `types.Type`
  is shared, substituted and compared structurally, and prose on one is a thing that
  quietly stops being true. It also settles the anonymous-struct question by
  construction: `CollectStructFields` is shared with the anonymous form, so the docs
  pass is a *separate* walk that only the named-declaration collectors call, and no site
  has to remember to ask which kind of struct it is in.
- **A stray doc warns** (`lyra-W017`) rather than being discarded. Discarding is the
  failure this language spends its budget avoiding: the author writes the documentation,
  the generator never emits it, nothing says so. Attachment is strict adjacency for the
  same reason — attaching across a blank line makes the last comment in a file silently
  become the docs of whatever is appended after it. It is a post-pass over the tree, not
  a check at each site, because whether a `///` was claimed is only knowable once every
  collector that might have claimed it has run.
- **`//!` for the module**, not a `///` above the `module` line, because a module is a
  file *or a directory* (08/07). A directory module has no single header to sit above,
  and its files each want to say something about the module they join; the headers are
  joined in file order under `SymbolTable.ModuleDocs`.
- **No `@param`/`@returns`.** The signature is in the AST; a tag restating it is a
  second copy to drift, and the one thing it could add is a sentence in the body.

**Two placement rules in the CST cost more than the feature did**, and both fail the same
way — they document every member except one, which is a rule with an invisible exception:

1. **An extra before a node's first token attaches to the *enclosing* node.** So in a
   trait body the second method's doc is a sibling of its `trait_method`, and the
   **first** one is a sibling of `trait_methods` itself, because the `{` belongs to
   `trait_declaration`. `prevSibling` climbs out of a node it begins, guarded on
   `parent.StartByte() == node.StartByte()`. `struct_type_body` includes its own `{`, so
   a struct's fields never take that path.
2. **A separator token can sit between the doc and its node.** The `|` in the
   leading-bar `data` style is an anonymous sibling of the constructor, so a plain walk
   stopped on it — documenting the first constructor and no other, in the style most
   people write. A non-comment sibling is skipped only on the documented node's *own*
   row, and only before any comment has been collected, so the exception cannot reach
   across a line.

**And it surfaced a scanner bug that predated it by two weeks.** A comment on a line of
its own ended the statement, so a continuation line could not be commented at all:

```lyra
data Dir =
  North
  // Towards the bottom of the map.
  | South     // `| South` left over as a statement of its own
```

`scan_newline`'s switch had no `/` case, so a comment fell to `default` and the
terminator fired — while the grammar's notes asserted a `/` case existed and returned
false. Every continuation token was affected (`.`, `|`, `else`, `where`) and a plain `//`
broke it identically; documenting a `data` constructor is simply the first thing that
makes anyone write a comment there. The scanner now skips whole-line comments before
testing for a continuation, which is safe in both directions: on a continuation it
returns false and tree-sitter re-lexes from the token start, so the comment still becomes
an ordinary extra node — which the doc collector depends on — and on a terminator
`mark_end` has already fixed the token's end before the comment.

**Grammar cost: zero new parse states** (7,786 → 7,786), `parser.c` +631 KB (+4.4%).
Comments are extras, so they add lex-table entries in every state rather than parse
states. Attributed: `inner_doc_comment` plus tightening `doc_comment` is +257 KB, and the
other +374 KB buys the rule that **`////` is an ordinary comment**. That rung is not
optional — tree-sitter compares token precedence before match length, so without
`comment` outbidding at `prec(2)` a `////////` section rule lexes as the doc comment
`///` plus a stray `/////`, and a divider above a declaration silently becomes its
documentation.

Docs render in LSP hover, under the type — hover is read at a glance, and a long block
above the signature pushes the signature out of a fixed-height popup. `resolveDoc`
mirrors `resolveDefinition` case for case on purpose: "which declaration does this
expression name?" already had an answer in that package, and a second walk answering it
differently is how hover comes to show one symbol's docs above another symbol's type.

`lyrac doc` does not exist yet. This is the representation it will read.

**The prelude is documented, and documenting it changed two things.** All 72 top-level
declarations and 29 members across the eight files carry `///` blocks, and each file's
`//!` header contributes a paragraph to `std.prelude`'s own documentation.
`prelude_docs_test.go` collects the real sources as the multi-file module they are and
asserts the coverage, the members, the `# Panics` sections and the join — because
**`lyrac check` will not catch a detached doc**: W017 is a warning, so a shell loop
testing the exit code reports every file clean. That is exactly how the one real mistake
in the prelude survived a first pass, and the test is what found it.

That mistake is the convention worth recording: **an implementation note goes *above* the
doc block, never between it and the declaration.** The prelude's existing `//` comments
are dense design rationale sitting immediately above each function, so the natural edit is
to add the `///` block above *them* — which detaches it. The docs and the rationale also
turn out to want different homes: `///` is the caller's contract (what it returns, when it
traps, what it costs), `//` is why the code is the way it is (why `parse_i64` accumulates
negatively, why `below` rejects the top bucket). Several entries kept both.

Two changes fell out of writing them:

- **`//!` may sit directly under the `module` line**, not only at the top of the file.
  Every prelude file opens with `module std.prelude`, so the top-of-file rule — inherited
  from Rust, which has no module header — put the documentation *above* the line naming
  the thing it documents, and made the natural spelling warn. The header region now admits
  the module declaration once, as the first statement; a `//!` further down is still
  stray.
- **Hover must not bail on a missing type.** `desugarUFCSCall` rewrites `s.trim()` into
  `trim(s)` with a callee synthesized at the method name's location, and that node has no
  recorded type — so hovering a *method name* answered nothing, which is the spelling the
  entire standard library is written for. Hovering the receiver and a bare call both
  worked, which is why it read as "hover works". That position now renders the
  documentation with no signature block above it; every position that already resolved a
  type is untouched.

Known wart, latent for now: a multi-file module's `Summary` is its **first file's**, so
`std.prelude` summarizes as `Combinators over []t.` from `array.lyra` sorting first.
Nothing reads a module summary yet. A generator wanting a real lead would need either a
designated lead file or per-topic sections.

**(Fixed the same day, by the generator that stopped it being latent** — the summary
became the page's `description`. `leadsModule` designates the file named for the module's
last path segment; see the `lyrac doc` entry above.**)**

**Regex matching at run time — without a regex engine in the runtime.** `lyra-E052`
and `lyra-E054` both recorded the same absence: the runtime is hand-written shims and
libc with no FFI, the compiler's own `regexp` runs at compile time and cannot ship, so
nothing could match a pattern against a value the compiler had not already read. That
is why a `pattern(...)` constrained newtype could only be built from a literal.

**The way out is that a pattern never needs compiling at run time.** A `where
pattern(r"…")` constraint is part of a *type*, and `r"…"` is a literal — so the
pattern is always known while compiling. The engine can therefore run at compile time
and only its **answer** need ship. What ships is two constant tables and a loop.

`pkg/regex` already had a derivative-based lazy DFA; `regex.Matcher` flattens it into
`Trans[state*256+byte]` plus a per-state accept array, with the beginning- and
end-of-text boundaries **folded in** so the emitted loop never has to know boundaries
exist. The backend emits those as private constant globals (one set per distinct
pattern, cached by pattern text) and one shared `lyra_regex_match` driver walks them.
Matching is O(n) with no backtracking and no allocation, which is what a DFA buys —
and a trap is `EffectNone`, so `pure noalloc` code can construct a constrained newtype
freely.

**The hard part was agreement, and it was attacked before any IR existed.** The
compile-time and run-time answers for one pattern must be identical — a value passing
one and failing the other is worse than either — and the boundary handling is where
they could most easily diverge. `MultiLine` is on by default, so `^`/`$` fire at every
`\n`, and `IsMatch` deliberately omits the trailing beginning-of-line after the input's
final byte. Rather than re-derive that, `stepByte` mirrors `IsMatch`'s sequence of
calls exactly and the trailing newline gets its **own column** (`NewlineLast`), so the
difference lives in the table rather than in logic that could drift. Then
`matcher_test.go` checks the table against the engine over a corpus of patterns × inputs
— curated newline cases, exhaustive 1–3 byte strings over an adversarial alphabet, and
all 256 byte values — rather than against hand-written expectations, which would only
have tested my reading of the patterns. It passed on the first run, which is the
evidence the mirroring approach was right.

`llvm_regex_test.go` then closes the remaining link by running **compiled programs**
against the engine for 31 pattern/input pairs: a Go test can check the table, but only
a compiled program can check that the IR implements it. Engine → table → emitted loop,
each step compared to the one before.

Two properties worth keeping. **A literal costs nothing**: it was matched at compile
time, so no table, no driver and no call are emitted — verified by an IR test, and the
same rule the numeric constraints follow. And **one pattern is one table** however many
places use it; the tables are the large part of this feature, so sharing them is what
keeps it affordable. Measured: `^[0-9]+$` is 3 states (3 KB), a realistic email pattern
8 states (8 KB), the whole program a 36 KB binary.

What is still refused (`lyra-E054`, now naming the *pattern* rather than the value) is a
pattern that cannot become a table: a **lookbehind**, whose gate depends on text
preceding the input and which a flat byte table cannot represent, and a DFA beyond
`regex.MaxTableStates`. The cap lives in `pkg/regex` because two users must not disagree
about it — the typechecker asks whether a pattern will compile, and the backend then
compiles it, so a different cap on each side would accept what could not be emitted. For
the same reason `PatternConstraint.Body()` (stripping the `r"…"` delimiters) moved onto
the type: the typechecker and the backend compile the *same* pattern, and a difference in
what they strip would be a difference in what they match.

`lyra-E052` — a regex as a first-class **value** — is unchanged and still refused. This
gives constraint checking a runtime implementation, not the language a `Regex` type.

**A `where` constraint is enforced at run time — the ladder's second rung, which
constraints did not have.** A constraint caught a literal, and whatever the
value-range pass could pin to an interval, and silently accepted everything else:

```
newtype Percent = u8 where range(0..<=100)
let mk = (n: u8) -> Percent => Percent(n)
mk(200)      // built, ran, printed 200
```

That is the exact shape the language refuses everywhere else — provable → compile
error, otherwise → trap — missing its second half in the one construct whose entire
purpose is to be checked. And the values a constrained newtype meets at run time are
the ones from outside the program (parsed input, computed results), which is where a
range or unit mistake actually lives. `range`, `values` and `step` now trap;
`pattern` refuses what it cannot read.

**The typechecker publishes the sites; the backend emits the checks**
(`typetable.ConstraintTable`, `pkg/backend/llvm/constraint_check.go`). Only the
typechecker knows which values it verified, so having codegen re-derive "is this a
construction, and is it provable" would be the same answer computed twice — the rule
the method, callee and instantiation tables already follow. The practical consequence
is that **a foldable constant is never recorded**: it was decided at compile time in
either direction, so a literal construction emits no check at all and the cost falls
exactly where the compiler could not do better. That is one compare-and-branch, which
is what overflow-checked arithmetic already costs, and the optimizer folds away the
ones provable by other means.

**`step(...)` became a real constraint.** It had been collected, validated for
well-formedness — `types/step.go` refuses a zero step and a fractional step over an
integer domain — and then read by nothing at all, so `range(0..<360), step(15)`
accepted 7. That file's own comment had recorded the gap as a known asymmetry, which
is the collected-and-unread shape this project keeps digging out. The grid is measured
from the **range's start**, since the meaning already fixed for both spellings is
"start, start+step, start+2\*step, …": `range(5..<=95), step(10)` accepts 15 and
refuses 10, and a step with no range anchors at zero. Integers use `srem`/`urem` by
signedness and floats use `fmod`, and the compile-time and runtime rungs share the
same origin rule — they must agree, or a value would pass one and fail the other.

**`pattern(...)` refuses rather than admits** (`lyra-E054`). It cannot join the others:
testing a regex at run time needs an engine in the runtime, and `lyra-E052` records
why there is none. That left two honest options for a value the compiler cannot read,
and admitting it is what made `Digits("abc")` build and print `abc` while the type's
declaration says it cannot hold that. Refusing keeps the guarantee whole; the cost is
that such a newtype cannot be built from runtime data until an engine exists, which is
a feature waiting on the engine rather than a rule. A literal still works, checked
where it is written.

Two details worth keeping. The integer comparisons are **signedness-correct**
(ULT/UGT for an unsigned base), which matters more here than usual because a
constraint's whole job is to be exact about a boundary. And **one construction is one
check**: `Percent(n)` reaches the checker twice — as the constructor's operand, and
again as the constructor node once the enclosing context propagates the newtype onto
it — which emitted the range test twice (four traps for one construction) and reported
E054 twice for one `Digits(s)`. The guard against that initially matched nothing,
because a newtype constructor is a **`TupleLiteralExpr`**, not a `FunctionCallExpr`:
`Percent(n)` parses as the same named-tuple node `tuple Rgb(u8, u8, u8)` constructs
with. "Constructor" reads like "call" everywhere else, which is exactly why it is
written down at the guard.

Tests: `llvm_constraint_trap_test.go` (exec: range with boundaries and an exclusive
end, values, step including the offset grid, floats, plus IR pins that a constant
construction emits no check and a runtime one emits exactly one per bound) and
`step_constraint_test.go`. ASan clean on macOS and Linux.

**An `f16` literal no longer makes llir print a bug-report demand.** `let a: f16 =
0.1` logged *"unable to represent floating-point constant 0.1 of type half exactly;
please submit a bug report to llir/llvm"* on a build that was entirely correct — llir
warns for any inexact half, which is nearly all of them, and then emits the correctly
rounded value regardless. Noise rather than wrongness, but a compiler telling its
users to file bugs against a library they never chose is a bad look for the language,
not for llir.

`floatConst` gained a `Half` arm that pre-rounds the value, so what llir receives is
already exactly representable and its exactness test finds nothing to report. **The
rounding goes through `binary16.NewFromFloat64` — llir's own conversion, the same one
`Float.Ident()` calls when it emits a half.** That was the point of the fix rather
than an implementation detail: Go has no float16, so the alternative was a
hand-written round-to-nearest-even with subnormal and overflow cases, and a rounding
routine that disagreed with the library consuming it would turn a cosmetic problem
into a correctness one. Routing through the same function makes agreement structural
instead of tested-for. `binary16` was already in the module graph through llir, so
this promotes a transitive dependency to a direct one and adds nothing new.

The emitted value is unchanged — 0.1 as an f16 is 0.0999755859375, emitted `0xH2E66`
before and after — which is exactly what the test asserts, since the whole claim is
that only the noise went away.

**A float literal in a comparison takes the operand's width, and `lyra-E012` names
the fix instead of the mechanism.** Two small ones, and the first is a good specimen
of a bug hiding inside a control-flow shape rather than inside any statement.

`let x: f32 = 0.1` then `x == 0.1` emitted `fcmp oeq float %1, 0x3FB999999999999A` —
a **double** constant in a `float` compare — and clang rejected the module. Every
link in the chain was already correct: `numericResultType` answers
`untyped_float + f32 → f32`, `propagateLiteralType` has a float arm, and the
backend's `literalFloatType` reads the recorded type. The break was an `else if`:

```go
} else if isFloatType(leftType) || isFloatType(rightType) {
    …warn that float ==/!= is precision-sensitive…   // and return
} else {
    tc.propagateComparisonWidth(…)
}
```

The imprecision warning sat *where the propagation belonged*, so the operators the
warning was about were precisely the ones whose width never propagated — a warning
about floating-point precision that stopped the program compiling. The warning is
advice, not an alternative to doing the work, so it is emitted alongside the
propagation now. The relational operators never had the bug, their branch
propagating unconditionally; that `x < 0.1` worked and `x == 0.1` did not was a
difference with no reason behind it, which is what an `else if` doing double duty
looks like from outside. The emitted constant is now `float 0x3FB99999A0000000` —
the right *type* from this fix and the right *value* from the same day's
`floatConst` rounding, which is why the IR test pins both.

**`lyra-E012`** reported `const A = u8(200)` as "a function call is not constant".
True of the mechanism — a conversion is spelled as a call — and useless as advice,
naming neither what was wrong nor what to do. It now says a *conversion* is not
constant and shows the spelling that works: `const A: u8 = ...`, with the const's own
name and the conversion's target filled in. The rule itself is right and unchanged;
an annotated const is the supported way to write a typed constant, and probing
confirmed `const A: u8 = 200` compiles and runs. An ordinary call keeps the ordinary
message, since no annotation would rescue it. The conversion is recognized through
`types.ConversionTargetName`, the same shared answer the newtype and ownership work
uses, so this cannot drift from the conversion rules themselves.

Tests: `llvm_float_compare_test.go` (exec, plus the IR pin) and four additions to
`const_initializer_test.go`.

**A fixed-array *binding* no longer takes a `[]T` slot — it segfaulted.** Found the
same day while deciding how wide to make the generic-inference fix below, and it is
the entry that pair exists to justify: the narrow fix was chosen *because* probing
turned this up first.

```
let take = (xs: []i64) -> i64 => xs[0]
let ys: [3]i64 = [1, 2, 3]
take(ys)      // checked clean; segfaulted
```

`[N]T` is stack storage and `[]T` is a ref-counted box, so the callee indexed through
a pointer that was really the array's first element. **The cause is the shape this
audit keeps finding**: `isAssignable`'s rule carried the comment "a static array
*literal* is assignable to a dynamic array" directly above code that tested only the
*type*, so every `[N]T` value passed, binding included. The comment had the rule
right; nothing enforced the word "literal".

The two claims are now separate functions. `isAssignable` answers about types alone
and refuses the widening; `assignableValue` adds the one allowance that depends on
**what the expression is**, and is used at the fourteen sites where a value is
checked against a type — binding, reassignment, l-value assignment, argument, return,
clause body, struct field, aggregate element, and the generic and trait-dispatch
argument paths. **The list came from measurement rather than from reading**: deleting
the type-level rule and running the suite reported exactly which paths depended on
it, and *every* failure was a literal case. That is the evidence that literals were
its only legitimate use, which no amount of grepping would have established.

**The allowance walks the expression alongside the type, and nesting is what forces
that.** `[[1, 2], [3, 4]]` and `[y1, y2]` — where those are `[2]i64` bindings — have
the *same* type, `[2][2]i64`. A type-level recursion cannot tell the legal case from
the crashing one; only the expressions can, so the walk descends through array
literals and their elements, the repeat form's value, a newtype target (`newtype Row
= []i64` accepts a literal, being its base at run time), and a tuple literal's
elements (`() -> (i64, []i64) => (1, [2, 3])` — the tuple is not itself malleable but
what it contains is).

One correction the suite caught immediately, worth recording because it is the risk
inherent in adding a second path: the tuple arm was at first a way *past* the name
check, accepting a `Point` into a `Vector` slot and an anonymous literal into a named
slot. A tuple is nominal when it has a name; the arm exists only to let an array
literal inside one take its shape, so it now re-checks the name before recursing.
Widening machinery that bypasses an existing check is the classic way a fix becomes
a bug.

What this deliberately does **not** do is convert a built array. There is no implicit
copy from stack storage into a heap box, because that is a hidden allocation in a
language whose whole posture on allocation is explicitness — `noalloc` would have to
charge it, and the copy would be invisible at the call site. A binding that must be
dynamic is declared dynamic. Tests: `static_to_dynamic_array_test.go`, covering both
directions plus the nested-bindings case and the nominal-tuple guard. ASan clean on
macOS and Linux.

**A generic `[]t` parameter solves `t` from an array-literal argument — and the
narrowness of the fix is the interesting part.** `first_of([1, 2, 3])` against
`(xs: []t)` reported *"cannot infer type variable t from these arguments"*, while the
same call with a `[]i64` binding worked and a `[3]t` parameter took the same literal
happily. So the diagnostic named the wrong problem: `t` is plainly `i64`, and the
literal was the only obstacle.

The cause is a genuine chicken-and-egg rather than a missing case. An array literal is
the one expression whose *representation* its context chooses — `[1, 2, 3]` is a fixed
`[3]T` or a heap-allocated `[]T` "told apart by what the literal is used as" — and the
mechanism for that is propagating the target type onto the literal. At a generic call
the target is `[]t` and `t` is what is being solved, so propagation has nothing
concrete to push. The literal therefore inferred `[3]i64`, and `unifyGenericTarget`'s
`DynamicArrayType` arm accepts only a `DynamicArrayType`. `arrayLiteralAsDeclared`
reads the *shape* off the declaration instead (a `[]…` parameter means "this literal
will be built as a box", whatever `t` turns out to be), which is all unification needs;
the ordinary propagation then runs against the substituted `[]i64` and records the
literal as dynamic.

**The fix deliberately adapts only a literal, and probing is what established that it
must.** The obvious generalization — let any `[N]X` unify with `[]t` — would have
imported a live memory fault, because the non-generic path already does exactly that
and the resulting program **segfaults**:

```
let take = (xs: []i64) -> i64 => xs[0]
let ys: [3]i64 = [1, 2, 3]
take(ys)      // checks clean; segfaults
```

`[N]T` is stack storage, `[]T` is a ref-counted box, and the callee indexes through a
pointer that is really the array's first element. The cause is this audit's recurring
shape one more time: `isAssignable`'s rule says in its own comment "a static array
**literal** is assignable to a dynamic array" and then tests only the *type*, so every
`[N]T` value passes. For a literal the rule is not even load-bearing — the literal is
*built* as a box when its context says so — so the rule's whole observable effect is
the case it was never meant to allow. That is recorded in `todo.md` rather than fixed
here: the diagnosis is not in doubt, but the rule is consulted from everywhere and
deserves its own verification pass. A test pins the generic path's refusal of the
binding, so fixing the open bug will be a deliberate act rather than something a silent
test invites.

Verified by *running* the compiled programs — solving the variable is only half the
claim, the other half being that the literal is built as a box the callee can index —
including string elements under ASan, where a stack array reinterpreted as a box would
show up as a refcount fault rather than a wrong number. Only the outermost level is
adapted; a nested `[][]t` taking `[[1, 2]]` stays unsolved rather than guessed at,
since the inner elements need not be literals and the same memory question applies one
level down. Tests: `llvm_generic_array_arg_test.go`, `generic_array_arg_test.go`.

**A printed float reads back as the same value — and making it so immediately exposed
a shipped correctness bug the old printer had been hiding.** Two fixes, and the
relationship between them is the entry's point.

Printing a float was one `snprintf("%g")`. `%g`'s default precision is **six
significant digits**, so `println(0.1 + 0.2)` printed `0.3`, `1.0 / 3.0` printed
`0.333333`, `3.14159265358979` printed `3.14159`, and `1234567890.0` printed
`1.23457e+09`. Each is a different number from the one the program held, printed with
no indication that anything was dropped — the silent wrongness this language exists to
refuse, in the one operation whose whole job is to report a value faithfully. Reading
printed output back is how data moves between programs, and it was not safe to do.

`lyra_f{16,32,64}_to_str` (one emitted per width a program actually prints) is the
classic printf-based shortest construction: render at increasing precision, `strtod`
the candidate, stop at the first that comes back equal. The top of each ladder is the
width's IEEE round-trip guarantee — 17 digits for binary64, 9 for binary32, 5 for
binary16 — so the last rung always succeeds and the loop always terminates with a
faithful answer. The bottom rung is chosen so the common case costs one iteration:
`%g` strips trailing zeros, so 15 significant digits of 0.1 is already `0.1`.

**The round-trip comparison is made at the value's own width**, which is the detail to
keep. An f32 is widened to double to be passed (varargs, and every f32 is exactly a
double), so a check performed *as* a double would compare against 0.10000000149011612
and reject `0.1`, printing all nine digits for every f32. Narrowing the parsed
candidate back to `float` before comparing is what keeps a narrow float's output
narrow. NaN never compares equal, so it runs to the last rung and prints libc's `nan`;
infinities round-trip immediately.

It is shortest *within the ladder* rather than provably minimal — a value whose true
minimum is 14 digits prints 15. Ryu and Grisu are the algorithms that do better and
are several hundred lines of hand-written IR here; what actually mattered was that a
printed float denote the value that was printed, and that is now exact.

**Then the faithful printer found the second bug in its first run.** `let x: f32 = 0.1`
printed `0.099999994`. That was not a printing fault: the emitted constant really was
`float 0x3FB9999980000000`, one ULP below 0.1f32 (`0x3FB99999A0000000`). llir's
`constant.NewFloat` stores the float64 and **truncates** the mantissa when it emits a
narrower type instead of rounding to nearest, so the program held a number its own
source did not name — and it shipped. `floatConst` rounds to the target width in Go
before constructing, after which the value is exactly representable and llir's
truncation has nothing left to remove; every float literal now goes through it.

The two are worth recording together because the first concealed the second: at six
significant digits the wrong constant and the right one both printed `0.1`, and no
test could have told them apart through output. **A lossy printer does not merely lose
detail, it hides other faults** — which is a better argument for round-tripping output
than the ones usually given for it.

Two adjacent findings came out of the same session and are left open in `todo.md`
rather than folded in: a float literal is **not narrowed to the operand's width in a
comparison**, so `let x: f32 = 0.1` then `x == 0.1` emits a double constant into a
`float` compare and clang rejects the module (loud, pre-existing, and a
literal-propagation issue rather than a float-formatting one); and an inexact **f16
literal makes llir log a "please submit a bug report" line** on an otherwise correct
build. Tests: `llvm_float_print_test.go`. ASan clean on macOS and on Linux, where the
older clang's typed pointers also check the new `strtod` declaration.

**The regex-value phantom is closed (`lyra-E052`), in two positions — the audit named
one and probing found its twin.** A regex literal used as a **value**
(`let re = r"[a-z]+"`) inferred the built-in `regex` type and then died in the backend
(`expression lowering not implemented for *ast.RegexLiteralExpr`); a regex **match
pattern** (`match s { r"^[0-9]+$" => … }`) type-checked clean and died as
`match pattern *ast.RegexPattern not implemented … (regex patterns deferred)`. The
second is the more interesting find, because a string scrutinee was the *only* place a
regex pattern was accepted — `checkDataMatchArm`, the rune arm and the numeric arm all
refused it already — so the language was refusing the construct in three positions and
silently taking it in the fourth.

Refused rather than built, for the reason the runtime dictates: a regex **value** needs
a regex engine, and Lyra's runtime is hand-written C shims with no FFI. The `regexp`
this compiler uses to validate patterns runs at *compile* time and cannot ship into the
compiled program, so "make the literal lower" is a project with a dependency the
language does not have.

The `regex` primitive type turned out to have exactly **one** consumer — the literal's
own inference — and it is not even spellable: a lowercase type name parses as a type
*variable*, so `(re: regex)` declares one named `regex` and behaves identically to
`(re: zzzz)`. That is worth recording because it is the opposite of `^T` in the E051
entry above, where the type genuinely lived in the type system (unify, substitute,
newtype base, array element) and only the operations were missing. Here the type was
part of the phantom.

**`where pattern(r"…")` is untouched and still works**, verified end to end (a valid
value builds and runs; an invalid literal is still rejected). That is why the refusal
lands on the two value positions rather than on the literal *syntax*: a constraint
stores the pattern's source text and compiles it at type-check time, so it never
produces a value of type `regex` and never needs a runtime engine. Compile-time syntax
validation left the two refused positions along with them — reporting `invalid regex
pattern` beside "not implemented" is one mistake reported twice, the E011-and-E001
fault this series has been removing — and stays exactly where it does real work.

Six tests inverted, all of which had pinned the acceptance
(`..._RegexPattern_Ok`, `..._MixedLiteralAndRegex_Ok`, `..._RegexLiteralExpr_Valid_Ok`
and the two invalid-pattern ones). One was kept as a *pair* rather than replaced:
regex arms never made a string match exhaustive on their own, and they still do not, so
the arms are now rejected **and** the match still asks for a catch-all — a refusal that
also silenced the exhaustiveness analysis would hide a second mistake behind the first.

**A separate finding came out of the probing and is left open in `todo.md`**: a `where`
constraint — `range(…)` as much as `pattern(…)` — is enforced only where the value is
*provable*. `Percent(n)` and `Digits(s)` inside a function whose parameter is opaque
pass straight through, so `mk(200)` builds, runs and prints 200. Whether a constraint
is a compile-time assertion or a runtime trap is a language decision (and for `pattern`
the runtime answer needs the very engine E052 says does not exist), so it is recorded
rather than patched.

**The raw-pointer / `unsafe` phantom is closed (`lyra-E051`), and what set it apart
is that the compiler's own advice was impossible to follow.** `&x` reported
`lyra-E011` — "taking a raw pointer with `&` requires an `unsafe` block or function" —
and `unsafe { … }` was itself `unknown expression type "unsafe_block"`. Doing exactly
what the diagnostic said produced a different error. Two smaller faults sat beside it:
`&x` double-reported (E011 *and* E001 at the same location), and a pointer **write**
drew only the misleading advice, because `WalkStmt` descends into a deref
assignment's *operand* rather than the `DerefExpr` node — so `p^ = v` was the one raw
pointer form that never reached the typechecker's default arm, and type-checked clean.

**This surface is far more built than the arena one, which is what made "refuse it"
the harder call.** `^T` is a real type — `types.RawPointerType` unifies, substitutes
and heads, a newtype may wrap one, it is a legal array element — the grammar and
collector build every node, and E011's policy checker is genuinely good: a
raw-pointer op or a call to an `unsafe` function needs an enclosing `unsafe` block or
function, and unsafe-ness does **not** leak across a lambda boundary (a safe closure
inside an `unsafe` block is its own safe context), with ten tests. What is missing is
only the two ends: nothing infers these expressions, nothing lowers them.

Refusing still wins, for the same reason it did for arenas: implementing raw pointers
is a project, not a fix — `&x` needs its operand forced into memory (Lyra values are
largely SSA), and a `&` to a managed value drives straight through the reference
counting the ownership pass owns. Getting that wrong is memory unsafety in a language
whose thesis is the opposite. So all four forms are refused at the expression, in the
register of "not implemented" rather than one that reads like an internal error.

**E011 is no longer reported**, and that is the deliberate part. `driver.go` keeps the
call site as a comment explaining why; the checker and its tests stay and are
exercised directly. Its policy is exactly right for the day pointers work — it is not
wrong, it is *premature*, and a correct policy that directs the reader to an unbuilt
construct is worse than none. Wiring it back is one line the day E051 comes out.

Two details worth keeping. A `^T` **annotation still resolves** (`(p: ^i64) -> i64` is
not itself an error), which is why the diagnostic names the operations rather than the
type — the type system's half was never the phantom. And `DerefAssignmentStmt` is
**overloaded**: the grammar reuses it for const reassignment (`X = val` where X is a
const identifier parses as a deref-assignment wrapping the const), so the const case
keeps precedence and only a genuine pointer write reports E051 — otherwise a plain
assignment to a constant would report as a pointer operation the author never wrote.

Unlike the arena discharge, **there was no soundness hole**: all four standalone
passes that special-case `UnsafeBlockExpr` (shadowing, unused variables, unreachable
code, use-before-declaration) recurse into the body rather than skipping it. This
phantom was inert and misleading, not unsound. Tests: `raw_pointer_test.go`, plus the
inversion of `TestDerefAssignment_NonConstTargetOK` — which asserted `ptr^ = 42`
produced *no errors*, the phantom in miniature.

**The `with`-arena phantom is closed (`lyra-E050`) — and it had teeth, which is
what separated it from an inert surface.** Arenas were designed early (grammar,
collector, the reserved `lyra_arena_alloc` shim, the `PinnedRC` sentinel that makes
retain/release no-op on an arena-owned box) and never implemented. The audit's second
sweep found three things wrong at once:

- **Nothing type-checked the arena expression.** `checkNode` had no `WithStmt` arm at
  all, so `with a = 42 { … }` type-checked exactly as well as
  `with a = Arena.new(1024) { … }`. The canonical spelling is in fact doubly
  unreachable — no `Arena` type is declared anywhere, and `Type.method(…)` has been
  `lyra-E035` since 08/06 — so the feature's own documented syntax could not have
  worked for a week. Nothing reported it because nothing inferred it.
- **The purity pass discharged the block's allocations**, which was the only
  observable thing `with` did. `buildAllocContext` marked every expression lexically
  inside a `with` body, and all three allocation predicates consulted the mark, so
  `noalloc … => with a = 42 { let n: shared Node = … }` checked clean while the
  identical code outside the block was `lyra-E016`. A bound that silently stops
  binding — the `slice` hole's shape and the closure hole's shape, a third time — and
  here it discharged into an allocator that does not exist.
- **Nothing lowered it**: `llvm: block statement lowering not implemented`.

The fix follows the 08/06 precedent for `Rng.seeded` rather than the one for
`wallClock`: **refuse it at the source**, because implementing arenas is a project
(a real allocator, handle lifetimes, and an escape analysis) and, unlike
`EffectTime`, deleting the discharge orphans nothing — `noalloc` works fine without
arenas. `with` is `lyra-E050` at the statement, the discharge is gone, and the arena
expression and body are both checked now.

**The body check is the part worth remembering, because removing the discharge alone
did not close the hole.** `WithStmt.Body` was a `BlockExpr` held **by value**, so
`&w.Body` — the address of the struct's own copy — was never the `*BlockExpr` the
ScopeTable keyed the body's scope under. Checking the body therefore reported every
name declared inside it as undefined, which is why nothing checked it. But allocation
is a *use-site* property the **typechecker records** (the purity pass reads each
construction's resolved flavor off the TypeTable), so an unchecked body is one whose
`shared` constructions `noalloc` cannot see at all: with the discharge deleted and the
body still unchecked, the probe reported the `${…}` interpolation and missed the
`shared Node` entirely. Making `Body` a `*BlockExpr` — which every other
block-holding statement already was; this one was the outlier — fixed both halves at
once, and the suite caught the intermediate state, which is the evidence the two
layers were independent.

Kept deliberately: the conservative reading that an arena handle is interior-mutable
(`declaredMutability`, `arenaScopes`). It is unreachable from a buildable program but
the purity pass still runs on one that has errors, and it makes purity *stricter* —
the opposite direction from the discharge. If arenas are built, the discharge returns
**with an escape analysis**, not without one: the old note already conceded that a
`shared` value built inside a `with` block and returned out still escapes, and
"everything lexically inside is discharged" was the approximation standing in for the
analysis nobody wrote. Tests: `with_arena_test.go` (refusal, both grammar forms, the
body still checked, no false undefined, the unspellable canonical form) and
`TestNoAlloc_ArenaDoesNotDischarge` — the inversion of the test that had pinned the
unsound behavior.

**`??` lowers — `?`'s value-position sibling, and the same match in disguise.** It had
type-checked and failed to lower since it was collected (the 07/30 `?` shape: a
front-end surface with no backend, `llvm: expression lowering not implemented`), which
the second audit sweep flagged as finding 2. The lowering
(`null_coalescing.go`) is `match a { Some(v) => v, None => b }` with everything that
made `?` a hard lowering removed — nothing leaves the expression, so there is no
rebuild at the enclosing return type, no early return, and both arms feed one phi
under the ordinary merge rules the dominance-based temp flush already handles.

Three design points worth the record:

- **The default is lazy, and that is why this is a branch, not a select.** It is an
  arm, evaluated only on the None path — `m ?? panic("missing")` is the spelling that
  proves it (the exec test pins both directions: Some never runs the panic, None
  traps). A diverging default seals its block and feeds the phi nothing.
- **Ownership is the match rules verbatim** (ownership.go's `NullCoalescingExpr` case,
  placed beside `TryExpr`): the optional is borrowed as a scrutinee, the default is a
  conditional arm coerced to owned by its own node's marks, and the merged value is a
  uniformly-owned temporary the enclosing statement releases once. The Some payload is
  the one value with no AST node of its own to mark, so its +1 (duplicate-never-move —
  the scrutinee still owns and drops its copy) is emitted in the lowering directly,
  exactly `?`'s failure-rewrap arrangement. Verified by ASan plus the retain/release
  conservation check on both paths, macOS and Linux (typed-pointer clang).
- **The typechecker gained the one call the phi forced**: `propagateLiteralType` on
  the default against the unified type — `m ?? 7` on a `Maybe<u8>` must lower the 7
  at u8 or the phi's incomings disagree (invalid IR, loud but wrong) — and with it
  `checkIntegerLiteralRange`, so `m ?? 300` on a `Maybe<u8>` is refused: the default
  is an ordinary value position under the same-day literal rule.

A left operand that is not a canonical Maybe can never be null, and that is now a
**hard error** (`lyra-E049` — it had warned as lyra-W007 since the operator landed,
and was promoted the same day the operator started lowering, at the user's call): the
`??` can never fire, so the default is dead code that reads as a handled case, and a
construct that cannot mean anything is refused where it is written — the E034
(directionless descending range) and E035 (type-name call) reasoning. The recovery
stays (the left type is treated as the payload, so one dead `??` does not cascade),
and the backend keeps its own loud refusal as a broken-guarantee defense (rule 5 —
there is no meaning to lower, and inventing one, e.g. "just evaluate the left", would
need its own ownership analysis for a construct whose only correct fix is deleting
the `??`). Tests:
`llvm_null_coalescing_test.go` (exec: payload/default/computed/chained, laziness both
ways, managed ASan, untyped-narrowing), `null_coalescing_test.go` (the two new
typechecker rules).

**A pattern literal is value-checked against the type it is compared to
(`lyra-E048`), and return-position literals joined the decl sites.** The second
audit sweep's headline finding, and unlike most dead-arm bugs it was a
**miscompile**: the backend lowers a pattern constant at the scrutinee's width, so
`match x { 300 => … }` on a u8 did not fail to match — it **matched 44**. `-1`
matched 255 (the negative-indexing bug's spirit in pattern position, found hours
after 08/12 removed it from indexing), `Some(300)` on a `Maybe<u8>` matched
`Some(44)` through the payload, and range-pattern bounds were equally unchecked. A
wrong value in a pattern is worse than in an expression: it redirects control flow.
Every one of these positions holds a compile-time constant by grammar, so the
standing ladder ("provable → compile error, otherwise → trap") collapses to its
first rung — there is no runtime half to build. Rust draws the same line ("range
endpoint is out of range" on `200..=300u8`), for the same reason: an out-of-range
bound is a bug in what the author wrote, not a clamp request.

**The shape of the fix is a conservative mirror, and why it must be one is the
interesting part.** The pairing of sub-pattern to sub-type already exists —
`walkDestructuredPattern` — but `withPatternBindings` runs that walk with its
errors *discarded* (deliberately: the per-kind arm checks own those reports),
which is exactly where a value-check's reports must not vanish. So
`pattern_literals.go` re-walks the pairing (binding, tuple, array, struct, and the
three data-payload shapes `bindDataPatternPayload` pairs), sharing the shape
helpers where they exist, and **skips silently anything it does not recognize** —
the authoritative shape errors live elsewhere, so a miss here degrades to a lost
diagnostic, never a false one. At the leaf, two flavors of impossible: the value
does not fit the base width, or it is outside a newtype's range constraint
(`pattern 200 is outside the range 0..<=100 of Percent, so this arm can never
match`) — the latter through `intOutsideRangeConstraint`, `checkIntRange`'s
judgment extracted into a shared predicate so pattern and expression positions
cannot come to disagree about one constraint (hazard 8's rule).

**One deliberate grace: an exclusive range end checks its bound minus one.**
`0..<256` on a u8 is exactly `0..<=255` — the full range, which the exhaustiveness
analysis already reads it as — so what must fit is the last *included* value. The
suite caught this immediately (`TestTypeCheck_NumericMatch_U8_ExclusiveRange_Ok`
failed on the first draft, which checked the bound itself), which is the
one-past-the-end convention `slice`'s end and `byte_offset(n)` already carry.
`0..<257` is refused; the grace is exactly one.

**Two adjacent holes closed in the same change.** A **newtype scrutinee** matched
none of `checkMatchExpr`'s kind branches — not numeric, not bool, not string — so
its match skipped *all* arm policing and exhaustiveness analysis, silently. Kind
dispatch now strips to the base type (`types.StripNewtype`); the data/tuple/struct
branches keep the unstripped type, which `lyra-E041` (no newtype over nominal
types) makes sufficient. And the **return-position gap open since 08/08** —
`() -> u8 => 300` compiled and returned 44 — is this same family in expression
position: `propagateLiteralType` deliberately leaves an unfitting literal untyped
for a downstream site to report, and a return has no downstream. `checkReturnValue`
now makes the same `checkIntegerLiteralRange` call every decl site makes, covering
the expression-body and nested-`return` forms alike.

**One test migrated, and its migration is evidence of consistency rather than
churn**: `TestExec_TraitMethodNarrowsToItsDeclaredReturn` computed `200 + 100` in a
`-> u8` return to pin trap-parity between trait methods and free functions — and a
*constant* overflowing expression in return position is now a compile error (decl
sites always refused exactly that expression; returns joined them). It pins the
same parity with runtime operands now. Tests:
`pkg/analyzer/typechecker/tests/pattern_range_test.go`.

### 08/12/26
**`s.len()` is O(1) — the rune count rides the fat pointer — and `for i, c in s` is
the indexed traversal.** The audit's last standing tension, and the subtlest of its
findings: not a wrong answer anywhere, but a *defense* that endorsed what the design's
own performance model condemned. `len()` had to count runes (it agrees with `s[i]` and
`for c in s`), and the docs justified that by holding up `for i in 0..<s.len() { s[i] }`
as the loop being protected — a loop whose every `s[i]` decodes from the start (O(n²)),
and whose `len()` calls alone measured 99.7% of `starts_with`'s cost when the prelude
fell into exactly this trap on 08/08.

**The string is `{ptr, byte_len, rune_count}` now** (STRING_LAYOUT.md; 24 bytes,
was 16; the count appended as field 2, so every field-0/1 consumer kept its index).
What makes the field affordable is that construction almost always knows the count
**arithmetically**: a literal counts at compile time (`utf8.RuneCountInString`), `++`
adds its operands' counts (concatenation cannot split a rune), `slice` subtracts its
rune bounds (they *are* rune indices). Only the two byte-sourced producers pay a scan —
read_line's libc buffer and interpolation's formatted segments — via `lyra_utf8_count`,
a lead-byte counter ((b & 0xC0) != 0x80, no decoding) over bytes they just produced,
cache-hot beside the allocation. Five construction sites total, and the hazard of the
design is a sixth that forgets the field — a silently wrong `len()` — so the **count
ledger** (`TestExec_StringRuneCountAgreesEverywhere`) recounts every producer's answer
with a `for c in s` walk, and an IR pin asserts len() contains no decode loop.
Measured: 100k `len()` calls on an 18000-rune string, **0 µs**. The exec suite passed
unchanged on the first full run after the layout change — the shape pins (`{ i8*, i64 }`
texts, the 16-byte size) were the only casualties, which is the evidence the five
sites were the complete set.

**`for i, c in s`** — the deferred index/rune pairing — lowers as a rune counter
beside the byte cursor: one linear walk, the array convention (first name the index,
second the element). The typechecker needed *nothing* — `bindForInLoopVars` had been
generic over iterables all along, and only the backend's loud deferral stood in the
way. This is the form the docs now hold up where they used to hold up the quadratic
loop; `s[i]` in a loop remains legal, remains O(i) per access, and the docs now say
that instead of endorsing it. The multi-byte fixture pins the index being a *rune*
counter: over "日本語" the pairs read 0:日 1:本 2:語, not 0/3/6.

One test-infrastructure lesson rode along: the conservation tracker treats an unknown
callee as an escape (correct — a callee may take ownership), so handing the fresh
box's payload to `lyra_utf8_count` made interpolation's allocation invisible to the
path check, and the corpus guard ("this program no longer exercises the check") caught
the *test* going vacuous — the exact failure mode that guard exists for. The counter
joined memcpy/memcmp on the reads-bytes-only list.

### 08/12/26
**Negative indexing is removed; `from_end(k)` is the end-relative accessor** — the one
audit finding that was a deliberate design rather than an implementation gap, reversed
four days after it landed. `s[-1]`/`xs[-1]` counted from the end (08/08, Python's rule),
and the objection was the design's own thesis: overflow traps, narrowing errors, shifts
trap, `split("")` traps — and an index underflowing past zero, the most common
off-by-one in existence, got a *valid read of the wrong element*. The 08/08 defense
measured the operation (34272 µs → 18 µs for the last rune) and concluded the spelling;
the correction keeps the operation and fixes the spelling.

**`from_end(k)`**, on strings and arrays alike, 1-based — "the 1st from the end" is how
the phrase reads, and it maps exactly onto the `-1`/`-2` it replaced. A builtin like the
index it mirrors (`pure noalloc` for free). On a string it lowers to the *same* backward
byte walk `s[-k]` lowered to: `lyra_str_rune_offset`'s negative branch is unchanged and
is now the method's **private contract** — which flips its hazard polarity, since every
caller handing the helper a surface index must now reject a negative first or the value
silently means from-the-end again (the string-index lowering grew exactly that guard).
Re-measured after the change: `from_end(1)` ×2000 on an ~1800-rune string is **2 µs**
where `s[s.len() - 1]` ×2000 is **6082 µs** — the win the accessor was obliged to keep.
On an array it is `len - k` behind one unsigned compare (`len - k >= len` catches k < 1
and k > len at once, the same trick the index paths use).

**The removal follows the standing ladder** — provable → compile error, otherwise →
trap. A constant negative index (literal or folded `let`) is refused in `inferIndexExpr`
naming the fix with the right ordinal ("use `.from_end(2)` for the 2nd value from the
end"); a runtime negative hits the bounds trap, which got *cheaper*: the from-the-end
`select` adjustment is deleted from all three index lowerings (fixed, dynamic, lvalue),
leaving the single unsigned compare whose sign-extension trick already caught negatives.
The value-range pass got **sharper**, not just adjusted: `[0, size)` is the whole valid
range now, so any provably-negative index — `if i < 0 { xs[i] }`, the refined
off-by-one itself — is definite `lyra-E022`, where `[-size, -1]` used to be a valid
from-the-end read the analysis had to let through.

The satellites went with it, each to its own answer: `slice`'s negative bounds trap
(the positional `s.slice(1, s.len() - 1)` is the same complexity class, since slice
already walks and copies — which is why from_end has no slice-bound counterpart), and
`byte_offset`'s negative position is `None` — now *agreeing* with `index`'s negative
offset, whose entry had recorded the disagreement as a live tension. An
index-assignment target has no end-relative form; `xs[xs.len() - 1] = v` is O(1) on
arrays, the only place assignment exists.

Tests migrated rather than deleted where they pinned semantics: the negative-index
exec tests became from_end tests with the same multi-byte fixtures ("日本語" still pins
the walk landing on lead bytes), and each family keeps one *negative-traps* case
pinning the change itself. The prelude used no negative indexing anywhere — the
feature was four days old — which is the "migrate before the library grows" argument
in its purest form.

### 08/12/26
**`noalloc` charges closures — by capture** (`lyra-E016`), closing the last finding of
the morning's audit. A `noalloc` function containing a capturing lambda had checked
clean while its emitted body called `lyra_rc_alloc` for the environment on every
invocation: the `slice` hole of 08/06 in a new coat — a bound that silently stopped
binding — defended by a doctrine ("`noalloc` is defined against the *release*
lowering") that deferred the charge to a Lambda Set Specialization lowering that is
not built.

**The charge is exact, not conservative, because the backend already draws the line.**
A nested lambda that captures heap-boxes its environment per construction
(`buildEnv` → `rcAllocPayload`); a capture-free one is the shared **pinned static**
(`emptyEnv` — "a plain function used as a value costs no allocation", the
string-literal device). So the rule is: charge exactly the capturing constructions.
That split also retires the LSS deferral on its own terms — a capture-free closure is
free under *both* tiers, an escaping capturing one allocates under both, and the one
case LSS would change (a non-escaping capturing closure) can only make the rule
**looser**, which is the compatible direction. What ships is charged for what it does.

Mechanically: the captures pass moved ahead of purity in the driver (it needs only the
TypeTable, so the reorder cost nothing), `CheckPurity` takes the captures table, and
the charge lives in the existing nested-lambda boundary arm of both effect walks —
which already `return false` there, confirming the model this rides on: a nested
lambda's *body* is a separate boundary charged at its call sites, and the construction
is the only thing that happens *here*. The `lyra-E016` site report names it ("a
closure captures its environment into a heap box at 2:13"), and the callee-path form
list gained "a closure that captures".

What stays free is as load-bearing as what is charged, and each is pinned: a callback
*parameter* received and called (the prelude's combinators are `pure noalloc` on
exactly that shape — the whole suite compiling the prelude is the regression test), a
top-level function passed by name (capture-free by construction), and a capture-free
nested helper. The charge travels through inference, so an unannotated closure-maker
refuses its `noalloc` callers at the call; the trait-method copy of the walk charges
identically (hazard 8's standing pair, pinned separately).

### 08/12/26
**A generic newtype constructs by call.** `Boxed(5)` is `Boxed<i64>`, and the whole
change is one arm learning to use machinery that already existed: the parameters are
solved from the operand by `solveDataTypeVars` — the named-tuple/data-constructor
solver, handed the newtype's base as its one declared field — or bound explicitly by
the `::<>` turbofish, the same ladder `inferNamedTupleLiteralExpr` runs. The bound set
then resolves through `ParameterizedType`, the same expansion the annotation form
(`let b: Boxed<i64> = 5`) has always taken, so everything downstream — the E046/E047
checks, constraint enforcement, the ownership pass-through, the backend's
forward-the-operand lowering — sees the substituted ConstrainedType it already knew.
No backend change; the solved result is fully nominal (its implicit read-out is
E047, pinned).

An untyped operand promotes to its default (`Boxed(5)` is `Boxed<i64>`, exactly as
`Some(5)` is `Maybe<i64>`), and a narrower instantiation is reached by saying so —
`Boxed(u8(7))`. What `lyra-E044` still refuses is a parameter the operand cannot
solve: `newtype Weird<t> = i64` mentions `t` nowhere in its base, so only the
turbofish can bind it, and the message names that spelling. The blanket refusal this
replaces had called itself "cannot be constructed by call *yet*" — accurate about the
state, wrong about the reason, which was a missing solver rather than a missing
answer.

### 08/12/26
**The newtype read-out is explicit too** (`lyra-E047`), completing the boundary work
below: Ada's rule now holds in both directions, and the same-day sequencing is why the
migration stayed cheap — the test suite had just been through the inbound flip.

**The spelling is the base's name applied** — `i64(c)`, the constructor's mirror, an
identity at runtime just as the constructor is. Three pieces made it exist:
conversions **look through a newtype on their operand** (so `i64(c)` is the identity
conversion `i64(x)` always was, and `u8(cents)` is admitted or refused exactly as
`u8(plain_i64)`); `string(...)` and `bool(...)` joined the conversion set as
**identity-only** targets, since string/bool bases otherwise had no spelling (no
stringification, no truthiness — an operand that is not the target after the strip is
refused, and no grammar change was needed: both already parsed as calls and died as
"undefined function"); and the backend returns the operand unchanged when source
equals target after its own strip.

**The refusal** (`checkImplicitNewtypeReadout`) is E046's mirror on the same
propagation path, with two structural differences. There is no literal half — a
newtype value is never a literal, so the refusal is unconditional where it applies.
And it applies only where the base is **nameable**: a newtype over an array or a
function type keeps its implicit read-out, because refusing with no spelling to offer
would make it write-only — pinned as the documented limit, so a future spelling knows
what to flip. The walk needed one new distinction: below the newtype arm the
propagation carries the *base*, so a `viaNewtype` flag rides the value-position chain
to keep `let c2: Cents = c` — one newtype on both sides — from reading as a read-out.

**Three passes had to learn that the transparent forms are their operand**, and each
lesson was earned rather than guessed:

- **Ownership.** `let s = string(copy.name)` routed through the unresolved-callee
  defaults and bound the same box with neither a retain nor a matching release — the
  ASan conservation count (`allocs + retains == releases`) caught it the day the
  spelling arrived, which is that test's whole argument. A conversion call and a
  newtype construction are now analyzed as their operand standing in that position,
  needOwned and all, matching what the backend emits.
- **Purity.** The conversion list was a fourth copy of "is this callee a
  conversion?", and it had **already drifted**: no `rune`, so `rune(n)` — the explicit
  spelling for building a code point, in exactly the classification arithmetic `pure`
  code writes — fell to the unresolved-callee default and was charged as impure. All
  four copies (typechecker, purity, ownership, backend) now delegate to one
  `types.ConversionTargetName`, hazard 8's durable fix.
- **Value-range** had been taught the constructor in the previous change; the
  conversion side arrives untracked (⊤), which is sound.

The E043 message now names `i64(...)` as the escape it had been spelling as an
annotated binding, and assignable.go's newtype comments record the layering: the
type-level rules answer "could this flow at all", the expression-level checks answer
"must the author write it down".

### 08/12/26
**A newtype means something at a boundary**, in three changes that answer one question:
where may a value become a newtype, and what is checked when it does. Sequenced so each
was independently reviewable — a bug fix, then an addition, then the rule that needed
both.

**1. A constraint is checked wherever the type flows, not only at a binding.**
`range(...)`/`pattern(...)` were called from the two assignment sites, so
`let p: Percent = 150` was caught while the same literal reaching the same newtype
through an **argument** (`show_it(150)`), a **return** (`-> Percent => 150`) or an
**array element** (`let xs: []Percent = [10, 150]`) was not. Silently — in the feature
whose entire purpose is to be checked. They ride `propagateLiteralType`'s newtype arm
now, the one point a newtype context reaches a value in every position it can arrive
from; that is hazard 8's rule (one predicate on the path the type already travels)
rather than the same check re-attached at each consumer. The two cases anyone tries
first — an annotation, and a variable the value-range pass independently proves — both
worked, which is why the hole was invisible.

`values(...)` is enforced for the first time (`lyra-E045`). It was collected, its shape
validated, and read by nobody, so `let s: Status = 302` against `values(200, 404, 500)`
compiled clean: the collected-and-unread shape again, in the one place where being
checked is the whole point of writing the declaration.

Two smaller things rode along. The pattern message wrapped an already-delimited pattern
in a second `r"…"` (`pattern constraint r"r"^#…$""`). And the range message no longer
leads with a binding name — most positions it now covers have none to give, the
diagnostic's location *is* the literal, and the value-range pass's sibling message
already read that way.

**2. A newtype has a constructor.** `Cents(150)`, and the juxtaposed `Cents 150` for
free, since the collector erases that spelling into the same node. It is a compile-time
assertion about which type a value has, not a wrapper: a newtype is nominal to the
typechecker and transparent to codegen, so it lowers to **its operand and nothing
else**. The backend arm reads the *raw* recorded type, because `recordedType` strips
newtypes — through it a `Cents(150)` looks like an i64 recorded on a tuple literal.

The operand is checked against the **base**, not the newtype, which is what keeps the
constructor meaningful under (3): construction is exactly the act of turning a base
value into a newtype value. Constraints are enforced through it for free by propagating
the newtype onto the operand, so `Percent(150)` gets the same report the annotation
does. `lyra-E044` — which had spent an hour meaning "a newtype has *no* constructor" —
now covers what is still malformed: the wrong operand count, an operand the base cannot
hold, and a generic newtype (whose base is a type variable, with nothing to check
against until the parameters are bound).

**3. A typed value needs the constructor; a literal does not** (`lyra-E046`). This is
the rule the other two were for. Until now base → newtype was assignable everywhere, so
`let plain: i64 = 150` followed by `take(plain)` against `(c: Cents)` compiled silently
— a newtype declared a distinction the compiler then declined to enforce at any
boundary, which also made `lyra-E043` narrower than its own rationale, since the same
laundering was available through any user-written function.

**The line is provenance, not convenience.** A literal has no unit yet — `150` is not
"150 of something else" — so adopting it costs nothing and reads as what the author
meant; a *typed* value came from somewhere, and that somewhere is where a unit mixup
lives. It is Ada's rule for derived types, for the same reason: `M : Meters := 3.0` is
legal, `M := F` for a `Float` F is not, `Meters(F)` is the conversion. Keeping literals
implicit is what makes the strict half affordable — `let xs: []Percent = [10, 20, 30]`
needs no ceremony, which was the standing objection to requiring constructors
everywhere.

It is enforced from the same newtype arm as (1), and it needs **both** halves of the
source: `isUntypedLiteralType` for numerics (where `untyped_int` already means
"constant with no provenance", so constant arithmetic is covered without walking the
expression) and `isSyntacticLiteral` for the rest, because a string or bool literal has
the same type as a variable holding one — there is no `untyped_string`. That is also
why the rule cannot live in `isAssignable`, which sees only types.

Three things it deliberately does not touch: reading *out* (`let raw: i64 = c`) stays
implicit, since there is no field accessor and refusing it would make a newtype
write-only; a value that is already the newtype is not a conversion; and a *different*
newtype keeps its own distinctness error rather than doubling up.

**Two bugs found by building it, both of the kind only a test catches.** The check
initially read the source type back off the value node — and `checkVarDecl` records the
*annotation* there before narrowing, so it saw `Cents` where the source was an `i64` and
concluded nothing was being converted. The binding position, the most obvious case in
the feature, silently passed; the from-type is a parameter now.

And the migration exposed a real regression in the **value-range pass**: written through
the constructor, `let p: Percent = Percent(y)` reported nothing where `let p: Percent =
y` had reported a definite violation, because a construction evaluated to ⊤ (untracked).
That would have made the stricter rule a *net loss* for constraint checking — the
opposite of the point. A newtype construction now evaluates to exactly its operand's
interval (the wrapper is nominal, so at runtime they are one value), and the identifier
guard tests the construction's **operand**, so a constructed *constant* stays the
typechecker's to report rather than being reported twice.

Migration was 14 call sites across the test suite and **nothing in `std/`**, which
declares no newtypes — the reason to do this now rather than later. Several read better
for it: an operator impl's `Cents(a.wrapping_add(b))` says what it does where
`let sum: Cents = a.wrapping_add(b)` relied on a silent conversion.

### 08/12/26
**The overflow-arithmetic builtins stop at a newtype's wrapper (`lyra-E043`), and the
operator impl they defer to dispatches on a scalar newtype for the first time.** Two
halves of one repair, and the second was found because the first's diagnostic
recommended it.

**The bypass.** With `newtype Cents = i64` and no impls, `a + b` was refused ("operands
must be numeric, got Cents and Cents" — the opt-in working) while `a.wrapping_add(b)`
compiled — and so did `a.wrapping_add(plain_i64)`, a **mixed** operand, with the result
assignable as either type. The transparency fallback (a newtype reaches its base's
methods, argued for `len`/`slice`/`trim` on a wrapped string) re-tried the builtin
registry at the base, and the registry's signatures are base-typed — so the *checked*
spelling honored the nominal barrier while the explicitly-unchecked escape hatches
ignored it, mixed operands included. A pit of success inverted.

**The rule now:** `wrapping_*`/`saturating_*`/`checked_*` are refused on a newtype
receiver, with the message naming both explicit paths through — an operator impl, or
reading the value into the base (`let raw: i64 = c`, one step, documented
assignability). The refusal is exactly the overflow-arithmetic family, because that
family is the *operators'* escape hatches and the operators are opt-in for a newtype;
the float rounding ops (`floor`/`ceil`/`round`) stay transparent, being `i64(x)`'s
alternative rather than an operator's, and the string methods stay transparent, being
what transparency was argued for.

**Why not "make the argument match the newtype" instead** — the softer option
dissolved on contact with the assignability rules. Base → newtype is assignable *by
construction* (assignable.go: "a value satisfying the base is assignable to a
newtype"), so a `Cents`-typed parameter accepts a plain `i64` everywhere in the
language, and enforcing strictness only inside the builtin registry would disagree
with every other parameter while pretending to a guarantee assignment does not make.

**It reverses a recorded decision, knowingly.** `TestChecked_ReachableThroughANewtype`
pinned the bypass with the argument "a wrapped integer you cannot do arithmetic on is
not a trade anyone would take" — written without noticing the operator half was
already making exactly that trade, and that the fallback accepted a mixed operand the
checked `+` refused. The test is kept, inverted, with the reversal's reasoning in
place.

**The guard bug the diagnostic flushed out.** The E043 message says "give Cents an
operator impl" — and it did not work: `dispatchOperator`'s primitive guard
newtype-stripped its receiver before refusing scalar receivers, so a scalar newtype
was operator-dead from *both* sides — the built-in numeric rule refused the nominal
type and the guard refused the base, leaving `impl Add for Cents` parsed, collected,
and silently inert (the collected-and-unread shape again, one resolution step further
along). The guard now tests the receiver unstripped: a `ConstrainedType` is not a
`PrimitiveType`, so a newtype proceeds to impl lookup while `impl Add for i64` stays
inert exactly as before (`TestOperator_PrimitivesKeepTheirBuiltInMeaning`). Two
`Cents`-annotated bindings add for the first time; end-to-end, an impl body reads its
operands into the base, does wrapping arithmetic there, and hands back a `Cents`,
with the caller's `x + y + x` pinning the return type — a `let total: Cents`
annotation would not, since base → newtype is assignable anyway
(`TestExec_OperatorOnScalarNewtype`).

**A correction to this entry as first written**, which claimed the operator section's
`Cents(150) + Cents(275)` example "dispatches for the first time". It does not: a
newtype has **no constructor call**, so that exact program does not compile. The 08/07
entry the example comes from is about a *parse* — a constructor call as a math operand
— and its behavioural test uses a `data` type, so it is correct as written; this entry
borrowed the spelling and asserted something stronger about a form the language does
not have. The wrong claim was live for about an hour, in this file and in todo.md.

**And the gap it exposed is now reported properly** (`lyra-E044`). `Cents(150)` parses
as a named-tuple literal, so it had been failing with *"Cents: not a tuple type"* —
true, useless, and naming a concept the author did not write. It now says a newtype has
no constructor and names how one is made (`let x: Cents = ...`), which is exactly the
fix lyra-E035 applied to `Rng.seeded(42)`: say what the language has rather than what
the parse was. The juxtaposed `Cents 150` reaches the same collector path and so gets
the same message; a genuine `tuple Point(i64, i64)` is untouched, since the new arm
keys on the declaration being a newtype rather than on the literal's shape.

Whether a newtype *should* have a constructor is left open in todo.md, with the finding
that decides it recorded there: implicit base → newtype conversion is unenforced at
every boundary today (`take(plain_i64)` against `(c: Cents)` compiles silently), so the
constructor is only worth adding as the half of a change that makes construction
explicit — and the standard library declares no newtypes, so that change is as cheap
now as it will ever be.

Printing remained the honest residue: `println(c)` on a newtype still refuses (the
formatter is picked per concrete type), and `impl Show for Cents` already works —
verified — which is arguably the right shape anyway, since print is the one place
transparency would *erase* the name the newtype exists to carry rather than merely
reach past it. Recorded as open in todo.md.

Tests: the `lyra-E043` block in `constrained_type_test.go` (refusal for wrapping and
checked, the mixed operand, float rounding and string transparency preserved, both
recommended escape paths compiling), the inverted
`TestChecked_NotReachableThroughANewtype`, and `TestExec_OperatorOnScalarNewtype`.

### 08/12/26
**A `for-in` range can no longer loop forever.** Two silent infinite loops in the counter
lowering, both closed in `lowerForInRange` (`pkg/backend/llvm/control_flow.go`), both found
by auditing the language against its own trap-on-overflow thesis rather than by hitting
them — and the finding underneath is about *where they lived*: neither was in `todo.md`.
The first was documented only as a code comment calling itself "the one edge to keep in
mind", which is the C attitude toward wrap this language defines itself against; the
second was documented nowhere.

**The wrap at the type's edge.** The advance was a plain add, so `for i in 0..<=hi` with
`hi: u8 = 255` incremented 255 to 0 and re-entered the range — forever, silently, in the
language whose `+` traps on exactly that wrap. A large step did the same to an *exclusive*
end by leaping it: `0..<250:100` over u8 is 0, 100, 200, then 200 + 100 = 44, still under
250. The advance is now guarded — the counter moves only when it can move by `step` and
stay inside the range:

- `dist` is the distance to the end bound **along the iteration direction** (`end - i`
  ascending, `i - end` descending), as a raw two's-complement subtraction compared
  **unsigned** at every counter type. The cond block has already held, so the counter is on
  the range's side of the end and the raw difference *is* the distance; a signed
  subtraction could itself overflow (`end = MAX, i = MIN` spans the whole domain — the
  full-domain `lo..<=hi` over i8 is a test case).
- An exclusive end continues on `step < dist` (the next value must land strictly inside),
  an inclusive one on `step <= dist` (it may land on the end). The landing-exactly case
  then exits on the next check with `dist = 0`, and a step that *skips* the end exits the
  same way — the guard is one rule, not an equality special case (`0..<=255:2` over u8 is
  the test: it never lands on 255 and must still terminate).

**An exit, not a trap** — the one place the thesis does *not* want a trap: visiting the
type's own max is what the author asked for, and nothing in what they wrote overflows;
the wrap was the lowering's artifact, not the program's arithmetic. The comprehension had
been given exactly this treatment on 08/04 ("the capacity bounds the loop by
construction"); the `for-in` half was simply never filed, and the stale note beside the
comprehension entry — "a backwards `for-in` range still loops forever" — turned out to
describe a bug that *had* been fixed (descending operators made `5..<1` an empty ascending
range in both forms) while standing in for the two that hadn't.

**The runtime step.** `types.InvalidStepReason` refuses a constant zero or negative step —
and only ever sees constants, so `for i in 0..<10:n` with `n` computed as 0 at run time
compiled clean and spun. It now rides the ladder a shift amount rides, for the recorded
reason ("the alternative is a silently target-shaped answer"): provable → compile error,
otherwise → a runtime check that traps (`lyra: range step must be positive`, exit 101,
`lyra_panic_range_step` on the shared `panicFunc` machinery). The check is emitted only
for a non-constant step — a constant already passed the typechecker, and a non-positive
constant reaching the backend is reported loudly as the front-end failure it would be
(rule 5) rather than guarded around. Unsigned counters test only `== 0`, since their step
cannot be negative.

**One deliberate divergence, recorded on both sides:** a comprehension answers the same
degenerate step with an **empty array**, not a trap — its count is computed up front, so
"never advances" has a defined size there, where a re-testing loop's only alternatives are
a trap or the runaway it used to be. `rangeSource`'s comment and the trap's own doc
comment each point at the other.

Tests: `TestExec_ForInRangeEdgeTermination` (inclusive end at the u8 max, the full i8
domain min-to-max, a stepped inclusive end that skips the max, a large step over an
exclusive end, descending onto the unsigned min, and a positive runtime step that must
*not* trap) and `TestExec_ForInRangeRuntimeStepTraps` (zero and negative runtime steps,
asserting the message and exit 101), in `llvm_forin_range_test.go`.

### 08/09/26
**A `return` inside a trait-impl method is legal**, and was reported as
`lyra-E003: return statement outside of a function body`.

`CheckReturnOutsideFunction` counts function depth by **LambdaExpr**, and a trait method's
body hangs off `TraitImplStmt.Methods[i].Clause.Body` without being wrapped in one.
`walk.go` descends into it with the *same* visitors, so the body was walked at the
enclosing depth — 0 for a top-level `impl` — and every `return` in it was reported.

It is the nested-return bug from the day before, one level out: one question — *am I
inside a function?* — and a body form that only one of the two ways of answering it knows
about. The reason it sat undiscovered is that every impl the prelude shipped was a single
tail expression; guard clauses in a trait method are what first needed it, which is to say
the feature was unreachable rather than merely awkward.

The fix opens a function scope for `TraitImplStmt` method bodies the way the existing
`exprVisitor` already does for lambdas, and stops the generic descent. Trait **default**
method bodies got the same treatment — `walk.go` descends into those identically, so they
would have failed the same way the first time anyone used the form. The regression tests
include a top-level `return` that must *still* be reported, since the failure mode of this
kind of fix is opening a scope for the enclosing statement rather than for the body.

Found by writing `trait Needle`, which folds `index_rune`/`contains_rune` into a single
`index`/`contains` generic over the needle. Two claims made against that design earlier the
same day were wrong and are corrected in `todo.md`: the trait method *can* carry the byte
cursor, and its dispatch costs 6% rather than 4x once the build is not `-O0`.

### 08/09/26
**`lyrac` links at `-O2` by default**, where it had passed no `-O` flag at all and so
shipped everything at clang's `-O0`. A `-O<level>` flag overrides it.

**The usual argument for `-O0` does not apply here, and that is the whole decision.**
Defaulting to unoptimized buys debuggability — except this backend emits **no debug
info at any level**, so there is nothing to step through either way. What `-O0` actually
bought was build time, against a large and permanent runtime cost.

Measured, on the workloads that motivated looking:

| | -O0 | -O2 |
|---|---|---|
| string scan (`index` / `index_rune` over 4001 runes) | 15925 µs | 5087 µs |
| arithmetic loop, 20M iterations | 22391 µs | ~0 (closed by the optimizer) |
| link time, 2091-line module | 0.049 s | 0.103 s |

So roughly 3x on ordinary code, and complete elimination where the optimizer can close
a loop, for about 50 ms of link time. `-O1` captures most of it; `-O3` adds little over
`-O2`; the conventional default is the right one.

**It came out of a different question.** Comparing a `Needle`-trait `index` against the
direct one showed the trait 4x slower — which looked like an argument against the trait
until the same comparison at `-O2` put it at 6%. The dispatch cost was an artifact of
the build, not a property of the design, and every performance number measured in the
prelude before this — `starts_with`, `byte_offset`, the Rabin–Karp discussion — was
taken against unoptimized code. That is the more useful finding than the flag.

**The safety check is the part worth keeping.** Optimization exercises undefined
behaviour far harder than `-O0` does, and this project has a history of IR faults that
only stricter conditions surface (Debian's typed pointers, which is what `asan.sh`
exists for). The whole backend behavioural suite — refcounting, weak references,
generics, strings, arrays, the ASan tests — passes at `-O1`, `-O2`, `-O3` **and** `-Os`.
The default is not resting on `-O2` happening to be gentle.

Two smaller decisions: the level is matched as `-O` plus anything and passed through
unexamined, so `-Os`/`-Oz`/`-Ofast` work and an unknown level is clang's error to report
in its own words rather than a second, staler copy of clang's list; and both
"compile it with" hints — `--emit-llvm`'s and the missing-compiler fallback's — carry
the level, since a hint naming a different build than the one it stands in for is worse
than no hint.

### 08/09/26
**`xs.push(v)` — the dynamic-array growth operation, and the representation change it
required.**

**The blocker was never the operation, it was the layout.** A `[]T` was a single box —
`ptr → { rc, weak, len, [0 x T] }` — with the elements *inline*, and a `[]T` **value is
that box pointer**. So growing meant moving the elements, which meant moving the box,
which leaves every other binding holding a dangling pointer. That is not a semantics
question one could decide either way: aliasing is already observable, verified before
touching anything —

```lyra
var a: []i64 = [1, 2, 3]
let b = a
a[0] = 99          // b[0] is 99
```

— so a relocating push is a use-after-free.

**So the elements moved behind a pointer**: `{ rc, weak, len, cap, T* elems }`. The box
address never changes, only the buffer is `realloc`'d, and every alias sees the push —
which is the reference semantics `[]T` already had for element assignment. **The cost is
one extra load on every element access, language-wide and permanent.** LLVM hoists it out
of a read-only loop but not out of one containing a push. That is what every growable
reference container pays, and it is why the alternative — leaving `[]T` alone and adding a
separate growable type — was rejected: `[]T` is *already* heap-allocated and
reference-semantic, so it is the vector and was merely missing an operation; a second type
would be two things at the same point on the design axis, the redundancy the consistency
section settled against for `data`/`struct`/`tuple`.

Three things fell out of the layout change that were not obvious going in:

- **Every `[]T` needs drop glue now, including `[]i64`.** `dynArrayDropFn` returned
  `nullDropFn` when the element type owned nothing managed — correct while the elements
  were freed with the box, and a leak of the buffer of every scalar dynamic array in the
  program once they are not. The element *loop* is what is conditional on `needsDrop`, not
  the function.
- **The buffer is freed after the elements, never before**: an element's own release may
  read through the buffer it is stored in.
- **`malloc(0)` for an empty array rather than a null.** It returns a pointer `free`
  accepts and `realloc` grows, so neither the drop glue nor push needs an empty case.

**The ownership bug is the one worth remembering.** A pushed managed value must transfer
into the array, and the conservative default for an unknown callee is exactly that — but a
builtin method is not unknown: the typechecker records its signature on the `MemberExpr`,
so `calleeType` answers first, and a builtin signature carries no written `own`/`ref`/`mut`,
which reads as a **borrow**. The temporary was released after the call while the array kept
the pointer. `s.push("b" ++ "!")` printed garbage; `s.push("alpha")` was fine, because a
literal interns as a pinned box whose release is a no-op — the coincidence that makes a
memory bug look like working code.

The fix is `calleeIsTransferringBuiltin`, mirroring the `calleeIsBorrowingBuiltin` that
already existed for `print`. Writing `own` on push's parameter would also have reached the
ownership pass, and was rejected for claiming something stronger: an owning position takes
a +1, it does not consume the caller's binding, and `xs.push(t); println(t)` is fine — the
pass simply retains when the push is not `t`'s last use. Keeping "transfers" and "consumes"
apart is why this is a hook rather than a mode.

Smaller decisions: amortized doubling from a floor of 4, so n pushes cost O(n) copying;
`push` returns void and mutates in place, since a value-returning push would be the odd one
out beside `xs[i] = v`; the receiver must permit interior mutation, checked by the same
`rootBindingIsMutable` predicate and reported with the same diagnostic, because push *is*
interior mutation with a different spelling; and `noalloc` refuses it, because the bound is
a static promise about what a function may do rather than a statistical one.

Verified under ASan **and** LeakSanitizer on Linux: growth from empty across the realloc
path, aliasing, `[]string` with heap elements, and `[][]i64` where the drop glue recurses
through a buffer it also has to free.

### 08/08/26
**`s.byte_offset(i) -> Maybe<i64>`** — the rune→byte conversion, and the last piece the
byte-level string set was missing.

Nothing in the language could perform it. `compare_bytes_at` answers "does `sep` occur at
byte offset b" and is the cheap primitive, but everything the *language* hands you is a rune
index — `index` returns one, `slice` takes them, `s[i]` uses them — so the fast test had no
argument to be given. A prelude `split` could ask "does `sep` occur at rune i" only by
allocating (`slice(i - m, i) == sep`, a fresh string per position tested) or by scanning to
the end of the string whenever the answer was no (`index(sep, i - m) == i - m`). Either makes
split quadratic; this makes it one memcmp.

The composition is the point, and it needs no `match`:

```lyra
let occurs_at = pure noalloc (self: string, sep: string, i: i64) -> bool =>
  self.compare_bytes_at(self.byte_offset(i).unwrap_or(-1), sep) == 0
```

That reads this way **because `compare_bytes_at` is total** — `unwrap_or(-1)` hands it an
offset it already answers negative for. Making it total rather than trapping was decided for
its own reasons on the same day; this is the second thing that decision bought, and the test
pinning the pair is deliberately written as the idiom rather than as two separate assertions,
so a later trap or a different out-of-range answer breaks *here* rather than in whatever
prelude function next reaches for it.

Two decisions:

- **It maps positions, not elements**, so the end position (`i` == the rune count) is
  `Some(byte_len)` rather than `None`. That is `slice`'s rule, not `s[i]`'s, and the
  asymmetry with the trapping `s[n]` is deliberate: this exists to convert *bounds*, and
  `s.slice(a, n)` is an ordinary slice, so `n` must have an answer. A negative `i` counts
  from the end, as everything else does since earlier the same day.
- **`Maybe<i64>` rather than -1**, for the reason the prelude's own functions return one — and
  it costs nothing, a Maybe of a scalar being an inline union, so it stays `noalloc`.
  Branchless, built the way `checked_*` builds its Maybe.

No new walk: it exposes `lyra_str_rune_offset`, which `s[i]` and `slice` already shared, so
there are now three callers of one definition rather than a third copy of the same question.

### 08/08/26
**Negative indices on strings, for `s[i]` and `s.slice(a, b)`.** `s[-1]` is the last rune
and `s.slice(1, -1)` drops it, which is the rule arrays already had.

**The reason to do it is not consistency, and the old comment is the reason it had not been
done.** `lowerStringIndex` recorded that a from-the-end form "would require a full rune
count first" — plausible, and false. Finding the k-th rune from the end is a **byte** walk
that steps back over continuation bytes (`10xxxxxx`) until it lands on a lead byte,
well-defined precisely because UTF-8 is self-synchronizing — the property `starts_with` and
`index` already lean on — and it **decodes nothing**. So it is O(k) in byte tests, where the
spelling it replaces is two full O(n) decode walks (`len()`, then `s[i]` at O(i)):

| reading the last rune of a 2000-rune string, 2000 reps | |
|---|---|
| `s[s.len() - 1]` | 34272 µs |
| `s[-1]` | **18 µs** |

So this removes an O(n) tax that had no workaround rather than adding a second spelling for
something already cheap — the opposite of the array case, where `xs[-1]` and `xs[size-1]`
cost the same.

**One definition, `lyra_str_rune_offset(data, byteLen, idx, allowEnd) -> i64`**, returning a
byte offset or -1. `s[i]` and `slice` had carried the same forward walk twice, which is why a
negative bound could not have been added to one without being added to the other by hand
(rule 8) — and folding them made `lowerStringIndex` shorter than before the feature. Two
details worth keeping:

- **It returns -1 instead of trapping**, so each caller raises the panic that fits it. The
  string-index and string-slice traps say different things, and a shared helper that trapped
  would have had to pick one.
- **`allowEnd` is the difference between an index and a bound.** `idx == runeCount` has a
  byte offset (the byte length) and slice needs it — an exclusive end, and `s.slice(n, n)` is
  "" — while `s[n]` must stay out of bounds. A negative index never reaches that case, since
  stepping back at least one rune always lands strictly inside.

**Two things in `slice` changed shape.** Its ordering test now compares the resolved *byte
offsets* rather than the written bounds, which is what admits `slice(1, -1)`: as written,
`1 > -1` would trap on an ordinary interval, and the rune-to-byte mapping is monotonic so the
two comparisons ask the same question. And it resolves each bound with its own call, giving
O(start) + O(end) where the old single pass was O(end). That pass is exactly what a negative
bound cannot be expressed in — it advances a rune counter forward, and no value of that
counter means "third from the end" — and the doubled decode sits inside a function that
already allocates and copies.

**`index`'s negative `offset` stays `None`, and now says why.** It had been justified by
strings having no from-the-end index, which this retires. The reason that survives is
different: an offset there is a *resumption point for a scan*, not an index — "resume at k
and keep going forward" has no reading for a negative k — and Python's `str.find`, which does
count its `start` from the end, is a well-known confusion for that reason.

### 08/08/26
**`starts_with` / `ends_with`, and the two byte-level primitives under them.** The prelude
gains both as one-liners; the compiler gains `s.byte_len()` (O(1) — the fat pointer's own
length field) and `s.compare_bytes_at(offset, other)` (memcmp at a byte offset). Both
predicates are `pure noalloc`.

**Written rune-indexed first, and that version is quadratic — which is the point of the
entry.** `s[i]` is O(i), so the natural loop is O(m²) for a prefix and O(n·m) for a suffix.
Worse, both paid an O(n) `len()` *before* comparing anything, so `s.starts_with("--")` on a
2000-rune string decoded all 2000 runes to answer a question about its first two bytes.
Isolated, that guard was **99.7%** of the cost: 19395 µs against 62 µs for the comparison it
was guarding.

The obvious repair — `self.slice(0, n).compare_bytes(other) == 0` — is faster and is the
wrong trade. It fixes the quadratic term (113 ms → 1.9 ms for a 400-rune needle) but
**allocates**, so `noalloc` refuses it, and `slice` still walks runes to find the byte
offset, so it stays O(n): on the short-needle case it won by only 1.3x, because both
implementations were walking the whole haystack for the length.

**Neither is the right shape, and the measurement is what says so.** A prefix test needs no
rune decoding at all. UTF-8 is prefix-free and self-synchronizing, so for a well-formed
string — which every Lyra string is — a byte-prefix is exactly a rune-prefix and a
byte-suffix exactly a rune-suffix. This is the same property `impl Ord for string` already
leans on to answer a rune-ordering question with one memcmp, and the same division of labour
`random_seed` and `read_line` follow: a memcmp at a byte offset cannot be written in Lyra
(byte offsets are not addressable from the language), the predicates on top of it are
ordinary code.

| case | rune loop | slice+compare_bytes | byte builtin |
|---|---|---|---|
| 2-rune needle, 2000-rune haystack, 2000 reps | 19884 µs | 14977 µs | **19 µs** |
| 2-rune needle, 30-rune haystack, 2000 reps | 289 µs | 317 µs | **18 µs** |
| 400-rune needle, 2000-rune haystack, 300 reps | 111887 µs | 1845 µs | **4 µs** |

Three decisions worth keeping:

- **`compare_bytes_at` compares exactly `other`'s length**, not the rest of `self`. That is
  the only semantic difference from `compare_bytes` and it is the whole reason it exists:
  comparing the remainder would make `"hello".compare_bytes_at(0, "he")` *positive* (self is
  longer), so `== 0` would be an equality test rather than a prefix test.
- **Total and branchless.** Every out-of-range case folds into a select — the offset is
  clamped before it reaches the GEP, the memcmp length is clamped to what `self` has, and a
  shortfall or an out-of-range offset forces a negative. No trap, so the prelude needs no
  guard (`ends_with` passes a possibly-negative offset straight in); branchless because a
  call site ending in a merge block is not a case `flushStmtTemps` handles, which is what
  made two `slice` results in one interpolation clobber each other on 08/07.
- **`byte_len()` is a deliberate, narrow crack** in "runes are the language, bytes are the
  representation". It exists so `compare_bytes_at` has an offset to be given, and the unit is
  in the name for the reason `wall_clock_nanos` puts its unit there. `len()` stays what
  ordinary code wants — it is the one that agrees with `s[i]` and `for c in s`.

One rule is easy to guess backwards, and both hand-written expectations for it were wrong the
first time: **a byte mismatch decides before a shortfall**, memcmp's own order. So
`"hello".compare_bytes_at(4, "lo")` is positive (`'o' > 'l'`) rather than negative for the
byte that is missing; short-sorts-first settles only a range that matched as far as it went.
Every predicate built on this asks `== 0`, where the distinction cannot arise — which is why
it is a comment and a test rather than a redesign.

**`ends_with` was wrong when first written, and the shape of the bug is worth recording**: it
was `starts_with` with the loop running backwards, indexing `self[i]` against `other[i]` —
a *prefix* comparison whichever direction the loop runs, since reversing the order of the
comparisons does not change which bytes are compared. So `"hello".ends_with("lo")` was false
and `"hello".ends_with("he")` was true, each answering the question the other one asked. It
looked right on the two cases that do not distinguish a prefix from a suffix (`s.ends_with(s)`
and a needle matching neither end), which is why the tests pair a prefix with a suffix of the
same string on every row.

**An import no longer makes an ordinary name unusable.** `import util.seq` (which exports a
`map`) plus a perfectly ordinary `let map = (n: i64) -> i64 => n + 1` was a hard error —
*function "map" is already defined at …/util/seq.lyra* — so a program had to choose between
importing a module and declaring a name it happened to export.

**The comparison is what made it wrong rather than merely strict.** The prelude — the names
you never asked for — took the *soft* path: the user's declaration warned (`lyra-W012`) and
won, with the prelude's still reachable. The names you deliberately imported took the hard
one. The explicit act punished and the implicit one forgiven, and the only reading a user
could take from the error is that the library owns `map` and their program may not have one.

It is one rule now. `shadowsPrelude` became **`shadowsAmbient`**: a module's own top-level
declaration of a name reaching it from *either* source is keyed `<module>::<name>`, the
source keeps the bare key, and the declaration warns (`lyra-W016`, W012's sibling with a
namespace to point at) instead of erroring. So the local declaration wins every bare
reference in that module, the shadowed one is reached as `seq.map`, and no other module is
affected. Nothing was invented — this is the key `declKeyIn` had been computing for the
prelude, asked of a wider set of sources, which is why the change is small enough to state
in one predicate.

**What stays an error, on purpose:** two modules exporting one name. A bare reference from a
third module could mean either and neither has a local declaration obviously meant to win, so
there is nothing for a shadowing rule to prefer — `exportToGlobal`'s check is untouched. That
includes a module `pub`-exporting a name it also imports, which is a second claim on the
program-wide name wearing an import; resolving *that* needs option (b) from `todo.md`
(qualified `pub` keys, bare lookups consulting the importing module's bindings), because the
answer depends on who is asking. Nothing shipped needs it.

**The one ordering constraint, and why `ImportGraph` exists.** A type's key is computed
*during* the walk, so the import graph must be complete before the first file is walked —
`Collector.SetImports`, called beside `SetPreludeModule`, whose own comment had already
recorded the identical trap. Assembling the graph per file as each is walked works for a
single-file module and quietly fails for the rest: a module whose `import` sits in its second
file would key the first file's types as though nothing were imported, and every later lookup
— with the graph complete — would compute a key that misses them. The other two inputs need
no such help, and it is worth knowing which: `ModuleDeclares` is true from the moment a
declaration lands in its own module's scope (which `RegisterType` does *before* computing the
key, and `recordModuleBindings` does before `Finish` registers a function), and
`ModuleExports` asks the *imported* module, which `Resolve` returns in dependency order.

**It surfaced two live bugs, both hazard 4's by-name form — a module question answered from a
name — and both already reachable without this feature.** Shadowing a *prelude* name has
qualified the shadowing declaration's key since 07/30, so a program that did that *and* then
called into a module through a namespace hit both; nothing did both at once, so nothing found
them. Verified on the commit before this change: `seq.map(20)` beside a local `let map`
reported `map is private to module "util.seq"`.

- **The `pub` check on a namespace member read the wrong module's binding.** `visibilityIn`
  falls through to `BindingOf(name)` for a function, since `pub` lives on the binding rather
  than the lambda — and `BindingOf` finds the module through `ModuleOf`, which is
  last-writer-wins. So `seq.map` looked up the *entry* file's `map`, found no `pub`, and
  reported an exported function as private to its own module. The fix is `BindingIn(module,
  name)`, the binding half of the `LookupTypeIn`/`LookupTraitIn` the same function had been
  using two lines above; its doc comment already explained why those are `In` forms.
- **The backend could not lower the namespace call at all.** `namespaceCallee` tested
  membership with `DeclaringModule` and took the callee out of `l.funcs[name]`, both sound
  only under the premise its own comment stated — "names are program-wide unique today". With
  a shadow in play the membership test rejected the call, it fell out of the path entirely,
  and the build died with `llvm: unsupported method call` on a program the front end had
  checked clean: rule 5 inverted. It asks `ModuleDeclares` now, and keys from the location of
  the **declaration it resolved to** rather than from the call's — the asking file being
  precisely the one that may have declared its own.

The behavioural half is pinned by an exec test rather than a type-check one, because "the
local declaration wins" is a codegen claim too: 30 from the local function plus 400 from the
imported one, so either half resolving to the other gives a different number rather than a
failure to build.

**Integer literals past 64 bits.** `let mx: i128 = 170141183460469231731687303715884105727`
is writable; before, a 128-bit constant had to be reached through arithmetic or an
`i128(x)` conversion, on a type the language otherwise supports fully.

The magnitude lives in a **`Wide *big.Int`** on the literal node, nil for every literal
that fits 64 bits. That choice is what kept the change small in the ways that matter: the
reflection printer omits nil fields, so no golden output moved, and every existing
`.Value` reader stayed correct for every input it had ever seen.

**The parse was the easy half; the readers were the work.** A dozen places take `.Value`
off an integer literal, and for a wide one that field is 0 — so the first end-to-end run
printed `0` for i128's maximum, silently. Each site had to be visited and given the
answer that is *conservative* for it:

- `Int64()` returns **ok=false** for a wide literal, which is the enforcement point: a
  consumer that folds in int64 declines instead of reading the zero. `ast.FoldIntExpr`,
  the value-range pass's `patternBound`, and `resolveConstantInt` all go through it.
- The value-range pass treats a wide literal as **untracked (⊤)**, like a large-u64 bit
  pattern. Reading 0 there would have been worse than wrong — it could have *proved away*
  an overflow trap.
- The **backend** sets `constant.Int.X` directly, since llir's constant is a big.Int
  underneath; the int64 constructor is what emitted 0. The match-pattern path had its own
  `strconv.ParseInt(…, 64)` and failed with a message about strconv rather than about the
  program.
- `isLiteralZero` folds through the same helper, so a wide *divisor* is no longer reported
  as a division by zero — which it would have been, `Value` being 0.

**One design decision needed correcting mid-flight.** A wide literal first reported a
*concrete* `i128`, on the reasoning that a large-u64 literal reports a concrete u64. But
u64 is the only type that can hold a large-u64 magnitude, whereas a 65-to-127-bit
magnitude fits **both** i128 and u128 — so committing to i128 made
`let b: u128 = 73786976294838206464` fail, which a test caught. It stays *untyped* where
both could hold it and names `u128` only above i128's range. That in turn is why
`checkIntegerLiteralRange` had to learn big magnitudes: an untyped literal has nothing for
assignability to object to, so without a 128-bit-aware range check a
`let x: u8 = <10^38>` would have passed unchecked.

**Compile-time folding is arbitrary precision.** It was `int64`-bound, which sounded like
a missing feature and was a **silent check**: a fold that declines reports nothing, so

```
let d: u8 = 100000000000000000000 + 1
```

passed the range check — there was no int64 for the walk to fold through — and reached
the backend, where the operand had already been narrowed to `u8` and the result was
invalid IR (`llvm.sadd.with.overflow.i64` called with an `i8`). The *bare* literal case
was caught and the arithmetic one was not, which reads as "the check works" right up
until it does not.

`ast.FoldBigExpr` does the same walk in `big.Int`, and `FoldIntExpr` **narrows at the
end**. That ordering is the whole design: a consumer that cannot handle more than an
int64 — the array-repeat count, the existing overflow checks — still gets ok=false rather
than a wrapped value, while the range check gets the true magnitude to report. The case
that only arbitrary precision reaches is the one where every leaf is representable and
the answer is not: `10000000000 * 10000000000` against an `i128`.

A 2^512 ceiling refuses a pathological chain rather than doing arbitrary-precision work
mid-compile. Nothing writable approaches it — literals are at most 128 bits and folding
is `+ - *` over a handful of them — and refusing loses a diagnostic, never correctness.

The int64 walk's `checkedAddInt64`/`checkedSubInt64`/`checkedMulInt64` are gone with it,
including the multiply's `MinInt64 * -1` special case: at arbitrary precision the true
value is 2^63, which simply does not fit, so the edge disappears rather than being
handled. Its test moved to the property that survives — that `FoldIntExpr` answers
ok=false for exactly the results an int64 cannot hold.

**The anonymous struct lowers**, so it is a usable value rather than a shape the checker
knows about. Six arms, in five walks, all of them the same omission:

- `lowerType` and `resolveForLayout` — structural, built from the fields on the spot,
  exactly as the anonymous *tuple* beside them is;
- construction, which places fields **by name in the type's order**. That is the one
  thing this path does that the named one does not have to: an anonymous struct's
  identity is its fields, so `{ y: "s", x: 1 }` and `{ x: 1, y: "s" }` are the same
  value, where a named struct's declaration fixes one order for every literal of it;
- field access, reading the same order back;
- `OwnsManaged`, `emitRetainValue` and `emitDropValue` — the retain/drop pair added in
  one change, as hazard 8's sixth instance insists.

**The arm that mattered was none of those.** The ownership pass's own expression walk had
`*ast.StructInstanceExpr` and not the anonymous one, so a struct literal's field values
were walked as *unowned* — and a temporary transferred into the struct
(`{ m: "a" ++ "b" }`) was released at the end of its own statement while the struct kept
the pointer. A use-after-free, and **neither ASan nor LeakSanitizer reported it**: the
freed bytes were simply not reused in between.

That is worth recording precisely, because it is the inverse of the lesson already in
CLAUDE.md ("a memory-safety test can pass because the code under it does nothing"). Here
the test exercised real code and the sanitizers were genuinely quiet — a leak check is
evidence, not proof, and the arm was found by *reading* the walk against its sibling
rather than by running anything. The first draft of the managed-field test was worse than
useless in the same way: it bound the string first (`let s = …; { m: s }`), so the
binding's own scope-exit release balanced the books whether the struct's glue existed or
not, and it passed with the arms deleted.

**An anonymous struct is assignable to itself.** `let a: { x: i64 } = { x: 1 }` reported
**"cannot assign struct to struct"** — a type refused against itself, with the message
naming the same thing twice.

The cause is hazard 8 in the form that keeps recurring: a list of aggregate forms with
one missing. `isAssignable` has an anonymous *tuple* arm that recurses element-wise, so
an untyped element widens to the annotation; it had no anonymous *struct* arm, so a
struct literal fell through to `TypesEqual`, which compares field types **exactly**. A
literal's field types come from its own leaves — `{ x: 1 }` is `{ x: untyped_int }` — so
it could never equal `{ x: i64 }`. A *named* struct was never affected, because
`inferStructInstanceExpr` narrows each field against the declaration; the anonymous one
has no declaration, and the annotation is its only source of a width.

The arm matches by **name** rather than position, which is the one thing distinguishing
it from the tuple rule, and `firstAllocationMismatch` got the same treatment for the same
reason: an allocation flavor buried in a field is as much a mismatch as one in an element.

`AnonymousStructType.String()` renders its fields now. Every one of them printed as the
bare word `struct`, so a *genuine* mismatch read exactly like the self-rejection this
fixed — `cannot assign { x: string } to { x: i64 }` is the same diagnostic doing its job.

**It was masking a larger gap**, which is the part worth carrying forward: with
assignability fixed, the same program reaches the backend and fails with
`expression lowering not implemented for *ast.AnonymousStructInstanceExpr`. The backend
never lowered an anonymous struct at all — `equality.go` and `layout.go` have arms,
`lowerType` and construction do not — because a value that could not be assigned never
got far enough to need one. Rule 5 is holding, and the type is unusable end to end until
that lands; it is recorded open rather than folded into this, being a feature rather than
a bug.

**An array of anonymous tuples parses.** `[](i64, string)` and `[3](i64, i64)` were
syntax errors while `[]Pair` and `[3]i64` were fine, so the workaround was to name the
tuple — which is the one thing an anonymous tuple exists not to require.

The cause was an omission rather than a decision. The grammar's element-type rule,
`_non_allocated_type`, is `type` minus the two modifier forms (added back by its callers
where they are meaningful) and minus `void_type`; the **anonymous tuple, the raw pointer
and the anonymous struct had simply never been listed**. That rule feeds the array
element, the pointee and the weak target, so one missing entry made three types
unwritable in every one of those positions. Adding all three made the parser 3 states
*smaller* (7789 → 7786).

Two things it surfaced, neither caused by it:

- **`[]void` parses**, despite the rule's comment claiming the `void_type` exclusion
  prevents it — `void` lexes as a lowercase `generic_type`, i.e. a type *variable* with
  that name, so the exclusion is real but the sentence it justifies is not.
- **An anonymous struct is not assignable to itself.** `let a: { x: i64 } = { x: 1 }`
  reports "cannot assign struct to struct" — the plainest self-rejection there is, and
  hazard 8's family again. It is long-standing and has nothing to do with arrays; making
  `[]{ x: i64 }` parseable just walked into it one layer down. Recorded as open, and it
  means that element type parses today and still cannot be constructed. The raw-pointer
  element is in the same position for a different reason: there is no `nil` to build one
  with.

**A concrete type with a `Show` impl prints directly**, not only through a bounded
generic. `println(pt)` and `"${pt}"` work for any type with a `show`.

The argument was the inconsistency rather than anything new: the *same* impl already
rendered a `Pt` through `describe(pt)` under a `where t: Show` bound, and `println(pt)`
was refused. One value, one impl, two answers — and the one that worked was the indirect
one. Mechanically it is the same desugar keyed on `resolveTraitMethod` instead of the
bound.

**The coherence question answered itself.** Extending it means `print` can call user code,
which is what made this worth deciding rather than assuming — the comparison operators and
arithmetic answered that question in opposite directions. But the alternative here was
never "print calls no user code": the bounded-generic path already did. It was "print
calls user code only when laundered through a generic", which is a rule with nothing to
recommend it. The rewrite produces an ordinary call, so the purity ladders charge it and a
`pure` function printing through an impure `show` is refused.

**It needed a guard, and finding that is the part worth recording.** With the concrete
case dispatching,

```
impl Show for Pt { show = (self) => "${self}" }
```

compiles into infinite recursion — `${self}` is now a call to the `show` being defined. It
built cleanly and died with SIGSEGV in under three seconds. That is the exact trap the
prelude sets by example: its scalar impls are *literally* `show = (self) => "${self}"`, so
copying that line for a struct is the first thing an author does.

`showApplies` now refuses to rewrite an operand into the method it is inside, and the
diagnostic names the fix rather than falling back to the printable-type message — which
would have been actively misleading, telling an author looking at a showable type that it
is not showable. The guard is **direct** self-recursion only: a `show` that renders a
different type whose own `show` comes back is ordinary mutual recursion, no more the
compiler's business than any other cycle. The direct case is the one that is *implicit* —
the author wrote `${self}`, not `self.show()`.

The primitive rule matters more here than anywhere else, and is asserted end to end: the
prelude's own `impl Show for i64` is `"${self}"`, so routing an i64 through it would be
infinite recursion inside the standard library.

**`checked_*` — overflow as a value.** `checked_add`, `checked_sub`, `checked_mul` and
`checked_div`, each `(self: T, other: T) -> Maybe<T>` on any concrete integer width,
answering `None` where the operation would have overflowed.

That completes the set trapping-by-default was for. `+` traps, which is the right answer
when the author has not thought about it; `wrapping_*` says "modular arithmetic is what I
meant"; `saturating_*` says "clamp"; and `checked_*` says "I will handle it", handing back
a `Maybe` the caller has to open. Rust's split is the same, for the same reason.

**The blocker in the backlog was not real.** The entry said this shares the unresolved
"return type from context" problem with the narrowing conversions — it does not: the
*receiver* fixes the width, so `i32(x).checked_add(y)` returns `Maybe<i32>` with nothing
to infer. The narrowing conversions have that problem because they take no argument to
read a width from.

**`checked_div` is in the set although division cannot overflow** in the intrinsic sense.
Its two failures — a zero divisor, and `INT_MIN / -1`, whose true quotient is INT_MAX+1 —
are exactly the two cases `/` traps on, so the name means the same thing there as it does
for the other three: the operation the operator would have refused, as a value. It is
arguably the most useful of the four, since a zero divisor is the overflow case that
usually comes from data rather than from a bug.

Two implementation notes worth keeping:

- **The lowering is branchless.** The with-overflow intrinsic already hands back
  `{ result, overflowed }`, so the union is two `buildDataValue`s and a `select`. Division
  has no such intrinsic, so the divisor is replaced by 1 on the failing paths and the
  meaningless quotient discarded by the same select — substituting rather than branching
  is what keeps LLVM from ever executing an undefined division. The `None` arm's payload
  is left as that meaningless value rather than zeroed: a nullary variant's payload blob
  is *undef* by DATA_LAYOUT.md, so selecting a zero would cost an instruction to establish
  something no correct program can read.
- **The Maybe is the canonical one**, resolved through `canonicalTypeName` — the same
  accessor `read_line` uses — so a program whose Maybe is named something else gets its own
  type back. With no Maybe declared at all the ambient fallback names the bare kind, the
  call type-checks, and the backend reports it loudly ("checked_add() must return a Maybe,
  got Maybe<i32>"). That is the arrangement `read_line` already has and is rule 5 working;
  a test pins it so the front end's silence is a recorded decision rather than an oversight.

`builtinMethodSignature` became a method on the typechecker to reach that accessor, which
is the only structural change: everything else is a registry entry and a lowering.

**`@builtin(Ord)` and `@builtin(Eq)` — the comparison traits are known by identity, not
by spelling.**

The compiler *owns* the operators that dispatch to these two: `<`/`<=`/`>`/`>=`/`<=>` all
derive from `Ord::compare`, and `==`/`!=` are overridden by `Eq::eq`. It therefore has to
find those traits, and until now it found them **by name** — a `trait Ord` with the right
shape. So a program declaring its own `trait Ord` had that trait silently taken for the
prelude's:

```
trait Ord { compare: (Self, Self) -> i64 }   // an ordinary trait, or so the author thinks
impl Ord for Ver { compare = (self, other) => 99 }
Ver { v: 1 } < Ver { v: 2 }
// llvm: Ord::compare must return the prelude's Ordering, got i64
```

That message is the *backend* catching a front-end mistake — rule 5 doing its job, at the
point where nothing can be said about the actual error. The marker moves the question
forward: the prelude's traits carry `@builtin(Ord)`/`@builtin(Eq)`, the collector stamps
`CanonicalKind`, and a user's `trait Ord` is left an ordinary trait whose `compare` is
callable by name and whose existence changes nothing about `<`.

**The half that is easy to get wrong is the filter.** Stamping the declaration is not
enough, because impl matching took a trait *name*: with the prelude's trait still called
`Ord`, `resolveTraitMethod(recv, "compare", "Ord")` matched the user's `impl Ord for Ver`
just as before — the marker had changed which declaration was canonical without changing
what was compared to it. `canonicalTraitMatches` resolves each candidate impl's trait as
that impl's own file sees it and keeps only those whose declaration **is** the canonical
one. Identity, resolved through the accessor, is rule 4 applied to a question the rule was
written for.

Everything that writes the name down follows the stamp too: `@derive(Ord)` synthesizes an
`impl <canonical name> for X`, so a renamed canonical trait gets a valid impl rather than
one naming a trait that does not exist, and `lyra-E039`'s "implement Ord instead" names
the actual trait.

**The fallback is what makes this carry no migration.** With no marker claiming a kind
anywhere, an unmarked correctly-shaped trait of that name is still stamped — so a snippet
with no prelude that declares its own `trait Ord` keeps working exactly as it did. What
the marker adds is that once a kind *is* claimed, the bare name stops being load-bearing.

The shape gate asks only that the trait declare the method the compiler will call
(`compare`/`eq`, two parameters). The **return** type is deliberately not gated: the
backend already reads `Ordering` off the matched impl's own signature by name rather than
assuming it, so re-deriving it here would be a second answer to a settled question — and
one that runs before the type it needs is resolved.

The diagnostic gained a clause for the shadowing case, because the rule being right does
not make the message readable: an author who wrote `impl Ord for Ver` and is told "Ver
must implement Ord" is being shown the answer as the problem. The collector's
`ShadowedCanonical` stamp is what lets it say which of the two mistakes was made — the
same trick `?` uses for a shadowed `Maybe`, for the same reason.

**An operator dispatches through a `where` bound.**

```
let total<t> where t: Add = (a: t, b: t) -> t => a + b
```

This is `dispatchViaGenericBound` for a node that is not a call, and deliberately the same
three steps in the same order: check the operands against the trait's declared signature
(Self bound to the type *parameter*, since that is what the operands are inside the body),
record the abstract resolution so the purity pass can account for it, and publish one
concrete resolution per implementing type so the specialization can pick the one it names.
Abstract dispatch has no single impl, so the last two are not alternatives — the first
keeps a `pure` function from silently admitting an impure impl, the second is how the
backend finds anything at all.

Three things had to be added, and two were holes rather than features:

- **The compound form had no receiver type to look up by.** `acc += b` under a bound
  failed to lower with "type not found for *ast.IdentifierExpr": a compound assignment
  never infers its own left-hand side — it resolves the binding — so nothing had put a
  type on that node, and the backend recovers the *substituted* receiver type from
  exactly there. The typechecker records it now.
- **The purity join ran over an empty group.** `collectTraitMethodGroups` built its
  (trait, method) → impls index from **identifier-named** methods only, a filter written
  when nothing dispatched to an operator method. So a `pure` function using a bound
  operator whose impl printed type-checked clean — the join answered EffectNone because
  there was nothing to join. Same shape as the identifier filter removed from
  `resolveTraitMethod` the day before, and the same lesson: **a filter written when a kind
  could not occur becomes a silent hole the day it can.**
- **The group key had to distinguish kind.** `BoundMethodRef.Method` is a string, and
  prefix `-` and binary `-` share a spelling, so keying on the operator text alone would
  merge two groups and charge one operator's effects to the other. `ast.MethodName.Key`
  spells an operator the way it is declared — `(_-_)`, `(-_)`, `(_--)` — so identity
  survives the flattening to a string.

The diagnostic for an unbound operand changed with it: it used to say an overloaded `+`
"needs a concrete operand type to find its impl", which stopped being true, and now points
at the bound.

**`Show` — a value whose type is a type parameter can be formatted.** `"${v}"` and
`println(v)` work inside a generic with a `where t: Show` bound:

```
let describe<t> where t: Show = (v: t) -> string => "value ${v}"
```

The gap was found the day the prelude's `expect` was written (08/04): the natural draft
reports what it got, `panic("expected ${value}")`, and none of it was expressible. `print`
and interpolation pick a formatter per *concrete* type, and a `t` has no representation, so
there was nothing to pick.

**The whole trait is ordinary Lyra**, which is the part worth recording. `"${self}"` on a
concrete primitive is exactly the formatter `print` already picks, so
`impl Show for i64 { show = (self) => "${self}" }` needs no compiler support at all — the
prelude gained a file and the builtin registry gained nothing. Same division of labour as
`parse_i64`, `trim` and the PRNG: anything expressible in the language belongs in the
prelude, where it is readable, testable and replaceable. (`string`'s impl returns the value
rather than interpolating it — `"${s}"` would copy the bytes into a fresh box for no
reason, and this is the one place that cost would be paid on every rendering of every
string.)

**The mechanism is a desugar, not a second formatting path.** Where the operand's type is a
type variable bound by a trait declaring `show`, it is rewritten to `operand.show()` before
anything downstream sees it. That is an ordinary bound-dispatched call, which has resolved
and lowered since 08/07 — so the backend learned nothing new, and `print`'s printable-type
rule is exactly as strict as it was, since the rewritten operand is a `string`. The shape
UFCS uses for a receiver, and what rule 10 recommends generally: one rewrite beats teaching
every later pass what a `t` in print position means.

**The trait is recognized by its method, not by its name.** The rewrite looks for an
in-scope bound whose trait declares `show`, so a user's own `trait Render { show: … }`
works identically and nothing keys on the spelling `Show`. That is the same decision
arithmetic operator overloading made a day earlier, and it is why this needed no
`@builtin(Show)` marker — the open item asking for one is about `Ord`, which the compiler
*does* have to know by name because it owns the comparison operators.

The diagnostic changed with it. An unbound type parameter used to draw "expected a string,
an integer, a float, bool, or rune", which is true of a `t` and no help at all — the author
cannot make a type parameter into one of those. It now names the thing they can do:

```
cannot interpolate a value of type t: a type parameter has no representation to format
— add `where t: Show` so the value can be rendered
```

A *concrete* unprintable type still gets the old message, which is the right one for it:
the fix there is a conversion or an impl, not a bound.

**The array-repeat literal, `[v; n]`.** It parsed and collected from the beginning and
nothing downstream read it — the typechecker reported `unknown expression type "[0; 5]"`
— which is loud rather than silent, so it was an unimplemented feature rather than a
phantom. Found 08/07 by the AST sweep, which noticed `ArrayRepeatExpr.Count` had no
consumer at all.

Three decisions, and the first is the one everything else follows from:

**The value is evaluated once.** `[next(); 3]` is one call, not three, and nothing in the
syntax would say otherwise — Rust arrives at the same place from the other side, requiring
`Copy` or a const expression precisely so the question cannot arise. A test asserts it
through a side effect, since that is the only way the difference is observable.

Evaluating once is what makes the retains necessary: each of the n slots is an **owner**,
so a managed element takes n-1 retains beyond the reference the value already carries, or
the array's drop glue releases n times what was retained once. The array literal
`[s, s, s]` needs none of this — it lowers three separate uses and the ownership pass
retains each. Verified under LeakSanitizer.

**The count is a compile-time constant by construction, not by a check.** The grammar
admits only a number literal or a `const_identifier` there, so `[0; n]` for a runtime `n`
is a syntax error — the right place for the restriction, since the size is part of the
*type* and a type cannot depend on a value the compiler has not got.

**The typechecker rewrites a `const` count to the literal it folded to**, which is what
keeps that resolution from becoming everyone's problem. The backend needs the same number
(a `[]T` context records `[]i64` and drops the size), and without the rewrite codegen
would need its own const lookup — a second copy of "what does this name mean", living
where the symbol table is least at hand. Rule 10's advice applied upward, and the shape
`desugarUFCSCall` already uses for a receiver. The folding itself moved to
`ast.FoldIntExpr` so the two passes cannot disagree about a length; it was **moved
verbatim rather than rewritten**, because a fresh implementation drops things like the
documented `MinInt64 * -1` case, and this one nearly did.

Three arms had to be added elsewhere, all the same hazard in different keys:

- **`lowerDynArrayRepeat`**, because a `[]T` annotation records a dynamic type and the
  first version had only the fixed-size path — it emitted a `[3 x i64]` bitcast to a
  pointer, which Apple clang rejects outright. The box allocation is now shared with the
  literal (`dynArrayBox`); what the two differ about is the loop, which is theirs.
- **The allocation walk**, which names the allocating *forms* rather than switching on
  types, so the repeat was not among them: `noalloc … => { let d: []i64 = [0; 3]; … }`
  type-checked clean while the identical `[1, 2, 3]` was refused. Live for exactly one
  build. Hazard 8, in a list of syntax rather than in a switch — which is why adding the
  form everywhere else did not surface it.
- **The ownership pass**, whose `ArrayLiteralExpr` arm transfers each element; without the
  matching arm the repeat's value fell to `default` and a managed element's transfer went
  unrecorded.

### 08/07/26
**Two parse gaps that operator overloading turned from curiosities into blockers**, both
closed by *removing* a path rather than adding one — the parser is 19 states smaller
(7730 → 7711).

```
Cents(150) + Cents(275)   // was: syntax error: unexpected "Cents(150) +"
(a + b).x                 // was: syntax error: unexpected ".x"
```

Neither was about the operator; both were about which rule a form is reachable from.

**A constructor call could not be a math operand.** `Cents(1)` parses as a
`tuple_literal`, which lives in `_literal` — and `_math_operand` is
`_postfix_expr | _math_expr | address_of_expr`, which never reaches it. So `f(1) + f(2)`
was fine (a `call_expr` is a postfix form) and `Cents(1) + Cents(2)` died at the
operator. `tuple_literal` cannot simply move into `_primary_expr` — the note there says
why: `Some(42)` would then have a second reading as a call of `user_defined_type_name`,
which is a reduce-reduce at every operand position. Adding it to `_math_operand`
specifically gives it the one position it was missing.

**A parenthesized binary expression could not be a postfix head.** `(x)` is a
`parenthesized_expr`, which *is* a `_primary_expr`, so `(x).f` always worked. `(x + y)`
is a `group` — a separate rule, `( _math_expr )` at its own precedence — reachable only
from `_math_expr`, so it could never head a member access, an index, or a call.
`(1 + 2).wrapping_add(3)` failed for the same reason, so this predated overloading and
the 08/06 literal-as-postfix-head work simply had not covered it.

The fix is the interesting half. Adding `group` to `_primary_expr` *alongside* its
`_math_expr` arm is an unresolved conflict — tree-sitter says so outright, since `group`
would then be derivable two ways at every operand. So the arm was **removed** instead:
`group` is now reachable only as a primary, and every math operand still finds it through
`_postfix_expr`. One path to one node, which is the same discipline the Go side applies
to a switch with two callers.

Both are pinned by corpus cases and by a behavioural test that runs the two forms an
author would actually write against a `data` type with a `(_+_)`.

**Arithmetic operator overloading**, and the deletion of the holding position that stood
in for it. `a + b` on a user type resolves to a trait method named `(_+_)`, exactly as
`a.show()` resolves to one named `show`:

```
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
```

Ten binary operators (`+ - * / % << >> & | ~`), the prefix `-` and `~`, and every
compound assignment built on them.

**The dispatch key is the method name, not a trait the compiler knows**, which is the
one real design decision here and the opposite of what `Eq`/`Ord` do. Those two *are*
the comparison operators: `<` and `<=>` must agree, so one trait has to own them and a
second mechanism would be a coherence question with no answer. Arithmetic carries no
such invariant — `+` on a matrix and `+` on a duration are unrelated operations — so
insisting they come from one blessed trait buys nothing and costs the author a name they
did not choose. Two traits providing one operator for one type is an ambiguity, reported
where the operator is written, which is the same answer the identifier path gives.

**A primitive is never routed through an impl.** `1 + 1` is a machine add whatever a
program declares, asserted with a deliberately wrong `impl Add for i64` that would
return 999 if it were ever reached — the rule `dispatchEq` and `dispatchOrdCompare`
already follow. Arithmetic a library can redefine is arithmetic no reader can trust.

The resolution is **one function**, not two: `resolveTraitMethodNamed` is the existing
identifier lookup keyed on a full `MethodName` rather than a string, with the old
`resolveTraitMethod` as a wrapper. Generic impls, Self substitution, trait
type-parameter binding and `where` bounds therefore behave identically under an operator
and under a call, rather than being a second copy that agrees until it doesn't.

Three things had to be fixed to ship it, and two were pre-existing:

- **The purity pass could not see an operator's impl at all.** A `pure` function using
  an overloaded `+` whose impl printed type-checked clean — and so did one comparing
  through an `Ord::compare` that printed, which had been true since `Ord` landed that
  morning. The three effect ladders (hazard 8's third instance, and its fifth) all ask
  "what does this expression call?", and an operator answers where a `FunctionCallExpr`
  would. `operatorImplEffect` is one function called from all of them, keyed on the
  *resolution* rather than on which operator it is — which is what makes it fix the
  comparison half as well as the arithmetic one.
- **`a += b` with no impl type-checked and then failed to lower.** `checkAssignToBinding`
  asks whether the right side is assignable to the left, which two structs of one type
  satisfy, so the compound form accepted what `a = a + b` had always reported (rule 5
  inverted). Now `+=` dispatches — it is `x = x + y`, and a working `a + b` beside a
  crashing `a += b` is the first thing anyone would hit — and the no-impl case reports.
- **A trait-impl method could not return its own receiver.** `same = (self) => self`
  against `(Self) -> Self` failed with `expected Vec2, got Vec2`, because the return type
  was resolved and the parameter types were not. Naming the same type twice is the
  signature of exactly that asymmetry.

**What is left, and why each is left.** `&&`/`||` cannot be overloaded at all: a call
evaluates its arguments, and not evaluating the right operand is the entire content of
those two operators. `!` is boolean negation and the language has no user truthiness.
`**` is a method spelling with no operator, and its mirror image `%%` is an operator
with no spelling — both grammar gaps, recorded. The suffix `_++`/`_--` name operators
the language does not have. Each of these still warns, but now with *its own* reason:
"nothing dispatches to it" left an author unable to tell "wait for it" from "this can
never work".

An operand that is a **type parameter** was refused here and resolves through a `where`
bound as of the next day; with no bound declaring the operator the message names both
readings, because the author meant one of them and the compiler cannot tell which. `==`
has no such problem — equality is structural, so a type variable is comparable and an impl
only overrides that.

Two grammar gaps surfaced while testing and are recorded rather than fixed:
`C(1) + C(2)` does not parse (a constructor call cannot be a math operator's left
operand, though `f(1) + f(2)` can), and neither does `(a + b).x` (a parenthesized binary
expression cannot be a postfix head). Both have workarounds — bind first — and both are
the first thing a user of this feature would write.

**Two `slice()` results in one expression clobbered each other**, and the fix is to stop
asking a proxy question:

```
let s = "abcdef"
println("${s.slice(0,2)} ${s.slice(2,4)}")     // "cd cd", want "ab cd"
println(s.slice(0,2) ++ "|" ++ s.slice(2,4))   // "cdcd" — even the "|" gone
```

A use-after-free, and the IR named it outright: the first slice's box was released **in
its own build block**, before the second slice ran, so the second `lyra_rc_alloc` handed
back the same memory and both fat pointers described the second result.

`flushStmtTemps` picks the block a statement's owned temporaries are released in, and it
chose by asking `p.block == start` — release at the statement's end block if the temp was
produced in its start block, otherwise release it where it was produced. The comment said
what that was standing in for: a temp produced *elsewhere* is one produced "inside a branch
of the expression (an `&&`/`||` right operand, a match-arm body)", which may not have run,
so releasing it at the end block would touch an undefined value.

**That proxy was exactly right until a lowering branched unconditionally.** `slice` does —
a bounds trap and a decode loop — and so do `read_line` and `<=>`; each ends in a
continuation block every path reaches. Their temps are not conditional at all, but they are
not in the start block either, so they were released at the point they were produced, which
is *before the rest of the statement*. One slice per expression was fine, which is why it
shipped; `let`-binding first was fine too, which is why it read as an interpolation bug.

The fix is to ask the real question — does the production block **dominate** the end block
— which is the same answer for both of the old cases and the right one for the new. The
tree is built lazily, so an ordinary statement still costs nothing.

Computing dominance here is sound for precisely the reason `dominators.go` says it is *not*
sound for `resolveExitReleases`: `end` is the block lowering continues into, so it never
becomes the target of a later edge, and nothing added afterwards can create a path to it
that bypasses a block dominating it today. A jump's target, by contrast, is fixed before
the CFG that reaches it exists.

The general lesson is the one hazard 8 keeps teaching in a different key. This was not a
missing case in a switch; it was a **proxy for a property**, correct on every input that
existed when it was written, silently wrong the moment a new kind of block appeared. Both
failure modes are invisible in review, and both are found by asking what the code is
actually trying to know.

**A `newtype` base must be structural, and a newtype is transparent to its base's
methods.** Two halves of one question — what a newtype is *for*, and what you get once
you have one.

`lyra-E041` refuses a base that already has nominal identity: a `struct`, a `data` type, a
named tuple, and an **anonymous** tuple. The rule is structural-versus-nominal, not
scalar-versus-compound: `newtype` exists to give identity to a type that has none
(`newtype Meters = f64` will not mix with other f64s), and an array earns its place the
same way — `struct Matrix { cells: [16]f64 }` is a wrapper with a field, not an alias, so
the newtype is the only route to a nominal array.

**The implementation already agreed with the rule before the rule was written**, which is
what made the decision easy: a struct base could not be constructed by *any* spelling
(`let w: W = Pt { … }` and a `-> W` return both rejected), and a data base type-checked and
then crashed the backend with `data constructor "Red" did not record a data type`. Neither
had ever been usable, so this refuses two forms rather than removing anything that worked.

The anonymous tuple is the interesting one, because it *did* work. It is refused for a
sharper reason than the others: `tuple Rgb(u8, u8, u8)` already names a product, so the two
differ only in whether the name is a constructor — `let c: Rgb = (1, 2, 3)` is rejected for
the named tuple, `Rgb(1, 2, 3)` for the newtype. Two spellings of "give this product a
name" is the redundancy the diagnostic exists to remove, so the message shows the `tuple`
line to write instead of describing the problem.

**The transparency half is what makes the surviving bases worth having.** A
`newtype Name = string` that could not be measured, sliced or trimmed is a string you can
do nothing with, so a method call falls back to the base — the builtins and the prelude's
`self:`-taking functions alike. It is tried *after* every other rung rather than by
stripping the newtype up front, so a method written for the newtype still wins, matching
the user-code-beats-builtin ordering everywhere else. The receiver *occurrence* is
re-recorded at the base type, exactly as the untyped-literal promotion beside it does: the
argument check that follows compares against a `self: string` parameter, and the backend's
`recordedType` already strips newtypes, so this only agrees with what lowering was going to
do anyway.

Making that ordering real needed **hazard 8's seventh instance**: `nominalHead` — the
unifier's "is this a named type, and which?" — had arms for `ParameterizedType`,
`NamedStructType`, `DataType` and `UnresolvedType` but not `*ConstrainedType`, though
`types.HeadName` one layer up says a newtype is nominal and explains why. So
`receiverAccepts` compared a `Name` receiver against a declared `self: Name` (an
`UnresolvedType` at that point), fell through to `TypesEqual`, and never matched: a method
written *for* a newtype was unreachable, while the base's were about to become reachable.
The fallback would have inverted the precedence it promises, and only in the one case where
the author had been most explicit.

Two bugs turned up while testing it, both recorded in todo.md and neither caused by this
work: two `slice()` results in one expression clobber each other (a temporary released
before the next allocation — `"${s.slice(0,3)} ${s.slice(2,4)}"` prints `hé hé`), and an
array of anonymous tuples (`[](i64, string)`) does not parse in any position.

**A generic `newtype` works** — `newtype Boxed<t> = t`, with `Boxed<i64>` nominal to the
typechecker and transparent to codegen, exactly as `newtype Plain = i64` already was.

**The grammar failure was quiet rather than loud**, which is why it lasted. `newtype` was
the one type declaration without a `generic_parameters` slot — struct, data, tuple and
trait all had one — so the `<t>` landed in an ERROR node *and the declaration still
collected*, silently becoming `newtype Point = …`. The golden file recorded that drop under
a test named for the feature: a golden regenerated from a truncated AST bakes the
truncation in and then reads as a specification. It surfaced when the does-it-parse guard
reached the golden helper, which is the second gap that guard has paid for.

**Three parts, and the third is the one worth remembering.** The grammar gained the slot
(+10 states), the collector attaches the parameters — and `resolveType` now **expands** a
parameterized newtype into its substituted `ConstrainedType`.

That expansion is deliberately asymmetric with every other parameterized type. A
`Box<i64>` stays a `ParameterizedType`, because the instantiation machinery is what gives
it a layout and a specialization. A newtype has neither: it *is* its base plus a nominal
name, so `Boxed<i64>` has to become a `ConstrainedType` over `i64` for the rest of the
compiler to treat it the way it already treats the non-generic case. Left unexpanded,
`StripNewtype` finds no `ConstrainedType` and every assignment is rejected —
`cannot assign integer literal to Boxed<i64>`, which is what the first attempt did after
the grammar and collector were both already correct.

`resolveGenericAggregate` was missing the same arm and got it too. It is not on the path
that failed here, but it is the same question asked in the other place, and leaving one of
two answers wrong is how the drift this codebase keeps recording begins.

The nominal half is pinned by a test: a `Boxed<string>` is still not an `i64`. Transparent
to codegen, distinct to the typechecker — the whole point of `newtype`.

### 08/07/26
**`let _ = expr` discards.** A bare `_` in binding position evaluates the value and binds
nothing — the canonical way to opt out of the must-use rule.

**The compiler had been recommending a spelling its own parser rejected.** The must-use
warning ends *"bind it (`let _ = ...`) to discard it intentionally"*, and following that
advice produced "cannot destructure integer literal with a data pattern": `_` fell into
`data_pattern`, which recovered with an **empty** name — the CST showed a `data_type_name`
spanning zero characters. `wildcard_pattern` is one of `destructuring_only_pattern`'s
alternatives now.

**Why it survived:** the named form `let _ignored = …` always worked, taking `declaration`'s
identifier branch, so the workaround reads as a style choice rather than a necessity. And
the test asserting the opt-out *worked* passed — the source did not parse, so the truncated
AST contained no call to warn about. It surfaced only when the does-it-parse guard landed in
the test helpers the same day, which is the guard paying for itself within hours.

**Cost: zero parser states** (7,720 → 7,720), no new conflicts. `_` in that position was
already unambiguous — nothing else can follow `let` there — so the alternative folds into
the existing automaton.

`_` remains not an *expression*: `let _ = 5; _` does not parse, which is what keeps a
discard from being read back. The side effect is preserved, which is the point of writing
one — `let _ = noisy()` still calls `noisy`.

### 08/07/26
**Every test source parses now, and the class is closed by a guard.** `parseCollectAndCheck`
and the collector's golden helper both check `tree.RootNode().HasError()`, so a source with
a syntax error fails the test instead of reaching the typechecker as a *truncated AST* that
every later assertion is vacuous against.

Of the fourteen counted on 08/06: **ten were mechanical** — two statements sharing a line
with no separator, invalid since statements gained a terminator on 07/31 — **one
deliberately tests a parse error** and now opts out explicitly through
`parseCollectAndCheckAllowingSyntaxErrors`, and **three were real bugs the vacuity was
hiding**:

- **the all-defaults struct literal** (`Person {}`), found 08/06;
- **`let _ = expr`**, which does not parse: a bare `_` in binding position is read as a
  *destructuring* pattern and recovers as a `data_pattern` with an empty name. So the
  canonical way to discard a must-use result is unavailable — and the test asserting the
  opt-out *worked* passed because the truncated AST contained no call to warn about. The
  named form `let _ignored = …` does work;
- **a generic `newtype`** (`newtype Point<t> = Tuple`), whose `<t>` lands in an ERROR node.
  Its golden file recorded a `ConstrainedType` with the parameters silently dropped — a bug
  documented as intended output, under a test named for the feature. A golden regenerated
  from a truncated AST bakes the truncation in, which is the sharpest argument for guarding
  the golden helper and not only the typechecker one.

Each of the three is kept as an inverted test that fails when the gap closes, so the fix
cannot land without someone restoring the real assertion.

**The pattern across all three is the same and worth naming:** a syntax error and a missing
diagnostic cancel out. Neither alone would have passed; together they read as success. That
is why the guard is worth more than the ten separator fixes — those were typos, and this is
a class.

### 08/07/26
**A bare `_` stands for a whole multi-field payload.** `Rect _`, where
`Rect(i64, i64)`, parsed and type-checked and then failed to lower — *"payload pattern for
Rect not implemented yet"* — while the arity-matched `Rect(_, _)` worked. Two spellings of
the same set of values, and only one of them compiled.

A wildcard binds nothing and tests nothing, so expanding it to one wildcard per field is
**exact rather than an approximation**: the two forms describe the same values, and the
expansion makes them lower the same way. Fresh nodes per field rather than the same one
repeated — nothing keys on a pattern's identity today, but sharing one node across field
positions is the kind of aliasing that makes a later map-by-pointer quietly wrong.

**Found by writing the data-type derive by hand**, which wants exactly this shape for its
"self is the earlier variant" arms. The synthesis generates arity-matched wildcards and
stepped around it, but a hand-written match still hit it — which is why it was recorded as
a gap rather than left as an implementation detail of the derive.

What is *not* fixed now says what it is: a single **binding** for a multi-field payload
(`Rect pair`) would bind the payload tuple as one value, which is a real feature and a
different one. It keeps an error, but one that names the spelling that works rather than
reporting "not implemented" about a form that was — the old message covered both cases and
so described neither.

### 08/07/26
**`string` is ordered** — `"a" < "b"` works, `<=>` answers on strings, and a `data` variant
or struct field carrying one can `@derive(Ord)`. Until now `<` on two strings was "operands
must be numeric" and a string payload made a type underivable.

**A `compare_bytes` builtin under a prelude `impl Ord for string`**, which is the same
division `random_seed` and `read_line` follow: the builtin is only what cannot be written
in Lyra, and the shaping sits in the prelude. It returns an `i64` on memcmp's convention
rather than an `Ordering`, so the backend needs no knowledge of a prelude type; the impl
maps the sign to `Less`/`Equal`/`Greater`.

**The property that makes it a builtin worth having: byte order is code-point order in
UTF-8.** A lower code point always encodes to a byte sequence that compares lower, by
design of the encoding — so a single memcmp answers exactly what a rune-by-rune walk would.
Written in the prelude with `s[i]` the same function is **O(n²)**, because indexing a
string is O(i); that was the choice recorded when this was deferred, and it is why the
builtin won.

A shorter string that is a prefix of a longer one sorts first, which falls out of comparing
the common prefix and then the lengths — computed as a subtraction rather than a branch, so
the call site stays branchless like the rest of that file. memcmp runs over `min(la, lb)`,
so it reads past neither buffer whatever the lengths.

**It is deliberately not collation.** `"Z"` sorts before `"a"` because their code points do,
and accented characters sort by code point rather than alphabetically — `"héllo" < "hello"`
is *false*, since é is U+00E9. Locale-aware ordering needs tables and a locale and belongs
in a Unicode library, not in the ordering `<` reaches for. The tests pin that case
specifically, so a later "fix" toward alphabetical order has to be a deliberate one.

### 08/07/26
**`@derive(Ord)` on a `data` type** — by constructor declaration order first, then by
payload. The language cannot read a variant's tag, so the comparison is written as a match
over the pair of scrutinees.

**It is 3n arms, not N-squared, and that is why it got built.** The estimate recorded when
this was deferred assumed every ordered pair of constructors needed an arm. It does not:
for each constructor in declaration order,

	(Ci(a…), Ci(b…)) => compare the payloads lexicographically
	(Ci(_…), _)      => Less        // self is the earlier variant
	(_, Ci(_…))      => Greater     // other is the earlier variant

and once those three are past, no later arm can see `Ci` on either side — so the next
constructor's three are reached only by values that are neither. The last constructor needs
only the first arm, since everything else has already been decided. The generated match is
exhaustive with no warning, which is the check that the reasoning is right.

**Wildcards are arity-matched** (`Rect(_, _)`, never `Rect _`), stepping around a
pre-existing gap found while writing the target by hand: a *single* wildcard standing for a
multi-field payload parses and type-checks and then fails to lower — `payload pattern for
"Rect" not implemented yet` — while the arity-matched form works. Recorded in todo.md,
since a hand-written match still hits it.

**Writing the target by hand first is what found that**, and it is the second time this week
the habit paid: the struct derive was built the same way. A synthesis is only as good as the
source it imitates, and a shape that does not compile when written by hand will not compile
when generated — but the failure then arrives as a confusing backend error against code
nobody wrote.

Also surfaced: **`string` has no `Ord`**, so a variant with a string payload cannot derive
one, and `<` on two strings is refused. An `impl Ord for string` is now expressible in the
prelude (`len` + indexing + `<=>` on runes) but indexing is O(i), so the obvious version is
O(n²); the honest choice is between that and a `compare` builtin over memcmp. Left open
rather than guessed at.

### 08/07/26
**Structural `==` on an aggregate lowers, and the float warning survives substitution** —
the two `==` bugs found while designing Eq/Ord.

**`a == b` on a struct, tuple, `data` value or inline array** type-checked from the start
(`areEqualityCompatible`) and the backend refused it from the start: *"comparison of
non-integer operands not implemented"*. Hazard 5 inverted, and the third instance of that
shape dug out this week after the type-name member call and the `where`-bound call. It now
compares field-wise, recursing through nesting and covering every element of a `[N]T`.

**A per-type glue function rather than an inlined comparison**, for the two reasons drop.go
gives for its own. A `data` value's equality has to branch on the tag, and a branching
*call site* returns a merge block the pending-temporaries machinery does not handle — the
fault that made `read_line`'s lowering branchless. Emitting a function keeps the call site
one instruction and puts the branching inside, where it is nobody else's problem; it also
means a recursive type's equality reaches itself through a cache entry instead of expanding
forever.

The field comparisons are ANDed rather than short-circuited. Each is a handful of
instructions with no side effect and no trap — the string case is a length test and a
memcmp — so branching past the rest would cost more in blocks than it saves in work, and
would reintroduce the merge block the design exists to avoid.

**`lyra-W008` did not survive substitution.** A direct `f64 == f64` warned; the identical
comparison reached through a type variable did not, though both do IEEE equality and both
answer `false` for `0.1 + 0.2 == 0.3`. Genericity silently stripped the safety net, which is
worse than never having had one: the check looks present and is not. It now fires at the
**instantiation**, alongside the `where`-bound check, and reports at the *call* rather than
at the comparison — the comparison is correct where it is written (`t` is not a float
there, and the body is sensible at every other type), and the call is the line the author
can change. At most one per call site, since a body comparing several float-bound variables
says the same thing about the same call.

That needed a warnings-inclusive test helper: `checkWithPrelude` returns errors only, and
most callers assert "no errors" and would otherwise have to filter out every advisory
diagnostic the compiler ever gains. Added beside it rather than widening it.

### 08/07/26
**Swept the AST for fields nothing reads**, after four collected-and-unread surfaces turned
up in two days — `wallClock`, the `where` bounds, `@derive`, and operator method names. The
method: enumerate every exported field of every struct in `pkg/ast`, then grep for a reader
outside `pkg/ast` itself, outside the reflection-based printer (which reads everything and
so proves nothing), and outside tests.

**The reassuring half is the result.** Of **119 exported fields, 3 looked suspicious and 2
were genuine.** `SymbolTable.Traits` was a false positive — read by the `Lookup*` accessors
that live inside `pkg/ast/symbols`, which the sweep excludes along with the rest of the
package. So the AST surface is largely clean, and the recurring phantoms were somewhere
else: effect tables and glue switches, not node fields. That is worth knowing before
spending more effort here.

The two real ones:

**`TraitDeclStmt.Bounds` — supertraits were never enforced.** `trait B: A` parsed, collected
and was read by *nobody*, so `impl B for S` compiled with no `A` in sight. A supertrait is
the promise that lets a `where t: B` bound reach `A`'s methods, and it was a promise the
compiler did not keep. Now `lyra-E040`, checked where the impl and its trait are both in
hand. Declaration order does not matter — impls are gathered up front so a call can dispatch
against one declared later, and there is a test that the same holds here.

**`SymbolTable.PureFuncs` — a map written and never read.** Its doc comment named the purity
checker as the consumer; the purity checker had never consulted it. Deleted, along with the
test that asserted it was populated: a test guarding dead state reports that the state is
still there, which is the opposite of useful.

Also recorded rather than fixed: **`[0; 5]`, the array-repeat literal, is unimplemented.**
It parses and collects, and the typechecker then says `unknown expression type` — loud, so an
unimplemented feature rather than a phantom, but the grammar and collector support a form
nothing downstream does. `ArrayRepeatExpr.Count` has exactly one mention outside `pkg/ast`
and no consumer.

### 08/07/26
**Operator-named trait methods are refused where the compiler owns the operator, and
warned about everywhere else.** `trait Eq { (_==_): (Self, Self) -> bool }` parsed,
collected, type-checked — and was dispatched by nobody: every consumer filters on
`MethodNameKindIdentifier` and skips the rest, so a `(_==_)` impl on a struct is never
called and `==` keeps its built-in meaning. The grammar reserves twenty binary spellings
plus prefix and suffix forms, none wired to anything. The fourth collected-and-unread
surface found in two days.

**The question that prompted it was whether the syntax still makes sense**, and the answer
turned on what had just been built rather than on taste. It makes *less* sense than before:
`Eq` and `Ord` now own those operators, so `(_==_)` would be a rival way to override `==`
with no rule for which wins, and `(_<_)` beside `(_<=>_)` reintroduces precisely the
disagreement `Ord`'s single `compare` was designed to prevent.

**But deleting the whole syntax would have thrown away the only design on the table for
user-defined arithmetic** — `(_+_)` on a vector type — and `(_-_)` is load-bearing for a
hazard already recorded, since `Empty - 1` parsing as `Empty(-1)` only bites a `data` type
that overloads `-`. So the list is split: seven refused, the rest warned.

Two existing tests used the refused shape — one of them *literally* the
`trait Eq { (_==_), (_!=_) }` in the question. The collector golden test is about
operator-name collection and keeps testing it on `(_+_)`/`(_-_)`; the impl-completeness
test was using the operator spellings incidentally and now uses identifier names.

It also turned up the twin of a fault fixed hours earlier: the **typechecker** test helper
failed on any collector diagnostic, treating a warning as fatal, exactly as the golden
helper had. Both are errors-only now. A warning leaves a well-formed program, so failing on
one makes any advisory diagnostic break every test whose source happens to trip it.

### 08/07/26
**A generic instantiated at a type declared in a named module lowers.** It failed with
`llvm: unknown named type "Tag"` for as little as

```lyra
module main
struct Tag { n: i64 }
let idf<t> = (a: t) -> t => a
```

— no traits, no nesting, the identity function. Remove the `module main` header and it
compiled, which is what made it look like a module bug rather than what it was.

**The specialization path was the one function-lowering path that never called
`enterModuleOf`.** `lowerFunction`, `defineFunction` and the entry point all set the
location that lookups are made *from*; `declareSpecialization` did not, so `l.currentLoc`
held whatever the previously-lowered item left behind. The type argument was then looked up
under its **bare** name, while a private module-scoped declaration is keyed
`<module>::<name>` — rule 4, in the one place that still resolved without it.

**A `pub` type has a bare key and worked**, and that is the whole reason this survived: it
reads as a bug about generics, the failing case looks exotic, and the existing tests declare
their types `pub` or use no module header at all. The one-line difference between a passing
and a failing program was a visibility keyword nobody would think to vary.

Found while writing an `Eq` test, which used `module main` because every test in that file
does. Verified pre-existing by stashing before assuming — a habit worth keeping after being
wrong about "pre-existing" twice in the same week — then fixed rather than routed around,
since it bites every generic over a user type in any program with a module header.

### 08/07/26
**The `Eq` override** — `pub trait Eq { eq: (Self, Self) -> bool }`, for the minority of
types whose equality is not field-wise. `==`/`!=` stay structural; an impl *replaces* them
for its type rather than enabling them.

That is the opposite of Rust and Swift, and it follows from where this language started
rather than from taste: unbounded structural equality already worked on every type
**including a bare type variable**, so requiring a bound would have removed working
capability to gain ceremony. A primitive is never routed through an impl, so `1 == 1` stays
a machine comparison and an impl cannot change what equality means on the built-in types —
the same rule `Ord` follows, with a test that asserts it using a deliberately-false impl.

**The hole worth recording is the generic one.** An operand that is a type *variable* names
no impl at check time, so the first working version had `p == q` using a type's `Eq` impl
while `same(p, q)` — the same comparison through a generic — silently used structural
equality. One operator meaning two things depending on whether it was written inside a
generic, which is the action-at-a-distance the override model was chosen to avoid. Fixed the
way bound dispatch was: the typechecker publishes a candidate per implementing type and the
backend picks by the substituted operand type.

**A design correction, made while building.** The decided design had `trait Ord: Eq`, on the
reasoning that a supertrait stops `compare` answering `Equal` where equality says false.
That is wrong under the *override* model and was not built: equality is always available
structurally, so demanding an `Eq` **impl** of every ordered type would make `@derive(Ord)`
— which synthesizes none — fail on every type that used it. A type implementing both and
letting them disagree writes a bug the compiler cannot see; that is the residual cost of
equality not being a bound, and it is smaller than the cost of the supertrait.

**And a pre-existing bug found by the test that hit it.** A generic instantiated at a struct
declared in a *named module* fails to lower — `llvm: unknown named type "Tag"` — with no
traits involved at all (`let idf<t> = (a: t) -> t => a` reproduces it). Verified pre-existing
by stashing, filed in todo.md, and routed around in the test so a failure there means what it
says. It bites every generic over a user type in any program with a module header.

### 08/07/26
**`@derive(Ord)`** — the structural ordering, lexicographic in field-declaration order.
`@derive(...)` had parsed and been collected onto `TypeDeclStmt.Derives` since the
attribute existed, and was read by nobody: the same collected-and-unread shape the `where`
bounds had that morning, and the third such field found in two days.

**It synthesizes an ordinary `ast.TraitImplStmt` and appends it to the program**, rather
than teaching dispatch about derives. Everything downstream then treats a derived impl
exactly as a hand-written one, and the payoff is that three behaviours came for free rather
than needing code: deriving over a field with no ordering is an ordinary comparison error
naming that field's type; a derive beside a hand-written `impl Ord` is the duplicate-impl
error added earlier the same day; and the backend lowers it through the path `Ord` already
uses. That is the erasure this compiler reaches for repeatedly — juxtaposition, bare
match-arm jumps, UFCS — and it is why the whole feature is one file with no counterpart
anywhere else.

**Declaration order is the ordering**, which is a real commitment: reordering a struct's
fields changes how its values sort. That is exactly why it is opt-in through an attribute
rather than automatic — a type that silently acquired an order nobody chose, and a field
reordering that silently changed it, is the worse failure. Rust makes the same trade.

Two diagnostics, and the split between them is the interesting part:

- **`@derive(Ord)` on a `data` type is an error** (`lyra-E038`) naming the hand-written
  fix. The derived ordering there is by constructor order and then payload, and the
  language has no way to read a tag, so the synthesis is an N-squared match over both
  scrutinees.
- **A derive naming a trait that does not exist yet is a *warning*** (`lyra-W014`). It was
  an error first, which broke three collector golden tests that use `@derive(Eq, Hash,
  Show)` as arbitrary names — and those tests were right. The derive is not *wrong*; it is
  a no-op, and refusing a program over a feature that has not landed is worse than saying
  so. Reporting it at all is what keeps it from being the next phantom builtin.

That also turned up a real fault in the golden helper: it failed on any collector
diagnostic, treating a **warning** as fatal. A warning leaves the collected AST exactly as
it is, which is what a golden file records, so an advisory diagnostic would break every
golden that happened to trip it. Errors only now.

### 08/07/26
**`Ord` — a user type can be ordered.** `compare: (Self, Self) -> Ordering` in
`std/prelude/ordering.lyra`, with `<=>` returning it directly and `<` `<=` `>` `>=` derived
from its tag. Nothing could order a user type before: `<` on a struct was "operands must be
numeric" and `<=>` was numeric+rune only.

**One method, and the derivation is the point.** An impl supplies `compare` and nothing else,
so it cannot make `<` and `<=>` disagree — the failure mode C++'s separate
`operator<`/`operator<=>` and Java's `compareTo`-beside-`equals` both carry. The cost is that
a type cannot offer a cheaper `<` than its `compare`; worth it while correctness is the
scarce thing.

**A numeric or rune operand is never routed through it.** `1 < 2` stays a single `icmp`, and
a deliberately wrong `Ord` impl cannot change what the operators mean on the built-in types —
there is a test that asserts exactly that, because "an impl changed what `<` does to i64"
is the kind of thing that would be found much later.

**The tags are looked up by name, not assumed to be 0/1/2.** The prelude declares
`Less | Equal | Greater` in that order and the derived operators are each one compare against
the union's tag — so hardcoding the order would make *reordering that declaration* silently
invert every comparison in every program. A wrong answer, not a build failure, which is the
category this language spends most of its effort avoiding. They come from the impl's own
resolved return type.

Two deviations from the decided design, both recorded in todo.md rather than papered over:

- **Recognized by name and shape rather than by `@builtin(Ord)`.** An attribute does not
  parse on a trait declaration, so the marker is a grammar change plus a trait arm in
  `canonical.go`, which is entirely type-shaped. Name-and-shape is the fallback that file
  already applies to *types*, so this extends an existing rule rather than inventing one —
  but a user's own `trait Ord` in the entry module would be taken for the prelude's.
- **`Ord: Eq` is not enforced.** The supertrait parses and is collected; whether anything
  checks it is unverified, and it only matters once `Eq` exists.

The resolution is published on the operator expression (`MethodTable.SetOperatorResolution`)
rather than desugared into a call, because `<=>` is its own AST node and rewriting it into a
`FunctionCallExpr` would mean replacing a node the parent holds by pointer. Same arrangement
the bound-dispatch candidates use.

### 08/07/26
**A trait may be implemented once per type** (`lyra-E037`). `impl Show for i64` twice drew no
diagnostic before, and which body a call ran was decided by declaration order.

It looked harmless while a trait only *added* methods — whichever impl won, the call had a
body — which is presumably why it survived. It stops being harmless the moment a trait
**overrides** something: the decided `Eq` design has an impl replace structural equality, so
two impls for one type would make `==` mean two things. That is why this is the first step of
that work rather than a tidy-up.

It also closed a rule-5 inversion introduced hours earlier the same day.
`publishBoundCandidates` requires exactly one match before publishing a bound-dispatch
candidate — correctly refusing to guess — so a duplicated impl published nothing and surfaced
as a *backend* error at a call site, far from the two declarations that caused it. The
diagnostic now lands on the second impl and names the first.

**Identical targets only, deliberately.** `impl Show for Box<t>` beside `impl Show for
Box<i64>` overlaps without being identical, and deciding which is more specific needs the
specificity ordering this language does not have — the same reasoning that keeps overloading
receiver-keyed. Exact duplicates are the unambiguous part and the part the `Eq` override
needs; genuine overlap is left open rather than half-answered.

Reported once per pair rather than per call site: a call-site report names a line that is
correct, repeats for every call, and leaves the reader hunting for the pair.

### 08/07/26
**A `where`-bound call lowers, so `Show` works end to end.** `describe(7)` and
`describe(true)` through one generic body now print "an int" and "a bool" — two
instantiations, two impls, one source function.

The gap was a mismatch in what "resolved" means at two stages. The typechecker resolves a
bound call **abstractly**, to a trait and a method name, and that is genuinely all it can
do: the receiver is a type *variable*, and every implementing type type-checks identically
against the trait's signature. The backend needs a real callee, and by the time it runs it
has one — a specialization is being lowered, so `l.typeSubst` maps the variable to a
concrete type.

**The impl matching stays in the typechecker.** The backend could have matched impls
itself, but `implTargetMatches`, the Self substitution and the trait's own parameter
bindings are dispatch's job, and a second copy in codegen is exactly the drift
`Resolution` was introduced to prevent — the same hazard-8 shape that cost a day earlier in
the week. So the typechecker publishes one concrete resolution per implementing type and
the backend selects by the substituted receiver type. Keyed by *type* rather than by
specialization because that is what the backend can compute locally: it holds the
substitution, not the enclosing specialization's key.

Every implementing type is published, not just the ones some specialization reaches —
which are unknown until the instantiation set is closed in the driver, a pass later. The
set is one trait's impls and an unselected candidate costs a table entry, not an emitted
function.

A receiver that is still a type variable when lowering reaches it gets a hard error naming
what is missing rather than a guess at which impl was meant.

### 08/07/26
**`where` bounds mean something.** They were collected and read by nobody but the
unused-parameter warning, so writing one bought nothing: a generic could be instantiated at a
type with no impl, which type-checked clean and died in the backend as
`llvm: unsupported method call`. Two halves were missing, and the first is what made the
second worth having.

**A binding's bounds were never in scope for its own body.** `tc.genericBounds` — what
`dispatchViaGenericBound` consults — was populated only from an *impl's* `where` clause, so

```lyra
let describe<t> where t: Show = (v: t) -> string => v.show()
```

reported *"type parameter t has no method `show`; add a `where t: Trait` bound whose trait
declares it"*: a diagnostic naming the exact fix the author had already applied. That is the
worst shape a message can have — it reads as the compiler not believing what is written.

The bounds are lifted onto the `LambdaExpr` by the collector, the same way the leading
modifiers already are, because they are written on the **binding** (`let f<t> where …`) while
every consumer downstream holds only the lambda.

**Enforcement is at the instantiation** (`lyra-E036`), because that is the only point where
the question has an answer: the declaration cannot know what `t` will be, and the backend is
too late. It reuses `typeImplementsTrait`, which already existed as "the bound-satisfaction
test for a generic impl's `where` clause" and simply had no second caller — the check was
written years-of-commits before the thing that needed it. Nearly added a second copy of it
before noticing, which is the failure mode this codebase's hazard 8 is entirely about.

**A type argument that is itself a type variable is checked against the enclosing scope's
bounds, not against any impl.** Inside another generic, `describe(x)` binds `t` to `u`, and
whether that satisfies `Show` is a question about the caller's own `where` clause — there is
no impl for `u` to find. Getting this wrong in either direction is bad: reject it and a
correctly forwarded bound becomes an error; accept it blindly and the bound stops meaning
anything one level in. So a forwarded bound compiles and an unforwarded one is refused with
the clause to add.

**What this does not yet do is lower.** A bound-dispatched call resolves *abstractly* — to a
trait and a method name — and the concrete impl is known only once a specialization fixes the
parameter; the backend has no path from one to the other. It is now a hard error saying
exactly that, rather than the generic "unsupported method call", because the program is
well-typed and the bound is satisfied and the author would otherwise go hunting for a mistake
that is not there. That is rule 5 working as intended rather than the inversion of it fixed
twice this week: the front end looked carefully and accepted; the backend has not built the
form. **It is the last piece before `Show` is usable**, and the natural home is the driver,
where the instantiation set is already closed per specialization.

### 08/07/26
**A `match` over a tuple scrutinee leaked one reference per call, and fixing it exposed a
double free in the ref-counting runtime.** This is what turned CI red; the two defects are
independent and both are pre-existing.

**1. A tuple scrutinee's elements were retained and never dropped.** Both aggregate glue
walks — `emitRetainValue` and `emitDropValue` — switch on the resolved type and neither had
a `ParameterizedType` arm, so a tuple holding a `Maybe<string>` walked straight past that
element. They were symmetric *in being broken*, which is why nothing complained; the element
is still retained at the **construction** site, where the type arrives already substituted
through `recordedType`, so only the drop side was actually missing and the imbalance was a
pure leak.

What makes it ordinary rather than exotic: **a multi-clause function desugars to
`match (p0, p1) { … }`**, whose scrutinee is a stack tuple over the parameters. So every call
to the prelude's `unwrap_or` leaked one reference to its receiver's box, and
`line.unwrap_or("")` in a read loop leaks one box per iteration. A single-scrutinee
`match m { … }` never did — there is no tuple to build — which is exactly what hid it: the
construct that leaks looks like pure sugar. `OwnsManaged` had been given this same arm long
ago, its comment recording that missing it was "a real double free"; the two glue walks are
the other half of that model and never got one. Hazard 8, in the variant where the copies
agree and are wrong together.

The ownership pass also had to *mark* the scrutinee tuple for release: a stack aggregate in
a borrowing position is a temporary whose elements' +1s die with it, and the package doc had
recorded that as a known limitation ("a stack tuple's [elements] leak, since a stack
aggregate has no death to hang a drop on"). It does have a death — the end of the statement
that produced it, where the backend already flushes temporaries.

**2. `drop_fn` could free the box it was called on.** `lyra_rc_release` decremented strong,
ran the payload's `drop_fn`, and *then* tested the weak count to decide whether to free. But
that glue runs arbitrary user code, and through a cycle it can drop the last **weak**
reference to the same box — a `Node` whose child holds `Maybe<weak Node>` back at it.
`lyra_rc_weak_release` sees weak 0 with strong already 0, frees the memory, and the outer
release frees it again.

The strong owners now hold **one implicit weak reference**, taken at allocation and dropped
after `drop_fn` returns, so the count cannot reach zero while the glue is running — Rust's
`Arc`, for the same reason. `lyra_rc_weak_release` needs no strong check any more: weak
reaching zero already means every strong owner is gone *and* its glue has finished.

**The order matters, and getting it wrong is instructive.** Landing the drop arm without the
retain arm is a release with no matching retain — an immediate double free, which
`TestExec_WeakOptionalField` caught at once. That test had been green *by leaking*: before
the glue walked a `Maybe<shared T>` field at all, the cycle's fields were never released, so
the test exercised nothing. A memory-safety test can pass because the code under it does
nothing.

**On the investigation itself**, since the wrong turns cost more than the fix. The leak was
first diagnosed as "an owned `Maybe<string>` is never released", from grepping the emitted
`main` for `lyra_rc_release` — the wrong symbol, because an aggregate releases through
generated glue. It was also called pre-existing on the strength of a `git stash` that could
not show that (the first suspect commit was already committed), then un-called on CI history
alone. What settled it was a worktree bisect against the last green commit (fails 6/6 there)
and symbolized leak stacks. Byte counts were nondeterministic run to run, so no single
measurement of them meant anything.

Still unexplained: why CI flipped. The program's IR is byte-identical across the suspect
commits and the leak reproduces at the last green one, so the trigger is outside the
compiler — a runner image or glibc change — and it is not confirmed.

### 08/07/26
**A module may be a directory of files**, and `std/prelude.lyra` became `std/prelude/` — seven
files split by topic (`maybe`, `result`, `array`, `ordering`, `parse`, `strings`, `rand`), one
module. `std.prelude` now resolves to `std/prelude.lyra` *or* every `*.lyra` directly inside
`std/prelude/`; both forms are the same module — one path, one namespace, one scope, one set of
declaration keys.

**The reason it had to be within a module, rather than into several modules.** The prelude was
425 lines and growing, and the cheap fix — make `std.strings`, `std.rand`, `std.time` separate
modules and implicitly import a *list* of them — silently changes what its names mean, in two
ways this file already records. Receiver-keyed overloading (08/03) is per-module, so
`unwrap_or` for `Maybe` beside `unwrap_or` for `Result` would become a cross-module duplicate —
undoing precisely the split the library was rescued from on 08/04. And prelude shadowing is
keyed on the prelude *module*, so N implicit modules means N sets of bare keys competing, which
is the land-grab still open in todo.md's Modules section, multiplied. Splitting within a module
leaves every one of those rules untouched, and generalizes: `std.io` gets to grow the same way.

Five decisions, each a real choice:

- **Not recursive.** A subdirectory is the next module path down (`std/prelude/text/` is
  `std.prelude.text`), so recursing would swallow a module into its parent and make two
  spellings of a name mean the same thing.
- **Every file in a module directory must declare its module.** Membership by location alone
  is less to type, but then a file's own text no longer says which namespace its declarations
  join — in a namespace where a name may be a receiver overload of one three files away. It
  would also cost the property `pkg/modules` opens with: a module's name and its location
  agree by construction, with no manifest. A *single-file* module still needs no header (its
  path is its location), and a header contradicting its location is an error either way,
  because one of the two is wrong and picking which is not the compiler's to do.
- **Both forms in one root is an error, not a preference.** Which one won would decide what
  half the program's names mean, and a reader looking at `std/prelude/strings.lyra` has no way
  to see that `std/prelude.lyra` beside it is quietly the real module. Across different roots
  the earlier one wins, as everywhere else in resolution.
- **Name order**, since a directory listing is not ordered on every filesystem and unit order
  feeds diagnostic order.
- **The entry file brings its module.** Entering at `std/prelude/strings.lyra` pulls in its
  siblings. Without it, "the prelude compiles standalone" — the property that makes it an
  ordinary module rather than a compiler built-in — would have held only while it fitted in
  one file, and `lyrac check` on one of its files would report the rest of it undefined.

**Almost all of it was in `pkg/modules`**, because the collector was already module-keyed
rather than file-keyed: `ModuleScopeFor` hands several files of one module the same scope, and
`SymbolTable.Imports` is keyed by module path. `visit` became module-granular (a unit *group*
rather than a unit, following the imports of every file), and `resolveImport` split into
`findModule` + `loadDir`.

**The one thing outside it is a timing variant of hazard 8, and it is worth the space.** Exports
are recorded per *file* (`recordModuleBindings`), so a name that becomes overloaded only in a
**later** file of a module had already been exported as a bare declaration, and the set built
when the second file was walked collided with it: `symbol "area" already defined`. Two
independent things had kept it invisible. Within one file the merge happens during the walk, so
by the time either member exports, both export the same set object and the identity guard in
`exportToGlobal` catches it. And the prelude branch of that same function discards
duplicate-definition errors outright — so the shipped prelude's `unwrap_or` worked across two
files while a user module doing the identical thing did not. A rule enforced in one place and
suppressed in another is not one rule; the fix lets a set supersede a global binding that is
one of its own members, which is true of both. Found by writing the test for the feature's own
motivating property rather than by hitting it.

### 08/06/26
**A literal is a postfix head**, so `"abc".len()`, `[1, 2, 3].len()`, `1.wrapping_add(2)` and
`1.5.floor()` parse. `_primary_expr` admitted an identifier, a `parenthesized_expr` and a
struct literal and **no literal at all**, so every postfix form was unreachable from one —
while `("abc").len()` and a bound `s.len()` both worked, which is the tell that nothing was
wrong with the methods. Found the day before, writing the tests for string `len`/`slice`.

It matters more than it reads: UFCS made method syntax the normal way to call, so every
combinator the standard library gains was unreachable from the literal a reader would
naturally try it on.

**The change is a partition, not an addition**, and that is the whole difficulty. `expression`
reaches `_literal` directly *and* `_postfix_expr` (hence `_primary_expr`), so a literal kind
listed in both is derivable two ways and every operand position becomes an unresolved
reduce-reduce. Each kind had to move rather than be copied, and the same de-duplication had
to reach every operand rule listing a literal beside `_postfix_expr`. Three kinds stay behind:
`tuple_literal` (which is how `Some(42)` already parses), `anonymous_struct_literal` (a bare
`{ … }` head contests the block), and `array_repeat_init` (nothing wants a method on one).

**Dropping `regex_literal` from `_literal` without adding it anywhere was the one wrong turn**,
and it is instructive: the regex was then reachable only as a *constructor operand*, so
`let phone = r"…"` parsed as a `data_constructor_expr` with a `MISSING` constructor name. The
`prec.right(PREC.LITERAL)` wrapper on `_literal` is what makes a plain literal outrank the
juxtaposition reading. The corpus caught it immediately; nothing else would have.

Three new conflict entries, all literal analogues of races a bare name has run since 07/16 —
`('a', 'b')`, `(1, 2)` and `(-1, 2)` are each a lambda parameter list of patterns or an
anonymous tuple of expressions, decided by the `=>` that may or may not follow. The `_literal`
precedence wrapper used to settle that statically; routing literals through `_primary_expr` is
what turns it back into a conflict.

**Cost: −4 states**, and `parser.c` slightly smaller. Removing the duplicate derivations bought
more than the new heads cost — the opposite of what this region usually does, where
juxtaposition cost +19%.

Two things fell out that were not the feature:

- **A numeric literal receiver needed pinning to its default width.** `builtinMethodSignature`
  promotes internally to decide *whether* the method exists, but that promotion is local to the
  lookup — the receiver node keeps what the literal inferred as, and the backend reads the
  node. So `1.5.floor()` type-checked and failed to lower with "floor() on non-float receiver
  float literal". No receiver could be a bare literal before, which is why it had never come
  up. The first guard written for it compared `promoteToDefault(objType) != objType` and
  **panicked at run time** — `types.Type` is an interface over structs that are not all
  comparable, and a `NamedStructType` carries slices. It asks whether the receiver *is* an
  untyped literal instead.
- **Three more of the non-parsing test sources surfaced.** The interior-mutation tests wrote
  `{ b.x = 99  a.x }` — two statements on one line with no separator, invalid since statements
  gained a terminator on 07/31. They passed because the truncated AST hid it; the new recovery
  path reads `99  a.x` as a member access on an integer literal and produces an *extra* type
  error. Fixed at the source. That is three of the fourteen counted on 08/06; the rest are
  still open (todo.md).

`0 - 200` still parses as a `binary_expr` with a `sub_operator` — the regression this grammar
region is on record for, and the one whose failure mode is a *program* rather than an error.
Pinned by corpus and by an execution test.

### 08/06/26
**String `len`, `slice` and the trim family.** `s.len()` was array-only and there was no
way to take a substring, so a program could inspect input (`for c in s`, `s[i]`) but never
carve it up — the gap the todo predicted "the first program that wants to validate input
before parsing it" would hit.

**The design question answered itself, which is the part worth recording.** Bytes-vs-runes
looked like an open fork — Go and Rust count bytes, Swift counts graphemes — but `s[i]`
already yielded the i-th *code point* and `for c in s` already walked code points, both
shipped. A byte-based `len` would therefore have made

```lyra
for i in 0..<s.len() { print(s[i]) }
```

wrong on the first non-ASCII input: silently, for some inputs only, in the most obvious loop
anyone writes. So `len` counts runes and is **O(n)**, which is the honest price of agreeing
with the index. The fat pointer's `len` field stays a byte count — that is the
representation (STRING_LAYOUT.md), not the language.

`slice(start, end)` is the half-open rune range, matching `..<`. **It allocates, and a
borrowed slice is deliberately not on offer**: a substring of UTF-8 is a contiguous byte
range, so `{data + off, n}` would be free and is exactly how Go does it — but every Lyra
string is a ref-counted box whose header sits at the box's *start*, so a pointer into the
middle cannot find the header to retain or release through. Copying into a fresh box keeps
the one uniform rule that a string value is a box.

Bounds are checked in rune terms and traps; `start > end` traps too, rather than quietly
yielding `""` that cannot be told from a correct empty slice. Both got their own trap
messages, because a string index and a string slice had been reporting **"array index out of
bounds"** — a message that sends the reader looking for an array that is not there.

`trim`/`trim_start`/`trim_end` are ordinary Lyra in `std/prelude.lyra`, by the rule that put
`parse_i64` there and `read_line` in the compiler: a UTF-8 walk needs the decoder and is
primitive, and everything built on it is not. Whitespace is the **five ASCII characters**
and deliberately not Unicode's `White_Space`, which needs a table that belongs in a real
Unicode library rather than smuggled into a prelude. `trim` makes one pass per end and a
*single* slice rather than `trim_start().trim_end()`, which would build a whole intermediate
string only to copy most of it again.

**Writing the prelude half exposed a live `noalloc` hole, and it is hazard 8 for the fifth
time.** A builtin method is charged **no** effect — that is the 08/05 fix that makes
`x.wrapping_mul(y)` usable from `pure noalloc` code — and the purity pass asks "what does
this call call?" in *three* places, all three of which say so. `slice` is the first builtin
method that genuinely allocates, so it was invisible to all three at once and

```lyra
let bad = pure noalloc (s: string) -> string => s.trim()
```

type-checked clean. A bound that silently stops binding is worse than no bound. Fixed by
hazard 9's rule rather than by a fourth name test: the typechecker records *whether the
resolved builtin allocates* (`MethodTable.SetBuiltinMethod(call, allocates)`), since only it
still has the receiver's type in hand — `slice` is a string method and nothing else, but a
consumer testing the bare name would be taking that on faith. All three ladders read the
flag. `len` and `s[i]` stay allocation-free, so the fix is not the blanket "builtin methods
allocate" that would have been wrong in the other direction.

Verified on Linux under ASan **with LeakSanitizer on**: the fresh box a slice builds is
released by the ownership model with no leak and no fault.

### 08/06/26
**`for flag { … }` parses.** The condition field was `alias($.boolean_expr,
$.for_condition_expr)` and a bare identifier is not a `boolean_expr`, so looping on a bool
binding had to be spelled `for done == true { … }` — a workaround nobody writes by choice,
and one that reads as the language not having a while loop. It is `$._bool_operand` now, so
a name, a call (`for ready(n)`) and a member access (`for cfg.enabled`) all work.

**`$.expression`, matching `if`'s condition, does not generate** — worth recording because
it is the obvious unification and it fails for a specific reason. A `block` *is* an
expression, so `for { … }` becomes genuinely ambiguous between "condition and no body" and
"no condition and a body"; `if` escapes it only because its `then_block` is mandatory.
`_bool_operand` excludes `block`, so the question never arises.

**It shrank the parser by one state** (7,725 → 7,724, −728 bytes), which is not a typo and
not luck: `_bool_operand`'s states already existed for the `&&`/`||` operand positions, so
the wider rule reused them and the narrower one it replaced stopped needing its own. Measured
before believing it, since this grammar region has a history of surprising costs in the other
direction.

Two consequences worth keeping. The **`for_condition_expr` alias is gone**, so a comparison
condition yields a plain `boolean_expr`; it never meant anything (the collector handled it in
the same `case`) and keeping it would have made the node kind depend on which *form* the
condition took, which is a trap for a query. And **bool-ness is entirely the typechecker's**
now — `for n { }` over an integer was a syntax error pointing at the brace and is now
`for loop condition must be boolean, got i64`, which is the diagnostic that was wanted all
along.

### 08/06/26
**A member call on a type name no longer type-checks into a backend crash.**
`Rng.seeded(42)` passed `lyrac check` with no diagnostic and then failed with
`llvm: unsupported method call "seeded"`. That is hazard 5 inverted — the backend
refusing a form the front end had never looked at, rather than one it accepted on
purpose — and it is the hole that let `Random.global()` *look* implemented for months.

**The silence was one rung below the member call, which is why it was so wide.** The lexer
guarantees a PascalCase name in expression position is not a variable, so the collector
reads every one as a nullary data constructor (`None`, `Red`). When no constructor owns the
name, `inferExprType` answered nil and said nothing — so the receiver was untyped and
`inferMemberCall` returned nil in turn, also silently. Three surface forms shared the one
hole: a call (`Rng.seeded(42)`), a plain access (`Rng.field`), and a bare mention
(`let x = Rng`). `Nonexistent.make(1)` — a name that exists nowhere at all — checked clean
too, which is the clearest statement of how little was being asked.

Fixed at the source rather than at each consumer (hazard 8), with the message phrased about
the *name* so it reads correctly in all three positions. `lyra-E035`, in three cases because
the fix differs: a **type** ("Lyra has no associated functions" — `Rng.seeded(…)` is not an
unimplemented call but a form the language does not have, and the free function `rng_seeded`
is the whole answer, which is why the prelude's constructors are spelled bare); a **trait**
(`Trait::method(…)` *is* a spelling the language has, so the message names it); and a name
that is neither (undefined).

**Two green tests were resting on that silence, and both were worth more than the fix.**
`TestIfHeader_NameFollowedByPlainBlock` asserted no errors for
`if Point { 1 } else { 0 }` — but a bare type name is not a bool whichever way the header
parses, so the assertion could never have held; it passed because the ill-typed condition
inferred as nil. It now asserts *which* error, which is the sharper test of the grammar fix
it was written for: E035 against `Point` alone means the brace was read as a block.

`TestTypeCheck_StructLiteralWithAllDefaults_Ok` was worse, and turned up a real gap: **an
all-defaults struct literal cannot be written at all.** `Person {}` is a syntax error — a
literal body needs at least one field — and `parseCollectAndCheck` never asks whether the
CST holds an error, so the source reached the typechecker as a truncated AST whose value was
a bare `Person`, which inferred as the same silent nil. A syntax error and a missing
diagnostic cancelled into a green test for a feature the language does not have. The test is
kept, inverted, as the record of the gap. Probing the helper with a `HasError` guard found
**14 sources in that package that do not parse**, some deliberate; auditing them is open
(todo.md).

### 08/06/26
**`wall_clock_nanos()` — the last entry naming nothing now names something.** `wallClock`
sat in `checker/effects.go`'s `builtinEffects` tagged `EffectTime`, with no typechecker
signature and no lowering anywhere: precisely the `Random.global()` shape, still in place,
a line below the comment explaining why that shape is a bug.

**Implemented rather than deleted**, which was the other option on the table. Deleting the
entry would have left `EffectTime` a bit nothing in the language could set — the same
phantom seen from the other side, and one that reads as a working feature in the effect
ladder's documentation.

`wall_clock_nanos() -> i64` is `clock_gettime(CLOCK_REALTIME, …)` and nothing else
(`backend/llvm/clock.go`), the same division of labour `random_seed` has beside the
prelude's `Rng`: asking the OS what time it is cannot be expressed in Lyra, while seconds,
elapsed durations and formatting are arithmetic and belong in the prelude. Three decisions
the old name did not record:

- **snake_case**, like every other name in the language (`random_seed`, `read_line`,
  `parse_i64`). `wallClock` matched nothing.
- **The unit is in the name.** A clock returning a bare number invites the reader to guess
  seconds, millis or nanos, and a wrong guess is silent; Go's `UnixNano` and Zig's
  `nanoTimestamp` both spell it out.
- **`i64`, not `u64`.** The useful operation on two instants is subtraction and a difference
  is signed. Nanoseconds in an i64 run to 2262.

The `timespec` is **zeroed before the call**, for the reason `random_seed` writes its
`time(NULL)` fallback before calling `getentropy` rather than after a failure test: POSIX
leaves the struct unspecified on failure, so a program ignoring the return value would be
reading uninitialized stack to decide a timestamp.

The effect story needed no new machinery, and that is the point of having charged the bit
in the right place. Ambient `wall_clock_nanos()` carries `EffectTime`, so `pure` and `det`
both refuse it with a message naming the *clock* rather than the generic input effect; a
timestamp threaded in as a parameter is ordinary `i64` data carrying nothing, so `det`
arithmetic over time works untouched. Exactly the split that lets `det` code use a seeded
`Rng`.

### 08/06/26
**Tuple match exhaustiveness is a pattern matrix now, so coverage spread across arms
counts.** The old test asked only "is any one arm irrefutable?", which cannot see this:

```lyra
match (self, predicate) {
  (Some v, pred) => …,   // column 0 covers Some
  (None, _)      => …,   // …and None; column 1 binds in both
}
```

Every value is matched, no single arm is irrefutable, so it warned. **That is the shape
every multi-clause function desugars to**, which is how the prelude ended up emitting
fourteen false `lyra-E009`s at once. A warning that fires on correct code is worse than no
warning: it is a standing instruction to ignore the class, and the class also contains
genuinely non-exhaustive matches that trap at runtime.

**The obvious cheap fix — check each column independently — is unsound**, and that is why
this is a matrix rather than a loop. `(Some v, None)` beside `(None, Some x)` has both
constructors in both columns and still leaves `(Some, Some)` unmatched; per-column would
call it exhaustive and go quiet on a real trap. So the check is Maranget's: specialize the
matrix by each constructor of column 0 and recurse, concluding coverage only from rows that
agree on every column to the left. Enumerable columns are `data` types and `bool`; every
other column is covered only by a row that binds it whole, which keeps `(Some x, 0) => …`
correctly incomplete.

Everything the matrix cannot interpret **drops its row**, which can only shrink coverage and
so can only produce a *warning* — the direction that over-warns, never the one that goes
silent. The old per-arm answer is subsumed: a fully irrefutable arm becomes a row of
wildcards, which survives every specialization.

Two things fell out worth keeping. Parentheses around a single-field payload collect as a
one-element `TuplePattern` (`Some (Some x)` and `Some None` reach the checker differently
spelled), so `unparenthesize` strips it — the type side already did the matching unwrap in
`FieldTypes`. And rows are copied rather than appended into, since a specialized row's head
can be a pattern's own `Elements` slice and appending would write the remaining columns into
the AST.

Structs still use the per-arm test: a struct pattern may list a *subset* of its fields, so
its columns are not the fixed positional list a tuple's are, and nothing in the language
reaches the case — the desugaring that made this matter produces a tuple.

### 08/06/26
**A `match` on a parameter now tells effect analysis that its arm bindings *are* that
parameter** — so renaming a parameter in a pattern costs nothing. It used to cost
everything, silently.

The reachable form is the multi-clause function. `desugarClauses` rewrites clauses into
`match (p0, p1) { … }` before any checker runs, so a clause's parameter list becomes arm
patterns and `LambdaClauses` is empty by the time purity sees the lambda. `callableParams`
mapped only the *declared* parameter names, so a body calling a callback through a
**renamed** binding resolved to nothing and hit the unresolved-callee default —
`AllEffects`, which is deliberately the worst case for an imported function nobody can
verify. The function was then reported as both impure and allocating, at the declaration,
naming a parameter that looked perfectly ordinary:

```lyra
pub let filter<t> = pure noalloc (self: Maybe<t>, predicate: (t) -> bool) -> Maybe<t> {
  (Some v, pred) => if pred(v) { Some v } else { None },   // `pred`, not `predicate`
  (None, _) => None,
}
```

That is the prelude's own `filter`, both copies of it, and it took ~25 tests across
`pkg/modules` and `pkg/backend/llvm` with it — every one reporting the same two
diagnostics twice, none of them naming the rename. The tell is that the *sibling*
combinators were fine: `unwrap_or_else`'s `(None, f) => f()` calls its callback too, and
passed only because that clause happened to reuse the declared name. **Correct analysis
was contingent on a coincidence of spelling.**

**It was never a clause-form bug**, which is why the fix is not at the clause list. The
hand-written `=> match (self, predicate) { (Some v, pred) => pred(v), … }` failed
identically; the desugaring only made the hole reachable from something that reads as a
plain function head. `addMatchAliases` therefore works on the body's own `MatchExpr`:
where a scrutinee position is an identifier naming a parameter, an arm's whole-value
binding at that position is another name for it. Both of clauseScrutinee's shapes are
covered, since arm patterns mirror them — the bare parameter for a one-parameter function,
a tuple of them otherwise.

Three limits, each deliberate:

- **Only a whole-value binding aliases the argument.** The `v` of `Some v` names the
  payload, and charging a call through it against the argument's position would read the
  *wrong parameter's* declared bound — a misattributed diagnostic, which is worse than the
  missing one.
- **Only the body's own match**, not every match in the body. Deeper down, a scrutinee's
  names sit under bindings this pass would have to track to know whether `f` is still the
  parameter; being wrong there misattributes rather than misses.
- **Ambiguity drops the name.** `(a, b) => …` beside `(b, a) => …` gives a name two
  positions and no single argument to charge against, so it stays unresolvable — which is
  what all of these were before.

Fixing `callableParams` fixed all four consumers at once, because `lambdaEffects`'
inference, both reporting sites, and `declaredBound` already funnel through it — so a
renamed parameter carrying `f: pure () -> t` has its bound enforced under the new name too,
rather than being laundered into an unconstrained callback.

One diagnostic improved on the way: the callback-argument message named the callee's
*internal* binding (`impure g argument` for a parameter declared `f`), which for a
desugared clause is an arm binding the caller cannot see anywhere. It now names the
parameter as the signature spells it.

### 08/06/26
**`<=>` lowers, and it yields `Ordering` rather than a bool.** It had parsed and
type-checked since the grammar had a `spaceship_operator` — as a **bool**, with its
operands never checked, because the operator appears in no case of
`checkBooleanBinaryOpExpr` — and then failed the build with "boolean operator <=> not
implemented". Same family as `Random.global()` and `Rng.seeded()`: a form the front
end waved through into a backend that had never heard of it.

**The result type is a sum type, not Ruby's -1/0/1.** An integer invites
`if (a <=> b) == -1`, which is strictly worse than `a < b` and would leave the
operator with no reason to exist. `data Ordering = Less | Equal | Greater` in the
prelude makes the one thing `<=>` is *for* — handling all three outcomes together —
the natural spelling, and exhaustiveness then insists every case is covered, which is
exactly what the `if`/`else if`/`else` chain it replaces cannot offer:

```lyra
match guess <=> secret {
  Less    => println("Too low!"),
  Greater => println("Too high!"),
  Equal   => { println("You got it!"); break },
}
```

**Floats are refused**, and that is the one place `<=>` is narrower than `<`. NaN is
neither less than, equal to, nor greater than anything; `<` can answer false and call
it a day, but a three-way answer must name one of three variants and every choice is a
lie. C++ splits `strong_ordering` from `partial_ordering` (with an `unordered` case)
for this reason. A fourth variant would burden every *integer* match with a case that
cannot occur, so the decision is deferred with a diagnostic that explains itself rather
than guessed. Integers and runes are supported, runes ordering by code point as they do
under `<`.

**The lowering is branchless**, and deliberately so. Every `Ordering` variant is
nullary, so the three values differ in one field — the tag — which two `select`s
compute and an `insertvalue` places. No new blocks, so the call site returns the block
it was given. That matters beyond tidiness: a branching call site returns a *merge*
block, which is neither case `flushStmtTemps` handles, and that is what made
`read_line` free its string before the `match` read it. `Ordering` owns nothing so the
bug could not bite here, but the shape is worth keeping, and a test asserts it on the
emitted IR.

Two details that would each have been a silent wrong answer. The predicates follow the
operand's **signedness** — `u8(200) <=> u8(1)` is Greater, but read as signed 200 is
-56 and the answer flips to Less. And the tags come from `findConstructor` rather than
being hard-coded 0/1/2, so reordering the prelude's variants cannot silently
miscompile every three-way match.

**A body ending in a loop now infers `void`.** `let f = () => { for { … } }` reported
"cannot infer the return type — annotate it (a recursive function always needs one)",
which was wrong twice: a loop is an expression in the AST but never *has* a value
(`break` with one is unimplemented), and the message blamed recursion for something
that had none. `blockValueExpr` answers void for a `for`/`for-in` tail — the same
answer it already gave for a block ending in a statement. The message no longer states
recursion as the cause, because a tail `if`/`match` used for effect still trips it and
is equally non-recursive.

**A `const` still cannot be a range-pattern bound**, and the attempt is worth
recording because the blocker is not where it looks. Admitting `const_identifier` to
`range_pattern` is one line, and it generates after two GLR conflict entries — but then
every all-uppercase *data constructor* pattern (`A`, `MAX`) misparses as a range bound
with a `MISSING ".."`, because `const_identifier` and `user_defined_type_name` match
that text identically and the lexer picks the constant as soon as one is legal in the
state. A third conflict entry is reported "unnecessary": the decision is lexical, not
syntactic, so GLR never gets to weigh it. That is the same ambiguity that blocked the
all-caps struct literal, and it wants that grammar project rather than this reflex.
Backed out; the finding is a comment in `include/patterns/index.js`, so the next person
does not re-derive it.

### 08/06/26
**A bare jump may be a `match` arm body** — `None => break`, `_ => continue`,
`v if v > 10 => return v`. With the statement-arm fix below, the read-until-EOF loop is
finally written the way it reads:

```lyra
for {
  match read_line() {
    None => break,
    Some line => match line.parse_i64() {
      None => println("That isn't a whole number — try again."),
      Some g => { … },
    },
  }
}
```

**Only the spelling was missing.** The jump forms are statements and a `match_arm` body
was `$.expression`, so `None => break` parsed `break` as an identifier and reported
`undefined identifier "break"` — while the *braced* form `None => { break }` worked end
to end, because a block holds statements and the backend already seals a block whose
statement jumped (`matchMerge` then drops the arm, since its block is no longer open).

So the collector **erases** it: `collectMatchArmBody` turns a bare jump into exactly the
single-statement `BlockExpr` the braced form produces. Nothing after the collector learns
the alternative exists — the same treatment juxtaposed constructor application gets.

That erasure is the whole design, and the alternative is the tempting one: letting
`MatchArm.Body` hold a *statement* would push the distinction into the typechecker, the
purity and ownership passes, and all four of the backend's arm-body lowering sites, each
needing a case that does what the block case already does. Erasing at the boundary costs
one function and zero downstream changes. The invariant it buys — and the thing to keep —
is that **the two spellings stay byte-identical in the AST**; a test collects both and
compares the printed trees, because the day they diverge is the day the erased form has a
meaning of its own.

Grammar cost was negligible: `parser.c` +21 KB, no new conflicts, 443 corpus parses.

### 08/06/26
**A `match` arm may end in an assignment — a `match` used as a statement.** It failed
with "block has no value (empty, or last statement is not an expression)", so
`match m { Some v => { x = v; }, None => { x = 0; } }` did not compile and every
statement-position match had to be rewritten as an `if`/`else` chain. Backwards for a
language whose sum types are its main idiom, and the reason the guessing game's read
loop was written with `is_none()`/`unwrap_or` instead of the `match` it wanted to be.

The cause is small and the shape is the interesting part: **`if` never had this
problem**. Its branches go through `lowerBranchValue`, which routes a block body to
`lowerBlockStmts` and lets it come back with no value; the arm bodies went through
`lowerExpr`, which routes a block to `lowerBlock` and *requires* one. Two helpers for
the same question — "lower this body, value optional" — and only one of them was used
by the arms. They now use the same one.

**Four sites, not one.** The arm body is lowered in the shared ladder
(`lowerMatchLadder`, covering scalar, struct and tuple scrutinees), in the `data` tag
switch, and in *two* helpers inside the array match. Fixing a subset would have left
the identical source failing with a different scrutinee — the remote-symptom shape
rule 8 is about — so the tests run one case per scrutinee form.

`matchMerge` gained the bookkeeping that makes it safe: a **reaching** arm with no
value marks the whole match void, so `value()` returns nothing instead of building a
phi over a nil operand. That is deliberately not the same as the case already handled —
an arm that *diverged* has a sealed block and contributes no edge at all, while a void
arm still reaches the merge and control still continues past the match. Mixing arms is
allowed and does the obvious thing: with one arm ending in an expression and another in
an assignment, the match is a statement and the stray value is discarded.

**A pre-existing segfault came with it, in `if` rather than in `match`.**
`lowerVarDecl` guarded a *diverging* initializer (`let x = panic("…")`) but not a
**void** one, and the two are different — diverging means control never reaches the
store, void means it does reach it with nothing to store. So `let r = if c { x = 1 }
else { x = 2 }` dereferenced a nil `init.Type()` and **crashed the compiler**, which is
the "a well-typed program must never panic the backend" invariant rather than a missing
feature. It is now an error naming the binding. The typechecker does not reject binding
a void expression (it only warns the binding is unused), so the backend is where this
has to be caught; making it a *type* error is a language decision, not a bug fix.

Verified by reverting: the four sites restored to `lowerExpr` fail 12 of the new
subtests. Full suite on macOS and on Linux under ASan + LeakSanitizer — worth running
here because an arm holding a managed value through the merge is exactly where a
release goes missing.

The guessing game now reads the way it should:

```lyra
match read_line() {
  None => { println("Giving up? The number was ${secret}."); done = true; },
  Some line => match line.parse_i64() {
    None => println("That isn't a whole number — try again."),
    Some g => { … },
  },
}
```

Still open, and now the only thing keeping that loop from being one `match`: `break`
inside an arm is not in scope (`undefined identifier "break"`). See todo.md.

### 08/06/26
**`lyrac run`.** The build pipeline with every artifact in a temp directory, then `exec`
with the child inheriting stdin/stdout/stderr — so a program reading `read_line()` works
under a pipe, and nothing lands in the source tree.

Two things it deliberately gives up. It **prints no build summary**, because `lyrac run
prog.lyra | grep …` should see the program's output and not the compiler's; that is what
moved the reporting out of `lowerAndEmit` and made it return the executable's path. And it
**passes the program's exit status through**, which means a program exiting 1 cannot be told
from a compile failure — the trade `go run` makes, mitigated by the compiler's own failures
being the ones that also print a diagnostic. The build flags choosing where an artifact
lands (`-o`, `--emit-llvm`, `--keep-ll`) are refused for `run` rather than ignored, and the
missing-compiler fallback that writes `<name>.ll` beside the source is suppressed: a promise
to leave nothing behind, and a message naming a temp path already deleted helps nobody.

**`lyrac build` produces an executable.** It emitted `<name>.ll` and printed the `clang`
command to run by hand; it now runs it — IR to a temp file, `clang <ir> -lm -o <exe>`, and
the artifact is `<name>` beside the source. `-o`, `--keep-ll`, `--emit-llvm` (the old
behaviour, and the one build that needs no C compiler) and `--cc` cover the rest;
`--cc` falls back to `$LYRA_CC` and then `clang`.

Two choices worth the words. **`cc` is not a fallback**: the input is LLVM IR, so a gcc
found under a generic name would reject the file with an error about the file rather than
about the setup. And **a missing compiler still writes the `.ll` next to the source**, even
though the default build otherwise leaves none — the IR is the only thing the user can
compile once they install one, and discarding it is only right on the path where something
better was produced.

### 08/05/26
**Randomness — and the number-guessing program is complete.** `random_seed() -> u64` is
the only builtin: one word of OS entropy. Everything else — `Rng`, `next_u64`, `below`,
`between`, `random_below`, `random_between` — is ordinary Lyra in `std/prelude.lyra`.
Same division of labour as `read_line` beside `parse_i64`, and for the same reason: a
PRNG is arithmetic and arithmetic is expressible; asking the operating system for entropy
is not.

**Keeping the *seed* as the primitive is what makes `det` usable with randomness, and
nothing enforces it — it falls out of bottom-up effect inference.** A seeded generator
only mutates its own receiver, so `rng.below(100)` carries `EffectMut`, which `det`
permits, and a program holding a seeded `Rng` is reproducible. `rng_from_entropy` and
`random_below` reach `random_seed`, so inference gives *them* `EffectRand` and `det`
refuses them. Nobody had to enumerate which randomness is deterministic: drawing from a
seed you were given is, and asking for a seed you were not is not. Had the builtin been
`random_below` instead, every draw would carry the Rand bit and `det` code could not draw
at all.

**That design was impossible until a pre-existing hole was fixed, and the hole is the
most interesting thing here.** A **builtin method** call — `x.wrapping_mul(y)`,
`x.floor()`, `xs.len()` — reaches the purity pass as a `MemberExpr` callee. The pass
derives the dotted name `x.wrapping_mul`, finds it in no table, and falls to the
unresolved-callee default: `AllEffects`. So every builtin method was charged as reading
external input *and* heap-allocating, which made the explicit wrapping/saturating
arithmetic unusable from any `pure`, `det` or `noalloc` function — precisely the code
that wants it, and precisely the functions that want to be `det`. The symptom was remote
in the usual way: not "wrapping_mul is broken" but "the prelude's `next_u64` cannot be
marked `det`".

The fix follows hazard 9 rather than re-deriving the answer: the typechecker
**publishes** the resolution (`typetable.MethodTable.SetBuiltinMethod`) at the one site
that definitively knows, and the checker reads it. Re-deriving it from the property name
would have been a second copy of a settled question *and* blind to the receiver's type —
so a user's own `wrapping_mul` on their own type would have been silently declared pure.

**And it was three copies, not two.** The purity pass asks "what does this call call?" in
`lambdaEffects`, in `methodEffects`, and in the reporting walk in `checkCallPurity`. All
three are ladders over `MethodTable.Get` → `GetBound` → the name; none had a builtin arm.
Fixing two of the three would have been *worse* than fixing none — a call charged no
effect by the inference while still reported as impure by the walk. Hazard 8's note that
"a second copy is enough" now has an instance with a third.

**`below` rejects rather than taking a modulo.** `next_u64() %% bound` is not uniform:
2^64 values do not divide evenly into `bound` buckets, so low residues get one extra value
each. The bias is negligible for a small bound and total for a large one, which is the
worst possible shape — it never appears in the case you test. The cutoff is
`m - ((m %% bound) + 1) %% bound`, where neither the `+ 1` nor the subtraction can
overflow. Note `~u64(0)` for the u64 maximum: it is not writable as a literal
(`IntegerLiteralExpr.Value` is an `int64`, the open >64-bit-literal gap), and
complementing zero is the spelling that does not need it.

**Seed 0 is redirected, not rejected.** xorshift maps 0 to 0, so a zero-seeded generator
emits zero forever; and 0 is exactly what someone reaching for the simplest fixed seed
would write. `rng_seeded` substitutes xorshift64's usual default rather than panicking on
the most natural input a caller has.

`getentropy` is the entropy source — on both targets (macOS 10.12+, glibc 2.25+), unlike
`getrandom` (Linux-only) or `arc4random_buf` (glibc 2.36+), and needing no `FILE*`, so it
avoids the platform-dependent `stdin` symbol that shaped `read_line`. The slot is filled
with `time(NULL)` **before** the call rather than after a failure test: POSIX leaves the
buffer unspecified on failure, so checking the return value and then reading an untouched
buffer would be seeding from an uninitialized stack word.

**`Random.global()` is gone.** It had been a `builtinEffects` entry naming nothing — no
typechecker signature, no lowering — so a program writing it got a clean `lyrac check`
followed by `llvm: unsupported method call "global"`. The existing
`TestDet_AmbientRandom_Violates` used it, and kept its own promise on the way out: it
asserts the diagnostic names *randomness* rather than the conservative Input fallback, and
swapping the source made it fail with exactly the fallback message it was written to rule
out.

Still open and now visible: `Rng.seeded(42)` type-checks and dies in the backend the same
way `Random.global()` did — a member call on a type name is not rejected by the front end.
That is why the constructors are bare (`rng_seeded`), and it is recorded in todo.md.

### 08/05/26
**Console input and `parse_i64` — a program can finally read a number from a user.**
Both halves of what the number-guessing exercise needed, minus randomness. The split
between them is the point: **`read_line` is a builtin because it has to be** (the line
comes from libc and Lyra has no FFI), and **`parse_i64` is written in Lyra in
`std/prelude.lyra` because it can be**. Anything expressible in the language belongs in
the prelude where it is readable, testable and replaceable; keeping the builtin surface
to what is genuinely primitive is what stops the compiler growing a standard library
inside itself.

**`read_line() -> Maybe<string>`, not `-> string`.** EOF has to be distinguishable from a
blank line, and with a bare `string` it is not — both are `""`. That is not a theoretical
loss: the natural shape for reading input is a loop, and a loop that cannot see EOF spins
forever the moment stdin closes. The guessing game demonstrates it either way — with the
`Maybe`, a closed stdin ends the game; without it, the program prints "that isn't a whole
number" until killed. `None` at EOF makes termination the case the reader must handle,
which is the argument for having `Maybe` at all.

Three things in the shim (`input.go`, `lyra_read_line`) that were decisions rather than
details. It reads with **`getchar`** rather than `getline`/`fgets`, because those need the
`stdin` global whose *symbol* differs by platform (`__stdinp` on macOS, `stdin` on glibc)
— a host conditional inside otherwise target-independent IR. It reads **straight into a
ref-counted box** (header + capacity, `realloc`'d as it grows), so the result is an
ordinary owned heap string that the existing release and drop glue already understand. And
it **returns the `Maybe` union itself**, doing the null test internally, so the call site
emits no branches.

**That last one was a real use-after-free, and the shape of it is worth keeping.**
`flushStmtTemps` releases an owned temporary at the statement's *end* block when the temp
was produced in the statement's *start* block, and otherwise **in its own production
block** — the latter because a temp produced inside a branch is undefined on the path not
taken. A **merge** block is neither case. The first version branched at the call site and
returned a merge block, so the owned `Maybe<string>` was released *there* — before the
`match` consuming it ran its switch. The program printed a line of blanks with the right
length instead of the input, which reads as a string-encoding bug rather than a lifetime
one. Making the call a single instruction is what makes an owned builtin result behave
exactly like an ordinary function call, which is what the temp machinery is written
against.

**The ownership pass needed a new predicate, in the direction that is not leak-safe.** A
builtin has neither a `LambdaExpr` nor a `LambdaType`, so it lands on the unresolved-callee
default — *borrowed*. For arguments that default is the safe bias (transfer, i.e. leak);
for a **result** it is the wrong direction: a consuming site retains a value that is already
+1, and a discarded call is never released at all. `calleeIsOwningBuiltin` says so
explicitly, as the result-side counterpart of the existing `calleeIsBorrowingBuiltin`.
Measured both ways with LeakSanitizer on Linux: reverting the rule leaks **848 bytes in 5
allocations**, exactly one per line read; with it, clean.

**`parse_i64` accumulates negatively**, which looks wrong and is not. The i64 range is
asymmetric — there is no `+9223372036854775808` to mirror the minimum — so a
magnitude-then-negate parser cannot represent its own most negative input and would have to
reject `-9223372036854775808`, a perfectly good i64. Building on the negative side covers
the whole range in one path (Go's strconv does the same, for the same reason). The range
guard runs *before* each digit is folded in rather than checking the result after, because
there is no result to check: arithmetic traps on overflow here, so `acc * 10 - d` on a
too-long input would abort the program instead of returning the `None` the function exists
to return. It is also why the minimum is never written as a literal — it is not
representable in `IntegerLiteralExpr.Value` (an `int64`), the open >64-bit-literal gap — so
the final digit is bounded by 8 or 7 according to the sign instead.

Tested against the **real** `std/prelude.lyra` through the resolver rather than a copy
pasted into the test, since a copy would be a second implementation free to drift from the
one users get (`llvm_input_test.go`, `buildAndRunWithPrelude`).

**`!` lowers, and it had been grouping the wrong way.** `!x` is `xor i1 x, true`. It had
type-checked since long before it lowered, so it reached the backend as "expression
lowering not implemented for `*ast.NotBooleanExpr`" and no program using `!` could be
built — which is exactly why nobody had noticed that **`!a && b` parsed as `!(a && b)`**.
`!`'s operand was `$.expression`, so it absorbed the `&&`: the opposite grouping from every
C-family language, and silent, because both readings are well-typed `bool`. `PREC.UNARY` on
the rule could not fix it — a precedence does not stop a wider operand rule from absorbing
more — so the operand was narrowed to a postfix expression (`_not_operand`), which still
reaches `parenthesized_expr` so `!(a && b)` says the other thing. Parser cost was mild:
441 corpus tests pass, no new conflicts.

The regression tests assert on an observable **side effect** rather than on the result,
because the two readings usually agree on the value and differ only in whether the right
operand is evaluated — `if !a && side()` takes the same branch under both, and only a
`println` inside `side` distinguishes them.

**Still missing for the guessing game: randomness.** `Random.global()` is an entry in
`checker/effects.go`'s table and nothing else — it passes `lyrac check` clean and dies in
the backend with "unsupported method call \"global\"". See todo.md.

### 08/05/26
**Three clarity cleanups, two of which were the codebase telling readers something
false.**

**One match ladder instead of two, and one merge instead of three.**
`lowerScalarMatch` had its own copy of the if-else ladder that `lowerAggregateMatch`
already expressed as a driver taking `test` and `bind` closures — the same merge block,
incoming/phi bookkeeping, per-arm scope reset, catch-all handling and seal, differing
only in the two closures. A scalar match is that shape with a comparison for `test` and
nothing to bind, so it delegates now, and the driver is renamed **`lowerMatchLadder`**
since it is no longer aggregate-only. The `data` tag switch and the array ladder stay
separate — a tag switch is not a ladder, and an array pattern's test spans several blocks
rather than yielding one condition in the current block, which is a different *shape*
rather than a different filling — but all three shared one more thing, a local
`type incoming struct` and its phi epilogue declared three times over. That is
**`matchMerge`** now, which also gives the invariant a name: an arm reaches the merge only
if its block is still open, so a diverged body contributes neither a branch nor an
incoming, and a match whose arms all diverge has no value rather than an empty phi. Net
−57 lines across the three files.

**`llvm.go`'s package comment was actively wrong.** A ~120-line status inventory lived
there and had drifted into announcing default parameters, destructuring parameters, string
interpolation and higher-order calls as "deferred with loud errors" long after each
lowered, describing `Emit` as a SKELETON emitting a placeholder body, and listing 14 of the
package's 35 files. It is now a short pointer to README.md — which is the inventory — plus
the two cross-cutting invariants (refuse rather than guess; never panic on a well-typed
program) and an accurate file map. This is the same drift the workspace CLAUDE.md records
against its own duplicated package map, with the same fix.

**Dead code in the typechecker.** `inferIdentifierCall` repeated a `*ast.LambdaExpr` type
assertion that had already returned twenty lines earlier, so the block was unreachable and
its comment ("sym is some other Named (e.g. Parameter)") described something that could not
happen — a `Parameter` would not satisfy that assertion either. And `checkStructDecl` was an
empty function still dispatched from `checkTypeDecl`, which read as "structs are checked"
while doing nothing; the switch is now an `if` for the one type that has declaration-level
rules, with a note that a struct is checked by being used.

### 08/05/26
**A call could not be the first thing inside parentheses**, and the cause was a grammar
rule no legal program could have used. `(f(7))`, `(f(7), 1)`, `(f(7) + 1, 1)` and
`((f(7)), 1)` were all syntax errors — while `(1, f(7))` was fine, which is the tell: by
the second element the competing reading is already dead.

`tuple_pattern` carried an optional leading name aliased from `$.identifier`. That name
could never be right — `identifier` is lowercase-leading by lexer rule, a named tuple
*type* is PascalCase — so it matched nothing a program could legally write, but it did
outbid the expression reading of the same tokens: `(f(7))` parsed as a parameter list
holding the tuple pattern `f(7)`, then failed at the close paren. The parse tree said so
directly, which is what turned a puzzling "unexpected `(f(7), 1)`" into a one-line fix.

An **uppercase** named tuple pattern was never this rule: `Point(x, y)` is a
`data_pattern`, which the typechecker resolves to a tuple type when the name is one. So
`tuple_pattern` is anonymous-only now, and the generic-argument slot went with the name,
since arguments with nothing to apply them to are not a form. No corpus test used the
name and no collector read the field — only `tuple_literal.go` reads `tuple_name`, from
the *expression* rule that happens to share the alias.

Removing it **shrank** the parser: 8,262 → 8,234 states, `parser.c` −44 KB. The forms
that share the rule are pinned by tests — a lambda's tuple parameter, a destructuring
`let`, a match arm, and `(Some(x): Opt) -> i64`, the destructured parameter the grammar's
own notes call this region's canary.

### 08/05/26
**Two parse bugs, and the second was why the first had been stuck.**

**An all-uppercase type name could not be used in a struct literal.** `struct S` declared
fine and `S { v: 1 }` was a syntax error — in every position tried: block `let`, top-level
`let`, call argument, return expression. It was reported as being about *short* names, and
it is not: the affected set is any name with **no lowercase letter and no underscore** —
`S`, `S0`, `AB`, `HTTP2`, `A1B` — while `Sa`, `So`, `Box2` and `Point9` are fine.
`user_defined_type_name` (`[A-Z][a-zA-Z0-9]*`) and `const_identifier` (`[A-Z][A-Z0-9_]*`)
match that text at the same length, so the state-aware lexer must pick one, and in
expression position — where a constant is also a legal expression — it picks the constant.
Scoping it turned up two more victims of the same collision: a named tuple (`AB(1, 2)`,
which failed with "cannot resolve function AB") and juxtaposition (`CD 5`).

**`if Point { 1 } else { 0 }` was also a syntax error**, for any type name, and this is what
blocked the first fix. `prec.left(PREC.STRUCT_LITERAL)` resolved `Name • {` statically toward
the struct literal, so the parser committed before it could see that `{ 1 }` is a block and
not a struct body. Letting all-caps names start struct literals extends that failure to
`if MAX { 1 }` — ordinary code — so the struct half was **reverted** on the first attempt and
only the tuple half shipped, with the blocker written down. Fixing the brace ambiguity
unblocked it, and both landed together.

The fix is `named_struct_literal` as a **choice of two alternatives with different precedence
kinds**, because the name is contested by two rivals wanting opposite resolutions. With
generic arguments (`Point::<f64> { … }`) the rival is `_tuple_name`, settled by the static
precedence the two deliberately share, so that alternative keeps `prec`. Without them the
rival is the bare-name reading, so that alternative takes `prec.dynamic` and three declared
conflicts let GLR decide on the brace's contents.

**Two dead ends, both found by measurement.** Putting the whole rule on `prec.dynamic` breaks
the first contest — `_tuple_name`'s static precedence wins and `Point::<f64> { … }` stops
parsing. Making `_tuple_name` dynamic to match fixes that and breaks parenthesized forms far
afield, down to `(f(7), 1)`. And the corpus did **not** catch that second one; the sibling Go
suite did, which is the case for running both before believing a grammar change. Cost: 8,206
→ 8,262 states (+0.7%), `parser.c` +82 KB.

**Juxtaposition is deliberately still PascalCase-only.** `data_constructor_expr` must keep
taking a name that can only be a constructor, because that is what leaves `MAX - 1` with no
competing "apply `MAX` to `-1`" reading. So `CD 5` remains an error for an all-caps
constructor while `CD(5)` works — the one part of the collision left unfixed, on purpose.

### 08/05/26
**The front end is ~25% faster, and the reason was not on anybody's list.** The 08/05 review
proposed ten performance fixes, ranked by reading the code: linear scans in the typechecker
(`findDataTypeByConstructor` over every type × constructor per constructor expression,
`resolveTraitMethod` over every impl per method call), fixpoint recomputation in purity,
four body walks in ownership, quadratic captures. All plausible from the source. The first
thing built here was not a fix but a **benchmark** — `pkg/driver`'s `BenchmarkAnalyze_*`,
running the real pipeline over the real prelude, since the LSP re-runs all of it per
keystroke — and a profile of it disagreed with the whole list.

The cost was `(*Node).ChildByFieldName`, at **~26% of all samples**, about half the front
end, spread over ~12 collector call sites with no single hot caller. Not the lookup: the
*name*. Every call allocates a C string from the Go field name, calls into C, and frees it,
and the collector asks at nearly every node. tree-sitter also takes a pre-resolved field
**id**, so `pkg/cst`'s `Field` memoizes name → id and calls `ChildByFieldId`. Same answers,
nil included — verified node-by-node over the prelude, including a name the grammar does not
define — and ~3.7x faster on the same walk. All 231 call sites moved with `gofmt -r`, which
is AST-aware and so safe on arbitrary receivers. End to end: **1.33ms → 0.99ms, 2.44 → 1.85,
6.84 → 5.23** (small/medium/large, three runs each), a consistent ~25%.

**What the measurement said about the proposed fixes is the more useful half.** Seven of them
— `resolveTraitMethod`, `lambdaTypeVars`, `ownsManaged`, `typeImplementsTrait`, `ufcsFunction`,
`substituteTypeVars`, `capturesOf` — do not appear in the profile at all, and the remaining
three sit near 1%. That is not proof they are cheap in principle, so the claim was tested
where it should bite: `BenchmarkAnalyze_WideTypes` builds 60 data types with three
constructors each, 60 trait impls and 120 use sites, exactly what those scans are quadratic
in. `findDataTypeByConstructor` reaches 2%; the rest are still absent. The scans are real in
big-O and irrelevant at any size this language is written at today. None were changed.

The remaining tree-sitter cost is now diffuse — `ChildByFieldId` 6.7%, `Kind` 3.9%, `Child`
2.9%, `StartPosition` 2.9% — with no single win left. `Kind()` is the next one worth
considering, since it converts a C string to a Go string per call and the collectors switch
on it constantly, but it needs kind *ids* threaded through many switches for ~4%.

Found while building the wide benchmark and left as separate work: **a struct literal whose
type name is a single uppercase letter or letter-plus-digit (`S`, `S0`) does not parse
anywhere** — block `let`, top-level `let`, call argument, return expression — while `Sa`,
`So`, `Box2` and `Point9` are fine, and the *declaration* parses either way. Almost certainly
a grammar rule reserving short uppercase names.

### 08/05/26
**The two typechecker duplications behind the same hazard, and one of them was a live
semantic bug.** Same review pass as the backend entry below; these are the front-end half.

**A trait method did not narrow to its declared return type, so its arithmetic had
different semantics from an identical free function.** The check that compares a value
against the declared return — infer, `contextualType`, assignability, then
`propagateLiteralType` / `propagateAllocation` and the owned-return allocation check — was
written four times: a single-expression body, an explicit `return`, a block's trailing
expression, and a trait-impl body. The fourth had drifted, running neither `contextualType`
nor either propagation. The consequence was not cosmetic. Lyra's arithmetic is checked, so
width decides whether an operation traps:

```
trait Small { get: (Self) -> u8 }
impl Small for Pt { get = (self) => 200 + 100 }   // silently 44
let get = () -> u8 => 200 + 100                   // traps, exit 101
```

The same expression with the same declared return gave two answers depending on where it
was written — the body computed at the `i64` default and was truncated at the return
boundary, so the overflow the language exists to catch went unreported. All four sites now
call one `checkReturnValue`, and the trait path inherited the propagation it never had.

**`resolveType` and `resolveTypeIfKnown` are one walk.** The pair `CLAUDE.md` hazard 8
named as its outstanding instance, and `todo.md`'s open item: ~120 lines of identical
recursion over the same composites, differing only at an unknown *name*. That is what let
the twin fall behind by `ParameterizedType` and `*LambdaType` on 08/03 ("expected
`Maybe<weak Node>`, got `Maybe<weak Node>`"). They now share `resolveTypeWith`, which takes
the leaf as a callback; both names stay as wrappers, so no call site changed.

The fold's one subtlety is the thing todo.md warned about: **the leaves differ by more than
whether they report.** The reporting one also follows alias chains, caches by resolved
identity, checks visibility and guards circularity — none of which the quiet one does — and
it recurses through `resolveType` rather than the walk's own recursion because it resolves a
declaration's type *from the declaration's location*, not the reference's. Those stayed in
the leaf; only the composite recursion is shared.

### 08/05/26
**Three backend paths that had drifted from their siblings.** All three are hazard 8 — a
thing written more than once, where only one copy got the fix — found by reviewing the
backend for duplication rather than by hitting the bugs.

**An indirect call had no diverging-argument guard.** `lowerDirectCall` checks `diverged`
per argument because a `panic(…)` argument seals the block and yields nil; the README
calls that guard load-bearing, since without it llir dereferences the nil and the compiler
segfaults. `lowerIndirectCall` had its own argument loop and never grew the check, so the
same `panic` through a function *value* still crashed:

```
let apply = (f: (i64) -> i64) -> i64 => f(panic("indirect argument"))
```

crashed `lyrac` in `InstCall.LLString` — a Go stack trace, not the loud error the backend
is supposed to produce. Both paths now lower arguments through one `lowerCallArgs`, which
also owns the by-reference `mut`/`ref` handling; an indirect call passes no parameter list,
because a lambda *type* carries no borrow modes, so that half is simply inert there.

**The three shape resolvers were one switch written three times, and only one had been
fixed.** `resolveDataType` grew a `ParameterizedType` arm when a `data` sub-pattern nested
inside an aggregate failed — the top-level match path resolves its scrutinee before
dispatching, but a sub-pattern reads the element type straight off the enclosing tuple,
where it is still parameterized. `resolveStructType` and `resolveTupleType` sit on that
identical path and had none of it, so a nested generic struct or named-tuple sub-pattern
failed with *"struct pattern on non-struct value of type `Box<i64>`"* — the same sentence,
one noun changed. The durable form was available here and taken: the normalization is now
one `resolveShape`, and the three resolvers are a type assertion each. Only the **nested**
position was broken; the top-level one is now a control case in the tests, since it
resolves its scrutinee first and worked throughout.

**A struct literal skipped `coerceAggregateElem`**, which the tuple-literal and
data-payload paths both apply so a residual int-width mismatch is coerced rather than
panicking llir. This one is a **parity fix, not a fixed crash**: no program was found that
reaches it, because the typechecker currently *rejects* the shapes that would (a generic
struct at a narrow width — `let b: Box<u8> = Box { v: 42 }` — is "cannot assign `Box<i64>`
to `Box<u8>`", where the analogous data payload narrows fine). So the third way into an
aggregate now matches the other two before literal-width propagation ever reaches through
a generic struct and makes it reachable.

**Two adjacent gaps found while testing, both left alone as separate work:** a named-tuple
constructor pattern (`Pair(7, _)`) nested in an aggregate is parsed as a data pattern and
never falls back to the tuple shape, so it fails for a *non-generic* named tuple too; and
binding a name out of a generic aggregate pattern (`Box { v }`) is "undefined identifier
`v`" in the typechecker, at top level as much as nested — a front-end gap, not a lowering
one.

### 08/04/26
**A bare call resolves across modules the way a method call does.** `map(b, f)` now reaches
whatever `b.map(f)` reaches.

The two spellings had different machinery, and that is the whole bug. A method call has
never used the scope chain — `ufcsFunction` gathers every reachable declaration and picks by
receiver — while a bare call resolves a *name*, module → prelude → global, stopping at the
first hit. So with a `map` for `Box` in an imported module, `b.map(f)` resolved and
`map(b, f)` reported *"no overload of map takes a Box receiver"*: the prelude's scope sits
nearer than the global one an import exports into. Same call, same receiver, two answers,
and the desugar's own premise is that those two spellings are one call.

**The fallback runs only after the scope chain has failed**, which is what makes it
additive. A name that resolves to something accepting the receiver is untouched, so a local
declaration still wins and nothing that compiled before changes meaning — only calls that
were errors become resolutions. Reachability and ambiguity reuse the method form's rules
exactly, because they are the same question asked from the other spelling rather than a
second dispatch mechanism.

Two boundaries held deliberately:

- **A plain function still errors normally.** Only a *receiver* function gives way; a
  non-`self` function whose first argument does not fit is an ordinary argument-type error.
  Dispatching there would turn a typo into a call to something else entirely.
- **The single-declaration case needed it as much as the overloaded one**, and reported
  worse: a prelude `is_some<t>(self: Maybe<t>)` shadowing an imported `is_some(self: Box)`
  produced *"cannot infer type variable t from these arguments"* — a unification failure
  describing the symptom of a resolution that had already gone wrong.

**The backend needed widening too**, which is the part that would have been easy to miss:
a call resolved this way reaches its callee by *identity*, and that callee is usually an
ordinary singly-declared function rather than an overload member. `l.overloads` held only
overloads, so the typechecker resolved the call and the backend then reported `call to
unknown function "map"`. Every user function is now recorded by declaration
(`recordByDecl`), which costs one map entry and removes the distinction. Found by running
the program rather than by type-checking it — the analysis was already clean at that point.

**What this does not fix**, and cannot: names with **no receiver**. Two modules exporting a
plain `helper` still collide on the bare key, and an imported `pub` name still forbids the
importer its own. Receiver dispatch settles the names that have something to dispatch on;
the key-level fix in todo.md is what settles the rest.

### 08/04/26
**`lyra-E016` points at the allocation.** The message is now *"`noalloc` function
heap-allocates: an array comprehension builds a `[]T` at 2:46"* instead of a list of every
form the language can allocate with.

It listed forms because `EffectAlloc` is a single bit: by the time the bound was checked,
*that* something allocated was known and *what* was not. So the effect inference records
each callable's first directly-allocating expression as it walks
(`allocContext.lambdaSites`/`methodSites`, alongside the existing
`impureLambdas`/`impureMethods` pair). The alloc context was already threaded to every point
that sets the bit, so this cost a local recorder in each walk rather than another parameter.

Three decisions in it:

- **First, not all.** One precise location is what a reader acts on, and a second allocation
  in the same `noalloc` function is not a separate mistake — removing the bound or the
  allocation fixes both.
- **First-write-wins, because the inference is a fixpoint.** Each body is walked several
  times; keeping the earliest write makes the reported site independent of how many passes
  convergence happened to take, which is the difference between a stable diagnostic and one
  that moves when an unrelated function is edited.
- **An allocation through a callee is not attributed to the call.** There is no allocating
  expression in this body — the call is here, the allocation is in the callee — so that case
  keeps the form-listing wording. Pointing at the call would name a line that does not
  allocate, which is a worse error than a vague one. Pinned by its own test.

The descriptions track the *syntax* rather than the representation (`describeAllocation`):
"an array comprehension builds a `[]T`", "`++` builds a new string". Naming the construct is
only useful if the name matches what is on the line the position points at.

### 08/04/26
**`noalloc` sees string allocation too.** `"a" ++ s` and `"n=${n}"` in a `noalloc` function
are `lyra-E016`; a string **literal** is not, and neither is passing one through or
comparing two.

**Strings needed a second rule, not an extension of the first.** The array fix earlier today
made the alloc effect ask about a value's *representation*, which works because the type is
exactly what separates the allocating `[]T` from the stack-resident `[N]T`. For strings the
type carries no such information: a literal, a `++` and an interpolation are all `string`,
and only the last two allocate. A literal interns as a **pinned static box** whose
retain/release are no-ops; `++` and interpolation each build a fresh ref-counted box
(`lowerStringConcat`, `lowerInterpolatedString` — the backend's own note calls concatenation
"the first value this backend heap-allocates").

So the discriminator is the **expression kind**, and `allocatesByForm` is deliberately kept
separate from `heapRepresented` rather than folded in. One predicate covering both would
have to mean "the type says so" in one case and "the syntax says so" in the other, which is
how a rule stops being checkable by reading it.

Two details that are decisions rather than mechanics:

- **`allocatesByForm` is gated on the TypeTable even though it never reads one.** The
  AST-only `InferredEffects` entry point reports no allocation at all by contract; letting
  it find strings but not `shared` values or arrays would make its answer partial in a way
  no caller could detect, which is worse than the documented nothing.
- **The arena discharge already covered it.** `buildAllocContext` marks *every* expression
  inside a `with` body, not only constructions, so a `++` inside an arena was discharged
  from the moment it started counting.

The `lyra-E016` message now lists all three allocating forms and says explicitly that a
literal is not one — that distinction is the whole reason strings are shaped differently
from arrays, so a reader who hits this needs it. It names forms rather than the offending
expression because `EffectAlloc` is one bit; recording the site is noted in todo.md.

Only **escaping closures** are left of the original deferral, and that one is genuinely
blocked rather than merely undone: they are boxed in the dev lowering and free under Lambda
Set Specialization, so what `noalloc` should say depends on a tier that does not exist yet.

### 08/04/26
**`noalloc` sees array allocation.** The alloc effect asks about a value's **representation**
now, not its flavor, so `pure noalloc (…) -> []i64 => [1, 2, 3]` and a `noalloc` function
containing a comprehension are both `lyra-E016`.

The old rule — flavor is `shared` — is the *right* question for a construction, and that is
why it was written that way: allocation there is a use-site property, the same `Node{…}`
being stack or heap depending on how the value is used. It is the wrong question for a
`[]T`, which lowers to a pointer to `{ rc, len, [0 x T] }` before any flavor is consulted.
There is no flavor that makes `[1, 2, 3]` not allocate, so a function could claim `noalloc`
and allocate on every call.

It became urgent rather than theoretical when `map` and `filter` for arrays went into the
prelude as comprehensions: the annotation was briefly on functions that allocate per
element, which is how it was noticed. (That one was removed by hand at the time — this is
the check that would have caught it.)

Two properties the rule keeps, both worth stating because the obvious simplification loses
them:

- **It is about value-*producing* forms, not about types.** A `[]T` identifier is
  heap-represented and allocates nothing, so `heapRepresented` is asked of the construction
  cases in the effect walk rather than of every expression. Asking it everywhere would
  charge every mention of an array to its enclosing function.
- **The same literal is judged by what it was used as.** `[1, 2, 3]` as a `[]T` allocates
  and as a `[3]i64` does not, because `allocates` reads the type the typechecker recorded
  rather than the syntax. Pinned by a test whose two cases differ only in the return type.

The diagnostic no longer says "by constructing a `shared`-typed value" — a reader told that
about an array would go looking for a construction that is not there.

Still deferred, and both are the shape the array case had: **strings** (`"a" ++ b` builds a
new one; the extra decision is that a string *literal* is a constant, so unlike `[1, 2, 3]`
only the producing forms should be charged) and **escaping closures** (boxed in the dev
lowering, free under Lambda Set Specialization — so the answer depends on the tier, which is
why `noalloc` is defined against the release lowering).

### 08/04/26
**A comprehension's result may be any expression.** `[ x in xs | "a" ++ b ]` parses, as do
an `if`, a `match`, and a lambda in result position. `result_expr` had been a
hand-maintained `choice` — `_math_operand`, the tuple and struct literals, an array literal
— which is a list of "forms someone has needed so far" rather than a rule, and it read to a
user as one they had to learn: string concatenation in a comprehension was a *syntax error*
with nothing to suggest why.

**Widening it to `$.expression` made the parser smaller**, which is the part worth
recording, because the reflex is to add the one missing node (`string_concat_expr`) and move
on. Measured: 8,232 → 8,202 states and 35 KB off `parser.c`, *and* it retired the
`[result_expr, _primary_expr]` conflict entry outright. The narrow list had been competing
with `_primary_expr` over what a bare name or literal in result position reduces to;
`$.expression` subsumes that reduction, so the ambiguity stops existing rather than being
resolved. The conflict removal was verified against the corpus rather than trusted —
`tree-sitter`'s "unnecessary conflict" warning is documented in that repo's CLAUDE.md as
unreliable in exactly this region, and this time it happened to be right.

The `|` rule is untouched: `[ x in R | A | B ]` is still guard-then-result by `prec.dynamic`,
and a bitwise-or meant as a value is still parenthesized. That is a choice between two
*complete* parses, which widening the operand does not affect — pinned from both sides now,
in the corpus and in a behavioural test.

### 08/04/26
**Descending ranges, and `..=` became `..<=`.** The four end operators are now `..<` `..<=`
`..>` `..>=` — two axes, direction and whether the end bound is included, each named by the
operator. `5..>1` is 5, 4, 3, 2; `5..>=1` is 5, 4, 3, 2, 1. Both work in `for-in` and as a
comprehension source.

**What the design turns on: direction is the operator's, never the bounds'.** `5..<1` is an
*ascending* range that happens to be empty, not a descending one. The first proposal was to
add only `..>` and let `..=` mean "inclusive, whichever way the bounds point" — one token
fewer, and no migration. It was rejected on the second pass because direction would then be
a property of the operand *values*: `a..=b` over variables would decide at run time which
way to run, and could silently run the opposite way from the way it reads, with nothing to
report. Every operator naming its own direction makes it a parse-time fact instead. The cost
was renaming the inclusive end at 260 sites across three repos; the benefit is that the
failure mode does not exist rather than being documented.

Two things fall out of it. The **step becomes a magnitude**, so `InvalidStepReason` can
finally judge a negative one — the open item that deliberately refused to, on the grounds
that judging the sign would invent a direction semantics the language had not chosen. And
the **predicate and the increment both come from the operator**: `rangeLoopPredicate` picks
one of eight comparisons from direction × inclusivity × signedness, and a descending loop
subtracts. Signedness stays a property of the counter's type, not the direction, so an
unsigned descending range does not depend on a wrap the author never wrote.

**What this fixed, which was worse than "descending was missing".** Measured before
changing anything: `for i in 1..<10:-1` ran forever (1, 0, −1, … all satisfy `i < 10`), and
`for i in 5..<1:-1` silently did nothing. A negative step either hung or was ignored
depending on which way the bounds happened to point, and there was no way to count down at
all.

**Descending is refused where a range is a set** (`lyra-E034`) — a match pattern, a
`newtype` constraint. There `5..>1` describes exactly the members `1..<5` does, so the
spelling implies an order the construct does not have. The grammar accepts all four
operators at all three sites, keeping the one node kind the 08/01 unification established,
and the collector draws the line: that is the rule `rangeBounds` already states — the
grammar refuses what has no meaning anywhere, the collector refuses what has a plausible
meaning needing disambiguation. The message names the ascending spelling of the same set,
built from the `start`/`end` **fields** rather than by splicing the node's source, because
a constraint's text is the whole `range(…)` wrapper and splicing produced `0)..<=range(100`
— a suggestion that will not parse, which is worse than no suggestion.

One migration note for anyone reading older entries here: they show `..<=` because the
spelling was rewritten throughout, not because it existed before today.

### 08/04/26
**Range and string sources for comprehensions.** `[ x in 1..<=10 | x * x ]` — the shape the
grammar has documented since it was written — and `[ c in "héllo" | c ]` both lower, and mix
with array sources in one comprehension.

**A source now drives its own loop.** The first version reduced every source to "a length,
plus the value at index *i*", which fits an array and a range and does *not* fit a string:
UTF-8 is variable width, so a string's walk is a byte cursor whose advance is whatever the
decoder just consumed. The choice was to special-case strings in the nesting or to let each
source emit its own loop; the second keeps the nesting ignorant of what it is nesting, and
arrays and ranges still share one emitter (`countedSource`).

**The interesting constraint is that a comprehension's capacity must bound its loop by
construction rather than by agreement.** Everywhere else, a count that disagrees with its
loop is a wrong answer; here the loop writes into a box sized from that count, so a
disagreement is memory corruption. An array's bound is its length, and a string's is its
*byte* length — which bounds the rune count because no encoded rune is shorter than a byte,
an over-approximation of exactly the kind a guard already forces.

A range is the one that had to change shape for this. A `for-in` range re-tests `i < end`
each iteration; a comprehension instead derives the count once and runs the loop exactly
that many times. The formulas would agree for every sensible range, and "would agree" is not
the property worth having — driving the loop from the count means they *cannot* disagree
whatever the bounds turn out to be at run time. The payoff is visible in the degenerate
cases: `5..<1` and a non-positive step both yield an empty array, where a re-testing loop
would run away past the allocation. (`for i in 5..<1:-1` loops forever today. A
comprehension deliberately does not inherit that, because the consequence here is not a
hang.) The division is guarded against a zero divisor before the fact, since `sdiv` by zero
is undefined even on a path whose result is then discarded by a `select`.

Tested through the edges rather than the happy path: inclusive against exclusive ends, a
stride, an empty span, a backwards span, a guard over a string, multi-byte runes (`"héllo"`
is five runes in six bytes, so the recorded length must be the rune count and not the
capacity), and a mixed array-plus-range comprehension. Clean under Linux ASan with
LeakSanitizer.

### 08/04/26
**Array comprehensions lower, and with them `map` for arrays.** `[ x in xs | x % 2 == 0 |
x * 2 ]` collects, type-checks and emits. The grammar has had them since before there was a
collector for them — with a careful note about `|` being both a section separator and
bitwise-or — but nothing downstream did, so the expression reported `unknown expression
type "array_comp_expr"`.

**Why this was the thing to build rather than the thing that was asked for.** The request
was a `map` for `[]t` in the prelude, written recursively over `[head, ...tail]`. That
formulation cannot work, and neither can any other, because **there is no way to build an
array**: no growth operation, and a spread in an array literal (`[0, ...xs]`) parses but is
never collected. A comprehension is the only construct that produces one, which makes it the
prerequisite rather than an alternative. `map` for arrays is now a single line in the
prelude, and `[]t` is a third receiver head beside `Maybe` and `Result`.

The lowering allocates one box, walks the generators as nested counter loops, short-circuits
the guards, and stores each survivor at a running count that becomes the box's length.
**Capacity is the product of the source lengths**, allocated up front. The alternatives were
to run the generators twice — once to count, once to fill — or to grow the box as it fills.
Running twice is wrong rather than slow: a guard may call a function, and evaluating it
twice per element makes the number of calls a property of the lowering. Growing needs a
reallocation primitive that does not exist. Over-allocating costs memory only when a guard
actually filters, and keeps every guard evaluated exactly once.

A comprehension is always `[]u`, never `[N]u`, even with no guard — a guard decides the
length at run time, and adding one should not change the expression's type.

**It exposed a real bug in yesterday's overloading work, of the worst kind.** A
specialization was keyed by `name + bindings`, which stopped identifying a function the
moment one name could mean several: the `Maybe` `map` and the array `map` at `t=i64,u=i64`
produced the *same* key, collapsed onto one emitted function, and a call to one reached the
other. There is no diagnostic anywhere in that path — it surfaced as an invalid GEP against
the wrong type while emitting IR, and only in a program using **both** overloads at the same
type arguments, which is why neither overload's own tests found it. `Instantiation` now
carries a `Disc`riminant — the receiver head, the same discriminant `userSymbol` uses for a
non-generic overload, so the two paths agree about what makes two `map`s different
functions. It is empty for a name with one declaration, so every existing key and emitted
symbol is byte-for-byte unchanged. Pinned by a test that was confirmed to fail without it,
where the wrong resolution returns 0 instead of 12 rather than crashing.

Deferred loudly rather than approximated: a **range or string** generator source (a range
needs its count derived from start/end/step including inclusive and negative-step cases; a
string yields *runes*, whose count is not its byte length, so the capacity rule is wrong for
it as well as the walk), and a generator whose source **depends on an earlier generator**
(`[ row in grid, cell in row | cell ]`) — sources are materialized once before the loops,
which is precisely what makes the capacity computable.

Two limitations found on the way and recorded rather than fixed: `result_expr` in the
grammar is narrower than an expression, so `[ x in xs | "a" ++ b ]` is a *syntax* error; and
**`noalloc` does not see an array allocation at all** — `allocContext.allocates` counts only
values whose flavor is `shared`, and a `[]T` box is heap-allocated without being one, so an
allocating function may be declared `noalloc` today. That one is pre-existing and applies to
array literals just as much as to comprehensions.

### 08/04/26
**A generic body may call another generic at a variable-dependent instantiation.** The
prelude's `unwrap` is now written as `expect(self, "unwrap on a None")` — one trap site
instead of two — and `let get_or<t> = (o: Maybe<t>, d: t) -> t => o.unwrap_or(d)` compiles,
which is the case todo.md had listed as open since generics landed.

**What was actually wrong.** The typechecker records, per call site, the bindings it
solved. Inside a generic body those are written in the *enclosing* body's type variables:
`unwrap<t>`'s call to `expect` records `expect<t=t>`, where the right-hand `t` is unwrap's.
That is not a specialization — it is a template, one per enclosing specialization — and the
monomorphizer refused to lower it (`type variable "t" has no concrete type here`) rather
than guess, which was the correct failure and not a useful one.

The fix is composition, closed to a fixpoint: for each concrete specialization the program
uses, walk its body and compose its bindings into every generic call it contains, turning
`expect<t=t>` into `expect<t=i64>` — a specialization that must be emitted although no call
site in the program names it.

**Where it runs is the part worth recording, because the obvious place is wrong.** The
failure surfaces in the backend, so that is where the fix wants to go. It cannot: the
ownership pass runs **once per instantiation** (`OwnershipBySpec`), because whether a value
is reference-counted is a property of the type argument. A specialization discovered after
that pass has no table of its own and silently falls back to the program-wide one — which
is analyzed generically, where a type variable is *not* managed, so a `t = string` body
emits neither retains nor releases. That is precisely the double free this project fixed
once before, and re-creating it would have been invisible on every scalar test. So the
closure runs in the driver, before ownership; `pkg/driver/instantiations.go` says so at the
top, and a test asserts every concrete specialization has a table.

**Polymorphic recursion, and a bound chosen by measurement rather than taste.** `f<t>`
calling `f<Box<t>>` needs infinitely many specializations, each one deeper than the last;
monomorphization is not defined for it, and every monomorphizing language refuses it. The
first bound written here was a count — 10,000 specializations, "deliberately far above any
real program". It terminates in theory. In practice the run went past a minute and a
gigabyte of resident memory before firing, because with polymorphic recursion *each member
is also huge*: the keys are `Box<Box<Box<…>>>` strings growing linearly, so reaching the cap
costs quadratic time and space. A compiler that eventually errors after a gigabyte is
indistinguishable, to the person waiting, from the hang it was meant to prevent.

The bound that works tests the thing that is actually growing — **type depth**. Twenty-four
constructors deep is far beyond anything hand-written, it fires in a few dozen cheap steps,
and it names the real condition in the diagnostic. The count survives as a backstop for
divergence that is wide rather than deep, which nothing is known to produce. The test pins
the *deadline*, not just the message: a 30-second timeout fails if the depth bound ever
stops firing. Recursion at the same type is untouched — the composed key repeats and the
worklist closes.

`substituteTypeVars` moved to **`types.Substitute`**, since the driver now needs the same
walk the backend has always had, and hazard 8 is explicit about what two copies of a switch
over composite types do. Taking the union turned up `*LambdaType`, which the backend's copy
never handled: invisible until something substitutes into a signature carrying a callback,
which is exactly what a generic combinator is.

**And the test gap that let this ship.** The prelude was committed in a state that
type-checked and could not build, and `go test ./...` was fully green. Every prelude test
stopped at analysis, and the backend's own tests use hand-written declarations rather than
`std/`, so nothing ever asked the code generator to emit the real combinators — the failure
was reachable only by running `lyrac build` by hand. `TestShippedPrelude_Lowers` closes it
by emitting IR for a program that exercises every combinator on both receivers, at a managed
payload as well as a scalar one; it was confirmed to fail against the unfixed driver rather
than merely added. This is the same gap as yesterday's "nothing analyzed the shipped
prelude", one layer down, which is the uncomfortable part: the fix then should have been the
fix now.

### 08/04/26
**A method call resolves against every declaration of the name the file can reach, not
against the one its key resolves to.** `ufcsFunction` now gathers candidates by name
(`SymbolTable.FunctionsNamed`) and filters them by the three things that actually decide a
method call — it takes a `self` receiver, this file can reach it, and it accepts this
receiver — instead of resolving the name to one declaration and asking whether *that* one
fits.

**The bug it fixes had no diagnostic, which is the whole reason it is worth recording.**
Adding `map` for `Result` to the prelude, while `std.maybe` still declared `map` for
`Maybe`, did not produce an error — it silently removed a method. The prelude keeps bare
declaration keys, so its `map` took the program-wide one; that flipped
`shadowsPrelude("std.maybe", "map")` to true, which pushed `std.maybe`'s `map` onto a
qualified key; and the UFCS rung consulted exactly one candidate by name, so `m.map(f)` on
a `Maybe` found the `Result` overload, failed to match the receiver, and reported "member
access on non-struct type Maybe<i64>". Two features that are each individually correct,
composing into a method that quietly was not there. Nothing was ambiguous and nothing was
shadowed in a sense a reader would recognise — one lookup could not see the other
declaration, and the failure surfaced three layers away as a type error about the receiver.

Confirmed against the commit *before* receiver-keyed overloading landed, with only `map`
added to the prelude: identical failure. So this is the cross-module name-claiming bug
recorded under todo.md's Modules section, not a fault in that feature — which matters,
because the tempting reading was that overloading had broken UFCS.

**What did not change is as deliberate as what did.** The **import requirement** still
gates reachability: gathering every declaration of a name must not quietly become "every
`pub` function in the program is a method on values of its type", which is the design that
was explicitly declined when UFCS landed. An unimported module's same-named method stays
unreachable, and there is a test that fails if that stops being true.

Two decisions inside the new rule:

- **A file's own module wins a tie.** Two reachable candidates accepting one receiver is
  otherwise ambiguous, and the module doing the asking is the one whose intent is least in
  doubt — the same precedence the scope chain applies everywhere else. This is what lets a
  file declare its own `map` for `Maybe` over the prelude's, which is the soft-shadowing
  rule the prelude already promised for bindings.
- **A tie that survives that is reported, not broken.** Candidates are gathered from a map
  of modules, so "whichever came first" would not be stable between runs — a call whose
  meaning depends on iteration order is worse than one that refuses. The message names the
  modules and suggests a qualifier the reader can actually type
  (`` `dup.map(m, …)` ``), taken from the import's own namespace alias; a test asserts the
  suggestion, and it was checked by running it.

One thing this turned up on its own: the **entry module** takes bare keys like any other,
*except* when it declares a name the prelude also has — `declKeyIn` then qualifies it to
`::name`, a key nothing else produces. Skipping the empty module path while enumerating
candidates therefore hid a file's own declaration from its own call, which is how the
local-wins test first failed.

Also fixed here, and caused by the overloading work rather than found by it: **LSP
completion dropped every overloaded name.** `ufcsCompletions` enumerates module scopes and
type-asserted each symbol to `*ast.VarDeclStmt`, so a scope holding an `*ast.OverloadSet`
was skipped — `m.` stopped offering `map`, `flat_map`, `unwrap_or` and `unwrap_or_else` the
moment those became sets. It now offers each member and keeps the one the receiver can call,
so the name appears once, described by the overload that applies.

**Still open, and narrowed:** only the *method* form resolves this way. A **bare call**
(`map(m, f)`) still goes through the scope chain, where the prelude shadows an imported
module's name, and two modules exporting one name still collide on the bare key. The
receiver is available there too — it is argument 0, which is the premise of the UFCS desugar
— so extending the same gathering to `inferIdentifierCall` is the natural next step. The
key-level fix in todo.md is what settles the names with no receiver at all.

### 08/04/26
**`std.maybe` folded into the prelude and deleted.** `map`, `flat_map` and `filter` for
`Maybe` moved beside the `Result` ones; the standard library is a single module again.

The split existed for one reason, and receiver-keyed overloading removed it the day before:
a name could be declared only once per module, so `Maybe` and `Result` could not both have a
`map` written in one place. `map` and `flat_map` are now overload sets exactly as
`unwrap_or`/`unwrap_or_else` already were. `filter` stays `Maybe`-only and single — rejecting
an `Ok` would have to invent the `e` to fail with, which only the caller can supply, and that
is `ok_or`'s job.

The user-visible half: the combinators need **no import at all** now, so the shipped-prelude
tests dropped their `import std.maybe`. Comments across the checker, typechecker and LSP that
described the split as current structure were corrected; the dated entries in this file are
left as the record of what was true when written.

### 08/03/26
**Receiver-keyed overloading: one module may declare a name several times, if the
declarations differ in what they take as `self`.** The prelude now declares `unwrap_or` and
`unwrap_or_else` twice each — once for `Maybe<t>`, once for `Result<t,e>` — so the two types
share a vocabulary instead of the second one getting a name it did not need.

This is the **declaration-side half of UFCS**, and only makes sense read against it. UFCS
(earlier today) made `m.map(f)` dispatch on the receiver's type, which settled *call* sites;
what it did not touch is that a second `let map` in one module was a redeclaration error. So
the two halves of "`Maybe` and `Result` both want `map`" had opposite answers: reachable
from a call, unwritable in a module. That is the whole reason the standard library split
`maybe.map` from `result.map` into separate modules, and the reason putting either in the
prelude claimed the name `map` for one type forever.

**The rule is the receiver's *head*** — the type constructor with its arguments dropped
(`types.HeadName`, one definition, shared with the backend's symbol mangling). `Maybe<t>`
beside `Result<t,e>` is two heads and is admitted; `Maybe<i64>` beside `Maybe<string>` is one
head twice and is refused **where it is written**. That refusal is the design decision worth
recording: two members that can both match a receiver need a specificity ordering to rank,
and the language has none — so the choice was between inventing one and forbidding the
overlap. Forbidding it makes the error a fixed thing in one place, with the clashing receiver
named, instead of an ambiguity reported at every call site that the author cannot resolve
without changing a declaration anyway. Relaxing it later means *adding* an ordering, not
reinterpreting anything. A bare type variable (`self: t`) has no head at all — it accepts
every receiver — so it can never be one candidate among several, which is the case the head
rule has to reject to stay coherent rather than a case it happens to miss.

**Resolution is in one place because the desugar had already earned it.** UFCS rewrites
`m.f(x)` to `f(m, x)` before anything downstream runs, so the receiver is argument 0 whichever
way the call was written, and one predicate (`receiverAccepts` — `unifyGenericTarget` again,
the same one trait dispatch and UFCS use) serves both spellings. The UFCS rung still has to
resolve *before* desugaring, since it is what decides whether `m.f` is a method call at all:
asking an arbitrary member would answer "no method" for a receiver a different member accepts.

**Where the cost actually landed: four passes that resolved a callee by name.** Ownership,
use-after-move, purity and the backend each looked a callee up in the symbol table to read its
parameter modes, and that question has no answer for an overloaded name. Two ways to fix it,
and the tempting one is wrong — re-deriving dispatch in each pass, from a receiver type each
would have to recover, is three more chances to disagree with the front end about which
function a program calls. So the pass that *did* resolve it publishes the answer
(`typetable.TypeTable.SetCallee`), which is exactly what `MethodTable` already does for trait
dispatch, and each consumer reads it first and falls back to name lookup. **Only overloaded
calls are recorded**: filling it in for every call would be a second answer to a question the
symbol table already answers, and a second answer is a thing that can drift.

Two structural choices that make the omissions loud rather than silent:

- **A scope holds an `ast.OverloadSet` in place of the single declaration**, so every pass
  that type-asserts a looked-up symbol to `*VarDeclStmt` now *fails* instead of quietly taking
  a member. Picking one needs a receiver; a pass without one has no business picking, and a
  failed assertion sends it down its not-found path where the worst case is a missing feature.
  This is CLAUDE.md hazard 8's reasoning run in the other direction — make the gap visible on
  purpose.
- **An overloaded name is absent from `SymbolTable.Functions` entirely**, for the same reason.
  Leaving a member under the bare key would have made every existing reader silently correct
  for one receiver and silently wrong for the rest.

**It turned up a shipped miscompile in the same code path.** The backend wrote `funcParams`
under the module-qualified key a private declaration gets and read it back under the *bare*
name, so a private function's parameter list came back empty — and with no parameters to
consult, `paramIsByRef` is never asked and a `mut` argument is passed **by value where the
callee expects an address**. A private function taking `mut` and called from inside its own
module segfaults; the arity guard that might have caught it is skipped by the same empty list.
Fixed here because overloading made the keying load-bearing, and pinned by
`TestExec_PrivateMutParamPassedByReference`, which was confirmed to fail (exit -1, a signal)
against the unfixed backend rather than merely asserted to.

Also closed a gap the tests had: nothing analyzed the **shipped** `std/prelude.lyra` — every
prelude test built its own fixture — so the real file could have stopped compiling with the
suite still green. `TestPrelude_ShippedStdlibAnalyzes` runs it through the ordinary resolve.

What this does **not** fix is the neighbouring bug in todo.md's Modules section: a `pub` name
still claims the bare program-wide key, so `import std.maybe` still forbids the importer its
own `map`. The two do not compose — an overload set is confined to one module, and that
collision is cross-module by construction. What did change is the *motive*: splitting a module
to give two types the same method name is no longer a reason to, so that bug is now the only
one left.

### 08/03/26
**Shadowing a canonical type explains itself instead of reporting the answer as the
problem.** `?` on a user's own `data Maybe` said `` `?` operand must be a Result or Maybe,
got Maybe ``. The rule behind it is right — `std/prelude.lyra` marks its types
`@builtin(Maybe)`/`@builtin(Result)`, the marker confers the kind, and a same-named
*unmarked* declaration is therefore an ordinary type — so what changed is only the message.

It now distinguishes the two mistakes, because they want opposite fixes:

- **Same shape** (`None | Some(t)`) — the author re-declared the prelude's type, almost
  always without knowing it was already in scope: *"`?` works on the prelude's Maybe, and
  "Maybe" here is your own declaration at 1:6, not that one. Remove it to use the prelude's
  Maybe, or rename it if you meant a separate type."*
- **Different shape** (`Nothing | Just(t)`) — a genuinely different type wearing the name,
  which `?` was never going to accept; the shared name is what made that read as a
  contradiction: *"… a different type that happens to share the name. Rename it, or return
  the prelude's Maybe instead."*

An operand that is simply the wrong type keeps the original wording, which reads correctly
there ("got Foo").

**The advice the plan called for turned out to be wrong, and that is the useful part.** The
recorded suggestion was to say "mark it `@builtin(Maybe)` or rename it". Marking it is
`lyra-E017` — *duplicate `@builtin(Maybe)`* — because the prelude already claims the kind,
so that message would have walked the author straight into a second error. A program can
have exactly one canonical Maybe, and it is the prelude's. The shipped message never
mentions `@builtin`; both fixes it *does* offer are covered by tests that actually run them
(`TestCanonicalShadow_AdviceResolvesIt`), and a third pins why the tempting fix is refused
(`TestCanonicalShadow_MarkingTheShadowIsAnError`) so no future edit reintroduces it.

The other option on the table — letting a shadow *inherit* the kind it shadows — was
declined: `@builtin` exists to give the kind exactly one owner (claiming it twice is already
an error), and silently granting canonical identity to an unmarked same-named type
re-creates the ambiguity the marker was introduced to remove.

`ShadowedCanonical`/`ShapeMatchesCanonical` are stamped by `resolveCanonicalTypes` beside
`CanonicalKind`, not re-derived at the diagnostic site, for CLAUDE.md rule 4's reason — the
shape test has one home. One trap on the way: the stamp walks the statement list rather than
looking up `c.table.Types[kind]`, because a declaration shadowing a prelude name is
registered under a *qualified* key so the prelude keeps the bare one, and the lookup
therefore returns the prelude's declaration — precisely the one this is not about.

**`break` and `continue` no longer leak the pending temporaries of the statements they jump
out of.** `for { if ("a" ++ "b") == "ab" { break } }` leaked the concatenation — 18 bytes,
measured with LeakSanitizer on Linux both before and after. `lowerBreak`/`lowerContinue`
called `flushTemps()`, which releases only from `pendingBase` up, and the concat belongs to
the enclosing `if` statement's scope, whose flush the jump skips entirely.

**The jump cannot answer the question it needs to answer, which is what shapes the fix.**
It may only release a temporary that is live where it stands — an SSA value is defined in
one block, so it is available at the jump exactly when that block dominates the jump's
block. Release one that is not dominated and the taken path frees a value it never
produced: a double free, the failure direction that actually matters here (skipping one
merely leaks). And dominance is a property of the *whole* CFG, which does not exist yet
while the jump is being lowered — later blocks can still add edges, and an edge added later
can only *remove* dominators, so computing it early against a partial CFG could report a
dominance that is subsequently false. That is precisely the unsound direction.

So the jump records the obligation and `resolveExitReleases` settles it once the body is
complete, against a dominator tree that can no longer change (`dominators.go`, the standard
iterative Cooper/Harvey/Kennedy fixpoint — functions here have tens of blocks, so the simple
algorithm is the right trade against a Lengauer-Tarjan nobody would maintain). Deferring
works because llir keeps a block's `Insts` and its `Term` in separate fields and prints the
instructions first: appending to a sealed block lands *before* its jump. Every release
emitted is straight-line (a store and a call), so none needs a block of its own.

`loopCtx.tempBase` is the other half, and the one a too-eager fix gets wrong. Only
temporaries recorded at or above the loop's entry are the jump's to release; one below it
belongs to a statement enclosing the whole loop, whose flush still runs after the loop
exits, so releasing it at the jump as well would double-free. It is the temporary-side
mirror of `frameDepth`.

**What I could not do, stated plainly: I could not construct a program where the dominance
check actually rejects a temporary.** The obvious candidates do not reach it — a temporary
produced in a conditional sub-expression (an `&&`/`||` right operand) is released in its own
block by the existing statement flush, so it is no longer pending by the time a `break` in a
sibling branch is lowered, and a temporary belonging to an enclosing statement is produced
on a path the jump is nested inside, which dominates it. The check is therefore defensive
rather than demonstrated: it is cheap, it is computed once per function that jumps, and the
alternative to having it is relying on that structural argument holding for every lowering
shape added later, in the region this project already treats as carrying real double-free
risk. It stays, and this paragraph is here so the next person knows it has never been seen
to fire.

Coverage: `TestEmit_BreakReleasesPendingTemp` pins two release sites in the loop (one per
way out; one is the leak, and it fails with that message without the fix), five
`TestExec_ExitReleases` cases over break/continue/labeled-break plus the two negative
shapes — a temporary enclosing the loop and one after it, which a too-eager fix would
double-free — each under ASan, and `TestDomTree` over a hand-built diamond CFG, including
that an unknown block answers false (the direction that leaks rather than double-frees).

**A `?` no longer leaks a temporary produced by a sub-expression of its operand.**
`f(g())?`, where `g`'s owned result is consumed by a borrowing parameter: the temporary was
released on the success path and not on the propagating one. Measured both ways with
LeakSanitizer on Linux (macOS has no LSan) — **19 bytes in 1 allocation before, none
after**.

`lowerTryPropagate` held the *whole* pending list back from its return's flush. The reason
was sound: that flush releases each temporary in the block that produced it, and the
operand's block is the one before the branch, so a release emitted there would sit ahead of
the tag test and free the value on the **success** path too. Holding everything back avoided
that, at the cost of also suppressing temporaries that genuinely die on the propagating path.

The fix is that the propagating path releases them itself, into its own block —
`releaseTempsOnExit`, which differs from `flushStmtTemps` in the one way that matters: **it
does not truncate the pending list.** An early exit is one path out of a statement that still
has another, and that other path must still reach the statement's own flush. Truncating would
move the release rather than add one, leaking on every non-exiting path instead. The operand's
own temporary stays excluded, since its reference transfers into the rebuilt error.

**The residue is bounded by dominance, and that is what makes it stop here.** Only
temporaries produced in the exiting branch's *predecessor* are released — that block is known
to dominate the exit, so the value is live. One produced inside a conditional sub-expression
(an `&&` right operand, a match arm) is not dominated, and releasing it would touch a value
undefined on the path that branch did not take. Those still leak, which is the safe direction.

**A prediction this disproved, worth recording.** The old note said the general fix was to
give `pendingTemp` a *release* block rather than a production block, and that one change
would stop `?`, `break` and `continue` leaking alike. Checking `break` with LSan showed it
leaks too (18 bytes, confirmed) — but not in a way that idea reaches. A `?` propagates from
a block whose predecessor produced the temporaries, so block equality is enough. A `break`
sits at the end of a *branch*: the producing block dominates it without being its
predecessor, and widening the test to "release everything pending" is unsound, because a
temp produced in a sibling branch is undefined on the path the `break` takes — a double free
rather than a leak. Closing that one needs real dominance information, which the backend does
not compute today; it is now an open item saying so, instead of an estimate that was too
cheap.

**An array *element* may carry an allocation or `weak` modifier** — `[]shared Node`,
`[3]weak Observer`, `[16]stack Vec3`. Measured while closing the `Maybe<weak T>` item below,
which is where the gap surfaced: the two look like one problem and are not.

**A grammar change only; this repo needed no code at all.** `array_type`'s `element_type` was
`_non_allocated_type`, so a modifier had no way in — but every pass downstream was already
built for one. `firstAllocationMismatch` (`assignable.go`) recurses into array elements and
its comment names the case it exists for: "a `stack` element assigned into a `[N]shared`
slot". That rule shipped, and the syntax needed to reach it did not, so it sat unreachable
and untested until now. The backend was the same story — an element flavor flows through the
existing layout and ownership paths, and a `shared` element is pointer-sized — so `lyra-E018`
fires on `[2]stack Node` → `[2]shared Node`, and `[]shared Node`, `[N]weak T`, `for-in` over
managed elements, and the whole tree shape all worked on the first build.

*Why it mattered:* `kids: []shared Node` is the obvious spelling for a tree's children, so
the natural shape for the very object graph `weak` exists to support was unwritable. The
cycle work below had to use `kid: Maybe<shared Node>` for exactly this reason.

*The rule that shaped it:* exactly **one** modifier deep. `_element_type` is a `choice` of
`_non_allocated_type | allocated_type | weak_type` whose operand stays `_non_allocated_type`,
so `[]shared shared Node` is still an error, and the *other two* users of that rule
(`weak_type`'s inner, `allocated_type`'s type) are deliberately untouched — widening them
would make `shared weak T` and `weak shared T` writable, and `weak T` already means
"non-owning reference to a `shared T`". Two `:error` corpus tests pin that.

Cost: 8,237 → 8,240 states (+3, +0.04%), `parser.c` +5 KB — cheap because the element sits
after a closing `]`, leaving no prefix for the automaton to track. Coverage: six corpus tests
in `tree-sitter-lyra`, `TestAlloc_ArrayElement*` / `TestAlloc_DynamicArrayElement*` /
`TestAlloc_ArrayElementModifier_Ok` here, and five `TestExec_ArrayOfManagedElements` cases
ending in the full shape — `shared` children, a `weak` parent edge — run under ASan and on
Linux.

**A `weak` field is constructible — `Maybe<weak T>` — and the two things blocking it had
nothing to do with `weak`.** A cycle back-edge is the reason `weak` exists (refcounting
leaks cycles and there is no collector, ALLOCATION.md), and it could not be written: a
field must be initialized, there is no empty weak, so the edge has to be *optional*.

**Both premises in the todo item were wrong, which is the useful part.** It said the fix
needed a grammar change because `Maybe<weak Node>` "does not parse". It parses, and always
did — `parameterized_type`'s arguments are `$.type`, and `$.type` includes `weak_type`. The
claim was never tested, only reasoned from the neighbouring gap that *is* real (`[N]shared T`
mis-parses, because `array_type`'s element is `_non_allocated_type`). Checking cost one
`tree-sitter parse` and would have redirected the work at the start; the whole item was
filed against the wrong repo.

What actually blocked it was **two hazard-8 misses**, and neither is weak-specific — a
`shared` struct holding a plain `Maybe<i64>` failed identically, which is the tell that the
feature was never the variable:

- **`resolveForLayout` had no `ParameterizedType` case.** A generic instantiation used by
  value inside another type reached `SizeAndAlign` as a shape none of its cases match, so
  boxing the enclosing value failed with "cannot size a `shared Node` payload yet". The fix
  is to normalize through `resolveInstantiation` — the choke point `recordedType` and
  `resolveDataType` already use, and whose own comment predicted this: "adding a case to
  each would be a dozen places to keep in agreement". A `shared` instantiation
  short-circuits before the recursion, exactly as the `UnresolvedType` arm does, which is
  what keeps resolution finite on a recursive generic.
- **`resolveTypeIfKnown` had drifted from `resolveType` by exactly two composites** —
  `ParameterizedType` and `*LambdaType`, the argument-list pair every such switch forgets.
  It resolves the *return* annotation (`checkLambdaBody` uses it so an unknown name is not
  reported twice), so the symptom appeared only in return position: `-> Maybe<weak Node>`
  kept `Node` unresolved while the body's value resolved it, and the two spellings compared
  unequal — "return type mismatch: expected `Maybe<weak Node>`, got `Maybe<weak Node>`".
  `resolveType` had been fixed for this same pair earlier; its twin was not, and the file
  documenting that fix (`named_type_in_composite_test.go`) is now where both live.

That is the fifth and sixth instance of hazard 8, and the first where the drifted switch was
a *twin in the same file* rather than a copy in another package — so the rule's "check the
others in the same file" now has a companion: check the function it was copied from.
`resolveType`/`resolveTypeIfKnown` differ only in what they do at an unknown leaf and
duplicate the whole recursion for it; folding them into one walk is the durable fix and is
now an open item.

Coverage: five `TestExec_WeakOptionalField` cases — the two no-`weak` regressions (a generic
field, and a nested `Maybe<Maybe<i64>>` so resolution has to recurse rather than stop at the
first normalization), a back-edge read through the field, a real parent/child cycle with the
`shared` edge one way and the `weak` edge back, and the dead-referent path where the field
is `Some` but the upgrade fails — that last one being why the field is `Maybe<weak T>` and
not a nullable weak: "no back-edge" and "the referent is gone" stay distinct branches. All
five run under ASan and on Linux (`./asan.sh`), where the older clang's typed pointers would
catch a payload built at the wrong width.

**Diagnostics render literals as source, not as Go structs.** A real message read
`expected array pattern, got IntegerLiteralExpr(0, Base: 10)..<= IntegerLiteralExpr(10,
Base: 10)`; it now reads `expected array pattern, got 0..<=10`.

`GetName` on an expression is a **source rendering** — parents compose them, so a match arm
builds `match <pattern> { <body> }` out of its children's — and the literals were the family
that returned their Go type and fields instead. Their parents dutifully composed *that*,
which is how a `RangePattern` came to hand someone the compiler's internals about their own
program. The fix is small; the interesting part is why it lasted.

**It reaches no golden file.** Every other rendering in the compiler is pinned by the
collector's golden tests, so drift is caught the next time anyone regenerates. `GetName`
is interpolated into diagnostics and nowhere else, which means the only reader who ever
sees it is a user, and the only reviewer is whoever happens to read a failing message
closely. `RangePattern` printing `0..9=` for `0..<=9` survived the same way (fixed 08/01).
There is now a test that no expression rendering contains `Expr` or `Pattern` — neither
substring can occur in the Lyra source these are meant to produce, so a node added later
fails there rather than in someone's terminal.

Fixed across the family rather than at the reported site, per the note the bug carried:
the literals, the postfix forms (`xs[0]`, `xs.len`, `xs.1`, `xs?`, `Show::show`), array
repeat (`[0; 3]`), and the pattern lists — a tuple or array pattern was formatting its
element slice with `%v`, printing Go's list of pointers.

Two things corrected in passing, both stale rather than wrong-by-design: a regex rendered
as `r/…/`, the spelling that stopped parsing on 07/29, and it rendered through `%q`, which
doubles every backslash — `r"\\d+"` for what the author wrote as `r"\d+"`. A regex is
mostly backslashes, so that one is worth the verbatim form even though `%q` is right for
strings.


### 08/03/26
**Trait and impl methods may be separated by newlines.** Statements gained a terminator on
07/31 and member lists did not, so `trait Ops { a: … ⏎ b: … }` failed — and failed in the
expensive way: "missing }" pointed at the end of the **first** signature, several lines above
anything a reader would suspect, then cascaded through the file. A misdirecting parse error
costs minutes rather than seconds, which is the whole reason this was worth fixing rather
than documenting.

`memberList` (`include/helpers.js`) is the shared shape, and two of its details are
decisions rather than mechanics. Its separator is `_statement_separator`, not a bare
`_newline`, so `;` works here too and the language keeps one answer to "what ends a thing on
its own line". And it keeps `commaSep1`'s structure rather than `statementList`'s, because
that is what makes the list non-empty — `trait C {}` stays a syntax error, preserved on
purpose rather than by accident.

A signature wrapped across lines is unaffected, and that is the property the design rests
on: the scanner only runs where tree-sitter marks the terminator valid, so a newline inside
an unfinished parameter list never reaches it. Verified rather than assumed.

Cost: 8,208 → 8,224 states (+0.2%), `parser.c` +21 KB.

**Struct declarations followed** (`struct_type_body`, `anonymous_struct_type`), which were
the last users of the comma-only shape. Held back from the first commit on purpose — they
were not what it set out to fix — and done as its own change once that one was in.

**A struct literal's fields still require commas, deliberately.** That list sits inside the
literal-vs-block ambiguity — `Point { … }` is contested between a struct literal and a name
followed by a block, which the postfix-head change touched earlier the same day — so a
newline separator there is a question about *that* conflict rather than the same one-word
change. There is a test pinning the current behaviour, so if it ever changes it changes on
purpose.

Total cost of both: 8,208 → 8,237 states (+0.35%), `parser.c` +32 KB.


### 08/03/26
**A struct literal is a postfix head.** `Node { n: 7 }.n`, `Node { n: 7 }.a()` and
`Grid { cells: […] }.cells[0]` parse; before this *no* postfix attached to a struct literal
— not a method call, not plain field access, not an index — while every other
value-producing expression already worked as one (`mk().a()`, `(Node { n: 7 }).a()`, a
literal in argument position). The grammar change is `named_struct_literal` joining
`_primary_expr`; the reasoning is in `tree-sitter-lyra`'s CLAUDE.md.

Two things worth keeping from doing it as a measured prototype rather than a guess.

**The cost was 26 states.** 8,182 → 8,208 (+0.3%), and +69 KB of `parser.c`. The estimate
going in was "unknown, possibly large" — juxtaposition had cost +19% states for less, and
`lambda_expr` once owned 91% of a 62,663-state parser, so the honest answer before measuring
was that it might not be affordable. It was, by two orders of magnitude, and the measurement
took one `generate`.

**Lyra needs no "no struct literal in an `if` header" rule.** Rust and Go both have one,
because the `{` of `if Node { n: 7 }.n > 0 {` cannot be told from the body's opening brace
with bounded lookahead. The plan here assumed Lyra would need the same restriction and said
so; GLR keeps both readings alive until a token decides, so the form simply works and is now
in the corpus. That is a real advantage of this grammar's architecture over theirs, and it
was found by trying rather than by reasoning — the prediction was wrong in the direction of
caution.

Found while writing `own`-receiver tests, where it read as a puzzling test failure.

### 08/03/26
**`own` on a trait method's parameter — and on its receiver — is supported; lyra-E030 is
retired.** The restriction existed because the ownership pass analyzed no trait-method body,
so `take: (Self, own string) -> string` compiled to a heap-use-after-free (measured, 07/31).
Method bodies are analyzed per specialization as of earlier today, which is the condition
the restriction itself named for lifting.

**Deleting the check was the first ten minutes.** Two other passes could not resolve a
*method* callee, and both mattered:

- **Use-after-move went unchecked through a method call.** `resolveCallee` handled an
  identifier callee only, so a method call recorded no moves whatever its signature said —
  `h.take(msg)` followed by `println(msg)` drew nothing, while the free-function spelling of
  the same mistake was a clean lyra-E019. That is the diagnostic that makes `own` safe for
  the *caller*, so lifting E030 without it would have reintroduced the same use-after-free
  one layer up. It now resolves through the MethodTable, with the receiver as parameter 0.
- **The ownership pass did not transfer the receiver.** Its argument loop had carried the
  +1 offset from the start, but the receiver is not in `e.Arguments`, so an `own Self` method
  left the caller lending a value the callee had adopted. Both released it: an ASan
  heap-use-after-free inside `lyra_rc_release`, which is how it announced itself — the plain
  run of the same program exits 0 and prints the right answer.

That is the receiver-offset hazard for the **third and fourth time in one day** (purity's
`methodArgumentAt`, the UFCS desugar, then these two). The pattern is worth naming: every
pass that reads a callee's parameter modes has to know that a `.`-call's receiver is
parameter 0, and each has discovered it separately, late, and with a silent failure mode.
UFCS avoids it by rewriting the receiver into the argument list before any later pass runs,
which is the shape the others would benefit from.

Two limitations found while testing and left alone, both unrelated to `own`: a trait or impl
with several methods needs **commas** between them (a newline does not separate them), and a
struct literal cannot be a method receiver directly (`Node { n: 0 }.a()` does not parse).

### 08/03/26
**Generic trait-impl methods are monomorphized.** `impl Unwrap<t> for Maybe<t>` type-checked
and then died in the backend — `match on Maybe<t> not implemented yet` — because the body
was emitted once with `t` still abstract. This is the gap that decided UFCS's sequencing
earlier the same day: method ergonomics through traits needed this built, while UFCS rode
the free-function path that already monomorphized.

**It was worse than an unimplemented error, which is what the investigation turned up.**
Where a generic impl's body *did* lower — one that never touches the type variable — the
emitted symbol was keyed by `impl.Type` (`Box<t>`), so every instantiation shared one
function:

```llvm
define i64 @Box$Sized$size(%Box$i64 %self)
  call i64 @Box$Sized$size(%Box$i64 %4)
  call i64 @Box$Sized$size(%Box$boolean %6)   ; ← the same function
```

Apple clang accepts that and runs it: opaque pointers make the two function types
indistinguishable, which is precisely the class of invalid IR `asan.sh` and its
typed-pointer clang exist to catch. A silent miscompile, not a missing feature.

**The bindings existed the whole time.** `resolveTraitMethod` unifies the impl's target
against the receiver and keeps the result to check the impl's `where` bounds; it simply
never left the typechecker. They now travel on `typetable.Resolution`, and
`Resolution.SpecKey()` names the specialization for the three consumers that have to agree —
the emitted symbol, the method cache, and the ownership table. Those three disagreeing *was*
the bug, so they get one string.

Lowering is then the existing monomorphizer, not a second one: `pushTypeSubst` with the
bindings, and the body's types come out concrete through the two accessors every type
already funnels through. One wrinkle worth recording: method bodies are **deferred to a
queue** (a method calling another would otherwise be lowered re-entrantly, corrupting the
lowerer's per-function state), so a substitution pushed while declaring is long gone by the
time the body is lowered. The queue entry carries the bindings.

**Ownership was the larger half.** `pkg/analyzer/ownership` walks top-level declarations,
which impl methods are not — they hang off a `TraitImplStmt` — so **no method body had ever
been analyzed**, generic or not: a method holding a `string` emitted neither retains nor
releases. `driver.OwnershipByMethod` now holds one table per specialization, built by the
same `ownership.AnalyzeLambda` the generic-function path uses. Whether a value is
reference-counted is a property of the type *argument*, so `t = string` retains its returned
payload and `t = i64` does nothing, from the same source line and the same AST nodes — which
is why the tables cannot be merged. Verified under `LEAKS=1 ./asan.sh`, which is where a
wrong answer here shows up as a fault rather than as a number.

Two things deliberately not changed. A generic body calling a generic impl method
(`getOr<t>` calling `o.unwrap(d)`) is still refused with "type variable t has no concrete
type here" — substitutions are not composed, and the free-function analogue was already
refused identically; it is now in todo.md as one limitation rather than two. And
`traitMethodLambda` moved out of the backend onto `Resolution.Lambda`, because the ownership
pass needs the same synthesized function and two constructions of it would be two answers to
"what are this method's parameters".

### 08/03/26
**A negated literal and a plain one now have a common type.** `if c { -1 } else { 2 }` did
not compile. Neither did `match n { 0 => -1, _ => 2 }` or `[-1, 2]` — ordinary code, in
three constructs, rejected with a message comparing a type to itself: *then is integer
literal, else is integer literal*. The match form managed *i64 vs i64*.

The cause is that a negated literal is `untyped_signed_int` while a plain one is
`untyped_int`, and `branchCommonType` only knew two moves — equality, and assignability in
either direction. Neither untyped kind is assignable to the other, so it gave up, and both
print as "integer literal", which is why the diagnostic read as nonsense. The join is the
signed kind: a set containing a negative value cannot settle to an unsigned one. The result
stays *untyped*, so an annotation narrows it exactly as it would either operand, and the
range check still applies to whatever it narrows to.

Deliberately narrow: it joins two untyped integers and says nothing about concrete widths,
which remain a real disagreement worth reporting. Found by a boundary case in the tests for
the range-check fix below (`[-128, 127]`), which is a reminder that a test written for one
rule is a decent probe for its neighbours.

### 08/03/26
**Composite narrowing range-checks its literals.** `let t: (u8, u8) = (300, 1)` and
`let a: [2]u8 = [300, 1]` were both accepted, silently, putting 300 into a u8 slot — while
the scalar `let n: u8 = 300` was rejected. `checkIntegerLiteralRange` reads the *declared*
type, and for those the declared type is a tuple or an array rather than an integer, so it
returned immediately and nothing else looked.

The check now sits where the narrowing happens, in `propagateLiteralType`'s tuple and array
branches, which means it covers every context that narrows rather than only an annotated
`let`: a return body (`() -> (u8, u8) => (300, 1)`) and an argument position are now caught
too. It carries a guard keyed by literal node, because a leaf can be narrowed by more than
one context on the way down — a struct field holding a tuple is narrowed by the field's
declared type and again by the enclosing annotation — and one too-large literal is one
mistake however many times it is checked.

### 08/03/26
**An annotation now narrows a data constructor's untyped payload.** `let m: Maybe<u8> =
Some 7` was rejected — *cannot assign Maybe<i64> to Maybe<u8>*, against an annotation
sitting right there. Solving binds `t` from the payload, and to unify it promotes the
untyped 7 to its i64 default; the result was recorded as a settled `Maybe<i64>`, and the
annotation never got a say. The scalar, tuple and array spellings of the same narrowing all
worked, which is what made it read as a quirk of data types rather than as the general rule
failing in one place.

**The missing distinction is between a width the program determined and one the expression
guessed.** `Some(x)` where `x: i64` is a `Maybe<i64>` because the program says so;
`Some 7` is one only by defaulting. Both were recorded identically, so the second could not
be told from the first after the fact. Such nodes are now marked
(`markDefaultedConstruction`), their payload leaf is left untyped for a context to narrow,
and `stampDataConstruction` accepts them alongside the bare declarations it already
accepted. Everything else stays closed to the context, which is the half that keeps a real
mismatch from being overwritten.

**The line runs through the declared field, not the payload.** A field that is a type
variable takes its width from the substitution and may be deferred; a concrete one
(`Wrapped(u8)`) takes it from the declaration and must be narrowed immediately. The first
version of the fix deferred both — it type-checked cleanly, and announced itself in the
backend as `aggregate element type mismatch: cannot store i64 into double`. A type rule
whose failure surfaces two layers away is the argument for `fieldTakesWidthFromSolve`
existing as a named predicate rather than as an inline condition.

Two consequences worth noting. A narrowed literal is now **range-checked against what it
was narrowed to** — `Maybe<u8> = Some 300` is an error, where previously the question could
not arise because the annotation was rejected wholesale first, and where assignability
alone cannot help (after narrowing, a u8 payload holding 300 is assignable to u8 and
wrong). And a payload type mismatch now reports at the payload — "Some: cannot assign
integer literal to string" rather than "cannot assign Maybe<i64> to Maybe<string>" — which
is the same preference the surrounding code already had for reporting the precise site.

Found while writing UFCS tests, where it first read as a UFCS failure. The tuple and array
narrowing paths still skip the range check; that is now in todo.md's Known bugs with the
two cases that demonstrate it.

### 08/03/26
**UFCS: `m.unwrap_or(0)` is `unwrap_or(m, 0)`.** A free function opts in by naming its first
parameter `self`; everything else stays call-only, so adding a helper to a module cannot
change what `x.f()` means elsewhere in it. The rung sits in `inferMemberCall`, making the
ladder field → trait method → UFCS → builtin.

**It went before the trait work, and the reason is a measurement.** The 07/31 design
sequenced this after the open trait gaps. But a generic *free function* over `Maybe<t>`
monomorphizes and runs today, while a generic *trait impl's* method type-checks and then
dies in the backend — `match on Maybe<t> not implemented yet`, because an impl method's body
is not specialized per instantiation. So the trait route to `m.map(f)` needs monomorphization
built first, and the UFCS route needed nothing: it desugars to the call that already works.

**The whole feature is one rewrite.** A matching call becomes `f(receiver, args…)` in place —
receiver into `Arguments[0]`, callee into an ordinary identifier — so generic solving,
purity, ownership, captures and the backend all see a direct call and none of them learned
anything. The two spellings emit byte-identical IR, which is the test that says so.

That is not a shortcut, it is the defence against a silent bug. Purity indexes arguments
*positionally* against the declaration's parameters, so a receiver left outside `Arguments`
shifts every index by one and each callback is checked against the bound of the parameter to
its right — and a function-typed argument satisfies the wrong function-typed parameter
without complaint, so a declared `pure` bound would simply stop being enforced, reporting
nothing. Trait methods pay exactly that tax through `methodArgumentAt`. `applyDefaultArguments`
was already rewriting calls for the same reason, which is what made this feel like the
grain of the code rather than against it.

**What the build turned up that the plan had not:**

- **The unused-import warning became wrong.** A UFCS call never writes the module's name, so
  the syntactic check called the import that permitted it unused — advice that breaks the
  program. `TypeChecker.UFCSModules()` now carries the resolutions to it. The lesson is
  general: a syntactic "is this used" test is only as good as the ways a thing can be used,
  and adding a resolution path silently invalidates it.
- **The obvious multi-clause exclusion was order-dependent.** Refusing candidates with
  `len(LambdaClauses) > 0` looks safe and is not: `desugarClauses` *consumes* the clauses, so
  the same function would qualify after its declaration had been checked and not before. The
  test is on the declared parameter instead, and multi-clause functions are candidates like
  any other (their heads must bind plain names, so one can be `self`).
- **`structFieldsOf` in the LSP was passing a zero Location**, correct only while the server
  analysed one unnamed file at a time — which stopped being true when it started resolving
  the import graph the day before. It now passes the document's own path, which is what
  decides whose declaration a name means.

Two decisions the design had left open, both taken toward the conservative reading: an
**import is required** to reach another module method-style (so what a file may call depends
on its own import list, not on what an unrelated file imported), and an **`own` receiver is
refused** with an error naming the call form (so a move always looks like a call).

The standard library's `Maybe`/`Result` combinators now name their receiver `self`, so they
read both ways. Also fixed a stale note in `std/prelude.lyra` claiming no method lowers —
that stopped being true on 07/30; the real limit is *generic* impls.

### 08/02/26
**The language server analyzes a program, not a buffer.** `cmd/lyra-lsp` called
`driver.Analyze` on the single open document, and a single unit is not a smaller version of
the real thing — it is a *different program*. It has no prelude in it, so `Maybe`, `Some`,
`Ok` and every standard-library name were undefined **in the editor only**: the reported
symptom was `undefined tuple type "Some"` on `std/maybe.lyra`, a file `lyrac check` compiled
cleanly. A program's own modules were unresolved the same way.

The cause was a fork nobody had noticed: `modules.Resolve`, the search roots and `stdRoot`
lived in `cmd/lyrac`, and `stdRoot` had no other caller. Those moved to
`pkg/modules/roots.go` as `DefaultRoots`/`DefaultOptions`/`StdRoot`, so the two tools cannot
disagree about where the standard library is — the same "one definition, so two callers
cannot drift" rule the type-variable walk was consolidated under.

**The buffer is not the file**, which is what an editor adds to the compiler's problem.
`Options.Overlay` maps a path to in-memory source that wins over the disk, and — the half
that is easy to miss — makes an overlaid path count as *existing*, so an import of a file
that has never been saved resolves instead of being reported missing. Every open document
goes in, not just the one being analyzed.

**What resolving a program forced back out again.** With several files in one AST, a line
and column no longer identify a position: the prelude's line 40 would answer a request about
the user's line 40. So `diagnosticsFor` filters diagnostics by file before publishing (one
naming no file is kept — it is program-level and has nowhere else to go), and `docProgram`
narrows the stored AST to this document's own top-level statements, which is what every
position-based handler walks. Two handlers needed more than filtering, both because they
return locations rather than consume them: a definition resolving into another file is now
returned against *that* file's URI, and a rename whose declaration lives in another file is
declined outright rather than applied at those coordinates in this buffer — that one would
have been a silent corruption. The remaining single-file edges are in todo.md.

`docAnalysis` also gained the module scope of the file it holds. A file declaring
`module std.maybe` puts its declarations there, not in the unnamed entry scope every handler
was asking for, so a top-level position in such a file resolved against none of its own names.

### 08/02/26
**Shift-check elision, and `x <<= n` typed like `x = x << n`.** The two follow-ups the
bitwise work left open.

**`NoShiftOverflow`** joins `NoDivZero`/`NoOverflow` in the safety table. A constant count
was already folded away at lowering, so this is the variable case — a count refined by
`if n < 8`, or a bounded loop counter. The proof obligation is written to mirror the
emitted check rather than to approximate it: that check compares the count **unsigned**
against the width, so a negative count reads as an enormous one, which is why marking
requires *both* a lower bound of 0 and a finite upper below the width. Getting that half
wrong would elide a check a negative count needs.

**The compound-shift asymmetry came from one function doing two jobs.**
`checkAssignToBinding` resolved the assignment *target* — does the name exist, may it be
written, what type is it — and checked the *value* against that type, in one pass. For
every other operator that is right. For a shift it is not: the right operand is a count,
not a value in the target's domain, so `x <<= count32` on a `u8` demanded a conversion
that `x = x << count32` never asked for. Splitting out `resolveAssignTarget` lets the
shift path take the target rules and supply its own value rule.

One detail preserved through the split, because it is load-bearing and easy to lose: a
*rejected* target still hands back its type. Every caller checks the value regardless, so
that a refused assignment reports its own diagnostic without swallowing the errors inside
its right-hand side.

`isIntegerOperand` now strips a constrained newtype first — `newtype Mask = u8` is a u8
wearing a name, and masking one is exactly what such a type is for.

### 08/02/26
**The value-range pass tracks bitwise results.** `andI`/`orI`/`xorI`/`shlI`/`shrI` join
`addI`/`subI`/`mulI`, so a masked value carries its bound into the arithmetic that
follows: `(x & 0x0F) + 1` is now proved in range and drops its overflow trap, where
before the mask widened to ⊤ and the addition kept a check it could never need.

**The rule that does the work is the masking one, and it is stronger than the obvious
version.** For a mask `m >= 0`, `x & m` lies in `[0, m]` **whatever the sign of x** — the
result can only have bits that m has, so it is non-negative and no larger. Stating it as
"both operands non-negative" would have missed exactly the case worth having, a signed
value masked down to a small range. `|` and `~` do need both operands non-negative (their
bound is the all-ones ceiling over the wider one), and `&` with both sides possibly
negative has no useful bound at all (`-1 & -1 == -1`), so it widens.

**None of this goes through `checkArith`, and that is the substantive decision.** Those
operators do not trap on overflow, so there is no E020 to report and no `noOverflow` to
record — and `<<` **wraps**. Routing a too-wide shift through the arithmetic path would
have reported "this operation always overflows", which is simply false: the value is
defined, it just lost bits. For the same reason `shlI` *refuses* rather than clamps when
the mathematical product could leave the type — clamping would assert a range the wrapped
value need not be in, which is the unsound direction.

**Soundness is checked by exhaustive brute force, not by argument.** These intervals feed
trap elision, so an interval that is too narrow is a miscompile — the backend drops a
check the program needed — while one that is too wide only costs precision. That asymmetry
does not tolerate hand-picked examples, so `bitwise_interval_test.go` enumerates *every*
interval over a 4-bit type, both signednesses, every value in each, and every non-trapping
shift count, comparing the abstract answer against what the machine would really produce
(including the truncation `<<` performs). 18,496 interval pairs are proved for `u4 &`
alone. The first attempt tried this at i16 and did not finish — 65,536 values per interval
— which is why the exhaustive layer is small-width and the real widths are covered by
targeted boundary tests instead. The per-rule counter is an anti-vacuity guard: a rule
that always gave up would otherwise pass having checked nothing.

The ±∞ sentinels needed care throughout. A `u64` upper bound is `+∞` because its true
maximum does not fit an int64, so any rule that *computes* with a bound (the all-ones
ceiling, a shift count) refuses an infinite one rather than treating `MaxInt64` as a real
value — while `&`, which only needs to know a bound is non-negative, still profits from it.

### 08/02/26
**Bitwise and shift operators.** `& | ~ << >>`, prefix `~` for complement, and the five
compound assignments. A systems language aimed at games had no way to touch a bit, which
made every mask, flag set and packed field unwritable; the trait `binary_operator` list had
reserved `<< >> & | ^` for overloads since before any of them existed in expression
position.

**Xor is `~`, not `^`, and that is forced rather than stylistic.** `^` is already prefix
raw-pointer syntax (`^T`) and postfix deref (`ptr^`), so a binary `^` is ambiguous with a
deref in operand position — `ptr^ ^ mask` has no good reading. `~` was completely free, and
already reserved in the trait `prefix_operator` list. Odin, which this language borrows from
elsewhere (`%%`, the `rune` naming), spells xor `~` for its own reasons and gets the same
result. Complement is the same token in prefix position, exactly as `-` is both subtraction
and negation.

**Precedence is deliberately not C's**, and the tests check the grouping by what the program
*computes*, since a parse-tree assertion would not catch a lowering that ignored it. Bitwise
binds tighter than comparison, so `flags & MASK == 0` means `(flags & MASK) == 0` — in C it
means `flags & (MASK == 0)`, which is why C codebases parenthesise every masked comparison.
It binds looser than arithmetic (Python/Ruby, not Go, which ties `|`/`^` to `+`), and shifts
sit above addition as in Go. `&` > `~` > `|` matches everyone except Go.

**An out-of-range shift amount traps.** `shl`/`lshr`/`ashr` are undefined behavior when the
amount reaches the operand's width, so this is the same call div-by-zero already makes: the
alternative is whatever the target's shift hardware does (x86 masks, ARM saturates), which
is exactly the divergence the fixed-width primitives exist to rule out. The comparison is
*unsigned*, which catches a negative count in the same instruction — as a two's-complement
pattern, -1 is enormous — and it runs on the count **before** it is coerced to the shifted
width, because coercing first could truncate an out-of-range count into range and hide the
thing being checked. A compile-time constant already in range emits no check at all, which
covers `x << 3`.

**A shift's count is typed independently of the value being shifted.** It is a *distance*,
not a value in the left operand's domain, so `u8 << i64` is well-typed and the backend
narrows the count. Unifying them the way `+` does would demand a conversion that buys
nothing — the count is not stored anywhere, and the trap is what makes it safe. The compound
form `x <<= n` is stricter, because it routes through `checkAssignToBinding`; that asymmetry
is recorded in todo.md rather than papered over.

**Three `|` collisions, resolved two different ways.** `|` was already the struct-update
separator (`P { base | f: v }`) and the array-comprehension delimiter. The struct-update and
generator races are shift/reduce and take `conflicts:` entries. The comprehension needed
something else: `[ x in R | A | B ]` fits two *complete* parses — guard `A` with result `B`,
or no guard and the single result `A | B` — which is an ambiguity between finished trees,
the one thing `prec.dynamic` resolves and `conflicts:` does not. Getting it wrong was not a
parse error: every guarded comprehension silently became an unguarded one whose result was a
bitwise-or, and only the pre-existing corpus test caught it.

**Cost, measured before accepting it** (the grammar's CLAUDE.md says to run
`--report-states-for-rule -` before adding anything here): 6,606 → 8,182 states, `parser.c`
12.0 → 15.3 MB. The alternative of collapsing the three bitwise bands into Go's two saves
only **424 states (5%)**, so the distinct bands are nearly free and buy the conventional
`&` > `~` > `|` ordering — the bulk of the growth is having the operators at all. `>>` does
not break nested generics (`Maybe<Result<i64, string>>`): tree-sitter's lexer only considers
tokens valid in the current parse state, verified before and after.

The compound-assignment → binary-operator mapping moved onto `ast.MathAssignOp.BinaryOp`,
replacing a table in the backend. Adding an operator used to mean editing two lists nothing
checked against each other, and the typechecker needs the same mapping to carry the binary
operand rules onto a compound assignment.

**The value-range pass is untouched and that is the sound outcome, not an oversight.** Its
operator switch leaves an unrecognised operator's `ok` false, which falls back to the type's
full interval — so bitwise results are ⊤ rather than mistracked, and no trap elision can go
wrong on them. Tracking them properly (a mask is *the* idiom for producing a known-small
value) is in todo.md.

### 08/01/26
**One `..` notation, three sites.** The range notation appears in an expression (`0..<n`),
a match pattern (`0..<=9`) and a `newtype` constraint (`range(0..<=100)`). They were three
independent grammar rules that had drifted on four axes — whether the end operator was
required, whether either bound could be omitted, what an operand may be — plus a fifth:
the same two characters `<`/`=` were `range_end_operator` in two rules and
`less_than_comparator`/`equal_to_comparator` under a `comparator` field in the third.
`rangeBounds` in `tree-sitter-lyra/include/helpers.js` is now the one shape.

**Two of those axes are real and stayed parameters; the rest were drift.** The *operand*
must differ — a pattern needs a compile-time literal (exhaustiveness and the jump-ladder
lowering depend on it), a constraint a constant expression, an expression arbitrary runtime
values; unifying them would either let a match arm hold a call or break `for i in 0..<n`.
*Open-endedness* must differ too: `range(0..)` and the pattern `10..` are useful, while an
open-ended expression range needs the lazy iterator the language does not have. What is now
uniform: one node kind for the operator, and — where bounds may be omitted — at least one
required, so `range(..)` and a bare `..` pattern are unspellable.

**The defect worth the whole change was a silent default.** `range_pattern` made the end
operator optional, and every reader of `RangePattern.EndOperator` tests `== "<"`, so an
omitted one fell through to *inclusive*: `0..9` meant `0..<=9`. Not cosmetic — that extra
value is exactly the boundary the exhaustiveness checker and the emitted comparison would
disagree on. It is now `lyra-E032` at all three sites through one collector check, and the
suggestion is spliced from the source so it is right for every form the notation takes,
including open-start (`..9`) and stepped (`0..10:2`).

*Where each rule is enforced, and the line between them.* The operator is optional in the
grammar everywhere and required by the collector everywhere; a bare `..` is refused by the
grammar. **Enforce in the collector when the construct has a plausible intended meaning
that must be disambiguated** — `0..9` is what a Rust or Python programmer writes *meaning*
something, and a message naming both fixes beats a syntax error pointing at whichever token
failed to shift (the `lyra-E029` trade) — **and in the grammar when it has no meaning at
all.** Only one existing line in the whole tree used the ambiguous form, in a test whose
subject was rune-scrutinee rejection.

**Patterns gained open bounds, and exhaustiveness got better for it.** `..<0`, `0..<=9`,
`10..` with no wildcard now compiles: `armIntInterval` reads an absent bound as the
scrutinee type's own limit, which is what writing it means. The backend *omits* the
comparison for an open side rather than emitting one against the type's limit — on an
unsigned scrutinee `x >= 0` is not merely redundant but the always-true compare the range
analysis reports as `lyra-W011` elsewhere. Ten exec cases cover it, weighted toward the arm
*not* taken and the boundary values, because dropping the wrong side of a two-sided test
still passes for one input.

**The two step spellings now answer to one rule.** An expression's `:step` and a newtype's
`step()` are deliberately separate — the constraint composes with `precision()` and the
newtype's domain, the expression drives a loop counter — but they did not agree on what a
legal step *is*: the expression form was checked for numeric type-compatibility and nothing
else, and the constraint form was collected and validated by nothing at all.
`types.InvalidStepReason` is the shared rule (`lyra-E033`): zero never advances, and a
fractional step cannot be represented over an integer domain. Type compatibility does not
subsume it — `0..<10:0` type-checks perfectly and is a loop that cannot terminate. A
negative step is deliberately *not* judged there: which direction a range runs is unsettled,
and inventing an answer inside a well-formedness check is how the two would drift apart
again. Still open: a step constraint is not enforced against values at run time, so
`step(0.25)` documents and validates but does not yet reject 0.3.

**`RangePattern.GetName` printed the operator after the bound it qualifies** — `0..9=` for
`0..<=9`. It reaches users (diagnostics interpolate it) but no golden file, which is how it
survived. Fixing it surfaced a larger one in the same messages, left alone and noted in
todo.md: a literal renders as `IntegerLiteralExpr(0, Base: 10)`, so a real diagnostic reads
"expected array pattern, got IntegerLiteralExpr(0, Base: 10)..<=IntegerLiteralExpr(10, Base:
10)".

One thing this cost that is worth writing down: **a recovered parse is not an absent
bound.** Where the grammar requires a bound, tree-sitter inserts one to keep going —
`range(..)` yields a zero-width `decimal_int` on the `)` — and a nil check reads that as a
bound of value zero. `collector_ctx.RangeBound` tests missing-or-empty, and the pre-existing
"range constraint must have a start or end" check only kept working because of it.

**Types and traits have per-module identity.** Two modules can each declare a private
`Point`, and a module declaring its own `Maybe` no longer takes the prelude's away from
everybody else. Both were one missing piece, and `todo.md` had said so: their namespace was
program-wide *by construction*, because `SymbolTable.Types` was keyed by bare name.

**Nothing new was invented — types were made to follow the rule bindings already had.**
`declKey` (the old `functionKey`) gives a declaration its own name when it is `pub` or in
the entry module, and `<module>::<name>` when it is private, or when it takes a prelude
name whatever its visibility — so the prelude keeps the bare key for every module that did
not shadow it. `FunctionKey` and `TypeKey` are now two names for that one function rather
than two rules that happen to agree, which is hazard 4's whole point: they would have to
agree anyway, since "whose declaration is this" does not depend on the kind of declaration.
`noteShadowed` consequently does nothing but record the warning; withdrawing the prelude's
entry was only ever a way to make one `Maybe` fit in a namespace that had room for one.

**The reachable symptom was a diagnostic about a declaration you had never seen.** A module
that never mentioned `Maybe` lost the canonical one the moment *another* module shadowed
it, and its `?` reported `` `?` operand must be a Result or Maybe, got Maybe ``. `todo.md`
called that message indefensible; it was the namespace, not the message. What is left is
the same message in the module that actually did shadow — poor, but about something its
author did (todo.md, Pit of Success #1).

**Three things had to move with the key, and each was a separate way to be wrong.**
Registration writes the module scope *before* computing the key (the key is read out of
that scope, and unlike functions — registered in `Finish`, after every file — types
register mid-walk, so there is no later point at which it is populated) and no longer
defines into the global scope at all: publication is `exportToGlobal`, the same path a
`pub` binding takes, which is what stops a private type competing for a program-wide name.
The typechecker's `resolvedTypes` cache was keyed by bare name, which would have let the
first module to mention a type answer for every other one — precisely the hazard that had
already kept the *visibility* check out of that cache. And the backend's `structTypes`
registry needed the same key.

**The backend could not thread a location, so it carries one.** `funcKey(name, loc)` works
because a call site is a node; a resolved `NamedStructType` reaching `lowerType` is just a
name, and `lowerType` is reached from nearly every expression. The lowerer therefore holds
`currentLoc`, set per type declaration/definition and inside `declareFunctionAs` /
`defineFunctionInto`. **Those two shared bodies, not `declareFunction`/`defineFunction`** —
a trait method and a generic specialization lower through them too, and putting it on the
outer pair left those resolving names against whichever module was lowered last. A location
rather than a module path so the backend deals in the same currency as everything else and
the symbol table keeps sole authority over file → module.

**Measured, not predicted:** two same-named private structs emitted a single `%Point = type
{ i64, i64, i64 }` — the union of both field lists — which clang rejected as a redefinition.
Loud, but at clang rather than as a Lyra diagnostic. A generic instantiation's symbol
(`Box$i64`) is derived from the bare name and is a separate path, so it would still have
collided after the plain-declaration fix; it is qualified too.

**Privacy for types became structural, and took the message down with it.** A private
declaration simply is not found from another module, exactly as a private binding is not in
the global scope. That is the right mechanism and, alone, the wrong message: "unknown type"
reads as a typo for a name plainly visible in another file. `lyra-E028` is recovered on the
not-found path (`reportPrivateType`, via the new `DeclaringModulesOf`, which is the exact
form of the question `ModuleOf` answers approximately).

**One bug this surfaced immediately, and it was the old one wearing a new hat.** `impl Size
for Point` in module one reported *its own* `Point` as "private to module two". `visibilityOf`
found a declaration by name through `DeclaringModule` — last-writer-wins — so with two
private `Point`s it answered for whichever was collected last. That is the identical mistake
the bare-**call** path made before privacy became structural (asking whether *some*
declaration of that name was private, rather than whether *the one this reference resolved
to* was visible), and it has the same fix: `declVisibility` asks about the declaration in
hand. A namespace member asks `visibilityIn(imp.Path, …)`, about the module the import names.

**A map key is not a source name.** Two user-facing readers iterated `SymbolTable.Types` and
used the *key*: LSP completion would have offered `one::Point` as a type to type, and
`captures` would have recorded it as a declared name while missing the real one. Both read
`decl.Name` now. Worth stating as a rule (CLAUDE.md hazard 4) because the map is the obvious
place to enumerate declarations from and the keys look like names until one is qualified.

**Coverage.** `pkg/modules/type_identity_test.go` pins the three front-end cases;
`pkg/backend/llvm/llvm_type_identity_test.go` compiles and *runs* four two-module programs
whose same-named private types have deliberately different shapes and field orders, so a
collision cannot accidentally produce the right answer. The LSP tests are honest about being
smoke tests: the server analyses one document through `driver.Analyze`, which builds a unit
with no `Path`, so the collector sees module `""` and a qualified key never arises there
yet — reverting the fix still passes them. They become load-bearing when the server resolves
a real import graph. Whole suite green on macOS and on Linux (`./asan.sh ./...`), ASan clean.

**Still program-wide:** two modules exporting the same type name is an error, as it is for
two exported functions — a bare reference could mean either, so it is a genuine clash rather
than something privacy resolves.

**A written generic parameter list is authoritative.** `let mismatch<t> = (a: u) -> u => a`
compiled and ran: it declares `t`, is generic in `u`, and nothing reconciled the two. The
list is now checked in both directions — a signature variable absent from a written list is
`lyra-E031`, a declared parameter the signature never mentions is `lyra-W013`
(`checker/generic_params.go`, before typechecking, AST-only).

**The list stays optional, and that is not a compromise.** Type variables are *lexical* — a
lowercase type name is a variable wherever it appears — so `let unbox = (b: Box<t>, fb: t)
-> t` is generic with no list and stays legal. That much follows from the lexical rule.
What never followed from it is the list being unchecked *when written*, which is the only
thing that changed. This was option (b) of three: (a) warn on both mismatches, (c) require
the list outright. (c) is the least ML-ish and buys little over (b).

**Why an error rather than the cheaper warning.** Both catch the typo, and the typo is the
motivating case — a misspelled lowercase type name does not fail, it silently becomes a
*new* type variable, so the function turns generic in something its author never meant and
the diagnostic (if any) lands at a call site or in the backend. That is how the prelude's
`ok`/`err` shipped without their `<t, e>` and drew nothing. But only an error settles the
part that outlives the typo: the list is the only place a **bound** can be written
(`<t: Show>`), so a list that need not agree with its signature means a constraint can
silently constrain nothing. An unenforced bound and a bound on a variable nothing solves
are indistinguishable from outside, and only the first stops being a problem when bound
enforcement lands — which is the argument for doing this *before* that, not after. The
warning half says so explicitly when the unused parameter carries a bound.

**The `where` half is enforced in the collector, and had to be.**
`Collector.MergeWhereConstraints` merges `where u: Show` into the matching list entry and
*silently discarded* a name matching nothing — so a bound on an undeclared variable was
already gone by the time there was an AST to check. Reported at the point the name still
exists, under the same code. This also covers trait declarations, which share the merge.

**Three copies of "which type variables does this signature mention?" became one.** The
pass needed that walk, and adding a third switch was the one thing todo.md asked whoever
took this not to do: the existing two — the typechecker's `collectTypeVars` and the
backend's `mentionsTypeVar` — had already drifted, the backend's missing
`ParameterizedType`, which is the 07/30 build failure in this log. Both now delegate to
`types.CollectTypeVars` (`pkg/types/typevars.go`); `MentionsTypeVar` is defined *over* it
rather than as its own short-circuiting switch, trading a map allocation for the guarantee
that a case added in one place cannot be missing in another. Taking the union turned up two
composites **neither** copy had: `AnonymousStructType` (structural — `(p: { v: t }) -> t`
writes its field types out in the signature) and `RangeType`. Nominal types are
deliberately *not* walked, and the unified walker says why: a `struct Box<t>` binds its own
`t`, so descending into it from a signature that merely mentions `Box<i64>` would report a
use and make every function touching a generic type spuriously generic.

Scope: bindings only. Type declarations, traits and impls have the same unreconciled list
and are tracked in todo.md — a type declaration's arity is at least load-bearing at
instantiation, so a mismatch there tends to surface as an arity error rather than silence.

The full suite, `std/prelude.lyra` and `std/maybe.lyra` reconcile with no new diagnostic —
the prelude's lists were corrected when `ok`/`err` were fixed, so this pass confirms them
rather than finding them. Verified non-vacuous the way this project verifies things: with
`CheckGenericParams` stubbed to return nil, 13 of its tests fail.

**`?` lowers.** The language's primary error-propagation operator type-checked — including
the enclosing-return kind and error-type checks — and then failed the build with
`expression lowering not implemented for *ast.TryExpr`, so no program could actually use
it. `pkg/backend/llvm/try.go` closes that. Verified end to end against the real
`std/prelude.lyra` (not just the single-file test harness): Result and Maybe, success and
propagating paths, through `lyrac build`.

`x?` is a `match` in disguise and lowers as one — tag test, unwrap on success, propagate on
failure. **The one thing that is not a plain match is the error arm**, and it is the whole
reason this needed a lowering of its own rather than a desugaring: the propagated value has
a *different LLVM type* from the operand. `?` on a `Result<i64, string>` inside a
`-> Result<bool, string>` function cannot forward the operand's union, because those are
two distinct monomorphizations, so the error payload is extracted and a fresh `Err` is
built around it at the enclosing type. That is what `retLyra` (the unlowered return type,
new on the lowerer) is for — the LLVM return type alone cannot say which constructor to
build or what the payload's Lyra type is. `TestExec_TryRebuildsErrorAtEnclosingType` is
pointed at exactly this, since a skipped rebuild emits a type-confused union rather than a
wrong answer.

**The bug found on the way there was in the ownership pass, and it was the more serious
half.** `pkg/analyzer/ownership` had no `*ast.TryExpr` case at all, so a `?` operand was
never visited — and that package's own doc is explicit that skipping a node is *not* the
leak-safe direction the rest of its bias assumes: a missed retain at an owning position
dangles rather than leaks. This was reachable without any of the lowering above: in
`parse(name)?` the operand's own sub-expressions went unannotated, so a managed value
inside one missed its retain. The case added mirrors `MatchExpr`/`MemberExpr` — the operand
is borrowed like a scrutinee, and the payload read out of it is duplicated in an owning
position, never moved.

**Whether the propagated payload is duplicated or moved is decided by how the operand's own
reference is disposed of**, and it has to be, because both mistakes are real bugs in
opposite directions. A *borrowed* operand (a binding) still owns its copy and will release
it — at scope exit, or via the `releaseAllManagedFrames` the propagating return itself runs
— so the rebuilt error needs a reference of its own. An owned *temporary* is not released
on that path, so its reference is what the error carries away and a dup would leak it. That
distinction was not taken on faith: inverting it (always transfer) makes
`TestExec_TryBorrowedOperand` print fifteen NUL bytes out of freed memory and
`TestASan_TryManagedPayload` report a fault, which is the check that these tests are worth
having — this suite has passed vacuously before.

**The propagating return deliberately does not flush the enclosing statement's
temporaries.** `emitReturn` releases each pending temp *in the block that produced it*, and
the operand's producing block is the one before the branch — so flushing from the error
block would place a release ahead of that block's own terminator, freeing the operand ahead
of the tag test that reads it, on the **success** path as well. Raising `pendingBase` leaves
the release where it belongs (the enclosing statement's flush, on the success path). The
residue is any temp produced by a *sub*-expression of the operand, which leaks on the
propagating path — the same conservative bias break/continue take, and tracked in todo.md
with the fix that would serve all three: a release block on `pendingTemp` rather than a
production block.

`buildDataValue` (aggregates.go) is new and shared: the write-side mirror of
`extractDataPayload`, and now the one place DATA_LAYOUT.md's `{ tag, payload-blob }`
encoding is *written*. `lowerDataConstruction` reaches it with lowered argument
expressions, `?` with values extracted from another instantiation's union — two callers
from opposite directions, which is precisely the shape that drifts if each keeps its own
copy of the layout.

`TestConservation_TryReleasesEnclosingScope` covers the other direction ASan cannot see: a
managed binding allocated *before* the `?` must be released on the propagating path too,
since `?` leaves every enclosing scope at once exactly as `return` does. A leak on one edge
of a branch is invisible to a count of allocations against releases.

### 07/31/26
**Return-type inference for a function written without `-> T`.** `let sum = ((a, b): (i64,
i64)) => a + b` now builds. It type-checked before and then failed the *build* with
"function needs a return type annotation" — the same front-end-accepts-what-the-backend-
refuses split that default params, multi-clause functions and destructuring parameters
each had, and the last one on the list from the higher-order-readability discussion.

The fix is an elaboration, not a new pass: `inferLambdaReturnType` writes the body's type
**onto the AST node**, exactly as `contextual_lambda.go` fills a lambda literal's missing
annotations. Everything downstream reads `ast.LambdaExpr.ReturnType`, so filling the blank
once means ownership, captures and the backend need no notion of an un-annotated function.

**Scope, chosen rather than stumbled into.** The body's *value* is the return type. A body
containing an explicit `return` is refused with a diagnostic asking for an annotation,
because inferring it means joining several candidates, and what happens when they disagree
— or when one arm diverges through `panic` — is a design question that deserves its own
decision rather than whatever a first implementation happens to do. The refusal is still an
improvement on what it replaces: a front-end diagnostic naming the fix, where there was a
backend error naming an internal requirement.

Recursion mostly works and does so for a reason worth knowing: `fact` infers fine because
`if n == 0 { 1 } else { … }` takes its type from the first arm, so the recursive call is
never consulted. When the recursive branch does come first, inference cannot finish and
says so — the cycle guard added earlier the same day is what makes that a diagnostic
instead of a stack overflow.

**The interesting interaction was the entry point.** `let main = () => { 0 }` is a
*documented* spelling of a void entry point. Inference fills the blank from the trailing
`0`, so `main` became `i64` and the entry check rejected it — "must return u8 or void, got
i64" — breaking a program that compiled the day before, via a feature with nothing to do
with it. Caught by `TestBuild_Clean`, whose fixture is exactly that shape.

The resolution keeps the convention: only a *written* annotation decides, so
`ReturnTypeInferred` marks the filled-in case and the entry point discards it, resetting
the node to `void` rather than merely classifying it — otherwise the backend would build a
return value nobody reads. `-> u8` is still an exit code, `-> i64` is still the error it
always was, and an inferred *u8* is honored, since there the inference and the convention
agree. That flag is the only place in the compiler that needs to tell a written signature
from a derived one, which is the argument for it being one narrow flag rather than a
general "was this elaborated?" facility.

### 07/31/26
**Transparent `type` aliases.** `type Op = ((i64, i64)) -> i64`. The name and the type are
interchangeable — no conversion at the boundary, no identity of its own. Grammar in
`tree-sitter-lyra` 0741c3c; this side carries the collector and the three places that had
to learn about them.

The motivating case is a function type, which is where the language reads worst. A
callback parameter spelled out is `(g: ((i64, i64)) -> i64, p: (i64, i64)) -> i64`, and
the double parens are *not* removable — single parens would be a two-argument function, so
a single tuple parameter has no shorter form. `newtype` could not serve: it is nominal, so
it makes the value un-callable without unwrapping. Which is the argument for both
declarations existing rather than one flag: an alias removes repetition, a `newtype` adds
meaning at a boundary.

**The implementation is one decision — register the aliased type *itself* under the
alias's name** — after which most of the compiler needs no notion of aliases at all.
Three places did:

- **`resolveType` had to stop after one hop.** `type Point = Pt` registers
  `UnresolvedType{Pt}`, so returning `decl.Type` handed back a *name*, and assignability
  then rejected a real `Pt` with "cannot assign Pt to Point". It now resolves what the
  declaration holds, which walks alias chains and leaves everything else alone (a struct
  or data declaration lands on the switch's default and returns as-is).
- **A cycle guard came with it.** `type A = B; type B = A` would otherwise recurse until
  the stack ended — the type-level twin of the `inferExprType` guard added the same day,
  and worth noticing that the *second* piece of work in a row needed one. Any resolver
  that follows a user-controlled edge needs an in-progress set; the pattern should be
  reached for by default now rather than after the crash.
- **The backend had to skip the declaration and expand the name.** An alias holds the
  aliased type itself, so without an explicit `IsAlias` marker `type Point = Pt` would
  declare *and define* Pt's LLVM struct a second time under the name `Point`. `IsAlias` is
  on the AST node because `Type` genuinely cannot distinguish the two. And since the
  typechecker resolves types without rewriting the AST, an annotation still says `Op` when
  it reaches codegen, so `lookupNamedType` expands an alias there — the one place a named
  type is resolved.

Validation happens at the **declaration**, not the use: an alias nothing mentions is still
checked, so an unknown target or a cycle is reported even when unused. A declaration that
cannot mean anything should not need a use site to be told so.

One consequence accepted on purpose: the alias name is gone from diagnostics. A mismatch
on an `Op` parameter names the function type, not `Op`. That is correct for a transparent
alias — the name is a spelling, not an identity, and claiming otherwise in a message would
be claiming an identity the type does not have — but it is the thing to revisit if the
messages read badly.

Tests: `typechecker/tests/type_alias_test.go` (transparency at each comparison site,
chains, the cycle, declaration-site validation, and an explicit "is not nominal" case that
would fail if someone later made aliases nominal) and `llvm_type_alias_test.go`, including
an IR assertion that a struct alias emits exactly one type.

### 07/31/26
**Fixed: a definition cycle crashed the compiler, and with it the language server.**
`let f = f(1)`, or the mutual `let a = b(1); let b = a(1)`, sent inference around a loop
until the Go stack was exhausted. Not an error — a **process death**: `lyrac` printed a
runtime traceback instead of a diagnostic, and `lyra-lsp`, which runs the same
`driver.Analyze` on every keystroke, simply vanished. In an editor that reads as
completions and diagnostics going quiet with no explanation, and it fires exactly when a
half-written cycle exists, which is most of the time you are typing one.

The loop is `inferIdentifierCall` → `inferExprType(decl.Value)` → back to
`inferIdentifierCall`: a binding whose type must come from its initializer, whose
initializer names the binding. **The cache could not break it** — it is written *after* the
recursive call returns, so a cycle never finds an entry. Marking the node in-progress on
the way *in* is the whole fix, four lines in `inferExprType`.

Two things about where it went. It is in `inferExprType` rather than in the call path that
exposed it, because a cycle is a property of the graph and not of any one route through
it — that is the single entry point every recursion passes, so any shape is caught. And it
returns nil, "cannot be determined yet", which every caller already handles.

*How it was found is the part worth keeping.* The original repro needed a **syntax error**:
`{ let f = mk(1); u8(f(3)) }` before semicolons were legal, where error recovery produced
two call nodes that inferred through each other. That framing — "reachable from a malformed
AST" — made it look like an error-recovery problem and lower priority than it was. When the
semicolon change made that program valid, the repro stopped reproducing, and reducing it
from scratch produced `let f = f(1)`: two tokens, no syntax error, a plain typo. The bug
was always reachable from ordinary source; the first repro just disguised it.

Also fixed on the way past: the diagnostic reached for a nil type and rendered
`identifier "f" is not callable (type %!s(<nil>))` — a Go format verb in a user-facing
message. It now says the definition depends on itself and suggests breaking the cycle, and
a test asserts no diagnostic ever contains `%!`.

Tests: `typechecker/tests/definition_cycle_test.go` (the three cycle shapes, plus the two
cases a too-broad guard would break — a legitimate `let add5 = makeAdder(5)`, and a
genuinely non-callable binding keeping its own typed message) and
`driver/driver_test.go`, which asserts through `Analyze` because "always returns, for any
input" is the property the language server needs. Verified the tests catch it: with the
guard stashed, the test binary dies with `fatal error: stack overflow`.

### 07/31/26
**Fixed: a negative literal in a `match` pattern now parses.** `-1 => …` and
`-128..<=127 => …` never did. `_number_literal` carries no sign and both `literal_pattern`
and `range_pattern` were defined over it, so the `-` landed in an `ERROR` node.
Pre-existing; found because the statement-terminator work changed how the wreckage looked.

**Why it survived this long is the interesting part.** The error swallowed the *whole*
match, so the collector never built a match expression, the exhaustiveness check never ran,
and `TestTypeCheck_NumericMatch_I8_FullRange_Ok` — which asserts *no* errors on
`match x { -128..<=127 => "ok" }` — got none and passed. **Vacuously.** A test that asserts
an absence is satisfied by a parse failure, which is the failure mode to remember: an
"assert nothing went wrong" test cannot tell "it worked" from "it never ran". Better error
recovery under the new grammar made the check start running, and it correctly objected to
the `128..<=127` it could see, which is what surfaced this.

The fix is a signed form for both pattern rules, **aliased to `negation`** so the CST shape
is one the tree already contains: `collectRangePattern` reads `start`/`end` with
`CollectExpr`, which handles a `negation` with an `operand` field and would not know a new
node kind. Two details that cost a cycle each. It has to be a *named* rule that is then
aliased — `alias(seq(…), $.negation)` inline is not a node of its own, so its
`operator`/`operand` fields hoisted onto the enclosing `range_pattern` and displaced
`start`/`end`, leaving the collector with nothing. And the sign cannot live in the token:
`decimal_int` swallowing a `-` would lex `a-1` as `a` then `-1` instead of subtraction.

Two conflict declarations, both mirrors of entries already there for the unsigned case
(`[expression, _signed_number_literal]` replacing the old `[expression, literal_pattern]`,
which now warns as unnecessary, and `[_math_operand, _negated_number_literal]`). The
grammar's own comments flag this as the finely balanced region, so `0 - 200` still parsing
as subtraction — not as `0` plus a dangling `negation(-200)` — was checked explicitly.

Tests are deliberately of the kind that cannot go vacuous: exec cases in
`llvm_match_test.go` where a dropped sign is a wrong exit code (`-5` must take the
`-128..<=-1` arm, not `0..<=127`), and two typechecker tests that assert a diagnostic is
*produced* rather than absent.

### 07/31/26
**A line break now ends a statement, and `;` is the explicit form.** A `tree-sitter-lyra`
change; the reasoning lives in that repo's commit and `CLAUDE.md`. What matters on this
side is *why it was worth a breaking grammar change*, and what it cost here.

It started as an ergonomics question — whether to allow `;` so several statements could
share a line — and the premise turned out to be inverted. Several statements per line
**already worked**: `block` was `seq("{", repeat($.statement), "}")` with no separator at
all, and newlines were only `extras`. So `;` would have added no expressiveness. What the
missing separator *did* add was a silent misparse, since a line break meant nothing to the
parser and the parse was maximally greedy:

```
let b = a          let f = add3        let n = xs
-2                 (4)                 [1]
```

Each ran as one statement — `a - 2`, `add3(4)`, `xs[1]` — with no diagnostic. That is why
the change is "newlines become significant, `;` is the explicit form" rather than "`;` is
now allowed": optional `;` alone fixes nothing, because nobody writes a terminator on the
line they did not know needed one.

**Fallout here was mechanical and small.** About twenty test fixtures put two statements on
one line separated only by spaces (`{ n = n + 1  n }`), which is exactly what no longer
parses; they now use `;`, and read better for it. One `lyrac` golden moved: the syntax
error for `let x = = 5` went from column 7 to column 9, pointing at the *second* `=` — the
better answer, since `let x =` is fine.

**One test-fixture note worth keeping.** `std/` needed no changes at all, and neither did
any multi-line construct: the terminator is only offered where the grammar accepts one, so
a newline inside an unfinished expression never produces it. Match arms, argument lists and
multi-line `data` declarations are all untouched.

**And it exposed a pre-existing bug**, now in `todo.md`: a negative literal in a pattern
(`-1 => …`, `-128..<=127 => …`) has never parsed. Two numeric-match tests were passing
*vacuously* — the old parser wrapped the whole match in an `ERROR`, so the collector never
saw a match expression and the exhaustiveness check never ran. Better error recovery under
the new grammar makes the check run, and it correctly objects. Those two tests are red
until the pattern gap is fixed; that is the honest state, not a regression.

**Process note, and it cost real time:** A/B-ing the old and new parsers by stashing
`src/parser.c` repopulates Go's build cache with the *old* compiled parser, so the next
`go test` silently runs against it — presenting as "the semicolons I just added are syntax
errors." `go clean -cache` after **every** parser swap, including a temporary one done only
for diagnosis. This is hazard 1 in `CLAUDE.md`, walked into from the one direction the note
did not spell out.

### 07/31/26
**Fixed: the shared AST walker never descended into a tuple index, and `pure` was unsound
because of it.** `p.0` is a `*ast.TupleIndexExpr`, a different node from `p.x`
(`*ast.MemberExpr`), and `ast.walkExprChildren` had no case for it. So every pass built on
`WalkExpr` was blind to anything reached through a tuple index — and each consequence read
as a bug in the pass that suffered it, not in the walker:

- **`pure` accepted an impure call**: `pure () -> i64 => noisy().0` drew no diagnostic,
  while the identical program through a struct field (`noisy().x`) was correctly rejected
  with `lyra-E007` the whole time. A soundness hole in the effect system, reachable by
  typing two characters.
- **A closure capture was missed**, which is a *build failure*: a lambda whose only use of
  `p` was `p.0` got no environment slot and died in lowering with `unbound identifier
  "p"` — on a correct program, and only when no other use of `p` happened to be present.
- **Use-before-declaration missed `b.0`**, and both "never used" warnings (`lyra-W005`
  parameter, `lyra-W003` variable) fired on names that were plainly used.
- Ownership last-use lost precision, which is the one harmless case: a missed last use
  falls back to the scope-exit frame release, so it defers a free rather than double-freeing.

One `case *TupleIndexExpr` fixes all of them, and no existing test changed — nothing had
come to depend on the gap. Found while probing why higher-order signatures read badly,
which is the second time a readability question has surfaced a correctness bug.

**Fixed: `resolveType` left named types unresolved inside function types and inside a
parameterized type's arguments.** The symptom is assignability rejecting a type against
itself, because one side expanded the name and the other did not:

```
cannot assign (Pair(i64, i64)) -> i64 to (Pair) -> i64      // *types.LambdaType
cannot assign Box<Pt> to Box<Pt>                            // types.ParameterizedType
```

The static-array, dynamic-array, tuple and weak cases in that switch each carry a comment
saying this is precisely what they exist to prevent; these two composites had no case. The
`LambdaType` one only bit *through* a function type — a plain `p: Pair` parameter always
worked — so naming a type failed exactly where a signature is long enough to want the name,
which is why it went unnoticed. `ParameterizedType` bit a plain parameter too. The
`LambdaType` case returns a **copy**: it is the one type held by pointer, so resolving in
place would rewrite the declaration every other reference shares.

Both are the hazard now written up as rule 8 in `CLAUDE.md` — the same omission
`mentionsTypeVar` had, in a third switch. Tests:
`typechecker/tests/named_type_in_composite_test.go`,
`checker/tuple_index_use_test.go`, and an exec test for the capture failure in
`llvm_closure_test.go`.

### 07/31/26
**Destructuring parameters lower.** `let sum = ((a, b): (i64, i64)) -> i64 => a + b`,
`({ x, y }: Pt)`. Parsed, collected and type-checked since long before; the backend refused
them in two places ("destructuring parameters are not implemented yet"), which is the same
front-end-enforces-what-the-backend-can't-build gap default params and multi-clause functions
had. Like those, it was not a codegen project: a destructuring parameter is the **fourth
destructuring form**, and the machinery the other three drive (`patternMatcher` →
`aggPatternTest`/`aggPatternBind`) was already there. Routing it through the same helper is
the point — two implementations of "does this value match this pattern" would drift.

It is the **irrefutable** form, and that is *checked*, not assumed. A parameter has no failure
path — no `else`, no next arm, and a function cannot decline to be called — but the typechecker
happily admits a value-testing sub-pattern in one (`((1, b): (i64, i64))`, and
`(Just(v): Opt)`, which the grammar accepts outright). Both are now refused with a message
naming the fix, the same way `lowerDestructuringDecl` refuses `let Some(v) = m`.

The two parameter-binding loops became one, `bindParameters`, and that is what made the feature
reach every shape of function at once: a plain function, a generic specialization, a lifted
closure (its `ir.Param` slot 0 is the environment, so the Lyra parameters carry an offset), and
a **trait-impl method**, whose clause patterns *are* its parameters via `traitMethodLambda`.
The trait case needed a front-end change to match: `checkTraitImplMethodBody` bound only
identifier patterns, so `total = ({ x, y }) => x + y` reported `x` and `y` undefined. It now
walks the pattern against the trait signature's parameter type with the same
`walkDestructuredPattern` `withParamScope` uses — and the impl is the one place an *unannotated*
destructured parameter works, because the signature supplies the type a free function has to
write.

**Ownership follows the rule the other pattern forms already use:** a bound name is a
**borrow**, never framed, because it is a field copy out of a value someone else owns. For a
bare or `ref` parameter that owner is the caller; for an `own` one it is the callee, so the
*whole* incoming aggregate is framed for one release that `drop.go` walks into every managed
field. That is deliberately not one release per bound name — a pattern need not name them all,
and `({ age }: own Person)` must still free `name`. A field that escapes gets a retain for
free: a pattern name has no declaration inside the function, so it is never last-use-eligible.
Both directions are ASan-clean, and the refcount shape itself is pinned (exactly one release
for the aggregate; a retain for the escaping field), because "it exits with the right code" and
"it frees the right number of times" are different claims.

Two refusals, both stated rather than incidental. A **`mut`** parameter cannot be destructured:
its bindings would be copies, so a write could not reach the caller — which is the whole content
of `mut`, and lowering it would be a mutable borrow that silently is not one. **`ref` is
supported**, by loading the pointee and destructuring that: it is read-only, so copying the
fields out is unobservable, and a destructuring parameter asked for them by value anyway. The
load is the copy by-reference exists to avoid, which is an argument about cost, not correctness.
An **array**-pattern parameter still fails, with the same message `let [a, b] = arr` gives —
static-array patterns are unimplemented everywhere (`match` on one is refused too), so that is
not a gap in this feature.

Tests: `llvm_destructuring_param_test.go` — 16 exec cases across all four function shapes,
`shared`/`ref`/`own` parameters and managed fields, every one repeated under ASan, plus the two
refusals and the refcount-shape assertions.

### 07/31/26
**Default parameter values work.** `add(5)` against `(a: i64, b: i64 = 10)`. Like multi-clause
functions, they were already parsed, collected, and honoured by the arity check — which counts
required parameters and allows a call to omit the rest — so the only thing missing was that
the **call site never received them**. The backend saw a call shorter than the parameter list
and refused the function outright.

They are filled in the front end (`typechecker/default_args.go`): the declaration's default
expressions are appended for any trailing arguments the call omits, before arity is counted or
the generic path is taken. Everything downstream then sees a call that passes every argument
explicitly, so the defaults are type-checked against their parameters like any other argument
and the backend needed only to *stop refusing* — in two places, since specializations have
their own parameter-lowering loop.

Two decisions inside it. The appended expression is the **same AST node** as the declaration's
default rather than a copy, so two call sites that omit it share one node; that is sound for
everything keyed by node, since a default is evaluated against the parameter's declared type
and cannot vary by caller, and cloning would need a deep AST copy this compiler avoids
everywhere else. The case that would expose a problem — a heap-allocating default at several
call sites — is covered by an exec test and is ASan-clean.

And a defaulted parameter followed by an undefaulted one is now **rejected**. It used to be
silently accepted: the arity check counts required parameters without checking their *order*,
so `(a: i64 = 1, b: i64)` called as `f(5)` bound 5 to `a` and left `b` unfilled — a call the
programmer plainly did not mean, accepted without a word.

Still refused, and now for a stated reason rather than for want of lowering: a default on a
lambda used as a **value**. Defaults are filled from the callee's declaration and an indirect
call has none — a function type records that a parameter has a default, not what it is.

### 07/31/26
**Multi-clause functions lower.** They always parsed, collected and type-checked — only the
backend refused, with "multi-clause functions are not implemented yet". So this was never a
codegen project: a multi-clause function *is* a match on its parameters, and the match
machinery it needs (the if-else ladder, tuple destructuring, guards, the sealed fall-through)
was already there and tested. Verified before writing anything, by running the hand-written
equivalent: `match (n, a, b) { … }` compiled and returned fib(10) = 55.

So it is a **front-end desugaring**, in `checkLambdaBody` before the body is walked. It has to
be: the backend reads every type by AST-node identity, so a match synthesized *there* would
have no TypeTable entry for any of its nodes. Synthesized in the typechecker, it is typed like
any other match and the ownership, capture and lowering passes needed no changes at all.

Four details that are decisions rather than mechanics. A **one-parameter** function matches its
parameter directly rather than through a one-element tuple, so it reaches the scalar ladder.
The clauses are **consumed**, or `checkLambdaBody` would check every clause body a second time
and turn one mistake into two diagnostics. **Arity is checked in the desugaring**, with the
counts named, because left to the synthesized match it surfaces as a tuple-shape error about a
tuple nobody wrote. And **no clause matching traps** (exit 101, `lyra: match not exhaustive`)
rather than being undefined — the right semantics for a function-clause error, and what Erlang
and Elixir do.

**Generic multi-clause functions work too**, which needed a second, unrelated fix. The backend
had refused them twice — once for being multi-clause, once in `declareSpecialization` — and
with the body desugared, a third failure appeared: `data pattern on non-data value of type
Opt<i64>`. `resolveDataType` had no `ParameterizedType` case, so a data pattern *nested inside*
an aggregate pattern (`(Just(v), _)`) failed where a top-level one worked, because the
top-level path normalizes its scrutinee and the sub-pattern path reads the element type
straight off the tuple. Pre-existing and independent of clauses — the hand-written tuple match
fails identically, which is how the regression test is written.

### 07/31/26
**A lambda literal now takes its missing annotations from context.** It used to take
nothing: `(x) => x` reported `undefined symbol "x"` because an unannotated parameter never
reached `tc.paramTypes`, and `() => 7` was rejected against `() -> i64` because the body's
untyped leaf never learned the expected width. Only a fully annotated lambda worked, which
meant every call site of every lazy combinator had to restate types the signature already
gave — `maybe.map(m, (x) => x * 2)` was not writable.

**The fix is elaboration, not a second inference mode.** When a lambda literal meets an
expected function type, its blanks are filled *on the AST node* before its body is inferred,
so everything downstream sees the lambda the user would otherwise have had to write by hand:
`withParamScope` seeds the parameters because they now have types, `checkLambdaBody` checks
and width-propagates the body because there is now a declared return, and the backend — which
reads `ast.Parameter.Type` to lower a parameter — needed no change at all. One mechanism, both
halves of the bug. It only ever fills what was left blank, so an explicit annotation still
wins and can still be diagnosed as wrong.

Wired at the three sites that know what they want: an annotated binding (which had to go in
the *lambda-valued* branch of `checkVarDecl`, which returns before the general path), a
direct call's arguments, and a generic call's.

The generic case is the one with a real ordering problem, and it took three passes to get
right:

1. A bare lambda cannot be inferred until it knows what is expected, but `(t) -> u` is not
   concrete until the *other* arguments solve `t` — so `solveTypeVars` defers incomplete
   lambdas to a second pass. A **fully annotated** lambda is not deferred, since it can solve
   variables itself (`or_else(Nil, () -> i64 => 0)` binds `t` from the callback's return) and
   deferring it would lose that.
2. A type still mentioning a variable must **not** be planted: in `map(m, (x) => x * 2)`,
   `u` is solved from *this lambda's own body*, so writing `u` as the declared return leaves
   it unsolved forever — the symptom was "cannot convert u to u8" at the use site.
3. Which means the return type can only be filled *after* solving completes, in
   `inferGenericCall`, or the lambda reaches the backend as a value with no return type.

One existing diagnostic changed, deliberately. `apply((n: u8) => n, 0)` against
`(u8) -> string` used to report "cannot assign (u8) -> u8 to (u8) -> string", from inferring
the lambda in isolation and comparing signatures; it now reports "return type mismatch:
expected string, got u8" against the body. Both are true and the second points at the
expression that has to change — and it is the same mechanism that makes `apply(() => 7, 0)`
work at all, since a context that can supply a return type has to supply it before the body
is walked.

### 07/31/26
**Borrow modifiers on trait signatures — `ref` and `mut`, with `own` rejected.**
`bump: (mut Self) -> void` now writes through to the caller and `peek: (ref Self) -> i64`
borrows without copying. The grammar was never the gap, contrary to the entry this replaces:
`trait_method_signature` is an aliased `lambda_type` and its `parameter_type` has always
carried an optional `type_modifier`, so `(mut Self, own i64) -> void` parsed all along.
`Collector.parseParameterType` read only the `type` field, and `types.ParameterType` had no
field to put a borrow in — `Modifier` there is the `stack`/`shared` allocation flavor, a
different axis. It gained `Borrow`.

Four passes had to agree, and the interesting part is that three of them are only correct
*together*:

- **collector** reads the modifier; **`traitMethodLambda`** carries it onto the synthesized
  parameter, which is the line the old comment warned about ("or the call site and the body
  will disagree about who owns the receiver");
- **backend** passes the receiver and each argument by address when its parameter is a
  by-reference borrow (`methodOperand`), mirroring `lowerDirectCall` — it cannot share that
  loop, because a method call's receiver is not in `call.Arguments`, and that offset is the
  whole difference;
- **typechecker** applies the same `checkMutArgument`/allocation checks a free call gets, so
  a `mut` argument must be a mutable lvalue rather than a temporary whose writes vanish;
- **ownership** learned to read a method's modes at all — `resolveCallee` returns nil for a
  `.`-call, so every method argument previously fell to the conservative transfer. It now
  resolves the trait signature through the `MethodTable` (threaded into `Analyze`), again
  with the receiver at index 0 and arguments from 1.

**`own` is rejected (`lyra-E030`), and that is a measurement rather than caution.**
Implemented alongside the rest, `take: (Self, own string) -> string` compiled to a
heap-use-after-free — ASan report, read-after-free in the `print` of the returned value.
The cause is that `pkg/analyzer/ownership` **does not analyze trait-method bodies at all**,
so nothing records that a returned `own` parameter was transferred rather than dropped: the
backend dutifully drops it at scope exit and the caller uses the corpse. `ref`/`mut` are
immune by construction — a borrow is retained and released by nobody, so the pass needs to
know nothing about the method. Lifting the restriction means teaching ownership about method
bodies, which is its own slice; the diagnostic says so and names the modes that do work.

One class of bug worth remembering from building it: **rebuilding a `types.ParameterType`
field-by-field silently drops new fields.** Three sites did (`substituteSelf`, the
lambda→signature conversion, and `lambdaSignature`), and the symptom was a `mut` receiver
that parsed, type-checked, lowered, ran — and wrote to a copy. It was found by an exec test
asserting the caller's value changed, not by anything that could have been caught earlier.

### 07/31/26
**Trait-impl methods are effect-polymorphic, and a trait signature can bound a callback.**
The last conservative corner of the effect work: `methodEffects` charged a call through a
method's own parameter the full `AllEffects` taint, so a trait method taking a callback was
as poisoned as every function was before effect polymorphism landed, and the taint spread to
its callers. It now returns a base effect plus callback parameters exactly as `lambdaEffects`
does, and `methodCallEffect` charges each call site for the arguments it actually supplies.

A method's parameter *types* live only in the trait declaration — an impl binds patterns
(`show = (self) => …`), not typed parameters — so `collectMethodSignatures` maps each impl
method to its declared signature. That is also what makes a bound written in a trait
signature enforceable: `apply: (Self, pure () -> i64) -> i64` now binds every caller,
including impure ones, via `signatureBound`.

**The receiver offset is the trap in this path, and it is a silent one.** A trait signature
counts `Self` as parameter 0, but `x.foo(a)` puts the receiver *outside* `call.Arguments` —
so signature index i is `Arguments[i-1]` (`methodArgumentAt`). Reading `Arguments[i]` would
check every callback against the argument one place to its right and report nothing, because
two function-typed arguments type-check against each other's parameters perfectly well. The
regression test uses two callbacks in different positions, since a single-callback test
passes either way. This is the same hazard already written into the UFCS decision entry,
which is where it will surface next.

**What was *not* done, and why not partially:** borrow modifiers on trait signatures. The
grammar was never the gap — `trait_method_signature` is an aliased `lambda_type` and its
`parameter_type` has always taken `ref`/`mut`/`own`, so `(mut Self, own i64) -> void` parses
today. The collector drops it, and three passes would have to learn about method parameter
modes: the typechecker performs no `checkMutArgument` for method calls, the ownership pass
contains no reference to trait methods at all, and only the backend is close to ready.
Collecting the modifier without teaching ownership would have the backend pass a *pointer*
where the ownership pass still believes a copy was made — the borrowed-`string`
use-after-free shape from 07/30 with a different origin. It wants one vertical slice with
ASan over it; todo.md carries the corrected scope.

### 07/31/26
**The parser shrank 9×, and `src/parser.c` left Git LFS.** 62,663 states → 6,475; 116 MB →
12.8 MB. It started as a storage question — every grammar change cost 116 MB of LFS quota,
and `.git/lfs` was 2.5 GB across 17 revisions against a 1 GB allowance — but the storage was
the symptom.

`tree-sitter generate --report-states-for-rule -` attributed **57,026 of the 62,663 states to
`lambda_expr` alone (91%)**. The cause was seven independent `optional()` modifiers in
sequence: an LR automaton tracks every distinct prefix through such a chain (2^7 = 128 before
the parameter list), and because the GLR conflicts around `(` keep the lambda-parameter-list,
tuple and parenthesized-expression readings alive simultaneously, each prefix grew its own
family of states across the whole expression grammar. `LARGE_STATE_COUNT` agreed: 35% of
states, where a few percent is normal.

Worth recording what *didn't* work, since it is the obvious first move: ablating each of the
17 declared GLR conflicts one at a time. Every one failed to generate — they are each
load-bearing for a specific ambiguity, exactly as their comments claim. The conflicts are not
the problem; what the conflicts *multiply* is.

Three forms were measured before choosing:

| Form | States | `parser.c` |
|---|---|---|
| Seven ordered `optional()`s | 62,663 | 116 MB |
| Ordered, mutually-exclusive ones grouped (5 optionals) | 37,687 | 70 MB |
| `repeat(choice(…))` — order-free | 6,475 | 12.8 MB |

The 10× needs the third, and it costs modifier **order and repetition as parse errors** —
one corpus test out of 373. Those moved to the collector (`lyra-E029`,
`expressions/modifier_order.go`), which is a better home than a trade-off: a syntax error
could only point at whichever token failed to shift, where the collector names the offending
modifier and the canonical order. The semantic sibling — `pure` and `det` conflicting — was
already a checker diagnostic, so the rules now sit together. Field labels survive a
`repeat(choice(field(…)))`, so no collector read changed.

Consequences beyond size, all of them the point: `git-lfs` is no longer a prerequisite for
cloning (`setup.sh`/`setup.ps1` lost the skip-with-install-hint path, `README.md` the
prerequisite row, CI its `lfs: true`), `parser.c` is diffable in review, and a grammar change
costs a large text diff instead of an LFS revision. **A commit from before this still needs
git-lfs** — `asan.sh` keeps its pointer-file guard for exactly that, and `lyra-zed-ext`'s pin
reintroduces the requirement if it names an older commit.

### 07/31/26
**Effect polymorphism — the declared half: `f: pure () -> t`.** The inferred half (below)
decides a higher-order function's purity per call site from the argument, which is what makes
a standard library usable but leaves a signature unable to *promise* anything: `pure` on a
combinator claims only that its own body is clean. A parameter's **type** may now carry the
same `pure`/`det`/`noalloc` modifiers a lambda value does, and a parameter carrying one is no
longer polymorphic — what calling it can do is known from the signature, so the function is
pure **for every caller**.

- **Grammar** (`tree-sitter-lyra`): `lambda_type` takes the three modifiers as labelled
  fields, matching `lambda_expr` so a type and the value inhabiting it are written the same
  way. No new node kind — `pure_modifier` already existed — so no highlight query changed and
  `lyra-zed-ext` needed nothing.
- **Enforced at every call site**, not only inside `pure` functions
  (`checkDeclaredCallbackBounds`): the bound is a property of the callee's signature, so an
  impure program may not quietly hand an impure callback to a `pure`-declared slot.
- **The argument's *inferred* effect is what is compared, not its annotation.** Requiring the
  word `pure` on every lambda literal a program writes would cost more than the bound is
  worth, and inference is exactly what this pass has and the typechecker does not. That is
  also why `isAssignable` deliberately passes two function types differing only in bounds:
  a shape mismatch there would report "cannot assign `() -> i64` to `pure () -> i64`", which
  explains nothing, where the checker says "this argument mutates state outside itself".
  `TypesEqual` *does* distinguish them, so identity questions still see two types — and it
  has to, or `isAssignable`'s equality short-circuit would fire first and the annotation
  would be decorative.
- **Bounds compose one way.** A constrained parameter forwarded into a constrained slot is
  verified from its own declared type (a parameter has no body to inspect); an unconstrained
  one is *rejected*, since it promises nothing. A bound the compiler cannot check is not a
  bound. Propagating the requirement outward instead — inferring that a wrapper's parameter
  becomes bounded — is the obvious next step and is open in todo.md.

**The standard library deliberately does not use it.** A `pure` bound on `unwrap_or_else`
would forbid a fallback that logs, which is a legitimate thing to want from a lazy default,
and the inferred half already keeps pure callers pure without taking that choice away from
impure ones. The declared half is for APIs that genuinely require purity — something that
memoizes, reorders, or parallelizes a callback — where the restriction is the feature.

**Effect polymorphism over function-typed parameters — the inferred half.** A higher-order
function's effects are not a property of the function alone: what `unwrap_or_else(m, f)`
does depends entirely on `f`. The pass charged the *definition* for a call it could not
see — an unresolvable callee tainted `AllEffects` — so every combinator was maximally
impure and the taint spread to its callers. **No callback-taking function was callable from
`pure` code at all**, which is the entire std.maybe/std.result combinator layer, and
dropping the annotation did not help: it moved the error to every caller.

A function's stored effect is now its **base** (what its own body does) plus its **callback
parameters**, the function-typed ones it calls. A call site pays base ∪ the effects of the
arguments actually supplied for them, so one definition gives `unwrap_or_else(m, () -> i64
=> 0)` pure and an effectful callback impure. The callback set is part of the same fixpoint
as the effects, because finding a callback changes a caller's effect and finding an effect
can reveal a callback a round later.

Two consequences that are the point, not side effects:

- **An annotation constrains a function's own body.** `pure` on a higher-order function
  claims "contributes no effects of its own", not "no effect can occur through me" — the
  second is not the function's to promise while it cannot constrain its callback, which
  needs the declared half (`f: pure () -> t`) the grammar cannot spell. That is what finally
  let the prelude annotate `unwrap_or_else` and `ok_or_else` `pure noalloc`, with all of
  `std.maybe` alongside them; a caller passing an impure callback is still rejected, at the
  call site, and the diagnostic names the **argument** rather than the innocent callee.
- **A callback passed onward stays polymorphic.** `(f) => or_else(m, f)` is polymorphic in
  `f` too. Without that, a combinator built from another combinator — which is most of a
  standard library — would be exactly as poisoned as before.

One thing had to be fixed for any of it to be observable: **a namespace-qualified callee had
no resolution at all**, so *every* cross-module call from a `pure` function was reported
impure, and `maybe.map(…)` — the whole point of the namespaced-module split — could not be
called from pure code however pure it was. `resolveCallee` resolves the last segment against
the merged program's top-level functions, and only when the object segment names no binding,
mirroring the backend's `namespaceCallee`: with a local `math` in scope, `math.double` is a
field read, and resolving it elsewhere would attribute another body's effects to it.

Deliberately still conservative, all sound and all noted in todo.md: a callback reached
through a struct field, a call result or an array element; multi-clause lambdas, whose
per-clause patterns give no index to match an argument against; and trait-impl methods,
where `methodEffects` is unchanged.

**`never` and `panic(msg)`.** A program had no way to reach the trap machinery on purpose:
the four traps (overflow, divide by zero, bounds, match fallthrough) are all emitted on
conditions the compiler checks, and nothing in the builtin registry exposed one. So
`expect`-shaped functions — anything that has to produce a `t` from a case that has none —
could not be written in Lyra at all, in the standard library or out of it.

- **`types.NeverType`**, the bottom type, assignable to **everything** (`isAssignable`) and
  equal only to itself (`TypesEqual` — subtyping belongs in assignability, and making it
  *equal* to everything would make two unrelated types equal through it). That one rule is
  what puts a diverging expression in value position: `None => panic("…")` as a match arm
  satisfies the arm's `t` without inventing a value, and `branchCommonType` folds it away
  from either side because it already goes through `isAssignable`. No syntax spells it, so a
  user cannot annotate with it.
- **`panic(msg: string) -> never`**, resolved like `print`/`println` — by name, only after
  scope resolution misses, so a user binding of the same name shadows it and adding this
  cannot break a program that already had its own. The message is a runtime `string`, not a
  literal: an interpolated one ("negative index ${i}") is the case that makes a panic
  message worth writing, so the runtime `void @lyra_panic(i8*, i64)` takes the fat pointer
  rather than baking the text in like the other four.
- **EffectNone** — callable from `pure`, `det` and `noalloc`. Tagging it Output would have
  made `pure` mean "cannot panic *on purpose*" while `a + b` and `xs[i]` panic implicitly
  from inside the same function, to the same fd, with the same exit code. Purity is about
  what a function returns and mutates, not whether it terminates. Reasoning and the Koka
  counterpoint are in `checker/README.md`.

Two things fell out that were not the point. The backend needed **no control-flow work at
all** — `lowerPanicCall` seals its block with `unreachable`, and every phi incoming and
fall-through `br` was already guarded on `Term == nil` for `return`/`break`/`continue`. What
it *did* need was a guard at each site that **consumes** an operand's value, because those
dereference what they get back: `let x = panic(…)`, a reassignment, a call argument, a
numeric conversion, and an array element each crashed `lyrac` with a nil dereference — a Go
panic rather than the loud error the backend is supposed to produce. `diverged(v, block)`
(nil value *and* sealed block, since several lowerings produce no value while still
reaching the next statement) is the shared test.

And the purity checker turned out to consult **`builtinEffects` before scope**, the opposite
of the typechecker's order, so a user's own `print`/`panic` was classified by the builtin's
entry instead of by its body. Harmless-but-noisy for `print` (a pure one reported impure);
unsound the moment `panic` joined the table as EffectNone, since a user `panic` that mutates
would have been waved through as pure. Fixed at all three call sites. The name is not the
callee.

### 07/30/26
**A generic function whose type variable appears only inside a composite is now recognized
as generic.** `mentionsTypeVar` (`backend/llvm/functions.go`) — the predicate behind
`isGenericLambda` — recursed through arrays, tuples and `weak` but had **no case for
`ParameterizedType`**, so `is_some<t> = (m: Maybe<t>) -> bool` answered "not generic".
`forEachUserFunction` then stopped skipping it, the backend tried to emit it under its bare
name, and lowering died on `cannot lay out data type "Maybe" yet` — a message naming the
*type* for a bug about the *function*.

Cost: **no program could build**, including programs that never mention `Maybe`, because the
prelude is implicitly imported. It went unnoticed for exactly as long as every generic
function happened to take a bare `t` parameter — `unwrap_or(m: Maybe<t>, fallback: t)` hits
the `GenericType` case through `fallback` and lowers fine, which is why the prelude worked
right up until it gained a predicate. The bisect is worth keeping: of the prelude's ten
functions, the three with a bare-`t` parameter built and the other seven did not.

Cases for `LambdaType`, `RawPointerType` and `ConstrainedType` went in on the same
reasoning rather than from an observed failure — a boxed closure is a pointer, so `() -> t`
happens to need no layout under the dev-tier lowering, and that is not a property to depend
on once Lambda Set Specialization lands. Every composite that can hold a type needs a case
here; a miss is not a missing feature but a wrong answer, and the symptom appears far from
the cause.

**A generic function is now callable through a module namespace.** `opt.wrap(7)` was rejected
with "cannot assign integer literal to t" while `import util.opt.{ wrap }` and the same call
inside its own module both checked — so the namespace form, which is the one the `std.maybe` /
`std.result` split is built on, was the only broken way to call a generic. Found while
exercising `maybe.map`.

Two independent halves, each verified load-bearing by reverting it alone. The **front end**
checked the call against the *declared signature*: `moduleMemberType` handed back a
`*types.LambdaType` whose type variables are still free, and nothing downstream could solve
them, because the solver (`inferGenericCall`) works from the declaration. It now returns the
`*ast.LambdaExpr` too, and `inferMemberCall` calls the same `inferLambdaCall` a direct call
does — which is what the comment there already claimed happened. The **backend** then failed
one step later: a generic function has no emitted body (a type variable has no
representation), so `namespaceCallee`'s `l.funcs` lookup found nothing, the call fell out of
the namespace path entirely, and it died as `unsupported method call "unwrap"` *after* type-
checking cleanly. It now asks `specializedFuncFor(call)` first, exactly as the by-name path
does — the instantiation is keyed by call node, so the specialization the typechecker solved
is already the right answer.

Worth naming: the two failures are the same omission at two layers, and the second was
invisible until the first was fixed. That is also why the test is an exec test —
`pkg/backend/llvm` had **no multi-module harness at all** (`driver.Analyze` resolves no import
graph), so nothing in that package could have caught a cross-module call regardless;
`buildAndRunModules` is that gap closed.

**Modules landed.** Design was already settled by the grammar (`module a.b`, `import a.b` / `as`
/ `.{ X, Y as Z }`, `pub` — with `IsPublic` already collected); what was missing is resolution
semantics and implementation. Decided: the prelude is a **normal module, implicitly imported**
(so it is written once, with `pub`, against the real mechanism); a plain `import a.b` binds a
**namespace** under the last segment (`.{ }` brings names in unqualified, `as` renames); module
paths map to files **by directory convention**. The implicit prelude import is the one exception
to the namespace rule — it brings names in unqualified, since `prelude.Maybe` defeats the
purpose, and ambient-ness is a concept a prelude needs under any design.

- **Resolution + merge** (`pkg/modules`, `driver.AnalyzeUnits`) — transitive imports,
  dependency-first order, shared dependencies emitted once, cycles (`lyra-E027`) and
  unresolvable imports (`lyra-E026`) reported. Diagnostics carry their file everywhere via
  `ast.Location.File`. Multi-file programs compile and run.
- **Per-module scoping / namespacing.** Every read of the symbol table's
  `Types`/`Functions`/`Traits` maps goes through **`LookupType`/`LookupTrait`/`LookupFunction`**
  rather than indexing them — the 37 sites across 7 packages became one choke point.
  Cross-module **collisions are rejected** (a duplicate type already errored; `RegisterFunction`
  overwrote silently, which with modules meant a program built against whichever module was
  collected last — the collector also dropped the error), with messages naming the *other* file.
  **`import util.math` now binds a namespace**, used as `math.double(…)`, in both the
  typechecker (`typechecker_modules.go`) and the backend (`namespaceCallee`). The asking module
  is recovered from the node's own `Location.File`, so no pass needed a module context threaded
  through it — the second time stamping the file onto every location paid off. Membership is
  *checked*, not assumed: names are program-wide unique today, so a bare lookup would resolve
  `math.secret` to another module's `secret`, binding a reference the source never made. A local
  value shadows a namespace, so `math.double` stays a field read when `math` is a binding; the
  namespace check runs before the object is inferred, since a namespace is not a value and
  inferring it would report an undefined identifier. Fixing this also stopped the unused-import
  lint misfiring on a used import.

- **`pub` enforcement** (`lyra-E028`). Within a module everything is visible; `pub` crosses the
  boundary. One check (`visibilityOf`/`checkVisible`) wired at the three places a cross-module
  reference resolves — a namespace member, `resolveType`, and a **bare call**. The bare-call
  case is the one that matters: top-level names still share one namespace, so `helper()` reaches
  another module's function without naming it, and a namespace-only check would leave private
  functions private in name only. Needed two fixes first: `VarDeclStmt` had no `IsPublic` field
  at all (so every binding was implicitly public), and `visibility` is an anonymous grammar
  child rather than a labelled field, so reading it by field name returned nil silently.
  Single-file programs are unaffected — file and names alike have no module, so every reference
  is same-module.
- **Implicit prelude** (`std.prelude`). An ordinary module — `pub` exports, same roots — that
  the compiler imports for you, so it stays readable, testable and replaceable rather than baked
  into the compiler. Names are available **unqualified** (the one exception to the namespace
  rule; `prelude.Maybe` defeats the purpose). A missing prelude is not an error, the prelude
  does not import itself, `LYRA_NO_PRELUDE` opts out, and a user declaration taking a prelude
  name **warns** (`lyra-W012`) with the local one winning — erroring would make every exported
  name permanently unusable and adding one later would break existing programs. Two ordering
  traps found while building it: the prelude module must be named before any file is walked
  (types register during the walk), and prelude names need their own set because `ModuleOf` is
  last-writer-wins, so a user module declaring the same name erases the record before functions
  are registered in `Finish`.
  - **The shadow is confined to its own module.** It used to replace the prelude's declaration
    *program-wide*: a second module that never mentioned the name got the first module's, or —
    when the shadowing declaration was **private** — got nothing at all, since withdrawing the
    prelude's registration deleted it for everyone. A module's *own* private helper made a
    prelude name undefined in every other module. The fix is where the prelude's exports live: a
    **`PreludeScope`** between every module scope and the global one (`ast/symbols`), so the
    prelude stays reachable everywhere, a module's own declaration beats it *there*, and nothing
    about that reaches another module's chain. Below the global scope would not do — that is
    where every module's exports live and every module falls through to it, so the first
    declaration of `Maybe` would again win for the whole program. `FunctionKey` gained the
    matching key (a prelude-shadowing declaration is module-qualified whatever its visibility,
    so the prelude keeps the bare key and every non-shadowing module still finds it), which the
    backend and the ownership pass already ask for from the *referencing* location. Two things
    had to follow: the **entry module needs a scope of its own** — sharing the global scope was
    the reason an entry-file `let unwrapOr` rebound the prelude's program-wide — so anything
    walking scopes for one file starts from `EntryScope` (the LSP's completion, definition,
    references, rename and highlight walks all did start from global, which is exactly why
    sharing looked simpler); and `ownership`/`use_after_move` now resolve a callee with
    `LookupFunctionFrom`, since a bare `Functions` lookup hands back another module's function
    and those passes read the callee's parameter modes to decide where a reference is retained.
    Tests: `TestPrelude_ShadowIsConfinedToItsModule` (entry file / private / exported, each
    asserting both directions by giving the shadow a different signature, so either resolution
    going the wrong way is a type error rather than a different answer).
    - **Still program-wide: a shadowed type or trait.** Their namespace is program-wide by
      construction — `SymbolTable.Types` is keyed by bare name, and so is the backend's registry
      of emitted LLVM struct types, which resolves a type reference carrying no location to say
      who is asking. A program therefore has exactly one `Maybe`, and the shadowing declaration
      is it. Confining a type shadow means per-module type *identity* end to end (mangled type
      symbols plus a location-aware `LookupType`), which is the same work two modules declaring
      unrelated same-named types needs.
- **Cross-module symbol mangling.** A user function is emitted as `lyra.<module>.<name>`, a
  specialization as `lyra.identity$i64`; `main` keeps the C entry-point name. It turned out to
  fix a **present bug** rather than only prepare for privacy: emitted functions took their
  source name verbatim, so a program with a function named `malloc`, `write`, `memcmp` or
  `lyra_rc_alloc` produced a module clang rejected outright against libc or the emitted runtime.
  The dot after `lyra` means a user symbol can never spell one of the runtime's `lyra_` names.
  - **Open when this landed, closed the same day:** two modules could not each declare a private
    `helper`, because the *front end* rejected duplicate top-level names program-wide. Mangling
    removed the backend's objection; per-module name resolution (below) removed the remaining
    one.
- **Per-module name resolution.** Two modules may now each declare a **private** name of the
  same spelling, and each reaches its own. Four parts: each file is walked inside its module's
  scope (so every nested scope parents under it — the keystone; without it a function body's
  chain ran past its own module); a declaration always lands in its module's scope and only a
  `pub` one *also* lands globally; `SymbolTable.Functions` keys a private function by module;
  and the backend's `l.funcs` uses the same key, asked for from the *referencing* location so
  the two cannot disagree. The entry file's module scope **is** the global scope — a program
  root has nothing to be private from, and giving it a child pushed every single-file program's
  declarations out of sight of the LSP's scope walks. One check had to go: `inferIdentifierCall`
  verified visibility *after* a successful lookup, asking whether *some* declaration of that
  name was private (via a last-writer-wins map) rather than whether the resolved one was visible
  — so one module's call to its own function reported another module's privacy. Scoping now
  enforces privacy structurally; the diagnostic only improves the not-found message.

- **Trait-method lowering.** An impl method lowers to a function taking the receiver first; a
  method call is a direct call. Static dispatch, no vtables. Emitted **lazily at the first
  call**, which is what makes a **generic impl** work with no extra machinery — dispatch has
  already substituted `Self` with the concrete receiver, and `typetable.Resolution` now hands
  the backend the impl and that signature so Self substitution is never re-derived. The
  synthesized function is a real `*ast.LambdaExpr` lowered through the shared
  `defineFunctionInto`. Symbols name type + trait + method (neither pair is unique). Bodies are
  queued rather than lowered re-entrantly, so a method calling another — or itself — works.
  Covers data and struct receivers, arguments, managed receivers and returns, and two traits on
  one type. **Open:** trait signatures carry no borrow modifier, so every parameter including
  the receiver is by value.

- **A generic constructor takes its unsolved type parameters from the context.** `Some(v)` fixes
  `t` and always lowered; `None` fixes nothing and `Ok(v)` fixes `t` but not `e`, so both stayed
  the bare declaration — deliberately, since inventing an instantiation from a partial
  substitution would claim precision the construction did not supply. The cost was that they
  lowered *only* under an annotated `let`, the one site that stamped its type onto the value:
  `-> Maybe<i64> => None` failed the build with `unknown named type "Maybe"`, and the prelude's
  `Result` was unusable outright, since neither constructor determines both parameters. Fixed by
  `propagateInstantiation` (`typechecker/propagate_instantiation.go`), the generic-type analogue
  of `propagateLiteralType`/`propagateAllocation`, wired at the same choke points — annotated
  `let`, the three return-body sites, the call-argument site, and the *generic* call's argument
  site, which is the one that makes `unwrap_or(None, 42)` work (the parameter is only
  `Maybe<i64>` once another argument has solved `t`). **It checks rather than assumes**, and
  that turned out to matter more than the lowering: a partly solved construction's payload was
  not verified against the context at all, so `let r: Result<i64, string> = Ok("x")` passed the
  front end and was caught only by the backend refusing to store a string into an i64 payload —
  a type error found by the code generator is one found in the wrong place, and it survived only
  because the value could not lower, so making these lower is exactly what would have turned it
  silent. The payload is now re-checked under the context's substitution, and the node is left
  bare on a mismatch so a wrong payload can never lower as that instantiation. One ordering fix
  went with it: solving promotes an untyped literal to its default in order to unify, so
  `Ok(42)` fixed the payload at i64 and then rejected the `Result<u8, string>` it was returned
  as; an untyped leaf is now left untyped when the payload alone did not pin down every
  parameter, for the context to narrow. **Still open:** the same gap for a generic struct or
  named tuple with a parameter no field can pin down. Tests:
  `TestExec_ContextSuppliesGenericInstantiation` (seven compile-and-run cases) and
  `TestGenericContext_*`, both mutation-verified.

- **Context-directed instantiation extended to generic structs and named tuples, and struct
  field inference made structural.** The data-constructor half (above) left aggregates open.
  They fail differently, which is why they needed their own pass: a bare `DataType` is
  assignable to any instantiation of itself, so a partly solved data construction reached the
  backend and died there, while a bare `NamedStructType`/`TupleType` is *not*, so a partly
  solved one was rejected by the front end with "return type mismatch: expected Tagged<i64,
  boolean>, got Tagged" — a spurious error on correct code. That meant propagating **before**
  the assignability check rather than after it, so every context site now goes through
  `contextualType`, which propagates, re-reads the record, and reports whether it already
  emitted a diagnostic (without that flag one mistake produced two errors: the precise
  "Tagged.value: cannot assign string to i64" plus a coarse return mismatch). Two independent
  causes were behind the aggregate failures. A **phantom** parameter appears in no field at all,
  so only the context can supply it — that is what the propagation handles. A parameter
  appearing only *inside* another type (`inner: Opt<t>`, `items: [2]t`, `pair: (t, t)`) was
  unsolvable for a different reason: field inference matched only a field declared as a *bare*
  parameter, so `struct Wrapper<t> { inner: Opt<t> }` could not be solved from its own fields.
  It now unifies structurally with `unifyGenericTarget` — the same unifier data constructors and
  generic calls use — and the field types are substituted structurally too, rather than by
  looking the field's type *name* up in the solution. The latter caught a silently-accepted
  error: `Holder { tag: 1, inner: Just("x") }` compared against the raw `Opt<t>`, which the
  "still generic, check leniently" guard swallowed, so a wrong value went unreported while the
  surrounding instantiation looked complete. That guard is now `mentionsGenericParam`, which
  walks the type instead of testing its name. **Still open:** nothing known — the named-tuple
  element check now also defers to the context when the elements alone leave a parameter
  unsolved, so `Pair<t, u>(t, t)` built as `Pair(40, "x")` no longer blames the second element
  for a binding the first one guessed. Tests: `TestExec_ContextSuppliesAggregateInstantiation`
  (eight compile-and-run cases) and `TestGenericContext_*`, each mutation-verified against
  reverting the propagation, the structural solve, the structural substitution, and the
  duplicate suppression independently.

- **A generic function solves its type variables through a *function-typed* argument.**
  `unifyGenericTarget` had no `LambdaType` case, so a declared `() -> t` matched against a
  supplied `() -> i64` bound nothing and the call reported "cannot infer type variable t from
  these arguments"; `substituteGenerics` had the same omission, so even once `t` was solved the
  parameter stayed `() -> t` and the argument was rejected as "cannot assign () -> i64 to () ->
  t". Both halves are required, and between them they are what makes any callback-taking
  combinator expressible at all — `unwrap_or_else`, `map`, `and_then`. Parameters unify in the
  same direction as the return type: a function type is contravariant in its parameters, but
  this is unification against a *pattern* rather than a subtyping test, so direction only
  decides which side a variable may be read from and either is correct. `collectGenericNames`
  gained the matching case, so a variable appearing *only* inside a function type is still
  recognised as one in play, and the substitution returns a **copy** — `LambdaType` is the one
  type here held by pointer, so rewriting in place would mutate the declaration every other call
  site shares. Found because the prelude gained `unwrap_or_else`, which type-checked standalone
  and then could not be called: nothing had exercised a higher-order generic. Tests:
  `TestExec_GenericSolvedFromFunctionArgument`,
  `TestGenericContext_FunctionArgumentUnificationStillRejects` (inconsistent bindings, wrong
  arity, and the solved return type enforced at the use site), and
  `TestShippedPrelude_CombinatorsAreCallable`; both halves mutation-verified independently.

**Bugs fixed.**

- **`heap-use-after-free` when a *borrowed* `string` parameter is reassigned.** `let f = (s:
  string) -> string => { s = "l" ++ "1"  s }` freed the caller's argument, which the caller then
  released again. A by-value `string`/`ref` parameter is a **borrow** — the callee's copy shares
  the caller's reference — but `lowerVarReassignment` released the overwritten value whenever
  the type needed a drop, without asking whether the binding owned it (its own comment claimed
  it checked "the same condition lowerVarDecl framed the binding"; nothing did). Now guarded by
  `slotIsOwning`, shared with the interior-lvalue path's `releaseOldTarget` so the two cannot
  disagree — framed slot (a local, or an `own` param) or a by-reference `mut` param (whose slot
  *is* the caller's owning storage, so writing through it still releases, correctly). Surfaced
  the moment ASan started instrumenting; note the original diagnosis blamed `mut`, which was
  wrong — `mut` was the case that already worked. Tests:
  `TestExec_BorrowedParamReassignment_NoUseAfterFree`.
  - **Residual leak: gone (07/30).** Reassigning a borrowed parameter is now `lyra-E025`, so the
    program that leaked (`(s: string) => { s = "a" ++ "b"  0 }`) no longer compiles — the
    language rule removed the codegen problem rather than the backend having to frame the slot.
    The `slotIsOwning` guard stays as defense in depth, on the backend's standing rule that it
    errors or does nothing rather than emitting a wrong release.

- **An anonymous tuple literal was built at the untyped default, not the context's width.** `let
  f = (t: (u8, u8)) -> u8 => …` called as `f((10, 40))` emitted `call i8 @f({ i64, i64 } …)`
  against a `{ i8, i8 }` parameter — invalid IR. `propagateLiteralType` narrowed the tuple's
  *leaves* but never re-recorded the tuple **node**, which is what the backend builds the
  aggregate from; the array case beside it had always re-recorded, with a comment explaining
  why. Fixed by re-recording an anonymous tuple literal at the context's element widths (named
  tuples are excluded — nominal, and already recorded against their declaration, possibly as a
  generic instantiation). Covers the argument, return, struct-field, and data-payload positions.
  Invisible to Apple clang 21 (opaque pointers make the two function types indistinguishable,
  and arm64 passes small structs in registers so the value was right anyway); found by
  `./asan.sh`. Tests: `TestExec_AnonymousTupleTakesContextWidth` plus an IR assertion, since on
  macOS only the emitted text shows it.

- **`i128` overflow-checked multiply did not link on Linux.** `llvm.smul.with.overflow.i128` is
  not lowered inline — LLVM expands it into a call to compiler-rt's `__muloti4`, and clang links
  compiler-rt by default on macOS but **libgcc on Linux**, which has no such symbol. So `lyrac
  build` of a signed i128 multiply failed at link time there while the identical IR was fine on
  macOS. Fixed by emitting the helper into the module (`lyra_i128_mul_overflow`, `trap.go`,
  routed at `overflowIntrinsic`'s single choke point so the call site is unchanged) rather than
  by adding `--rtlib=compiler-rt` to the link line: the same reason the ref-counted runtime and
  the 128-bit formatter are emitted as real bodies — one `clang out.ll` stays self-contained
  everywhere. Defining `__muloti4` under its own name would also have worked but squats on
  another runtime's ABI. Unsigned multiply is unaffected (LLVM expands it inline), as are
  division and addition (`__divti3` is in libgcc too). Tests: `TestExec_I128MulOverflowHelper`
  drives every branch directly, including `-1 × INT128_MIN` — which has no Lyra spelling
  (128-bit literals aren't representable yet) and is the case that must *not* be checked via
  division, since `sdiv INT128_MIN, -1` is itself undefined.

### 07/29/26
- **Generic types** (`Box<t>`, `Maybe<t>`, recursive `List<t>`, generic named tuples) — the
  keystone the prelude, `checked_*`, `?` error conversion, and a stdlib `BigInt` were all queued
  behind, and they **compose** with generic functions (`(x: t) -> Box<t>`). Typechecker: a
  construction evaluates to the *instantiation* it denotes (a `ParameterizedType` carrying the
  solved arguments) instead of the bare declaration — the substitution was already being solved
  to check the fields and then discarded, which is why a field read returned the type variable
  and an annotated binding reported "cannot assign Box to Box"; a data constructor and a named
  tuple solve theirs positionally with the same unifier a generic call uses. Backend
  (`generic_types.go`): one LLVM type per instantiation (`%Box$i64`), materialized **lazily** by
  `lowerType` rather than from a collected table (no single site "uses" a type, and every site
  that can name one already funnels through `lowerType`), with the declare-then-define split
  making a recursive `shared List<t>` terminate, and `resolveInstantiation` normalizing an
  instantiation at the same choke point that strips newtypes so no aggregate-reading site needed
  a new case. A **managed** type argument was a **double free**: `OwnsManaged` had no
  `ParameterizedType` case, so the pass minted no retain for a copy while the backend released
  both bindings — macOS ASan missed it, and the regression test compares retain/drop-glue counts
  against the equivalent concrete declaration.
- **Generic functions work end to end — instantiation plus monomorphization.** The biggest item
  on the board, and further back than the todo implied: a generic function did not even
  *type-check*. `identity(7)` reported "cannot assign integer literal to t", because nothing
  solved `t` — the existing generic machinery (`substituteGenerics`, `unifyGenericTarget`,
  bounded-impl dispatch) all served trait dispatch, and there was no path that solved a type
  variable from a value. Chosen slice: generic **functions**, end to end, over generic *types*
  first — the narrowest complete vertical (one calling form, one specializer), and it builds the
  monomorphizer that generic types and release-tier closures (LSS) both need next.
  **Instantiation** (`typechecker/instantiate.go`): each declared parameter type is unified
  against its argument's inferred type to solve the variables, the call is checked against the
  *substituted* signature, and the result is the substituted return type. The unifier is the one
  trait dispatch already uses — sharing it keeps "what does this type variable match" a single
  definition rather than two that drift. An untyped literal argument settles to its default
  width before binding (`identity(7)` → `t = i64`): a type variable is a real type in the
  specialization, deciding an alloca's width and an instruction's signedness, so leaving it
  untyped would push an unresolved literal type into codegen — the same class of bug as an int
  literal in a float slot. A variable appearing only in the return type is unsolvable from
  arguments and is reported at the call, not discovered during lowering; arity is checked first,
  since a missing argument is just a variable with nothing to bind it.
  **Monomorphization** (`backend/llvm/monomorphize.go`): one function per distinct instantiation
  (`identity$i64`, `identity$boolean`), shared between call sites that solve alike, with the
  bare generic name never emitted and an uninstantiated generic costing nothing. **By
  substitution, not by cloning the AST** — the one shared body is lowered per binding set with a
  substitution installed on the lowerer, consulted by the two accessors every lowering decision
  already funnels through (`lowerType` for a source-written type, `recordedType` for a TypeTable
  one). That is enough to make the body concrete down to its locals and arithmetic. Cloning
  would mean deep-copying every node and re-typechecking each copy or hand-patching a parallel
  TypeTable: much more machinery, and two ways for a specialization to disagree with the body it
  came from. `defineFunctionInto` is shared with the ordinary function path, so parameter
  binding, `own`-param framing, and the void/typed return split cannot drift between a generic
  function and a plain one.
  **Two boundaries, both found by writing the tests, both deliberate.** An **unbounded** type
  variable supports only what every type supports: `x + x` on a `t` is rejected, and that is the
  sound answer — `t` could be `bool`. Arithmetic needs bounded polymorphism over an operator
  trait (`where t: Add`), which does not exist; the test pins the refusal rather than the wish.
  And a **managed** type argument is refused loudly: the ownership pass analyzes the generic
  body *once*, where a type variable is not managed, so it records no retain, release, or drop
  anywhere in it — at `t = string` those decisions are simply wrong. Measured before the refusal
  went in: an ASan abort, 2 allocations against 3 releases. Substituting types cannot repair it,
  because the ownership *decisions* were made generically. Running the pass per instantiation —
  teaching it to take a substitution and produce a table per specialization — is the fix and the
  natural next slice.
  Tests: `backend/llvm/llvm_generic_test.go` (8 exec cases — one and two instantiations, a
  variable inside an array type, two variables, one variable in several positions, a body-local
  of the variable's type, called from another function, instantiated at a narrow width — plus
  the emitted-specialization shape, that two identical call sites share one function, that an
  unused generic emits nothing, and the managed-type refusal),
  `typechecker/tests/generic_call_test.go` (8: solving from arguments, the substituted result
  flowing on, solving through a composite, two variables, inconsistent binding rejected, an
  unsolvable return-only variable, arity first, and the unbounded-arithmetic boundary).
  **Still deferred:** generic *types* (`Box<T>`/`Maybe<T>` construction, inference, and
  monomorphized layout — which is what a prelude, and with it `Maybe<weak T>`, `checked_*`, and
  `?` error conversion, are all still waiting on), multi-clause generic functions, and a generic
  function calling another at a variable-dependent instantiation. **The managed-type refusal is
  gone** — see the per-instantiation ownership entry.
- **The ownership pass runs per generic instantiation — a managed type argument works.** The
  limitation generics landed with, and the reason it was a *refusal* rather than a miscompile:
  every decision the ownership pass makes turns on whether a value is reference-counted, and
  that is a property of the type *argument*, not of the generic body. Analyzed once with `t`
  abstract, nothing looks managed, so `pick(a: t, b: t) -> t` recorded no retain on its result
  and no release for the caller's temporaries — correct at `t = i64`, a double free at `t =
  string` (measured: an ASan abort, 2 allocations against 3 releases).
  **The fix is one table per instantiation, not one table.** `ownership.AnalyzeLambda(lam,
  symTable, tt, subst)` analyzes a single body under an instantiation's bindings; the driver
  runs it per specialization into `Result.OwnershipBySpec`, keyed by the instantiation's
  `Key()`. They genuinely cannot be merged: the tables are keyed by AST node, and the *same*
  node carries different annotations in different instantiations — precisely the information one
  shared table could not hold. Two choke points made it small: the pass gained a single type
  lookup (`analyzer.typeOf`) that applies the substitution, so all four of its type reads became
  correct at once; and the backend reads the table through one accessor (`l.ownership()`), which
  returns the specialization's table inside a generic body and the program-wide one everywhere
  else. An annotated binding inside a generic body (`let copy: t = x`) needed the same
  substitution applied to its *declared* type.
  Verified by mutation: reverting the accessor to the program-wide table makes the managed case
  fail again (wrong exit code and an ASan crash), so the per-instantiation table is load-bearing
  rather than incidental. Tests: `llvm_generic_test.go` gained 4 exec+ASan cases — the shape
  that aborted, identity at a string, identity at a `[]string` (a managed *container* argument,
  where the element drops are the box's and must not be doubled), and a managed and a scalar
  instantiation side by side in one program, which is the case that most directly needs two
  tables — plus an assertion that the *scalar* specialization contains no refcount traffic at
  all, so the managed one's decisions cannot have leaked across.
- **`weak` gets runtime semantics — created, upgraded, and released (phase 2).** `weak T` was a
  type and nothing more: it collected, resolved, and broke E014 size cycles, but no expression
  produced one, so a struct with a weak field could be declared and never built. Three pieces
  landed, in the order the design forced.
  **The two-count box header.** A weak reference must be able to ask "is the referent alive?"
  *without* dereferencing freed memory, so a box's memory has to outlive its strong count: the
  header is now `{ i64 strong, i64 weak }`, the payload's drop glue runs at strong 0, and the
  memory is freed only once weak reaches 0 too. Uniform across every box kind — string, dynamic
  array, closure environment, `shared` value — which costs 8 bytes per heap value and buys one
  protocol with no per-kind branching. Two alternatives were rejected and recorded: giving only
  weakly-referenced types a wide header makes the *header size type-dependent*, the same silent
  split-by-representation this backend has been bitten by twice (by-value `mut` params, newtype
  managed-ness); packing two 32-bit counts into one word puts mask/shift arithmetic in the
  hottest runtime functions plus an unenforced overflow assumption.
  **The delicate part was the field indices, so it was done in two steps.** A GEP index is a
  bare integer — nothing type-checks that field 2 is the payload — so every box access first
  moved behind named constants and helpers (`boxPayloadPtr`, `dynArrayLenPtr`,
  `dynArrayElemPtr`, `pinnedBoxConstant`) as a pure refactor, verified green, and only then was
  the layout flipped in one edit. That ordering paid immediately: the one site the refactor
  missed — a `shared` struct's field *read* — surfaced as a panic ("cannot index into type
  *types.IntType using gep") rather than as silently wrong memory, which is exactly what the
  two-step was for.
  **The create and upgrade forms.** `x.weak()` is a builtin method on a `shared` receiver (no
  grammar change; `weak` is only a type in the grammar), and the upgrade is `if let s = w { … }
  else { … }` — decided today over a `Maybe`-returning `upgrade()`, which needs generics, and
  over an `alive()`/`get()` pair, which is a two-call footgun. The upgrade calls
  `lyra_rc_upgrade`: strong != 0 → increment and hand back the box as a real `shared T`, else
  null. So the then-branch holds a genuine owning reference (framed and released like any
  other), the value cannot die under it, and **there is no other way to read a weak reference at
  all** — a weak value has no fields and no dereference, which is what makes a dangling read
  unexpressible rather than merely discouraged. The pattern must be a plain name: a
  destructuring pattern would conflate "the referent is gone" with "it didn't match".
  **A weak reference has its own lifecycle.** It stopped being "never retained/released":
  `IsManaged` covers it, so a copy takes another weak count and a death gives one back — but
  through the weak shims, never the strong ones. Getting that wrong leaks the box's *memory*,
  which is invisible: the payload is already gone, nothing misbehaves, and macOS ASan cannot see
  leaks. The counts are the detector, and the test asserts weak retains == weak releases.
  **The payoff, verified:** a helper creates a `shared Node`, returns a weak reference to it,
  and the strong reference dies at the helper's exit — the upgrade in the caller then *fails*
  and takes the else branch, ASan-clean. Reading the strong count out of a
  dead-but-not-yet-freed box is safe precisely because the weak count kept the header alive.
  That is the property the whole header change exists for.
  **Found and fixed on the way:** a **collector panic** on `if let s = w` — a bare-name binding
  takes the *identifier* branch of `declaration`, which has a `name` field and no `pattern` one,
  and reading the absent pattern field indexed straight into a nil node. It panicked on any `if
  let <name> = …`, not just a weak one. It now synthesizes the equivalent identifier pattern, so
  every downstream pass sees one shape.
  Tests: `backend/llvm/llvm_weak_runtime_test.go` (5 exec cases, each also under ASan — upgrade
  succeeding, upgrade *failing* after the referent died, no-else, upgraded twice, passed through
  a function — plus the weak-count accounting and a check that the upgrade goes through the
  runtime rather than a dereference), `typechecker/tests/weak_test.go` (5: the downgrade, the
  upgrade binding a `shared`, and the three refusals — no field access, no stack receiver, no
  destructuring pattern). Several IR-shape tests were updated for the wider header, and
  `layout_test.go` now pins the field indices against the header shape, since nothing else
  type-checks that correspondence.
  **Still open, and the honest limit:** a `weak` **field** cannot be constructed. A field must
  be initialized at construction, there is no empty weak, and every candidate initializer is a
  self- or forward-reference (rejected by use-before-declaration) — so the parent-pointer shape
  that motivates `weak` in the first place still needs `Maybe<weak T>` (generics) or a nullable
  weak. **Cycle leaks are therefore not closed yet**: the mechanism is in place and works for
  local weak references, but the field case that would actually break a cycle is still
  unexpressible.
- **Destructuring statements lower — `if let` compiles at last (phase 1 of `weak`).** All three
  forms type-checked but none compiled: `let (a, b) = v`, `if let pat = v { … } else { … }`, and
  `let pat = v else { … }` each hit "block statement lowering not implemented" — the same
  front-end-enforces-what-the-backend-cannot-build gap `newtype` had. Landed first because the
  **`weak` upgrade is going to be spelled `if let strong = w { … }`** (decided today over a
  `Maybe`-returning `upgrade()`, which needs generics, and over an `alive()`/`get()` pair, which
  is a two-call footgun), so this had to exist anyway — and it is worth having on its own.
  **One mechanism, three failure paths.** All three drive the *same* pattern machinery `match`
  is built on: `patternMatcher` hands back the `aggPatternTest`/`aggPatternBind` pair for a
  single pattern, so a pattern means exactly the same thing in a match arm and in an `if let`.
  Two implementations of "does this value match this pattern" would drift, which is the whole
  reason for the indirection. Being statements, they need no merge phi, only a control-flow
  join. A plain `let` requires an **irrefutable** pattern — one that imposes a test is a loud
  error pointing at `let … else`, rather than binding on a path where the match may not hold; an
  `if let` binds in the then-block, which is what scopes the names to that branch; a `let …
  else` binds into the *continuation* block, sound precisely because its else branch must
  diverge (a fall-through would be a use of unbound names, so that too is a loud error). A
  `shared` scrutinee unboxes first, as in a match. Deferred with a loud error: destructuring an
  **array** with a pattern, whose length-test-plus-element-tests shape belongs to the array
  match driver rather than a single test/bind pair.
  **Two front-end fixes it needed.** Both `if let` branches (and a `let … else`'s) are now
  checked *for effect* — they are statements, so neither is in value position, and treating the
  last one as a value rejected an ordinary one-armed `if` at the end of a branch (the same fix
  loop bodies got earlier today). That in turn exposed a real hole: `checkExpressionStmt` had
  **no default arm**, so an expression kind it did not name went completely unchecked in
  statement position — a bare `a` naming nothing reported no error at all. It was invisible
  while every block's trailing value inference happened to check the final statement as a side
  effect; three existing if-let tests caught it the moment a branch stopped being inferred as a
  value. It now infers by default, which closes the hole everywhere rather than just here.
  Tests: `backend/llvm/llvm_destructuring_test.go` (11 exec cases — irrefutable tuple and
  struct, if-let matching and failing, no-else, a nested tuple payload, a `shared` scrutinee,
  let-else both ways, if-let in a loop body, the one-armed-`if` branch — plus a managed-payload
  case under ASan with exact alloc/retain/release accounting, and the refutable-plain-`let`
  refusal), `typechecker/tests/if_let_else_test.go` (+3).
- **A `let` inside a loop body is visible there — two loop bodies become pointers.** Open since
  07/13 and a papercut on every loop: `for var i = 0; i < 3; i += 1 { let doubled = i * 2  … }`
  reported `doubled` undefined, so *nothing* could be declared inside any loop body. It also
  blocked the closure work's most natural shape (a closure bound in a loop). **Cause:** the
  collector puts body-locals in a child block scope keyed on the body `*BlockExpr`, and both
  loop nodes stored that block **by value** — the copy has a different address, so `enterScope`
  missed. What made it a silent wrong answer rather than an error is that `enterScope`'s miss
  path just runs the body in the *enclosing* scope; the loop variable lives there, so loops
  looked like they worked. **Fix:** `ForLoopExpr.Body` and `ForInLoopExpr.Body` are
  `*BlockExpr`, exactly as `IfDestructuringStmt.Then/Else` already were and for the same
  recorded reason.
  **The change surfaced two more defects, one in each direction.** (a) `assignedNames(node any)`
  silently accepted the now-`**BlockExpr` argument and returned the empty set, so a loop havoc'd
  *nothing* — a stale interval survived the loop and the range analysis then elided a downstream
  safety check on it. Two existing range tests caught it immediately
  (`TestRange_Safety_HavocInNestedBlockNotElided` and its false-positive twin), which is exactly
  what they were written for. Its parameter is now `ast.AstNode`, so the same mistake is a build
  error rather than an empty map. (b) A loop body was being type-checked as though its last
  statement were the block's *value*, so a one-armed `if` at the end of a loop body — an
  ordinary conditional side effect — was rejected with "`if` used as a value must have an `else`
  branch". Loop bodies now go through `checkBlockForEffect`, which is the same walk minus that
  step; every statement is still checked and its types recorded, so nothing downstream loses
  information.
  Tests: `backend/llvm/llvm_loop_local_test.go` (8 exec cases across both loop forms — including
  a `var` body-local, a body-local reading an outer binding, a closure bound in a loop body, the
  one-armed `if`, and nested loops with their own locals — plus a managed body-local checked
  under ASan and the path-sensitive conservation check, proving each iteration's string is freed
  on that iteration), `typechecker/tests/for_loop_test.go` (+6, incl. that a body-local does
  *not* escape the body). The conservation corpus's "allocation in a loop" case had its
  allocation moved back into the loop body, where it belonged — it lived in a helper only
  because of this bug.
  **Found here, fixed next:** the backend's `l.locals` had no scope discipline, so a shadowing
  binding clobbered the outer one permanently — see the shadowing entry.
- **Backend locals are lexically scoped — shadowing no longer clobbers the outer binding.**
  Found while testing the loop-body fix and confirmed pre-existing (the same program misbehaves
  on the pre-change compiler): `l.locals` was a single flat name→slot map for an entire
  function, so `let n = 100; let inner = { let n = 5  n }; n + inner` returned 10 instead of 105. Every construct that binds a name for the duration of a sub-tree leaked that binding into
  everything after it — a block's `let`, a loop variable, a C-style loop's counter, a match
  arm's pattern. Silently, and with a wrong *value* rather than an error, since the typechecker
  resolves all of these correctly and only codegen disagreed.
  **Fix:** `pushLocalScope` snapshots the visible bindings and returns the restore (`defer
  l.pushLocalScope()()`), applied at a block (`lowerBlockStmts`, next to the managed frame it
  already pushes), each of the four loop lowerings, and each match-arm loop. **The match needs a
  reset *per arm*, not just a restore after the match** — the sharp case is an arm that reads an
  outer binding an earlier arm's pattern shadows (`let v = 100; match Right(5) { Left(v) => v,
  Right(x) => v + x }`): without the reset the second arm reads the first arm's slot, which on
  that path was never stored to, so the result is whatever was on the stack (measured: 6 instead
  of 105). The restore installs a fresh copy on each call so repeated resets can't leak writes
  back into the snapshot.
  Name scoping is deliberately independent of the ownership bookkeeping, which tracks *slots*:
  two same-named managed values in nested scopes are two allocations, each released exactly once
  (pinned by an ASan + path-sensitive-conservation case). Tests:
  `backend/llvm/llvm_shadowing_test.go` (10 exec cases — nested block, `if` branch, loop
  body-local, loop counter, for-in variable, `data` match arm, scalar catch-all, arm-to-arm
  non-leakage, the outer-read-after-an-earlier-arm case, two-level nesting — plus the managed
  pair). Each scope site was mutation-checked: removing the block scope fails 3 subtests,
  removing the loop scopes 6, removing the per-arm reset exactly the one case written for it.
- **Closures lower — a function is a value at last (the boxed dev tier).** The largest missing
  language capability: a lambda could be *written* but never used as a value, so `apply(double,
  3)` failed with "unknown type: (i64) -> i64" and a nested lambda with "expression lowering not
  implemented for *ast.LambdaExpr". This is the dev tier of the two decided on 07/17; Lambda Set
  Specialization stays gated on the generics monomorphizer, as decided.
  **Representation:** a function value is `{ i8* fn, i8* env }`. One shape for every function
  type — which is the point, since it lets a `(i64) -> i64` parameter accept a named function, a
  captureless lambda, and a capturing closure with no per-call-site specialization, and is what
  a stable hot-reload ABI requires. `fn` is the lifted body, always `ret (i8* env, params...)`;
  `env` points at the *payload* of a ref-counted box `{ i64 rc, { i8* dropFn, captures... } }`,
  exactly `rcHeaderSize` past its header — deliberately the same relationship a string's data
  pointer has to its box, so `managedBox` recovers it by subtracting the header and
  **retain/release/drop needed no new machinery**: `IsManaged` gained one case and closures
  became ordinary managed values. A **captureless** closure shares one *pinned* static
  environment, the same device string literals use, so a plain function value costs no
  allocation while still flowing through the ownership model unchanged.
  **Three design choices worth recording.** (1) A **named** function used as a value gets a
  thunk (`@name.closure`) rather than every function growing an environment parameter — a direct
  call by name keeps its plain signature, so every existing call site and its pinned IR are
  untouched, and the forwarding call exists only for functions actually used as values. (2)
  Nested lambdas are collected up front and their bodies lowered **last**, never re-entrantly at
  the creation site: lowering a body mid-expression would mean saving and restoring the whole
  per-function state (locals, loop stack, managed frames, pending temporaries), and one missed
  field there is a leak discovered three features later. (3) The environment's captures are
  freed through **one generic trampoline** that reads the per-capture-set glue out of the
  environment's first slot — a release site knows only the static type `(i64) -> i64` and never
  which lambda produced the value, so the glue has to travel *with* the value rather than be
  chosen at the release.
  **Captures are by value** (`pkg/analyzer/captures`): copied at creation, which is what lets a
  closure outlive the frame its captured bindings live in — `makeAdder(5)` returns a closure
  over `n` and calling it later is valid. Capturing the slot by reference would dangle the
  moment that frame returned, and there is no escape analysis to tell the safe case apart. The
  pass is deliberately simple — a name read inside the lambda, not bound inside it, not a global
  — and **flow-insensitive**, which is sound because reading an outer binding later shadowed by
  an inner one is already a use-before-declaration error. What makes that simplicity safe is
  that both failure directions are *loud*: an over-capture costs a wasted copy (the body's own
  binding shadows it) or errors if no enclosing binding exists, and an under-capture errors too,
  since a lifted body starts with an empty local set. The visible consequence of by-value
  capture is that **assigning to a captured binding is rejected** (`lyra-E024`) instead of
  silently writing to the closure's own copy — the same lost-write failure the by-value `mut`
  parameter had, and refusing it is the same call.
  **Four front-end gaps had to close.** A binding holding a *call result* was "not callable"
  (`let add5 = makeAdder(5)`) — its type says otherwise. An **annotated** lambda in value
  position never had its body checked, so its expressions had no recorded types at all and
  neither the capture pass nor the backend could read them. A nested lambda could not see the
  enclosing lambda's **parameters** (`withParamScope` replaced the map rather than extending
  it), which stayed hidden precisely because those bodies were never checked — so `(n) -> … =>
  (x) -> … => x + n` reported `n` undefined the moment they were. And any expression evaluating
  to a function is now callable, which is what makes `fs[1](5)` and a closure-valued struct
  field work.
  **One leak found by accounting, not by ASan.** An indirect call had no `LambdaExpr` to
  resolve, so the ownership pass treated it as an *unknown* callee — whose result is
  conservatively borrowed — and every string a closure returned leaked. It now reads the
  callee's static `LambdaType` (`calleeLambdaType`): parameters of a function type are borrows
  (a function type cannot express `own`), and the result follows the declared return convention.
  Separately, dropping the environment's own retain on a captured managed value is a genuine
  **double free** — the enclosing binding releases the box and the environment's glue releases
  it again — and the program still runs clean under ASan (the second release reads a refcount
  out of freed memory, gets a poison pattern rather than 1, and never reaches the second free).
  The `alloc + retain == release` count catches it instantly; that is the assertion the test
  carries, with the mutation verified to fail it.
  Tests: `backend/llvm/llvm_closure_test.go` (11 exec cases — named function as a value, local
  lambda, captured local, returned closure, lambda literal as an argument,
  called-twice-through-a-parameter, closure in a struct field, array of closures, multi-width
  captures, capture through a nested closure, void closure; 3 managed-capture cases on stdout +
  ASan; 4 IR-shape cases pinning the fat pointer, the lifted signature, no-allocation for
  captureless, and that a direct call keeps its plain signature; and the two accounting cases
  above), two closure entries in the path-sensitive conservation corpus,
  `analyzer/captures/captures_test.go` (12), `checker/captured_assignment_test.go` (6),
  `typechecker/tests/closure_test.go` (6). The `cmd/lyrac` backend-error fixture moved again —
  to struct record-update syntax, since higher-order calls now lower.
  **Deferred, loud errors:** a `mut`/`ref` parameter on a lambda used as a value (a function
  type carries no borrow mode, so the call site would pass by value while the body expected a
  pointer — a disagreement that is a miscompile, not an error), multi-clause lambdas, and a
  lambda with no return annotation used as a value. Still open and pre-existing: a `let`
  declared inside a loop body is not visible there, so a closure cannot be bound inside one.
- **`newtype` lowers — the typechecker no longer enforces a feature the compiler can't build.**
  Nominal isolation landed earlier the same day, but a `newtype` declaration hit `llvm:
  unsupported type`, so *no* program using one could be compiled. **The lowering is to emit
  nothing.** A newtype is nominal to the typechecker and *is* its base at run time, so it
  registers no LLVM type — deliberately not an alias, which would force every arithmetic,
  comparison, and coercion site to reconcile two llir types for one machine value, for no gain,
  since nominal identity has already done its work by the time codegen runs. Transparency then
  runs through **two choke points**: `lowerType` strips the wrapper for a type read off an
  *annotation* (parameter, return, field, element), and a new **`recordedType`** — replacing all
  ~24 direct `TypeTable.Get` calls in the backend — strips it for a type read off the TypeTable.
  Both use `stripNewtype`, which also resolves a type written as a *name* just far enough to
  answer "is this a newtype?" (a field declared `Email` is recorded as an `UnresolvedType`, so
  the lookup is the only way to reach the newtype at all) and leaves every other name alone,
  since `UnresolvedType` is load-bearing for
  `lookupNamedType`/`namedStructFields`/`resolveDataType`. **What that reveals is how many
  questions are representation questions**: which LLVM type, is the value refcount-managed, an
  untyped literal's width, how `print` formats it, which drop glue, whether an overwritten slot
  owns what it holds. Each is answered against the base now (`types.StripNewtype`, shared with
  `ownership.IsManaged`/`OwnsManaged`), so a newtype over a *managed* base is managed — retained
  on copy, released on death, a move into an `own` parameter (`lyra-E019`) — exactly as the base
  is. Verified end to end: construction and read-out, through parameters and returns, copies,
  struct fields, fixed and dynamic array elements, `match` scrutinees, bool and signed bases,
  and managed bases under ASan with alloc/retain/release accounting. **Three front-end gaps had
  to close for any of it to be usable**, each of which had been silently making newtypes
  unusable or wrong:
  1. **Literal width.** The recorded type of an initializer annotated with a newtype *is* the
     newtype, so `propagateLiteralType` bailed and nothing narrowed the leaves: `let s: Small =
     200 + 100` computed 300 in signed i64, tripped no check, and truncated to 44 — where the
     identical expression against a bare u8 traps. A newtype context now propagates its base.
     Both halves are pinned: the constant form is a compile error, the runtime form traps at the
     base's width (`uadd.with.overflow.i8`).
  2. **The base's own range went unchecked.** `checkIntegerLiteralRange` skipped a
     `*ConstrainedType` entirely on the reasoning that a range constraint subsumes base overflow
     — true when there *is* one, but the commonest newtype (`newtype Meters = i64`, no
     constraints) then had no range check at all. It now checks the base unless the newtype
     declares its own `range(…)`, in which case `lyra-E023` still owns the report, so one
     mistake still yields one diagnostic.
  3. **A named type couldn't survive a function or field boundary.** A call's declared return
     type and a struct field's declared type were returned raw, and a raw `UnresolvedType`
     compares unequal to the same type resolved from an annotation — so `let p: Point = mk()`
     reported the tell-tale **"cannot assign Point to Point"**. Not a newtype bug at all: it hit
     every named type, and structs equally. Both are resolved now (`resolveTypeIfKnown`,
     matching how parameter types already were). Distinctness is untouched — resolving both
     sides is what lets `TypesEqual` compare them at all, so a `Meters`-returning call is still
     rejected against a `Feet` annotation.
  **One leak found and fixed along the way:** the lvalue walk carries the assignment target's
  declared type, so a managed field declared `Email` failed the managed test and the overwritten
  box was **never released** — one leak per assignment, and invisible (macOS ASan doesn't report
  leaks, the program prints the right answer, and the release counts stay plausible). Fixing
  only the managed test would have been *worse* than the leak: the release path asks that same
  type whether the value is a string fat pointer (recover its box) or already a box pointer, so
  it would have released a fat pointer as a box. Normalized in one place instead —
  `lvalueAddress` strips before returning — so the two questions can't disagree. Tests:
  `backend/llvm/llvm_newtype_test.go` (9 exec cases, the base-width arithmetic pair, 3
  managed-base cases with stdout, an ASan + conservation case, 2 IR-shape cases, and the
  managed-assignment leak case whose release *counts* are the detector), a newtype case in the
  path-sensitive conservation corpus, `typechecker/tests/constrained_type_test.go` (+9:
  call/field round trips incl. the struct one, base-range overflow, and the
  constraint-owns-the-report split), `ownership/ownership_test.go` (+2),
  `checker/use_after_move_test.go` (+1). Two tests that asserted the old deferral were
  repointed: the backend one now pins that a newtype emits *no* LLVM type, and `cmd/lyrac`'s
  backend-error fixture moved to a higher-order call (closures are the long-lived deferral).
  **Still open:** a *chained* newtype (`newtype UserId = Id` where `Id` is one) doesn't
  type-check — `isAssignable` has no symbol table, so it compares against the unresolved base
  name; codegen already handles the chain, so this is a front-end fix whenever it's wanted.
  Arithmetic on a newtype value stays rejected (`types.IsNumeric` excludes it) and so does
  `print` — reading out to the base is the one way to operate on one, which is the nominal
  discipline working as intended, not a gap.
- **A path-sensitive conservation check over the emitted IR.** Four bugs this month shared one
  shape: accounting that looks balanced but isn't *per path*. The `[head, ...tail]` guard leak
  was the sharpest — one allocation, one release, perfectly balanced totals, with the
  guard-false edge carrying the box past its only release. Nothing caught it: not the counting
  conservation test (totals are the wrong granularity), not the behavioral tests (the program
  returned the right answer), not ASan (which on macOS reports use-after-free and double-free
  but *not* leaks). It took reading the CFG by hand. This makes that reading mechanical: from
  each `lyra_rc_alloc`, follow the box forward — through bitcasts, GEPs, phis, selects,
  insert/extractvalue, and stores into local slots — and report if a `ret` is reachable with it
  neither released nor escaped (`conservation_check_test.go`). **Tuned for no false positives**,
  since a noisy assertion gets deleted: any use it doesn't fully model — passing the box to a
  user function (which may take ownership), storing it through a computed pointer, returning it
  — marks the value *escaped* and drops it from consideration. It admits false negatives by
  design; it is a net for one specific, repeatedly costly shape, not a verifier. `Backend.Emit`
  was split over a new `emitModule` so the check gets the real `*ir.Module` instead of
  re-parsing the printed text (the alternative, llir's `asm` parser, would have added module
  dependencies for a test). **Two guards keep it honest, and both earned their place
  immediately.** (1) A hand-built leaky module it must flag: while matching names with llir's
  `Ident()` — which prefixes the `@` sigil — it matched *nothing*, so every corpus program
  tracked zero allocations and the whole suite passed vacuously; the self-test is what exposed
  that. (2) A per-program assertion that at least one allocation was genuinely path-checked,
  which then caught three corpus programs that proved nothing (two passed their box to a call,
  one returned it — all legitimately escaping). **Validated against the real bug:** with the
  guard fix temporarily reverted, the check fires on `pick` and names the leaking exit;
  restored, it is silent. Corpus of 10 branching programs (guarded and unguarded tail bindings,
  concat in a branch, an if merging two fresh boxes, allocation in a loop, early return and
  break past a live box, match on a fresh string, interpolation in a branch, a managed dynamic
  array).
- **`[head, ...tail]` array patterns lower — the recursive list idiom works end to end.** The
  last deferred array-match form. `tail` is the one array binding that is *not* a borrow: the
  elements it needs are a suffix of a box whose header describes the whole array, so there is no
  existing storage to alias. `bindTailSubArray` (`match_array.go`) allocates a box sized at run
  time (`length - fixedCount`), copies the suffix in a loop, and binds it; the arm's length test
  becomes `>=`. **Managed elements are retained per element** — the tail's drop glue releases
  them when it dies, so the reference is duplicated rather than moved out of the source, or the
  two would both free them. **It needed no ownership-pass change**, which is what it had been
  filed as blocked on for weeks: the pass keys managed-ness off the recorded type, so it already
  sees `tail` as managed, and because a pattern binding is not a `VarDeclStmt` it is never
  last-use-eligible — so every *owning* use inside the arm (returning the tail, passing it to an
  `own` parameter) records a plain `Retain`, which is exactly the +1 an escape needs. The frame
  release balances the box's own reference. Cost is one retain/release pair versus a transfer:
  refcount traffic, not a leak. **The one real design catch** was *where* to release it: framing
  it in the enclosing scope faults, because those releases run on every path and an arm that
  never matched has an uninitialized slot (found by a BUS error in `lyra_rc_release`). It lives
  in an **arm-scoped frame** instead, putting the release exactly on the path that allocated —
  and `emitReturn`'s release-all-frames still covers a body that returns. Tests:
  `backend/llvm/llvm_match_array_test.go` (recursive sum, two fixed elements before the rest, a
  one-element array yielding an *empty* tail, a literal element in front of the rest, the tail
  escaping as the arm's value, and `[]string` managed elements; plus an ASan case where a
  managed tail outlives the match and the source must stay intact). The deferral test that
  asserted the loud error was replaced. **Guard edge (fixed in the same change):** a guard is
  tested *after* the pattern's bindings exist, so a `[h, ...t]` arm has already allocated by the
  time it runs — and a failing guard falls through to the next arm, skipping the body's release,
  leaking a box per failure. Confirmed against the emitted CFG: the allocation sat in the
  pre-guard block, the release only in the body block. The guard's false edge now gets its own
  block releasing the arm frame before branching on. The *pattern*'s own failure edges need no
  such treatment (the length and element tests all branch before anything is allocated), and an
  unguarded arm gains no extra release. Covered by an exec+ASan case driving 0, 1, and 2 failed
  guards per iteration across 150 calls with managed elements — a missed release leaks a box per
  failure, an over-release is a double free — plus IR assertions pinning one allocation against
  two releases for a guarded arm and exactly one for an unguarded one.
- **Array-match exhaustiveness understands length unions.** `arrayMatchIsExhaustive` only
  recognized a *single* covering arm (a bare `[...rest]` or a catch-all), so the canonical `[]
  => …, [h, ...t] => …` — the very idiom the tail-binding work above enables — drew a spurious
  "not exhaustive" warning demanding an unreachable wildcard. Same corrosive shape as the
  tuple/struct false warning fixed earlier: it trains users to ignore the warning class that
  also covers genuinely non-exhaustive matches. An array match is over *lengths*, so the arms'
  coverage is now unioned: `[e1..en]` covers exactly n, `[e1..en, ...rest]` covers every length
  ≥ n, and the match is exhaustive when every length below the smallest open-ended arm is
  covered exactly. Only arms whose element sub-patterns are all irrefutable count (reusing
  `patternIsIrrefutable`) — `[1, ...rest]` matches only arrays starting with 1, so it proves
  nothing — and a guarded arm contributes nothing, as everywhere else. Tests:
  `typechecker/tests/match_expr_arrays_test.go` (the idiom and a three-arm full cover accepted;
  a gap below the rest arm, a literal-test arm, no rest arm at all, and a guarded arm each still
  warn).
- **Use-after-free: the ownership pass didn't recurse into arithmetic.** Went looking for the
  ownership-pass extension that `[head, ...tail]` was blocked on, and found the blocker was a
  live memory-safety bug rather than a mere prerequisite.
  `MathBinaryOpExpr`/`MathAssignOpExpr`/`NegationExpr` fell through the pass's `expr` walker to
  a `default:` that records nothing, documented as safe because "a missed release only leaks".
  Arithmetic genuinely has no managed *operands* — they're numbers — but a managed value can sit
  **inside** one, and `consume(p.name) + 1` passes a managed field to an `own` parameter, an
  owning position that needs a retain. With none recorded, the callee released a reference the
  caller never granted: the box was freed while the struct still held it, so the struct's own
  drop freed it a second time and any read of the field in between was a **heap-use-after-free**
  (ASan abort, exit 134, on `if p.name == "xy"`). The premise was wrong the same way the
  stack-aggregate use-after-free's was — a missed *retain* at an owning position dangles, it
  doesn't leak — so skipping a node is not the conservative default it was written up as; the
  safety bias only applies to nodes the pass actually *visits*. **Fix:** recurse into all three
  forms with `needOwned=false` (the arithmetic borrows its numeric operands; any call beneath is
  classified by the existing `FunctionCallExpr` case), and rewrite the `default:` comment to
  state the real condition — a form may be skipped only if nothing beneath it can hold a value.
  Verified the whole-*binding* shape (`consume(a) + 1`) still transfers exactly once rather than
  gaining a double retain: it worked before only because the last-use walker, which does
  recurse, happened to see it. **Note the shielding:** the double-*move* shape (`consume(a)`
  twice in a loop) never reached the bug because `lyra-E019` rejects it at compile time; only a
  *field* argument slipped through, since E019 deliberately doesn't count `p.name` as a move
  (that's a partial move, its own design question). Tests:
  `backend/llvm/llvm_ownership_arith_test.go` (the reduced repro, all three arithmetic forms
  consuming a field then reading it back, and the binding-transfers-once control — each plain +
  ASan). **Unblocks** `[head, ...tail]`, which now needs only its allocate-and-copy lowering.
- **`rune` becomes an ordered, convertible scalar — classification logic is expressible at
  last.** A `rune` supported `==`/`!=`, `match` on literal arms, and `print`, and nothing else:
  `c < 'z'` errored ("operands must be numeric"), `i32(c)` errored ("cannot convert rune to
  i32"), and `rune(n)` wasn't even a conversion target ("undefined function"). Net effect:
  is-digit / is-alpha / digit-value could not be written *at all*. **Design (Rust's `char`
  split):** rune is now **ordered** — the four comparisons work between two runes, by code point
  — and **convertible** to and from the *integer* types explicitly (`i32(c)`, `i64(c)`,
  `rune(n)`; rune↔float and rune↔string stay rejected, since a code point has no float reading).
  It deliberately stays **out of `types.IsNumeric`, so it still has no arithmetic**: `c + 1` is
  rejected and the idiom is `i32(c) - i32('0')`, which writes the code-point/number crossing
  down rather than letting rune silently behave as an i32. Comparing a rune against an integer
  likewise needs the conversion. **One representation decision:** `IsSignedInt(rune)` flipped to
  true, so widening sign-extends and ordering uses the signed predicate — a rune lowers as i32
  and Go's rune *is* int32. That was unobservable while nothing consulted it for a rune (an
  existing test asserted the opposite and was updated with the rationale); valid code points are
  non-negative, so the readings differ only for a rune built by `rune(n)` from a negative
  integer, where sign-extension preserves the bit pattern's meaning as Go's does. Tests:
  `typechecker/tests/rune_type_test.go` (ordering, rune-vs-int rejected, arithmetic rejected,
  conversions both ways incl. an untyped literal, float/string rejected),
  `backend/llvm/llvm_rune_test.go` (6 exec cases — is-digit both ways, is-alpha across both
  ranges, digit-value via conversion, a `rune(n)` round trip, and a multibyte `é` ordering +
  widening; IR: `icmp slt i32` + `sext i32`). **Still open:** char *range* patterns (`'a'..'z'`)
  don't parse — `range_pattern`'s bounds are number-literal-only — so `match`-based
  classification still needs literal arms or an `if` chain. Now ergonomics rather than a
  blocker, since ordering makes the `if` form expressible.
- **Newtypes are nominally isolated — `Meters` and `Feet` no longer interconvert.**
  `isAssignable` had two individually-correct rules — a value satisfying the base is assignable
  *to* a constrained type (construction, `let m: Meters = 5`), and a constrained value is
  assignable to its base (`let raw: i64 = m`, the only way to read it, since a newtype has no
  field accessor) — but nothing stopped them **chaining**: `Meters` → `i64` → `Feet`
  type-checked silently, so every newtype over a common base was mutually assignable and the
  unit-mixup a newtype exists to prevent went uncaught. Fixed by rejecting the pair up front
  when both sides are `*ConstrainedType` with different names, leaving both single-step rules
  intact — the minimum that restores nominality without making newtypes unbuildable or
  unreadable. Holds at annotations, call arguments, and returns. Tests:
  `typechecker/tests/constrained_type_test.go` (distinct newtypes rejected via return type and
  call argument; construction, read-out, and same-type all still accepted). **Note this was
  check-only** — a `newtype` declaration couldn't be lowered (`llvm: unsupported type`), so the
  isolation was enforced where it matters (the typechecker) but no program using one could be
  built. That gap closed the same day; see the newtype-lowering entry.
- **`ref` parameters are passed by reference too, and a `mut` borrow is now exclusive.** `ref`
  is a borrow like `mut`, but it was still copied at every call: a `ref [8]i64` was passed as a
  64-byte first-class `[8 x i64]`, a `ref` struct as a whole struct. Since an immutable borrow
  can't write, this was a **codegen waste rather than a correctness bug** — the reason it sat
  open — and `paramIsByRef` now covers `Ref` alongside `Mut` (scalars still excluded via the
  shared `types.IsCopiedScalar`, as `lyra-W010` calls the modifier inert there). **The catch,
  found before implementing:** by-reference makes a `ref` see the caller's *live* value instead
  of a snapshot, which is observable when the same binding also reaches a `mut` parameter of the
  same call. `both(p, p)` with `(a: ref Pt, b: mut Pt)` compiled silently and returned 1
  (snapshot); by-reference it returns 99. So this is not a pure optimization, and Lyra has no
  borrow checker to reject the aliased call. **Paired fix:** `checkExclusiveMutableBorrow`
  rejects passing a binding to a `mut` parameter *and* to any other argument of that call —
  Rust's exclusivity rule, scoped narrowly to argument roots within one call, which is exactly
  the aliasing by-reference passing introduces. That also closes a hole the `mut` change opened
  the same day: `two(p, p)` with two `mut` parameters let each write clobber the other, silently
  (measured: returned 20, not 10). Two `ref` arguments may still share a binding (neither can
  write), and scalars are exempt. **One shape `mut` didn't need:** a `ref` argument may
  legitimately be a **temporary** (`f(Pt { x: 1 })`, `f("a" ++ "b")`) — lending one out is fine,
  where writing to one is meaningless — so `argumentAddress` materializes a non-lvalue into an
  entry-block alloca and passes that; an owned temporary is still released after the statement
  by the ordinary pending-temp machinery (ASan-verified for a managed field and a managed
  temporary through `ref`). Tests: `backend/llvm/llvm_mut_param_test.go` (5 exec cases —
  binding, temporary, fixed array, forwarded ref→ref, managed temporary; IR: `[8 x i64]*` for
  the aggregate, by-value `i64` for the scalar), `typechecker/tests/interior_mutation_test.go`
  (5 exclusivity cases incl. the two allowed shapes and the scalar exemption).
- **Regex literals are `r"…"`, not `r/…/` — killing the division ambiguity outright.** `r` is an
  ordinary identifier and `/` is division, so the old delimiters made `let ratio = r/2 + a/b`
  lex as the regex `r/2 + a/` plus a stray `b`, silently and with no diagnostic from any pass.
  Earlier that day the token was bounded to one line, which stopped an unterminated literal from
  swallowing the rest of the file but left the same-line case intact — a mitigation, not a fix.
  **There is no lexical fix for slash delimiters:** the deciding context (is this expression
  position or after a value?) is arbitrarily far to the right, and a regex may legally contain
  spaces, digits, and operators — the phone-number pattern in the corpus has spaces — so no
  heuristic on the content can separate the two readings. The cure has to be a delimiter that
  cannot follow an identifier. **`"` is exactly that:** Lyra has no juxtaposition application
  (calls require parens), so `r"` can only ever start a regex, and `r/2` is unambiguously
  division again. Kept the `r` sigil, so the node and highlight query are unchanged and the form
  matches how a Python programmer already writes a pattern (`r"\d+"`). **Two bonuses:** `/`
  needs no escaping inside a pattern — `r"/usr/local/bin"` where the old form demanded
  `r/\/usr\/local\/bin/` — and an unterminated literal now degrades to an identifier plus an
  unterminated string, still a loud parse error, rather than running on. The delimiter itself
  escapes as `\"` (verified through `pkg/regex`, which reads it as a literal quote).
  **Migration:** every `r/…/` becomes `r"…"`, dropping any `\/`; the two collection sites
  (expression and pattern position), `regexPatternBody`, and three diagnostic messages were
  updated with it. Tests: corpus (phone pattern, unescaped slashes, escaped quote, `r/2 + a/b`
  parsing as arithmetic, unterminated-is-an-error), `typechecker/tests/regex_test.go` (division
  not shadowed — same-line, cross-line, and before a `//` comment; slashes need no escape;
  escaped quote), regenerated collector goldens. **Note this is a user-visible syntax change** —
  the only one in this batch — so any existing `r/…/` in user code or docs must be migrated.
- **Assignment to a parameter was never type-checked (unreported errors + a backend panic).**
  Found while fixing #8(d): `n = n + 1` on a parameter failed the build with `llvm: type not
  found for *ast.IdentifierExpr`. The cause was upstream of the backend — `checkAssignToBinding`
  resolves the target via `tc.scope.Lookup` + a `*ast.VarDeclStmt` assertion, but a parameter is
  neither (it lives in `tc.paramTypes`), so the function returned at the failed lookup and the
  statement was **skipped entirely**. Consequences, all silent: no assignability check (`n =
  "hello"` on an `i64` parameter — accepted), no literal-range check (`n = 9999` on an `i8` —
  accepted), and, because the RHS was never *inferred*, not even an undefined-identifier report
  (`n = undefinedVar` — accepted). The backend then either failed loudly (integer arithmetic,
  whose `getIntSignedness` needs the recorded type — a bare literal RHS doesn't, and the float
  path doesn't consult signedness, which is why only that one shape surfaced) or **panicked
  outright** (`panic: store operands are not compatible: src=i1; dst=i64*` for `n = true`),
  violating the backend's "never emit wrong code, error loudly" discipline — a panic is a crash,
  not a diagnostic. **Fix:** `checkAssignToBinding` resolves `paramTypes` first (parameters
  shadow outer bindings, mirroring `IdentifierExpr` resolution and `checkLValueAssignment`'s
  ordering), and the shared tail — infer RHS, assignability, allocation flavor — moved into
  `checkAssignedValue` so the variable and parameter paths cannot diverge. **Semantics:**
  reassignment stays permitted for every parameter mode; by value it rebinds the callee's own
  copy, and through a by-reference `mut` parameter it now writes to the caller, which is what
  that modifier means. A `mut` *scalar* remains by value and so doesn't propagate — the one
  split, and precisely the case `lyra-W010` already warns is inert. Tests:
  `typechecker/tests/interior_mutation_test.go` (the four previously-unreported diagnostics +
  well-typed acceptance across bare/`own`/`mut`/float),
  `backend/llvm/llvm_param_reassign_test.go` (7 exec cases incl. narrow width, reading another
  parameter, by-value rebind *not* reaching the caller vs a `mut` aggregate rebind that does;
  plus an ASan case for managed reassignment on both sides of the convention).
- **`mut` parameters are passed by reference — the silent lost-write miscompile (borrow-model
  #8(d)).** `mut` is a *mutable borrow* and the typechecker's own diagnostic tells users it
  mutates "the caller's value", but `lowerParameter`/`defineFunction` copied **every** argument
  into a fresh alloca, so the callee mutated a private copy and the write vanished. Worse, it
  split on the value's representation with **no diagnostic either way**: `mut []T` and `mut
  shared T` propagated (already pointers) while `mut Person`, `mut [2]string`, and even `mut
  Counter { n: u8 }` silently dropped the write — nothing to do with managed values. **Fix:**
  `paramIsByRef` (functions.go) makes a `mut` parameter a pointer to the parameter's type;
  `defineFunction` binds that incoming pointer *directly* as the binding's slot (the
  alloca+store *was* the bug); and the call site passes the argument's address through the
  existing `lvalueAddress` walker, so a bare binding, a path (`f(ps[0])`, `f(o.inner)`), and
  forwarding a by-ref parameter onward all name the caller's storage. `own` stays by value (it
  transfers — the copy is the point); `ref` stays by value (an immutable borrow can't write, so
  by-reference is unobservable — a future optimization for large aggregates, not a semantic
  fix). **Two things the change forced:** `arrayLValue` had to address a by-ref array parameter
  *in place* (its fall-through materializes the array into a fresh alloca, which would have
  reintroduced the copy for `mut [N]T` — the one site where the fix could have silently
  half-worked), and the seven `slot.(*ir.InstAlloca)` assertions became `slotElemType`, since a
  by-ref parameter's slot is an `ir.Param`, not an alloca. **Scalars are exempt and that is not
  a silent split:** a `mut` on a copied scalar stays by value, which is exactly the case
  `lyra-W010` already warns is inert — both now read one shared predicate
  (`types.IsCopiedScalar`) so the diagnostic and the lowering cannot drift. A scalar has no
  interior, and the only construct that could observe a by-ref scalar — whole-parameter
  reassignment (`n = n + 1`) — **doesn't lower for integers at all** (verified pre-existing:
  identical failure before and after this change, `getIntSignedness` finds no recorded type for
  the identifier); passing them by reference would change their ABI and reject `f(5)` for no
  observable gain. If that ever lowers, `paramIsByRef` and W010 should be revisited together.
  **Call-site enforcement (new):** `checkMutArgument` requires a `mut` argument to be an
  **lvalue rooted at a mutable binding**, sharing `rootBindingIsMutable` with
  `checkLValueAssignment` so passing-to-`mut` and writing-through-the-path obey one rule.
  Neither half was checked before: a temporary (`poke(Pt { x: 1 })`) silently discarded its
  writes, and a deeply-immutable `let` could be mutated through a call — the mutability system
  laundered by a function boundary. Forwarding a `ref` parameter into a `mut` one is now
  rejected for the same reason. **Also closes a leak:** `releaseOldTarget`'s "borrowed root"
  refusal existed precisely because the callee's copy shared the caller's reference; a by-ref
  slot *is* the caller's storage, so the overwritten managed value is a genuine reference to
  drop (`lvalueRootIsOwning` consults `byRefParams`), ASan-verified over repeated writes. Tests:
  `backend/llvm/llvm_mut_param_test.go` (8 exec cases — stack struct, `[N]T`, nested field,
  two-level forwarding, struct-in-array, plus the always-worked `[]T`/`shared` shapes and an
  lvalue-path argument; IR: the parameter is a pointer with no entry-block copy, `own` stays by
  value, `mut` scalar stays by value; ASan: a managed field renamed through a `mut` param is
  leak-free), `typechecker/tests/interior_mutation_test.go` (7 call-site cases incl. the
  `ref`→`mut` launder). Two existing tests asserted the bug — the "mut parameter callee"
  aliasing case (which expected the write to be *lost*) and the release-IR case expecting 0
  releases in the callee — and were rewritten to the corrected behavior.
- **Scanner: comments are no longer recognized *inside* string literals (silent code-swallowing
  fix).** Comments are `extras`, so `BLOCK_COMMENT` is valid at essentially every token boundary
  — including each string content-chunk boundary — and the scanner's comment branch ran *before*
  its in-string branch (`tree-sitter-lyra/src/scanner.c`). A string whose content began with
  `/*` therefore lexed as a **comment running to the next `*/` anywhere later in the file**:
  `let open = "/*"` followed by `let close = "*/"` collected `open` as a two-line string
  containing code, made `close` vanish entirely, and **`lyrac check` exited 0 with no
  diagnostic**. It fired wherever a fresh chunk starts — after the opening quote, right after a
  `${…}` interpolation, and (because `scan_block_comment` skips leading whitespace as token
  padding) after a leading space, `" /* note */ price"`. **Fix:** gate the comment scan on
  `!in_string(scanner)`. An interpolation is an expression context where comments are
  legitimate, and `in_string()` is already false for `CTX_INTERPOLATION`, so that distinction
  came for free. `//` was never affected (the content scan consumes it as ordinary bytes).
  **Bonus fix, same root cause:** the padding skip was also eating a content chunk's *leading
  whitespace* from the CST — `"${prefix} ${name}"` emitted its middle space nowhere, and one
  corpus test had encoded that loss as expected output. The CST is now byte-exact; the
  collector's raw-source re-slice (which already produced correct values) stays as the
  authoritative path. Tests: `tree-sitter-lyra/test/corpus/literals/string.txt` (openers/closers
  as content at each boundary, leading-whitespace case, `//` cases, mid-content `path/*.txt`,
  and real comments still parsing outside strings — incl. nested),
  `lyra/pkg/analyzer/collector/tests/string_whitespace_test.go` (exact values, plus "the
  following declaration still exists"). 370/370 corpus, full Go suite green after `go clean
  -cache`.
- **Regex literals can no longer span a newline.** `r/…/` is one token that outranks the
  identifier rule and `r` is a valid identifier, so `let ratio = r/2` (no spaces) starts
  something shaped like a regex; the content classes had no newline bound, so the token ran to
  the next `/` **anywhere later in the file** — including the first slash of a `//` comment —
  swallowing the intervening code into one literal with zero diagnostics. Excluding `\n`/`\r`
  from the content classes (`include/literals/regex.js`) bounds the damage to a single line and
  restores `r/2` at end-of-line as ordinary division. **Not removed**, despite the token being
  pure cost in expression position: `regex_literal` also backs `pattern(r/…/)` constraints on
  `newtype` and `regex_pattern` in match arms, and the constraint path is *implemented*
  (`pkg/regex` is a full DFA engine; the typechecker enforces `PatternConstraint`) — only the
  match-arm pattern form is unlowered. **Still open:** a same-line `r/2 + a/b` is mis-lexed; the
  real cure is a delimiter that can't collide with identifier-plus-division, which is a language
  design decision. Tests: corpus `A regex literal cannot span a newline`,
  `typechecker/tests/regex_test.go` (`r/2` + `a/b` on consecutive lines type-check as division).
- **Non-exhaustive `match` traps instead of running off `unreachable` (UB fix), and irrefutable
  aggregate patterns count as exhaustive.** Exhaustiveness is a hard error (`lyra-E009`) only
  for `bool` and `data`; for int/string/rune/float/array/tuple/struct it is a **warning**, and
  warnings never gate a build (`driver.Result.HasErrors` filters `SeverityError`) — so a
  fell-through match was reachable in a program that compiled clean, and every match ladder
  ended in a bare `unreachable`: **undefined behavior** (SIGTRAP/exit 133 at -O0, arbitrary
  under optimization), outside the language's own trap discipline. A **fully-guarded** match
  (`match x { y if y > 100 => … }`) reached that edge *deterministically*, since a guarded arm
  never seals the ladder. **Fix:** a new `sealMatchFallthrough` (`trap.go`) terminates the edge
  with the standard trap — `lyra: match not exhaustive` on stderr, exit 101, via the same
  `panicFunc` machinery as overflow/divide-by-zero/bounds — wired into all **four** fall-through
  sites: the scalar ladder (`match.go`), the aggregate ladder (`match_aggregate.go`), the array
  ladder (`match_array.go`), and the `data` tag-switch default (which is unreachable in a
  well-typed program thanks to E009, so there it is defense in depth costing one basic block).
  An exhaustive match emits no trap at all (pay-for-what-you-use, pinned by a test). **Second
  half — the false warning that trained users to ignore this class:** `match pair { (a, b) => …
  }` warned "not exhaustive" and demanded an unreachable wildcard, because tuple/struct
  exhaustiveness was just `hasUnguardedCatchAll`. A tuple/struct is *single-shape* (no tag to
  discriminate), so a destructuring arm whose every sub-pattern binds rather than tests is
  **irrefutable** and complete: new `patternIsIrrefutable`/`aggregateMatchIsExhaustive`
  (`typechecker_control_flow.go`), recursing through nested tuple/struct patterns and `name @
  inner` bindings, treating a shorthand field (`{ x }`, nil sub-pattern) as a binding leaf. This
  deliberately mirrors the backend's `aggPatternTest`, which returns a nil condition for
  precisely those patterns — "emits no runtime test" and "counts as exhaustive" are now the same
  judgment. A literal sub-pattern (`(0, b)`, `{ age: 30 }`) is still refutable and still warns;
  a guarded irrefutable arm still warns. **Also:** the six open-type exhaustiveness warnings
  carried the generic `lyra-E001` instead of `lyra-E009` (only the bool/data errors used the
  code) — now all eight are `lyra-E009`, so the diagnostic is filterable by code. **Deliberately
  unchanged:** the error-vs-warning split itself, which `diag.CodeNonExhaustiveMatch`'s doc
  comment documents as intentional (closed types error, open types warn) and which is now backed
  by defined runtime behavior rather than UB. Tests: `backend/llvm/llvm_match_trap_test.go`
  (exec: scalar fall-through traps with exit 101 after the matched arm ran; all-guards-fail
  traps; string/rune/float/dynamic-array/tuple/struct scrutinees each trap; IR: an exhaustive
  match emits no `lyra_panic_match_failed`, a non-exhaustive one defines and calls it;
  irrefutable aggregate destructuring runs trap-free), plus rewritten tuple/struct
  exhaustiveness tests (`match_expr_tuples_test.go`, `match_expr_structs_test.go` — the two that
  asserted the old false warning now pin irrefutable-is-exhaustive, with nested-irrefutable,
  literal-sub-pattern, and guarded-arm counterparts).
- **Int literal in a float slot — miscompile fixed (typechecker adaptation + backend float
  constant).** `let x: f64 = 5` type-checked (untyped int is assignable to floats) but no
  conversion was ever materialized: `propagateLiteralType` bailed on a float context ("handled
  by assignability, not here") and the backend's `literalIntType` fell back to i64 — an integer
  value in a float slot. `print(x)` on `let x: f64 = -5` printed 18446744073709551611 (print
  dispatches float-vs-int on the lowered LLVM type but signedness on the Lyra type → `%llu`),
  and `x + 0.5` emitted invalid IR (`@llvm.uadd.with.overflow.i64(i64, double)`) that `lyrac
  build` "succeeded" on and only clang rejected. Blast radius: every context that accepts an
  untyped int against a float type — annotation, call argument, struct field, data payload,
  return body, match arm, comparison sibling, reassignment. **Fix, two sides:**
  `propagateLiteralType`'s `IntegerLiteralExpr` case records the float type onto an untyped int
  literal in a float context (all nine context sites inherit it), and the backend's literal
  lowering emits a float constant at the recorded width when the typechecker adapted the literal
  (`literalRecordedFloatType`, `arithmetic.go`), keeping the i64 fallback otherwise.
  **Range-analysis companion:** the interval pass no longer tracks a float-adapted literal
  (`literalAdaptedToFloat`) — it's an *integer* analysis, and a float's runtime value can
  diverge from the source integer (f32 rounds 16777217 → 16777216), so a source-text interval
  would be wrong, not just imprecise; this also stops a spurious W011 on float comparisons (`let
  a: f64 = 5; a > 4`). Unchanged: mixed-kind literal unification still errors loudly (`if flag {
  3 } else { 4.5 }` against `-> f64` — untyped int and float literals don't unify), and an
  unannotated literal still defaults to i64. Tests:
  `typechecker/tests/float_literal_context_test.go` (recorded leaf types across annotation /
  nested arithmetic / f32 / negation / call arg / return / struct field / data payload /
  comparison, + the no-context default), `backend/llvm/llvm_float_literal_test.go` (IR: `alloca
  double`/`fadd double`/`fmul float`, no `with.overflow`; exec: the original print-garbage case
  now prints -5, plus arg/arithmetic/comparison and
  field/payload/if-through-return/compound-assign programs).
- **Deep-retain-on-copy — ownership becomes deep, closing the stack-aggregate use-after-frees
  and their leaks.** Every *owning-position* decision now asks `ownership.OwnsManaged(t,
  symTable)` ("does this value transitively own anything refcounted?") instead of `IsManaged`
  ("is this value itself a box"). A `struct Person { name: string }` is not itself managed, but
  a stack aggregate is a *value*: `let q = p` copies it and the copy points at the same string
  box. Treating that as uninteresting left the copy holding a reference nobody had counted —
  which was **not merely a leak**, since an uncounted alias is freed the moment the counted
  owner dies. Two ASan-confirmed use-after-frees, both now fixed: `let q = ps[0]` on a
  `[]Person` then letting the array die (the box's drop glue freed the struct's `name` out from
  under `q` — the reported bug), and interior assignment through one copy (`let q = p; p.name =
  …; q.name`, patched defensively on 07/28 by suppressing the release; now fixed at the root).
  **New `retain.go`** generates a cached `@lyra_retain_T(i8* payload)` per type — the exact
  mirror of `drop.go`'s glue — retaining every managed reference reachable *by value* from T,
  with the same "by value" stopping rule and the same termination argument (a recursive cycle
  must pass through a `shared` field, lyra-E014, which is managed and stops the walk). Both deep
  retain and deep release route through a **glue call** rather than inlining, because a `data`
  value's walk has to switch on its tag and copy sites are everywhere — the call keeps every
  site straight-line, so no CFG threading was needed at the ~6 hook sites. The backend's
  `needsDrop` now delegates to `OwnsManaged`, so the pass (which mints the +1) and the backend
  (which releases it) cannot drift apart — a divergence there is a leak or a double free.
  Framing extended from "managed LLVM value" to "owns managed" at `lowerVarDecl`, `own` params
  (`defineFunction`), var reassignment's release-old, the scope-frame releases, last-use drops,
  and temp flushes; `isManagedSlot`/`isManagedLLVMType` retired from that role. Match arm
  bindings and for-in loop variables stay **borrows** (unframed), as before. **Also re-enabled
  the 07/28 stopgap:** `releaseOldTarget` now permits the release for an inline target rooted at
  an *owning* binding (`lvalueRootIsOwning`/`slotIsFramed`), since copies carry their own +1 —
  closing the leak that the stopgap accepted. It still refuses for a **borrowed root** (a
  `mut`/`ref` param), whose by-value copy shares the *caller's* reference; that leaks instead,
  and is moot anyway because a by-value `mut` param's mutation never reaches the caller (the
  real fix there is by-reference `mut` params, a separate design item). Tests:
  `llvm_deep_retain_test.go` — 21 exec+ASan programs covering every copy site (bindings,
  nesting, tuples, fixed/dynamic arrays, aggregate-field init, array-literal elements, borrow vs
  `own` params, `data` payloads and the tag-switch glue, match arm bindings, for-in,
  reassignment, if-merges, reads out of `shared`/`[]T` boxes, owned temporaries, and a
  copy-and-reassign loop), plus `TestEmit_DeepRetainConservation` (an exact `alloc + retain ==
  release` accounting that walks glue call sites, since macOS ASan can't see leaks and `leaks`
  only reports *unreachable* memory — a never-freed box still on the stack goes unreported),
  `TestEmit_RetainGlueMirrorsDropGlue` (the two glues must cover the same fields), and
  `TestEmit_NoGlueForUnmanagedAggregate` (still pay-for-what-you-use).

### 07/28/26
- **Use-after-free fix: interior assignment through an aliased stack aggregate.** Copying a
  plain **stack** aggregate (struct/tuple/`[N]T`) duplicates the fat pointers of its managed
  fields with **no retain** — the ownership pass has no deep-retain-on-copy, and a stack
  aggregate is not a managed slot. But managed-target interior assignment (landed 07/27)
  *released* the overwritten value, so every other copy of the aggregate dangled. Three
  ASan-confirmed shapes, all compiling with zero warnings: `let q = p; p.name = …; q.name`
  (struct); `let ys = xs; xs[0] = …; ys[0]` (a `[2]string` element); and the worst — **no copy
  visible in the source at all** — a callee taking `p: mut Person` doing `p.name = …`, where the
  by-value parameter copy *is* the alias and the callee freed the *caller's* string. **Fix:**
  `lvalueAddress` now returns an `lvalueLoc{ptr, ty, viaBox}` recording whether the hop that
  reached the slot crossed a ref-counted box, and `lowerLValueAssignment` emits the release-old
  only when it did (`releaseOldTarget`, `lvalue.go`). The reasoning: *every* way of reading a
  managed value out of a container goes through the ownership pass's retain, so it yields its
  own +1 — the one unretained copy is a whole inline aggregate. A slot reached through a box has
  no such alias (copying a `shared` value or a `[]T` copies the box **pointer**, which is
  managed and therefore retained), so every copy names the same slot and overwriting it is
  ordinary aliasing. Only the **final** hop is consulted, since crossing into a box
  re-establishes that invariant — `p.arr[i] = v` on a stack struct holding a `[]string` still
  releases the element. Cost of saying no is a leak (the standing safety bias), and
  inline-aggregate managed values were never freed anyway. `[]string`/`shared
  [N]string`/`shared` struct fields stay fully leak-free. Chose this over deep-retain-on-copy
  (the real fix, below): that redesign must get *every* copy site right — bindings, args,
  returns, aggregate fields, array elements, match bindings, reassignment — and a single missed
  retain is another use-after-free, i.e. exactly the bug class being fixed. Tests:
  `llvm_managed_assign_test.go` — `TestExec_ManagedAssignment_AliasedStackAggregate` (all three
  shapes, plain + ASan) and `TestEmit_ManagedAssignmentReleaseIR` (pins the release present for
  box-interior targets, absent for inline ones, including the mixed `h.items[0]` case). Also
  corrected the stale "a leak, never a double free" claim for managed-in-stack-aggregates in
  `ALLOCATION.md`, `drop.go`, `ownership.go`, and both `CLAUDE.md`s — that invariant died when
  managed-target interior assignment landed.
- **[RESOLVED 07/29 by deep-retain-on-copy] Reading a stack aggregate by value out of a box is a
  use-after-free.** `let q = ps[0]` on a `[]Person` copies the `Person` out of the box by value,
  duplicating its `name` with no retain; when the box dies, the per-type drop glue frees `name`
  and `q` dangles (ASan-confirmed; pre-dates the interior-assignment work, and unaffected by the
  fix above since no assignment is involved). Not fixable the same way — *not* emitting the glue
  would reintroduce the leak it exists to close (freeing a list would leak the spine). **Only
  deep-retain-on-copy closes it**, which makes that item a correctness fix rather than the leak
  cleanup it has been filed as.

### 07/27/26
- **`shared` struct in an lvalue path (`p.x = v` on a `shared` struct) — completes interior
  assignment.** `memberFieldAddress` (`lvalue.go`) previously errored on a `shared` struct
  object; now it addresses the field *through the box* — `box → payload (field 1) → field idx`,
  reusing `lvalueBoxPtr` to load the box pointer — the write counterpart to how
  `lowerMemberExpr` reads a shared field. Because `lvalueAddress` recurses on the object, stack
  fields nested inside a shared struct (`ln.start.x` on a `shared Line`) fall out for free, and
  a **managed field of a shared struct** (`n.name = v` on a `shared Named`) is **fully
  leak-free** — the assignment's release-old frees the overwritten string and the box's drop
  glue frees the final field (unlike a *stack* struct, whose final managed field leaks). Tests:
  `backend/llvm/llvm_shared_member_assign_test.go` (exec: shared struct field, a stack Point
  nested in a shared Line, a managed string field; ASan: heap-string field of a shared struct —
  leak-free). The member-assignment deferral test (which expected a shared struct to error) was
  removed. With this, **interior assignment is complete across stack/shared/dynamic ×
  index/member × managed/non-managed targets**; only an *optional* member target (`p?.x = v`) is
  still a loud error.
- **Managed-target interior assignment (`xs[i] = "s"`, `p.name = "s"`) — release-old +
  own-new.** Previously a managed assignment target (a `string`/`shared`/`[]T` array element or
  struct field) was a loud error; now it lowers. **Ownership pass:** added an
  `LValueAssignmentStmt` case to the `stmt` handler (`a.expr(s.Value, a.isManaged(s.Value))`) —
  it was the one assignment form the pass didn't visit, so the RHS never got its +1; now a
  managed target's RHS is an owning position (retain a borrowed value / transfer an owned temp),
  exactly like managed `var` reassignment. **Backend** (`lowerLValueAssignment`): for a managed
  target, load the old value and `lyra_rc_release` it *before* storing the new (+1) one — and
  the new value is computed before the release, so a self-referential `xs[i] = xs[i] ++ y`
  (which reads the old element) is safe. **Memory accounting:** for a `[]string` / `shared
  [N]string` this is fully leak-free — the assignment frees the overwritten element and the
  array's drop glue frees the final ones (each element freed exactly once); for a `string` field
  of a *stack* struct (or a stack `[N]string`), the overwritten value is freed but the final one
  still leaks — the pre-existing stack-aggregate-managed-field leak, memory-safe (never a double
  free). Tests: `backend/llvm/llvm_managed_assign_test.go` (exec: `[]string` element, struct
  `string` field, self-referential concat; ASan: heap-string element overwrite + double-reassign
  of one element — no double-free/UAF); the array/member deferral tests dropped their
  now-working managed cases. **Deferred:** a `shared` struct in the assignment path.
- **`[]` empty array pattern (grammar) — the list-match base case.** `match xs { [] => …, [a,
  ...rest] => … }` previously failed to parse (the parser inserted a MISSING node for `[]` in
  pattern position). Fixed with a small grammar change: `array_pattern` uses `commaSep`
  (zero-or-more) instead of `commaSep1` (`tree-sitter-lyra/include/patterns/index.js`), plus an
  `[$.array_literal, $.array_pattern]` entry in the `conflicts:` array — `[]` is ambiguous
  between an empty array *literal* (expression) and *pattern*, so GLR keeps both alive until the
  surrounding position decides. The collector's `collectArrayPattern` no longer rejects a
  zero-element pattern (returns an empty `ArrayPattern`). **No backend change** —
  `lowerArrayPatternMatch` already treats `fixedCount=0, no rest` as a `len == 0` test, so `[]`
  matches a zero-length array for free. Tests: `tree-sitter-lyra` corpus (`Match empty array
  pattern`), `backend/llvm/llvm_match_array_test.go` (`TestExec_ArrayMatch_EmptyPattern`:
  `[]`→base, `[5]`→one-element, `[1,2]`→catch-all). Regenerated `parser.c` (363 corpus tests
  green). Note: this completes the array-match *pattern* surface except `[head, ...tail]`
  (blocked on the ownership-pass extension).
- **Mixed index+member interior assignment (`grid[i].y = v`, `p.arr[i] = v`, `m[i][j] = v`) —
  unified recursive lvalue.** Folded the array-index and struct-field assignment paths (both
  landed the same day) into **one** recursive `lvalueAddress` (`lvalue.go`) that walks any
  assignable path: an identifier root → its alloca; a `.field` hop → gep into the object's
  stack-struct storage; an `[i]` hop → gep to the array element (bounds-checked,
  negative-from-end, via `boundsCheckedIndex`) — a fixed-size array through the object's own
  storage, a `shared`/dynamic array through its box (loaded from the object's slot by
  `lvalueBoxPtr`, which avoids the ownership retain/release hooks since the write only mutates
  the box, taking no reference). Because `lvalueAddress` recurses on the *object* of each hop,
  index and member hops nest in any order and to any depth. `lowerLValueAssignment` collapses
  to: address the target, reject a managed target (deferred), lower + coerce + store. Tests:
  `backend/llvm/llvm_mixed_lvalue_test.go` (`grid[0].y` — field of an array element; `b.data[1]`
  — element of a struct's fixed array field; `v.items[1]` — element of a struct's *dynamic*
  array field, mutating the shared box; `m[0][1]` — a 2-D array); the earlier single-hop
  array/member tests still pass unchanged. **Deferred, loud errors:** a `shared` struct in the
  path, and a managed target type (needs release-old + retain-new). **Note:** a `[]T` field of a
  *stack* struct still leaks its box at scope exit (the pre-existing
  stack-aggregate-managed-field leak — memory-safe), unrelated to the assignment.
- **Struct-field assignment (`p.x = v`, nested `p.a.b = v`) lowers (backend).** Extends the
  `LValueAssignmentStmt` path (whose array-index case landed the same day) to a `MemberExpr`
  target. `lowerLValueAssignment` now dispatches to `lowerIndexAssignment` (arrays) or
  `lowerMemberAssignment` (structs), and a shared `storeLValue` does the value lower + coerce +
  store. `lowerMemberAssignment` uses a recursive **`lvalueAddress`** (`lvalue.go`) that walks
  an identifier root + `.field` hops, gep-ing down through the stack struct's storage to the
  target field's address, then stores in place (so a later read of the struct sees the
  mutation). Nested chains work because `lvalueAddress` recurses (`ln.start.x` → address of `ln`
  → gep `start` → gep `x`). The typechecker (`checkLValueAssignment`) already enforced the root
  binding is mutable (`var`/`let mut`) and the value matches the field type, and rejects a
  `readonly` field in the path — so the backend just computes the address and stores. Tests:
  `backend/llvm/llvm_member_assign_test.go` (single field, mutate-one-read-another, a nested
  `Line { start: Point }` chain, `let mut`; deferral errors); the array-assignment deferral test
  dropped its now-working `p.x = v` case. **Deferred, loud errors:** an array index *inside* a
  member path (`grid[i].y = v`, `p.arr[i] = v` — needs a unified recursive lvalue over both
  index and member hops), a `shared` struct/array in the path, and a *managed* field/element
  type (needs release-old + retain-new).
- **String indexing (`s[i]` → rune) lowers (backend).** The typechecker types `s[i]` as `rune`
  (any integer index, no compile-time bound); the backend had no string-index case. Now
  `lowerStringIndex` (`strings.go`, dispatched from `lowerIndexExpr` before the array cases)
  yields the i-th **rune**: since a string is UTF-8 and runes aren't randomly addressable, it
  walks from the front decoding one rune per step (reusing the `lyra_utf8_decode` shim the
  string for-in added) until the rune counter equals `i`, then yields that code point. **O(i)**
  — for a full traversal `for c in s` is the right tool; this is for occasional access. Running
  off the end before reaching `i` (which includes *any* negative index — the rune counter only
  grows, so there is no from-the-end form for a string, unlike an array) traps out-of-bounds via
  `lyra_panic_index_out_of_bounds`. Reading the string borrows it (no ownership action). Tests:
  `backend/llvm/llvm_string_index_test.go` (first/third/runtime rune; rune-indexing *past* a
  2-byte `café[3]='é'` and a 4-byte `a😀b[1]='😀'`, proving it's rune- not byte-indexed;
  out-of-bounds trap). **Deferred:** none for reads — this completes string/array element
  *access*; a from-the-end negative string index would need a full rune count first (not done).
- **Array element assignment (`xs[i] = v`) lowers (backend).** The front-end already
  type-checked interior-mutation statements (`checkLValueAssignment` — enforces the root binding
  is mutable: a `var`, `let mut`, or a `mut`/`own` parameter; checks the value against the
  element type); the backend had no `LValueAssignmentStmt` case. Now `lowerLValueAssignment`
  (`lvalue.go`) handles an `IndexExpr` target: it computes the element's address — a fixed-size
  array through its own alloca (`arrayLValue`), a `shared` array through its box payload
  (`sharedArrayPayloadPtr`), a dynamic array through its box payload — bounds-checks the index
  (negative-from-end, trapping via `lyra_panic_index_out_of_bounds`, through a new shared
  `boundsCheckedIndex` helper; a write target isn't marked by the value-range pass so the check
  is always emitted), and stores the (defensively width-coerced) value in place. Wired into
  `lowerBlockStmts`'s statement dispatch. Tests: `backend/llvm/llvm_lvalue_test.go` (exec:
  fixed-size constant/runtime/negative index, mutate-one-read-another, `shared` array, dynamic
  array, dynamic via a `mut []u8` parameter mutating the caller's box; bounds trap; deferral
  errors). **Deferred, loud errors:** a member target (`p.x = v` — struct-field write), a nested
  path (`grid[i].y = v`), and a *managed* element type (`[]string` — needs release-old +
  retain-new). **Note:** a static array is a value type, so mutating a *copy* (e.g. a by-value
  `[N]T` parameter) doesn't affect the caller — only a dynamic array (a shared box) or a
  `mut`/`own` reference propagates; the tests reflect that.
- **String for-in (`for c in s`) lowers (backend) — a rune walk, completing for-in.** A string
  iterable — the last for-in gap — now lowers to a UTF-8 rune walk (`lowerForInString`,
  `control_flow.go`): `bi = 0; while bi < byteLen { c = decode(data, bi); <body>; bi += n }`.
  Each iteration decodes one rune via a new runtime shim **`lyra_utf8_decode(i8* data, i64 pos,
  i32* cpOut) -> i64`** (`strings.go`, the inverse of the `lyra_rune_to_utf8` encoder — reads
  the lead byte's length class (1–4) and its continuation bytes, writes the code point, returns
  the byte count) and advances the byte index by that count, so a multibyte character is one
  iteration (not one per byte). Like the encoder it's unvalidated (rune's contract); well-formed
  UTF-8 — the only kind Lyra can build — never straddles the byte length, so the continuation
  reads stay in bounds. The byte-advance `n` is computed at the top of the body block, which
  dominates the continue/increment block, so `bi += n` is valid on both the fall-through and
  `continue` paths. The rune loop variable is a plain i32 (not a borrow — nothing to free).
  Tests: `backend/llvm/llvm_forin_string_test.go` (rune counts for ASCII, a 2-byte `café`→4, a
  4-byte `a😀b`→3; last-rune binding; a 2-byte rune decodes to the right code point (`'é'`);
  break); the for-in deferral test now covers only the two-variable-over-string form.
  **Deferred:** the two-variable form over a string (`for i, c in s` — the index/rune pairing
  isn't defined).
- **Range for-in (`for i in START..<END`) lowers (backend).** A numeric-range iterable —
  previously a loud error — now lowers to a counter loop (`lowerForInRange`, `control_flow.go`):
  `i = START; while i </<= END { <body>; i += step }`, reusing the C-style loop's break/continue
  machinery. `..<` is an exclusive end (`i < END`), `..<=` inclusive (`i <= END`); an optional
  `:step` (grammar `START..<END:STEP`) sets the stride, default 1. The **counter is the loop
  variable** (a plain integer value, not a borrow, and immutable so never re-stored by the
  body). Its **width** is the first concrete-integer bound's type (End, then Start, then Step),
  else i64 — `rangeIntType`, mirroring the typechecker's `iterableElementType` so the counter
  matches the loop variable's declared type — and the bounds/step are `coerceIntWidth`'d to it
  (so `0..<n` with a u8 `n` runs at u8, an untyped `0..<3` at i64). The comparison predicate is
  signed/unsigned per the counter type. The increment is a **plain (wrapping) add**, so an
  inclusive `..<=` whose end is the counter type's max loops forever (the increment wraps past
  it) — the one documented edge; no two-variable form over a range. Tests:
  `backend/llvm/llvm_forin_range_test.go` (exclusive/inclusive sums, variable end, typed-u8
  bounds, `:2` step, break, continue, and the `for i in 0..<xs.len() { sum += xs[i] }` indexing
  idiom); the for-in deferral test now covers only a **string** iterable (the last for-in gap).
  **Deferred:** a string for-in iterable in the backend.
- **Two-variable for-in (`for i, x in xs`) — index + element.** The backend previously errored
  on the two-variable form; `lowerForInLoop` now binds the loop counter as the index `i` (i64)
  in addition to the element `x`. The collector puts the first name in `Key` (the index) and the
  second in `Value` (the element) — the single-variable form leaves `Value` empty (`Key` is then
  the element) — so the backend resolves `elemVar`/`indexVar` from that and, each iteration,
  stores `arr[i]` into the element slot and the counter into a separate index slot (both borrows
  of the loop state, not framed). The typechecker already typed both (`bindForInLoopVars`: index
  → i64, value → element type). Tests: `backend/llvm/llvm_forin_test.go`
  (`TestExec_ForIn_TwoVar` — `i*x` over a dynamic array, `i+x` over a fixed array, and the index
  reaching the last position); the deferral test now covers only the still-deferred range/string
  iterable. **Deferred:** a non-array for-in iterable (range/string) in the backend.
- **`.len()` on arrays — a compiler-provided builtin.** `xs.len()` on any array (fixed-size or
  dynamic) returns its length as `i64` (signed so it composes with the negative-from-end index
  arithmetic). Registered in `typechecker/builtins.go`'s `builtinMethodSignature` for any array
  receiver (`types.IsArray`), consulted last like the other builtins so a user method shadows
  it; the backend `lowerArrayLen` (`dynarray.go`, dispatched from `lowerBuiltinMethodCall`)
  returns the compile-time size for a `[N]T` (lowering the receiver for effect in case it has
  one, e.g. `makeArray().len()`) and loads the box's `len` field for a `[]T`. Reading the length
  **borrows** the array (no reference consumed), so there's no ownership action on the receiver.
  Tests: `backend/llvm/llvm_len_test.go` (exec: dynamic / empty / fixed / `shared` fixed
  lengths, and the practical `for var i = 0; i < xs.len(); i += 1 { … xs[i] … }` index-loop
  idiom), `typechecker/tests/array_len_test.go` (acceptance on all array kinds + composing in
  arithmetic; `.len()` on a non-array errors). **Context:** chosen as a safe, self-contained
  pivot after finding the recursive-list idiom's `[head, ...tail]` blocked on an ownership-pass
  extension (see that item's Open note).
- **`match` on a dynamic array `[]T` lowers (backend) — first slice.** The front-end already
  type-checked array patterns (length, literal elements, bindings, rest, exhaustiveness); the
  backend previously errored on an array scrutinee. Now
  `lowerArrayMatch`/`lowerArrayPatternMatch` (`match_array.go`) lower it as an if-else ladder —
  the array analogue of `lowerScalarMatch` — where each `[...]` arm is a **length test** (`len
  == fixedCount`; a lone `[...rest]` matches any length) followed by per-element literal/range
  tests (reusing `scalarMatchTest`), first-match-wins into a merge phi. **In-bounds by
  construction:** the element tests/bindings run in a block reached only *after* the length test
  succeeded, so an `xs[i]` in a pattern is never an out-of-bounds read. **Bindings are
  borrows:** an element binding (`[a, b]`) and a whole-array `[...rest]` bind into `l.locals`
  but are *not* framed for release — reading an element or aliasing the whole array consumes no
  reference (the scrutinee's own binding owns the storage), the same borrow treatment as the
  for-in loop variable, so a `[]string` match is memory-safe. Tests:
  `backend/llvm/llvm_match_array_test.go` (exec: length dispatch `[a]`/`[a,b]`/catch-all,
  literal elements `[1,2]`/`[3,4]` incl. right-length-wrong-element fall-through, `[...rest]`
  binds-whole; `[]string` element read under ASan; `[head, ...tail]` deferral error).
  **Deferred, loud errors:** a `[head, ...tail]` pattern binding a *tail sub-array* (needs an
  alloc+copy), a rest not last, a nested non-scalar element pattern, and a fixed-size-`[N]T`
  scrutinee. **Found in passing (grammar gap, not fixed here):** an `[]` *empty* array pattern
  doesn't parse (the parser inserts a MISSING node) — so the recursive-list base case `[] => …`
  isn't expressible; it pairs with the deferred `[head, ...tail]` for the full recursive idiom.
- **`for x in <array>` iteration lowers (backend) — the first for-in codegen.** The backend
  previously had no `ForInLoopExpr` case (loud error); now `lowerForInLoop` (`control_flow.go`)
  lowers `for x in <array>` as an index-counter loop — `i = 0; while i < len { x = arr[i];
  <body>; i++ }` — over a fixed-size array (`[N]T`, stack or `shared`; length = the compile-time
  size, elements via `arrayLValue` / `sharedArrayPayloadPtr`) or a dynamic array (`[]T`; length
  = the box's runtime `len`, elements gep'd through the box payload), reusing the C-style loop's
  `loops` stack so break/continue resolve. The element source and length are materialized once
  before the loop (they dominate the body). **The loop variable borrows each element** — bound
  into `l.locals` but *not* framed for release: reading an element consumes no reference, and
  for a managed element type the array frees it when the array itself dies (matching the C-style
  loop, the ownership pass does not recurse into loop bodies; managed values *declared inside*
  the body are framed per iteration by the ordinary block machinery). **Typechecker
  prerequisite** (`typechecker_control_flow.go`): `checkForInLoopExpr` now types the loop
  variable from the iterable's element type (`bindForInLoopVars`/`iterableElementType` — an
  array's element, a string's `rune`, a range's numeric type) — before this the loop variable
  resolved in scope but had *no recorded type*, so a body use of it (`println(x)`, `sum += x`)
  couldn't lower. A range over *untyped* bounds keeps the loop variable untyped (assignable to
  any int width) rather than defaulting to i64, so `for i in 0..<3 { t: u8 = i }` still binds
  `i` to `t`'s width (fixing `TestAnalyze_ForInLoopVariableResolves`, which the naive i64
  default broke). Tests: `backend/llvm/llvm_forin_test.go` (exec: fixed / dynamic / `shared`
  array accumulate, break, continue, empty array; println-each order; `[]string` element read
  under ASan; two-var + range deferral errors). **Deferred, loud errors:** the two-variable
  index form (`for i, x in xs`) and a non-array iterable (range/string) in the backend.
  **Ergonomic note found in passing:** a trailing one-armed `if` in a loop body is rejected (the
  body's last statement is checked as a value) — pre-existing, shared with the C-style loop; not
  fixed here.
- **Managed-element dynamic arrays (`[]string`, `[][]T`) — the drop-glue extension of the `[]T`
  slice.** A `[]T` whose element type is itself managed (a `string`, a nested `[]T`, a `shared`
  value) previously errored loudly at construction (element drop glue deferred); now it lowers
  and frees its elements. **`dynArrayDropFn`** (`dynarray.go`) generates, once per element type
  and cached, the box's `drop_fn`: it receives the box payload (`box + rcHeaderSize` = `{ i64
  len, [0 x T] }`, per `lyra_rc_release`'s contract), loads `len`, and **loops** releasing each
  element via `emitDropValue` — the dynamic-length counterpart to a fixed `shared [N]T`'s
  *unrolled* `emitDropArray`. Routed via a new `DynamicArrayType` case in `boxDropFn` (null when
  the element owns nothing managed, so a `[]i64` still just frees its box). No ownership-pass
  change was needed: the `ArrayLiteralExpr` case already transfers each element's reference into
  the array, so the box owns them and the loop frees each exactly once. **Testing note:** a
  looped drop makes a static release-*site* count unable to express conservation (one call site
  runs `len` times), so the managed-element leak-side check is *structural* (assert the drop
  glue is generated, loops, and is passed as a non-null `drop_fn`) plus an ASan run for
  double-free/UAF — the unrolled `shared [N]T` case keeps its exact static count. Tests:
  `backend/llvm/llvm_dynarray_test.go` (exec: index a `[]string` element, nested `[][]i64`
  double-index; ASan: `[]string` of heap strings + `[][]i64`; IR: drop-glue loop + non-null
  drop_fn). **Deferred:** iteration, `match` on `[]T`, `.len()`, growth.
- **Dynamic arrays (`[]T`) — first backend slice (construction, indexing, ownership).** The
  front-end already type-checked `[]T` (annotate, build from a literal incl. empty, index,
  iterate, match, pass/return); this lands the codegen. **Representation** (`dynarray.go`,
  `DynArrayBoxType`): a `[]T` is a `ptr` to a ref-counted box `{ i64 rc, i64 len, [0 x T] }` — a
  *single box pointer*, chosen over a `{ data, len }` fat pointer precisely so it reuses the
  shared-value managed machinery **unchanged**: the value is a pointer, so `ownership.IsManaged`
  covers `[]T`, `managedBox` bitcasts it, and retain/release/last-use/drop act on it exactly
  like a `shared` value (no new managed-dispatch code). `lowerType` maps `[]T` → the box pointer
  *before* the `shared`-strip (a dynamic array is inherently heap-boxed regardless of flavor).
  **Construction** (`lowerDynArrayConstruction`): alloc a box sized `rcHeaderSize + 8 +
  N*stride`, store the length (field 1) and each element into the `[0 x T]` flexible tail (field
  2) — an empty `[]` still allocs a len-0 box, so every `[]T` is a uniform managed box (no null
  special case). **Indexing** (`lowerDynArrayIndex`): load the runtime `len`, apply the same
  negative-from-end + unsigned-`>=`-bound trap as a fixed array but against `len` (always
  checked — the value-range pass doesn't track dynamic lengths), then GEP+load. **By-value
  flow** through `let`/params/returns is the ordinary pointer path (`emitReturn`'s pointer
  case). **Typechecker fix** (the one real bug): a `[]T` **return-body / arg** literal was
  recorded as a `StaticArrayType` (lowered to an inline `[N x T]`, so `() -> []i64 => [1,2,3]`
  returned `[3 x i64]` from a box-pointer function → clang rejected it) — `propagateLiteralType`
  re-recorded only the *static* context, relying on `checkVarDecl` to record the dynamic type,
  which happens for a `let` but not a return/arg; now it re-records the *dynamic* case too (kept
  dynamic, never rewritten to static, so the dynamic→static assignment error is preserved).
  **Verified** under AddressSanitizer across construction, indexing, an aliased copy (retain, rc
  1→2→1→0, no double-free), and scope-exit free, with a static alloc+retains==releases
  conservation check. Tests: `backend/llvm/llvm_dynarray_test.go` (exec: construct +
  constant/runtime/negative index, pass-to-callee, return a `[]T`, empty array, move-copy;
  bounds trap; IR box `{ i64, i64, [0 x i8] }` + alloc/release balance; managed-element deferral
  error; two ASan cases). **Deferred, loud errors:** a *managed* element type (`[]string` — the
  box's drop glue must loop over `len` to release each element; errored at construction),
  iteration (`for x in xs`), `match` on `[]T`, `.len()`, and growth (no grow operation exists in
  the language — the one append-shaped syntax, `[...xs, v]` spread, isn't even type-checked).
  This is the second of the three parts of the arrays/reuse backend area (after `shared`
  arrays); Perceus stage 4 remains.
- **`shared` arrays (`shared [N]T`) lower end to end — the foundation slice of the arrays/reuse
  backend work.** A `shared`-flavored fixed-size array is now a heap-boxed, ref-counted value,
  reusing the whole `shared`-box machinery (alloc / retain / release / drop glue) rather than
  new plumbing. **Representation:** `lowerType` already mapped `shared [N]T` to a `ptr` to `{
  i64 rc, [N x T] }` (the generic `shared`-box path); this slice wires construction, indexing,
  and element drop. **Front-end (1 line + ordering):** `propagateAllocation` gained an
  `*ast.ArrayLiteralExpr` case so a `shared [N]T` annotation stamps the flavor onto the
  array-literal construction node (it runs *after* `propagateLiteralType`, which re-records the
  literal as a flavorless `StaticArrayType` — `checkVarDecl` already orders it that way, so the
  stamp survives). **Construction** (`lowerArrayLiteralExpr`, `arrays.go`): build the inline `[N
  x T]` as before, then `lowerBoxShared` it when the recorded type is `shared` — the exact
  mirror of `lowerStructInstanceExpr`. **Indexing** (`lowerIndexExpr` + new
  `sharedArrayPayloadPtr`): a `shared` array is a box pointer, so gep to the payload (`box →
  field 1`) and index through it — a constant index is a bare gep+load (typechecker
  range-checked it), a runtime/negative index keeps the from-the-end adjustment + bounds trap,
  all through the box; reading through the box *borrows* it (no reference consumed), so the
  binding's own release still fires at scope exit. **Drop glue** (`drop.go`):
  `needsDrop`/`emitDropValue` gained `StaticArrayType` cases (`emitDropArray` — N is constant,
  so element drops are unrolled via `extractvalue`), so a `shared [N]String` frees each element
  when the box dies. **Ownership** (`ownership.go`): added an `ArrayLiteralExpr` case to the
  `expr` classifier so array-literal elements are owning positions (transfer, mirroring
  tuples/structs) — *necessary*, since without it a managed element flowing into a `shared`
  array would be freed both by its own binding and by the box's drop glue (double free); a stack
  array's managed elements leak conservatively, matching tuples/structs. `IsManaged` already
  covered `AllocationOf == Shared`, so the pass frames the binding with no other change.
  **Verified** under AddressSanitizer with a static allocations==releases conservation check.
  Tests: `backend/llvm/llvm_shared_array_test.go` (exec: construct + constant/runtime/negative
  index, borrowed-param index in a callee, return a `shared` array, bounds trap; IR: box `{ i64,
  [3 x i8] }` + alloc/release balance; ASan: managed-element `shared [2]string` frees each
  element with 3==3 conservation), `typechecker/tests/shared_array_test.go` (acceptance in every
  position + the construction-flavor stamp). **Deferred:** dynamic arrays (`[]T`), `match` on a
  `shared` array (the element-wise array pattern isn't lowered), and Perceus stage 4 — the other
  two parts of this backend area.
- **`i128`/`u128` — the MVP slice (change-set steps 1 + 2b + 4 + 5), end to end.** The two
  128-bit integer types now lower and run: annotate a binding, do checked arithmetic, convert,
  `match`, and `print`. **Front-end (step 1):** `types.Int128`/`UInt128` added
  (`primitives.go`), and the by-hand width/signedness/numeric enumerations extended to include
  them — `types.IsNumeric`, `assignable.go`'s
  `numericPrimitiveByName`/`isAnyConcreteSignedInt`/`isAnyConcreteUnsignedInt` (so
  assignability, `numericResultType`, and `checkIntegerLiteralRange` treat them like any
  concrete int), the backend `layout.go` tables (`LLVMPrimitive` → `i128`, `IsSignedInt`,
  `IsNumericConversionTarget`, `primitiveSizeAndAlign` → 16/16), `purity.go`'s
  `isTypeConversionCall`, and the grammar (`number_types.js`: `i128`/`u128` added to
  `signed_/unsigned_integer_type`). **Literal storage (step 2b, MVP):** unchanged —
  `IntegerLiteralExpr.Value` stays `int64`, so a 128-bit value is reached via arithmetic or an
  `i128(x)`/`u128(x)` conversion of an i64/u64-range operand (`inferTypeConversion` now lets an
  unsigned literal in `(int64max, u64max]` target `u128` as well as `u64`); a true >64-bit
  literal is still unrepresentable (step 2a, open). **Backend arithmetic (step 4):** free by
  width — `add`/`sub`/`mul`/`icmp`, the `llvm.{s,u}{add,sub,mul}.with.overflow.i128` trap
  intrinsics, and `coerceIntWidth` all key off the operand bit width; division and floored `%%`
  lower to `udiv`/`sdiv`/`urem`/`srem i128`, which clang resolves against **compiler-rt**
  (`__divti3`…). **Correctness fix the plan didn't call out:** the `INT_MIN`/`INT_MAX` trap and
  saturating-mul-bound constants were built as `1 << (BitSize-1)`, which yields **0** for a
  128-bit width (the untyped `1` is Go's 64-bit `int`, so the shift falls off the end; it only
  worked at i64 by two's-complement wraparound). Introduced `intMinConst`/`intMaxConst`
  (`intconst.go`) that build the bound with `big.Int` — correct at every width — and rewired the
  three sites (`emitCheckedDivOp`'s `INT_MIN÷-1` guard, `lowerNegationExpr`'s `-INT_MIN` guard,
  `emitSaturatingMul`'s bounds). **Print (step 5):** there is no printf length modifier for 128
  bits, so `lyra_i128_to_str` (`print.go`, lazily defined like `lyra_rune_to_utf8`) formats by
  repeated `udiv`/`urem` by 10 — taking the magnitude as the *unsigned* bit pattern
  `select(isNeg, 0-val, val)` so it is correct even for `INT_MIN` (whose negation wraps to the
  right unsigned magnitude, 2^127) — and `formatForPrint` routes an i128/u128 value to it.
  **Range analysis:** unchanged and sound — `intBounds` returns `ok=false` for i128/u128 (their
  bounds don't fit int64), so they widen to ⊤ (untracked), the same conservative treatment
  `u64`'s upper half already had; a diagnostic can only be missed, never invented. Tests:
  `backend/llvm/llvm_i128_test.go` (exec: exit-code via i128→u8 narrowing, big multiply
  exceeding i64/u64 formats correctly, large negative, zero, u64-max via conversion, division
  via compiler-rt; IR shape), `typechecker/tests/i128_test.go`
  (annotation/arithmetic/conversions, `u128` rejects a negative, no implicit i64↔i128 widening),
  `tree-sitter-lyra` corpus (`assignments.txt` 128-bit annotations). **Still open:** step 2a
  (widen the literal node to `big.Int`/hi-lo so a >64-bit constant is writable) + step 3
  (128-bit compile-time folding in `typechecker/overflow.go`), both deferrable until a large
  literal constant is first needed.

### 07/26/26
- **for…in range widening — the last item in the value-range backlog, plus a latent for-in
  scope-bug fix.** A `for i in START..<END` (or `..<=`) over a numeric range now binds its loop
  variable to the range interval in the body (`forInRangeKey` in `checker/range_analysis.go`),
  the for-in analogue of the C-style loop counter — but with no fixpoint, since the range gives
  the bounds directly. So `for i in 5..<10 { xs[i] }` on a size-3 array is a definite
  out-of-bounds (E022), `for i in 0..<3 { xs[i] }` elides its bounds check (`IndexInBounds`),
  and an inclusive `0..<=2` bounds to `[0,2]`. **Sound via a provably-non-empty guard:** the
  variable is bound (enabling body diagnostics) only when `start.hi < end.lo` (`..<`) /
  `start.hi <= end.lo` (`..<=`), so the loop definitely runs ≥ 1 iteration and a body diagnostic
  — which holds for the whole widened `[start.lo, end.hi]`, hence at the first iteration `i =
  start` — is genuinely definite, not a maybe-empty false positive. A variable-length range
  (`0..<n`, not provably non-empty), a stepped range, a two-variable form, or a non-range
  iterable (array/string, whose element/index we don't track) still havocs. **Prerequisite bug
  found + fixed:** the for-in **loop variable didn't resolve in the body** —
  `checkForInLoopExpr` (`typechecker_control_flow.go`) type-checked the body without entering
  the loop's scope (unlike `checkForLoopExpr`), so every use of the loop variable was an
  "undefined identifier"; no existing test exercised a non-empty for-in body, so the gap went
  unnoticed. Now it enters the scope. (The backend doesn't lower `for … in` yet, so the widening
  is a front-end diagnostic/elision feature — the `IndexInBounds` mark is computed and ready for
  when for-in codegen lands.) With this, the value-range analysis's original deferred list is
  fully cleared. Tests: `checker/range_analysis_test.go` (range index past-end E022, in-bounds +
  inclusive-range no-diagnostic, variable-length no-false-positive, `SafetyTable` marks a for-in
  range index in-bounds), `driver/driver_test.go` (for-in E022 through the pipeline; the loop
  variable now resolves).
- **Flow-sensitive `RangeConstraint` enforcement — the non-constant twin of the E023 constant
  check.** The value-range analysis now catches a *non-constant* value proven entirely outside a
  range-constrained newtype's range, extending the same `lyra-E023` from the typechecker's
  constant-only check to flow-proven variables: `if x > 100 { let p: Percent = x }` (x ∈
  [101,255] entirely outside [0,100]) and `let y = 150; let p: Percent = y` (constant-propagated
  through a binding the typechecker can't fold) are now errors (`checker/range_analysis.go`
  `checkConstraintViolation`, fired from the `VarDeclStmt` case). **How the target constraint
  reaches the range pass:** the typechecker stamps the resolved annotation type onto the
  assigned value node (`checkVarDecl` → `tt.Set(decl.Value, resolvedDeclType)`), so `tt.Get(x)`
  for `let p: Percent = x` recovers the `*ConstrainedType`; the check reads the
  `RangeConstraint` bounds from it (folding literal/negated-literal bounds, honoring
  `..<`/`..<=`/open-ended, replicated from the typechecker's folder). **Scoped to an *identifier*
  value** — the typechecker's `checkRangeConstraints` folds and owns literal/constant values,
  and an identifier is exactly what it can't fold, so restricting to a variable both avoids a
  double report (verified: a literal `let p: Percent = 150` yields exactly one E023, the
  typechecker's) and captures the value-add (a variable refined by flow, or bound to a
  constant). Definite-only, zero-false-positive like every other range diagnostic — a value that
  *might* be out of range (a full-range param) is left to the runtime. Only the annotated-`let`
  site is covered (that's where the target type is stamped; a plain reassignment's non-constant
  case isn't, and is noted). Tests: `checker/range_analysis_test.go` (refined-var violation
  showing `[101, 255]`, constant-propagated-binding violation, refined-in-range +
  possible-not-definite no-diagnostic), `driver/driver_test.go` (flow-sensitive E023 through the
  pipeline; a literal violation reported exactly once).
- **`RangeConstraint` enforcement on `newtype … where range(…)` — the constant-value case
  (`lyra-E023`).** A range-constrained newtype's constraint was collected by the front-end but
  *never checked* against actual values — `let p: Percent = 150` for `newtype Percent = u8 where
  range(0..<=100)` compiled clean. Now a compile-time numeric constant assigned or annotated to a
  range-constrained newtype is checked against the declared range: `checkRangeConstraints`
  (`typechecker/range_constraint.go`), the numeric analogue of the existing string
  `checkPatternConstraints`, wired into the three value sites (`checkVarDecl`,
  `checkVarReassignment`, member-assign). It folds a constant value (int or float literal, incl.
  a negated one / a folded arithmetic constant) and the constraint's literal/negated-literal
  bounds, honoring inclusive start, `..<` exclusive vs `..<=` inclusive end, and open-ended
  bounds (`0..`, `..<=100`); an unfoldable identifier/compound bound leaves that side unenforced
  (conservative — no false positive). Covers both integer and float base types (`newtype Ratio =
  f64 where range(0..<=1)`). **No double report:** `checkIntegerLiteralRange` skips a
  `*ConstrainedType` (it matches only a bare `PrimitiveType`), and a range constraint is
  normally ⊆ the base type, so a range violation subsumes any base overflow. Definite-only, like
  the literal integer range check — a *non-constant* value (`let f = (x: u8) -> Percent => x`)
  is left to the runtime / a future flow-sensitive pass. New code `lyra-E023`. Tests:
  `typechecker/tests/constrained_type_test.go` (int above/below, exclusive vs inclusive
  boundary, open lower/upper bound, negative start, float over/in-range, non-constant no-error,
  reassignment), `driver/driver_test.go` (E023 through the pipeline).
- **Per-match-arm scrutinee refinement — the `match` analogue of `if`-branch refinement.** A
  `match` on a tracked integer *variable* now refines the scrutinee, per arm, to the values that
  arm's pattern matches — a literal (`0 => …` → `[0,0]`) or a numeric range (`1..<=10`, `0..<3`),
  extracted by `patternInterval` (mirroring the typechecker's exhaustiveness reader
  `armIntInterval`, so the two agree on what a pattern covers; guards are irrelevant to the
  refinement). `evalMatch` intersects that with the scrutinee's current interval in the arm's
  env (`refineScrutinee`); an empty intersection makes the arm **unreachable** (skipped — no
  value, env, or diagnostics), and a catch-all / identifier / non-numeric pattern refines
  nothing (the arm sees the full range — sound). This lets every range-analysis conclusion fire
  inside a constraining arm: `match x { 100..<=127 => x + 100 }` on an i8 → E020, `match x { 0 =>
  a / x }` (scrutinee *is* the divisor) → E021, `match i { 5..<=10 => xs[i] }` on a size-3 array
  → E022, and an in-range arm elides its checks (`match i { 0..<=2 => xs[i] }` →
  `IndexInBounds`). The post-match env still unions with the pre-match state, so the scrutinee's
  range after the match is unchanged (sound). No new false positives (an in-range arm and a
  catch-all arm both stay clean); full suite green. Tests: `checker/range_analysis_test.go`
  (arm-range overflow E020, arm-literal divide-by-zero E021, arm-range + exclusive-range OOB
  E022, in-range/catch-all no-diagnostic, `SafetyTable` marks a match-refined in-bounds index),
  `backend/llvm/llvm_elision_test.go` (`TestExec_MatchRefinementElisionPreservesResults` — a
  match-refined elided index reads the right element).
- **u64 tracking — the last untracked integer type, now tracked with a +∞ upper sentinel.**
  `u64` was the sole integer type the value-range analysis skipped, because its true max
  (2^64-1) overflows the `int64` the interval bounds are stored in. Rather than a full dual
  `uint64` domain (a large refactor), `intBounds(UInt64)` now returns `[0, posInf]` where
  `posInf = math.MaxInt64` is a **+∞ sentinel**: a sound over-approximation of the true `[0,
  2^64-1]`. **Why this is sound** (the load-bearing argument): the *exact* lower bound of 0
  drives every u64 diagnostic — `x < 0` → always-false W011, a refined `x >= size` index → E022,
  a subtraction proven below 0 → E020 underflow — while the fake upper is only ever consumed in
  ways that stay conservative (the interval arithmetic `addI`/`subI`/`mulI` are
  int64-overflow-guarded, so any u64 op that could reach the real upper half overflows the int64
  computation → untracked; elision's "entirely within" test then can't fire for it). The one
  place the fake upper *would* be unsound is `compareConst` (`x > MaxInt64` on a u64 is
  genuinely satisfiable and must not fold to always-false), so it gained **sentinel guards** — a
  fold never treats a ±∞ bound as a finite separator
  (`upperFin`/`lowerFin`/`finiteSingle`/`disjoint`); this changes nothing for i8..u32 (their
  bounds are never the sentinels) and only suppresses folds keyed off a u64 +∞ or an i64 extreme
  (sound — it can only *remove* a warning). Diagnostics print a sentinel as `+∞`/`-∞`
  (`fmtBound`) rather than the misleading `9223372036854775807`. Empirically grounded first
  (probed the four diagnostic kinds on u64 before/after). ASan-clean, full suite green — the
  behavioral tests confirm no wrong elision (a full-range `u64` `a - b` given `5 - 10` still
  traps; a proven-safe `100 - 58` elides and yields 42). **Not done** (a genuine dual `uint64`
  domain would add): precise tracking of u64 values in the upper half `(2^63, 2^64-1]`, and
  near-2^64 overflow *detection* (unreachable with int64 arithmetic — the interesting overflow
  cases self-untrack). Tests: `checker/range_analysis_test.go` (u64 `>= 0`/`< 0` const
  comparisons, refined-index E022 showing `+∞`, underflow E020, the `x > MaxInt64` soundness
  non-warning, full-range-add no-FP, u64 div-by-zero), `backend/llvm/llvm_elision_test.go`
  (`TestExec_U64_ElisionSoundness`).
- **Definite out-of-bounds diagnostic (`lyra-E022`) — the range analysis's error-reporting twin
  of the array-bounds elision.** Completes the diagnostic/elision symmetry across all three
  range facts (overflow E020, div-by-zero E021, bounds E022). An `xs[i]` whose index range is
  proven *entirely outside* `[-size, size)` (a negative index counts from the end) on a
  reachable path is a guaranteed runtime bounds trap, now caught at compile time (`evalIndex` in
  `checker/range_analysis.go`, folded into the same index handling that marks `IndexInBounds`
  for elision). **Scoped to a *non-singleton* index range** (`if i >= size { xs[i] }`, an
  arithmetic/loop-derived range) — this is the key difference from E021's *identifier* scoping:
  the typechecker's own constant-index check (`inferIndexExpr` via `resolveConstantInt`)
  resolves an index to a *single* constant and **looks through let-bindings** (`let i = 5;
  xs[i]` is already its error, unlike the div case), so restricting E022 to a non-singleton
  range provably means that check didn't fire (a resolvable constant always yields a singleton
  `[k,k]`) — no double report, while still catching the flow-proven range case constant-folding
  can't see. Same definite-only, zero-false-positive bias as E020/E021 (a merely *possible* OOB
  — a full-range index — is left to the runtime trap; an unreachable branch reports nothing). No
  existing test needed changing: every runtime-OOB-trap test already passes the bad index
  through a full-range parameter (which intersects the valid range, so E022 correctly doesn't
  fire). Tests: `checker/range_analysis_test.go` (E022 via positive `i >= size` and negative `i
  < -size` refinement; none for a possible/refined-in-bounds index), `driver/driver_test.go`
  (E022 flows through the pipeline; a constant `xs[5]` is reported once, by the typechecker,
  with no duplicate E022).
- **Definite divide-by-zero diagnostic (`lyra-E021`) — the range analysis's error-reporting twin
  of the divide-by-zero elision.** Symmetric with `lyra-E020` (definite overflow): a
  `/`/`%`/`%%` whose divisor is proven *always zero* on a reachable path is a guaranteed runtime
  trap, now caught at compile time (`checkDivision` in `checker/range_analysis.go`, folded into
  the same div handling that marks elision safety). **Scoped to an *identifier* divisor proven
  `[0,0]`** — a literal/folded-constant zero (`5 / 0`, `10 / (5-5)`) is already the
  typechecker's own constant-fold check, so restricting E021 to a variable both avoids a double
  report *and* captures exactly this pass's value-add: the *non-constant* divisor proven zero by
  flow (`let b = 0; a / b`, or the flagship `if b == 0 { a / b }` via branch refinement — which
  constant-folding can't see). Same definite-only, zero-false-positive bias as E020 (a merely
  *possible* zero divisor is left to the runtime trap; an unreachable branch reports nothing).
  Float division is untracked (not an integer trap). Error, not warning — a definite
  divide-by-zero is as fatal as a definite overflow. The four constant-zero-divisor runtime-trap
  cases in `llvm_checked_div_test.go` were switched to param divisors (a constant zero is now a
  compile error, so exercising the *runtime* trap needs a runtime-opaque divisor — the same fix
  the elision IR test took). Tests: `checker/range_analysis_test.go` (E021 for a
  constant-assigned var + a refinement; none for a possible/nonzero/refined-nonzero divisor),
  `driver/driver_test.go` (E021 flows through the pipeline; a literal `5/0` is reported once, by
  the typechecker, with no duplicate E021). Deferred: the symmetric definite-OOB *bounds*
  diagnostic (the twin of bounds elision), not yet built.
- **Divide-by-zero / array-bounds trap elision — the range analysis's second optimizer slice.**
  After overflow-trap elision (07/25) the same `SafetyTable` channel now removes two more
  provably-dead runtime traps. The pass gained two facts alongside `NoOverflow`:
  **`NoDivZero`/`NoDivOverflow`** for a `/`/`%`/`%%` (via `markDivSafety`, wired into the
  `MathBinaryOpExpr`/`MathAssignOpExpr` div cases) — nonzero when the divisor interval excludes
  0, non-overflowing when the dividend can't be the type min *or* the divisor can't be -1
  (unsigned division is always non-overflowing); and **`IndexInBounds`** for `xs[i]` (via a new
  `evalIndex` case) — marked when the index interval is provably within `[0, size)`, which the
  loop-widening fixpoint proves for a counter (`for i = 0; i < size; i += 1`) and branch
  refinement proves for a guarded param (`if i < len { xs[i] }`). The `SafetyTable` grew from
  one map to four, each with a nil-safe accessor. **Backend:** `applyIntMathOp` now threads the
  AST node to `emitCheckedDivOp`, which skips the divide-by-zero `icmp`+trap when `NoDivZero`
  and the signed `INT_MIN/-1` `icmp`+trap when `NoDivOverflow`; `lowerIndexExpr` skips the
  negative-from-end `select` adjustment *and* the unsigned-`>=`-size bounds trap when
  `IndexInBounds` (a proven index is non-negative and in range, so a bare `getelementptr`+`load`
  is correct). **Sound by construction** (same argument as overflow elision): only *proven*-safe
  ops are in the table, absence keeps the trap, a nil table reports false — so a real
  divide-by-zero / OOB is never elided (verified: full ASan suite green; a param-fed `d(84,0)`
  and `get(arr,5)` still trap with exit 101). `TestEmit_CheckedDivisionIR` was switched to
  param-passed operands (a constant divisor is now elided, so the checked shape it pins needed a
  runtime-opaque divisor — same fix the overflow IR test took). Deferred: a range-based
  div-by-zero *diagnostic* (the error-reporting twin, symmetric with E020). Tests:
  `checker/range_analysis_test.go` (`NoDivZero`/`NoDivOverflow`/`IndexInBounds` marked for
  provable ops incl. a loop-counter index, not for full-range params),
  `backend/llvm/llvm_elision_test.go` (elided vs kept IR for div + bounds, results preserved,
  real div-by-zero / OOB still trap).
- **Precise loop widening — the value-range analysis tracks loop counters instead of havoc'ing
  them.** A C-style `for` is now analyzed with a textbook **widening/narrowing fixpoint**
  (`evalForLoop`) rather than dropping every loop-assigned variable to ⊤. The counter is tracked
  **precisely on both sides** — the init gives the lower bound, the loop guard the upper — so
  `for var i: u8 = 200; i < 250; i += 1 { … i + 100 … }` now catches the definite overflow (i ∈
  [200,249] → i+100 ∈ [300,349]) that havoc missed (with i ∈ [0,255] it merely straddled), a
  comparison on the counter can be proven constant, and **counter arithmetic inside the loop
  elides its overflow trap** (`i + 1` on i ∈ [0,2] is provably safe). **How:** the body is
  analyzed *silently* (a new `rangeChecker.silent` flag gates `report` + safe-marking) to
  compute the loop-head invariant H — widen to a fixpoint (each unstable bound jumps to ±∞ so an
  unbounded/million-iteration counter converges in a handful of steps, not N), then narrow
  (replace ∞ bounds with the finite values the guard now implies, monotone → terminates) — then
  **once loudly** with H so diagnostics fire once and elision keys off the precise ranges. Both
  phases capped at `maxFixpointIters` as a safety net. **Sound because H over-approximates:** a
  wider interval only makes E020/elision fire *less* often, never wrongly (verified: the full
  suite — many loop programs — stays green, no false positives; a 1,000,000-iteration loop
  analyzes in ~9ms). An **accumulator** with no bounding guard still widens to ⊤ (correctly — it
  genuinely can grow); the **after-loop** state havocs the loop-assigned vars (sound under
  `break`, which carries a mid-body state H needn't cover); a **`for … in`** loop (no
  counter/guard to narrow) still havocs. Nested loops each run their own fixpoint. Tests:
  `checker/range_analysis_test.go` (counter overflow both-sided, up/down counters make a
  comparison constant, accumulator no-false-positive, large-bound termination, nested),
  `backend/llvm/llvm_elision_test.go` (in-loop counter arithmetic elided + result preserved).

### 07/25/26
- **Overflow trap elision — the range analysis's optimizer half.** With the diagnostics slice
  validated, the same engine now *removes* provably-unnecessary runtime overflow traps. The pass
  returns a **`checker.SafetyTable`** — the set of `+`/`-`/`*` (and `+=`-style) ops whose
  operand ranges prove the result fits its type on every path — populated in `checkArith`'s
  "result entirely within the type" case (the third branch, between definite-overflow and
  possible-overflow). The driver stores it on `Result.RangeSafety`; the backend's
  `applyIntMathOp` consults `NoOverflow(e)` and emits the **plain** instruction (via the
  existing `emitWrappingOp`, the `wrapping_add` codegen) instead of
  `llvm.{s,u}{add,sub,mul}.with.overflow` + trap. Keyed by the AST expression node — the same
  object both passes walk (one `*ast.Program`), so the lookup is a pointer match. **Sound by
  construction:** membership is conservative (only a *proven*-safe op is present; anything
  uncertain is absent and keeps its trap), the interval is an over-approximation (if even the
  widened range fits, the real value can't overflow), and a nil table reports false — so a wrong
  entry, the only thing that could turn a real overflow into a silent miscompile, never occurs.
  Elision fires for constant/refined-range operands (`let a: u8 = 5; let b: u8 = 3; a + b` →
  plain `add i8`) and correctly does **not** for full-range ones (they keep the trap and still
  fire it at runtime). One IR test (`TestEmit_CheckedArithmeticIR`) was switched to param-passed
  operands so the checked shape it asserts is actually emitted (the constant version it used is
  now elided). Div/rem and array-bounds elision are the next slice (same channel, different
  facts). Tests: `backend/llvm/llvm_elision_test.go` (elided vs kept IR, results preserved, real
  overflow still traps), `checker/range_analysis_test.go` (`SafetyTable` marks a provable add,
  not an unprovable one).
- **Collector miscompile fixed: a trailing comment on a block's final expression dropped that
  expression.** Found while testing the value-range pass (an overflow went undetected only when
  the line had a trailing `// comment`). `CollectBlockExpr` appended `CollectStatement(child)`
  for every *named* CST child, and a `comment` is named but collects to **nil** — so a nil
  statement landed at the end of the block. The block's value is its final statement, so the
  value became that nil: the backend returned garbage (`a + b // c` in a `-> u8` body exited 1,
  not the sum) and the typechecker mis-typed the block. A comment-only body (`() => { // do
  stuff }`) likewise produced a block of nils. Fix: `CollectBlockExpr` now skips nil/typed-nil
  results, mirroring the top-level program collector's existing guard (`isNilStmt`). Six
  collector goldens that had baked-in `nil` block statements were regenerated (comment-only
  blocks now correctly collect to empty). Pre-existing and orthogonal to range analysis, but a
  genuine correctness bug. Tests: `backend/llvm/llvm_comment_test.go` (a trailing comment on a
  block value returns the right result), `collector/tests/block_comment_test.go` (no nil
  statement leaks into the block).
- **Value-range (interval) analysis — a flow-sensitive front-end pass
  (`checker/range_analysis.go`).** The engine tracks each integer variable's interval `[lo, hi]`
  at every program point and reports two things the literal-only checks can't: **`lyra-E020`
  (error)** a *definite* integer overflow — an `+`/`-`/`*`/unary-`-` whose operand ranges prove
  the result can't fit its type on any path — and **`lyra-W011` (warning)** a *constant
  comparison* (always true/false). The value-add over `checkIntegerLiteralRange` is **flow
  sensitivity**: branch refinement narrows a variable inside a branch (`if x > 100 { x + 100 }`
  on an i8 → x∈[101,127], x+100∈[201,227] → definite overflow), which constant-folding can't
  see. **Product chosen: diagnostics first** (user pick, over the check-elision optimizer) —
  pure front-end, so the engine is validated as a diagnostic before it's ever trusted to
  *remove* a runtime safety check (where an unsound narrowing would be a miscompile).
  **Soundness bias — zero false positives:** anything not precisely trackable widens to ⊤ (the
  type's full range), which can only miss a diagnostic, never invent one — an absent variable is
  ⊤; int64-overflowing interval math is ⊤ (so i8..u32 are precise, i64 mostly ⊤; u64 untracked,
  2^63 doesn't fit int64); a loop **havocs** every variable it assigns (a var modified across
  0..N iterations can be anything — sound, imprecise); a contradictory branch refinement marks
  that branch unreachable (its diagnostics suppressed); blocks restore shadowed/locally-declared
  names on exit. Interval arithmetic (`addI`/`subI`/`mulI`/`negI`) is overflow-guarded in int64;
  branch refinement handles `<`/`<=`/`>`/`>=`/`==`/`!=` against a pure constant side (variable
  on either operand), `&&` (then-branch) and `||` (else-branch), and `!`. A *possible* overflow
  (`a + b` on two full-range i8s) is deliberately left to the runtime trap. Wired into the
  driver after typechecking (needs the TypeTable for widths/signedness). **Found in passing:**
  three backend tests wrote *statically-provable* overflow (`let a: u8 = 200; let b: u8 = 100; a + b`) to exercise the runtime trap — now correctly caught at compile time by E020 — so their
  operands were routed through function parameters (runtime-opaque, so the runtime trap still
  fires on the same values). **Deferred:** trap elision (the optimizer half), precise loop
  widening, per-match-arm refinement, range div-by-zero, and `RangeConstraint` enforcement.
  Tests: `checker/range_analysis_test.go` (overflow via
  refinement/const-propagation/sub/mul/compound-assign; no-false-positive on
  possible-overflow/refined-safe/i64/loop-counter; always-true/false via type-bounds and
  refinement; no-false-warning on genuine variables and loop conditions),
  `driver/driver_test.go` (pipeline wiring).
- **Fixed-size array lowering (backend) — construction, indexing, bounds checks.** The biggest
  backend gap: the front-end already type-checked arrays
  (`inferArrayLiteralType`/`inferIndexExpr`, layout via `SizeAndAlign`/`resolveForLayout`), but
  `lowerExpr` had no `ArrayLiteralExpr`/`IndexExpr` case (errored `not implemented for
  *ast.ArrayLiteralExpr`). Now `[N]T` lowers end to end (`arrays.go`): a literal builds an `[N x
  T]` aggregate (undef + `insertvalue`, elements coerced to the element type; `lowerType` gained
  the `StaticArrayType` case), and `xs[i]` reads an element — a **constant** index is a bare
  `extractvalue` (the typechecker already range-checked it, no runtime guard), a **runtime**
  index is bounds-checked (`lowerIndexExpr`: widen the index to i64 by signedness, then a
  `getelementptr`+`load` guarded by a new `lyra_panic_index_out_of_bounds` trap). **Negative
  indices count from the end** (Python-style, added on request): `i < 0` → `i + size` via a
  `select`, so `-1` is the last element and `-size` the first; the single unsigned `>= size`
  compare on the *adjusted* value catches both `i >= size` and `i < -size` (an out-of-range
  negative stays negative → large unsigned). A constant index (positive or negative) is
  range-checked against `[-size, size)` at compile time — `resolveConstantInt` now folds a
  `NegationExpr`, and the typechecker's range check widened accordingly (`inferIndexExpr`). A
  local/param array is indexed through its own alloca (no copy, `arrayLValue`); any other array
  value is materialized into a temp. Arrays flow through `let`/params/args and **returns**
  (`emitReturn` gained an `ArrayType` by-value case). **Width fix found in passing:**
  `propagateLiteralType` had no `ArrayLiteralExpr` case, so an annotated narrow array's element
  leaves stayed i64 — a `let` tolerated it (checkVarDecl sets the node type +
  `coerceAggregateElem` fixes each element), but a function return (which fixes the type from
  the *signature*) miscompiled (`() -> [3]u8 => [4,5,6]` built `[3 x i64]` and `ret`'d it from a
  `[3 x i8]` function). Added the case: narrow the leaves and, **static context only**,
  re-record the literal as a concrete `[N x elem]` (a `[]T` dynamic annotation must keep its
  `DynamicArrayType`, or a later dynamic→static assignment error is masked — that regression
  caught two existing tests). **Deferred, loud errors:** dynamic arrays (`[]T`), string indexing
  (`s[i]`), element assignment (`xs[i] = v`). The `cmd/lyrac` backend-error fixture was
  repointed (array literal → a `newtype` decl, still unsupported). Tests:
  `backend/llvm/llvm_array_test.go` (exec: const/runtime index, param/arg/return,
  i32/bool/no-annotation elements; bounds trap on past-the-end + negative; IR:
  insertvalue/extractvalue, runtime bounds trap defined once, array-return signature),
  `typechecker/tests/array_literal_test.go` (annotated element narrows to u8; dynamic annotation
  stays dynamic).
- **`weak` type — from grammar-only crash to a usable type.** The grammar had parsed `weak T`
  (`weak_type`, e.g. `parent: weak Node`) for a while, but the collector's `parseType` had no
  case for it, so it hard-errored `unknown type node kind: weak_type` — `weak` was unusable in
  any program. Now a real type end to end: **`types.WeakType{Inner}`** (a non-owning reference,
  `pointer.go`, next to `RawPointerType`; nil-safe `String()` → `weak <inner>`), collected by
  `parseWeakType` off the `inner_type` field. **E014** (`collectByValueNames`) treats a `weak`
  field as pointer-indirection, so it breaks a recursive *size* cycle exactly like `shared`
  (`struct Node { parent: weak Node }` and `data List = Nil | Cons(i64, weak List)` are now
  well-formed). **Typechecker** `resolveType`/`resolveTypeIfKnown` resolve the inner (so `weak
  Node` isn't left an UnresolvedType); `TypesEqual` compares two weaks by inner. **Backend**
  lowers `weak T` to an opaque `i8*` (`lowerType`) and sizes it pointer-sized (`SizeAndAlign`),
  so a `weak`-broken recursive type declares and *builds* (`%Node = { i64, i8* }`). Crucially
  weak is **non-managed**: `AllocationOf(WeakType)` is Unspecified (the default), so the
  ownership pass and per-type drop glue never retain/release/drop it — the whole point of a weak
  reference, and the reason it can't double-free. **Deferred (and unconstructible today):** the
  non-owning runtime semantics — a separate weak count + upgrade-to-strong — which is what
  actually breaks refcount *cycle leaks* (ALLOCATION.md); no surface syntax creates a `weak`
  value yet, so the concrete pointee representation is intentionally left unspecified. Tests:
  `checker/recursive_type_test.go` (weak field/param/mutual-recursion break the cycle),
  `collector/tests/weak_type_test.go` (collects to WeakType, named + parameterized inner),
  `driver/driver_test.go` (`TestAnalyze_WeakType_TypeChecks` — full pipeline clean),
  `backend/llvm/llvm_weak_test.go` (pointer field IR + build/run).

### 07/24/26
- **String interpolation (`"… ${expr} …"`) end to end** — collector, typechecker, backend.
  Unblocked now that per-type value→string formatting exists (07/24 print work). **Backend**
  (`lowerInterpolatedString`, strings.go): the N-segment generalization of `++` — each segment
  is formatted to bytes by the same `formatForPrint` machinery print uses (literal chunk = a
  string; an interpolated int/float/bool/rune/string rendered per type), then concatenated into
  one fresh ref-counted box, so the result is an owned heap string like `++`. The ownership pass
  already modeled `InterpolatedStringExpr` as an owned producer whose segments are borrowed, so
  it's freed with no new pass work (IR test: exactly 1 alloc / 1 release; ASan-clean).
  **Typechecker** (`inferInterpolatedStringExpr`): each segment is now type-checked as a
  printable scalar (the print set) and an untyped numeric-literal segment is settled to its
  default width; the whole expression is `string`. This is a real check — an undefined name or a
  non-printable aggregate inside `${…}` is now an error (segments were previously unchecked;
  three `string_concat` tests that interpolated undeclared names were updated to declare them).
  **Collector whitespace fix (the surprise):** a `string_content` chunk's *leading* whitespace
  was silently dropped — tree-sitter, with `/\s/` in `extras`, strips it as token padding, so
  `"a ${x} b"` lost the space before `b`, and a plain `"  x"` lost its leading spaces, and `" "`
  collected as **empty**. Confirmed via an instrumented scanner (the scanner *is* called at the
  space and consumes it, but the node's start byte is advanced past it) — not fixable from the
  scanner (a `mark_end`-at-top JS-template-style rewrite didn't help). Fixed in the collector
  (`expressions/string_literal.go`) by reconstructing each literal chunk from the **raw source
  between** interpolation nodes (their byte ranges are exact and start at `$`), which also fixed
  the latent plain-string bug. Two collector goldens gained the previously-dropped space chunk
  (`"${prefix} ${name}"` now has a `" "` segment; `show(x) ++ " " ++ show(x)`'s `" "` is no
  longer empty); golden comparison normalizes whitespace, so the fix is guarded by exact-value
  unit tests instead (`string_whitespace_test.go`). The backend-error CLI fixture (`cmd/lyrac`,
  interp.lyra) was repointed to an array literal (still unsupported); `TestEmit_StringDeferred`
  dropped its interpolation case. Tests: `typechecker/tests/interpolation_test.go`,
  `backend/llvm/llvm_interpolation_test.go` (exec across all scalar kinds + whitespace +
  adjacency + `++` composition, IR alloc/release balance, ASan),
  `collector/tests/string_whitespace_test.go`.
- **Checked division + negation — the second checked-arithmetic slice.** After `+`/`-`/`*` (same
  day), the remaining integer operations LLVM leaves as undefined behavior are now trapped:
  `/`/`%`/`%%` guard the divisor against **zero** (both signs → a new
  `lyra_panic_divide_by_zero` trap) and, when signed, against the one overflowing division
  **`INT_MIN / -1`** (→ the overflow trap; `srem` is UB on the same inputs); and unary `-`
  guards against **`-INT_MIN`**. `trap.go` was generalized — a `panics` map +
  `panicFunc(name,msg)` (two messages now) and a shared `emitTrapIf(block, cond, fn) → cont`
  helper that all the checks (including the refactored `+`/`-`/`*` path) use. **Negation
  subtlety:** the check fires only for a *non-literal* operand — `-9223372036854775808` (and
  every narrow min) is the canonical way to *write* INT_MIN, lowers to `sub 0, INT_MIN_bits ==
  INT_MIN`, and is already range-checked by the typechecker, so trapping it would make INT_MIN
  unwritable; a runtime `-x` on a variable holding INT_MIN still traps. **Found in passing:** a
  narrow signed-min *literal* (`let x: i8 = -128`) lowered at i64 width (propagateLiteralType
  checked the positive magnitude 128 against i8's max 127, didn't fit, bailed to i64) — latent
  for a plain read but broke typed arithmetic against a real i8; **fixed same day** (see the
  narrow-signed-min entry below). Tests: `llvm_checked_div_test.go` (div-by-zero across
  `/`/`%`/`%%` + signed; `INT_MIN/-1` and `INT_MIN%-1`; `-INT_MIN`; non-trapping
  division/negation stay transparent incl. `INT_MIN / 1`; IR: unsigned `/` gets only the zero
  check, signed gets both, negation guards, trap defined once).
- **Wrapping / saturating integer methods lowered (`wrapping.go`) — the escape hatches from
  checked arithmetic.** `x.wrapping_{add,sub,mul}(y)` and `x.saturating_{add,sub,mul}(y)`
  type-checked since 07/10 but weren't lowered; now they are, dispatched from
  `lowerBuiltinMethodCall` (the same `MemberExpr`-callee path as float rounding). **wrapping** =
  LLVM's raw `add`/`sub`/`mul` (modular two's-complement — the exact op plain `+`/`-`/`*` used
  before the overflow trap wrapped them). **saturating add/sub** = the
  `llvm.{s,u}{add,sub}.sat.iN` intrinsics (signedness from the receiver). **saturating mul** has
  no native intrinsic (LLVM only has fixed-point `.fix.sat`), so it's composed: a
  `{s,u}mul.with.overflow` multiply, then on overflow a `select` to the saturation bound —
  unsigned → max (all ones); signed → min/max chosen by the product's sign (`(a<0) ^ (b<0)`).
  The argument is coerced to the receiver's width defensively. Tests: `llvm_wrapping_test.go`
  (wrap wraps, saturate clamps in both directions and both signs incl. mixed-sign mul → min and
  same-sign mul → max, the escape hatch never traps where plain `+` would, and IR: wrapping is a
  raw op with no `with.overflow`/trap, saturating add/sub uses `.sat`).
- **Checked arithmetic by default — integer `+`/`-`/`*` trap on overflow (Pit-of-Success #2).**
  The language's defining safety promise, unblocked once the backend had integer arithmetic +
  `print`/`println` (for the trap message). `applyIntMathOp` now lowers `+`/`-`/`*` to the
  matching `llvm.{s,u}{add,sub,mul}.with.overflow.iN` intrinsic (signedness from the operand,
  width per type), extracts `{result, overflow}`, and cond-branches overflow → a trap block; the
  fall-through carries the result (so `applyIntMathOp` returns the continuation block, threaded
  through `lowerMathBinaryOpExpr` and `lowerMathAssignOp` — `i += x` is checked too). The trap
  is one per-module noreturn `lyra_panic_overflow` (`trap.go`, emitted lazily like the rc
  runtime): it `write(2, …)`s "lyra: arithmetic overflow" to stderr and calls libc `exit(101)`
  (Rust's panic-code convention — deterministic and testable). `/`/`%`/`%%` and unary `-` are
  **not** checked yet (division overflow / div-by-zero / `-INT_MIN` are a separate slice,
  grouped with the range-analysis pass); `wrapping_*`/`saturating_*` remain the explicit escape
  hatches (they type-check but still need their own backend lowering to the raw ops /
  `llvm.*.sat`). Six existing tests that asserted the *old* silent-wrap behavior
  (`u8(200)+u8(100)`→44, etc.) were migrated to assert the trap — the narrow-width overflow that
  used to wrap now traps, and the trap still proves the narrow width (a wide-width op wouldn't
  overflow). Tests: `llvm_checked_arith_test.go` (overflow traps exit 101 across signed/unsigned
  × add/sub/mul × i8/u8/i32/i64 + compound `+=`; non-overflow returns the real value; IR:
  `with.overflow` + trap present, division not checked, lazy — a non-arithmetic program carries
  none).
- **`i64` min literal (`-9223372036854775808`).** The magnitude `2^63` overflows `int64` as a
  *positive* literal, so the collector records it as an `Unsigned` (u64) literal — and negating
  a u64 was rejected ("cannot negate unsigned type u64"), making i64 min unwritable.
  `inferNegationExpr` now special-cases it: a negated `Unsigned` literal whose magnitude is
  exactly `2^63` is `i64` min (a valid signed value), typed `untyped_signed_int`. No backend
  change — the literal's bit pattern already *is* i64 min (`0x8000000000000000`) and `sub 0,
  i64min == i64min` in two's complement, so `lowerNegationExpr`'s `sub 0, x` emits the right
  bits; `println(id(-9223372036854775808))` prints `-9223372036854775808`. A narrower signed
  target (`let x: i32 = <i64min>`) is still caught by `checkIntegerLiteralRange` ("overflows
  i32"), and a magnitude *above* `2^63` negated (`-18446744073709551615`) is a clean "below the
  minimum i64" error. `-9223372036854775807` (int64max negated) is unchanged (its magnitude fits
  int64). Tests: `typechecker/tests/negation_test.go` (`TestTypeCheck_Negation_I64Min*`),
  `backend/llvm/llvm_print_test.go` (i64-min exec).
- **Narrow signed-min literals (`i8 -128`, `i16 -32768`, `i32 -2147483648`) — the narrow-width
  analogue of the i64-min fix above.** Each type's minimum written as a negated literal was left
  at i64: `propagateLiteralType` narrows a literal leaf "only when the value fits", and for
  `-128` it checked the *positive* magnitude 128 against i8's max 127 — doesn't fit → left
  untyped → i64 default. Latent for a plain read (the i64 value truncates to the right i8 bits)
  but the value in typed integer arithmetic against a proper-width operand emitted an `sdiv`
  mixing i64 and i8, which clang rejects. Fix: `propagateLiteralType`'s `NegationExpr` case now
  narrows the operand leaf directly when its magnitude is exactly `2^(bits-1)` for the signed
  context type (new `signedTypeMinMagnitude` helper in `overflow.go`, covering i8/i16/i32 —
  i64's `2^63` overflows int64 and stays on the `inferNegationExpr` Unsigned path). The backend
  then reads i8 off the leaf, and `sub i8 0, 128` yields the min bit pattern (llir renders the
  `2^(bits-1)` constant as unsigned hex `u0x8000` for i16/i32 — clang accepts it); a negated
  literal is trap-exempt, so the INT_MIN negation trap is unaffected. Out-of-range (`i8
  -129`/`-200`, positive `i8 128`) still gives a clean `checkIntegerLiteralRange` overflow
  error; i64-min unchanged. Tests: `typechecker/tests/negation_test.go` (leaf-width assertions
  for i8/i16/i32 min, below-min/positive-magnitude errors, `-128` in a wider i16 context),
  `backend/llvm/llvm_narrow_min_test.go` (IR width + exec `-128 / -2 == 64`),
  `collector/tests/expr_negation_test.go` (golden documenting the plain non-Unsigned `128`
  operand the fix keys on).
- **Fixed a typechecker panic on an out-of-range numeric literal.** A literal that overflowed
  `int64` (a valid `u64` value like `18446744073709551615`, or larger) made the collector's
  `collectIntegerLiteralExpr` return **nil**, which entered the AST as a *typed-nil* expression
  and later crashed `propagateLiteralType` with a nil dereference (`e.Value` on a nil
  `*IntegerLiteralExpr`). Root-caused at the source: an expression collector must never return
  nil into the tree — `collectIntegerLiteralExpr`/`collectFloatLiteralExpr` now emit a clear
  diagnostic and a **placeholder node** (`Value 0`) on parse failure, so no downstream pass ever
  dereferences a typed-nil. Messages distinguish the cases: a value in `(i64max, u64max]` →
  "exceeds the range of i64; unsigned literals above 9223372036854775807 are not yet supported"
  (large `u64` literals still aren't representable — `IntegerLiteralExpr.Value` is an `int64` —
  a separate feature); beyond `u64` → "too large to represent"; a bad float → "out of range for
  f64". Tests: `driver/driver_test.go` (full-pipeline).
- **Large `u64` literal support** (all four bases — decimal, `0x`, `0o`, `0b`). A literal in
  `(int64max, u64max]` (e.g. `18446744073709551615`, `0xFFFFFFFFFFFFFFFF`, `0b1…1`) now
  type-checks instead of erroring. `IntegerLiteralExpr` gained an `Unsigned` flag: the
  collector's int64-overflow fallback re-parses via `strconv.ParseUint` with the *same base* (so
  it's base-agnostic), stores the value's **bit pattern** (`int64(uint64value)`, so `Value`
  reads negative) and sets the flag, and `GetType()` reports a concrete **`u64`** — the
  literal's *only* valid type, so it isn't adaptable like an ordinary untyped literal. That's
  what makes it correct in every direction with no other typechecker change: `let x: u64 =
  <max>` assigns (u64→u64), `let x: i64 = <max>` is a clean `cannot assign u64 to i64` error
  (**never** a silent negative), `u32` too-small likewise, and `println(<max>)` formats unsigned
  (`snprintf %llu` over the bit pattern → `18446744073709551615`). The backend needs nothing new
  — `constant.NewInt` from the bit pattern gives the right bits, and the recorded `u64` type
  drives width/signedness. Beyond `u64` is still the clean "too large to represent" error.
  Tests: `collector/tests/large_unsigned_literal_test.go` (the `Unsigned` flag + magnitude
  across all bases), `driver/driver_test.go` (`TestAnalyze_LargeU64Literal_*`),
  `backend/llvm/llvm_print_test.go` (u64-max decimal/hex/binary exec). An explicit narrowing
  conversion of such a literal (`i8(<max>)`, `u32(<max>)`) is also flagged out-of-range against
  the true magnitude (`inferTypeConversion` special-cases an `Unsigned` literal — it fits only
  `u64` — before the bit-pattern `extractIntLiteralValue` path; `u64(<max>)` is fine). Tests:
  `typechecker/tests/type_conversion_test.go` (`TestTypeCheck_Conversion_LargeU64*`).
- **Non-string `print` formatting — `print`/`println` now format every scalar type.** Building
  on the string-only print (07/23), `print`/`println` are now **polymorphic over the printable
  scalars** (string, any integer/float, bool, rune → void). **Typechecker:**
  `builtinFunctionSignature` (the fixed `(string) -> void`) is replaced by `inferPrintCall` +
  `isPrintableType` (`builtins.go`) — it checks the one argument is printable and settles an
  untyped numeric literal to its default width (`propagateLiteralType(arg,
  promoteToDefault(argType))`) so the backend has a concrete type; an aggregate errors clearly
  ("cannot print a value of type …"). **Backend** (`print.go` `formatForPrint`): **int** → libc
  `snprintf` `"%lld"`/`"%llu"` by signedness (widened to i64) into an entry-block stack buffer;
  **float** → `snprintf` `"%g"` (promoted to double); **bool** → a pointer/length `select`
  between interned `"true"`/`"false"`; **rune** → UTF-8 encoded (1–4 bytes by magnitude) by a
  new lazily-defined runtime `lyra_rune_to_utf8`; **string** unchanged (fat-pointer `write`).
  snprintf formats into memory (not stdio), so numeric output stays in program order with the
  raw writes (verified: `print("count: "); println(7)` → `count: 7`). Signedness verified
  (`u8(200)` → `200`, not `-56`), negatives, u64-range, 3- and 4-byte UTF-8, all ASan-clean.
  **First-cut limitations:** float uses `%g` (so `1.0` prints `1`; shortest-round-trip is a
  future refinement); aggregates need a Show/Display trait. **Found in passing (separate task,
  not print-related):** a numeric literal exceeding i64 range (`big(18446744073709551615)`)
  panics `propagateLiteralType` (`typechecker.go:1436`) — pre-existing, spawned as its own task.
  Tests: `typechecker/tests/builtin_print_test.go`, `backend/llvm/llvm_print_test.go` (per-type
  stdout capture + snprintf IR).

### 07/23/26
- **`print`/`println` — the backend's first observable output, plus void-function lowering.**
  `println("Hello, world!")` now compiles and runs. **Typechecker:** a new builtin
  *free-function* registry (`builtins.go` `builtinFunctionSignature`, the free-function analogue
  of the builtin-method registry) resolves `print`/`println` as `(string) -> void` in
  `inferIdentifierCall` — but only after scope resolution misses, so a user `let print = …`
  shadows it (verified). Their effect classification (`EffectOutput` — allowed in `det`,
  forbidden in `pure`) already lived in `checker/effects.go` `builtinEffects`. Also fixed a
  latent gap: a **void single-expression** lambda body (`() -> void => print("x")`) was never
  inferred (silently unchecked); it now is, so the call is validated. **Backend:**
  `print`/`println` lower to libc `write(1, data, len)` (lazily declared, like memcmp/malloc)
  over the string fat pointer, with a second `write` of an interned `"\n"` byte for `println`
  (`print.go`); intercepted in `lowerFunctionCallExpr` *after* the user-function lookup (user
  shadowing wins, matching the typechecker). **Void functions now lower** (previously a loud
  "not implemented"): `lowerType` maps `VoidType` → LLVM `void`, `declareFunction` drops the
  void guard, `emitReturn` emits `ret void` (discarding any body value), and both
  `defineFunction` and `lowerEntry`'s void case lower the body for *effect* (`lowerForEffect` —
  handles an empty or non-expression-terminated block) then return; routing the void entry
  through `emitReturn(nil)` also flushes owned temporaries, so `println("a" ++ b)` frees its
  heap concat instead of leaking. **Ownership:** `print`/`println` **borrow** their argument
  (`calleeIsBorrowingBuiltin`), so an owned temporary argument is released after the call rather
  than conservatively transferred (alloc==release verified, ASan-clean). **Deferred:**
  formatting a non-`string` value (int/float/bool/rune → text) — the value→string machinery
  interpolation also waits on; `print` currently takes only `string`. Tests:
  `typechecker/tests/builtin_print_test.go`, `backend/llvm/llvm_print_test.go` (stdout capture +
  IR + alloc/release balance).
- **Rune literals in `match` arms + character-literal lowering (backend) — closes the grammar
  gap the 07/21 char→rune entry spawned.** A character-literal pattern (`'a' => …`) now parses,
  type-checks, and lowers end to end, and a `CharacterLiteralExpr` lowers as an expression at
  all (it previously had no `lowerExpr` case — `let c = 'a'` couldn't compile). **Grammar:**
  added `$.char_literal` to `literal_pattern` (`patterns/index.js`); the existing `[expression,
  literal_pattern]` GLR conflict already covers it (char_literal reaches `expression` via
  `_literal`), so no new conflict entry. **Collector:** a `literal_pattern` wrapping a
  `char_literal` stores its **decoded code point** as a new `ast.RunePatternValue` (a `rune`
  with a quoting `String()`), reusing the expression collector's `CharacterLiteralExpr` decode
  so escape handling (`\n`, `\x41`, `\U…`) lives in one place; every other literal keeps its raw
  source text. The Stringer keeps `%s`/`%v` diagnostics and golden output readable (`'a'`, not
  `97`). **Typechecker:** `literalPatternKind` returns `Rune` for a `RunePatternValue`; a new
  `isRuneType`/`checkRuneMatchArm` branch in `checkMatchExpr` (rune is deliberately *not*
  `IsNumeric`, so a rune scrutinee previously fell through every branch, unchecked) accepts
  char-literal/identifier/wildcard arms, rejects number/string/range/regex arms, and warns on a
  missing catch-all (like strings — code points aren't enumerated). **Backend:**
  `CharacterLiteralExpr` → `constant.NewInt(i32, codepoint)`; a rune scrutinee (i32) already
  routed to `lowerScalarMatch`, so `scalarMatchTest` just gained a `RunePatternValue` case
  emitting `icmp eq i32` against the pre-decoded point — guards and identifier catch-alls work
  unchanged. Verified: char/escape/unicode arms, wildcard fallthrough, inline char scrutinee,
  and a `x if x == 'a'` guard all compile+run to the right exit code. **Deferred:** char *range*
  patterns (`'a'..'z'`) — the `range_pattern` grammar bounds are still `_number_literal`-only;
  would round out rune match but adds grammar + exhaustiveness surface. Tests: grammar corpus
  (`match.txt`), `typechecker/tests/match_expr_runes_test.go`, `backend/llvm/llvm_rune_test.go`
  (exec + IR).

### 07/21/26
- **`char` primitive type: fixed collection + renamed to `rune`.** `char` had no grammar rule in
  `_primitive_type`, so it collected as a `GenericType` (a type variable), silently accepting
  anything (`let c: char = 5` type-checked). Added a `char_type` grammar rule + collector case,
  making it a real `PrimitiveType` (the backend already mapped it to i32). Then **renamed `char`
  → `rune`** (grammar `rune_type`, `types.Rune`, collector, backend, tests) to match Go/Odin and
  Lyra's UTF-8 byte-length string model — the honest name for an unvalidated i32 code point
  (Rust's `char` implies scalar-value *validation* this doesn't do). The character *literal*
  `'a'` keeps its syntax and its `CharacterLiteralExpr` node (now typed `rune`), exactly as Go's
  `token.CHAR` yields a `rune`. Separately discovered: character *literal patterns* in `match`
  arms (`'a' => …`) don't parse — a distinct grammar gap, spawned as its own task. Tests:
  `typechecker/tests/rune_type_test.go`, collector golden `rune_type_annotation`, grammar
  corpus.
- **Use-after-move on `own` parameters (`lyra-E019`).** New
  `pkg/analyzer/checker/use_after_move.go`: a flow-sensitive definite-move analysis flagging a
  read of a binding after its value was moved into an `own` parameter. A *move* is exactly one
  thing — a bare identifier naming a **managed** value (string or `shared`, via
  `ownership.IsManaged`) passed to an `own` param; an `own` scalar or stack struct is copied,
  and a field argument (`p.name`) is a partial move, so neither counts. Joins take the **union**
  (moved in either `if`/`match` branch → moved after), a loop body is seeded with every move
  inside it (so a move on one iteration is visible to the next iteration's reads, with a message
  that says so), and a `let`/`var` declaration or reassignment of the name clears the move.
  Every uncertain case resolves toward *not* reporting — an unresolvable callee records no move
  — so a new hard error can't fire on shapes the analysis doesn't understand. Reports dedupe by
  (binding, move site), which collapses a loop-carried move to one error instead of one per
  read. Runs after typechecking (it needs the TypeTable to identify managed values). **Framing
  correction:** the todo called this "the only unsound hole today"; measuring it disproved that
  — the ownership pass retains a managed value flowing into a non-last-use `own` argument, so
  use-after-`own` is memory-safe (verified ASan-clean with the correct result). The real payoffs
  are enforcing the `own` contract, surfacing the otherwise-invisible **reuse/FBIP perf cliff**
  (the defensive retain leaves rc = 2, `lyra_rc_drop_reuse` reports shared, and a rebuild that
  should be zero-allocation starts allocating per cell), and unblocking removal of that retain
  so `own` becomes a true move. Trait-impl and trait-default method bodies are covered too, each
  from a fresh state (the generic AST walker reaches them, so without an explicit case one state
  would thread through every method in an impl and a move in one would flag a read in the next).
  Tests: `use_after_move_test.go` (20 cases — base/borrow/field-read/repeated-arg, non-moves,
  branch+match+loop flow, reassign/rebind clearing, unresolvable-callee and per-function
  conservatism, trait methods), mutation-checked against both the join-union and loop-seeding
  rules.
- **Aggregate-field drop — a `shared` box now frees what its payload owns.** Release passed a
  null `drop_fn`, so a managed value inside a box (a string field, a nested `shared` value, a
  list's tail) was abandoned when its owner died — freeing a list freed the head cell and leaked
  the spine. New `drop.go` generates a cached `@lyra_drop_T(i8*)` per payload type, releasing
  every managed reference reachable *by value* from `T`, and `lowerManagedRelease` passes it as
  the box's `drop_fn`; "by value" is both the stopping rule and the termination argument (a
  managed field is released, never walked into, and a recursive cycle must pass through a
  `shared` field per lyra-E014). Two consequences: arm-binding *transfer* (`armTransfer`) is
  gone — a moved field would now be freed twice, so arm bindings always dup, costing refcount
  traffic but not allocations (reuse still reclaims the shell, so FBIP stays
  zero-alloc-per-cell) — and the reclaimed box's old payload is dropped at the match's merge
  (`dropReclaimedPayload`), past every arm, not inside `lyra_rc_drop_reuse`, which would free a
  field the arm hasn't dup'd yet. **Still leaking (safe):** a managed value inside a plain
  *stack* aggregate, which needs deep-retain-on-copy first. Tests: `llvm_aggregate_drop_test.go`
  (11 programs, exec + ASan + IR conservation).

### 07/18/26
- **Perceus stage 3 — reuse analysis / FBIP for `shared` values.** When a `match` destructures
  an owned `shared data` value at its last use, its ref-counted box is *reclaimed* in place
  instead of freed-then-reallocated. **Runtime:** `lyra_rc_drop_reuse(box)` (`runtime.go`)
  returns the box as a *reuse token* when unique (`rc==1`, not freed), null when shared
  (decrements) or pinned. **Construction:** `lowerBoxSharedReuse` (`shared.go`) branches at
  runtime on the token — write the new payload into the reclaimed box, else a fresh
  `lyra_rc_alloc`. **Pass** (`pkg/analyzer/ownership`): `ReuseMatch`/`ReuseTarget` mark a
  reuse-source match (owned scrutinee at last use, `shared data`, plain tag switch, ≥1
  same-type-constructing arm) and its target constructions; `lowerDataMatch` drop-reuses the box
  once, retires the scrutinee's slot (suppressing its ordinary drop), and hands the token to
  each arm (a target consumes it, a non-constructing arm frees it). **The recursion enabler:**
  `armTransfer` — a field binding used exactly once in a consuming match *moves* (no dup,
  `LastUseTransfer`) into an owning position, so the reused cell stays unique and a recursive
  `map` reclaims every cell (zero allocation per cell) instead of leaking the tail. **Supporting
  pieces:** a `shared`-value return path (`emitReturn`'s pointer case) and the typechecker's
  `propagateAllocation` (a `shared` return/annotation stamps construction leaves inside
  `match`/`if` arms `shared`, so the arm's value is heap-boxed — also closes the alloc-detection
  half of the "`shared` construction in return position" gap). A **borrowed** scrutinee is never
  reused (caller still owns it). ASan-verified across linear in-place update, recursive FBIP
  map, the token-free path, and the borrow safety boundary. **Deferred:** stage 4 specialization
  (skip shared-field stores + static-uniqueness fast path), the ladder-fallback path
  (guards/value-tests), struct/tuple reuse. Tests: `llvm_reuse_test.go` (runtime primitive +
  exec + ASan + IR), `ownership_test.go` (`TestReuse_*`).
- **`match` on a `shared` aggregate value (backend) — Perceus reuse prerequisite.** A `shared`
  data/struct/tuple scrutinee lowers to a box pointer, not a first-class aggregate, so `match`
  failed with "did not lower to a struct". New `unboxSharedData` (`shared.go`) loads the inline
  payload out of the box (`box → field 1`);
  `lowerDataMatch`/`lowerStructMatch`/`lowerTupleMatch` unbox up front and the existing
  tag/pattern machinery (incl. the payload-test/guard ladder fallback) runs unchanged on the
  union. An identifier catch-all binds the *box pointer* (its declared type), so
  `lowerAggregateMatch` now threads a `whole` value separately from the unboxed `scrut`. The
  box's own drop is the ordinary last-use release (reading through it consumes no reference —
  ASan-verified, 1 alloc/1 release). This is the destructuring foundation Perceus stage 3
  (reuse/FBIP for `shared` values) builds on — next up is drop-reuse (a reuse token when `rc ==
  1`) + reuse-aware construction. **Deferred (errors loudly):** a *nested* `shared data`
  sub-pattern (destructure a tail through its own box). Tests: `llvm_shared_match_test.go`
  (data/nullary/ident-catch-all/recursive-list/payload-test/guard/struct — exec + ASan + IR
  conservation + nested-deferred).

### 07/17/26
- **Clear diagnostic for SCREAMING_CASE struct names (`lyra-W009`).** Diagnosing the "shared
  multi-field struct copy fails" report turned up the real cause: it wasn't `shared` or
  multi-field at all — an all-uppercase type name (`P`, `AB`, `NODE`, …) matches the
  `const_identifier` lexer rule (`[A-Z][A-Z0-9_]*`) instead of `user_defined_type_name` (which
  needs a lowercase letter to win the longest-match tie), so a struct literal `NAME { … }` won't
  parse and every use surfaces a confusing "undefined symbol". New checker pass `CheckTypeNames`
  (`checker/type_names.go`) warns at the *declaration* (where the fix lives). Scoped to
  **structs** only — a `data` type constructs via its constructors and a named tuple via
  `Name(…)`, both of which work with a SCREAMING_CASE name (verified), so those aren't flagged.
  A warning, not an error, since the type is still usable by reference (e.g. a `data` payload
  `Wrap(P)`). Tests: `checker/type_names_test.go`. (Confirms the `shared` lowering has no
  multi-field limitation — `struct Pair { x, y }` with a `shared` copy runs, exit 42.)
- **`shared`-value lowering (backend).** A `shared T` now lowers to a **pointer to a ref-counted
  box `{ i64 rc, T }`** (`lowerType` + `SharedBoxType`), reusing the string runtime + ownership
  machinery. **Construction** (`lowerStructInstanceExpr`, `lowerDataConstruction`): a
  `shared`-flavored construction builds the inline payload and `lowerBoxShared` (`shared.go`)
  allocs `header + sizeof(payload)` via `lyra_rc_alloc` (rc=1) and stores it — the value is the
  box pointer. The flavor is read from the construction's recorded type (the typechecker stamps
  `Shared` on a `shared`-annotated binding's initializer and, transitively, on a `shared`
  payload arg, so a recursive `Cons(1, Nil)` boxes the nested `Nil`). **Field access**
  (`lowerMemberExpr`): a `shared` object is a box pointer, so a field is `getelementptr` through
  the box + load. **`shared` fields** lower to pointers (`lowerType`), which is also what makes
  a recursive `shared` field finite. **Ownership**: `IsManaged` now covers `shared`
  (`AllocationOf == Shared`), so a `shared` binding gets the full
  retain/release/last-use/transfer/drop treatment; retain/release dispatch on the value's
  representation (`lowerManagedRetain`/`Release` + `managedBox`: a string recovers its box via
  `stringBox`, a `shared` value *is* the box pointer). Verified memory-safe under
  AddressSanitizer with release==allocation conservation. Removed the old `shared`-payload
  "deferred, loud error". **Still deferred:** `shared` arrays, `shared` construction in bare
  arg/return position, `match` on a `shared data` value, and recursive drop of a managed value
  inside an aggregate field (leaks — the aggregate-drop follow-on). Tests: `llvm_shared_test.go`
  (construct/transfer/i64-payload/recursive-data, exec + ASan + IR conservation), updated
  `llvm_data_test.go`.
- **Perceus stage 2 — drop fusion (scalars).** Completes stage 2 by fusing the last-use *drop*
  (the transfer half landed earlier the same day). Replaced the sentinel + pending-slot-action
  mechanism with `dropLastUsesInStmt`: after each statement, `lowerBlockStmts` walks it for
  last-use-borrow identifier nodes and, for each binding declared in the current scope (top
  frame), releases it and retires the slot — emitted in the statement's **end block, which
  post-dominates** the statement's internal branches, so a *conditional* last use (inside an
  `if`/`match` arm) is freed correctly on every path. Doing drops at statement boundaries (not
  via a cross-statement pending list) is what makes it robust against the "steal" hazard: a
  statement that seals (an early return) is skipped, so its bindings are freed by the seal's
  frame release on that path, while the fall-through frees at the boundary — exactly once each.
  Removed the pinned-sentinel machinery entirely (`stringSentinel`, `pendingSlotActions`,
  `flushSlotActions`). A copy chain `a → b → c` now compiles to **one allocation and one
  release, zero retains** (was one no-op per binding). Verified under AddressSanitizer across
  conditional last-use, nested-return-in-the-last-use-statement, nested-block, and
  early-return-before-last-use cases, plus static release==allocation conservation checks (macOS
  ASan can't see leaks). Tests: `llvm_ownership_test.go` (four new `TestExec_Ownership` cases +
  `TestEmit_OwnershipIR` single-binding=1 / chain=1 / conservation).
- **Perceus stage 2 — transfer fusion (scalars).** A last-use *transfer* moves the reference to
  the consumer at the use point, so the transferred binding's scope-exit release was a pure
  no-op (a load + `lyra_rc_release` on a pinned sentinel). The backend now retires the binding
  from its managed frame *immediately at the move* (`retireManagedSlot` in `ownership_lower.go`,
  called from the `lowerExpr` last-use hook) instead of sentinelling it — no scope-exit release
  emitted at all. A copy chain `let b = a; let c = b; …` now emits **zero** per-transfer
  releases (was one sentinel no-op each); only the final binding's real drop (+ its backstop
  no-op) remains. Safe because a transfer is unconditional (the pass only marks a non-branch
  use) and the removal is compile-after any earlier seal, so an early return still saw the
  binding in-frame and released it on its own path — ASan-verified. **Still open in stage 2:**
  the last-use *drop* keeps its sentinel + frame backstop (its release must follow the borrow,
  so it's deferred, which entangles with the seal/pending timing — one residual no-op per
  dropped binding). Tests: `llvm_ownership_test.go` (`TestEmit_OwnershipIR` transfer-chain = 2
  releases, `TestExec_OwnershipASan`).
- **Perceus stage 1 — last-use precision (scalars).** The ownership pass
  (`pkg/analyzer/ownership`) evolves from scope-exit release toward Perceus's garbage-free
  last-use dup/drop. `computeLastUse` finds each eligible managed binding's *final textual
  reference* (a sound over-approximation of its dynamic last use; a binding that is shadowed, a
  parameter, reassigned, or referenced inside a loop is ineligible and keeps scope-exit release
  — the leak-safe direction). At the last use the pass emits **`LastUseTransfer`** (owning
  position — `let b = a`, `return a`, an `own` arg: the reference *moves*, so **no dup**, the
  tightness win over the old always-dup-then-scope-drop; applied only to an *unconditional* use
  so it happens on every path) or **`LastUseDrop`** (borrowing last use — released at that
  statement instead of scope exit). Backend (`ownership_lower.go` + `lowerExpr`/`emitReturn`):
  at a last use it retires the binding's slot with a **pinned empty-string sentinel**
  (`stringSentinel`) so the scope-exit frame release — kept as a leak-safe **backstop** — no-ops
  on already-handled slots and still frees anything the pass didn't reach (a missed last use
  only defers a free, never double-frees). Slot actions flush after *every* statement (before
  the frame release) so a transferred/returned binding is sentinelled before the frame could
  free it — the bug the ASan test caught. Also **closed the break/continue leak** (a loop's
  managed frames are released on those edges via a recorded `loopCtx.frameDepth`). **Verified
  memory-safe under AddressSanitizer** (`TestExec_OwnershipASan`) across copies, transfer
  chains, an early-return-before-last-use, and conditionals. **Deferred to later stages:**
  aggregate-field drop, hoisting a *conditional* last use, and the residual sentinel no-op
  releases (Perceus stage 2 = drop specialization + dup/drop fusion). Tests: `ownership_test.go`
  (transfer/drop/last-use decisions), `llvm_ownership_test.go` (new last-use/early-return/chain
  cases + ASan + IR), `runtime_test.go` unchanged.
- **Ownership model — heap strings are freed (front-end pass + backend retain/release).** The
  full ownership model ALLOCATION.md describes, realized for strings. **Uniform
  representation:** every string value is a ref-counted box, so retain/release are total and
  safe on any string — a **literal** now interns as a *pinned* static box `{ i64 PinnedRC, [N x
  i8] }` (`data` at `box+8`, still zero allocation; retain/release no-op via the PinnedRC
  sentinel), a `++` value is a heap box. This is the enabler: no site has to distinguish
  literal-vs-heap. **Ownership pass** (`pkg/analyzer/ownership`, runs after typecheck, produces
  a `Table` the backend consumes, no diagnostics): ARC over managed (string) values — a binding
  / `own` param holds one owning +1 released at scope exit; the pass computes the
  context-dependent adjustments — `Retain` (a borrowed value into an owning slot: a copy `let b
  = a`, an owned `return`, an `own` arg) and `ReleaseTemp` (an owned temporary into a borrowing
  slot: a `==`/match/`++` operand, a discarded statement, a borrowed arg) — using the same
  `paramOwnsArgument`/`isOwnedReturn` semantics as the typechecker. An `if`/`match` is treated
  as one merged owned value (branches coerced to +1), released once at the phi rather than
  per-branch (which would free the value the phi still refers to). Safety-biased: an
  unresolvable callee's args and values entering an aggregate are *transferred* (leak —
  memory-safe), never released. **Backend** (`ownership_lower.go` + hooks in
  `lowerExpr`/`emitReturn`): a stack of managed-scope frames releases bindings at scope exit (a
  `return` releases every live frame before it seals; the retain-on-escape the pass emitted
  makes that safe); each temporary is released **in the block it was produced in** — llir lets a
  release be appended before a sealed block's terminator — so a temp built inside an `&&`
  right-hand or `if` branch is freed there (dominating its uses), fixing the "instruction does
  not dominate all uses" hazard a merge-block release would hit. `own` string params are
  released by the callee; bare/`ref`/`mut` are borrows the caller keeps. Wired into
  `driver.Analyze` (`res.Ownership`). **Verified memory-safe under AddressSanitizer** (no
  double-free / use-after-free across copies, returns, chains, reassignment, own-params,
  conditionals, loops). Still leaking conservatively (safe, never a double free): strings inside
  aggregate fields, and bindings on break/continue paths. Tests:
  `pkg/analyzer/ownership/ownership_test.go` (retain/temp decisions), `llvm_ownership_test.go`
  (`TestExec_Ownership` behavioral, `TestExec_OwnershipASan`, `TestEmit_OwnershipIR` balance),
  `llvm_string_test.go` (`TestEmit_StringLiteralIsPinnedBox`).
- **Ref-counted heap runtime + string concatenation `++` (backend).** "The heap allocator" — the
  runtime that string `++`/interpolation and (later) `shared` values need. Architecture call:
  since there's no separate runtime object and `lyrac build`/the test harness both just run
  `clang out.ll`, the shims are emitted as **real function definitions into the module itself**
  (`runtime.go`, `ensureRCRuntime`) on libc `malloc`/`free` (declared like `memcmp`/`memcpy`),
  replacing the old dead `declareRuntime` externs. `lyra_rc_alloc` (malloc a box, rc=1),
  `lyra_rc_retain` (rc+=1), `lyra_rc_release` (rc-=1, `drop_fn(payload)` + `free` at 0) — all
  three defined together, all no-op on a `PinnedRC` (arena) box, box header a single `i64`
  refcount (payload at `box+8`). Emitted **lazily** — a non-allocating program carries none of
  it. First consumer: `lowerStringConcat` (`++`) — a concatenated string can't point into a
  constant global (its bytes are runtime), so it allocates a box via `rcAllocPayload`, `memcpy`s
  both operands into the payload, and returns a fat pointer `{ box+8, la+lb }`; operands are
  ordinary fat pointers wherever their bytes live, so chains and empty operands (`memcpy` n=0)
  compose with no special cases. **Ownership deferred (leaks):** nothing frees a heap string yet
  — `retain`/`release` exist and are correct but no call/scope site emits them (needs
  `own`/`ref`/`mut` reading + scope-liveness); this is the next allocation slice. Interpolation
  is no longer allocator-blocked — it now needs value→string formatting. Verified end-to-end
  (`lyrac build` + `clang` on a real `.lyra` using `++`) plus: `runtime_test.go` (white-box —
  hand-built `main` checking rc=1 after alloc, 2 after retain, 1 after release, pinned no-ops,
  and the `drop_fn`-runs-before-free path, all compiled+run via clang), `llvm_string_test.go`
  (`TestExec_StringConcat`
  literals/empties/left-associated-chain/param-strings/matching-a-heap-string,
  `TestEmit_StringConcatIR`, `TestEmit_NoRuntimeWhenUnused`).
- **Float→int rounding (typechecker + backend) — `x.floor()`/`.ceil()`/`.round()`.** The
  explicit, non-lossy escape hatch from the numeric-conversion rejection (`i64(x)` on a float is
  a typecheck error). Registered in `builtins.go`'s `floatRoundingOps` (float-receiver-only, 0
  args, fixed `i64` return — same "default width, narrow explicitly" approach as an unannotated
  literal, since context-directed *return*-type inference doesn't exist yet; this is the same
  open problem the still-unregistered `truncate`/`saturate`/`narrow` builtins have). This is
  also the first method call the LLVM backend lowers at all (`wrapping_add`/`saturating_add`
  type-check but were never lowerable) — `lowerFunctionCallExpr` gained a
  `*ast.MemberExpr`-callee branch routing to `rounding.go`'s `lowerBuiltinMethodCall`, which
  picks the receiver-width `llvm.<op>.<width>` intrinsic (lazily declared + cached, same shape
  as `memcmpFunc`) and `fptosi`s the result to i64. `round` uses `llvm.round`
  (half-away-from-zero) over `rint`/`nearbyint` (round-to-even). Tests exercise both positive
  and negative inputs and all three float widths, so `fptosi`/intrinsic-width selection is
  actually verified, not just the happy path. Tests:
  `typechecker/tests/builtin_rounding_test.go`, `llvm_float_test.go`.

### 07/16/26
- **Strings (backend): literals, equality, `match`, params/returns.** Representation decided —
  immutable fat pointer `{ i8* data, i64 len }` (byte length, not NUL-terminated;
  Go/Rust/Swift-standard, O(1) len, UTF-8/NUL-clean, literals need no allocation). Spec in
  `STRING_LAYOUT.md` (ALLOCATION.md had deferred this decision). Literals intern bytes in a
  private immutable global + fat-pointer struct (`lowerStringConstant`); `==`/`!=` branchless
  `len_eq && memcmp(min)==0` via libc `memcmp`; string `match` on the shared scalar ladder
  (byte-equality literal arms, identifier binds the fat pointer); by-value params/returns
  (`emitReturn` aggregate path); `lowerType`/`LLVMPrimitive`/`SizeAndAlign` map `string` → the
  struct (16/8). Concatenation `++`/interpolation (need a heap allocator), `print`, and
  escaped/regex patterns deferred with loud errors. Tests: `llvm_string_test.go`, all built +
  run via clang.
- **Float scalar `match` (backend).** `lowerScalarMatch` and the `lowerMatch` dispatch accept a
  float LLVM type (not just integer); `scalarMatchTest` delegates a float scrutinee to
  `floatScalarMatchTest` — `fcmp oeq` for a literal arm, a two-sided ordered range check
  (`oge`/`olt`/`ole`) for a range arm, `constFloatFromExpr` folding float/int/negated bounds.
  Identifier catch-alls bind the float, guards work unchanged, and a float match always needs a
  wildcard (the reals can't be enumerated; typechecker warns otherwise). Also added a
  typechecker warning on a float *literal* pattern (`checkNumericMatchArm`) — it lowers to `fcmp
  oeq`, the same exact-equality hazard as the `==`/`!=` operator warning; both now share
  diagnostic code `lyra-W008` ("imprecise float equality"). Only string/array match scrutinees
  stay deferred. Tests: `TestExec_FloatMatch` (literal/wildcard/range/binding/f32/guard),
  `TestEmit_FloatMatchIR`, `match_binding_test.go`
  (`TestMatch_FloatLiteralPatternWarns`/`_FloatRangePatternNoWarn`).
- **Floats (backend): literals, arithmetic, comparisons, conversions, params/returns.** Float
  literals lower at their context-recorded width (`literalFloatType`, default f64).
  `applyFloatMathOp` handles `fadd`/`fsub`/`fmul`/`fdiv`, `frem` for truncated `%`, and a
  `select`-based floored `frem` (`lowerFlooredFRem`) for `%%` — the float mirror of the integer
  path. `lowerFloatComparison` emits `fcmp` (ordered predicates, `une` for `!=` so `NaN != x`
  holds). `lowerNumericConversion` gained int→float (`sitofp`/`uitofp`) and float widening
  (`fpext`); `emitReturn` handles a float `retType`. The three arithmetic/comparison entry
  points dispatch on the already-lowered operand's LLVM type. Since there's no float→int
  conversion (typecheck error → `floor`/`ceil`/`round`, unimplemented), a float is observed via
  a comparison; **float→int rounding and float `match` stay deferred** (the scalar-match ladder
  is int-gated — a small follow-on). Tests: `llvm_float_test.go` (arithmetic, int→float/widening
  conversions, float function, IR shape), all built + run via clang.
- **Match arm guards (typechecker + backend) — `Some(x) if x > 0`.** A guard is a bool test
  evaluated after the pattern matches and its variables are bound; when false, control falls
  through to the next arm. Typechecker checks the condition with bindings in scope and requires
  `bool` (`checkMatchExpr`); guarded arms already didn't count toward exhaustiveness. Backend
  `lowerGuardedArmBody` cond-branches on the guard to the body or the next arm, plugged into
  both ladders (`lowerScalarMatch`/`lowerAggregateMatch`); a guarded arm never seals the ladder,
  and a `data` match with any guard takes the ladder fallback (`matchHasGuard`) rather than the
  tag `switch`. Only string/float/array scrutinees remain deferred for `match`. Tests:
  `TestExec_MatchGuards` (data/scalar/struct/tuple), `TestEmit_MatchGuardIR`,
  `match_binding_test.go`.
- **Value-testing `data` payload sub-patterns (backend) — `Some(0)`.** A tag `switch` can't
  discriminate two arms that share a variant tag but test different payloads. Nested in an
  aggregate (`(c, Some(0))`): `aggPatternTest`'s `DataPattern` case now ANDs the tag check with
  a branchless test per value-testing payload field (extract + recurse; safe on a tag mismatch
  since the AND is already false). Top-level (`match m { Some(0) => .., Some(x) => .., None =>
  .. }`): `lowerDataMatch` falls back to the shared if-else ladder (`lowerAggregateMatch`) when
  `dataMatchHasPayloadTest` finds one, keeping the compact `switch` otherwise. Arm guards and
  string/float/array scrutinees remain the only deferred match forms. Tests:
  `llvm_match_test.go` (`TestExec_DataLiteralPayload`, `TestEmit_DataLiteralPayloadIR`, nested
  cases).
- **Narrow tuple-literal payload width propagation (typechecker + backend) — fixes the
  spawned-task panic.** `propagateLiteralType` now recurses element-wise into a
  `TupleLiteralExpr` against a `TupleType` context, so a tuple data payload/struct
  field/annotation (`Wrapped((20, 22))` with `(u8, u8)`, `let a: (i32, i32) = (1, 2)`) narrows
  each leaf. Enabled by leaving an anonymous tuple's element leaves untyped in
  `inferTupleLiteralExpr` (deferred promotion, like arrays) with
  `promoteToDefault`/`isAssignable` gaining `TupleType` cases; the backend also runs each
  aggregate element through a defensive `coerceAggregateElem` so a residual width mismatch
  coerces instead of panicking `insertvalue`. Arrays remain the open half of the old gap. Tests:
  `data_ctor_width_test.go` (tuple payload + annotation narrowing), `llvm_data_test.go`
  (`TestExec_DataNarrowTuplePayload`, exit 42 end-to-end).
- **Nested `data` sub-patterns (backend).** Integrated into `aggPatternTest`/`aggPatternBind`: a
  data sub-pattern's test is its tag check (`extractvalue`-the-tag == index, ANDed into the
  aggregate condition — `(c, Some(x))` discriminates on the nested tag), and its bind
  reinterprets the payload (`extractDataPayload`, store-to-slot + `bitcast`) and recurses.
  `bindDataPayload` (top-level data arm) also recurses via `aggPatternBind`, so `Wrapped((a,
  b))`/`Boxed({ x, y })` destructure. Deferred: a value-testing payload sub-pattern (a literal
  `Some(0)` or nested data pattern — `patternHasTest`), since a tag switch can't
  test-and-fall-through. Found a separate pre-existing panic (spawned task): constructing a data
  value with a narrow tuple-literal payload (`Wrapped((20,22))` with `(u8,u8)`) doesn't
  width-propagate → `insertvalue` panic. Tests: `llvm_match_test.go`
  (`TestExec_NestedDataPatterns`, literal-payload deferred).
- **Nested sub-patterns in struct/tuple match (backend).** Generalized the struct/tuple
  test+bind into mutually-recursive `aggPatternTest`/`aggPatternBind` that walk a pattern
  against the first-class aggregate value: a literal sub-pattern ANDs an `icmp`/range check, a
  nested struct/tuple sub-pattern recurses via `extractvalue` (`((a, b), c)`, `{ inner: { v }
  }`, `(Pt { x, y }, c)`) — always safe with no branch, since a single-shape aggregate has no
  tag. Threads the Lyra type down to resolve nested field/element types. Replaced the four flat
  `structPatternTest`/`bindStructPattern`/`tuplePatternTest`/`bindTuplePattern` helpers.
  Deferred: a nested `data` sub-pattern in an aggregate (needs tag+memory), data-payload
  destructuring (`W((a,b))`). Tests: `llvm_match_test.go` (`TestExec_NestedAggregatePatterns`,
  data-payload-destructure deferred).
- **`match` on a tuple (backend) + shared aggregate ladder.** `lowerTupleMatch` is the
  positional counterpart to struct match: `(a, b)` binds elements by index (`extractvalue`),
  `(0, b)` tests element 0. Refactored the struct/tuple ladders into one shared
  `lowerAggregateMatch` (single-shape aggregate, no tag) parameterized by `test`/`bind` closures
  — the struct-vs-tuple difference (named fields vs positional elements) is all that differs.
  Now every match scrutinee kind lowers (data/struct/tuple/scalar); remaining deferred:
  string/float (types don't lower), guards, nested sub-patterns. Tests: `llvm_match_test.go`
  (`TestExec_TupleMatch`, nested-pattern deferred).
- **`match` on a struct (backend).** `lowerStructMatch`: a struct has one shape (no tag/switch),
  so `{ x, y }` matches unconditionally and binds fields (`extractvalue`→alloca), while a
  literal field sub-pattern (`{ x: 0, y }`) makes the arm conditional (`structPatternTest`,
  reusing `scalarMatchTest`) — same if-else ladder, `_`/identifier catch-all. Struct patterns
  are brace-only or type-named. Tuple/string/float scrutinees, guards, nested field sub-patterns
  deferred. Tests: `llvm_match_test.go`.
- **`Pt { x, y }` struct patterns (unbundled from data patterns).** A struct pattern may now
  name its type (symmetric with construction `Pt { x: 1 }`), not just the brace-only `{ x, y }`.
  Since `Pt { … }` and a data variant `Node { … }` are syntactically identical (both parse to
  `DataPattern`), the collector's new `reclassifyStructPatterns` finishing pass splits them
  semantically: a name that's a declared struct type → named `StructPattern`; a data constructor
  → stays `DataPattern`. So struct and data-constructor patterns are now distinct AST nodes
  everywhere downstream. Typechecker verifies a named pattern's type matches the scrutinee/value
  (a new safety check the brace-only form couldn't express); backend unchanged (a named
  `StructPattern` lowers like a bare one). Tests:
  `collector/tests/reclassify_struct_pattern_test.go`,
  `typechecker/tests/match_struct_named_test.go`, `llvm_match_test.go`.
- **`match` on a bool/integer scalar (backend).** `lowerScalarMatch`: an if-else ladder — each
  arm a comparison (`icmp eq` for a literal, two-sided range check for a range pattern) that
  cond-brs to the body or the next test, first-match-wins; `_`/identifier arm ends it
  (identifier binds the scalar), arms feed a merge phi, fall-through is `unreachable`. Uniform
  ladder (not a switch) so range arms fit. Tuple/struct/string/float scrutinees and guards
  deferred. Tests: `llvm_match_test.go`.
- **`match`/destructuring on a `data` value (typechecker + backend).** Typechecker now binds
  match-arm pattern variables in the arm body
  (`checkMatchExpr`/`withPatternBindings`/`walkDestructuredPattern` via `paramTypes`;
  `bindDataPatternPayload` handles flat `Rect(w, h)` and tuple `MkPair((x, y))` forms), and
  propagates arm-body widths (10th `propagateLiteralType` site +
  `MatchExpr`/`IfExpr`/`BlockExpr` recursion) so bare-literal arms adapt to a sibling or the
  return type. Backend `lowerDataMatch`: store scrutinee → load tag → `switch`; data-pattern
  arms `bitcast`+`load` the payload and bind fields (`extractvalue`→alloca), `_`/identifier arm
  is the default, arms feed a merge phi, exhaustive → `unreachable` default. Closes the
  observability loop (construct→match→extract). Guards, non-`data` scrutinees, and tuple/nested
  payload sub-patterns deferred with loud errors. Tests: `llvm_match_test.go`,
  `typechecker/tests/match_binding_test.go`.
- **`data` value construction (backend) + by-value-payload sizing gap closed.**
  `resolveForLayout` deep-resolves `UnresolvedType` payload leaves through the symbol table
  (short-circuiting `shared` refs, which keeps it finite), so a by-value named-type payload
  (`Wrap(P)`) lays out. `lowerDataConstruction` materializes the tagged union through memory
  (alloca, store tag, `getelementptr`+`bitcast`+`store` the payload, load back) for nullary +
  positional variants. `types.DataTypeConstructor.FieldTypes()` unwraps the collector's
  single-anonymous-tuple param, shared by the backend and a new typechecker width-propagation
  site (9th) so narrow ctor args take the field width. `shared`-payload and inline-record
  construction deferred with loud errors. `match` is the remaining step. Tests:
  `llvm_data_test.go`, `typechecker/tests/data_ctor_width_test.go`.
- **`data` type-declaration layout (backend).** A `data` decl lowers (same two-pass type-decl
  machinery) to its tagged union `%T = { iTAG, [K x iA] }` via `lowerDataDef` + the existing
  `DataUnionType`/`SizeAndAlign`: an `i8` tag then a payload blob sized to the largest variant.
  Enums → `{ i8 }`; recursive types finite via the `shared` (pointer) field. Not-yet-sizeable
  payloads (string, generic) defer with a loud error. Tests: `llvm_data_test.go`.
- **Struct instances (backend) — construction + field access.** `lowerStructInstanceExpr` (undef + `insertvalue`, fields keyed by name and built in declared order) and `lowerMemberExpr`
  (`extractvalue`, field position from the object's declared struct type via
  `namedStructFields`, resolving `UnresolvedType` fields through the symbol table for nested
  access). Typechecker: `inferStructInstanceExpr` now propagates the declared field width onto
  untyped literal field values (8th `propagateLiteralType` site) so narrow fields lower
  correctly. Record-update, default fields, and inline-record data constructors deferred with
  loud errors. Tests across backend (exec + IR) and typechecker. Next: `match`/`data` layout.

### 07/15/26
- **Type-declaration lowering (backend) — tuple/struct shapes to named LLVM struct types.**
  Two-pass declare-then-define (like functions) so fields can reference other named types in any
  order, forward refs included; `lowerType` resolves named refs via `structTypes`. Fields lower
  by value; `shared`/boxed, `data`/sum, and instances (construction/field access) deferred.
  Fixed the switches that had made the whole path dead code (value-vs-pointer type match, empty
  tuple case, name-keying panic). Tests: `llvm_typedecl_test.go`.
- **Grammar: positional tuple access (`pair.0`).** New `tuple_index_expr` postfix rule, distinct
  from `member_expr`; no float-token collision even for nested `pair.0.1` (context-sensitive
  lexer). Grammar + corpus only — the collector→backend wiring for tuple instances is the
  follow-on.
- **Tuple instances end to end (collector → typechecker → backend).** `TupleIndexExpr` AST node + collector; typechecker `inferTupleIndexExprType` (element type, bounds/non-tuple errors,
  named + anonymous); backend lowers construction via `insertvalue` and access via
  `extractvalue` as first-class struct values, with an anonymous-tuple structural `lowerType`
  case. Data-constructor literals still error loudly. Struct instances are next. Tests across
  all three layers + exec.
- **Allocation is now a use-site flavor only — removed declaration-level `stack`/`shared`
  modifiers.** Rationale (matches Lyra's already-made "allocation isn't nominal identity"
  decision + the explicitness ethos; Rust is the closest neighbor — no "this struct is always
  heap"): flavor is a property of a value's storage, chosen where it's used, not baked into the
  type declaration. **Grammar:** dropped the `optional(allocation)` field from
  `struct_type`/`data_type`/`named_tuple_type` (kept `allocated_type`, the use-site `let n:
  shared Node` / `field: shared Node` form). **Types/collector:** removed
  `TypeDeclStmt.Allocation` and the decl collectors' flavor collection; **added
  `TupleType.Allocation`** (now required — a use-site `shared Point` on a named tuple had
  nowhere to land, so `WithAllocation`/`AllocationOf` gained a `TupleType` case). **E014
  (`recursive_type.go`):** dropped `declIsSharedType` — a recursive cycle is broken only by a
  `shared` *field* now (`Cons(i64, shared List)`); the error message updated to match.
  **`noalloc`/EffectAlloc rebuilt use-site (the substantive part):** `buildAllocContext` no
  longer scans for `shared`-declared type names (there are none); instead
  `allocContext.allocates` reads the construction's recorded `TypeTable` flavor
  (`AllocationOf(typeTable.Get(expr)) == Shared`) — an annotated binding `let n: shared Node =
  Node{…}` records the flavor onto the construction via `checkVarDecl`, so the alloc is detected
  there. `CheckPurity` gained a `*typetable.TypeTable` param (threaded from the driver); the
  AST-only `InferredEffects` helper has no TypeTable and so no longer sets `EffectAlloc` (its
  only consumer, `InferredPureFunctions`, masks `PurityEffects` and ignores the alloc bit — no
  impact). **Still deferred:** a `shared` construction in return/argument position (flavor not
  recorded on the construction node there) is not yet detected — future escape pass. Tests:
  rewrote the E018/`noalloc`/recursive-type/collector-golden tests to source flavor from
  binding/param annotations and field-level `shared` instead of declaration modifiers; the
  AST-only `InferredEffects` alloc tests were removed (behavior now tested via the
  `CheckPurity`/E016 path). ~30 files across grammar + Go; full suite green.
- **Named tuples are actually nominal now, closing a gap in the 06/19 "positional nominal"
  decision.** `types.TypesEqual`'s `TupleType` case previously ignored `Name` entirely,
  comparing every tuple — named or anonymous — by element shape alone, so `tuple Point(i32,i32)`
  and `tuple Vector(i32,i32)` were wrongly interchangeable. Fixed: a named tuple
  (`!types.IsAnonymousTupleName(t.Name)` — the sentinel is `""` or `"?"`, both meaning
  anonymous) now compares by name alone, matching `NamedStructType`; both-anonymous stays
  structural (element-wise) as before. `isAssignable` needed no separate fix — it delegates to
  `TypesEqual` on its first line. This alone would be unsound (two unrelated literals sharing a
  name could compare equal despite different shapes), since named-tuple *construction* never
  validated a literal (`Point(3, 4)`) against its declaration the way struct literals do — so
  also fixed `inferTupleLiteralExpr`/new `inferNamedTupleLiteralExpr`: a named literal now
  requires a declared `tuple Point(i32, i32)` in `symTable.Types` and its arity + positional
  element types are checked against it (turbofish substitutes generics; no turbofish leaves a
  still-generic position unconstrained — per-position generic *inference* from supplied values,
  the way structs infer from named fields, is deliberately not attempted, a smaller scope than
  structs). Elements needed the same literal-width propagation as calls (`propagateLiteralType`,
  the 7th site) — without it, an untyped literal promotes to i64 before the assignability check
  runs and spuriously fails against a narrower declared element type (e.g. `i32`). Tests:
  `pkg/types/unify_test.go` (direct `TypesEqual` cases) +
  `pkg/analyzer/typechecker/tests/named_tuple_test.go` (construction validation, generics, the
  motivating cross-name-same-shape case). **Found along the way, left alone (pre-existing, out
  of scope):** an anonymous tuple/array literal's elements still don't propagate width against a
  `let` annotation's element types (`let a: (i32,i32) = (1,2)` errors) — a distinct gap from
  named-tuple nominality. *(The tuple half was fixed 07/16 — see that day's tuple-payload entry;
  arrays are still open.)*

### 07/14/26
- **User-defined functions (backend): definitions, calls, `return`, recursion.** Two-pass `Emit`
  (declare all → main → define all) so calls resolve before bodies exist; per-function state via
  `beginFunction`; params bound as entry-block allocas; a single `emitReturn` path shared by
  main (u8→i32 ABI), explicit `return` (`ReturnStmt`, reusing the block-sealing discipline), and
  the tail return; calls via `lowerFunctionCallExpr` with args passed un-coerced (param widths
  propagated onto literal args in `inferLambdaCall`, the sixth `propagateLiteralType` site).
  Scalar params/returns only; void/multi-clause/default-param/destructuring-param/higher-order
  forms deferred with loud errors. Tests: `TestExec_Functions` (simple call, narrow-arg wrap,
  early return, recursion, mutual recursion, call-in-loop).

### 07/13/26
- **`for` loops (backend) + three-clause form end to end.** Backend: `lowerForLoop`
  (cond/body/post/exit CFG), `break`/`continue` via a `loops []loopCtx` stack (labeled ones walk
  it), one-armed `if` statements, `MathAssignOpExpr` (`i += 1`) as load/op/store, and a
  block-termination discipline (`lowerBlockStmts` stops at a sealed block; fall-through `br`s
  guarded by `end.Term == nil`). Typechecker unblocked the full `for var i = 0; i < n; i += 1`
  form: a `MathAssignOpExpr` `inferExprType` case (void, with RHS width propagation) +
  `checkForLoopExpr` entering the loop's registered scope so the init variable resolves. Still
  open: body-local declarations don't resolve inside a loop body (needs `ForLoopExpr.Body` to
  become a pointer). Tests: `TestExec_ForLoop`, `TestExec_ForLoopThreeClause`.
- **Context-directed literal-width inference.** An untyped integer literal now takes its
  concrete width from context instead of always defaulting to i64. New typechecker helper
  `propagateLiteralType(expr, concrete)` pushes a concrete numeric width onto untyped literal
  *leaves*, recursing through width-preserving arithmetic (`+ - * / % %%`, unary `-`) and
  stopping at identifiers/calls/conversions. Wired into five context sites: annotated `let`, a
  `MathBinaryOp` with a concrete result, numeric comparisons/`==`, `var` reassignment, and the
  lambda/entry return body. It only narrows a leaf that *fits* the target width — a value that
  doesn't (`i8(x) < 300`) is left untyped rather than silently wrapped, so overflow surfaces
  loudly (the fold-based `checkIntegerLiteralRange` at decl/reassign sites, or a backend width
  mismatch) instead of miscompiling; this keeps propagation from double-reporting the overflow
  the range check already owns. Backend reads the recorded leaf width (`literalIntType`,
  fallback i64) instead of hardcoding i64, so mixed-width comparisons (`i8(x) < 3`) and narrow
  arithmetic (u8 wraps at u8, distinguishable from i64-then-truncate) now compile and run. This
  closes the deferrals threaded through `pkg/backend/llvm` (the mixed-width comparison guard is
  now defensive-only) and the `/`÷`%` truncation concern in `lowerEntry`'s doc comment.
  **Deferred:** call-argument and match-arm context, and a nicer typechecker diagnostic for a
  literal that exceeds its context width (today it's caught at lowering).
- **`let`/`var` lowering + sequential-rebind typing fix.** Backend: `let`/`var`/rebinding and
  identifier reads lower via entry-block `alloca` + store/load; `var` reassignment stores into
  the existing alloca (fixed a bug that overwrote the `locals` slot with the stored value, which
  would panic the next read). Typechecker: a rebind's initializer that reads its own name (`let
  x = x + 1`) resolves the self-reference to the prior binding — the collector now records it as
  `VarDeclStmt.Shadows` and `checkVarDecl` tracks `currentVarDecl` so the identifier redirects
  there instead of to the not-yet-typed decl (previously left nil, silently masked by
  nil-guards).

### 07/11/26
- **Comparisons + `&&`/`||` lowering.** The six comparison ops lower to a single `icmp` with the
  right signed/unsigned predicate; `&&`/`||` short-circuit via a cond-br + phi diamond reusing
  the `if` machinery (the constant branch is a phi edge, no extra block). Enables non-constant
  `if` conditions. Comparisons are int-only (float/mixed-width deferred, loud error). Tests:
  `TestEmit_BoolBinaryOp`, `TestExec_BoolShortCircuit`.
- **`if`/`else` + blocks lowering.** `lowerIf` builds the standard cond-br → then/else →
  merge-phi diamond; `lowerBlock` lowers to its last statement's value. `lowerExpr` now returns
  *(value, endBlock, err)* so a branching form can move the insertion point. Conditions were
  still bool literals only at this point (comparisons landed later same day). Tests:
  `TestExec_If`.
- **Typechecker: one-armed `if` as a value is now an error.** A one-armed `if` used as a value
  has no result when false, so `checkIfExpr` now requires a terminal `else`; as a statement it's
  still fine. Prereq for the backend `if` lowering above.
- **Int-width conversions — `i8(x)`, `u32(x)`, etc.** Lyra's one conversion syntax lowers to
  `trunc`/`sext`/`zext`/identity based on the source's signedness and lowered bit width — the
  only way to exercise a non-i64 width today. Verified with an overflow-wrap case and a
  division-based test that actually distinguishes `sext` from `zext`. Float conversions deferred
  (unreachable from valid source yet).
- **Arithmetic — `+ - * / % %% -(unary)`.** All lower and are tested behaviorally
  (compile+run+check exit code). Matches Odin's split: `%` (Mod) is truncated (native
  `srem`/`urem`), `%%` (Remainder) is floored via a branchless `select` sign-fixup
  (`lowerFlooredSRem`); unsigned floored/truncated remainder are identical. Also fixed a real
  gap where a bare literal used directly as a binary operand had no TypeTable entry.
- **`main`'s entry-point convention changed from `i64` to `u8`** — the OS truncates a process
  exit code to its low 8 bits regardless of declared width (verified: even real C `int main() {
  return 300; }` exits 44, not 300), so `i64` only added the silent-truncation surprise Lyra
  rejects elsewhere; `u8` makes the 0–255 constraint visible in the type itself (matches Zig's
  `pub fn main() u8` and Rust's move to the narrow `std::process::ExitCode`).
  `driver.ResolveEntryPoint`/`EntryReturn` updated. Also fixed an adjacent, unrelated-but-real
  ABI bug found while researching this: `@main` at the LLVM level was declared `i64`, but the
  actual C runtime signature is `i32` (verified against real clang output) — it happened to
  produce correct results only by x86-64 register-truncation coincidence. `@main` is now
  correctly `i32`, with the Lyra-level `u8` body value coerced (new `coerceIntWidth` helper,
  shared with `lowerNumericConversion`) and zero-extended into it. Tests + all affected
  exec-test sources updated (some needed an explicit `u8(...)`/`i8(...)` wrapper, since e.g.
  negating a literal produces `UntypedSignedInt`, which `isAssignable` correctly refuses to
  assign directly to unsigned `u8`).
- **First real lowering** — `lowerEntry` + `lowerExpr` (`llvm.go`) lower an integer-literal
  `main` body to a real `ret`, so `let main = () -> i64 => 42` compiles+runs to exit 42 (`=> 7`
  → 7; `-> void` → 0). `lowerExpr` returns an error for unhandled forms so the build fails
  loudly rather than emitting wrong code. Tests: `llvm_test.go`
  (`TestEmit_IntegerLiteralBody`/`_VoidEntry`/`_UnsupportedBody`).
- **llir/llvm set up for the backend** — added `github.com/llir/llvm` v0.3.6 (pure Go);
  `Emit`/`lowerEntry`/`declareRuntime` now build a real `ir.Module` instead of string assembly,
  and `layout.go`'s type helpers (`LLVMPrimitive`/`SharedBoxType`/`TagType`/`DataUnionType`)
  return llir `types.Type`. Placeholder `@main` module still compiles + runs via clang. Typed
  pointers (`i8*`), not opaque — fine for the scalar milestone. Tests updated to compare via
  `.String()`; full suite green.

### 07/10/26
- **Backend layout helpers scaffolded** — `pkg/backend/llvm/runtime.go` (shim names + `PinnedRC` + `emitRuntimeDeclarations`, now emitted into every module) and `layout.go` (`LLVMPrimitive`,
  `SharedBoxType`, `TagType`, `DataUnionType`, `SizeAndAlign`). `SizeAndAlign` implements
  C-style struct padding, static-array stride, and the sum-type union sizing, and treats a
  `shared` value as pointer-sized (so recursive `shared` types are finite). Emitted IR (now with
  the shim `declare`s) still compiles+runs. Tests: `layout_test.go` (12 cases). The type toolkit
  `lowerType` will call; expression/statement codegen is still the from-scratch work.
- **`data`/sum-type layout decided** (backend) — tagged union `%T = { tag, payload-blob }`:
  smallest-fitting int tag in declaration order, payload sized/aligned to the largest variant
  and accessed as the active variant's struct. Orthogonal to the alloc flavor (inline vs boxed).
  Monomorphized generics; recursive occurrence is `shared` (a `ptr`, finite size).
  Niche/tag-fold optimization (e.g. `Maybe<ptr>` = null) deferred. Spec:
  `pkg/backend/llvm/DATA_LAYOUT.md`.
- **`stack`/`shared` representation decided** (#5 (d)) — `stack` = inline value; `shared` =
  `ptr` to a ref-counted box `{rc, payload}` with retain/release driven by `own`/`ref`/`mut`,
  and arena values pinning the rc for bulk free. Full spec + runtime-shim signatures in
  `pkg/backend/llvm/ALLOCATION.md`. Non-atomic refcounts (parallel readers borrow, so no rc
  races); atomic/weak/COW deferred.
- **LSP migrated onto `driver.Analyze`** — `cmd/lyra-lsp`'s ~300-line inline pipeline replaced
  by a thin `driver.Analyze` + `diagToLSP` wrapper; pipeline now defined once. LSP suite green.
- **LLVM backend skeleton** — `pkg/backend` interface + `pkg/backend/llvm` (textual IR); `lyrac
  build` writes a placeholder `main` module confirmed to compile and run (exit 0).
- **Program entry-point convention** — `driver.ResolveEntryPoint`: a zero-param top-level `let
  main` returning `i64`/`void`; build-time only, enforced by `lyrac build`.
- **Builtin-method registration** — `typechecker/builtins.go`;
  `wrapping_/saturating_{add,sub,mul}` type-check on integer receivers. Primitives are now valid
  method receivers (missing → `T has no method "x"`).
- **Removed the `given` keyword** — retired the grammar rules, reserved word, AST node, and
  checker cases; corpus + suite green.
- **Purity scope Phase 2 (lambdas + free functions)** — purity reads the collector's ScopeTable
  instead of re-walking the AST; zero behavior change. Methods deferred.
- **Allocation-compatibility check (`lyra-E018`)** — owning a value across a `stack`↔`shared`
  boundary is an error at binding/reassign/lvalue sites; fires only on concrete differing
  flavors (`Unspecified` is polymorphic).
- **…args/returns** — E018 extended to `own` arguments and owned returns; borrowed (`ref`/`mut`)
  are polymorphic and skipped.
- **…element-level** — E018 recurses into tuple/array elements; fixed a pre-existing bug where
  named tuple/array element types weren't resolved by `resolveType`.

### 07/09/26
- **Per-trait-method effect bounds** — a trait method may be declared `pure`/`det`/`noalloc` as
  a contract every impl must satisfy (`E007`/`E016`; `E015` for `pure det`).
- **Bound-dispatched calls visible to purity** — a call through a `where` bound scores as the
  join over all impls (pure/det only if all are).
- **Impl method body return-type checking** — each impl method body is checked against the
  trait's declared return type (Self + trait params substituted).
- **Binding a trait's own type params** — `impl Get<t> for Box<t>`; `box.get()` on `Box<i64>`
  returns `i64`.
- **Bounded polymorphism in method bodies** — calling a trait method on a bare type-param value
  dispatches through its `where` bound (abstract dispatch).
- **Generic struct field access** — `b.value` on `Box<i64>` substitutes type args into field
  types → `i64`.
- **Generic impl dispatch + bound checking** — `impl Show<t> for Box<t>` unifies against a
  concrete receiver; `where` bounds constraint-checked.
- **`noalloc` catches unknown external calls** — an unresolvable call taints alloc too, so
  `noalloc` flags it.
- **ScopeTable population (Phase 1)** — collector records node→scope for lambda params, both
  loops, and `with` blocks.
- **Error-type checking for `Result?`** — `?` compares the operand's error type against the
  enclosing return (assignability-only).
- **Canonical Result/Maybe identity** — a `CanonicalKind` stamp replaces per-site name matching
  (`@builtin` or name+shape fallback).
- **`det` rand/time detection** — ambient `Random.global()`/`wallClock()` carry Rand/Time so
  `det` forbids all nondeterminism sources.

### 07/08/26
- **ML-style function sugar** — `let name(params) => body` (and modifier-led `let pure
  name(...)`) desugar to `let name = (params) => body`.

### 06/24/26
- **Recursive-type well-formedness check (`lyra-E014`)** — a by-value type cycle errors unless
  broken by `shared`.
- **Alloc as type identity (first step)** — types carry an `Allocation` flavor through the AST;
  `AllocationOf`/`WithAllocation` added.
- **Method-to-method call tracking** — impl method bodies are type-checked so inner `.`-calls
  dispatch into MethodTable, feeding the purity fixpoint.
- **Trait methods can be `pure`; real method dispatch** — `obj.method()` / `Trait::method()`
  resolve to an impl, type-check, and report ambiguity; recorded in a new MethodTable.
- **Purity inference for unannotated methods** — inferred via a joint fixpoint over functions +
  methods.
- **Impurity of imported/external functions** — unresolvable calls (not builtins/conversions)
  are conservatively impure.
- **Fixed: parser hang on a lambda-typed struct field** — nil-guard on an absent optional
  `parameter_types` node.

### 06/23/26
- **Purity inference records *pure* as queryable** — `InferredPureFunctions` exposes every
  top-level function's inferred purity by name.

### 06/22/26
- **Fixed: `if let`/`let … else` were never type-checked** — added the missing `checkNode`
  cases.
- **Purity foundation** — if-let/let-else names registered with correct scoping;
  if-let/else/`with`-arena bindings treated as locals.
- **Purity: `await` is an impure effect.**
- **Constant-folded division-by-zero** — folds constant int expressions, not just bare `0`.
- **Fixed: `DataPattern` as a lambda parameter mis-parsed** — added `PREC.DATA_PATTERN`.
- **Fixed: destructuring never bound names** — `walkDestructuredPattern` binds all pattern
  kinds.
- **Result/Maybe shape validation** — recognition checks constructor shape, not just name.
- **Confirmed generic type params are lowercase-only by design** (not a bug).

### 06/21/26
- **Purity: impurity inference for non-top-level functions** (keyed by lambda pointer).
- **Purity: track captured mutable bindings from non-top-level enclosing scopes.**

### 06/19/26
- **Purity: reject reading captured mutable globals (`lyra-E007`).**

### 06/17/26
- **LSP:** folding ranges, workspace symbols, signature help, rename + prepare-rename, document
  highlight; `@sizeof` on unknown types.

### 06/16/26
- **Fixed keyword carving in identifiers** (grammar) — `letter` no longer lexes as `let`+`ter`.
- **LSP:** semantic tokens, completion, find references.

### 06/15/26
- **LSP:** code actions / quick fixes.
- **Fixed: nullary constructor swallowed the following statement** (grammar) — residual: nullary
  binding + bare call still swallowed.
- **`const` requires a compile-time-constant initializer (`lyra-E012`).**
- **Unsafe ops outside `unsafe` require an `unsafe` context (`lyra-E011`).**
- **Wire `ref`/`mut`/`own` parameter modifiers into mutation/purity checks.**
- **Struct field mutability** — mutable by default, with a deep `readonly` freeze marker.
- **Fixed: named-struct field types weren't resolved** (broke nested member access/literals).
- **Safe mutable lvalues / three-level binding** — added `let mut`.

### 06/14/26
- **Allow same-scope sequential rebinding** (`let x = parse(x)`).
- **Require initialization at declaration (`lyra-E010`).**
- **Fixed: nullary data constructors as values were dropped.**
- **One conversion syntax decided** — `f32(x)` is the single widening form.
- **Non-exhaustive `match` on closed types is an error (`lyra-E009`).**
- **Restrict `??` to optional operands (`lyra-W007`).**

### 06/13/26
- **Must-use `Result`/`Maybe` (`lyra-W006`)** — dropping one without binding/match/`?` warns.

### 06/12/26
- **Subtraction parser bug fixed** (`let x = 0 - 200` dropped `- 200`).
- **Constant-folded arithmetic overflow** (static slice, annotated types).
- **`?` (try) propagation operator (`lyra-E008`).**
- **Removed platform-dependent `int`/`uint` and bare `float`** — untyped int literals default to
  `i64`.
- **Trait/impl conformance** — missing-method/arity errors, warns on extra methods.

### 06/11/26
- **Match arm validation + exhaustiveness** for boolean, tuple, and named-struct scrutinees;
  duplicate/overlapping arms.

### 06/10/26
- **Type checks:** division/modulo by literal zero; always-true/false conditions; float
  `==`/`!=` warning; for-loop condition must be `bool`; null-coalescing operand types; range
  operand types; for-in iterable must be iterable; tuple/array literal element types.
- **Unused function parameters; unused imports (`lyra-W004`).**
- **Diagnostic codes** attached to all diagnostics; **better parser error ranges** from CST
  ERROR/MISSING nodes.

### 06/09/26
- **Unreachable code; unused variables** (`TagUnnecessary`).
- **`_`-prefixed identifier grammar fix.**
- **`DiagnosticTag` + related-information support** in diagnostics.

### 06/04/26
- **Context checks:** `await`/`break`/`continue`/`yield`/`return` outside their valid context.
- **LSP:** document symbols, inlay hints, go-to-definition.

### 06/03/26
- **LSP: hover.**
- **Type checks:** member access; higher-order/non-identifier callees; unresolved identifiers;
  undefined functions; unknown type names; index access.

### 06/02/26
- **Regex engine:** Unicode properties (`\p{…}`) and performance (SIMD/DFA); wired into
  `RegexLiteralExpr` + `PatternConstraint`.

### 06/01/26
- Various bugfixes and improvements.

### 05/31/26
- **Integer-literal overflow/range checking; duplicate-declaration detection; shadowing
  detection; regex lookarounds.**

### 05/27–29/26
- **Match exhaustiveness** for array/string/number/data scrutinees.
- **Regex engine Phase 1** — derivative DFA (intersection, complement, flags,
  `IsMatch`/`FindAll`).

### 05/16–20/26
- **Struct-literal + record-update type-checking; function-declaration type-checking; if/else
  type-checking; scope-aware variable resolution.**

### 05/13–15/26
- **Comparison / boolean operators; string concatenation (`++`); type conversion; int-as-float
  literals.**
- **Added the typechecker; added the LSP server** (wired to the VS Code extension).
- **Collected:** arena statements, regex, pointers, negation, var reassignment, data
  constructors, unsafe blocks, compose/yield/generators; `@sizeof`.

### Earlier (01–05/26)
- **Grammar/collector foundation:** trait decls + impls, function/lambda types, modules +
  imports, match expressions, if-let destructuring forms, tuple/struct/array/data destructuring,
  postfix expressions, array literals + comprehensions, range expressions, tuple types,
  constrained types (range/precision/literal/step/pattern), for/for-in loops with labels,
  math-assignment operators, character literals, function guards + bodies, `i8`/`i16`/`f32`/etc.
- **Collector refactored** into subpackages; tests moved to the golden-print harness.

## Backend lowering log (07/10 – 07/17)

The LLVM backend built up in slices, each landing end to end (emit → clang → run → check
the exit code) rather than as a layer. Kept as one list because that is what it is: the
order the language became executable in.

- `pkg/driver.Analyze(source) → Result` — one call returning the typed program + all tables +
  normalized diagnostics (the backend's input).
- `pkg/backend` interface + `pkg/backend/llvm` skeleton; `cmd/lyrac check`/`build` on top.
  `build` emits a placeholder `main` module that compiles with `clang` and runs — toolchain path
  proven.
- Entry point: top-level `let main = () -> u8` (exit code) or `-> void`
  (`driver.ResolveEntryPoint`, build-time only). `u8`, not i64 — see the 07/11 "u8 entry point"
  Completed entry for why.
- **[07/11]** `github.com/llir/llvm` (v0.3.6, pure Go) set up: `Emit` builds a real `ir.Module`,
  `layout.go` returns llir types, runtime shims declared. Emitted IR compiles + runs. Note:
  typed pointers (`i8*`), not opaque. Lowering order: types → trivial `main` → expressions →
  control flow → runtime shims (`print`, overflow trap).
- **[07/10]** `cmd/lyra-lsp` migrated onto `driver.Analyze` — the pipeline lives in one place
  now.
- **[07/10]** Layout helpers in code (`runtime.go`, `layout.go`): runtime-shim `declare`s
  (`emitRuntimeDeclarations`, wired into `Emit`), `SharedBoxType`, `TagType`, `DataUnionType`,
  and a `SizeAndAlign` engine (shared = ptr-sized). Ready for `lowerType` to call.
- **[07/11]** First lowering — `lowerEntry`/`lowerExpr` lower an integer-literal body, so `let
  main = () -> i64 => 42` exits 42 (`=> 7` → 7, `-> void` → 0). Unsupported bodies error loudly.
  Next: `let`/`if`/blocks.
- **[07/11]** Arithmetic — `+ - * / % %% -(unary)` all lower and are tested behaviorally
  (compile+run+check exit code, since IR isn't constant-folded). Signed vs unsigned `Div`/`Rem`
  chosen via a new `IsSignedInt` helper. **Decided (matches Odin's `%`/`%%` split):** `%` (Mod)
  is truncated — sign follows the dividend, exactly LLVM's native `srem`/`urem` (`11 % -3 = 2`).
  `%%` (Remainder) is floored — sign follows the divisor (`11 %% -3 = -1`), needing a branchless
  `select`-based sign-fixup after `srem` (`lowerFlooredSRem`); unsigned floored remainder is
  identical to truncated (nothing to floor), so `urem` covers both. Integer negation lowers as
  `sub 0, x` (LLVM has no dedicated int negate); float negation uses `fneg`. Also fixed a real
  gap: `inferExprType` only cached a type when a specific case called `Set` itself, so a bare
  literal used directly as a binary operand had no TypeTable entry — split into a caching
  wrapper + `inferExprTypeUncached` so every non-nil result is cached.
- **[07/11]** Int-width conversions — Lyra's one conversion syntax (`i8(x)`, `u32(x)`, …,
  Pit-of-Success #5) now lowers to `trunc`/`sext`/`zext`/identity, picked from the source's
  signedness (`IsSignedInt`) and the already-lowered operand's own bit width. This is the only
  way to exercise a non-i64 width in valid source today (bare literals default to i64; no
  implicit narrowing/widening between concrete int types). Verified with overflow-wrap cases
  (`u8(200)+u8(100)` → wraps mod 256) and a division-based test that actually distinguishes
  `sext` from `zext` (an additive check can't — the two candidate values for a negative narrow
  source always differ by exactly 256, invisible mod 256 in an exit code). **Float-target/source
  conversions deliberately deferred** — confirmed no valid, type-checked program can reach that
  path today (`main` must return u8, no `float→int` builtin), so it errors explicitly rather
  than shipping an untestable instruction sequence.
- **[07/11]** `if`/`else` + blocks lowering — `lowerIf` builds the standard cond-br → then/else
  → merge-phi diamond; `lowerBlock` lowers a block to its last statement's value (only
  `ExpressionStmt` for now — no `let` yet). `lowerExpr` now returns *(value, endBlock, err)*: a
  branching form moves the insertion point, so callers keep lowering into the block control ends
  in (every non-branching case returns its block unchanged). Phi predecessors use the block each
  branch *ends in* (thenEnd/elseEnd), verified with nested-if tests where a branch's control
  moves into an inner merge block. Conditions are bool literals for now (comparisons/`&&`/`||`
  not lowered → no non-constant conditions yet); `-O0` keeps the branch so it's still exercised
  at runtime. Tests: `llvm_test.go` `TestExec_If`.
- **[07/11]** Typechecker: a one-armed `if` used as a *value* is now an error ("`if` used as a
  value must have an `else` branch") — it has no result when the condition is false. As a
  *statement* it's still fine (conditional side effect). Correctly requires a terminal `else` on
  an `if…else if…` chain in value position. `checkIfExpr`; tests in `expr_if_test.go`. (Prereq
  for the backend `if` lowering, which can now assume both branches exist.)
- **[07/13]** `for` loops lowering (backend) — the C-style cond/body/post/exit CFG with a
  back-edge (`lowerForLoop`), plus `break`/`continue` (a `loops []loopCtx` stack on the lowerer;
  labeled break/continue walk it), and one-armed `if` statements (needed for `if cond { break
  }`). Introduced the block-termination discipline the earlier lowerings didn't need:
  `lowerBlockStmts` stops at a sealed block and every fall-through `br` is guarded by `end.Term
  == nil`, since break/continue are the first constructs that terminate a block mid-stream.
  `lowerBlock` split into a value-optional `lowerBlockStmts` + a value-requiring wrapper +
  `lowerForEffect` (loop/one-armed-if bodies need no value). Tests: `TestExec_ForLoop`
  (accumulator, break, continue, nested labeled break).
- **[07/13]** Three-clause `for var i = 0; i < n; i += 1` form now type-checks and lowers end to
  end. Fixed the two frontend gaps: (a) `MathAssignOpExpr` (`+=`) got an `inferExprType` case
  (delegates to `checkMathAssignOp`, result `void`), so a `+=` in value position (the loop
  `Post`, or a block's last statement) no longer hits "unknown expression type";
  `checkMathAssignOp` also now propagates the target's width onto the RHS (`i += 1` with a
  narrow `i`). (b) `checkForLoopExpr` enters the loop's own registered scope (`RecordScope(loop,
  loopScope)`) around the init/condition/post/body checks, so the init variable resolves
  everywhere (and the init-clause condition is now genuinely checked, not skipped). Backend
  lowers `MathAssignOpExpr` as load/op/store (`lowerMathAssignOp`, reusing the extracted
  `applyIntMathOp`). Tests: `TestExec_ForLoopThreeClause` (upward, `+=`-in-body, downward `-=`,
  narrow-u8 counter) + typechecker tests. **[FIXED 07/29]** a `let`/`var` declared *inside* a
  loop body wasn't visible there — the collector puts body-locals in a child block scope keyed
  to the original body pointer, but `ForLoopExpr.Body` was a value copy so `inferBlockType`'s
  `enterScope` couldn't reach it. Both loop bodies are pointers now; see the Completed entry.
- **[07/14]** User-defined functions + calls + recursion (backend). Two-pass `Emit`: every
  top-level `let name = <lambda>` is `declareFunction`'d (signature into `l.funcs`) before any
  body, so a call from main, between functions, or a recursive self-call resolves;
  `defineFunction` then lowers each body. Per-function state reset via `beginFunction` (fresh
  `locals`/`loops` + `retType`/`retSigned`/`entryABI`); params bind as entry-block allocas keyed
  by name (like `let`/`var`). `emitReturn` is the single return path (coerces to the declared
  width; main's `entryABI` does the u8→i32 ABI slot), shared by explicit `return` (`ReturnStmt`,
  sealing its block via the break/continue discipline) and the implicit tail return; `main`
  (`lowerEntry`) now routes through it too. Calls lower via `lowerFunctionCallExpr` (resolve
  `l.funcs`, lower args, `NewCall`); args pass un-coerced because the typechecker propagates
  each param's width onto its literal args (the sixth `propagateLiteralType` site,
  `inferLambdaCall`). Scalar params/returns only (`lowerType` handles `PrimitiveType`).
  **Deferred, loud errors:** void/multi-clause functions, default params, destructuring params,
  higher-order (lambda-value) calls. Tests: `TestExec_Functions`.
- **[07/13]** `let`/`var` bindings + identifier reads + `var` reassignment lowering — locals
  modeled as entry-block `alloca` + store/load (mem2reg builds SSA), name→alloca in
  `lowerer.locals` (`lowerVarDecl`/`lowerVarReassignment`, `IdentifierExpr` in `lowerExpr`).
  Prereq typechecker fix: a same-scope rebind's initializer (`let x = x + 1`) now types its
  self-reference against the prior binding (`VarDeclStmt.Shadows`) instead of leaving it nil.
- **[07/11]** Comparisons + `&&`/`||` lowering — the six comparison ops lower to a single `icmp`
  with the right signed/unsigned predicate (`eq`/`ne` signedness-agnostic, incl. bools);
  `&&`/`||` short-circuit via a cond-br + phi diamond (`a && b` ≡ `if a { b } else { false }`,
  `||` the mirror), reusing the `if` machinery — the constant branch is virtual (a phi edge, no
  block), so only one rhs block + merge are created. Enables non-constant `if` conditions (`if x
  < 3 && y > 0`). Comparisons are int-only (float + mixed-width deferred → explicit error, not
  invalid IR). Tests: `TestEmit_BoolBinaryOp`, `TestExec_BoolShortCircuit`,
  `TestEmit_ComparisonMixedWidth_Error`.
- **[07/15]** Type-declaration lowering (backend) — top-level `tuple`/`struct` decls lower to
  named LLVM struct types in two passes (`lowerTypeDeclarations` then `lowerTypeDefinitions`,
  mirroring functions): `declareNamedStruct` registers an empty placeholder per decl (keyed by
  declared name) before any body, so fields may reference other named types in any order incl.
  forward refs (`struct Line { a: Point }` → `%Line = type { %Point, %Point }`). `lowerType` now
  resolves named refs (`TupleType`/`NamedStructType`/`UnresolvedType` → the registered struct).
  Fields lower by value; `shared`/boxed is deferred to ALLOCATION.md lowering.
  `data`/`newtype`/constrained decls error loudly. **Instances (construction, field access) are
  not lowered yet** — this is only the type shapes. Tests: `llvm_typedecl_test.go`.
- **[07/15]** Grammar: positional tuple access (`pair.0`) — new `tuple_index_expr` postfix rule
  (`tree-sitter-lyra/include/expressions/postfix.js`), distinct node from `member_expr` (index
  is `decimal_int`, not an identifier). No float collision (tree-sitter's context-sensitive
  lexer never offers `float_literal` after `obj .`), so even nested `pair.0.1` lexes as two
  indices. Grammar + corpus only (`test/corpus/expressions/postfix.txt`);
  collector/typechecker/backend wiring for tuple instances is the follow-on.
- **[07/15]** Tuple instances end to end (collector → typechecker → backend). New
  `TupleIndexExpr` AST node (collector `collectTupleIndexExpr` off `tuple_index_expr`, parsing
  the index). Typechecker `inferTupleIndexExprType` resolves the object to a `TupleType`,
  bounds-checks the index, returns the element type (named + anonymous tuples;
  out-of-range/non-tuple errors). Backend lowers construction (`lowerTupleLiteralExpr` → undef +
  `insertvalue` per element) and access (`lowerTupleIndexExpr` → `extractvalue`) as first-class
  struct SSA values, so a `let`-bound tuple round-trips through the alloca/store/load path;
  `lowerType` gained an anonymous-tuple structural case (`lowerAnonymousTupleType`); a
  data-constructor literal (`Some(42)`) still errors loudly (DataType, not a tuple). Tests:
  `llvm_tuple_test.go` (exec + IR), `typechecker/tests/tuple_index_test.go`, `collector/tests`
  golden. **Struct instances (construction + field access) are still the next step.**
- **[07/16]** Struct instances (backend) — construction + field access.
  `lowerStructInstanceExpr` builds the declared struct via undef + `insertvalue`, keying literal
  fields by name and building in *declared* order (out-of-order literals lower correctly);
  `lowerMemberExpr` reads a field via `extractvalue`, finding the field's position from the
  object's declared struct type (`namedStructFields`, which resolves an `UnresolvedType` field
  via `res.SymbolTable.Types` so nested `line.start.x` works). Typechecker fix:
  `inferStructInstanceExpr` now propagates the declared field width onto an untyped literal
  field value (8th `propagateLiteralType` site) — without it a `u8` field's `3` stayed
  `untyped_int` and lowered at i64, mismatching the i8 field. Deferred, loud errors:
  record-update (`P { base | f: v }`), default-valued missing fields, inline-record data
  constructors. Tests: `llvm_struct_test.go` (exec incl. nested + through-call, IR,
  record-update-deferred), `typechecker/tests/struct_field_width_test.go`. **Next: `match` and
  `data` layout.**
- **[07/16]** `data` type-declaration layout (backend) — a `data` decl now lowers (in the same
  two-pass type-decl machinery) to its tagged-union struct `%T = { iTAG, [K x iA] }` via
  `lowerDataDef` + the existing `DataUnionType`/`SizeAndAlign` helpers: an `i8` tag
  (declaration-order variant indices) followed by a payload blob sized/aligned to the largest
  variant. Enum → `{ i8 }`; positional/mixed payloads → blob at the widest variant; recursive is
  finite because the recursive field is `shared` (pointer-sized, lyra-E014). **Layout/shape only
  — construction and `match` are the next slices.** Deferred, loud error: a not-yet-sizeable
  payload (`string`, un-monomorphized generic, or a by-value named-type ref stored as an
  `UnresolvedType` that `SizeAndAlign` can't size — a *recursive* `shared` ref is fine). Tests:
  `llvm_data_test.go` (emit layout, clang-validity, by-value-payload deferred).
- **[07/16]** `data` value construction (backend) + closed the by-value-payload sizing gap.
  **Sizing gap:** `resolveForLayout` deep-resolves a payload's `UnresolvedType` leaves through
  the symbol table (short-circuiting a `shared` ref, which also keeps it finite), so a by-value
  named-type payload (`Wrap(P)`) now lays out. **Construction** (`lowerDataConstruction`):
  materializes the union through memory (per DATA_LAYOUT.md) — alloca, store the tag, and for a
  payload variant `getelementptr` the blob + `bitcast` to the payload struct + `store` — then
  loads it back as a first-class value; nullary (`DataConstructorExpr`) and positional
  (`TupleLiteralExpr` recording a `DataType`) both lower. Added
  `types.DataTypeConstructor.FieldTypes()` (unwraps the collector's single-anonymous-tuple
  param) — used by both the backend and a new typechecker propagation site (9th) so a narrow
  ctor arg (`Wrap(200)` with `u8`) takes the field width instead of promoting to i64. Deferred,
  loud error: `shared` payload construction (needs ref-counted alloc), inline-record data
  constructors. **`match`/destructuring is the remaining step** (a constructed value can't be
  read back yet). Tests: `llvm_data_test.go` (exec construction, IR shape, shared/by-value
  cases), `typechecker/tests/data_ctor_width_test.go`.
- **[07/16]** `match`/destructuring on a `data` value (typechecker + backend). **Typechecker:**
  match-arm pattern variables are now bound in the arm body (`checkMatchExpr` →
  `withPatternBindings` → `walkDestructuredPattern`, reusing the `paramTypes` map, which also
  records each resolved identifier into the TypeTable); `bindDataPatternPayload` rewritten to
  accept both flat (`Rect(w, h)`) and single-tuple-param (`MkPair((x, y))`) forms via
  `FieldTypes()`. Arm-body width propagation added (10th `propagateLiteralType` site + new
  `MatchExpr`/`IfExpr`/`BlockExpr` recursion) so a bare `0` arm adapts to a concrete sibling or
  the declared return type instead of defaulting to i64. **Backend**
  (`lowerMatch`/`lowerDataMatch`): store scrutinee → load tag → `switch` per arm; a data-pattern
  arm `bitcast`+`load`s the payload struct out of the blob and binds fields (`extractvalue` →
  alloca → `l.locals`); `_`/identifier arm is the switch default; arms feed a merge phi;
  exhaustive matches get an `unreachable` default. This closes the observability loop —
  construct → match → extract → return. Deferred, loud errors: arm guards, non-`data` scrutinee,
  tuple-payload destructuring, nested payload sub-patterns. Tests: `llvm_match_test.go` (exec:
  enum/single/multi/wildcard/identifier/inline; IR; guard+non-data deferred),
  `typechecker/tests/match_binding_test.go`.
- **[07/16]** Value-testing `data` payload sub-patterns (backend) — `Some(0)`. A single tag
  `switch` can't route two same-tag arms to different payload tests, so: nested in an aggregate
  (`(c, Some(0))`), `aggPatternTest`'s `DataPattern` case ANDs the tag check with a branchless
  test per value-testing payload field (extract the payload, recurse — harmless when the tag
  mismatches, the AND is already false); top-level (`match m { Some(0) => .., Some(x) => .. }`),
  `lowerDataMatch` detects a payload test (`dataMatchHasPayloadTest`) and falls back to the
  shared if-else ladder (`lowerAggregateMatch`) instead of the `switch`, keeping the compact
  `switch` for the no-test case. Only arm guards and string/float/array scrutinees remain
  deferred. Tests: `llvm_match_test.go` (`TestExec_DataLiteralPayload`,
  `TestEmit_DataLiteralPayloadIR`, nested cases in `TestExec_NestedDataPatterns`).
- **[07/16]** Floats (backend) — literals, arithmetic, comparisons, conversions, params/returns.
  Float literals lower at their recorded width (`literalFloatType`, default f64);
  `applyFloatMathOp` covers `fadd`/`fsub`/`fmul`/`fdiv`, `frem` (`%`), and a `select`-based
  floored `frem` (`%%`, `lowerFlooredFRem`); `fneg` was already there; `lowerFloatComparison`
  emits `fcmp` (ordered, `une` for `!=`); `lowerNumericConversion` gained int→float
  (`sitofp`/`uitofp`) and float-widening (`fpext`); `emitReturn` handles a float return.
  `lowerMathBinaryOpExpr`/`lowerMathAssignOp`/`lowerBooleanBinaryOpExpr` dispatch on the
  operand's LLVM type. At the time, a float reached the u8 exit code only through a comparison
  (no float→int conversion); explicit rounding (`floor`/`ceil`/`round`) landed the next day —
  see the 07/17 entry. Tests: `llvm_float_test.go` (arithmetic, conversions, float function,
  IR), all compiled + run via clang.
- **[07/16]** Strings (backend) — literals, equality, `match`, params/returns. Representation
  decided: immutable fat pointer `{ i8* data, i64 len }` (byte length, not NUL-terminated;
  `StringLLVMType`, spec in `STRING_LAYOUT.md`). Literals intern their bytes in a private
  immutable global + build the struct from a `getelementptr` + length (`lowerStringConstant`, no
  allocation). `==`/`!=` are branchless `len_eq && memcmp(min)==0` (libc `memcmp`, lazily
  declared; `lowerStringEquality`/`lowerStringComparison`). String `match` joins the scalar
  ladder (`stringScalarMatchTest`, literal arms = byte-equality, identifier binds the fat
  pointer; escaped/regex patterns deferred). By-value params/returns (`emitReturn` aggregate
  path). At the time, concatenation `++` and interpolation were deferred (need a heap
  allocator); `++` landed 07/17 — see that entry. Still deferred: interpolation (value→string
  formatting), `print`, escaped/regex patterns. Tests: `llvm_string_test.go`
  (equality/function/match/IR/deferred), all built + run via clang.
- **[07/16]** Float scalar `match` (backend). `lowerScalarMatch` and the `lowerMatch` dispatch
  now accept a float LLVM type, not just integer; `scalarMatchTest` delegates a float scrutinee
  to `floatScalarMatchTest` (`fcmp oeq` for a literal, ordered two-sided range check for a range
  arm, `constFloatFromExpr` folding float/int/negated bounds). Identifier catch-alls bind the
  float; guards work unchanged; a float match always needs a wildcard (typechecker warns
  otherwise). Only string/array match scrutinees remain deferred. Tests: `llvm_float_test.go`
  (`TestExec_FloatMatch` literal/wildcard/range/binding/f32/guard, `TestEmit_FloatMatchIR`).
- **[07/16]** Match arm guards (typechecker + backend) — `Some(x) if x > 0`. Typechecker checks
  each guard condition with the pattern's bindings in scope and requires `bool`
  (`checkMatchExpr`); guarded arms already didn't count toward exhaustiveness. Backend
  `lowerGuardedArmBody` evaluates the guard after the pattern matches and its vars are bound,
  cond-branching to the body (true) or the next arm (false); it plugs into `lowerScalarMatch`
  and `lowerAggregateMatch`, a guarded arm never seals the ladder, and a `data` match with any
  guard takes the ladder fallback (`matchHasGuard`) instead of the `switch`. Only
  string/float/array scrutinees remain deferred for `match`. Tests: `llvm_match_test.go`
  (`TestExec_MatchGuards` across data/scalar/struct/tuple, `TestEmit_MatchGuardIR`),
  `typechecker/tests/match_binding_test.go`.
- **[07/17]** Float→int rounding (typechecker + backend) — `x.floor()`/`.ceil()`/`.round()`.
  Registered as float-receiver-only builtins (`typechecker/builtins.go`'s `floatRoundingOps`,
  zero args, fixed `i64` return — narrow further via `i32(x.floor())`), the explicit escape
  hatch the numeric-conversion error already pointed to. Backend: this is also the first *method
  call* (`MemberExpr` callee) the LLVM backend lowers at all — `lowerFunctionCallExpr` now
  dispatches a `MemberExpr` callee to `rounding.go`'s `lowerBuiltinMethodCall`, which resolves
  the receiver's Lyra float type, calls the matching lazily-declared `llvm.<op>.<width>`
  intrinsic (`round` = half-away-from-zero, C/Rust-style, not `rint`/`nearbyint`), then
  `fptosi`s to i64. Out-of-range/NaN is left as ordinary `fptosi` poison (no range check —
  matches arithmetic's still-deferred checked-by-default). Tests:
  `typechecker/tests/builtin_rounding_test.go`, `llvm_float_test.go` (`TestExec_FloatRounding`,
  `TestEmit_FloatRoundingIR`).
- **[07/17]** Ref-counted heap runtime + string concatenation `++` (backend). The runtime
  (`runtime.go`, `ensureRCRuntime`) is now emitted as **real function definitions into the
  module** — `lyra_rc_alloc` (malloc + rc=1), `lyra_rc_retain` (rc+=1, `PinnedRC` no-op),
  `lyra_rc_release` (rc-=1, `drop_fn(payload)`+`free` at 0, pinned no-op) — built on libc
  `malloc`/`free` declared like `memcmp`, so there's no runtime object to link and `clang
  out.ll` stays self-contained (the old `declareRuntime` dead externs are gone). Emitted lazily
  (a non-allocating program carries none). First consumer: `lowerStringConcat` — a concatenated
  string heap-allocates a box (`rcAllocPayload` → `lyra_rc_alloc(header+total)`), `memcpy`s both
  operands into the payload, and returns a fat pointer `{ box+8, la+lb }`; empty operands
  (`memcpy` n=0) and chains (`a ++ b ++ c`, left-associated fresh box per step) just work.
  Interpolation stays deferred (now on value→string formatting, not the allocator). Tests:
  `runtime_test.go` (white-box: rc transitions, pinned no-op, `drop_fn` path, clean
  free-at-zero), `llvm_string_test.go` (`TestExec_StringConcat` —
  literals/empties/chain/param-strings/match-a-heap-string, `TestEmit_StringConcatIR`,
  `TestEmit_NoRuntimeWhenUnused`), plus an end-to-end `lyrac build` + `clang` run. (Freeing
  landed same day — see the ownership entry.)
- **[07/17]** Ownership model — heap strings are freed (`pkg/analyzer/ownership` + backend
  retain/release). **Representation:** every string value is a box, so retain/release are total
  — a **literal** interns as a *pinned* static box `{ i64 PinnedRC, [N x i8] }` (`data` at
  `box+8`, no allocation, retain/release no-op on it), a `++` value is a heap box. **Pass**
  (`pkg/analyzer/ownership`): ARC over managed (string) values — a binding/`own`-param holds one
  owning ref released at scope exit; the pass computes `Retain` (borrowed value → owning slot: a
  copy, an owned `return`, an `own` arg) and `ReleaseTemp` (owned temporary → borrowing slot:
  `==`/match/`++` operand, discarded stmt, borrowed arg), mirroring the typechecker's
  `paramOwnsArgument`/`isOwnedReturn`; an `if`/`match` is one merged owned value (branches
  coerced to +1), released once at the phi. Safety-biased: unresolvable callees + aggregate
  elements *transfer* (leak-safe), never release. **Backend** (`ownership_lower.go` +
  `lowerExpr`/`emitReturn`): a managed-frame stack releases bindings at scope exit (a `return`
  releases all live frames before it seals); retains apply at production; each temp is released
  in the block it was produced in (so an `&&`/`if`-branch temp is freed there, dominating its
  uses, not at a non-dominating merge). Verified memory-safe under **AddressSanitizer**
  (`TestExec_OwnershipASan`). **Still leaking conservatively (safe):** strings inside
  aggregates, and bindings on break/continue paths. Tests:
  `pkg/analyzer/ownership/ownership_test.go` (pass decisions), `llvm_ownership_test.go`
  (behavioral + ASan + IR retain/release balance), `llvm_string_test.go`
  (`TestEmit_StringLiteralIsPinnedBox`).
