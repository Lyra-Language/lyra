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
     prelude's.
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
   - **Copies can agree and be wrong together.** All three purity ladders charge a builtin
     method no effect, which is right for scalar arithmetic and wrong for `s.slice(a, b)`,
     the first that allocates: `pure noalloc … => s.trim()` type-checked clean. A shared
     answer is a shared *assumption*, and it fails with no divergence to notice.
   - **Two switches can disagree about a case neither one names**, which grepping will not
     find — `nominalHead` lacked `*ConstrainedType` while `types.HeadName` one layer up
     gives a newtype a head, so a method written *for* a newtype was silently unreachable.
   - **When adding an expression kind, grep for the kind it is a variant of.** The purity
     pass's allocation walk names allocating *forms*, not types, so `ArrayRepeatExpr` was
     missed in five places `ArrayLiteralExpr` appeared in.
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
   - **Paired walks must be fixed in one change.** `emitRetainValue`/`emitDropValue` both
     lacked `ParameterizedType`; fixing only the drop is an instant double free.
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

9. **A name does not identify a declaration, and may not even identify one function.**
   A *key* is module-qualified for a private declaration (rule 4), and a
   **receiver-overloaded** name maps to several declarations at once, told apart only by
   the receiver's type. So any pass answering "what does this call call?" by looking the
   name up is wrong in one of two quiet ways: it gets another module's function, or it gets
   whichever overload was registered last. Read `typetable.TypeTable.Callee(call)` first —
   the typechecker publishes the member it picked — and fall back to `LookupFunctionFrom`.
   The backend pays the same tax: `l.funcs` cannot hold two functions of one name, so an
   overload is keyed by its *declaration* and its emitted symbol carries the receiver head.

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

14. **A diagnostic with no Location is not merely imprecise — it appears on every file.**
    `diagnosticsFor` keeps a location-less diagnostic deliberately, on the grounds that it
    is program-level and has nowhere else to go (a missing `main`), so a *per-node* warning
    whose node has a zero Location attaches itself to whatever the user is compiling. Two
    warnings on prelude loops showed up on a file with no loop in it, which is how the
    missing `Location` on `ForInLoopExpr` was found — it had never had one. When adding a
    node, set its span; when adding a diagnostic, check that the node you report against
    has one.

15. **A builtin returning an owned managed value needs two things the defaults get wrong.**
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
- **Every position-based feature starts at `findExprAtPos`** (`hover.go`), and
  `definition.go`'s `scopeInExpr` is its twin for scopes. A node kind missing from one of
  them makes hover, definition, rename and highlight silently answer nothing in that
  construct; missing from only *one* is worse, since the expression is found and its name
  then resolved in the wrong scope. Add a kind to both or to neither.
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
one (`operatorImplEffect`).

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

Three things to know before touching it:

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
  only the typechecker still has the receiver's type. All three purity ladders read it.
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
  supplies `CString` and `xs.data()` as ordinary Lyra.
- **Ownership never crosses.** Neither side adopts the other's buffer; both directions
  would need the other to understand the rc header. A `^T` into a live array dangles at the
  next `push`.
- **A link requirement rides the extern that needs it** — `@link("m")` on the declaration,
  collected across every module in the compile, sorted and deduplicated, emitted as `-l`
  (`lyrac`'s `linkFlags`, which every "compile with" hint prints too). Not a CLI flag (a
  module's requirement would not compose) and not a manifest (this compiler has
  deliberately never had one). It needs no `unsafe`: a wrong library name fails loudly at
  link time, which is exactly what an effect bound does not do.

**`std.ffi` is `CBuffer`/`get`/`cstring_len`/`decode_utf8`/`cstring`.** The last is the
out direction and is a plain `[]u8` — option A, chosen over a `CString` type because the
dangling shape is already `lyra-E059`, because a struct storing the pointer dangles for
real on the next `push` (measured), and because the wrapper that would help is the scoped
`with_cstring`, not a name. It traps on an interior NUL.

**Both directions work now.** A buffer goes *out* as `&mut xs[0]` plus a length, which is
what zlib's `compress` takes; a `^u8` coming *back* is read through `p.offset(n)^`, and
`std.ffi`'s `CBuffer` is the checked wrapper over it (see "Raw pointers" above). What is
still unbuilt is the rest of `std.ffi` — `CString`, `xs.data()`, `CLong`/`CULong` — all of
which are now ordinary Lyra rather than blocked. See `todo.md`.

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
