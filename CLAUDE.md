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
   bitten six times, in five different switches, and never looked like what it was:
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

   The durable fix for a switch with more than one caller is to stop having more than one
   of it. The type-variable walk was three switches (typechecker `collectTypeVars`, backend
   `mentionsTypeVar`, and the generic-parameter-list check that wanted a third); it is now
   one, `types.CollectTypeVars` in `pkg/types/typevars.go`, with the other two delegating.
   Taking the union of the copies turned up two composites *neither* had. Prefer that to
   grepping, wherever the switches are answering the same question.

   **`resolveType` / `resolveTypeIfKnown` are the outstanding instance of exactly that.**
   They walk the same composites and differ only in what they do at an unknown *name* —
   report it, or hand the type back untouched — so the recursion is duplicated for a
   difference that lives in one leaf. The 08/03 drift above is what that costs. Folding
   them into one walk parameterized by the leaf behaviour is `todo.md`'s open item; until
   then, an edit to either belongs in both.

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

## Package map

| Package | What it is | Depth |
|---|---|---|
| `pkg/parser` | CGO wrapper around tree-sitter; `Parse(source) (*sitter.Tree, error)` | — |
| `pkg/ast` | AST node definitions; `AstNode` / `Named` / `Statement` / `Expression` / `Pattern` | — |
| `pkg/ast/symbols` | `SymbolTable` + the `Scope` tree; per-module name resolution | [README](pkg/ast/symbols/README.md) |
| `pkg/types` | The `Type` interface and every implementation; allocation flavors | [README](pkg/types/README.md) |
| `pkg/typetable` | `ast.Expression` → resolved type; the method/instantiation tables | — |
| `pkg/analyzer/collector` | CST → `*ast.Program` + `*SymbolTable` | [README](pkg/analyzer/collector/README.md) |
| `pkg/analyzer/checker` | Standalone AST passes — purity, effects, use-after-move, value ranges | [README](pkg/analyzer/checker/README.md) |
| `pkg/analyzer/typechecker` | Inference and checking; generics; trait dispatch | [README](pkg/analyzer/typechecker/README.md) |
| `pkg/analyzer/captures` | Each lambda's free variables, for the closure environment | [README](pkg/analyzer/captures/README.md) |
| `pkg/analyzer/ownership` | Where the backend must retain/release; Perceus | [README](pkg/analyzer/ownership/README.md) |
| `pkg/modules` | Import resolution, namespacing, the implicit prelude | [README](pkg/modules/README.md) |
| `pkg/driver` | The one reusable front-end pipeline | below |
| `pkg/backend/llvm` | The LLVM IR backend | [README](pkg/backend/llvm/README.md) |
| `pkg/printer` | Reflection-based AST printer, for golden tests | — |
| `cmd/lyra-lsp` | LSP server over stdio | below |
| `cmd/lyrac` | Compiler CLI (`check` / `build`) | below |

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

Compiler CLI, built on `pkg/driver`. Two subcommands: `lyrac check <file.lyra>` (parse +
typecheck, print diagnostics, exit 1 on any error) and `lyrac build <file.lyra>` (check, resolve
the entry point via `driver.ResolveEntryPoint`, then hand the typed program to the backend).
Diagnostics print as `path:line:col: severity[code]: message` (the `line:col` is omitted for a
program-level error with no location, e.g. a missing `main`). `build` runs the
`pkg/backend/llvm` backend via `lowerAndEmit`, writing `<name>.ll` next to the source and
printing the `clang` command to compile it. Codegen is pre-release but no longer minimal —
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
  resolves beneath a root, so `std.prelude` is `<root>/std/prelude.lyra`; returning the
  `std` directory looked for `std/std/prelude.lyra` and silently found no prelude.
- **`build/std` is a symlink, not a copy.** A copy drifts: you would edit
  `std/prelude.lyra`, rebuild, and still get the old prelude. Every staleness failure
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

The standard library's sources live in `std/` and are tracked; `std/prelude.lyra`
documents the constraints on what may go in it (exports need `pub`, `Maybe`/`Result` are
shape-validated, and methods do not lower yet so combinators are free functions).

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
