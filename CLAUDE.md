# lyra (Go) — Project Context

This is the main compiler infrastructure for the Lyra programming language. It contains the
parser, AST, type system, collector, typechecker, a standalone semantic checker, and the LSP
server.

Never create a git commit or push to remote unless the user explicitly asks in their current message.

Go module: `github.com/Lyra-Language/lyra`

**This file is a map, and it records rules rather than history.** Each package's depth lives
in a `README.md` beside its code; the sections here say what a package is and where to read
further. What must be obeyed regardless of which package you are in is under
[Rules and hazards](#rules-and-hazards) — read that first.

Open work is in `todo.md`; finished work, with the reasoning behind it, in `COMPLETED.md`.
A dated account of why something ended up the way it did belongs there, not here.

The tree-sitter grammar is a local dependency via a `replace` directive pointing to
`../tree-sitter-lyra`. After regenerating the grammar (`npx tree-sitter generate` in that
directory), always run `go clean -cache` before `go test` — otherwise Go's build cache serves
the stale compiled C parser.

## Data Flow

```
source text
  → pkg/parser                        tree-sitter CST (*sitter.Tree)
  → pkg/analyzer/collector            CST → *ast.Program + *symbols.SymbolTable
  → pkg/analyzer/checker              standalone AST passes (e.g. use-before-declaration)
  → pkg/analyzer/typechecker          AST → *typetable.TypeTable + []TypeError
```

The LSP server (`cmd/lyra-lsp`) runs this full pipeline on every document change and publishes
diagnostics from all three analysis stages.

## Rules and hazards

Violating any of these produces something that looks like it works. Each was learned from a
real failure, and none is local to one package.

1. **After changing `tree-sitter-lyra/grammar.js`: regenerate, then `go clean -cache`,
   then test.** Go's build cache does not hash `#include`d sources, so the compiled C
   parser goes stale and the suite silently runs against the *old* grammar. Push the
   grammar repo before the `lyra` code that depends on it — CI regenerates from the remote.

2. **Never call an accessor on the result of an optional field lookup without a nil check.**
   `node.ChildByFieldName(…)` returns a genuine Go `nil` `*sitter.Node` for an absent
   optional grammar field, and calling `ChildCount`/`Child`/`Kind` on it **hangs inside the
   go-tree-sitter CGO binding instead of panicking**. See `pkg/analyzer/collector/README.md`.

3. **Never return a nil expression node into the AST.** A collector hitting an
   unrecoverable value error must emit a diagnostic *and* return a placeholder node. A
   `nil` returned as an `ast.Expression` is a *typed nil* — it slips past `expr == nil` and
   crashes a later pass on the first field access. The statement analogue: a block skips a
   child that collects to nil, because a block's value is its final statement.

4. **Resolve top-level names only through the `Lookup*` accessors**, never by indexing
   `SymbolTable.Types`/`.Functions`/`.Traits`. Which declaration a name means depends on
   *which module is asking*, and a lookup scattered over dozens of sites cannot be taught
   that. Same reason `recordedType`, `types.StripNewtype`, `slotIsOwning`,
   `types.IsCopiedScalar` and `types.CollectTypeVars` exist: one predicate, so two passes
   cannot drift apart.

   **All three maps are keyed by `declKey`** — bare when a declaration is `pub` or in the
   entry module, `<module>::<name>` when private or when it takes a name reaching it from
   elsewhere (the prelude's, or one exported by a module it imports — `shadowsAmbient`) —
   so *which* accessor you use is a correctness question, not a style one. Prefer
   `LookupTypeFrom`/`LookupTraitFrom`/`LookupFunctionFrom(name, loc)`, which resolve as the
   file at `loc` sees it; bare `LookupType(name)` answers only for a program-wide name, and
   asking it from inside a module that declares its own returns *another* module's
   declaration.

   Four corollaries, each of which has already bitten:

   - A map key is **not** a source name, so anything user-facing (an LSP completion label,
     a "declared names" set) must read `decl.Name` rather than the key.
   - A `pub` check must ask about the declaration a reference *resolved to*
     (`declVisibility`), never look one up by name; and inside the symbol table use
     `BindingIn(module, name)` rather than falling through to `BindingOf(name)`.
     `DeclaringModule` is last-writer-wins, and reported a module's own type as private to
     another module that happened to declare the same name.
   - The same applies to a **trait**: `checkImplCoherence` keys duplicate impls on the
     resolved `*ast.TraitDeclStmt` (via `LookupTraitFrom`), not on `impl.TraitName`, or a
     program's own `trait Add` plus `impl Add for i64` is reported as a duplicate of the
     prelude's. **The purity pass had the same bug and kept it two months longer**, because
     it was not indexing the symbol table wrongly — it had *no symbol table at all* and
     built its own `map[traitName]*TraitDeclStmt` by walking the merged program, which is
     last-writer-wins by construction. An impl inherits its trait's effect bound through
     that map, so with two modules each declaring a `Speak`, an impl of the one declaring
     `pure say` inherited the other's absent bound and printed from a method its contract
     said was pure. Nothing reported it: the collision draws lyra-W016, which is about
     which declaration a *reference* means and says nothing about a bound going missing.
     A pass that cannot do a rule-4 lookup needs the table threaded in, not a local index.
   - The backend pays it too: `namespaceCallee` must not test membership with
     `DeclaringModule` and read `l.funcs[name]`, or a shadowed name called through a
     namespace (`seq.map(…)`) dies as `llvm: unsupported method call` on a program the
     front end checked clean.

   When a premise like "a top-level name is program-wide unique" is written into a comment,
   it is a bug waiting for the feature that retires it.

5. **The backend errors loudly rather than emitting wrong code.** A form that does not lower
   yet is a hard error, never a guess — including where it must repeat a check the front end
   already made.

6. **ASan only works because the test harness adds the `sanitize_address` attribute** to
   every `define` in the emitted module. Without it the instrumentation pass rewrites
   nothing and the ASan tests pass vacuously — which they did, swallowing three real faults.
   See `pkg/backend/llvm/README.md`.

7. **`build/std` is a symlink, not a copy**, and the std root is the directory *containing*
   `std/`, not `std/` itself. Every staleness failure this project has hit presented as a
   behaviour difference rather than as staleness, which is what makes them expensive.

8. **A `switch` over AST node kinds or composite types must have a case for every one
   that can hold a child — a missing case is silent and its symptom is remote.** This has
   bitten more than a dozen times across a dozen switches, and never looked like what it
   was: assignability rejecting a type against *itself*; a generic function emitted under
   its bare name and failing in layout; `pure` silently accepting an impure `noisy().0`;
   a quiet leak of one reference per call to any multi-clause function. **When adding a
   node kind or a composite type, grep for the switches over it; when fixing one, check
   the others in the same file, since these travel in pairs.**

   Six lessons the instances add, none of which follows from "add the arm":

   - **A second copy is enough** — the drift does not need three.
   - **Copies can agree and be wrong together.** The purity ladders all charged a builtin
     method no effect, which is right for scalar arithmetic and wrong for `s.slice(a, b)`,
     the first that allocates: `pure noalloc … => s.trim()` type-checked clean. A shared
     answer is a shared *assumption*, and it fails with no divergence to notice.
   - **…and copies that disagree can be a soundness hole rather than a wrong message.**
     There were three of those ladders: two *inference* walks (a free function's,
     a trait method's) and the *reporting* walk. The two inference walks differed on one
     line — a call to a trait-impl method charged only the method's base effect on the
     lambda side, and its base **plus the effects of the callbacks supplied at this site**
     on the method side. A `pure` function passing an impure callback through a trait
     method was therefore inferred pure, checked clean, and printed. The reporting walk had
     the right rule all along, which is why the diagnostic never fired: the machinery was
     correct and the table it consults was wrong. Inference is now **one** walk
     (`bodyEffects` over a `callable`), leaving the reporting walk as the only mirror.
   - **Two switches can disagree about a case neither one names**, which grepping will not
     find — `nominalHead` lacked `*ConstrainedType` while `types.HeadName` one layer up
     gives a newtype a head, so a method written *for* a newtype was silently unreachable.
   - **When adding an expression kind, grep for the kind it is a variant of.** The purity
     pass's allocation walk names allocating *forms*, not types, so `ArrayRepeatExpr` was
     missed in five places `ArrayLiteralExpr` appeared in — and two more on 08/24, in
     `isSyntacticLiteral` (so `let n: Nums = [7; 3]` over a newtype was refused while
     `[7, 7, 7]` was not) and `firstNonConstant` (so `const XS = [7; 3]` was "not a
     compile-time constant"). **Seven instances of one omission.** The sweep that finds them
     is mechanical and takes a minute: list every file mentioning `ArrayLiteralExpr` and check
     each for `ArrayRepeatExpr`; the node and collector definitions correctly have their own,
     everything else is a candidate. The second 08/24 instance was found that way, having been
     invisible to the bug report that produced the first. **A *binder* is its own such
     family**, and the captures pass proves it: its free-variable walk binds a `for-in`
     variable and a C-style loop counter — with a comment explaining that a binder it does
     not know reads as a capture — and had no case for a comprehension's generator, so a
     comprehension inside a closure could not be compiled at all. Anything that introduces a
     name is a binder: parameters, `match` arms, loop variables, destructurings, generators.
   - **A field that is a bare `string` rather than a node is invisible to every walk.**
     `VarReassignmentStmt.Name` is the one assignment target with no `IdentifierExpr` behind
     it (`n += 1` has one in `Left`, `p.x = v` an expression path), so the captures pass
     never saw a write-only capture — and the symptom was not a missing diagnostic but a
     **nil deref in the backend**, since the closure environment had no slot for a name
     nothing had recorded. When a walk's contract is "every mention of a name", a plain
     string field is a hole in it that grepping for node kinds will never find.
   - **A new *declaration kind* has no checklist, and pays for it repeatedly.**
     `ExternDeclStmt` landed on 08/18 and by 08/19 had been found missing from ten
     switches over top-level declaration kinds — `declIsPublic` (two modules each
     declaring `extern abs` collided on a program-wide name), `attachDoc` and `docOf`
     (every `///` above an extern reported as documenting nothing), `captures.globalNames`
     (a closure calling an extern failed to lower), plus docgen and five LSP surfaces.
     None of them shares a file or a package with the others, so grepping for one kind
     finds them only if you know to grep. All ten are fixed (08/20); the sweep that found
     the last six is the thing to repeat, not the list.

     **`UnsafeBlockExpr` cost the same tax on the expression side**, and worse: `hover.go`'s
     `findExprAtPos` and `definition.go`'s `scopeInExpr` had no case for it, so hover,
     go-to-definition, rename and document-highlight all returned *nothing* inside an
     `unsafe` block — which is the whole of a program's FFI and raw-pointer code. The
     symptom of a missing case in a position lookup is an editor doing nothing, which
     reads as "unsupported" rather than as a bug, so it sat from 08/18 to 08/20.

     **`pkg/ast/exhaustive_test.go` is that checklist**, written 08/20. It parses the
     switches rather than reflecting on types — the question is about *code*, and
     reflection can say what fields a node has and never what a switch does with it. Two
     halves: registered mirrors of `walkExprChildren`/`walkStmtChildren` must cover every
     case the canonical walker has, and every switch in `declarationConsumers` must cover
     every kind in `declarationKinds` — a list guarded in turn by "a statement with a
     `Doc` field is a declaration", so a new declaration node fails there first with a
     message naming what to do.

     An omission is a bug; an **exclusion is a claim**, written next to its reason. It
     found thirteen more expression kinds missing from the LSP walker on its first run,
     including both loop forms, which had made navigation dead inside every loop body in
     every program. What it cannot do is find a switch nobody registered; adding an entry
     when you add a *consumer* is still manual, and adding a *node* is not.

     **A registered mirror is second best, and retiring one is the real fix** (08/23).
     Two hand-copied walkers were deleted rather than registered — the collector's
     constructor rewriter, now `ast.RewriteStmt`, and the LSP's position lookup, now
     `ast.WalkStmt`. Each had been written because the canonical walker could not do the
     job: one needed to *replace* a slot, which a visitor cannot, and the other to find
     the innermost node containing a position. Both are ordinary uses of a walker once
     the walker offers them, and the drift they had already suffered was invisible
     precisely where a test was absent — the LSP's *expression* switch was registered and
     in step, while its *statement* switch, which nothing watched, sat eight kinds behind
     and made navigation dead inside every trait-impl body in every program.
   - **A new *type* kind pays the same tax as a new declaration kind, and has no
     checklist.** `RawPointerType` landed 08/18 and was found missing from **two**
     composite-type switches on 08/22, each with a symptom nowhere near the cause:
     `SizeAndAlign`, where it was not "a pointer has no size" but *any aggregate
     containing one* failing to lay out (`std.ffi`'s `CBuffer` could not be captured by a
     closure or held in a `[]T`); and `resolveTypeWith`, where an unresolved *pointee*
     made `^mut CULong` and `^mut u64` different types, so a type alias could not be used
     for a C in/out parameter — which is what a pointer at the boundary is.
     `exhaustive_test.go` enumerates *declaration* kinds; nothing enumerates the switches
     over composite types, which is the family `emitRetainValue`/`emitDropValue` and
     `mentionsTypeVar` also belong to.

     **Every rung that reads a value's type must strip newtypes *itself*, and resolve
     between strips.** `stripNewtypeResolving` is the one answer — used by the read-out
     conversion, indexing and the method fallback. Two things make a plain
     `types.StripNewtype` insufficient: a binding's type can arrive as the bare declared name
     rather than a resolved `*ConstrainedType` (which is how a pattern-bound `d: Meters`
     reached the conversion check and was refused), and a **generic** newtype's base is a
     `ParameterizedType` that has to be resolved before the wrapper under it is visible, so
     `newtype Outer<t> = Inner<t>` over `newtype Inner<t> = []t` stops one layer short.

     This became load-bearing on 08/24. Literal propagation re-recorded an annotated *array*
     binding's root with the base's shape, so `let b: Bag = ["x"]` read as
     `DynamicArray<string>` — which made indexing work **by accident** and made the value fail
     assignability the moment it crossed a call boundary. Two needs were resting on one
     recorded type, and the first attempt at the fix (restore the wrapper, change nothing
     else) broke indexing and two tests. It is both halves or neither.

     **A newtype is the same tax, and `*ConstrainedType` is its `RawPointerType`.**
     `resolveInstantiation` (backend/llvm/generic_types.go) is the choke point that turns a
     `ParameterizedType` into the shape it denotes precisely so a dozen downstream switches
     need no generic case — and it had arms for NamedStructType, TupleType and DataType and
     none for the wrapper a parameterized newtype expands to, so `newtype Sorted<t> = []t`
     checked clean and failed the build. A *scalar* base hid it: no drop glue is generated,
     so nothing asks for the instantiation.

     **Sweeping the family by reading switches is the wrong move; probing behaviours is the
     right one.** 35 switches mention two or more of the composite kinds and 19 have no
     ConstrainedType arm — but most are container-shaped and correctly lack it, so the list is
     mostly false leads. Seven small programs putting a newtype in each position that matters
     (in a struct, in an array, captured by a closure, compared, matched, nested in a generic)
     found the one real second bug in minutes: the read-out conversion `i64(c)` was refused
     for a *pattern-bound* binding, having been resolved only after the first strip.

     **The sweep, when it was run, found only those two**: the type-variable walks
     (`Substitute`, `CollectTypeVars`, `mentionsGenericParam`, ownership's
     `substituteTypeVars`) already handle pointers, because generics over `^t` landed with
     the feature on 08/19. The container-shaped switches (`elementType`, `isByteArray`,
     `iterableElementType`) have no pointer case and are right not to. So the family to
     check when adding a type kind is the walks that must reach *every* composite —
     resolution, layout, substitution, retain/drop — not every switch that mentions one.
   - **Paired walks must be fixed in one change** — and better still, stop being a pair.
     `emitRetainValue`/`emitDropValue` both lacked `ParameterizedType`, and fixing only the
     drop is an instant double free. Note *both* lacked it, and both lacked
     `AnonymousStructType` too: two copies can agree and be wrong, which is the failure a
     side-by-side reading cannot find. They are now one walk over
     `emitOwnedValue(…, retainWalk|dropWalk)` (`owned_walk.go`), differing only at a
     managed leaf, so a copy and its death cover the same fields by construction.
     **Equality is deliberately not folded in with them**: it looks like a third copy but
     stops at a different place (it descends *into* a managed value to compare it) and
     nothing breaks if it visits a different set — that makes it a different walk, not a
     third copy of this one.
   - **A copy that admits it is a copy is still a copy.** The typechecker's
     `substituteGenerics` said in its own comment that it walked only "the handful of
     compound type shapes a data constructor's payload realistically takes today" — and
     `types.Substitute` beside it said in *its* comment that it exists so there is one
     walker. A generic over `^t` solved `t` and then compared the argument against an
     un-substituted `^t`. The local copy is now a one-line call to the real one.
   - **A memory-safety test can pass because the code under it does nothing** — a weak-cycle
     test was green *by leaking* before the glue walked the field at all.

   **The durable fix for a switch with more than one caller is to stop having more than one
   of it.** Established single answers, to use rather than re-derive:

   | question | the one answer |
   |---|---|
   | which type variables does this type mention? | `types.CollectTypeVars` |
   | resolve a type (reporting or quiet) | `resolveTypeWith`, behind `resolveType`/`resolveTypeIfKnown` |
   | is this tail a value or a statement? | `checkExprForEffect` |
   | did this expression produce a value? | `isVoidResult` (a nil **and** a void-typed `ir.Call`) |
   | does this value transitively own a reference? | `ownership.OwnsManaged` |
   | is that sharing observable? | `ownership.SharesMutableState` |
   | substitute type variables in a type | `types.Substitute` |
   | what are this node's children? | `ast.WalkStmt`/`ast.WalkExpr` (`…Children` to skip the node itself) |
   | does this statement carry an expression ownership must see? | `ownership.analyzer.stmt` — **all** statement kinds, not the five that were obvious |
   | rewrite expressions in place | `ast.RewriteStmt`/`ast.RewriteExpr` |
   | see through a newtype to its base | `stripNewtypeResolving` (typechecker) |
   | where is the expression at this position? | `findExprAtPos` (`cmd/lyra-lsp/hover.go`) |
   | bind a generic type's arguments to its parameters | `ast.BindGenericParams` |
   | what does a value of this type hold inline? | `ownership.eachComponent` |
   | retain or release what a value owns | `emitOwnedValue` (`backend/llvm/owned_walk.go`) |
   | did this operand diverge? | `diverged(v, block)` (`backend/llvm/trap.go`) |
   | is this CST node a comment? | `cst.IsComment` |

9. **A name does not identify a declaration, and may not even identify one function.**
   A *key* is module-qualified for a private declaration (rule 4), and a
   **receiver-overloaded** name maps to several declarations at once, told apart only by
   the receiver's type. So any pass answering "what does this call call?" by looking the
   name up is wrong in one of two quiet ways: it gets another module's function, or it gets
   whichever overload was registered last. Read `typetable.TypeTable.Callee(call)` first —
   the typechecker publishes the member it picked — and fall back to `LookupFunctionFrom`.
   The backend pays the same tax: `l.funcs` cannot hold two functions of one name, so an
   overload is keyed by its *declaration* and its emitted symbol carries the receiver head.

    **Allocation is context-determined, and a construction has three contexts.** A
    construction leaf has no flavor of its own — `Node { v: 2 }` is inline or heap-boxed
    depending on what it is used *as* — so `propagateAllocation` pushes the flavor down from
    an annotated binding, a declared return, **and an argument** (08/26). The argument was
    the one left out, and the symptom was not a missing box but the struct passed *by value*
    to a callee expecting a box pointer: a segfault on macOS, and on Linux the typed-pointer
    error `'@take' defined with type 'i64 ({…}*)*' but expected 'i64 (%Node)*'`. Add a
    fourth context and it needs the call; the width rule (`propagateLiteralType`) is its twin
    and sits on the adjacent line at each site.

10. **A pass that indexes a call's arguments positionally is one AST shape away from
    being silently wrong.** Purity reads `call.Arguments[idx]` against the *declaration's*
    parameter at `idx` (`callableParams`), so any call form whose receiver sits outside
    `Arguments` shifts every index by one — and a function-typed argument satisfies the
    wrong function-typed parameter without complaint, so a declared effect bound simply
    stops being enforced with nothing reported. Trait methods pay this with
    `methodArgumentAt`; UFCS avoids it by desugaring the receiver *into* `Arguments` before
    any later pass runs. Prefer the desugar.

11. **A desugaring can rebind a parameter, so a later pass must not assume a body refers
    to one by its declared name.** `desugarClauses` turns a multi-clause function into
    `match (p0, p1) { … }`, and a clause is free to name a parameter something else. When
    `callableParams` knew only the declared names, a call through the rename resolved to
    nothing and took the unresolved-callee default (`AllEffects`) — and it *worked* whenever
    a clause happened to reuse the declared name, so correctness was contingent on a
    coincidence of spelling, which review cannot see. `addMatchAliases` maps arm bindings
    back to the parameter position they destructure. Fix such a thing at the construct the
    desugaring *produces*, and be suspicious of any name-keyed analysis downstream of a pass
    that rewrites bindings.

12. **A box's `drop_fn` may free the box it is running on.** `lyra_rc_release` decrements
    strong, runs the payload's drop glue, then decides whether to free — and that glue is
    arbitrary user code which, through a cycle, can drop the last **weak** reference to the
    same box. `lyra_rc_weak_release` then frees the memory and the outer release frees it
    again. The strong owners therefore hold **one implicit weak reference**, taken in
    `lyra_rc_alloc` and dropped after the glue returns, so the count cannot reach zero
    mid-drop; Rust's `Arc` does the same, for the same reason. Do not "simplify" that back
    into a `weak == 0` test — it reads as equivalent and is an ASan-confirmed double free.

13. **A bound-dispatched call is resolved in two places, and both must be asked.** The
    typechecker resolves it *abstractly* — the receiver is a type variable there — and
    publishes one concrete candidate per implementing type; only a specialization names a
    function. Two faults came of forgetting the second half, and neither was diagnosable:
    reading borrow modes from the **resolution** table (which a bound call has no entry
    in) passed a `mut` receiver by value into a method expecting a pointer, a wild load
    rather than a mismatch; and looking a candidate up by `recordedType(...).String()`
    asked under the *instantiated* name — module-prefixed — while the table is keyed in
    the typechecker's spelling. Use `candidateKey` for the lookup and `methodParams` for
    the modes.

    **The module header is what makes the second one visible**, which is why it survived:
    every reproduction small enough to paste has no `module` line, and every real program
    has one. When a bug's trigger is a header, snippet-sized testing is structurally
    blind to it — the backend suite prepends `module main` for exactly this reason.

    **The general shape is a name resolved from the wrong scope, and its tell is that the
    bug disappears in a one-module program.** A second instance, 08/26: trait default
    bodies are checked by a *setup* pass, before the per-statement loop that installs each
    statement's module scope — so `tc.scope` was the **global** scope, which holds only
    what modules export, and a bare reference to the module's own top-level name resolved
    to nothing. With no prelude the global scope happens to hold the program's own
    declarations, so it worked in every small reproduction *and* in the typechecker's own
    test harness, which has no prelude. The rule that follows: **a pass that checks a body
    outside `checkInModule` must install the module scope itself**, and a test for one
    belongs where the prelude is real.

14. **A diagnostic with no Location is not merely imprecise — it appears on every file.**
    `diagnosticsFor` keeps a location-less diagnostic deliberately, on the grounds that it
    is program-level and has nowhere else to go (a missing `main`), so a *per-node* warning
    whose node has a zero Location attaches itself to whatever the user is compiling. Two
    warnings on prelude loops showed up on a file with no loop in it, which is how the
    missing `Location` on `ForInLoopExpr` was found — it had never had one. When adding a
    node, set its span; when adding a diagnostic, check that the node you report against
    has one.

15. **A diverging operand hands back a nil, and every consumer must stop.** `panic(msg)`
    has type `never`, so the typechecker accepts it anywhere — a struct field's value, an
    array element, a call argument. Lowering it terminates the block and returns a **nil**
    value; `diverged(v, block)` is the test, and a consumer that skips it builds an
    instruction around the nil.

    llir accepts that operand when the instruction is built and dies at **module
    serialization**, so the stack trace is one frame in the emitter at `m.String()`, naming
    neither the expression nor the pass. Thirteen sites consume such a value; **four had the
    guard and nine did not**, and the four were exactly those a bug report had once landed
    on — which is the shape to expect from a protocol enforced by convention rather than by
    a type. `coerceAggregateElem` is the funnel for the aggregate half and now rejects a nil
    with rule 5's loud error naming the expression, so a site added later fails legibly
    instead of at serialization.

16. **A comment is a *named* node, so `child.IsNamed()` does not mean "an element".**
    All three comment kinds are grammar `extras`, free to sit between any two tokens —
    including inside a comma-separated list, which is ordinary formatting in a multi-line
    call. Every list collector told an element from a comma by asking `IsNamed()`, so seven
    of them collected the comment as an element: a nil `ast.Expression`, which is rule 3's
    typed nil. `f(1, /*a comment*/ 2)` was reported as *"expected 2 argument(s), got 3"* and
    `[1, /*a comment*/ 2]` type-checked clean and died in the backend. `for … in` was worse
    than either — it assigned the iterable from any named child, so a comment **overwrote**
    it with nil. Use `cst.IsComment`; blocks had survived by discarding nil *statements*,
    which is the symptom rather than the cause and does not generalize to an expression list.

17. **A builtin returning an owned managed value needs two things the defaults get wrong.**
    `read_line` is the model: the ownership pass must know it owns its result
    (`calleeIsOwningBuiltin`), because the unresolved-callee default treats a *result* as
    borrowed and that direction leaks rather than being leak-safe; and its call site must
    lower **branchlessly**, because a branching one returns a merge block, which is neither
    case `flushStmtTemps` handles — it released the string before the `match` consuming it.
    `<=>` is lowered branchlessly for the same reason.

## Documentation comments

`///` documents the declaration below it, `//!` the module the file belongs to. The
language-level rules are in the workspace `CLAUDE.md`; what matters inside this project:

- **`ast.Doc`** (`pkg/ast/doc.go`) holds the Markdown body, the first-paragraph
  `Summary`, and the `#`-heading `Sections` with `# Examples`/`# Panics`/`# Errors`
  classified. `NewDoc` returns **nil** for an empty comment, so "documented with
  nothing" is not a state any consumer has to handle.
- **Docs attach to declarations, not to types.** A struct field's doc is in
  `TypeDeclStmt.MemberDocs` (read it through `MemberDoc(name)`), never in
  `types.StructField` — `pkg/types` knows nothing about documentation. If you add a
  documentable declaration, the Doc field goes on the AST node.
- **Attachment is `ctx.DocFor(node)`** (`collector_ctx/docs.go`), one helper for every
  site, because a doc comment is an `extra` and so is a *sibling* of what it documents
  at whatever level of the tree that lives. Top-level declarations are stamped in
  `walkProgram`'s `attachDoc` — that loop *is* the set of top-level declarations, so
  "only a top-level binding is documentable" holds with no per-site test.
- **Two CST placement rules `DocFor` absorbs**, both of which fail by documenting every
  member except one: an extra before a node's first token attaches to the *enclosing* node
  (so the first method in a `trait`/`impl` body sits one level up), and a separator token
  may sit between the doc and its node (the `|` of the leading-bar `data` style). Pinned by
  the `IncludingTheFirst` tests in `pkg/analyzer/collector/tests/doc_comment_test.go`.
- **A doc that attaches to nothing is `lyra-W017`**, reported by `ctx.ReportStrayDocs`
  as a post-pass over the file's tree — after the walk, because whether a `///` was
  claimed is only knowable once every collector that might have claimed it has run. The
  claim set is keyed by start byte and reset per file (`ctx.ResetDocs`).
- **A request handler's prologue is `handler.go`'s**: `defer recoverHandler(name, &result,
  &retErr)` for the panic guard, `h.docFor(uri)` for the analysis and source under one lock,
  and `h.cursorAt(uri, pos)` for those plus the cursor already converted into
  ast.Location's terms. Thirteen handlers spelled those twenty lines out. The guard is the
  one to be careful with: an LSP server must not die on a single bad request, and
  `recover()` reports a panic only when called *by the deferred function itself* — so
  wrapping `recoverHandler` in a closure silently disables it. `TestRecoverHandler_*` is
  what notices.
- **Every position-based feature starts at `findExprAtPos`** (`hover.go`), and
  `definition.go`'s `scopeInExpr` is its twin for scopes. `findExprAtPos` **no longer
  switches on node kinds** — it walks with `ast.WalkStmt`/`ast.WalkExpr` and keeps the
  narrowest span containing the position — so only the twin can now fall behind, and a
  kind missing from it means the expression is found and its name resolved in the wrong
  scope. It deliberately does **not** prune at a node failing to contain the position: a
  node with an unset `Location` (hazard 14) would otherwise take its whole subtree out of
  reach.
- **LSP hover** renders the doc under the type (`cmd/lyra-lsp/hoverdoc.go`).
  `resolveDoc` mirrors `resolveDefinition` case for case; keep them in step, or hover
  shows one symbol's docs above another symbol's type. **A typeless expression may still
  be documented** and `Hover` must not bail on the type alone: the method name of a UFCS
  call is a callee `desugarUFCSCall` synthesized, so it has no recorded type — and that is
  the spelling the whole standard library is written for. That position renders the doc
  with no signature block.
- **The prelude is the coverage guard**
  (`pkg/analyzer/collector/tests/prelude_docs_test.go`): it collects the real
  `std/prelude` as the multi-file module it is and asserts every declaration and member is
  documented, that `# Panics` sections sit on exactly the functions that trap, and that the
  module doc joins across files. `lyrac check` alone will *not* catch a detached doc —
  W017 is a warning, so the exit code stays 0.

## Operators that dispatch

Three groups, and which one an operator is in is a design decision rather than an
implementation state:

- **The comparisons are the compiler's.** `==`/`!=` are structural, overridden by the
  prelude's `Eq`; `<`/`<=`/`>`/`>=`/`<=>` all derive from `Ord::compare`. A `(_==_)`
  method name is refused (`lyra-E039`), because a second mechanism would be a coherence
  question with no answer and declaring them one at a time is how `<` comes to disagree
  with `<=>`. Both traits are found by **`@builtin(Ord)`/`@builtin(Eq)`**, not by the
  spelling, and dispatch filters candidate impls by the resolved *declaration* — filtering
  by name is what let a user's own `trait Ord` be taken for the prelude's.
- **Arithmetic and bitwise are the author's.** `+ - * / % << >> & | ~`, prefix `-` and `~`,
  and the compound assignments dispatch to a trait method named for the operator — keyed on
  the **method name**, with the trait whatever the author declared. `+` on a matrix and `+`
  on a duration share no invariant, so nothing is bought by insisting they come from one
  trait; two traits providing one operator for one type is an ambiguity reported at the
  operator. An operand that is a **type parameter** resolves through a `where` bound, the
  same abstract dispatch a bound `.method()` call takes.
- **The rest are inert, each for its own reason**, and the warning says which
  (`lyra-W015`): `&&`/`||` cannot short-circuit through a call, `!` is boolean negation,
  `**` is a spelling with no operator, the suffix forms name operators that do not exist.

Two rules hold across all of it. **A primitive is never routed through an impl** — `1 + 1`
is a machine add whatever a program declares — where "primitive" is the receiver
**unstripped**: a newtype over a scalar is not the scalar, so `impl Add for Cents`
dispatches while `impl Add for i64` stays inert. (Stripping first makes a scalar newtype
operator-dead from both sides, silently. Beside it, the overflow-arithmetic builtins are
refused on a newtype receiver, `lyra-E043`: they are the operators' escape hatches, so the
method fallback must not hand out what the operator rule withholds.) And the resolution is
`resolveTraitMethodNamed`, the *same* function the identifier path uses with a full
`MethodName` key, so an operator and a `.method()` call cannot come to disagree about
generic impls or `where` bounds. An operator is a call, so the purity ladders charge it as
one (`operatorImplEffect`), which both of them reach through the shared `inference`.

## Supertraits

`trait B: A` means both halves:

- **The obligation** (`lyra-E040`, `checkTraitImpl`): `impl B for T` requires an
  `impl A for T`. Declaration order does not matter — the impls are gathered up front.
- **The use** (`closeOverSupertraits` in `typechecker_trait_dispatch.go`): a `where t: B`
  bound reaches `A`'s methods, and satisfies a callee's `where u: A`.

The second is a **transitive closure taken where a bound set enters scope**, not a rule
each consumer applies. Four sites read `tc.genericBounds` — bound dispatch, the
generic-argument check, operator overloading, the `Show` desugar — and expanding at the
two write sites (`pushGenericBounds` for a binding, `checkTraitImpl` for an impl) is what
keeps them from needing to agree about anything. **If you add a fifth reader, it is already
correct; if you add a third writer, it is not.** Those two writers are twins: a bound that
reaches `A`'s methods when written on a binding and not when written on an impl means
different things depending on where it is written.

Two properties worth not breaking. The closure is **cycle-safe by a visited set** —
`trait A: B` alongside `trait B: A` is legal, meaning the two are always implemented
together, which is precisely what E040 then requires of every implementer; assuming a DAG
hangs the typechecker, which presents as a frozen editor. And the **backend needs nothing**:
dispatch publishes candidates for the trait that *declares* the method, so a supertrait
call resolves to that trait's impls like any other.

**An umbrella trait** — `trait Arithmetic: Add + Sub + Mul + Div`, with or without a `{}` —
needs a trait body to be optional, braces and all. The collector reads the field with
`cst.Field` + a nil check rather than `MustField` (`declarations/trait_decl.go`): an absent
list is an empty method list, **not** a dropped declaration. `MustField` returns nil, which
would erase the trait and then report `unknown trait` at every impl of it — a diagnostic
pointing everywhere except at the declaration.

**The prelude ships `Add`/`Sub`/`Mul`/`Div` and the `Arithmetic` umbrella**
(`std/prelude/math.lyra`), with impls for all ten integer widths and three float widths.
They exist so a **bound** can be satisfied, not so a call can dispatch — without them
`where t: Arithmetic` is undemandable of a number, and every generic numeric function is
unwritable. `impl Add for f64 { (_+_) = (self, o) => self + o }` is not recursion, by the
primitive rule above.

## Trait default methods

A trait method may carry a body an impl inherits by writing nothing and overrides by
writing a clause. **`Self` is a type variable in that body** — `types.GenericType{"Self"}`,
bounded by the declaring trait — so it is checked once (`checkTraitDefaultMethods`) and
monomorphized per implementing type, which is what a generic function already is. The
backend needed nothing: `dispatchViaGenericBound` types the body's calls,
`SetBoundCandidates` publishes one concrete impl per implementing type, and `recordedType`
substitutes `Resolution.Bindings` at each specialization.

The name is unforgeable — a type variable is lowercase by lexer rule, so no program can
declare one called `Self`.

Five things to know before touching it:

- **`ast.TraitMethod.DefaultImpl()` is the one instance**, cached on the AST rather than
  per pass. Dispatch, the MethodTable, the purity fixpoint, the ownership table and the
  backend's emitted-method cache all key on the pointer, so a second instance means the
  body is emitted once per call site. One per trait *method*, not per impl — the impl is
  what `SpecKey` varies over.
- **Dispatch tries the impl's own clauses first**, and falls back to the default only when
  they match nothing. That is what makes an override an override rather than an ambiguity.
- **The body's inner bound calls need publishing at the concrete receiver**
  (`publishDefaultBodyCandidates`). Without it the body type-checks — the bound is
  abstract — and then cannot be lowered, because the candidate table would hold only what
  `boundCandidatesByType` keys by the impl's *declared* target.
- **An unsatisfied bound in a default body is told to use a supertrait**, not a `where`
  clause (`reportUnboundTypeParameter`). `Self` is a type variable no program declares and a
  trait method has no `where` clause to constrain it on, so `trait B: A` is the only
  spelling that exists — the old message advised `where Self: A` and sent a reader looking
  for syntax the language does not have. A test **takes the advice** and checks the program
  then compiles, which is the only way to know a diagnostic's fix is real rather than
  plausible; one asserting the wording alone would have passed for as long as the message
  was wrong.
- **It is checked by a setup pass, so it must install the module scope itself**
  (`checkOneDefaultMethod` → `moduleScopeOf`). The per-statement loop wraps each top-level
  statement in `checkInModule`; this pass runs before that loop, so `tc.scope` is the
  *global* scope, which holds only what modules export. Without it a bare reference to the
  declaring module's own top-level name resolved to nothing — **silently**, since the
  "undefined function" arm is guarded by a visibility check that answers *found but private*
  for a name the global scope cannot see — and the program failed three passes later with
  `llvm: call to unknown function`. Any setup pass that checks a body has this obligation.
  And a **generic call** in that body records `callee<t=Self>`, a template, which
  `closeInstantiations` composes only because it seeds from `MethodTable.Specializations()`
  as well as from the instantiation table.

The alternative considered and rejected was deep-copying the default clause into every
impl that lacks it. That needs a full expression/statement cloner this compiler does not
have, and a missing case in one is a silently *shared* subtree — hazard 8 with a
miscompile at the end of it.

## Raw pointers

`&x`/`&mut x`, `p^`, `p^ = v` and `unsafe { … }` (`typechecker_pointers.go`,
`backend/llvm/pointers.go`). The language-level rules are in the workspace `CLAUDE.md`;
inside this project, three things:

- **`lyra-E011` is wired in again** (`driver.go`). Its policy — a raw-pointer op or a call
  to an `unsafe` function needs an enclosing `unsafe` block or function, and unsafe-ness
  does not leak across a lambda boundary — was written and tested throughout but *not run*
  between 08/13 and 08/18, because the block it recommended was itself an unknown
  expression.
- **Mutability is two checks, deliberately.** `requireMutableRoot` reuses the binding rule
  `checkLValueAssignment` applies, so a `&mut` cannot outrun the assignment rule; the
  pointer's own `IsMut` is what `checkDerefWrite` tests. Neither implies the other.
- **`UnsafeBlockExpr.Body` is a pointer**, and that is load-bearing rather than a style
  choice: stored by value the collector's `Body: *body` kept a *copy*, whose address
  differed from the node the scope table was keyed on, so every binding declared inside an
  `unsafe` block resolved nowhere. Invisible while the block was refused before anything
  looked inside it.
- **`^mut T` is assignable to `^T` and not the reverse** (`isAssignable`), while
  `TypesEqual` keeps telling them apart — the same split the effect-bound rule draws, and
  for the same reason: identity is a different question from what may be passed. The
  pointee stays invariant. The downgrade is *real*, so writing through a binding annotated
  `^T` is still lyra-E061 whatever the pointer came from.
- **`p.offset(n)` is a builtin method, and its unsafe-context check is in the
  typechecker** — `requireUnsafeBuiltin`, beside `requireUnsafeCall` and for the same
  reason: the question needs the **receiver's type**. `p.offset(n)` and `xs.offset(n)` are
  the same three tokens, so the syntactic pass could only refuse both or neither. It
  lowers to a `getelementptr` with the pointee's type, so "in elements" is what LLVM
  already means and no scaling is written by hand.

## Sweeping for surfaces nothing reads

Features that parse, collect, and are consumed by nobody look implemented and do nothing,
which costs more than an absent feature does. Known instances, all now closed: `wallClock`, a binding's `where` bounds,
`@derive`, operator-named trait methods, trait default methods.

The sweep that finds the AST half: enumerate every exported field of every struct in
`pkg/ast`, then grep for a reader **outside `pkg/ast`, outside `pkg/printer`, and outside
tests**. Excluding the printer is the part that matters — it reads every field by
reflection, so it makes everything look consumed. Excluding the declaring package matters
too, or a field read only by its own accessors (`SymbolTable.Traits`) reports as dead.

A full run over 119 fields found 2 genuine phantoms. The conclusion worth keeping is that
the AST surface is *not* where this problem lives — the phantoms were in effect tables
(`builtinEffects`), in glue switches missing a case, and in grammar rules with no collector
consumer. Those need their own sweeps, and a field-level one will not find them.

## Package map

| Package | What it is | Depth |
|---|---|---|
| `pkg/parser` | CGO wrapper around tree-sitter; `Parse(source) (*sitter.Tree, error)` | — |
| `pkg/cst` | CST accessors — `cst.Field`, the one way to read a grammar field | below |
| `pkg/ast` | AST node definitions; `AstNode` / `Named` / `Statement` / `Expression` / `Pattern` | — |
| `pkg/ast/symbols` | `SymbolTable` + the `Scope` tree; per-module name resolution | [README](pkg/ast/symbols/README.md) |
| `pkg/types` | The `Type` interface and every implementation; allocation flavors | [README](pkg/types/README.md) |
| `pkg/typetable` | `ast.Expression` → resolved type; the method/instantiation tables | below |
| `pkg/analyzer/collector` | CST → `*ast.Program` + `*SymbolTable` | [README](pkg/analyzer/collector/README.md) |
| `pkg/analyzer/checker` | Standalone AST passes — purity, effects, use-after-move, value ranges | [README](pkg/analyzer/checker/README.md) |
| `pkg/analyzer/typechecker` | Inference and checking; generics; trait dispatch | [README](pkg/analyzer/typechecker/README.md) |
| `pkg/analyzer/captures` | Each lambda's free variables, for the closure environment | [README](pkg/analyzer/captures/README.md) |
| `pkg/analyzer/ownership` | Where the backend must retain/release; Perceus | [README](pkg/analyzer/ownership/README.md) |
| `pkg/modules` | Import resolution (a module is a file *or* a directory), namespacing, the implicit prelude | [README](pkg/modules/README.md) |
| `pkg/driver` | The one reusable front-end pipeline | below |
| `pkg/backend/llvm` | The LLVM IR backend | [README](pkg/backend/llvm/README.md) |
| `pkg/docgen` | AST → per-module documentation; the Markdown renderer | below |
| `pkg/printer` | Reflection-based AST printer, for golden tests | — |
| `cmd/lyra-lsp` | LSP server over stdio | below |
| `cmd/lyrac` | Compiler CLI (`check` / `build` / `run` / `doc`) | below |

### `pkg/cst`

`cst.Field(node, "name")` is **the** way to read a grammar field, and the collector uses
nothing else. It answers exactly what `node.ChildByFieldName` did, nil included — so the
nil-node hazard (rule 2) is unchanged — but resolves the field name to a grammar id once
instead of allocating a C string, calling into C and freeing it on every lookup.

That matters more than it reads: `ChildByFieldName` was **~26% of all samples** in an
analysis run, because the collector asks at nearly every node, and the cached id made the
whole pipeline **~25% faster** end to end. Measure with `pkg/driver`'s `BenchmarkAnalyze_*`,
which run the real pipeline over the real prelude — the LSP re-runs all of it on every
keystroke, so this is per-keystroke cost.

### `pkg/ast`

All AST node definitions. Key interfaces: `AstNode` (`node()`, `GetLocation()`), `Named`
(adds `GetName()`), and the supertypes `Statement`, `Expression`, `Pattern` — every node
implements one. All concrete nodes embed `AstBase`, which holds a 1-based `Location`
(`Pretty()` formats a compact `line:col`). Files are organized by node kind:
`expr_math.go`, `stmt_for_loop.go`, `decl_trait.go`.

### `pkg/typetable`

- `TypeTable` — `ast.Expression` → resolved `types.Type`. Populated by the typechecker;
  read by later passes. `Set(expr, typ)` / `Get(expr)`.
- `MethodTable` — a `*ast.FunctionCallExpr` resolved to a trait-impl method → the matched
  `*ast.TraitMethodImpl`. Populated during dispatch; read by the purity checker so it does
  not re-derive dispatch. `Get` is nil-receiver-safe, so a caller with no typechecker pass
  can pass `nil`.
- `TypeTable.SetCallee`/`Callee` (`calleetable.go`) is the same arrangement one rung down,
  for **receiver-keyed overloading**. Only overloaded calls are recorded — every other
  callee still resolves by lookup, and a second answer to a settled question can disagree —
  so a consumer reads this first and falls back.
- A second map on `MethodTable` records **abstract bound dispatch** (a call on a bare type
  parameter resolved through a `where` bound): `SetBound`/`GetBound` associate the call with
  a `BoundMethodRef{Trait, Method}`. There is no single concrete impl, so the purity checker
  joins over all impls of that trait method.
- `SetBuiltinMethod(call, allocates)` records what a builtin method resolved to, because
  only the typechecker still has the receiver's type. Both purity ladders read it — the
  one inference walk and the reporting walk.
- `SetBoundCandidates` is how a bound-dispatched call **lowers**: the typechecker publishes
  one resolution per implementing type and the backend picks by the receiver's substituted
  type. **Impl matching stays in the typechecker** — a second copy in codegen is exactly the
  drift `Resolution` exists to prevent. An unsatisfied bound is `lyra-E036`, reported at the
  *instantiation*, the only point where the question has an answer.

### `pkg/driver`

The single reusable entry point to the whole front-end. `driver.Analyze(source []byte)
*Result` runs parse → collect → the standalone `checker.Check*` passes → `typechecker.Check`
→ `captures.Analyze` → `checker.CheckPurity` → `ownership.Analyze` and returns a
`Result{Program, SymbolTable, ScopeTable, TypeTable, MethodTable, Ownership, Captures,
RangeSafety, Diagnostics}`. Every pass's errors are normalized to
`[]diagnostic.Diagnostic` (CST parse errors converted from tree-sitter's 0-based positions
to 1-based `ast.Location`). `Result.HasErrors()` / `Result.Errors()` filter by severity.
This is where a backend, or any tool needing a typed program, starts.

**A post-typecheck pass may be there for the *settled* type, not only for the MethodTable.**
`checker.CheckArrayRepeatAliasing` (lyra-W019) is the clearest case: under a `[][]rune`
annotation the inner `[' '; WIDTH]` *infers* as a fixed `[WIDTH]rune`, which is copied per
slot, and only propagation widens it to the `[]rune` that every slot then shares — so a
check written inside inference would clear the exact program it exists for. When a check's
answer depends on what the backend will lower rather than on what inference first said, it
reads the `TypeTable` and lives here.

One ordering is load-bearing rather than incidental: **the generic instantiation set is
closed before the per-specialization ownership pass runs** (`instantiations.go`). A generic
body calling another generic records a *template* — bindings written in the enclosing body's
own type variables — and composing those into real specializations is what lets
`unwrap<t> = expect(self, …)` compile. Doing it later would leave the discovered
specializations with no ownership table of their own, falling back to the program-wide one;
that table is analyzed generically, where a type variable is not reference-counted, so a
`t = string` body would emit neither retains nor releases.
`typetable.Resolution.SpecKey()` is the one name for a specialization, shared by the symbol,
the emitted-method cache and that table.

**An instantiation carries the *site* it was requested from**, and that is a second module
rather than a detail. Lowering a specialization enters the **generic function's** module, so
the names in its own signature resolve; a **type argument** comes from the caller, and a
private declaration is keyed `<module>::<name>` — so one location cannot answer both, and
`Some(card).unwrap_or(x)` on a private `struct Card` failed as `unknown named type`.
`lookupNamedType` falls back to the site's key, *after* the current module's, so it can only
turn an error into a success. When a generic calls a generic the composed specialization
takes the **outer** instantiation's site, since that is where the substituted bindings were
resolved.

`driver.AnalyzeUnits(units)` is the multi-module form, with `Analyze` as its single-unit
case; both user-facing tools go through it, since both resolve an import graph first.
`Analyze` remains for a caller with a snippet and no file — a test, or an unsaved editor
buffer — and its units carry no file, which is why the LSP's per-file filtering treats an
empty file name as "this one".

`driver.ResolveEntryPoint(res)` (`entrypoint.go`) finds and validates the entry function: a
top-level `let main` that is a zero-parameter function returning `u8` (the process exit
code) or `void`/no annotation. `u8`, not a wider int — the OS truncates an exit code to its
low 8 bits regardless (even C's `return 300` exits 44), so a wider return type only adds the
silent-truncation surprise Lyra rejects elsewhere. It is a **build-time** requirement, so it
is intentionally not part of `Analyze` — only `lyrac build` calls it.

### `pkg/backend`

The seam between the front-end and code generation. `backend.Backend` is the interface a
code generator implements: `Name() string` and `Emit(res *driver.Result, entry
*driver.EntryPoint) ([]byte, error)`. `Emit` is called only after analysis is error-free and
the entry point resolves, so an implementation may assume a well-typed program.

`pkg/backend/llvm/tui.go` is **the only file that consults `runtime.GOOS`**, for one
constant (`TIOCGWINSZ` differs between the targets), and it is sound only because `lyrac`
compiles for its own host. The other terminal builtins avoid the question by going through
`cfmakeraw`, so `struct termios`'s genuinely-different layout is never indexed, only carried.

### `pkg/docgen`

`Collect(res, opts) []Module` builds the documentation model; `RenderMarkdown(m) []byte`
renders one module as a Starlight page. Backing `lyrac doc`.

The split is the design: nothing about Markdown reaches the model, so a terminal `go doc`
view or a JSON dump is a new renderer beside this one rather than a second walk of the AST
that can disagree with it about what a module contains.

**A page is organised by receiver, not alphabetically** (`pageSections`,
`Module.Partition`): types and traits first, then impls, then free functions, then one
section per `self` type — `## Methods on \`Maybe<t>\`` — and values last. That follows the
language rather than decorating it: with UFCS there is no separate method declaration, so
`self` is the only thing that says `trim` belongs to `string`. It also resolves
receiver-keyed overloads, which used to render as two adjacent `### unwrap_or` headings with
nothing to tell them apart.

Grouping keys on `types.HeadName` and displays `typeName` — **not the same string**:
HeadName is an identity never shown to a user, answering `boolean` for `bool` and `[]` for a
dynamic array. The borrow modifier is not part of a group either, or a type's methods would
split in two by whether each mutates. A generic receiver heads as nothing and stays a free
function, exactly as it cannot be an overload.

Two rules hold here and are easy to break:

- **Signatures are re-rendered from the AST, in source syntax.** Not sliced from the source
  text — a declaration's span runs to the end of its *body* — and not `Type.GetName()`,
  which is the diagnostic spelling. The page is read as the code to write, so a name on it
  the parser rejects is a broken promise: `DynamicArray<string>`/`[]string`,
  `boolean`/`bool`, `AnonymousTuple(a, b)`/`(a, b)`, and `ParameterizedType.GetName()`
  returning `Maybe` for `Maybe<t>` — a type that exists and is the wrong one.
  **The rule reaches members too**: a method's name on a page is `ast.MethodName.Key()`,
  never `GetName()`, which is the bare `Value` and erases *kind* — prefix `-` and binary `-`
  both render as `-` although they are different methods.
  **`TestSignature_RoundTripsThroughTheParser` is the guard that matters**: every generated
  signature is fed back through the parser, which is what caught `(mut self: Rng)` — the
  modifier binds to the type, after the colon — a spelling that looks entirely plausible on
  a page and does not compile. It covers `Decl.Signature`; a trait's `Members` need their
  own test.
- **A doc body is shifted before it is embedded.** A doc comment is written standalone, so
  its `# Panics` is an h1; nested under a declaration that breaks the outline and every
  table of contents built from it. `ast.ShiftHeadings` and `ast.TagBareFences` both go
  through `ast.walkDocLines`, the single fence tracker, so no consumer can come to a
  different conclusion about whether a `#` line is a heading or a comment inside an example.

### `pkg/printer`

Reflection-based AST printer used only in tests. `printer.PrintAST(program)` walks exported
struct fields; zero/nil/empty values are omitted. `printer.NewPrinter().Print(node)`
pretty-prints a raw tree-sitter CST node (useful for debugging).

### `cmd/lyra-lsp`

LSP server over stdio (`github.com/owenrumney/go-lsp`). On every `didOpen`/`didChange`:
apply incremental edits to an in-memory doc store; resolve the document's **import graph**
and run `driver.AnalyzeUnits` over the whole unit set (`units.go`), persisting the returned
`docAnalysis` for hover/definition/etc.; map this document's diagnostics to LSP and publish.

**The server analyzes a program, not a buffer** (`analyzeDocument`, `units.go`). Analyzing
the single open file is not a smaller version of the real thing but a *different program*:
it has no prelude, so `Maybe`, `Some`, `Ok` and every other standard-library name is
undefined in the editor on files `lyrac check` compiles cleanly. Roots and prelude selection
come from `modules.DefaultRoots`/`DefaultOptions`, so the server and `lyrac` cannot disagree
about where the standard library is.

Two things follow from being an editor rather than a compiler, and both are load-bearing:

- **The buffer is not the file.** Every open document is passed to the resolver as an
  `Options.Overlay`, so analysis sees unsaved text — including a file that has never been
  saved and has no on-disk content to read.
- **Only this document's half of the result may be used.** `diagnosticsFor` filters
  diagnostics by file (one naming none is kept — it is program-level and has nowhere else to
  go) and `docProgram` narrows the AST to this file's top-level statements. Every
  position-based handler walks that narrowed program: a line and column alone do not say
  which file they came from, so the prelude's line 40 would otherwise answer a request about
  the user's line 40. For the same reason a definition resolving into another file is
  returned against *that* file's URI (`locationIn`), and a rename whose declaration lives in
  another file is declined rather than applied at those coordinates in this buffer.

**Per keystroke the server re-resolves and re-analyzes the whole import graph**, and that —
not the user's file — is the cost. Measured on a small file with the standard prelude: 20.1 ms
total, of which the edited file is 0.09 ms. The other 99% is 11 prelude files that cannot have
changed.

Two caches address the half of that which is cacheable, and both are keyed on *content*:

- **`modules.Options.ParseCache`** (opt-in; the Handler owns one, `lyrac` passes nil) reuses a
  file's syntax tree when its bytes are unchanged — Resolve 8.4 ms → 1.7 ms. Keyed on bytes
  rather than path or mtime because the file is read either way and only the parse is skipped,
  so a stale tree is unreachable: a git checkout under a running server misses instead of
  serving the old parse.
- **`position.go`'s line index** makes a byte-column → UTF-16 conversion a slice read instead
  of a scan from the top of the file. A Range costs two conversions, so this was O(N·L) —
  93 ms for 2000 conversions over 5000 lines, now 0.18 ms.

The line index is keyed on the source's **data pointer and length, not its contents**: string
equality falls through to `runtime.memequal` over the whole text, which on a large file cost
more than the scan it replaced (94% of the profile). Equal pointer and length means the same
bytes, so a hit is sound; equal contents in distinct storage misses and pays one rebuild.

- **`driver.CollectCache`** (opt-in; the Handler owns one, `lyrac` passes nil) reuses the
  *collection* of every unit but the last — collection is ~75% of analysis and folds all 12
  units into one Program and SymbolTable every time. End to end: **19.9 ms → 4.9 ms per
  keystroke**.

Reuse is by **clone, not restore**: `SymbolTable.Clone` copies the table's own state and the
master is never mutated, so each keystroke starts from a fresh copy. A copy has to be right
once; an undo has to be right every time, and a slightly wrong undo is analysis that drifts as
a session runs. `TestClone_MentionsEverySymbolTableField` parses the clone's literal and fails
if a field is added without being copied — a shared field leaks between keystrokes and is
reported nowhere.

**The AST is shared, and that is checked rather than assumed.** It cannot be copied —
ScopeTable, TypeTable and MethodTable are all keyed by AST pointer — and it *is* mutated after
collection: `desugarClauses` replaces a multi-clause body with a match and clears the clauses.
What makes sharing safe is that re-analyzing one collected AST is idempotent, which
`TestZZDiff`-style re-analysis over the real prelude establishes directly. Note the shape of
that finding: reading the code said "blocker", and the code reading was right about the
mutation and wrong about its consequence.

The snapshot's key covers the prelude path and the **import graph** as well as the prefix's
bytes, because `SetPreludeModule` and `SetImports` are applied before the first file is walked
and both change how a declaration is keyed. Editing an `import` line invalidates the snapshot,
which is correct: it changes how every name in the program resolves.

A file's **imports** are extracted once at load and cached beside its tree (`Unit.Imports`),
because two passes want them — resolution follows them, the driver builds the import graph
from them — and each used to walk the CST for itself.

- **`typechecker.Snapshot`** carries the typechecking of that same prefix, in the same cache
  and under the same key — two caches keyed identically are two chances to invalidate one and
  not the other. It holds the four output tables and the error list and nothing else: `Check`
  is four whole-program setup passes and then a per-statement loop, the setup is under 1% and
  re-runs, and the loop is where the ~1.0 ms goes. The prefix can be skipped because the
  prelude cannot see user code.

Per keystroke, end to end: **17.3 ms with no cache, 2.68 ms with all of them.**

`Finish` is 3% of a cached run, worth recording because it was predicted to be the bottleneck
and was not — the import-graph rebuild was, and it was duplicated work rather than analysis.
What is left is purity, ownership and shadowing, each whole-program, with no duplicated work
and no CGO in the path.

Logs to `/tmp/lyra-lsp.log`. Build with `go build ./cmd/lyra-lsp`.

### `cmd/lyrac`

Compiler CLI, built on `pkg/driver`. `lyrac check <file>` (parse + typecheck, exit 1 on any
error), `lyrac build <file>` (check, resolve the entry point, hand the typed program to the
backend, link an executable), `lyrac run <file>` (build into a temp directory and execute)
and `lyrac doc <file>` (render the module's documentation as Markdown). Diagnostics print as
`path:line:col: severity[code]: message`, the `line:col` omitted for a program-level error
with no location.

`build` emits IR to a temp file and links with `clang <ir> -lm -o <exe>`, so the default
artifact is `<name>` beside the source, not `<name>.ll`. The `-lm` is unconditional,
matching what the backend's behavioural tests compile with.

```bash
lyrac build prog.lyra                 # -> ./prog, no IR left behind
lyrac build -o build/prog prog.lyra   # executable elsewhere
lyrac build --keep-ll prog.lyra       # executable *and* prog.ll
lyrac build --emit-llvm prog.lyra     # prog.ll only; the one build needing no C compiler
lyrac build -O0 prog.lyra             # optimization level; default -O2
lyrac build --cc /path/to/clang …     # else $LYRA_CC, else clang on PATH
```

**The default is `-O2`, not clang's `-O0`**, because this compiler does not face the usual
tradeoff: it emits **no debug info at any level**, so shipping unoptimized buys no
debuggability — only build time. `-O0` costs about 3x on ordinary code for roughly 50 ms of
extra link time on a 2000-line module, and the whole backend suite passes at `-O1`, `-O2`,
`-O3` and `-Os`. The level is matched loosely (`-O` plus anything) and passed through
unexamined, so an unknown one is clang's error to report in its own words rather than a
staler copy of clang's list kept here. Both "compile it with" hints carry the level, or they
would describe a different build than the one they stand in for.

The compiler must accept a `.ll` as input, so plain `cc` is deliberately not a fallback —
gcc would reject the IR with a confusing error instead of a clear one. When none is found
the build fails (exit 1) but **writes `<name>.ll` next to the source anyway** and prints the
`clang` line: that IR is all the user has to compile once they install one.

`run` is that same pipeline with every artifact in a temp directory
(`buildOptions.ephemeral`), then `exec` with the child inheriting stdio. Two consequences to
keep: **it prints no build summary** (so `lowerAndEmit` returns the executable's path and
leaves reporting to its caller — `lyrac run prog.lyra | grep …` should see the program's
output, not the compiler's), and **the program's exit status is the command's**, so an exit
1 from a program is indistinguishable from a compile failure, the same trade `go run` makes.
`ephemeral` also suppresses the missing-compiler `.ll` fallback, since the temp path it
would name is deleted by the time the message is read. `-o`/`--emit-llvm`/`--keep-ll` are
refused for `run` rather than ignored.

`doc` renders one Markdown page per module into `-o` (default `./docs`), with Starlight
frontmatter so it drops into `lyra-website/src/content/docs/reference/`. `--private`
includes unexported declarations, `--deps` follows imports, `--prelude` adds the standard
library (implies `--deps`), `--strict` exits non-zero on a gap. Four decisions in it, each
of which had an obvious wrong answer:

- **It refuses a program that does not type-check.** A signature is rendered from resolved
  types, so documenting a broken program prints `?` where a type failed to resolve and
  publishes it as though it were the API.
- **An undocumented public declaration is listed anyway**, with its signature. Dropping it
  makes the page silently misrepresent the module's surface. Coverage prints on *every* run.
- **The prelude needs its own opt-in even under `--deps`**, or every project's docs contain
  a copy of the standard library. It is still documented when it *is* the entry module.
- **An impl's methods are not counted as gaps.** The contract lives on the trait; an impl
  method's doc says what *this* implementation does differently, so having none is usually
  correct rather than missing.

The pages are `std-prelude.md`, not `std.prelude.md`: a site generator derives a URL slug
from the file name and strips dots, so the dotted form publishes at `/reference/stdprelude/`.
The page's title is still the real dotted path.

Codegen is pre-release but no longer minimal — closures, generics, strings, arrays, `match`,
traits, `?` and Perceus all lower; that package's README is the current inventory, and
`todo.md` the gaps. A form that does not lower yet is a hard error, so a non-trivial `main`
may still hit one rather than being lowered incorrectly. Build with `go build ./cmd/lyrac`.

## Building

```bash
./build.sh          # build/{lyrac,lyra-lsp} with std -> ../std
```

The binaries go in `build/` with `std` beside them, because that is where `lyrac` looks for
the standard library: the directory containing its own executable, or wherever `LYRA_STD`
points. It is the beside-the-executable convention Rust, Zig and Go use for a sysroot, and
building this way means the resolution path is exercised daily rather than only at release.

Two details that are easy to get wrong and were:

- **The root is the directory *containing* `std/`, not `std/` itself.** A module path
  resolves beneath a root, so `std.prelude` is `<root>/std/prelude/`; returning the `std`
  directory looked for `std/std/prelude` and silently found no prelude.
- **`build/std` is a symlink, not a copy.** A copy drifts: you would edit
  `std/prelude/maybe.lyra`, rebuild, and still get the old prelude. A real install would
  copy; development must not.

`stdRoot` resolves symlinks before taking the executable's directory, since `os.Executable`
does not do so consistently (Linux reads the already-resolved `/proc/self/exe`; macOS can
return the link's own path). Without it, a compiler symlinked onto `PATH` looks for the
library beside the *link*.

`build/` is gitignored as a directory rather than binary-by-binary, so a new command cannot
land in the source tree unnoticed, and a stale compiler is one `rm -rf build` away. The VS
Code extension's `lyra.languageServerPath` should point at `build/lyra-lsp`.

The standard library's sources live in `std/` and are tracked. The prelude is `std/prelude/`,
**one module across several files** — `std/prelude/README.md` documents the constraints on
what may go in it and why the split is within a module rather than into several.

## Testing

### Collector golden tests (`pkg/analyzer/collector/tests/`)

```bash
go test ./pkg/analyzer/collector/tests/...                  # run golden tests
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...  # regenerate .golden files
```

```go
func TestSomething(t *testing.T) {
    source := `let x = 42`
    runGoldenTest(t, source, "golden_file_name")  // no extension
}
```

Golden files live in `testdata/*.golden`. First run with a new file creates it and fails;
re-run to confirm. The printer omits zero/nil/empty fields, so only populated fields appear.
`parseAndCollect(t, source)` is the lower-level helper when you want `program` and `table`
directly without a golden file.

### Typechecker assertion tests (`pkg/analyzer/typechecker/tests/`)

```go
res := parseCollectAndCheck(t, source, false)
assertNoErrors(t, res)
// or
assertErrorsAre(t, res, "expected error message 1", "expected error message 2")
```

`res` exposes `res.program`, `res.symTable`, `res.typeTable` and `res.errors`.

### Backend behavioural tests (`pkg/backend/llvm/`)

They compile emitted IR with clang and run it. Two things to know before touching them —
the `sanitize_address` attribute (rule 6) and the binary cache that keeps the package at
~2s warm. Both are explained in `pkg/backend/llvm/README.md`.

Linux runs go through the workspace's `./asan.sh`, worth doing before pushing memory-model
work: Debian's older clang uses *typed pointers* and so rejects IR type mismatches that
Apple clang's opaque pointers cannot even represent.

### Running all tests

```bash
go test ./...
go test -run TestFunctionName ./pkg/...
```

**A test file's *name* can silently exclude it.** Go applies an implicit build constraint
from a filename's last underscore-separated segment when that segment is a GOOS or GOARCH,
so `match_unreachable_arm_test.go` is ARM-only — on arm64 it is not compiled, and
`go test ./...` prints `ok` with every test in it missing. The failure mode is the bad one:
a test that never runs looks exactly like a test that passes. `arm`, `ios`, `js`, `plan9`,
`android`, `wasm`, `mips`, `s390x` and `windows` are all plausible endings for a test about
a *match arm*, a JS target, or Windows paths.

`go list -f '{{.IgnoredGoFiles}}' ./...` names anything being skipped, and a non-empty
answer on this repo is a bug — nothing here is meant to be platform-gated by filename. Worth
running after adding a test file whose name ends in a word that could be a platform.

## Foreign functions

**Built, front to back**: an `extern` declares, type-checks, is charged effects, lowers to
a `declare`, and `@link` reaches the link line. Four things about the shape of it, then
the language rules:

- **`ExternDeclStmt.Func()` is the body-less function an extern *is*.** Registered in
  `SymbolTable.Functions`, so a call resolves, type-checks and is charged effects by the
  machinery every other call goes through — the arrangement `TraitMethod.DefaultImpl()`
  has, for the same reason. `LambdaExpr.IsExtern` marks it, because two passes must not
  read it as an ordinary lambda that happens to be empty: the purity fixpoint would charge
  no effect and call a foreign function *pure*, and the backend would emit a `define` with
  no blocks.
- **An extern's effects are its bound's** (`externEffects`), defaulting to `AllEffects` —
  the same conservatism the unresolved-callee rule already encodes.
- **`lyra-E011`'s "calling an unsafe function" half is in the typechecker**, not in the
  syntactic pass that owns the raw-pointer half. That pass could only match the callee's
  *name*, and a name does not identify a declaration (rule 9) — an `extern f` made every
  `f(…)` in the prelude report as an unsafe call.
- **An extern is private to its module and its symbol is global**, which are not in
  tension — they are the two halves of one arrangement. There is no `pub extern`
  (`declIsPublic` returns false for one), so two modules may each declare `strlen`, which
  is what two libraries *using* `strlen` looks like; the backend keys `l.externs` by the C
  symbol and emits one `declare`. What a module exports is the Lyra wrapper it puts over
  an extern, which is the whole division of labour `std.ffi` rests on.

**An extern names its parameters** (`lyra-E067`, 08/26): `(dest: ^mut u8, destLen: ^mut
CULong, source: ^u8, sourceLen: CULong)`. The argument is that an extern is a *declaration*
standing in for a C prototype, not a type — it substitutes for a `let`, and a `let` names its
parameters — and the boundary is where a positional mistake links cleanly and computes
garbage. The information exists in the header being transcribed, and was being pasted into a
doc comment beside a signature that could not carry it. A plain function *type* is refused
the other way, since a shape has no parameters to name; a **callback's own** signature is a
type, so its parameters stay unnamed even inside an extern.

**The name is documentation the compiler cannot check** — nothing compares it to the header,
so a wrong name is as silent as none. What it buys is a transcription a reader can verify by
eye, and `argument 2 (destLen)` where the numbered fallback said `argument 2 (arg1)`.

**Integer widths at the boundary are Lyra's fixed ones**, with no C-shaped aliases: the
compiler already hardcodes LP64 in three places — `layout.go`'s `pointerSize`, `clock.go`'s
`struct timespec` as `[2 x i64]` (a C `long` written as `i64`, in a shipped builtin), and
`i128`'s 16/16 ABI — so `extern` inherits that commitment rather than making it.
`pointerSize` is where the assumption lives; everything else should reference it.

| C | Lyra | C | Lyra |
|---|---|---|---|
| `char` | `i8` | `long`, `long long` | `i64` (`CLong` for `long`) |
| `unsigned char` | `u8` | `unsigned long` | `u64` (`CULong`) |
| `short` | `i16` | `size_t`, `uintptr_t` | `u64` |
| `int` | `i32` | `float` | `f32` |
| `unsigned int` | `u32` | `double` | `f64` |
| `void` | `void` | `T*`, `void*` | `^T` / `^u8` |
| `...` | `...` (extern only) | | |

**`...` declares a C variadic** — `unsafe extern printf: (^u8, ...) -> i32` — and it is the
only place the marker is legal (`lyra-E065`), because **Lyra has no variadic functions of
its own**. Two features share the spelling and only one is needed: calling a C variadic
requires nothing from the language, since every argument is known at the call site, while
defining one would need an argument pack nothing else here would use.

Three rules, of which the third is the reason the feature exists:

- **The arity gains a floor, not a ceiling.** C needs the named parameters (they are how a
  `va_list` starts), so `(...)` alone is refused and `...` must come last.
- **A variadic argument is still FFI-safe or nothing.** `...` widens the arity, not the set
  of types that cross.
- **The compiler owes C's default argument promotions.** An integer narrower than `int`
  widens to `int` — *signed by the Lyra type*, since an i16 and a u16 are the same `i16` in
  the IR — and a `float` widens to `double`. `checkVariadicArguments` decides them and
  publishes them (`TypeTable.VariadicPromotion`); the backend emits the sext/zext/fpext and
  keeps no table of its own.

Declaring one at **fixed arity** is what this replaces, and it was the worst failure the FFI
had: it compiled, linked, and printed garbage, because Apple aarch64 passes variadic
arguments on the stack while the fixed convention passes them in registers. Named arguments
were fine; only those landing in the `...` part were wrong.

**This makes the ABI right, not the call safe.** A format-string mismatch is undetectable
without parsing format strings, which would be a second language embedded in this one;
`unsafe` already covers that claim.

`_Bool` is absent deliberately (`lyra-E063`): Lyra's `bool` is one bit and C's is a byte.
So is a borrow modifier — `mut`/`ref` is Lyra's own by-reference passing, which at the
boundary is either inert or an ABI mismatch. `long` is the one type that moves off LP64
(Windows x64 is LLP64: 64-bit pointers, 32-bit `long`), which is what `CLong`/`CULong`
exist to make a grep target rather than an audit.

The design is **settled** in `todo.md` (Foreign functions — `extern`); the summary a reader
needs before touching anything nearby:

- **An extern carries `AllEffects` unless a bound is written, and writing one is `unsafe`.**
  For Lyra code a bound is a promise the compiler checks; for an extern it is a promise the
  compiler *records*, so the keyword marks the unverifiable claim. Declaring is safe;
  narrowing is not. Calling one needs an `unsafe` block, which `lyra-E011` already covers.
- **Only FFI-safe types cross**: the scalars, `^T`, `void`. `string`, `[]T`, closures,
  tuples, `data` types and anything `shared` are refused at the signature, so there is no
  implicit conversion and therefore no nul-termination policy to get wrong. `std.ffi`
  supplies `s.cstring()` and `xs.data()` as ordinary Lyra.
- **A struct is refused on an ABI ground, not a layout one** (08/26), and E063's hint says
  so. The layouts match — the fixture proves `sizeof` and every `offsetof` — so `^T` is the
  right shape rather than a workaround. By *value* needs a per-target classifier LLVM does
  not supply: aarch64 coalesces a ≤16-byte struct into registers while x86-64 SysV
  classifies it per eightbyte and can change the parameter count. The table is in
  `todo.md`; do not reopen without one for both ABIs.
- **A callback crosses as a bare function pointer, and only a top-level function is one**
  (08/26). A function type in an extern's *parameter* position is C's `int (*)(…)`, with
  every type in its own signature checked by the same FFI-safe predicate; in *return*
  position it is still refused, since a bare code address is not a Lyra closure and could
  not be called. It works on a coincidence worth knowing: `declareFunctionAs` lowers a
  function's parameters directly, with no environment word, so `@lyra.main.cmp(i8*, i8*)`
  *is* the C signature. A closure is `{code, env}` and has no such word — `lyra-E066`
  refuses one, including a **local binding that shadows a top-level function**, which the
  backend would otherwise resolve by name to the wrong symbol. What a capture would have
  carried travels through the callback's own `void *` context instead, as a `^u8`.
  `pushExternSignature` is what makes `lowerType` read a function type as C's for the
  duration of one declaration; the call site recognises the slot by its lowered type.
- **Ownership never crosses.** Neither side adopts the other's buffer; both directions
  would need the other to understand the rc header. A `^T` into a live array dangles at the
  next `push`.
- **A link requirement rides the extern that needs it** — `@link("m")` on the declaration,
  collected across every module in the compile, sorted and deduplicated, emitted as `-l`
  (`lyrac`'s `linkFlags`, which every "compile with" hint prints too). Not a CLI flag (a
  module's requirement would not compose) and not a manifest (this compiler has
  deliberately never had one). It needs no `unsafe`: a wrong library name fails loudly at
  link time, which is exactly what an effect bound does not do.

**`std.ffi` is `CBuffer`/`get`/`cstring_len`/`decode_utf8`/`cstring`/`with_cstring`/
`with_cstrings`/`data`/`data_mut`/`CLong`/`CULong`** — complete as of 08/22, and every
piece of it ordinary Lyra over the primitives.
`cstring` is the out direction for a string and is a plain `[]u8` — option A, chosen over a
`CString` type because the dangling shape is already `lyra-E059`, because a struct storing
the pointer dangles for real on the next `push` (measured), and because the wrapper that
would help is the scoped `with_cstring`, not a name. It traps on an interior NUL.

**`xs.data()`/`xs.data_mut()` are the out direction for a buffer**, and they copy nothing:
a `[]T`'s elements already live behind a contiguous `T*` in its box, so the pointer C wants
is the one Lyra is holding. **Two functions, because `&x` and `&mut x` are two spellings**
and a method call has nowhere to put the word — `data_mut` takes a `mut` receiver, the rule
`push` and `xs[i] = v` already follow. Both are `unsafe pure noalloc`, both trap on an empty
array with their own message rather than the index check's, and both are **dynamic arrays
only**: a `[N]T` carries its size in its type, so it cannot be a generic parameter until
const generics exist.

**A C buffer comes back in one copy** (08/26): `p.decode_utf8(byte_len)` is the `^u8`
spelling of the array method — the same operation on memory Lyra does *not* own, which is
the only kind a raw pointer can address. `std.ffi`'s `CBuffer.decode_utf8` is one line over
it, where it used to be a bounds-checked `get` and a capacity-checked `push` per byte into a
`[]u8` and then that array's own copy: **548 µs → 256 µs** over 400 KB, against 254 µs for
the array builtin, which is the floor (one memcpy, one count pass). The length is an
argument because a pointer carries none, which is also why it is `unsafe`; a negative one
traps, and a too-large one cannot be caught at all.

**A string crosses without a copy** (08/26): every string carries a NUL past its own bytes
(`pkg/backend/llvm/STRING_LAYOUT.md`), so `s.cstring_ptr()` — an `unsafe` builtin — checks
for an interior NUL with one `memchr` and yields `data` itself. `with_cstring` is one line
over it and is `pure noalloc`; measured at **146 ns → 8 ns** per crossing. `cstring()` still
copies, because it hands out an owned `[]u8` the caller keeps. The pointer is a `^u8` and a
*literal*'s bytes live in a read-only global, so a C function that writes through one
faults — which is what turned a latent UB in the `strtoul` FFI test (passing `p` for its
`char**` endptr) into a visible segfault.

**`with_cstring(s, f)` is the scoped string form, and it is deliberately *not* marked
`unsafe`.** The rule it establishes: **`unsafe` marks handing a pointer out to keep
(`data`), not lending one for the duration of a call.** Marking it would have made the
safer shape the more ceremonious one, since `unsafe` does not cross a lambda boundary — an
outer block for the call plus an inner one for the foreign call, against the *one* block
the unscoped `cstring()` spelling costs. The scope is not a lifetime, so
`s.with_cstring((p) => p)` still compiles; what holds is that the escaped pointer cannot be
used without `unsafe` at the use site. `with_cstrings(a, b, f)` is the flat two-string
form — a free function, because neither string is the receiver.

**`CLong`/`CULong` are `pub type` aliases, not newtypes.** They name the one C type whose
width moves between LP64 and Windows' LLP64. An alias, because changing this one line to
`i32` already fails every site that passes an `i64`, naming `CLong` in the message —
nominal identity would tax every crossing on every target to prevent a mixup that is
either harmless or already a width error.

**Both directions work.** A buffer goes *out* as `xs.data_mut()` plus a length, which is
what zlib's `compress` takes; a `^u8` coming *back* is read through `p.offset(n)^`, and
`std.ffi`'s `CBuffer` is the checked wrapper over it (see "Raw pointers" above).

**The boundary is tested in two layers, and the split is deliberate.** `llvm_extern_test.go`
calls **libc and libm**, because a library nobody wrote for Lyra is the thing most worth
proving and those need no package on either platform;
`pkg/backend/llvm/testdata/ffi_fixture.c` covers what libc's i32/i64/f64-and-pointers
surface cannot reach — the narrow widths, `float`, mixed register classes, a spilling
argument list, `CLong`/`CULong`, out-parameters, a struct by pointer, `data()`/`data_mut()`.
What they have in common is that **each links cleanly when it is wrong**, so the failure is
a wrong answer rather than a build error. Two rules if you add to it: the expected value
must come from `testdata/ffi_oracle.c`, a pure-C caller, since a value read off Lyra's own
output asserts only that Lyra agrees with itself; and the compile cache must stay salted
with the fixture's bytes, since its key otherwise names the *path*.

**And zlib is the third layer, run rather than only compiled** (08/26):
`TestExample_ZlibRoundTrips` builds and runs `examples/zlib.lyra` through the **CLI**, since
the backend harness hardcodes `-lm` and `@link("z")` reaching `-lz` is part of what it
proves. It self-skips when zlib is absent, which is safe only because `asan.Dockerfile` and
the CI workflow both install `zlib1g-dev` — the reason is written beside each `apt-get` line,
since a package whose purpose is undocumented is one a future cleanup removes.

**A libc function that Lyra can express is written in Lyra, not bound.** `cstring_len` is
`strlen` in prelude-style code, because scanning for a zero byte stopped needing C the
moment `offset` existed — the `read_line`/`parse_i64` division, applied at the boundary.
There is deliberately **no `std.libc`**: an extern cannot be exported anyway, and a shared
bindings module re-creates the libc shim layer FFI was built to dissolve. The shape that
works is a per-library binding module owning its own externs, which needs nothing new.

## Current Development Focus

The typechecker is the active area — match exhaustiveness (see
`pkg/analyzer/typechecker/README.md`) and the FP/imperative purity work (see
`pkg/analyzer/checker/README.md`).

`todo.md` is the **open** backlog; `COMPLETED.md` is the dated record of what landed and
why — the constraint that forced a design, the measurement that disproved a diagnosis. An
item citing "the Completed entry" means that file.

One module-system hazard worth carrying here, because it is a *timing* variant of rule 8:
exports are recorded per **file**, so a name that becomes overloaded only in a later file of
a multi-file module has already been exported as a bare declaration, and the set built when
the second file is walked collides with it (`symbol "area" already defined`).
`exportToGlobal` lets a set supersede a global binding that is one of its own members. Two
things had hidden it — within one file the merge happens before either member exports, so
both export the same set object; and the prelude branch of that function discards
duplicate-definition errors, so the shipped prelude worked while a user module doing the
same thing did not.
