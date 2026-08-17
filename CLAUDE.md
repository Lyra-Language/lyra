# lyra (Go) — Project Context

This is the main compiler infrastructure for the Lyra programming language. It contains the
parser, AST, type system, collector, typechecker, a standalone semantic checker, and the LSP
server.

Go module: `github.com/Lyra-Language/lyra`

**This file is a map.** Each package's depth lives in a `README.md` beside its code; the
sections here say what a package is, and where to read further. What must be obeyed
regardless of which package you are in is under [Rules and hazards](#rules-and-hazards) —
read that first.

Open work is in `todo.md`; finished work, with the reasoning behind it, in `COMPLETED.md`.

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

Violating any of these produces something that looks like it works. They are collected here
because each was learned from a real failure, and none is local to one package.

1. **After changing `tree-sitter-lyra/grammar.js`: regenerate, then `go clean -cache`,
   then test.** Go's build cache does not hash `#include`d sources, so the compiled C
   parser goes stale and the suite silently runs against the *old* grammar. Push the
   grammar repo before the `lyra` code that depends on it — CI regenerates from the remote.

2. **Never call an accessor on the result of an optional field lookup without a nil check.**
   `node.ChildByFieldName(…)` returns a genuine Go `nil` `*sitter.Node` for an absent
   optional grammar field, and calling `ChildCount`/`Child`/`Kind` on it **hangs inside the
   go-tree-sitter CGO binding instead of panicking** — it silently froze the whole collector
   once. See `pkg/analyzer/collector/README.md`.

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
   elsewhere (the prelude's, or one exported by a module it imports — `shadowsAmbient`,
   08/08) — so *which* accessor you use is a correctness question, not a style one. Prefer
   `LookupTypeFrom`/`LookupTraitFrom`/`LookupFunctionFrom(name, loc)`, which resolve as the
   file at `loc` sees it; bare `LookupType(name)` answers only for a program-wide name, and
   asking it from inside a module that declares its own returns *another* module's
   declaration. Two corollaries that have already bitten: a map key is **not** a source
   name, so anything user-facing (an LSP completion label, a "declared names" set) must read
   `decl.Name` rather than the key; and a `pub` check must ask about the declaration a
   reference *resolved to* (`declVisibility`), never look one up by name — `DeclaringModule`
   is last-writer-wins, and reported a module's own type as private to another module that
   happened to declare the same name.

   **A third landed 08/14, and it is the by-name form applied to a *trait*.**
   `checkImplCoherence` keyed duplicate impls on `{impl.TraitName, target}`, so a
   program's own `trait Add` plus `impl Add for i64` was reported as a duplicate of the
   prelude's — refusing a correct program. Keying on the resolved `*ast.TraitDeclStmt`
   (via `LookupTraitFrom`) is the fix, and it is what dispatch already does one function
   over, its comment recording that filtering by name is what let a user's own
   `trait Ord` be taken for the prelude's. Reachable since the prelude first shipped
   `impl Show for i64`; latent until the prelude gained arithmetic impls for every
   numeric width and a test that declares its own `Add` started failing.

   **The by-name form keeps coming back wherever a module is not in hand**, and two more
   were found 08/08 when a module became able to declare its own version of an imported
   name: `visibilityIn` fell through to `BindingOf(name)` — a `pub` check routed through
   `ModuleOf` after all — and reported an *exported* function as private to its own module
   (`BindingIn(module, name)` is the fix, the binding half of `LookupTypeIn`); and the
   backend's `namespaceCallee` tested membership with `DeclaringModule` and read
   `l.funcs[name]`, so `seq.map(…)` fell out of the path and died as
   `llvm: unsupported method call` on a program the front end had checked clean. Both had
   been reachable since 07/30 by shadowing a *prelude* name and then calling through a
   namespace; nothing did both at once. When a premise like "a top-level name is
   program-wide unique" is written into a comment, it is a bug waiting for the feature that
   retires it.

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
   bitten eight times, in seven different switches, and never looked like what it was:
   `mentionsTypeVar` missing `ParameterizedType` (a generic function emitted under its
   bare name, failing in layout); `resolveType` missing `*LambdaType` and
   `ParameterizedType` (assignability rejecting a type against *itself* — "cannot assign
   `Box<Pt>` to `Box<Pt>`"); **`resolveTypeIfKnown` — `resolveType`'s twin — missing the
   same two** (08/03: the identical self-rejection, but only in *return* position, because
   that is the one thing the twin resolves: "expected `Maybe<weak Node>`, got
   `Maybe<weak Node>`"); `resolveForLayout` missing `ParameterizedType` (08/03: a `shared`
   struct with a generic field could not be sized — "cannot size a `shared Node` payload
   yet" — which read as a `weak` bug because `Maybe<weak T>` was the case being built);
   and `ast.walkExprChildren` missing `*TupleIndexExpr`, which
   made every pass on the shared walker blind to anything reached through `p.0` — `pure`
   silently accepted an impure `noisy().0`, a closure capturing `p` only as `p.0` failed
   to lower, and two "never used" warnings fired on names plainly used. When adding a node
   kind or a composite type, grep for the switches over it; when fixing one, check the
   others in the same file, since these travel in pairs.

   **A ninth and tenth landed 08/17, and they are one bug in two places plus its
   front-end cousin.** "Produced no value" has **two spellings** in the backend — a nil,
   and an `ir.Call` whose LLVM type is void — and both branch merges (`lowerIfExpr` in
   control_flow.go, `matchMerge.arm` in match.go) tested only the nil. So
   `if b { f() } else { g() }` over two user-defined `void` functions emitted `phi void`
   and clang refused the module: six ordinary lines that did not compile. Both guards'
   comments already anticipated void branches; neither anticipated that one of them is
   not nil, which is the fifth instance's lesson again — a shared *assumption* fails
   silently rather than diverging. `isVoidResult` is now the one predicate, and the twins
   were fixed together because a partial fix leaves a phi well-formed on one construct
   and not the other.

   The front-end cousin, same day: `checkMatchExpr` had no `requireType` flag, so a
   match's arms were always checked in *value* position and a one-armed `if` as an arm
   body was refused. `checkIfExpr` had taken that flag all along — but only for the
   *mismatch* check, still inferring both branches as values, so the identical refusal
   applied one construct over (found the next day, writing a polling loop).

   **That family had five members and now has one answer.** `checkExprForEffect` — the
   value-optional twin of `inferExprType` — is what `checkBlockForEffect` (loop bodies),
   `checkBlock`'s tail (lyra-W006, 08/15), `checkMatchExpr(false)` and `checkIfExpr(false)`
   all use, and it subsumed a hand-rolled else-if special case that had propagated
   statement context only through a *bare* `else if` (a braced `else { if … }` still put
   its tail in value position). `checkLValueAssignment`'s missing context (08/15) is the
   same question asked about a value rather than a tail. The durable fix here was hazard
   8's own advice — stop having more than one of it — rather than a fifth copy that
   happens to agree today; **anything new that must decide whether a tail is a value or a
   statement should call `checkExprForEffect`, not re-derive it.**

   Two more landed 08/05, both in the backend and both found by *reviewing* for duplication
   rather than by hitting them: `resolveStructType`/`resolveTupleType` missing the
   `ParameterizedType` arm their sibling `resolveDataType` had been given (a nested generic
   struct sub-pattern failing with "struct pattern on non-struct value of type `Box<i64>`",
   the same sentence as the data case with one noun changed), and `lowerIndirectCall`
   missing the `diverged` argument guard `lowerDirectCall` had — a `panic(…)` argument
   through a function value segfaulted the compiler inside llir. The lesson those two add
   is that a *second* copy is enough: neither had three.

   **A third instance landed 08/05, and it had *three* copies rather than two.** The
   purity pass asks "what does this call call?" in three places — `lambdaEffects`,
   `methodEffects`, and the reporting walk in `checkCallPurity` — each a ladder over
   `MethodTable.Get` → `GetBound` → the name. None had an arm for a **builtin method**, so
   `x.wrapping_mul(y)` fell through all three to the unresolved-callee default
   (`AllEffects`) and was charged as reading input *and* allocating. The symptom was remote
   in the usual way: not "wrapping_mul is broken" but "the prelude's PRNG cannot be marked
   `det`". Fixing two of the three would have been worse than fixing none — a call charged
   no effect by the inference while still reported as impure by the walk.

   **A fourth landed 08/06, and it is the "two helpers, one question" variant.** An arm
   body was lowered at four sites through `lowerExpr`, which requires a block value, while
   `if` branches went through `lowerBranchValue`, which does not — so a `match` used as a
   *statement* (arms ending in an assignment) failed to lower while the identical `if` had
   always worked. Two helpers answering "lower this body, value optional", and only one of
   them reaching the arms. Same lesson as the missing switch case: when a question has more
   than one answer in the tree, the copies drift, and here the drift was old enough that the
   feature simply looked unimplemented.

   **A fifth landed 08/06, in the same three-copy ladder as the third, and it shows the
   copies do not even have to drift to be wrong — they can agree, and be wrong together.**
   All three of `lambdaEffects`, `methodEffects` and `checkCallPurity` treat a builtin
   method as carrying no effect, which is the *fix* from 08/05 and is right for every
   builtin that is arithmetic over a scalar. `s.slice(a, b)` is the first that allocates,
   so it was invisible to all three at once and `pure noalloc … => s.trim()` type-checked
   clean — a bound that silently stops binding, which is worse than no bound. The lesson
   the fourth instance does not carry: a shared answer is a shared *assumption*, and it
   fails the moment one case violates it, with no divergence to notice. The fix follows
   rule 9 rather than adding a fourth name test — the typechecker records whether the
   resolved builtin allocates (`MethodTable.SetBuiltinMethod(call, allocates)`), since only
   it still has the receiver's type, and all three ladders read the flag.

   **A sixth landed 08/07, and it is the fifth's twin one layer down.** The backend's two
   aggregate glue walks — `emitRetainValue` and `emitDropValue` — switch on the resolved
   type, and *neither* had a `ParameterizedType` arm, so a tuple or struct holding a
   `Maybe<string>` walked past that element entirely. `OwnsManaged` had been given exactly
   that arm long before, its comment recording that missing it was "a real double free";
   these are the other half of the same model. Symmetric-in-being-broken hid it: the element
   is retained at the *construction* site (where the type arrives substituted via
   `recordedType`), so only the drop was missing, and the result was a quiet leak — one
   reference per call to any **multi-clause** function, which desugars to
   `match (p0, p1) { … }` over a stack tuple. Two lessons beyond "add the arm": the fix must
   land in **both** walks in one change (drop-without-retain is an instant double free, which
   `TestExec_WeakOptionalField` caught), and that test had itself been green *by leaking* —
   before the glue walked a `Maybe<shared T>` field at all, the cycle it builds released
   nothing. A memory-safety test can pass because the code under it does nothing.

   **An eighth landed 08/08, and it is the "list of syntax, not a switch over types"
   variant.** The allocation walk in the purity pass names the allocating *forms* —
   `ArrayLiteralExpr`, `ArrayCompExpr`, `StringConcatExpr` — and `[0; 5]`
   (`ArrayRepeatExpr`) was not among them when the form was implemented, so
   `noalloc … => { let d: []i64 = [0; 3]; … }` type-checked clean while the identical
   `[1, 2, 3]` was refused. It survived adding the new node to the typechecker, the
   backend, the ownership pass and the reclassification walk, because it is not a switch
   over *types* and grepping for the type name is what one does. The lesson: when adding an
   expression kind, also grep for the kind it is a variant **of** — here `ArrayLiteralExpr`,
   which appeared in five places the new node needed to appear in too.

   **A seventh landed 08/07, and its lesson is that a switch can be wrong about a type the
   file above it already classified correctly.** `nominalHead` — the unifier's "is this a
   named type, and which?" — had arms for `ParameterizedType`, `NamedStructType`,
   `DataType` and `UnresolvedType` but not `*ConstrainedType`, while `types.HeadName` one
   layer up gives a newtype a head *and writes down why*. So `receiverAccepts` compared a
   `newtype Name = string` receiver against a declared `self: Name` (an `UnresolvedType` at
   that point), fell through to `TypesEqual`, and never matched — a method written **for** a
   newtype was silently unreachable. Nothing was reported; the call simply took the next
   rung, so the symptom was "member access on non-struct type Name" pointing at code that
   was correct. Grepping for the switches would not have found it either: the two disagree
   about a case *neither one names*, which is why the durable fix below is to have one.

   The durable fix for a switch with more than one caller is to stop having more than one
   of it. The type-variable walk was three switches (typechecker `collectTypeVars`, backend
   `mentionsTypeVar`, and the generic-parameter-list check that wanted a third); it is now
   one, `types.CollectTypeVars` in `pkg/types/typevars.go`, with the other two delegating.
   Taking the union of the copies turned up two composites *neither* had. Prefer that to
   grepping, wherever the switches are answering the same question.

   **`resolveType` / `resolveTypeIfKnown` were the outstanding instance of exactly that,
   and were folded 08/05.** They walked the same composites and differed only in what they
   do at an unknown *name* — report it, or hand the type back untouched — so the recursion
   was duplicated for a difference living in one leaf, and the 08/03 drift above is what
   that cost. They now share `resolveTypeWith`, which takes the leaf as a callback; the two
   names remain as wrappers, so no call site changed. What did *not* move into the shared
   walk is the reporting leaf's own work — alias-chain recursion, caching by resolved
   identity, the visibility check, the circularity guard — none of which the quiet twin
   does; the leaves differ by more than whether they report, which is the thing to preserve
   if this is ever touched again.

9. **A name does not identify a declaration, and since 08/03 it may not even identify
   one function.** Two facts compound here. A *key* is module-qualified for a private
   declaration (invariant 4), and a **receiver-overloaded** name maps to several
   declarations at once, told apart only by the receiver's type. So any pass that
   answers "what does this call call?" by looking the name up is wrong in one of two
   ways, both quiet: it gets another module's function, or it gets whichever overload
   was registered last. Read `typetable.TypeTable.Callee(call)` first — the typechecker
   publishes the member it picked — and fall back to `LookupFunctionFrom` for the rest.
   The backend pays the same tax twice over: `l.funcs` cannot hold two functions of one
   name, so an overload is keyed by its *declaration* and its emitted symbol carries the
   receiver head. The by-name form of this bug had already shipped — `funcParams` was
   written under the module-qualified key and read under the bare name, so a private
   function's parameter modes came back empty, a `mut` argument was passed by value
   instead of by address, and the program segfaulted (fixed 08/03,
   `TestExec_PrivateMutParamPassedByReference`).

10. **A pass that indexes a call's arguments positionally is one AST shape away from
   being silently wrong.** Purity reads `call.Arguments[idx]` against the *declaration's*
   parameter at `idx` (`callableParams`), so any call form whose receiver sits outside
   `Arguments` shifts every index by one — and a function-typed argument satisfies the
   wrong function-typed parameter without complaint, so a declared effect bound simply
   stops being enforced with nothing reported. Trait methods pay this with
   `methodArgumentAt`; UFCS avoids it by desugaring the receiver *into* `Arguments` before
   any later pass runs. Prefer the desugar: one rewrite beats teaching every consumer the
   same offset, and the mistake is invisible in review either way.

11. **A desugaring can rebind a parameter, so a later pass must not assume a body refers
   to one by its declared name.** `desugarClauses` turns a multi-clause function into
   `match (p0, p1) { … }`, and a clause is free to name a parameter something else —
   `(self: …, predicate: …)` destructured as `(Some v, pred)`. Until 08/06 `callableParams`
   knew only the declared names, so a call through the rename resolved to nothing and took
   the unresolved-callee default (`AllEffects`), reporting the function as impure *and*
   allocating. The trap is that it works whenever a clause happens to reuse the declared
   name — the prelude's `unwrap_or_else` passed and its `filter` did not, for no visible
   reason — so **correctness was contingent on a coincidence of spelling**, which review
   cannot see. `addMatchAliases` now maps arm bindings back to the parameter position they
   destructure. Two general lessons: fix such a thing at the construct the desugaring
   *produces* (the hand-written `match` had the identical hole, and the clause form only
   made it reachable), and be suspicious of any name-keyed analysis downstream of a pass
   that rewrites bindings.

12. **A box's `drop_fn` may free the box it is running on.** `lyra_rc_release` decrements
   strong, runs the payload's drop glue, then decides whether to free — and that glue is
   arbitrary user code which, through a cycle, can drop the last **weak** reference to the
   same box (a `Node` whose child holds `Maybe<weak Node>` back at it). `lyra_rc_weak_release`
   then frees the memory and the outer release frees it again. The strong owners therefore
   hold **one implicit weak reference**, taken in `lyra_rc_alloc` and dropped after the glue
   returns, so the count cannot reach zero mid-drop; Rust's `Arc` does the same, for the same
   reason. Do not "simplify" that back into a `weak == 0` test — it reads as equivalent and is
   an ASan-confirmed double free (08/07).

## Documentation comments (08/13)

`///` documents the declaration below it, `//!` the module the file belongs to. The
language-level rules are in the workspace `CLAUDE.md`; what matters inside this project:

- **`ast.Doc`** (`pkg/ast/doc.go`) holds the Markdown body, the first-paragraph
  `Summary`, and the `#`-heading `Sections` with `# Examples`/`# Panics`/`# Errors`
  classified. `NewDoc` returns **nil** for an empty comment, so "documented with
  nothing" is not a state any consumer has to handle.
- **Docs attach to declarations, not to types.** A struct field's doc is in
  `TypeDeclStmt.MemberDocs` (read it through `MemberDoc(name)`), never in
  `types.StructField` — `pkg/types` knows nothing about documentation, and an anonymous
  struct's field cannot carry one. If you add a documentable declaration, the Doc field
  goes on the AST node.
- **Attachment is `ctx.DocFor(node)`** (`collector_ctx/docs.go`), one helper for every
  site, because a doc comment is an `extra` and so is a *sibling* of what it documents
  at whatever level of the tree that lives. Top-level declarations are stamped in
  `walkProgram`'s `attachDoc` — that loop *is* the set of top-level declarations, so
  "only a top-level binding is documentable" holds with no per-site test. Members call
  `DocFor` at their own sites.
- **Two CST placement rules `DocFor` exists to absorb**, both of which fail by
  documenting every member except one: an extra before a node's first token attaches to
  the *enclosing* node (so the first method in a `trait`/`impl` body sits one level up —
  `prevSibling` climbs out of a node it begins), and a separator token may sit between
  the doc and its node (the `|` of the leading-bar `data` style). Pinned by the
  `IncludingTheFirst` tests in `pkg/analyzer/collector/tests/doc_comment_test.go`.
- **A doc that attaches to nothing is `lyra-W017`**, reported by `ctx.ReportStrayDocs`
  as a post-pass over the file's tree — after the walk, because whether a `///` was
  claimed is only knowable once every collector that might have claimed it has run. The
  claim set is keyed by start byte and reset per file (`ctx.ResetDocs`).
- **LSP hover** renders the doc under the type (`cmd/lyra-lsp/hoverdoc.go`).
  `resolveDoc` mirrors `resolveDefinition` case for case; keep them in step, or hover
  shows one symbol's docs above another symbol's type. **A typeless expression may still
  be documented** and `Hover` must not bail on the type alone: the method name of a UFCS
  call is a callee `desugarUFCSCall` synthesized, so it has no recorded type — and that
  is the spelling the whole standard library is written for, so it is the spelling whose
  documentation most has to show. That position renders the doc with no signature block.
- **`lyrac doc` renders it** (`pkg/docgen`, `cmd/lyrac/doc.go`) — one Markdown page per
  module, with Starlight frontmatter, into `-o` (default `./docs`). The package is split
  so `Collect` builds a model and `RenderMarkdown` renders one: a second renderer is a
  new function beside it, not a second AST traversal that can disagree about what a
  module contains. See that package's header for the rest.
- **A signature is re-rendered from the AST, in *source* syntax** (`docgen/signature.go`).
  `Type.GetName()`/`String()` are for diagnostics, where a type is described; a page is
  read as the code to write, and the two disagree — `DynamicArray<string>` for
  `[]string`, `boolean` for `bool`, and `GetName()` on a `ParameterizedType` dropping the
  arguments entirely, so `Maybe<t>` renders as the wrong type `Maybe`. Anything not
  special-cased falls through to `String()`. **`TestSignature_RoundTripsThroughTheParser`
  is the guard that matters**: every generated signature is fed back through the parser,
  which is what caught `(mut self: Rng)` — the modifier binds to the type, after the
  colon — a spelling that looks entirely plausible on a page and does not compile.
- **The prelude is the coverage guard** (`pkg/analyzer/collector/tests/prelude_docs_test.go`).
  It collects the real `std/prelude` as the multi-file module it is and asserts every
  declaration is documented, the members are, the `# Panics` sections are on exactly the
  functions that trap, and the module doc joins across files. `lyrac check` alone will
  *not* catch a detached doc — W017 is a warning, so the exit code stays 0.

## Operators that dispatch

Three groups, and which one an operator is in is a design decision rather than an
implementation state:

- **The comparisons are the compiler's.** `==`/`!=` are structural, overridden by the
  prelude's `Eq`; `<`/`<=`/`>`/`>=`/`<=>` all derive from `Ord::compare`. A `(_==_)`
  method name is refused (`lyra-E039`), because a second mechanism would be a coherence
  question with no answer and declaring them one at a time is how `<` comes to disagree
  with `<=>`. Both traits are found by **`@builtin(Ord)`/`@builtin(Eq)`** (08/08), not by
  the spelling — and dispatch filters candidate impls by the resolved *declaration*, since
  filtering by name is exactly what let a user's own `trait Ord` be taken for the
  prelude's.
- **Arithmetic and bitwise are the author's** (08/07). `+ - * / % << >> & | ~`, prefix
  `-` and `~`, and the compound assignments dispatch to a trait method named for the
  operator — keyed on the **method name**, with the trait whatever the author declared.
  `+` on a matrix and `+` on a duration share no invariant, so nothing is bought by
  insisting they come from one trait; two traits providing one operator for one type is
  an ambiguity reported at the operator. An operand that is a **type parameter** resolves
  through a `where` bound (08/08), the same abstract dispatch a bound `.method()` call
  takes.
- **The rest are inert, each for its own reason**, and the warning says which
  (`lyra-W015`): `&&`/`||` cannot short-circuit through a call, `!` is boolean negation,
  `**` is a spelling with no operator, the suffix forms name operators that do not exist.

## Supertraits

`trait B: A` means both halves as of 08/14, and they landed a week apart:

- **The obligation** (08/07, `lyra-E040`, `checkTraitImpl`): `impl B for T` requires an
  `impl A for T`. Declaration order does not matter — the impls are gathered up front.
- **The use** (08/14, `closeOverSupertraits` in `typechecker_trait_dispatch.go`): a
  `where t: B` bound reaches `A`'s methods, and satisfies a callee's `where u: A`.

The second is a **transitive closure taken where a bound set enters scope**, not a rule
each consumer applies. Four sites read `tc.genericBounds` — bound dispatch, the
generic-argument check, operator overloading, the `Show` desugar — and expanding at the
two write sites (`pushGenericBounds` for a binding, `checkTraitImpl` for an impl) is what
keeps them from needing to agree about anything. If you add a fifth reader, it is already
correct; if you add a third *writer*, it is not, and that is the thing to grep for.

Those two writers are twins, and their comments say so: a bound that reaches `A`'s methods
when written on a binding and not when written on an impl means different things depending
on where it is written.

Two properties worth not breaking. The closure is **cycle-safe by a visited set** —
`trait A: B` alongside `trait B: A` is legal, meaning the two are always implemented
together, which is precisely what E040 then requires of every implementer; assuming a DAG
hangs the typechecker, which presents as a frozen editor. And the **backend needs nothing**:
dispatch publishes candidates for the trait that *declares* the method, so a supertrait
call resolves to that trait's impls like any other.

**An umbrella trait parses as of the same day** — `trait Arithmetic: Add + Sub + Mul + Div`,
with or without a `{}` — which needed a grammar change: a trait's body is now optional,
braces and all. A method-less trait meant nothing before supertraits, so refusing it cost
nothing; now it is the shape the feature is written for. `impl_methods` was already optional,
so `impl Arithmetic for Vec2 {}` had been parsing all along — and its braces became optional too, later the same day.

The collector reads the field with `cst.Field` + a nil check rather than `MustField`
(`declarations/trait_decl.go`): an absent list is an empty method list, **not** a dropped
declaration. `MustField` returns nil, which would erase the trait and then report
`unknown trait` at every impl of it — a diagnostic pointing everywhere except at the
declaration.

**The prelude ships `Add`/`Sub`/`Mul`/`Div` and the `Arithmetic` umbrella**
(`std/prelude/math.lyra`), with impls for all ten integer widths and three float widths.
They exist so a **bound** can be satisfied, not so a call can dispatch — without them
`where t: Arithmetic` is undemandable of a number, and every generic numeric function is
unwritable. `impl Add for f64 { (_+_) = (self, o) => self + o }` is not recursion for the
reason immediately below.

Two rules hold across all of it. **A primitive is never routed through an impl** — `1 + 1`
is a machine add whatever a program declares — where "primitive" is the receiver
**unstripped** (08/12): a newtype over a scalar is not the scalar, so `impl Add for Cents`
dispatches while `impl Add for i64` stays inert. (The guard used to strip first, which made
a scalar newtype operator-dead from both sides — no machine ops and no impl — silently.
Beside it, the overflow-arithmetic builtins are refused on a newtype receiver, `lyra-E043`:
they are the operators' escape hatches, so the method fallback must not hand out what the
operator rule withholds.) And the resolution is `resolveTraitMethodNamed`,
the *same* function the identifier path uses with a full `MethodName` key, so an operator and
a `.method()` call cannot come to disagree about generic impls or `where` bounds. An operator
is a call, so the purity ladders charge it as one (`operatorImplEffect`).

## Sweeping for surfaces nothing reads

Four features turned up in two days that parsed, collected, and were consumed by nobody —
`wallClock`, a binding's `where` bounds, `@derive`, and operator-named trait methods. Each
looked implemented and did nothing, which costs more than an absent feature does.

The sweep that finds the AST half: enumerate every exported field of every struct in
`pkg/ast`, then grep for a reader **outside `pkg/ast`, outside `pkg/printer`, and outside
tests**. Excluding the printer is the part that matters — it reads every field by
reflection, so it makes everything look consumed. Excluding the declaring package matters
too, or a field read only by its own accessors (`SymbolTable.Traits`) reports as dead.

Run 08/07: **119 fields, 3 suspicious, 2 genuine** (`TraitDeclStmt.Bounds`, unenforced
supertraits — enforced later that day and fully usable 08/14, see [Supertraits](#supertraits);
`SymbolTable.PureFuncs`, a map written and never read). The conclusion worth
keeping is that the AST surface is *not* where this problem lives — the phantoms were in
effect tables (`builtinEffects`), in glue switches missing a case, and in grammar rules with
no collector consumer. Those need their own sweeps, and a field-level one will not find
them.

## Package map

| Package | What it is | Depth |
|---|---|---|
| `pkg/parser` | CGO wrapper around tree-sitter; `Parse(source) (*sitter.Tree, error)` | — |
| `pkg/cst` | CST accessors — `cst.Field`, the one way to read a grammar field | below |
| `pkg/ast` | AST node definitions; `AstNode` / `Named` / `Statement` / `Expression` / `Pattern` | — |
| `pkg/ast/symbols` | `SymbolTable` + the `Scope` tree; per-module name resolution | [README](pkg/ast/symbols/README.md) |
| `pkg/types` | The `Type` interface and every implementation; allocation flavors | [README](pkg/types/README.md) |
| `pkg/typetable` | `ast.Expression` → resolved type; the method/instantiation tables | — |
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
| `cmd/lyrac` | Compiler CLI (`check` / `build`) | below |

### `pkg/cst`

`cst.Field(node, "name")` is **the** way to read a grammar field, and the collector uses
nothing else. It answers exactly what `node.ChildByFieldName` did, nil included — so the
nil-node hazard (invariant 2) is unchanged — but resolves the field name to a grammar id
once instead of allocating a C string, calling into C and freeing it on every lookup.

That mattered more than anything the code review of 08/05 predicted: `ChildByFieldName` was
**~26% of all samples** in an analysis run, roughly half the front end, because the collector
asks at nearly every node. Going through the cached id is ~3.7x faster on the same walk and
made the whole pipeline **~25% faster** end to end. Measure with `pkg/driver`'s
`BenchmarkAnalyze_*`, which run the real pipeline over the real prelude — the LSP re-runs all
of it on every keystroke, so this is per-keystroke cost.

### `pkg/parser` and `pkg/ast`

Thin CGO wrapper around `go-tree-sitter`. Exports `Parse(source string) (*sitter.Tree, error)`.
The compiled C parser is linked in from `../tree-sitter-lyra/src/parser.c`.

All AST node definitions. Key interfaces:
- `AstNode` — base interface (`node()`, `GetLocation() Location`)
- `Named` — extends `AstNode` with `GetName() string`
- `Statement`, `Expression`, `Pattern` — supertype interfaces (all nodes implement one)

All concrete nodes embed `AstBase` (which holds the `Location`). `Location` is 1-based
`{StartLine, StartCol, EndLine, EndCol}`. `Location.Pretty()` formats a compact `line:col` or
`line:col-line:col` string.

AST files are organized by node kind, e.g. `expr_math.go`, `expr_if.go`, `stmt_for_loop.go`,
`decl_trait.go`.

### `pkg/typetable`

- `TypeTable` — maps `ast.Expression` nodes → resolved `types.Type`. Populated by the
  typechecker; read by later passes. `Set(expr, typ)` / `Get(expr)`.
- `MethodTable` — maps a `*ast.FunctionCallExpr` (a `.`-call or `Trait::method` call resolved to
  a trait-impl method) → the matched `*ast.TraitMethodImpl`. Populated by the typechecker during
  dispatch (`typechecker_trait_dispatch.go`); read by the purity checker so it doesn't have to
  re-derive dispatch. `Get` is nil-receiver-safe (no resolutions) so callers without a
  typechecker pass can pass `nil`.
- `TypeTable.SetCallee`/`Callee` (`calleetable.go`) is that same arrangement one rung down, for
  **receiver-keyed overloading**: a call whose callee name has several declarations records the
  one it resolved to, since the name no longer picks. Only overloaded calls are recorded —
  every other callee still resolves by lookup, and a second answer to a settled question is a
  thing that can disagree — so a consumer reads this first and falls back.

  A second map on `MethodTable` records **abstract bound dispatch** (a call on a
  bare type parameter resolved through a `where` bound): `SetBound`/`GetBound` associate the
  call with a `BoundMethodRef{Trait, Method}` — there is no single concrete impl, so the purity
  checker joins over all impls of that trait method.

### `pkg/driver`

The single reusable entry point to the whole front-end. `driver.Analyze(source []byte) *Result`
runs parse → collect → the standalone `checker.Check*` passes → `typechecker.Check` →
`checker.CheckPurity` → `ownership.Analyze` → `captures.Analyze` (the last three after
typecheck: purity needs the resolved `MethodTable`, ownership and captures the `TypeTable`) and
returns a `Result{Program, SymbolTable, ScopeTable, TypeTable, MethodTable, Ownership, Captures,
RangeSafety, Diagnostics}`. Every pass's errors are normalized to `[]diagnostic.Diagnostic` (CST
parse errors converted from tree-sitter's 0-based positions to 1-based `ast.Location`).
`Result.HasErrors()` / `Result.Errors()` filter by severity. This is where a backend (or any
tool needing a typed program) starts, instead of re-implementing the pipeline.

One ordering inside it is load-bearing rather than incidental: **the generic instantiation
set is closed before the per-specialization ownership pass runs** (`instantiations.go`). A
generic body calling another generic records a *template* — bindings written in the
enclosing body's own type variables — and composing those into real specializations is what
lets `unwrap<t> = expect(self, …)` compile. Doing it later, in the backend, would leave the
discovered specializations with no ownership table of their own, falling back to the
program-wide one; that table is analyzed generically, where a type variable is not
reference-counted, so a `t = string` body would emit neither retains nor releases.

`driver.AnalyzeUnits(units)` is the multi-module form, with `Analyze` as its single-unit case;
both user-facing tools go through it, since both resolve an import graph first (`cmd/lyrac`'s
`analyze`, `cmd/lyra-lsp`'s `analyzeDocument`). `Analyze` remains for a caller with a snippet
and no file — a test, or an unsaved editor buffer — and its units carry no file, which is why
the LSP's per-file filtering treats an empty file name as "this one".

`driver.ResolveEntryPoint(res) (*EntryPoint, []diagnostic.Diagnostic)` (`entrypoint.go`) finds
and validates the program's entry function: a top-level `let main` that is a zero-parameter
function returning `u8` (the process exit code, `EntryReturnExitCode`) or `void`/no-annotation
(`EntryReturnVoid`). `u8`, not a wider int — the OS truncates a process exit code to its low 8
bits regardless of what width the language lets you write (verified: even a real C `int main() {
return 300; }` exits 44, not 300), so a wider return type only adds the silent-truncation
surprise Lyra rejects elsewhere; matches Zig's `pub fn main() u8` / Rust's move to the narrow
`std::process::ExitCode` over a raw wide int. Absent/non-function/parametered/wrong-return
`main` → nil + diagnostics. It is a **build-time** requirement (a library or a `check` needs no
`main`), so it is intentionally *not* part of `Analyze` — only `lyrac build` calls it.

### `pkg/backend`

The seam between the front-end and code generation. `backend.Backend` is the interface a code
generator implements: `Name() string` and `Emit(res *driver.Result, entry *driver.EntryPoint)
([]byte, error)`. `Emit` is called by `cmd/lyrac` only after analysis is error-free and the
entry point resolves, so an implementation may assume a well-typed program.

### `pkg/docgen`

`Collect(res, opts) []Module` builds the documentation model; `RenderMarkdown(m) []byte`
renders one module as a Starlight page. Backing `lyrac doc`.

The split is the design: nothing about Markdown reaches the model, so a terminal `go
doc` view or a JSON dump is a new renderer beside this one rather than a second walk of
the AST that can disagree with it about what a module contains.

Two rules hold here and are easy to break:

- **Signatures are re-rendered from the AST, in source syntax.** Not sliced from the
  source text — a declaration's span runs to the end of its *body* — and not
  `Type.GetName()`, which is the diagnostic spelling. The page is read as the code to
  write, so a name on it that the parser rejects is a broken promise, and the mismatches
  are individually small: `DynamicArray<string>`/`[]string`, `boolean`/`bool`,
  `AnonymousTuple(a, b)`/`(a, b)`, and `ParameterizedType.GetName()` returning `Maybe`
  for `Maybe<t>` — a type that exists and is the wrong one.

  **The rule reaches members too, and that is where it was broken** (fixed 08/14): a
  method's name on a page is `ast.MethodName.Key()`, never `GetName()`. `GetName()` is the
  bare `Value`, so an operator-named method rendered as `/` where an author writes `(_/_)`
  — and it erases *kind*, so prefix `-` and binary `-` both render as `-` although they are
  different methods. It survived because every trait method in the standard library was an
  ordinary identifier, for which the two agree, until the prelude gained
  `Add`/`Sub`/`Mul`/`Div`; and because `TestSignature_RoundTripsThroughTheParser` checks
  `Decl.Signature` only, while a trait's methods are `Members`. Both gaps now have tests.
- **A doc body is shifted before it is embedded.** A doc comment is written standalone,
  so its `# Panics` is an h1; nested under a declaration on a page that breaks the
  outline and every table of contents built from it. `ast.ShiftHeadings` and
  `ast.TagBareFences` both go through `ast.walkDocLines`, the single fence tracker, so
  no consumer can come to a different conclusion about whether a `#` line is a heading
  or a comment inside an example.

### `pkg/printer`

Reflection-based AST printer used only in tests. `printer.PrintAST(program)` walks exported
struct fields; zero/nil/empty values are omitted. `printer.NewPrinter().Print(node)`
pretty-prints a raw tree-sitter CST node (useful for debugging).

### `cmd/lyra-lsp`

LSP server. Uses `github.com/owenrumney/go-lsp` over stdio. On every `textDocument/didOpen` or
`textDocument/didChange`:
1. Applies incremental edits to an in-memory doc store
2. Resolves the document's **import graph** and runs `driver.AnalyzeUnits` over the whole
   unit set (`units.go`), persisting the returned `docAnalysis` for hover/definition/etc.
3. Maps this document's `[]diagnostic.Diagnostic` to LSP via `diagToLSP` and publishes them

**The server analyzes a program, not a buffer** (`analyzeDocument`, `units.go`). It called
`driver.Analyze` on the single open file until 08/02, which is not a smaller version of the
real thing but a *different program*: it has no prelude, so `Maybe`, `Some`, `Ok` and every
other standard-library name was undefined in the editor — `undefined tuple type "Some"` on
files `lyrac check` compiled cleanly — and a program's own modules were unresolved the same
way. Roots and prelude selection now come from `modules.DefaultRoots`/`DefaultOptions`, so the
server and `lyrac` cannot disagree about where the standard library is.

Two things follow from being an editor rather than a compiler, and both are load-bearing:

- **The buffer is not the file.** Every open document is passed to the resolver as an
  `Options.Overlay`, so analysis sees unsaved text — including a file that has never been
  saved and has no on-disk content to read.
- **Only this document's half of the result may be used.** The program spans several files
  now, so `diagnosticsFor` filters diagnostics by file (a diagnostic naming none is kept —
  it is program-level and has nowhere else to go) and `docProgram` narrows the AST to this
  file's top-level statements. Every position-based handler walks that narrowed program:
  a line and column alone do not say which file they came from, so the prelude's line 40
  would otherwise answer a request about the user's line 40. For the same reason a
  definition resolving into another file is returned against *that* file's URI
  (`locationIn`), and a rename whose declaration lives in another file is declined rather
  than applied at those coordinates in this buffer.

Logs to `/tmp/lyra-lsp.log`. Build with `go build ./cmd/lyra-lsp`.

### `cmd/lyrac`

Compiler CLI, built on `pkg/driver`. Three subcommands (four): `lyrac check <file.lyra>` (parse +
typecheck, print diagnostics, exit 1 on any error), `lyrac build <file.lyra>` (check, resolve
the entry point via `driver.ResolveEntryPoint`, hand the typed program to the backend, link an
executable), `lyrac run <file.lyra>` (build into a temp directory and execute it) and
`lyrac doc <file.lyra>` (render the module's documentation as Markdown).
Diagnostics print as `path:line:col: severity[code]: message` (the `line:col` is omitted for a
program-level error with no location, e.g. a missing `main`).

`build` runs the `pkg/backend/llvm` backend via `lowerAndEmit` and **produces a native
executable** (08/06): the emitted IR goes to a temp file and `clang <ir> -lm -o <exe>` links
it, so the default artifact is `<name>` beside the source, not `<name>.ll`. The `-lm` is
unconditional, matching what the backend's behavioural tests compile with. Flags:

```bash
lyrac build prog.lyra                 # -> ./prog, no IR left behind
lyrac build -o build/prog prog.lyra   # executable elsewhere
lyrac build --keep-ll prog.lyra       # executable *and* prog.ll
lyrac build --emit-llvm prog.lyra     # prog.ll only; the one build needing no C compiler
lyrac build -O0 prog.lyra             # optimization level; default -O2
lyrac build --cc /path/to/clang …     # else $LYRA_CC, else clang on PATH
```

**The default is `-O2`, not clang's `-O0`** (08/09), and the reason is that this
compiler does not face the usual tradeoff: it emits **no debug info at any level**, so
shipping unoptimized buys no debuggability — only build time. Measured, `-O0` costs
about 3x on ordinary code (a string scan: 15925 µs against 5087 µs; an arithmetic loop
the optimizer can close disappears entirely) for roughly 50 ms of extra link time on a
2000-line module. The whole backend behavioural suite passes at `-O1`, `-O2`, `-O3` and
`-Os`, so the default does not rest on `-O2` happening to be gentle on this IR.

The level is matched loosely (`-O` plus anything) and passed through unexamined, so
`-Os`/`-Oz`/`-Ofast` work and an unknown one is clang's error to report in its own
words rather than a staler copy of clang's list kept here. Both "compile it with"
hints — the `--emit-llvm` one and the missing-compiler fallback — carry the level, or
they would describe a different build than the one they stand in for.

The compiler must accept a `.ll` as input, so plain `cc` is deliberately not a fallback — gcc
would reject the IR with a confusing error instead of a clear one. When none is found the
build fails (exit 1) but **writes `<name>.ll` next to the source anyway** and prints the
`clang` line: that IR is all the user has to compile once they install one, and the flags
said nothing about wanting it discarded on a path where nothing else was produced.

`run` (08/06) is that same pipeline with every artifact in a temp directory (`buildOptions.
ephemeral`), then `exec` with the child inheriting stdin/stdout/stderr. Two consequences to
keep:

- **It prints no build summary**, which is why `lowerAndEmit` returns the executable's path
  and leaves reporting to its caller. `lyrac run prog.lyra | grep …` should see the
  program's output, not the compiler's.
- **The program's exit status is the command's** (`exec.ExitError.ExitCode`), so an exit 1
  from a program is indistinguishable from a compile failure — the same trade `go run`
  makes. The compiler's own failures are the ones that also print a diagnostic.

`ephemeral` also suppresses the missing-compiler `.ll` fallback above: `run` promised to
leave nothing behind, and the temp path it would name is deleted by the time the message is
read. `-o`/`--emit-llvm`/`--keep-ll` are refused for `run` rather than ignored.

`doc` (08/13) renders one Markdown page per module into `-o` (default `./docs`), with
Starlight frontmatter so it drops into `lyra-website/src/content/docs/reference/`. Four
flags: `--private` includes unexported declarations, `--deps` follows imports, `--prelude`
adds the standard library (implies `--deps`), `--strict` exits non-zero on a gap.

Four decisions in it, each of which had an obvious wrong answer:

- **It refuses a program that does not type-check.** A signature is rendered from resolved
  types, so documenting a broken program prints `?` where a type failed to resolve and
  publishes it as though it were the API.
- **An undocumented public declaration is listed anyway**, with its signature. Dropping it
  makes the page silently misrepresent the module's surface, which is worse than a gap you
  can see. Coverage prints on *every* run, not only under `--strict`.
- **The prelude needs its own opt-in even under `--deps`**, because it is implicitly
  imported by everything — otherwise every project's docs contain a copy of the standard
  library. It is still documented when it *is* the entry module, which is how the standard
  library's own page is generated.
- **An impl's methods are not counted as gaps.** The contract lives on the trait, where a
  doc is required; an impl method's doc says what *this* implementation does differently,
  so for most impls having none is correct rather than missing.

The pages are `std-prelude.md`, not `std.prelude.md`: a site generator derives a URL slug
from the file name and strips dots, so the dotted form publishes at `/reference/stdprelude/`.
The page's title is still the real dotted path.

Codegen is pre-release but no longer minimal —
closures, generics, strings, arrays, `match`, traits, `?` and Perceus all lower; that
package's README is the current inventory, and `todo.md` the gaps. A form that does not
lower yet is a hard error, so a non-trivial `main` may still hit one rather than being
lowered incorrectly. Build with `go build ./cmd/lyrac`.

## Building

```bash
./build.sh          # build/{lyrac,lyra-lsp} with std -> ../std
```

The binaries go in `build/` with `std` beside them, because that is where `lyrac` looks
for the standard library: the directory containing its own executable, or wherever
`LYRA_STD` points. It is the beside-the-executable convention Rust, Zig and Go use for a
sysroot, and building this way means the resolution path is exercised daily rather than
only at release — a program can use the prelude with no environment set up at all.

Two details that are easy to get wrong and were:

- **The root is the directory *containing* `std/`, not `std/` itself.** A module path
  resolves beneath a root, so `std.prelude` is `<root>/std/prelude/`; returning the
  `std` directory looked for `std/std/prelude` and silently found no prelude.
- **`build/std` is a symlink, not a copy.** A copy drifts: you would edit
  `std/prelude/maybe.lyra`, rebuild, and still get the old prelude. Every staleness failure
  this project has hit — a cached parser object, a cached test binary, a leftover
  compiler — presented as a *behaviour* difference rather than as staleness, which is
  what makes them expensive to diagnose. A real install would copy; development must not.

`stdRoot` resolves symlinks before taking the executable's directory, since
`os.Executable` does not do so consistently (Linux reads the already-resolved
`/proc/self/exe`; macOS can return the link's own path). Without it, a compiler
symlinked onto `PATH` looks for the library beside the *link*.

`build/` is gitignored as a directory rather than binary-by-binary, so a new command
cannot land in the source tree unnoticed, and a stale compiler is one `rm -rf build`
away. The VS Code extension's `lyra.languageServerPath` should point at
`build/lyra-lsp`.

The standard library's sources live in `std/` and are tracked. The prelude is
`std/prelude/`, **one module across several files** — `std/prelude/README.md` documents the
constraints on what may go in it (exports need `pub`, `Maybe`/`Result` are shape-validated,
combinators are free functions taking `self` rather than trait impls) and why the split is
within a module rather than into several.

## Testing

### Collector golden tests (`pkg/analyzer/collector/tests/`)

```bash
go test ./pkg/analyzer/collector/tests/...          # run golden tests
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...  # regenerate .golden files
```

Pattern:
```go
func TestSomething(t *testing.T) {
    source := `let x = 42`
    runGoldenTest(t, source, "golden_file_name")  // no extension
}
```

Golden files live in `testdata/*.golden`. First run with a new file creates it and fails; re-run
to confirm. The printer omits zero/nil/empty fields, so only populated fields appear in the
output.

`parseAndCollect(t, source)` is the lower-level helper when you want `program` and `table`
directly without a golden file.

### Typechecker assertion tests (`pkg/analyzer/typechecker/tests/`)

```bash
go test ./pkg/analyzer/typechecker/tests/...
go test -run TestName ./pkg/analyzer/typechecker/tests/...
```

Pattern:
```go
res := parseCollectAndCheck(t, source, false)
assertNoErrors(t, res)
// or
assertErrorsAre(t, res, "expected error message 1", "expected error message 2")
```

`res` exposes `res.program`, `res.symTable`, `res.typeTable`, and `res.errors`.

### Backend behavioural tests (`pkg/backend/llvm/`)

They compile emitted IR with clang and run it. Two things to know before touching them —
the `sanitize_address` attribute (hazard 6 above) and the binary cache that keeps the
package at ~2s warm. Both are explained in `pkg/backend/llvm/README.md`.

Linux runs go through the workspace's `./asan.sh`, worth doing before pushing memory-model
work: Debian's older clang uses *typed pointers* and so rejects IR type mismatches that
Apple clang's opaque pointers cannot even represent.

### Running all tests

```bash
go test ./...
go test -run TestFunctionName ./pkg/...
```

## Current Development Focus

The typechecker is the active area. `todo.md` at the project root is the **open** backlog;
`COMPLETED.md` beside it is the dated record of what landed and why — the constraint that forced
a design, the measurement that disproved a diagnosis. An item citing "the Completed entry" means
that file.

The active areas are the typechecker (match exhaustiveness — see
`pkg/analyzer/typechecker/README.md`) and the FP/imperative purity work (see
`pkg/analyzer/checker/README.md`).

**A module may be a directory as of 08/07**: `std.prelude` is `std/prelude.lyra` *or* every
`*.lyra` inside `std/prelude/`, both being the same module — one path, one namespace, one
scope. The shipped prelude is now seven files (`std/prelude/`), split by topic.

The equivalence is the point. Receiver-keyed overloading, `pub`, prelude shadowing and
`SymbolTable.Imports` are all keyed on the **module**, so the alternative for a module that
outgrows a file — split it into several modules — silently changes what its names mean;
`unwrap_or` for `Maybe` beside `unwrap_or` for `Result` would have become a cross-module
duplicate. Splitting within a module leaves every one of those rules alone. `pkg/modules/README.md`
has the five decisions that shape it (not recursive, headers required and checked, both forms
in one root is an error, name order, and the entry file brings its module).

It surfaced one bug outside `pkg/modules`, worth remembering as a *timing* variant of hazard 8:
exports are recorded per **file**, so a name that becomes overloaded only in a later file of a
module had already been exported as a bare declaration, and the set built when the second file
was walked collided with it (`symbol "area" already defined`). `exportToGlobal` now lets a set
supersede a global binding that is one of its own members. Two independent things had hidden
it — within one file the merge happens before either member exports, so both export the same
set object; and the prelude branch of that same function discards duplicate-definition errors,
so the shipped prelude worked while a user module doing the same thing did not.

**Console input landed 08/05**: `read_line() -> Maybe<string>` (`pkg/backend/llvm/input.go`)
is the program's only input, and the only builtin returning an **owned** managed value —
which needed `calleeIsOwningBuiltin` in the ownership pass, because the unresolved-callee
default treats a *result* as borrowed and that direction leaks rather than being leak-safe.
Its companion `parse_i64` is in `std/prelude/parse.lyra`, **written in Lyra**: the line has to come
from libc and there is no FFI, so input cannot be expressed in the language, while parsing
can — and anything that can belongs in the prelude rather than the builtin registry. See
COMPLETED.md, and that backend README's `read_line` section for why the call site must emit
no branches (a merge block is neither case `flushStmtTemps` handles, which released the
string before the `match` consuming it).

**`<=>` landed 08/06**, yielding the prelude's `Ordering` (`Less | Equal | Greater`)
rather than a bool — so a three-way comparison is one exhaustiveness-checked `match`
instead of an `if`/`else if`/`else` chain. Floats are refused (NaN has no three-way
answer); integers and runes are supported. The lowering is branchless, for the reason
`read_line`'s is: a branching call site returns a merge block, which the temp-release
machinery does not handle. See `pkg/backend/llvm/README.md`.

**String `len`/`slice`/trim landed 08/06** (`pkg/backend/llvm/string_methods.go`), and the
bytes-vs-runes question was already answered by what shipped: `s[i]` and `for c in s` walk
code points, so `s.len()` counts runes (O(n)) and `slice(start, end)` is a half-open rune
range. The fat pointer's `len` field stays bytes — representation, not language. `slice`
copies into a fresh box rather than borrowing its parent's bytes, because a ref-counted
box's header sits at its *start* and a pointer into the middle cannot reach it.
`trim`/`trim_start`/`trim_end` are ordinary Lyra in the prelude.

It exposed a live `noalloc` hole worth remembering as hazard 8's fifth instance: a builtin
method is charged **no** effect by all three copies of the purity pass's dispatch ladder —
that is what makes `x.wrapping_mul(y)` usable from `pure noalloc` code — so `slice`, the
first builtin method that genuinely allocates, was invisible to all three at once and
`pure noalloc … => s.trim()` type-checked clean. The typechecker now records whether the
resolved builtin allocates (`MethodTable.SetBuiltinMethod(call, allocates)`), since only it
still has the receiver's type; all three ladders read the flag.

**Two phantom builtins closed 08/06.** A **member call on a type name** (`Rng.seeded(42)`)
type-checked clean and then crashed the backend with `llvm: unsupported method call` — hazard
5 inverted, and the hole that let `Random.global()` look implemented for months. The silence
was a rung below the member call: a PascalCase name owning no constructor inferred as a nil
with no diagnostic, so a plain access (`Rng.field`) and a bare mention (`let x = Rng`) were
equally quiet and `Nonexistent.make(1)` checked clean. `lyra-E035` now reports it at the
receiver (`typechecker_typename_value.go`) — one diagnostic at the source rather than one per
consumer, which is hazard 8's rule. The message says the language has **no type-namespaced
associated functions**, because that is the state of affairs rather than an unimplemented
call, and it is why the prelude's constructors are spelled `rng_seeded`.

And **`wall_clock_nanos()`** (`pkg/backend/llvm/clock.go`) replaced `wallClock`, the last
`builtinEffects` entry with no signature and no lowering. Implemented rather than deleted:
deleting would have left `EffectTime` a bit nothing in the language could set — the same
phantom from the other side. It is `clock_gettime` and nothing else, on the `random_seed`
model, and the effect ladder needed no new machinery — ambient reads carry `EffectTime` and
are refused by `pure`/`det`, a threaded timestamp is ordinary `i64` data.

**The terminal landed 08/15** (`pkg/backend/llvm/tui.go`): `set_raw_mode(on)`,
`read_key() -> Maybe<rune>` and `terminal_size() -> (i64, i64)`, on the `read_line`
model — the syscalls are builtins, and `std.tui` above them is unwritten Lyra. Only the
*input* half ever needed the compiler; `\e` already reached stdout as byte 27, so ANSI
colour and cursor positioning were always `print` calls.

**A fourth builtin landed 08/17**, `wait_for_key_ms(timeout) -> bool`: poll stdin with a
timeout and say whether anything is readable. It answers a **bool rather than a timed
read** because there are three outcomes (a key, nothing yet, input ended) and a
`Maybe<rune>` has two — so `read_key` still answers "a key or the end", and this answers
"is there anything". At EOF poll reports readable, so the pairing is exact rather than a
convention. Unlike `terminal_size` it needs no `runtime.GOOS`: `struct pollfd` and the
POLL* bits are identical on both targets.

`std/tui/` sits on top of them (`event.lyra`, `key.lyra`, `mouse.lyra`, `screen.lyra`,
`style.lyra`) and is where the `Event` type, the escape-sequence decoder and the ANSI
helpers live. **Mouse input needed no builtin**: a terminal reports clicks as escape
sequences on stdin, so enabling is `print` and receiving is `read_key`. It must be SGR
mode (`\e[?1006h`) — the legacy X10 encoding sends raw bytes, and a column past 95 is a
UTF-8 lead byte that `read_key` swallows the next two bytes into, losing the
coordinates. One thing it proved:
**three builtins are very nearly the whole story, and a lone Escape is the exception** —
`\e` begins every arrow, so the decoder must look ahead, and `read_key` blocks. It
buffers the lookahead so nothing is lost and Escape is merely one keypress late; the
proper fix is a timed read, whose signature is an open `todo.md` entry rather than a
hastily added fourth builtin.

Two things to know before touching the backend half. It is **the only file that consults
`runtime.GOOS`**, for one constant — `TIOCGWINSZ` differs between the targets — and it
is sound only because `lyrac` compiles for its own host; the other two builtins avoid
the question entirely by going through `cfmakeraw`, so `struct termios`'s
genuinely-different layout is never indexed, only carried. And the three **do not share
an effect**: `set_raw_mode` is EffectOutput (it changes the world deterministically, so
`det` allows it) while `read_key` and `terminal_size` are EffectInput — a window can be
resized between two calls, which is what makes the size external state rather than a
property of the program.

**Randomness landed 08/05**, and its shape is the same division of labour as `read_line`:
`random_seed() -> u64` (`pkg/backend/llvm/random.go`) is the only builtin — one word of OS
entropy via `getentropy` — while the generator (`Rng`, `next_u64`, `below`, `between`,
`random_below`) is ordinary Lyra in `std/prelude/rand.lyra`. A PRNG is arithmetic; asking the OS
for entropy is not.

Keeping the **seed** as the primitive is what makes `det` usable with randomness, and it is
not enforced by a rule anywhere — it falls out of effect inference. A seeded generator only
mutates its own receiver (`EffectMut`, which `det` permits), so a seeded draw is `det`-legal
and reproducible; `rng_from_entropy`/`random_below` reach `random_seed`, so inference gives
*them* `EffectRand` and `det` refuses them.

It also required fixing a pre-existing hole that had made the design impossible: a **builtin
method call** (`x.wrapping_mul(y)`, `x.floor()`, `xs.len()`) reaches the purity pass as a
`MemberExpr` callee whose dotted name matches nothing, so it fell to the unresolved-callee
default — `AllEffects` — and was charged as reading input *and* allocating. Explicit wrapping
arithmetic was therefore unusable from any `pure`/`det`/`noalloc` function, which is exactly
the code that wants it. The typechecker now publishes the resolution
(`typetable.MethodTable.SetBuiltinMethod`) rather than the checker re-deriving it from the
name — hazard 9's rule. **There were three copies of that dispatch ladder** (`lambdaEffects`,
`methodEffects`, and the reporting walk in `checkCallPurity`); all three needed the same arm,
which is hazard 8 again.

**`where` bounds mean something as of 08/07**, in three parts: a binding's bounds are in
scope for its own body (`tc.genericBounds` was fed only by an *impl's* clause, so a bounded
call reported "add a `where t: Trait` bound" with the bound already written); an
unsatisfied bound is `lyra-E036` at the **instantiation**, the only point where the
question has an answer; and a bound-dispatched call **lowers**, by the typechecker
publishing one resolution per implementing type (`MethodTable.SetBoundCandidates`) and the
backend picking by the receiver's substituted type. Impl matching stays in the typechecker
— a second copy in codegen is the drift `Resolution` exists to prevent. This is what
unblocks `Show`/`Eq`/`Ord`.

**`Show` landed 08/08**, closing the "no value of a generic type can be formatted" gap:
`"${v}"` and `println(v)` work under a `where t: Show` bound. The trait and an impl for
every printable scalar are **ordinary Lyra** in `std/prelude/show.lyra` — `"${self}"` on a
concrete primitive is the formatter `print` already picks, so no builtin was added — and
the compiler's half is a *desugar* (`typechecker_show.go`): the operand is rewritten to
`v.show()`, which is the bound dispatch that already existed, so the backend learned
nothing new. The trait is recognized by its **method**, not by its name, so a user's own
`trait Render { show: … }` works identically; that is why this needed no `@builtin(Show)`
marker, unlike `Ord`, which the compiler must know by name because it owns the comparison
operators.

**UFCS landed 08/03**: `m.unwrap_or(0)` resolves to a free function whose first parameter
is named `self`, by rewriting the call to pass the receiver as its first argument
(`typechecker_ufcs.go`, and that README's last section). The standard library's combinators
take `self`, so they read both ways. It matters beyond ergonomics — dispatch on the receiver
is what makes `map` on a `Maybe` and `map` on an array able to coexist, which the bare
top-level name cannot (see `todo.md`'s Modules section).

**Receiver-keyed overloading landed 08/03**, the declaration-side half of that
(`typechecker_overload.go`). A name may be declared several times in one module when every
declaration takes a `self` receiver and their receiver *type heads* differ (`types.HeadName`:
`Maybe<t>` and `Maybe<i64>` share the head `Maybe`). UFCS could already dispatch `m.map(f)`
on the receiver; what it could not do is let two `map`s be **written** in one module, which
is why the standard library had to split `maybe.map` from `result.map`. The prelude now
declares `unwrap_or` and `unwrap_or_else` twice each, for `Maybe` and for `Result`.
Overlapping heads are refused at the declaration rather than reported at each call, since
ranking two matching candidates would need a specificity ordering the language does not have.

**Generic trait-impl methods monomorphize as of 08/03**, which is the other half of the same
story: `impl Unwrap<t> for Maybe<t>` now emits one function per binding set, its body lowered
under those bindings, with an ownership table per specialization
(`driver.OwnershipByMethod`). Before that a generic impl either failed to lower or — for a
body that never touched the type variable — emitted *one* function that every instantiation
called, passing the wrong receiver type into it. Two consequences for anyone working nearby:
**a method body is analyzed for ownership at all now** (it never was, generic or not), and
`typetable.Resolution.SpecKey()` is the one name for a specialization, shared by the symbol,
the emitted-method cache and that table.
