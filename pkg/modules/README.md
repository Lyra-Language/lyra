# `pkg/modules` — import resolution, namespacing, the prelude

`Resolve(entryFile, roots, opts) ([]Unit, []diag.Diagnostic)` turns an entry file into the
ordered source units a compile needs, following `import`s transitively. Language-level rules
(file-or-directory modules, import boundaries, shadowing) are in
[`LANGUAGE.md`](../../LANGUAGE.md).

- **Paths map to files by directory convention**: `std.io` is `std/io.lyra` beneath a root. Roots
  are searched in order — the entry file's directory, then `LYRA_STD` or a `std/` beside the
  compiler binary — so a project's modules win over the standard library.
- **Units come back dependency-first**; a module reached twice is emitted once.
- **Cycles are rejected** (`lyra-E027`), not broken — there are no partial-initialization
  semantics. An unresolvable import (`lyra-E026`) names the paths it tried.
- `scan.go` reads `module`/`import` straight off the CST, because the graph must be known before
  collection.
- Out of scope: package management, versioning, separate/incremental compilation.

## A module is a file or a directory

`std.prelude` is `std/prelude.lyra` **or** every `*.lyra` directly in `std/prelude/` — one
module either way. That equivalence matters because receiver overloading, `pub`, prelude
shadowing and `SymbolTable.Imports` are all keyed on the *module*; splitting into several
modules would change what names mean.

- **Not recursive**: a subdirectory is the next path down (`std.prelude.text`).
- **Every file in a directory module declares its module** (`checkHeader`). A single-file module
  needs no header, but a header contradicting its location is an error.
- **Both forms in one root is an error**; across roots the earlier root wins.
- **Files load in name order**, since unit order feeds diagnostic order.
- **The entry file brings its siblings** (`entryGroup`) when it sits in a directory named by its
  own module path; otherwise it is a single-file module with neighbours.
- The overlay reaches into module directories too (`isModuleDir`, `moduleFiles`).

**Hazard — exports are recorded per file** (`collector.recordModuleBindings`): a name that becomes
overloaded only in a later file was already exported bare, so the second file's set collided
(`symbol "area" already defined`). `exportToGlobal` lets a set supersede a global binding that is
one of its own members. The prelude hid this because its branch discards duplicate-definition
errors.

## Roots and the overlay (`roots.go`)

`DefaultRoots(entryFile)`, `DefaultOptions()` and `StdRoot()` are the one answer for every
front end (`lyrac` and the LSP — without it the editor ran with no prelude).

- **The root is the directory *containing* `std/`**, not `std/` itself.
- **The executable path is symlink-resolved** before taking its directory (the LSP is usually a
  symlink on `PATH`).

`Options.Overlay` maps a path to in-memory source that **wins over disk** and makes the path count
as existing, so an import of a never-saved buffer resolves. Keys are `filepath.Clean`ed on entry.

## Namespaces

`import util.math` binds a namespace (`math.double(…)`); `.{ X, Y as Z }` binds names directly.
Resolution is `typechecker/typechecker_modules.go` (`moduleMemberType`) and the backend's
`namespaceCallee`, sharing these rules:

- **Check for a namespace before inferring the object** — inferring `math` as a value reports it
  undefined.
- **The asking module comes from the node's `Location.File`.**
- **Membership is checked, not assumed**, in both front end and backend — a bare lookup can bind
  `math.secret` to another module's `secret`. Ask `ModuleDeclares` and key from the declaration
  resolved to, never `DeclaringModule` + `l.funcs[name]` (last-writer-wins; a shadowed name then
  dies as `llvm: unsupported method call`).
- **A namespace call resolves to the callee's declaration**, not its signature: `moduleMemberType`
  returns the `*ast.LambdaExpr` and `inferMemberCall` goes through `inferLambdaCall`, so a generic
  callee's variables get solved. The backend's `namespaceCallee` asks `specializedFuncFor(call)`
  **before** `l.funcs`. Tests: `modules/generic_namespace_call_test.go`,
  `backend/llvm/llvm_module_call_test.go`.
- A local binding shadows a namespace.

## Visibility

The lookup chain is **module → imports → prelude**, and stops.

- `SymbolTable.ImportScopeFor(module)` holds what a module's imports bring in, filled by
  `PopulateImportScopes` in `Collector.Finish` (the earliest point — exports are per file).
- `PreludeScope.Parent` is **nil**. `GlobalScope` holds each name **one** module exports; a name
  several modules export moves to `SharedExports`, so a context-free lookup misses rather than
  picking one. `ExportingModules` reads both to say "module `lib` exports it, but this file does
  not import it". The remaining conflict — one module importing a name from two — is an
  `ImportClash` from `PopulateImportScopes`, reported by the collector.
- Only a selective import binds a bare name; an alias binds only its local name.
- **Types need their own gate**: values are gated structurally by the scope chain, but types go
  through `declKey`-keyed maps, so `importedAt` checks the module of the declaration's **file**
  (never `ModuleOf[name]`, which is last-writer-wins).
- `reportPrivateType` must not fire for an exported-but-unimported name.
- A syntactic walk (`collectRefsByFile`) cannot implement visibility — it has no scope
  information and misattributes local bindings.

**`pub`** (`lyra-E028`) is exactly the module boundary. Privacy is **structural**: a private
declaration lands only in its module's scope, so references from elsewhere don't find it; E028 is
recovered on the not-found path for a better message. Checks ask about the declaration a reference
**resolved to** (`declVisibility`; `visibilityIn(imp.Path, …)` for a namespace member,
`BindingIn(module, name)` not `BindingOf`). A bare call needs the check as much as a namespace
member does. A module-less single-file program is all same-module.

Collection gotchas: `VarDeclStmt.IsPublic` must be collected, and `visibility` is an **anonymous
child**, not a field — `ChildByFieldName("visibility")` returns nil silently; scan by kind.

## The prelude

`modules.PreludeModule` = `std.prelude`, an ordinary module imported implicitly, resolved before
the entry file's imports, its exports available unqualified.

- **A missing prelude is not an error** (most tests have none).
- **The prelude does not import itself** — compared by module path, so entering at one of its
  files works. `LYRA_NO_PRELUDE` disables it.
- **The prelude module must be named before any file is walked** (`SetPreludeModule`), since
  types register mid-walk.
- **Prelude names are tracked in their own set**, not inferred from last-writer-wins `ModuleOf`.
  `ModuleDeclares` is the precise "did this module declare it" question (`shadowsPrelude`,
  namespace members). Withdrawal of a shadowed type is guarded on the entry actually being the
  prelude's.

## Shadowing

A module's own top-level declaration of a name reaching it from the prelude (`lyra-W012`) or an
import (`lyra-W016`) warns and wins locally (`noteAmbientShadow`). Two modules *exporting* one name
is still an error (`exportToGlobal`).

- **`PreludeScope` sits between module scopes and the global scope**, so a shadow never becomes
  program-wide.
- **Every declaration has its own key** (`<module>::<name>`); resolution (`FunctionKey`, from the
  *referencing* location) tries the asking module first. So `ownership`/`use_after_move` must use
  `LookupFunctionFrom`, never a bare `Functions` read.
- **Types and traits are keyed the same way**, so two modules may each have a private `Point`:
  - `RegisterType`/`RegisterTrait` write the module scope *before* computing the key and publish
    only through `exportToGlobal`.
  - The typechecker's `resolvedTypes` cache is keyed by resolved key.
  - The backend's `l.structTypes` is keyed the same way, resolving from `currentLoc` (set per type
    definition and function body); generic symbols (`Box$i64`) are qualified too.
- **`ImportGraph` must be complete before the first file is walked** (`Collector.SetImports`),
  because type keys are computed during the walk and an `import` in a module's second file would
  otherwise mis-key its first file's types.

## Per-module scopes

Each file is walked inside its own `ScopeModule` (`ModuleScopeFor`); a declaration always lands
there, and a `pub` one also lands in the global scope. `SymbolTable.Functions` and the backend's
`l.funcs` key private functions by module (`FunctionKey`).

**The entry file has its own module scope** (`SymbolTable.EntryScope`) — sharing the global scope
let an entry-file `let unwrapOr` rebind the prelude's program-wide. **Anything walking scopes for
one file starts from `EntryScope`, not `GlobalScope`** (LSP completion, definition, references,
rename, highlight; `findScopeAtPos` falls back to it).
