# lyra (Go) — Project Context

The Lyra compiler: parser, AST, type system, collector, typechecker, standalone checker
passes, LLVM backend, LSP server and CLI. Go module `github.com/Lyra-Language/lyra`.

Never create a git commit or push unless the user explicitly asks in their current message.

**This file is a map and records rules, not history.**

- What the language *does*: [`LANGUAGE.md`](LANGUAGE.md).
- Open work: `todo.md`. Finished work and the reasoning behind it: `COMPLETED.md` (an item
  citing "the Completed entry" means that file).
- Package depth: the `README.md` beside each package (see [Package map](#package-map)).
- Read [Rules and hazards](#rules-and-hazards) first.

The grammar is a local dependency (`replace` → `../tree-sitter-lyra`). After
`npx tree-sitter generate` there, **always `go clean -cache` before `go test`**, or Go serves
the stale compiled C parser.

## Data Flow

```
source text
  → pkg/parser                        tree-sitter CST (*sitter.Tree)
  → pkg/analyzer/collector            CST → *ast.Program + *symbols.SymbolTable
  → pkg/analyzer/checker              standalone AST passes (e.g. use-before-declaration)
  → pkg/analyzer/typechecker          AST → *typetable.TypeTable + []TypeError
```

The full pipeline is `pkg/driver` (below). The LSP runs it on every change.

## Rules and hazards

Each of these produces something that looks like it works. Other docs cite them by number.

1. **After changing `grammar.js`: regenerate, `go clean -cache`, then test.** Go does not hash
   `#include`d sources. Push the grammar repo before `lyra` code depending on it — CI
   regenerates from the remote.

2. **Nil-check an optional field lookup before calling anything on it.**
   `ChildByFieldName`/`cst.Field` return a real nil for an absent optional field, and
   `ChildCount`/`Child`/`Kind` on it **hang inside the CGO binding** rather than panic. See
   `pkg/analyzer/collector/README.md`.

3. **Never return a nil expression node into the AST.** On an unrecoverable value error, emit
   a diagnostic *and* return a placeholder (it also keeps one mistake to one diagnostic). A
   nil `ast.Expression` is a typed nil that slips past `== nil`. A block skips nil statements.
   - `CollectExpression`/`CollectStatement`/`CollectPattern` pass results through
     `ast.TrueNil`, and list sites (call arguments, array/tuple elements, comprehension
     guards, struct field values) drop a nil (`appendCollected`) — but a placeholder is still
     the right answer: a dropped element costs a second, misleading arity diagnostic.
   - **A value error inside an expression is live in every position an expression is**, not
     just the `let` it was tested in. `TestValueErrorInAListReportsOnlyItself` pins "one
     error, no crash" for list positions.
   - **A dead error path is live after the next grammar change.** tree-sitter recovers with an
     `ERROR` node, not a partial named node, so missing-required-field guards rarely fire;
     what does reach them is a structurally valid token whose *content* the collector
     validates (e.g. a broadened escape token). `pkg/driver/malformed_input_test.go` is the
     standing check.

4. **Resolve top-level names only through the `Lookup*` accessors**, never by indexing
   `SymbolTable.Types`/`.Functions`/`.Traits`. Same reason `recordedType`,
   `types.StripNewtype`, `slotIsOwning`, `types.IsCopiedScalar`, `types.CollectTypeVars` exist:
   one predicate so passes cannot drift.
   - All three maps are keyed `<module>::<name>` (`DeclKey`; entry module is `::name`).
     `declKey(name, loc)` resolves: the asking module's own, an import (aliases followed), the
     prelude's export, then any program-wide export. A *written* unimported type name is
     refused via `ResolvedReachably` + TypeRefs. Unresolvable names pass through unchanged
     (the backend's `Maybe$i64` relies on it).
   - Prefer `LookupTypeFrom`/`LookupTraitFrom`/`LookupFunctionFrom(name, loc)`. Bare
     `LookupType(name)` answers the program-wide meaning and is wrong inside a module that
     declares its own.
   - A key is not a source name — user-facing text reads `decl.Name`.
   - A `pub` check asks about the declaration a reference *resolved to* (`declVisibility`);
     inside the table use `BindingIn(module, name)`, not `BindingOf(name)`.
     `DeclaringModule` is last-writer-wins.
   - Traits too: `checkImplCoherence` keys on the resolved `*ast.TraitDeclStmt`. **A pass
     with no symbol table must have one threaded in**, never build a local by-name index (the
     purity pass did, and impls inherited another module's trait's effect bound).
   - Layout resolves a struct/union's field types from **its declaration's** location:
     `declarationSite` in the typechecker (`hasCLayout`), `resolvingFrom`/`declLocOf` in the
     backend (`resolveForLayout`), not the site being lowered.
   - Backend `namespaceCallee` must not test membership with `DeclaringModule` and read
     `l.funcs[name]`.
   - A comment asserting "a top-level name is program-wide unique" is a bug waiting.

5. **The backend errors loudly rather than emitting wrong code** — including repeating checks
   the front end made. "Loudly" also means no llir panic and no clang error: `ir.NewPhi`
   accepts mismatched incomings (checked in `joinPhi`, for `if` and `match` merges) and
   `ir.NewStore` panics on mismatch (checked in `aggregateSpill`). Any site building an
   instruction from another pass's value owes a check naming the construct and location.

6. **ASan works only because the test harness adds `sanitize_address`** to every `define`.
   Without it the ASan tests pass vacuously. See `pkg/backend/llvm/README.md`.

7. **`build/std` is a symlink, not a copy**, and the std root is the directory *containing*
   `std/`. Staleness here presents as a behaviour difference.

8. **A `switch` over AST node kinds or composite types needs a case for every one that can
   hold a child — a missing case is silent and its symptom remote.** When adding a node or
   type kind, grep the switches over it; when fixing one, check its siblings in the same file.
   - **Copies drift even at two, and can agree and be wrong together** (all purity ladders
     charged builtin methods nothing, wrong for allocating `slice`). Copies that disagree can
     be a soundness hole: purity now has **one** walk, `bodyEffects` over a `callable`, with
     enforcement re-running it through `callable.reportPure`.
   - **Two switches can disagree about a case neither names** (`nominalHead` lacked
     `*ConstrainedType` while `types.HeadName` gives a newtype a head).
   - **Adding an expression kind: grep for the kind it is a variant of.** List every file
     mentioning `ArrayLiteralExpr` and check each for `ArrayRepeatExpr` (eight past misses).
   - **A binder is its own family** — parameters, `match` arms, loop variables,
     destructurings, comprehension generators. A binder a walk doesn't know reads as a capture.
   - **A bare `string` field is invisible to every walk** (`VarReassignmentStmt.Name` hid a
     write-only capture → backend nil deref).
   - **Declaration kinds**: `pkg/ast/exhaustive_test.go` parses switches and checks registered
     mirrors of `walkExprChildren`/`walkStmtChildren` and every `declarationConsumers` switch
     against `declarationKinds` (a statement with a `Doc` field is a declaration). An
     exclusion is a claim written beside its reason. Registering a new *consumer* is still
     manual. Better than registering a mirror: delete it (`ast.RewriteStmt`, `ast.WalkStmt`).
   - **Type kinds have no checklist.** The walks that must reach *every* composite are
     resolution (`resolveTypeWith`), layout (`SizeAndAlign`, `resolveForLayout`),
     substitution (`Substitute`, `CollectTypeVars`, `mentionsGenericParam`, ownership's
     `substituteTypeVars`), retain/drop (`emitOwnedValue`), `mentionsTypeVar`, and
     `resolveInstantiation` (`backend/llvm/generic_types.go`). Container-shaped switches
     (`elementType`, `isByteArray`, `iterableElementType`) rightly lack pointer cases.
     **Probe behaviours** (put the type in a struct, array, closure, comparison, match,
     generic) rather than reading switches — most switch-list hits are false leads.
   - **Every rung reading a value's type strips newtypes itself, resolving between strips**:
     `stripNewtypeResolving`. A binding's type may arrive as a bare declared name, and a
     generic newtype's base must be resolved to see the next wrapper. Annotated array
     bindings keep the wrapper recorded; indexing and assignability both depend on it.
   - **Sibling constructs are a pair**: `if` and `match` both push their join down via
     `propagateExpectedType`, tested together (`TestExec_IfBranchesJoinTheirUntypedWidths`
     and its match twin).
   - **A pass over the merged program sees every module's top level at once**; name
     questions go through the `SymbolTable` (`CheckShadowing` uses
     `SymbolTable.ModuleExports`: own module, admitted imports, prelude exports).
   - **Paired walks are fixed in one change, or merged**: retain/drop are one
     `emitOwnedValue(…, retainWalk|dropWalk)` (`owned_walk.go`). Equality is a different walk.
   - **A copy that admits it is a copy is still a copy** — call `types.Substitute`.
   - **A memory-safety test can pass because the code does nothing** (a weak-cycle test
     passed by leaking).
   - **A construct two passes misread is the construct's fault.** When a pass mirrors
     another, check the original (`let … else` in `CheckUseAfterMove` and `CheckMustRelease`).

   Single answers — use, don't re-derive:

   | question | the one answer |
   |---|---|
   | type variables a type mentions | `types.CollectTypeVars` |
   | resolve a type (reporting or quiet) | `resolveTypeWith`, behind `resolveType`/`resolveTypeIfKnown` |
   | is this tail a value or a statement? | `checkExprForEffect` |
   | did this expression produce a value? | `isVoidResult` (nil **and** void-typed `ir.Call`) |
   | does this value transitively own a reference? | `ownership.OwnsManaged` |
   | is that sharing observable? | `ownership.SharesMutableState` |
   | substitute type variables | `types.Substitute` |
   | a node's children | `ast.WalkStmt`/`ast.WalkExpr` (`…Children` skips the node) |
   | statement expressions ownership must see | `ownership.analyzer.stmt` — **all** statement kinds |
   | rewrite expressions in place | `ast.RewriteStmt`/`ast.RewriteExpr` |
   | see through a newtype | `stripNewtypeResolving` (typechecker) |
   | expression at a position | `findExprAtPos` (`cmd/lyra-lsp/hover.go`) |
   | a pattern binding's type (no use in hand) | `TypeTable.Binding(loc)`, via `recordBindingType` |
   | bind a generic type's arguments | `ast.BindGenericParams` |
   | what a value holds inline | `ownership.eachComponent` |
   | retain or release what a value owns | `emitOwnedValue` (`backend/llvm/owned_walk.go`) |
   | push a context type onto a value (width + `shared` flavor) | `propagateExpectedType` |
   | did this operand diverge? | `diverged(v, block)` (`backend/llvm/trap.go`) |
   | is this CST node a comment? | `cst.IsComment` |

9. **A name does not identify a declaration, or even one function.** Keys are
   module-qualified (rule 4) and a receiver-overloaded name maps to several declarations. To
   answer "what does this call call?" read `TypeTable.Callee(call)` first, then
   `LookupFunctionFrom`. Backend: `l.funcs` keys overloads by declaration with the receiver
   head in the symbol; `l.globals` slots are keyed by funcKey, named per module, resolved by
   `slotFor`.
   - **A by-name set consulted where the enclosing scope is the question fails silently.**
     `l.locals` must not decide a desugared UFCS callee (`data.data()` on a parameter named
     `data`) — ask `Callee` first (`calleeIsDeclared`). `captures.globalNames` subtracts a
     global only where `analyzer.outer` (the enclosing binders) doesn't shadow it.
   - A trigger may need a *sibling file of the same module*. When reductions keep failing,
     stop reducing and instrument.
   - **Allocation flavor rides the expected type**: `propagateExpectedType` is the one walk
     pushing width and `shared` flavor from every context (binding, return, argument,
     reassignment, field, payload, element). A new context is one call to it.

10. **Positional argument indexing is one AST shape from wrong.** Purity reads
    `call.Arguments[idx]` against `callableParams`; a receiver outside `Arguments` shifts every
    index and silently disables effect bounds. Trait methods use `methodArgumentAt`; UFCS
    desugars the receiver into `Arguments`. Prefer the desugar.

11. **A desugaring can rebind a parameter.** `desugarClauses` turns multi-clause functions
    into `match (p0, p1)`, and a clause may rename a parameter; `addMatchAliases` maps arm
    bindings back to positions. Fix at the produced construct; distrust name-keyed analysis
    downstream of binding rewrites.

12. **A box's `drop_fn` may free the box it runs on** (via a cycle dropping the last weak
    ref). Strong owners hold **one implicit weak reference**, taken in `lyra_rc_alloc` and
    dropped after the glue returns. Do not "simplify" to a `weak == 0` test — ASan-confirmed
    double free.

13. **A bound-dispatched call is resolved in two places; ask both.** The typechecker resolves
    abstractly and publishes one candidate per implementing type. Use `candidateKey` for the
    lookup (the table uses the typechecker's spelling, not module-prefixed
    `recordedType(...).String()`) and `methodParams` for borrow modes (the resolution table
    has no entry). A method reached *only* through a bound: `Specializations()` includes
    bound candidates, and `methodSignature` falls back to the trait's signature.
    - **Wrong-scope resolution disappears in a one-module program** — the backend suite
      prepends `module main` for that reason. **A pass checking a body outside
      `checkInModule` must install the module scope itself**; test it where the prelude is
      real.

14. **A diagnostic with no Location appears on every file** (`diagnosticsFor` keeps
    location-less diagnostics as program-level). Set a new node's span; check the node you
    report against has one.

15. **A diverging operand lowers to a nil, and every consumer must stop.** `panic` is
    `never` and fits anywhere; test with `diverged(v, block)`. llir accepts the nil and dies at
    `m.String()`. `coerceAggregateElem` rejects a nil with rule 5's loud error.

16. **A comment is a named node.** Comments are `extras` and can sit inside any list, so
    `IsNamed()` does not mean "element" — use `cst.IsComment` in every list collector.

17. **A builtin returning an owned managed value** must be named in
    `calleeIsOwningBuiltin` (the unresolved default treats a result as borrowed → leak) and
    lower **branchlessly** (a merge block defeats `flushStmtTemps`). `read_line` is the model;
    `<=>` is branchless for the same reason.

18. **A value-range fact is only as good as the pass's knowledge of every write — and the
    backend drops traps on it** (`res.RangeSafety` in `arrays.go`, `arithmetic.go`). A stale
    interval is a missing trap in safe code.
    - A name whose address is taken with `&mut` is never tracked in that function
      (`tracked`), flow-insensitively.
    - `resolveConstantInt` folds only a binding that cannot change (`!CanMutateInterior()`).
    - **Adding a way to write a binding**: the range pass must model it or untrack the name,
      and constant folding must not treat the binding as fixed.

## Feature implementation notes

Semantics are in `LANGUAGE.md`; these are the compiler-side traps.

**Documentation comments**
- `ast.Doc` (`pkg/ast/doc.go`): body, `Summary`, classified `Sections`. `NewDoc` returns nil
  for an empty comment.
- Docs attach to declarations: field docs are `TypeDeclStmt.MemberDocs` (`MemberDoc(name)`),
  never `types.StructField`. A new documentable declaration gets a Doc field on its AST node.
- Attachment is `ctx.DocFor(node)` (`collector_ctx/docs.go`); top-level stamping is
  `walkProgram`'s `attachDoc`. `DocFor` absorbs two CST quirks: an extra before a node's first
  token attaches to the enclosing node, and a separator (`|`) may sit between doc and node
  (`IncludingTheFirst` tests in `collector/tests/doc_comment_test.go`).
- `lyra-W017` is `ctx.ReportStrayDocs`, a post-pass; claims keyed by start byte, reset per
  file (`ctx.ResetDocs`).
- `collector/tests/prelude_docs_test.go` is the coverage guard (every prelude declaration
  documented, `# Panics` exactly on trapping functions). `lyrac check` exits 0 on W017.

**Operator dispatch**
- Comparisons are the compiler's: `Eq`/`Ord` found by `@builtin(Eq)`/`@builtin(Ord)`, with
  candidate impls filtered by resolved *declaration*. `(_==_)` is `lyra-E039`.
- Arithmetic/bitwise dispatch keys on the **method name**; inert operators warn `lyra-W015`.
- "Primitive" is the receiver **unstripped** (stripping first makes scalar newtypes
  operator-dead). Overflow builtins on a newtype are `lyra-E043`.
- Resolution is `resolveTraitMethodNamed`, shared with `.method()` calls. Purity charges
  operators via `operatorImplEffect`.

**Supertraits**
- Obligation `lyra-E040` in `checkTraitImpl`. Use: `closeOverSupertraits`
  (`typechecker_trait_dispatch.go`), a transitive closure at the **two writers** of
  `tc.genericBounds` — `pushGenericBounds` and `checkTraitImpl`. A new reader is correct for
  free; a new writer must close too.
- Cycle-safe by a visited set (a DAG assumption hangs the editor). The backend needs nothing.
- A bodiless trait: `declarations/trait_decl.go` reads the body with `cst.Field` + nil check,
  not `MustField` (which would drop the trait → `unknown trait` at every impl).
- `std/prelude/math.lyra` ships `Add`/`Sub`/`Mul`/`Div`/`Arithmetic` impls for all numeric
  widths so bounds are satisfiable.

**Trait default methods**
- `Self` is `types.GenericType{"Self"}` bounded by the trait; checked once
  (`checkTraitDefaultMethods`), monomorphized per implementer via `SetBoundCandidates` and
  `Resolution.Bindings`. An applied `Self<a>` stays a `types.SelfType` (args are types, so
  `Self<i64>` is representable) and dispatches through Self's bound like the bare variable;
  `TypesEqual`/`isAssignable` compare its arguments. For a **holed** impl target
  (`Result<_, e>`) `Bindings["Self"]` is the pattern at the receiver's bindings
  (`Result<_, string>`), not the receiver: only the holes say which position `Self<a>`'s
  argument fills (`types.ApplySelf`).
- `publishCandidatesAt` substitutes each bound call's recorded receiver under the
  specialization's bindings before matching: inside a default a receiver may be `Self<bool>`
  after a `map`, whose candidate key is `Box<bool>`, not the enclosing `Box<i64>`.
- **`ast.TraitMethod.DefaultImpl()` is the one instance** — many caches key on its pointer.
- Dispatch tries the impl's clauses first, then the default.
- `publishDefaultBodyCandidates` publishes the body's inner bound calls at the concrete
  receiver.
- An unsatisfied bound in a default body advises a supertrait (`reportUnboundTypeParameter`);
  its test applies the advice and compiles.
- `checkOneDefaultMethod` → `moduleScopeOf` installs the module scope (rule 13). A generic
  call there records `callee<t=Self>`; `closeInstantiations` seeds from
  `MethodTable.Specializations()`.
- Deep-copying defaults into impls was rejected: no cloner exists, and a missed case is a
  silently shared subtree.

**`?`, `From` and receiver-less calls** (`typechecker_try.go`, `typechecker_functions.go`)
- `inferTryExpr` pushes `Result<want, E>`/`Maybe<want>` as the operand's context
  (`wrapInEnclosingReturnKind`). A mismatched error looks up `impl From<from> for to`
  (`fromConversion`, matched on the trait argument too) and records it on the `?` node
  (`MethodTable.SetTryConversion`); the backend calls it in `lowerTryPropagate`, the purity
  pass charges it in its `TryExpr` arm, and `Specializations()` includes it so the ownership
  table exists. `checkImplCoherence` keys on trait args, so `From<A>`/`From<B>` coexist.
- `inferTraitMethodPathCall` routes a method whose first parameter is not `Self` to
  `inferReturnDirectedTraitCall`: `Self` is solved from `currentExpectedType()` against the
  declared return, else from an argument. A variable `Self` is a bound call with the solved
  variable recorded (`SetBoundSelf`), which `lowerTraitPathCall` keys the candidate by.

**Raw pointers** (`typechecker_pointers.go`, `backend/llvm/pointers.go`)
- `lyra-E011` runs in `driver.go`; its unsafe-call half and `requireUnsafeBuiltin`
  (`p.offset(n)`, needs the receiver's type) are in the typechecker.
- `&mut` on a captured binding is `lyra-E024` via `checker/captured_assignment.go`.
- Mutability is two checks: `requireMutableRoot` (reuses `checkLValueAssignment`'s rule) and
  the pointer's `IsMut` in `checkDerefWrite`.
- **`UnsafeBlockExpr.Body` is a pointer** — by value, the scope table's key would be a copy.
- `^mut T` → `^T` is assignable (`isAssignable`); `TypesEqual` still distinguishes them.
- `offset` lowers to `getelementptr` on the pointee type.
- **`nullptr`**: `NullPtrExpr.GetType` is the `untyped_nullptr` placeholder (a
  `PrimitiveType` name, not a new type kind); the context pins it in `propagateExpected`, and
  the backend reads the TypeTable — clang 15's typed pointers make `i8* null` ≠ `i64* null`.
  `lyra-E069` is a sweep at the end of `checkRange` (`checkUnpinnedNullPtrs`). `lyra-E070` is
  in the collector.
- **Pointer `==` keys on the Lyra type** (`isRawPointerExpr`) — a `shared` aggregate is also
  an LLVM pointer and compares by value (`TestExec_SharedAggregateEquality`, `TestExec_NullPtr`).

**Lazy sequences** (`typechecker_yield.go`, `seqElementType`, `backend/llvm/seq_lower.go`,
`seq_coro.go`)
- `Seq<t>` is `typechecker.SeqTypeName`. A consumed producer is inlined at its consumer.
- `mentionsSeq` is the skip: such functions are never declared/specialized;
  `lowerFunctionCallExpr` inlines first. A `Seq` reaching `lowerType` is `errSeqNotLowered`.
- Every inlined body runs in a `seqEnv`; anything a body's lowering depends on beyond the
  shared frame/temp stacks belongs there (`pendingBase`).
- `emitReturn` honours `inlineRet`; nothing else may emit `ret`.
- A consumer's `loopCtx` carries the consumer's depths.
- A sequence *value* is a coroutine: a `shared` box with `lyra_seq_drop` glue and a
  `{ i1 has, t value }` promise via `llvm.coro.promise`. Bodies are emitted after everything
  else (`defineSeqCoroutines`). `yieldValueTo` picks inline vs suspend. Managed parameters
  are retained and framed. `CheckCoroutineSupport` gates clang < 15 — any new consumer of
  emitted IR must ask it.
- **Pass a drop function to the release shim as `i8*`**, never the `*ir.Func` (Linux
  typed-pointer error).

**`let … else` (`lyra-E074`)** — `checker.CheckLetElseDiverges`, after typechecking (reads
`never`). The backend check in `lowerElseDestructuring` stays (rule 5). `lowerIf` seals an
unreached merge with `unreachable`, as `matchMerge.value` does; if front and back end
disagree, the front end must stay the more permissive. `CheckUseAfterMove.letElse` and
`CheckMustRelease.letElse` rely on it (payload in the enclosing scope, else unreachable).

**Leading `-`/`(`/`[` (`lyra-W023`, `lyra-W024`)** — `checker.CheckLeadingMinusContinuation`.
Fires only when the previous statement is a discarded pure value (`discardedValue`, a short
list defaulting to silence). The negation is found on the **left spine**. A parenthesized
single expression is erased from the AST, so `(` is detected by **column** (statement span
starts one before its expression).

**`@must_release` (`lyra-W022`)** — `checker.CheckMustRelease`, `must_release_test.go`.
- The attribute argument is an `identifier`, resolved from the **type declaration's**
  location; discharge is matched against the resolved declaration, name as fallback. An
  unrecognized call shape never manufactures a warning.
- Branch join is **intersection** (use-after-move's is union).
- A call to a `@borrowed` function (`LambdaExpr.ReturnsBorrowed`, set by the collector's
  `collectDeclarationAttributes`) is tracked with `resource.borrowedFrom` set: `report` skips
  it, and `discharge` or a direct argument to its release function reports `lyra-W025`. It is
  marked in `resourceOf`, so every acquisition path sees it.
- Unwrapping: `match`/`if let`/destructuring share `arm` over an `unwrapper`; `let … else` is
  handled like a `VarDeclStmt`. `beginUnwrap` also treats an obligation-producing scrutinee
  expression. A payload is seeded only when the pattern binds exactly one name.
- Views, not handovers: a named binding's unwrapped payload; field/element/deref reads
  (`isStoredPlaceRead`, looking through a one-expression `unsafe` block); `MemberExpr`/
  `IndexExpr`/`TupleIndexExpr` over a name. A temporary scrutinee's payload *is* the handle.
- **Escape is the default** — no list of construction kinds (rule 8).
- An under-reporting pass is silent when broken: keep must-fire tests (`assertLeaks`) and check
  against real programs using `bindings/raylib`.

**Foreign functions** — see [`pkg/backend/FFI.md`](pkg/backend/FFI.md).

**Sweeping for surfaces nothing reads** — to find AST fields no pass consumes, enumerate
exported `pkg/ast` fields and grep for readers **outside `pkg/ast`, `pkg/printer` (reads all
by reflection) and tests**. The AST is rarely where phantoms live; check effect tables
(`builtinEffects`), glue switches and grammar rules with no collector consumer.

**Module exports are per file** — a name overloaded only in a later file of a multi-file
module was already exported bare; `exportToGlobal` lets a set supersede a global binding
that is one of its members. The prelude branch discards duplicate-definition errors, so test
such changes on a user module.

## Package map

| Package | What it is | Depth |
|---|---|---|
| `pkg/parser` | CGO wrapper around tree-sitter; `Parse(source)` | — |
| `pkg/cst` | CST accessors | below |
| `pkg/ast` | AST nodes | below |
| `pkg/ast/symbols` | `SymbolTable` + `Scope` tree; per-module resolution | [README](pkg/ast/symbols/README.md) |
| `pkg/types` | `Type` and implementations; allocation flavors | [README](pkg/types/README.md) |
| `pkg/typetable` | expression → type; method/instantiation tables | below |
| `pkg/analyzer/collector` | CST → `*ast.Program` + `*SymbolTable` | [README](pkg/analyzer/collector/README.md) |
| `pkg/analyzer/checker` | Standalone passes — purity, effects, use-after-move, value ranges | [README](pkg/analyzer/checker/README.md) |
| `pkg/analyzer/typechecker` | Inference, checking, generics, trait dispatch | [README](pkg/analyzer/typechecker/README.md) |
| `pkg/analyzer/captures` | Lambda free variables | [README](pkg/analyzer/captures/README.md) |
| `pkg/analyzer/ownership` | Retain/release placement; Perceus | [README](pkg/analyzer/ownership/README.md) |
| `pkg/modules` | Import resolution, namespacing, implicit prelude | [README](pkg/modules/README.md) |
| `pkg/driver` | The reusable front-end pipeline | below |
| `pkg/abi` | C calling conventions per target | below |
| `pkg/backend` | `Backend` interface; FFI notes | [FFI.md](pkg/backend/FFI.md) |
| `pkg/backend/llvm` | LLVM IR backend | [README](pkg/backend/llvm/README.md) |
| `pkg/docgen` | AST → per-module docs; Markdown renderer | below |
| `pkg/printer` | Reflection AST printer for golden tests | below |
| `cmd/lyra-lsp` | LSP server | [README](cmd/lyra-lsp/README.md) |
| `cmd/lyrac` | CLI: `check`/`build`/`run`/`doc` | [README](cmd/lyrac/README.md) |
| `bindings/` | Per-library FFI binding modules (SDL3, raylib, jpeg) | [README](bindings/README.md) |

**`pkg/cst`** — `cst.Field(node, "name")` is *the* way to read a grammar field (same nil
semantics as `ChildByFieldName`, rule 2, but caches the field id — a large per-keystroke win).
Benchmark with `pkg/driver`'s `BenchmarkAnalyze_*`.

**`pkg/ast`** — `AstNode` (`GetLocation()`), `Named`, and `Statement`/`Expression`/`Pattern`.
Nodes embed `AstBase` with a 1-based `Location`. Files by kind (`expr_math.go`,
`stmt_for_loop.go`, `decl_trait.go`). The pattern rules every pass shares live here:
`WalkPattern`/`EachPatternBinding` (walk_pattern.go) and `MatchPositions` (which element of a
tuple or payload pattern matches which position, `...rest` included).

**`pkg/typetable`**
- `TypeTable`: `Set`/`Get`. `SetCallee`/`Callee` (`calleetable.go`) records only
  receiver-overloaded calls — read it first, then fall back.
- `MethodTable`: call → `*ast.TraitMethodImpl`; nil-receiver-safe `Get`. `SetBound`/`GetBound`
  record abstract bound dispatch (`BoundMethodRef`); purity joins over all impls.
- `SetBuiltinMethod(call, allocates)`: read by purity's body walk.
- `SetBoundCandidates`: one resolution per implementing type; the backend picks by
  substituted receiver. **Impl matching stays in the typechecker.** Unsatisfied bound is
  `lyra-E036` at the instantiation.
- `Resolution.SpecKey()` names a specialization (symbol, method cache, ownership table).

**`pkg/driver`** — `driver.Analyze(source)` / `AnalyzeUnits(units)`: parse → collect →
`checker.Check*` → `typechecker.Check` → `captures.Analyze` → `checker.CheckPurity` →
`ownership.Analyze`, returning `Result{Program, SymbolTable, ScopeTable, TypeTable,
MethodTable, Ownership, Captures, RangeSafety, Diagnostics}` with all errors as
`[]diagnostic.Diagnostic` (CST positions converted to 1-based). `HasErrors()`/`Errors()`.
- A check whose answer depends on the *settled* type (after propagation) reads the TypeTable
  post-typecheck and lives here (e.g. `CheckArrayRepeatAliasing`, `lyra-W019`).
- **The generic instantiation set is closed before per-specialization ownership runs**
  (`instantiations.go`); otherwise composed specializations get the generic ownership table
  and a `t = string` body emits no retains/releases.
- **An instantiation carries its request site**: lowering enters the generic's module, but
  type arguments resolve from the site — `lookupNamedType` falls back to the site's key after
  the current module's. Composed specializations take the outer site.
- `driver.ResolveEntryPoint(res)` (`entrypoint.go`): top-level `let main`, zero parameters,
  returning `u8` or void. Build-time only, not part of `Analyze`.

**`pkg/backend`** — `backend.Backend{Name(); Emit(res, entry)}`, called only on error-free
analysis. `backend/llvm/tui.go` is the **only** `runtime.GOOS` consumer (`TIOCGWINSZ`), sound
because `lyrac` compiles for its host.

**`pkg/abi`** — `Classify(target, aggregate, isReturn)` for AAPCS64 and SysV AMD64.
`abi_diff_test.go` checks against clang (19 shapes × 3 targets). AAPCS64 register-passes an
HFA of any size and has asymmetric param/return widths; SysV can change arity, so
classification reaches call lowering. `DetectTarget(cc)` / `HostTarget()`. **Windows is
`Unknown`** — refused at the crossing, never guessed.

**`pkg/docgen`** — `Collect(res, opts) []Module` + `RenderMarkdown(m)`; the model knows nothing
of Markdown.
- Pages group by receiver (`pageSections`, `Module.Partition`): types/traits, impls, free
  functions, `## Methods on \`T\``, values. Group on `types.HeadName`, display `typeName`
  (not the same string); borrow modifiers are not part of a group.
- **Signatures are re-rendered from the AST in source syntax** — never sliced from source or
  via `Type.GetName()`. Member names use `ast.MethodName.Key()`. Guard:
  `TestSignature_RoundTripsThroughTheParser` (covers `Decl.Signature`; trait `Members` need
  their own).
- Doc bodies are heading-shifted before embedding; `ast.ShiftHeadings` and
  `ast.TagBareFences` share `ast.walkDocLines`, the single fence tracker.

**`pkg/printer`** — `printer.PrintAST(program)` (omits zero/nil/empty);
`printer.NewPrinter().Print(node)` dumps a CST node.

## Building

```bash
./build.sh          # build/{lyrac,lyra-lsp}, with std -> ../std, bindings -> ../bindings
```

- `lyrac` finds the standard library beside its executable, or at `LYRA_STD`.
- **The root is the directory containing `std/`** (rule 7); returning `std/` itself silently
  finds no prelude.
- **`build/std` is a symlink** — a copy drifts from edited prelude sources.
- `stdRoot` resolves symlinks before taking the executable's directory (`os.Executable`
  differs between Linux and macOS).
- `build/` is gitignored as a directory. Point VS Code's `lyra.languageServerPath` at
  `build/lyra-lsp`.
- **`build.sh` also builds `lyrafmt`** when a C compiler, `libtree-sitter` and the sibling
  `tree-sitter-lyra` checkout are present, and skips it with a note otherwise. It lands
  beside `lyra-lsp`, which is where the server looks for it (`cmd/lyra-lsp/formatting.go`)
  — so `Format Document` works in both editors without either extension knowing about it.
- `std/prelude/` is one module across several files (constraints in
  `std/prelude/README.md`). Also: `std/collections/`, `std/json.lyra`, `std/math/`,
  `std/tui/`, `std/ffi.lyra`, `std/io.lyra`.

## Testing

```bash
go test ./...
go test -run TestFunctionName ./pkg/...
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...   # regenerate goldens
```

**Collector golden tests** (`pkg/analyzer/collector/tests/`): `runGoldenTest(t, source,
"name")` (no extension) against `testdata/*.golden`; a new golden is created and fails on
first run. `parseAndCollect(t, source)` returns `program` and `table` directly.

**Typechecker tests** (`pkg/analyzer/typechecker/tests/`):
`res := parseCollectAndCheck(t, source, false)`, then `assertNoErrors(t, res)` or
`assertErrorsAre(t, res, "msg1", …)`; `res` has `program`, `symTable`, `typeTable`, `errors`.
This harness has **no prelude** (see rule 13): a test that needs it — including one for a
diagnostic whose fix names a standard-library function — belongs where the prelude is real
(`cmd/lyrac` tests, or the backend's `…WithPrelude` helpers).

**Backend tests** (`pkg/backend/llvm/`) compile and run IR; see its README for
`sanitize_address` (rule 6) and the binary cache.
- **Run ASan binaries through `buildAndRunASan`/`buildAndRunASanWithPrelude`, never a bare
  `exec.Command`.** `asanOptions` enables leak detection where LeakSanitizer exists (Linux: CI
  and `./asan.sh`) and disables it on macOS, so a leak fails on Linux only.
  `buildAndRunLSanWithPrelude` is for leak-specific tests and skips off Linux.
- Run `./asan.sh` (workspace root) before pushing memory-model work: clang-15's typed
  pointers reject mismatches Apple clang cannot see.

**A test must not depend on the shell's `LYRA_STD` or `LYRA_NO_PRELUDE`.** Both change what
resolution finds, so `cmd/lyra-lsp` and `cmd/lyrac` clear them in `TestMain` and each test that
needs the standard library sets `t.Setenv("LYRA_STD", …)` (or `LYRA_NO_PRELUDE`) itself. A
package that resolves modules through `modules.DefaultRoots`/`DefaultOptions` and lacks such a
`TestMain` passes in CI and fails wherever a developer exported one — which once read as a
flaky race for a day.

**A test file's name can silently exclude it.** A final `_arm`, `_ios`, `_js`, `_plan9`,
`_android`, `_wasm`, `_mips`, `_s390x`, `_windows` (any GOOS/GOARCH) segment is a build
constraint — `match_unreachable_arm_test.go` never runs on arm64 and `go test` prints `ok`.
`go list -f '{{.IgnoredGoFiles}}' ./...` must be empty on this repo.

## Current Development Focus

**`lyrafmt` — the formatter written in Lyra — as the self-hosting probe** (`todo.md`,
Modules and tooling). The round-trip baseline, indentation and spacing rules are in, the
repo is formatted by it, and both editors reach it through `lyra-lsp`. Its value is that it
keeps finding **compiler** bugs rather than formatter ones: a `let … else` use-after-free, a
destructuring release that never happened, and an interpolation that swallows source to the
next `}` — each found by the first Lyra program large enough to hit them.

The typechecker is **maintenance rather than build-out**. Match exhaustiveness is built
(Maranget pattern matrices for tuple and `data`, plus arrays, runes and structs —
`pkg/analyzer/typechecker/README.md`), and so is the FP/imperative purity work
(`pkg/analyzer/checker/README.md`), which has one open question left, impurity of imported
functions. What turns up now is a *position that accepts a value it should refuse* — a
`data` payload that truncated a literal in silence, a bound on a generic type's parameter
that parses and is never read. Reproduce a README "Open" marker before believing it: of the
three standing on 09/16, one was stale, one pointed at a `todo.md` item that never existed,
and the real bug was in a position none of them named.

Codegen is pre-release but broad (closures, generics, strings, arrays, `match`, traits, `?`,
Perceus); the backend README is the inventory and `todo.md` the gaps.
