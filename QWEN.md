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

4. **Resolve top-level names only through `LookupType`/`LookupTrait`/`LookupFunction`**,
   never by indexing `SymbolTable.Types`/`.Functions`/`.Traits`. Which declaration a name
   means depends on *which module is asking*, and a lookup scattered over dozens of sites
   cannot be taught that. Same reason `recordedType`, `types.StripNewtype`, `slotIsOwning`
   and `types.IsCopiedScalar` exist: one predicate, so two passes cannot drift apart.

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
  typechecker pass can pass `nil`. A second map records **abstract bound dispatch** (a call on a
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
tool needing a typed program) starts, instead of re-implementing the pipeline. Both `cmd/lyrac`
and `cmd/lyra-lsp` call it, so the pipeline is defined in exactly one place.

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
2. Calls `driver.Analyze` (the shared pipeline) and persists the returned `docAnalysis`
   (program + tables) for hover/definition/etc.
3. Maps the returned `[]diagnostic.Diagnostic` to LSP via `diagToLSP` and publishes them

The `analyze` method is now a thin wrapper over `driver.Analyze` — it no longer re-implements
the pass sequence.

Logs to `/tmp/lyra-lsp.log`. Build with `go build ./cmd/lyra-lsp`.

### `cmd/lyrac`

Compiler CLI, built on `pkg/driver`. Two subcommands: `lyrac check <file.lyra>` (parse +
typecheck, print diagnostics, exit 1 on any error) and `lyrac build <file.lyra>` (check, resolve
the entry point via `driver.ResolveEntryPoint`, then hand the typed program to the backend).
Diagnostics print as `path:line:col: severity[code]: message` (the `line:col` is omitted for a
program-level error with no location, e.g. a missing `main`). `build` runs the
`pkg/backend/llvm` backend via `lowerAndEmit`, writing `<name>.ll` next to the source and
printing the `clang` command to compile it. Codegen is early (literals/arithmetic/int-width
conversions — see that package's doc comment), so a non-trivial `main` may still hit a form
`lowerExpr` errors on rather than lowering incorrectly. Build with `go build ./cmd/lyrac`.

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
