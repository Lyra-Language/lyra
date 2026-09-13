# Lyra

Lyra is a statically-typed programming language under active development. This
repository (`github.com/Lyra-Language/lyra`) is its compiler, written in Go: parser,
AST, type system, collector, typechecker, semantic checkers, LLVM backend, the `lyrac`
CLI and the language server.

> **Status:** pre-release and evolving. Open work is in [`todo.md`](todo.md); the
> language reference is [`LANGUAGE.md`](LANGUAGE.md).

## Language at a glance

```lyra
// Sum type (variants carry typed payloads)
data Shape = Circle f64 | Square f64

// A function is a binding to a lambda; the ML-style sugar reads like a definition.
let pure add(a: i32, b: i32) -> i32 => a + b

// Pattern-matching function body, with effect modifiers leading the name.
let pure rec fib(n: i32) -> i32 {
  (0) => 0,
  (1) => 1,
  (n) => fib(n - 1) + fib(n - 2),
}
```

- **Deterministic primitives.** Fixed-width integers (`i8`–`i128`, `u8`–`u128`), floats
  (`f16`/`f32`/`f64`), `bool`, `string`, `rune`. No platform-dependent `int`; untyped
  literals default to `i64`/`f64`.
- **Functions are values.** `let add = (a, b) => …`, `let add(a, b) => …` and
  `let pure add(a, b) => …` produce the same binding.
- **An effect ladder.** `pure`, `det` and `noalloc` are checked assertions; effects are
  also inferred.
- **Traits, generics, pattern matching** with exhaustiveness checking, `data` sum types,
  structs, tuples, newtypes.
- **Explicit memory intent** via `stack`/`shared` allocation, `weak` references, and
  reference counting with Perceus-style reuse.

## Repository layout

| Directory | Purpose |
|---|---|
| `lyra/` (this repo) | Go — the compiler, CLI and LSP server |
| `tree-sitter-lyra/` | The tree-sitter grammar (compiled to C, linked via CGO) |
| `lyra-vscode-ext/` | VS Code extension that launches the LSP server |
| `lyra-zed-ext/` | Zed extension that launches the LSP server |
| `lyra-website/` | The public site (dev blog and docs) |

The Go module reaches the grammar through a `replace` directive pointing at
`../tree-sitter-lyra`, so that checkout must sit alongside this one.

```
source text
  → pkg/parser                tree-sitter CST, via CGO
  → pkg/analyzer/collector    CST → *ast.Program + *symbols.SymbolTable
  → pkg/analyzer/checker      standalone AST passes (purity, use-after-move, …)
  → pkg/analyzer/typechecker  AST → *typetable.TypeTable + type errors
  → pkg/backend/llvm          LLVM IR, linked with clang
```

`cmd/lyrac` is the CLI (`check`/`build`/`run`/`doc`); `cmd/lyra-lsp` is the language
server. [`CLAUDE.md`](CLAUDE.md) maps every package.

## Prerequisites

- **Go 1.25+**.
- **clang** (15 or later) — CGO needs a C compiler, and `lyrac build` links LLVM IR with
  clang.
- **`tree-sitter-lyra`** checked out at `../tree-sitter-lyra`.
- **Node.js + `npx`** — only to regenerate the grammar.

## Building

```bash
./build.sh               # build/{lyrac,lyra-lsp}, with build/std -> ../std
go build ./...           # just compile everything
```

`lyrac` finds the standard library beside its own executable, or at `$LYRA_STD`.

## Testing

```bash
go test ./...                                               # all tests
go test -run TestName ./pkg/...                             # a single test
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...  # regenerate .golden files
```

Golden files live in `pkg/analyzer/collector/tests/testdata/*.golden`; a new one is
created (and the test fails) on first run.

## Working on the grammar

After changing `../tree-sitter-lyra/grammar.js`, regenerate and clear Go's cache
**before** testing:

```bash
# in ../tree-sitter-lyra
npx tree-sitter generate
# back in lyra/
go clean -cache    # otherwise Go silently uses the stale compiled parser
go test ./...
```

## Editor support

`cmd/lyra-lsp` serves completion, hover, go-to-definition, references, rename, semantic
tokens and more. Point the VS Code extension at it with `lyra.languageServerPath`
(e.g. `build/lyra-lsp`), or put `lyra-lsp` on your `$PATH`.

## License

[MIT](LICENSE) © Avram Eisner
