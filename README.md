# Lyra

Lyra is a statically-typed programming language under active development. This
repository (`github.com/Lyra-Language/lyra`) is its **compiler infrastructure**,
written in Go: the parser, AST, type system, name/type collectors, typechecker,
standalone semantic checkers, and the language server.

> **Status:** pre-release and evolving. The parser, collector, typechecker, and
> LSP server are functional; the compiler CLI (`cmd/lyrac`) is scaffolded and not
> yet implemented. The active area of work is the typechecker and an
> effect/purity system — see [`todo.md`](todo.md).

## Language at a glance

```lyra
// Sum type (variants carry typed payloads)
data Shape = Circle f64 | Square f64

// A function is a binding to a lambda; the ML-style sugar makes a definition
// read like a function rather than a variable assignment.
let pure add(a: i32, b: i32) -> i32 => a + b

// Pattern-matching function body, with effect modifiers leading the name.
let pure rec fib(n: i32) -> i32 {
  (0) => 0,
  (1) => 1,
  (n) => fib(n - 1) + fib(n - 2),
}
```

Some design points that shape the language:

- **Deterministic primitives.** Fixed-width integers (`i8`–`i64`, `u8`–`u64`) and
  floats (`f16`/`f32`/`f64`), plus `bool`, `string`, `char`. There is no
  platform-dependent `int`/`uint` — untyped integer literals default to `i64`,
  untyped floats to `f64`.
- **Functions are values.** A function is a `let`/`var` binding whose value is a
  lambda. All of `let add = (a, b) => …`, `let add(a, b) => …`, and
  `let pure add(a, b) => …` produce the same binding.
- **An effect/purity ladder.** `pure` (no observable effect), `det`
  (deterministic), and `noalloc` (heap-allocation-free) are checked assertions;
  purity is also *inferred* bottom-up, so the keyword is a guarantee rather than a
  tax.
- **Traits, generics, pattern matching** (with exhaustiveness checking),
  destructuring, `data` sum types, structs, tuples, and newtypes.
- **Explicit memory intent** via allocation modifiers (`stack`/`shared`), arenas
  (`with` blocks), and `weak` references.

## Repository layout

This project is one of several in the Lyra workspace:

| Directory | Purpose |
|---|---|
| `lyra/` (this repo) | Go — parser, AST, types, collector, typechecker, LSP server, compiler CLI |
| `tree-sitter-lyra/` | JavaScript — the tree-sitter grammar (compiled to a C parser, linked via CGO) |
| `lyra-vscode-ext/` | TypeScript — VS Code extension that launches the LSP server |
| `lyra-website/` | Astro — the public site (dev blog and docs) |

The Go module depends on the grammar via a `replace` directive pointing at
`../tree-sitter-lyra`, so that checkout must sit alongside this one.

### Key Go packages

```
source text
  → pkg/parser              tree-sitter CST (*sitter.Tree), via CGO
  → pkg/analyzer/collector  CST → *ast.Program + *symbols.SymbolTable
  → pkg/analyzer/checker    standalone AST passes (use-before-declaration, purity, …)
  → pkg/analyzer/typechecker  AST → *typetable.TypeTable + type errors
```

- `pkg/ast`, `pkg/ast/symbols` — AST nodes and the lexical symbol table
- `pkg/types`, `pkg/typetable` — the type system and expression→type table
- `pkg/printer` — reflection-based AST printer (used by golden tests)
- `cmd/lyra-lsp` — the language server (completion, hover, go-to-definition,
  references, rename, semantic tokens, folding, inlay hints, symbols, …)
- `cmd/lyrac` — the compiler CLI (scaffolded)

More detail lives in [`CLAUDE.md`](CLAUDE.md).

## Prerequisites

- **Go 1.25+** (see `go.mod`).
- **A C toolchain** (clang or gcc) — the tree-sitter parser is linked in via CGO,
  so `CGO_ENABLED=1` (the default) and a working C compiler are required.
- **A checkout of `tree-sitter-lyra`** in a sibling directory (`../tree-sitter-lyra`).
- **Node.js + `npx`** — only needed if you regenerate the grammar (see below).

## Building

```bash
# Build everything
go build ./...

# Build just the language server binary
go build ./cmd/lyra-lsp
```

The `lyra-lsp` binary is what the VS Code extension launches over stdio; it
defaults to `lyra-lsp` on `$PATH` (overridable via the `lyra.languageServerPath`
setting in the extension).

## Testing

```bash
go test ./...                                                 # all tests
go test -run TestName ./pkg/...                               # a single test

# Collector golden tests
go test ./pkg/analyzer/collector/tests/...
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...    # regenerate .golden files
```

Golden files live in `pkg/analyzer/collector/tests/testdata/*.golden`. The printer
omits zero/nil/empty fields, so only populated fields appear in the output. A new
golden is created (and the test fails) on first run; re-run to confirm.

## Working on the grammar

The tree-sitter parser is a compiled C artifact linked via CGO, and Go caches its
compiled form. After changing `../tree-sitter-lyra/grammar.js` (or any file it
includes), regenerate and invalidate the cache **before** running Go tests:

```bash
# in ../tree-sitter-lyra
npx tree-sitter generate      # regenerate src/parser.c
npx tree-sitter test          # (optional) run the grammar corpus tests

# back in lyra/
go clean -cache               # otherwise Go serves the STALE compiled parser
go test ./...
```

Skipping `go clean -cache` causes Go tests to silently run against the old
grammar. Note the Lyra grammar is large (its `parser.c` is ~120 MB and
`tree-sitter generate` takes ~60s), so budget for that on grammar edits.

## Editor support

Language features are provided by `cmd/lyra-lsp` over LSP. The
[`lyra-vscode-ext`](../lyra-vscode-ext) extension is the reference client; point
it at a built `lyra-lsp` binary via the `lyra.languageServerPath` setting, or put
`lyra-lsp` on your `$PATH`.

## License

[MIT](LICENSE) © Avram Eisner
