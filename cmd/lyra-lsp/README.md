# `cmd/lyra-lsp`

LSP server over stdio (`github.com/owenrumney/go-lsp`). Build with `go build ./cmd/lyra-lsp`
(or `./build.sh`). Logs to `/tmp/lyra-lsp.log`.

On every `didOpen`/`didChange`: apply incremental edits to the in-memory doc store, resolve
the document's import graph and run `driver.AnalyzeUnits` over it (`units.go`), store the
`docAnalysis`, and publish this document's diagnostics.

## The server analyzes a program, not a buffer

`analyzeDocument` (`units.go`) analyzes the whole import graph with the prelude; a single
file alone is a different program in which `Maybe`/`Some`/`Ok` are undefined. Roots and
prelude come from `modules.DefaultRoots`/`DefaultOptions`, shared with `lyrac`.

- **The buffer is not the file.** Every open document is an `Options.Overlay`, so analysis
  sees unsaved (and never-saved) text.
- **Use only this document's half of the result.** `diagnosticsFor` filters by file (a
  diagnostic with no file is kept — so a per-node diagnostic with a zero Location appears on
  every file, rule 14). `docProgram` narrows the AST to this file's top-level statements, and
  every position handler walks that, since a line/column does not name a file. A definition
  in another file returns that file's URI (`locationIn`). `Analyze` units carry no file name,
  so an empty name means "this one".
- **References and rename span every file** (`crossfile.go`). `programViews` splits the whole
  program (`docAnalysis.fullProgram`) into one view per file over the shared tables, widened to
  an exported name's importers (`importerAnalysis`, keyed on the *declaring* file); occurrences
  are resolved per view and matched by declaration. A binding's uses include namespace members,
  UFCS calls (by recorded callee) and `import m.{ name }` members. Rename routes each edit to
  its file and declines only a standard-library declaration or an unreadable file. When the
  importer search for an exported name had no workspace folder to root it (`searchRoot` fell back
  to the document's directory), rename shows a `window/showMessage` warning naming the directory
  searched, rather than walking further up.

## Handler conventions

- Prologue is `handler.go`'s: `defer recoverHandler(name, &result, &retErr)`, `h.docFor(uri)`
  (analysis + source under one lock), `h.cursorAt(uri, pos)`. **`recover()` only works when
  called by the deferred function itself** — wrapping `recoverHandler` in a closure silently
  disables the panic guard. `TestRecoverHandler_*` guards it.
- **Position lookups start at `findExprAtPos`** (`hover.go`), which walks with
  `walkProgramExprs` (`codeaction.go`) and keeps the narrowest containing span. That walk adds
  the `const` bounds of range patterns (`RangePattern.ConstBounds`), which no AST walk reaches
  and the typechecker has folded to literals; expression walks go through it, not a bare
  `ast.WalkStmt`. It does **not** prune
  at a node failing to contain the position (a zero `Location` would hide its subtree).
  `definition.go`'s `scopeInExpr` is its twin and still switches on kinds — a missing kind
  resolves names in the wrong scope. A missing case in a position lookup shows up as the
  editor doing nothing.
- **A pattern binding resolves through `patternBindingAt`** (`rename.go`), from the name's own
  span (`ast.PatternBinding.Loc`), and its declaration's span through `bindingNameLoc`, never
  `namedNameLoc` alone: a destructured name's entry spans the whole statement.
- **Hover docs** (`hoverdoc.go`): `resolveDoc` mirrors `resolveDefinition` case for case —
  keep them in step. A typeless expression may still be documented (a UFCS method name is a
  synthesized callee with no recorded type), so `Hover` must not bail on a missing type.
- A map key is not a source name: completion labels read `decl.Name`.

## Per-keystroke caching

All caches are keyed on content; the handler owns them and `lyrac` passes nil.

- **`modules.Options.ParseCache`** reuses a file's syntax tree when its bytes are unchanged
  (keyed on bytes, not path/mtime, so a stale tree is unreachable). A file's imports are
  cached beside its tree (`Unit.Imports`).
- **`position.go`'s line index** makes byte-column → UTF-16 a slice read. It is keyed on the
  source's **data pointer and length**, not contents — string equality is `memequal` over the
  whole text.
- **`driver.CollectCache`** reuses the collection of every unit but the last. Reuse is by
  **clone**: `SymbolTable.Clone` copies, the master is never mutated.
  `TestClone_MentionsEverySymbolTableField` fails when a field is added but not copied.
  The **AST is shared** (Scope/Type/MethodTable key on AST pointers); this is safe because
  re-analyzing a collected AST is idempotent even though `desugarClauses` mutates it.
  The snapshot key covers the prelude path and the **import graph** as well as the prefix
  bytes, since `SetPreludeModule`/`SetImports` change declaration keys.
- **`typechecker.Snapshot`** holds the prefix's typechecking (four output tables + errors)
  in the same cache under the same key — two caches keyed identically are two chances to
  invalidate one and not the other. Valid because the prelude cannot see user code.

Remaining per-keystroke cost is purity, ownership and shadowing (whole-program). Measure
with `pkg/driver`'s `BenchmarkAnalyze_*`.
