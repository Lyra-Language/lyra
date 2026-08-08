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
   entry module, `<module>::<name>` when private or when it takes a prelude name — so
   *which* accessor you use is a correctness question, not a style one. Prefer
   `LookupTypeFrom`/`LookupTraitFrom`/`LookupFunctionFrom(name, loc)`, which resolve as the
   file at `loc` sees it; bare `LookupType(name)` answers only for a program-wide name, and
   asking it from inside a module that declares its own returns *another* module's
   declaration. Two corollaries that have already bitten: a map key is **not** a source
   name, so anything user-facing (an LSP completion label, a "declared names" set) must read
   `decl.Name` rather than the key; and a `pub` check must ask about the declaration a
   reference *resolved to* (`declVisibility`), never look one up by name — `DeclaringModule`
   is last-writer-wins, and reported a module's own type as private to another module that
   happened to declare the same name.

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
   bitten seven times, in six different switches, and never looked like what it was:
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
supertraits; `SymbolTable.PureFuncs`, a map written and never read). The conclusion worth
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

Compiler CLI, built on `pkg/driver`. Three subcommands: `lyrac check <file.lyra>` (parse +
typecheck, print diagnostics, exit 1 on any error), `lyrac build <file.lyra>` (check, resolve
the entry point via `driver.ResolveEntryPoint`, hand the typed program to the backend, link an
executable) and `lyrac run <file.lyra>` (build into a temp directory and execute it).
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
lyrac build --cc /path/to/clang …     # else $LYRA_CC, else clang on PATH
```

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
