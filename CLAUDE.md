# lyra (Go) — Project Context

This is the main compiler infrastructure for the Lyra programming language. It contains the parser, AST, type system, collector, typechecker, a standalone semantic checker, and the LSP server.

Go module: `github.com/Lyra-Language/lyra`

The tree-sitter grammar is a local dependency via a `replace` directive pointing to `../tree-sitter-lyra`. After regenerating the grammar (`npx tree-sitter generate` in that directory), always run `go clean -cache` before `go test` — otherwise Go's build cache serves the stale compiled C parser.

## Data Flow

```
source text
  → pkg/parser                        tree-sitter CST (*sitter.Tree)
  → pkg/analyzer/collector            CST → *ast.Program + *symbols.SymbolTable
  → pkg/analyzer/checker              standalone AST passes (e.g. use-before-declaration)
  → pkg/analyzer/typechecker          AST → *typetable.TypeTable + []TypeError
```

The LSP server (`cmd/lyra-lsp`) runs this full pipeline on every document change and publishes diagnostics from all three analysis stages.

## Package Reference

### `pkg/parser`
Thin CGO wrapper around `go-tree-sitter`. Exports `Parse(source string) (*sitter.Tree, error)`. The compiled C parser is linked in from `../tree-sitter-lyra/src/parser.c`.

### `pkg/ast`
All AST node definitions. Key interfaces:
- `AstNode` — base interface (`node()`, `GetLocation() Location`)
- `Named` — extends `AstNode` with `GetName() string`
- `Statement`, `Expression`, `Pattern` — supertype interfaces (all nodes implement one)

All concrete nodes embed `AstBase` (which holds the `Location`). `Location` is 1-based `{StartLine, StartCol, EndLine, EndCol}`. `Location.Pretty()` formats a compact `line:col` or `line:col-line:col` string.

AST files are organized by node kind, e.g. `expr_math.go`, `expr_if.go`, `stmt_for_loop.go`, `decl_trait.go`.

### `pkg/ast/symbols`
Lexical `SymbolTable` with a tree of `Scope` nodes:

- Scope kinds: `ScopeGlobal → ScopeModule → ScopeFunction → ScopeBlock / ScopeLoop`
- `Scope.Define(Named)` — adds to current scope, errors on duplicate
- `Scope.Lookup(name)` — walks up the scope chain
- `Scope.LookupLocal(name)` — current scope only
- `SymbolTable.Types` / `.Functions` — flat maps for fast global lookup by name; `.Functions` (and its `.PureFuncs` subset, the explicitly-`pure`-declared ones) is populated only for top-level `let`/`var name = <lambda>` bindings — a nested same-named function is deliberately not registered here (a name-keyed flat map can't disambiguate it from an unrelated binding elsewhere)

Destructuring-declaration names (`let (a,b) = …`, `let {x} = …`, `if let [a,b] = … { }`, `let {x} = … else { }`) are registered via `RegisterDestructuredName(name, decl)`, which maps each bound name to its owning `*ast.DestructuringDeclStmt` (so the binding's `var`/`let mut` mutability is recoverable). The typechecker later overwrites that placeholder with a typed synthetic `VarDeclStmt` once it infers each leaf's type, via `checkDestructuringDecl` — shared by plain `let`/`var`, `if let`, and `let … else`. Scoping differs by form: plain `let`/`var` registers into the current scope; `if let … { Then } else { Else }` registers into a scope pushed around `Then` only (names visible there via the parent chain, not in `Else`); `let … = v else { Else }` registers into the *enclosing* scope only after collecting the diverging `Else` block, so `Else` never sees the names but code after the statement does.

### `pkg/types`
Type system. All types implement the `Type` interface (`typeNode()`, `String()`, `GetName()`).

Concrete types:

| Type | Notes |
|---|---|
| `PrimitiveType` | `i8`–`i64`, `u8`–`u64`, `f16`/`f32`/`f64`, `bool`, `string`, `char` (no platform-dependent `int`/`uint`; no bare `float` — untyped literals default to `i64`/`f64`) |
| `PrimitiveType` (internal) | `untyped_int`, `untyped_signed_int`, `untyped_float` — for numeric literal inference |
| `StructType` | named struct with fields |
| `DataType` | sum type (variants) |
| `LambdaType` | function type with param types and return type |
| `TupleType` | anonymous tuple |
| `ArrayType` | with element type and optional size |
| `ConstrainedType` | range/literal/pattern/precision/step constraints |
| `PointerType` | raw pointer |
| `GenericType` | type variable (e.g. `T`) |
| `SelfType` | the `Self` type inside trait impls |
| `VoidType` | `void` |
| `UnresolvedType` | placeholder for a named type not yet resolved |

Helper predicates: `types.IsNumeric(t)`, `types.IsString(t)`, `types.IsBoolean(t)`.

Allocation modifiers: `None`, `Stack`, `Shared`. Type modifiers: `Mut`, `Ref`.

### `pkg/typetable`
- `TypeTable` — maps `ast.Expression` nodes → resolved `types.Type`. Populated by the typechecker; read by later passes. `Set(expr, typ)` / `Get(expr)`.
- `MethodTable` — maps a `*ast.FunctionCallExpr` (a `.`-call or `Trait::method` call resolved to a trait-impl method) → the matched `*ast.TraitMethodImpl`. Populated by the typechecker during dispatch (`typechecker_trait_dispatch.go`); read by the purity checker so it doesn't have to re-derive dispatch. `Get` is nil-receiver-safe (no resolutions) so callers without a typechecker pass can pass `nil`.

### `pkg/analyzer/collector`
Converts a tree-sitter CST into `*ast.Program` and `*symbols.SymbolTable`.

**Entry point:** `collector.NewCollector(source []byte)` → `c.Collect(rootNode) (program, table, errors)`

**Dispatch:** `collector.go` owns `CollectStatement` and `CollectExpr` (switch on `node.Kind()`), and `ParseType`. Subpackages call back into the root collector via the `Collector` interface to avoid circular imports.

**Nil-node hazard:** `node.ChildByFieldName(...)` returns a genuine Go `nil` `*sitter.Node` for an absent *optional* grammar field (e.g. a zero-parameter `lambda_type`'s `parameter_types`). Calling any accessor (`ChildCount`, `Child`, `Kind`, …) on that nil node **hangs inside the go-tree-sitter CGO binding instead of panicking** — found via a real bug (`parseParameterTypes`, fixed 06/24/26) where this silently froze the whole collector. Always nil-check before touching the result of an optional field lookup, the same way `parseType`/`CollectExpression` already do.

**Subpackages** (all pass `*collector_ctx.Ctx` as their first argument):

| Subpackage | Files handle |
|---|---|
| `declarations/` | `let`/`var`/`const` decls, destructuring decls, `if let`, trait decls, trait impls, module decls |
| `typedecls/` | `struct`, `data`, named tuples, `newtype`, constrained types, attributes |
| `expressions/` | all expression kinds (one file per kind) |
| `statements/` | `for`, `for-in`, `return`, `break`, `continue`, `with`, var reassignment, deref assignment |

**`collector_ctx.Ctx`** — shared state passed to every subpackage function:
- `ctx.Source []byte` — raw source bytes
- `ctx.NodeText(node)` — extract text from a node
- `ctx.NodeLocation(node)` — convert to 1-based `ast.Location`
- `ctx.AddError(node, severity, format, args...)` — append a `CollectorError`
- `ctx.MustField(node, fieldName)` — get a required child field, emitting an error if missing
- `ctx.Collector` — embedded interface for recursive dispatch back to the root

**Tree-sitter traversal conventions:**
- `node.ChildCount()` / `node.Child(i)` — all children including anonymous keyword tokens; use with `switch child.Kind()`
- `node.ChildByFieldName("field")` — first child with that field name
- `node.FieldNameForChild(uint32(i))` — field name at index `i`; use when a rule repeats the same field name (e.g. multiple `value:` fields in `commaSep1`)

### `pkg/analyzer/checker`
Standalone AST-level semantic passes. Most (e.g. `use_before_declaration.go`, `shadowing.go`, `unused_variables.go`) run after collection but before typechecking and only need the AST. **`purity.go` is the exception** — `CheckPurity(program, methodTable)` takes the typechecker's `*typetable.MethodTable` (nil-safe) so a pure function/method calling a trait method can be checked against the method it actually dispatches to; it must run *after* `typechecker.Check`, not before (see `cmd/lyra-lsp/main.go`'s ordering).

- **`use_before_declaration.go`** — `CheckUseBeforeDeclaration(program) []UseBeforeDeclarationError`
  Two-pass algorithm: collect all names declared directly in a block, then walk in order flagging any use of a not-yet-seen name.
- **`purity.go`** — `CheckPurity` enforces `pure` (lambdas and, since 06/24/26, trait-impl methods): no captured mutation, no calls to non-pure functions/methods, no `await`. `InferredPureFunctions(program)` separately exposes bottom-up-inferred purity (explicit or not) for top-level functions by name; methods aren't covered by that one yet (explicit `pure` only).

### `pkg/analyzer/typechecker`
Walks the collected AST and infers/verifies types, writing results into a `TypeTable`.

**Entry point:** `typechecker.New(symTable, scopeTable, typeTable)` → `tc.Check(program) []TypeError`

**`TypeError`** has `Message string`, `Location ast.Location`, `Severity` (`SeverityError` / `SeverityWarning`).

Key methods:
- `inferExprType(expr)` — returns the `types.Type` for an expression and records it in the `TypeTable`
- `checkNode(node)` / `checkVarDecl` / `checkVarReassignment` / `checkExpressionStmt` — statement-level checks
- `checkIfDestructuringStmt` / `checkElseDestructuringStmt` — type-check `if let`/`let … else` bodies, reusing `checkDestructuringDecl` to bind pattern names with the right scope (if-let's names are local to `Then`, entered via `enterScope` against a scope the collector pushed and recorded against the `*ast.IfDestructuringStmt` node itself; let-else's persist in the enclosing scope, like a plain `let`)
- `assignable.go` — `effectiveType` and unification logic for type compatibility
- `resolveTraitMethod(receiverType, methodName, requiredTrait)` (`typechecker_trait_dispatch.go`) — finds every impl whose target type structurally equals `receiverType` (`types.TypesEqual`) providing that method, optionally restricted to one trait; multiple matches with no `requiredTrait` is the "two traits, same method name" ambiguity a fully-qualified `Trait::method(...)` call resolves. Drives both `inferMemberCall`'s fallback (after struct-field lookup fails) and `inferTraitMethodPathCall`. Records each resolution in `tc.MethodTable()` for the purity checker. Generic impls (`impl<T> Show for Box<T>`) don't match yet — `TypesEqual` against a concrete receiver correctly returns false, but that's a separate, larger dispatch feature.

Files split by concern: `typechecker.go` (core + var decls + expressions), `typechecker_control_flow.go` (if/match), `typechecker_functions.go` (lambda/call/member-call dispatch), `typechecker_trait_dispatch.go` (trait-method resolution), `typechecker_traits.go` (impl conformance), `errors.go` (error helpers), `assignable.go` (type compatibility).

### `pkg/printer`
Reflection-based AST printer used only in tests. `printer.PrintAST(program)` walks exported struct fields; zero/nil/empty values are omitted. `printer.NewPrinter().Print(node)` pretty-prints a raw tree-sitter CST node (useful for debugging).

### `cmd/lyra-lsp`
LSP server. Uses `github.com/owenrumney/go-lsp` over stdio. On every `textDocument/didOpen` or `textDocument/didChange`:
1. Applies incremental edits to an in-memory doc store
2. Runs `parser.Parse` → `collector.Collect` → most `checker.Check*` passes → `typechecker.Check` → `checker.CheckPurity` (purity must come *after* typechecking — it needs the resolved `MethodTable`)
3. Publishes all collected diagnostics via `textDocument/publishDiagnostics`

Logs to `/tmp/lyra-lsp.log`. Build with `go build ./cmd/lyra-lsp`.

### `cmd/lyrac`
Compiler CLI — currently empty/scaffolded.

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

Golden files live in `testdata/*.golden`. First run with a new file creates it and fails; re-run to confirm. The printer omits zero/nil/empty fields, so only populated fields appear in the output.

`parseAndCollect(t, source)` is the lower-level helper when you want `program` and `table` directly without a golden file.

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

### Running all tests

```bash
go test ./...
go test -run TestFunctionName ./pkg/...
```

## Current Development Focus

The typechecker is the active area. See `todo.md` at the project root for the full backlog. Currently in progress:

- **Match exhaustiveness for number types** — range patterns, type-checking scrutinee and arm patterns

Queued:
- Match exhaustiveness for `string`, `data`, and `array` types
- Generic substitution bug: a data-pattern destructuring against a generic instantiation parsed from a parameter type annotation (`let f = (m: Maybe<i64>) -> i64 => { let Some(x) = m; x + 1 }`) binds `x` to a malformed type that prints as `?(i64)` instead of `i64`, so arithmetic on it then fails with a confusing "operands must be numeric" error. Reproduces with plain `let Some(x) = m`, independent of if-let — likely an incomplete case in `substituteGenerics`/`resolveToDataType` (found 06/22/26 while testing if-let against data types)
