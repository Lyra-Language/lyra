# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The Go module references the tree-sitter grammar via a `replace` directive pointing to `../tree-sitter-lyra`. The Go binding includes `src/parser.c` directly via CGO. After regenerating the C parser (`npx tree-sitter generate` in `../tree-sitter-lyra`), you must run `go clean -cache` before `go test` — otherwise Go's build cache will serve the stale compiled parser and the grammar changes won't take effect.

## Commands

```bash
go test ./...                                     # Run all tests
go test ./pkg/analyzer/collector/tests/...        # Run collector golden tests
go test -run TestFunctionName ./pkg/...           # Run a single test
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...  # Regenerate golden files
```

## Architecture

**Data flow:** source text → `pkg/parser` → tree-sitter CST → `pkg/analyzer/collector` → AST + `SymbolTable`

Key packages:

- `pkg/parser` — thin wrapper around tree-sitter Go bindings; returns `*sitter.Tree`
- `pkg/ast` — AST node definitions (`Statement`, `Expression`, `Pattern` interfaces; `AstBase` embedded in all nodes for source `Location`)
- `pkg/types` — type system (`Type` interface; `StructType`, `DataType`, `LambdaType`, `PrimitiveType`, `ConstrainedType`, etc.)
- `pkg/analyzer/collector` — main CST→AST walker; dispatches by `node.Kind()` to subpackages
- `pkg/printer` — reflection-based AST printer used exclusively for golden tests

**Collector subpackages** (all under `pkg/analyzer/collector/`):

| Subpackage | Handles |
|---|---|
| `declarations/` | `let`/`var`/`const`, trait declarations, trait implementations, modules/imports |
| `typedecls/` | `struct`, `data`, named tuples, constrained types |
| `expressions/` | all expression kinds |
| `statements/` | for loops, arena/with |

`collector.go` owns top-level dispatch (`CollectStatement`, `CollectExpr`, `ParseType`). `collector_ctx.Ctx` carries shared mutable state (source bytes, error sink) and the `Collector` interface, which lets subpackages call back into the root collector without a circular import.

## Golden Tests

1. Write a Lyra source string in `pkg/analyzer/collector/tests/*_test.go` and call `runGoldenTest(t, source, "file_name")`
2. First run creates `testdata/file_name.golden` and fails — re-run to verify
3. `UPDATE_GOLDEN=1` updates existing golden files instead of failing

The printer (`pkg/printer/ast_printer.go`) walks exported struct fields via reflection. Zero/nil/empty values are omitted, so a `[]string` field only appears in the golden output when non-empty.

## Collector Node Traversal

- `node.ChildCount()` / `node.Child(i)` — ALL children including anonymous keyword tokens; use with `switch child.Kind()`
- `node.ChildByFieldName("field")` — first child with that field name
- `node.FieldNameForChild(uint32(i))` — field name of child at index `i`; use when a rule repeats the same field name (e.g. multiple `value:` fields in `commaSep1`)
