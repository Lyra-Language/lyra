# `pkg/ast/symbols` — scopes and the symbol table

## Scopes

- Kinds: `ScopeGlobal → ScopePrelude → ScopeModule → ScopeFunction → ScopeBlock / ScopeLoop`. Read
  outward, that is resolution order: the module's own declarations, its imports
  (`ImportScopeFor`), the prelude. See [`pkg/modules`](../../modules/README.md) for the chain.
- The entry file has its own `ScopeModule` (`EntryScope`); **a per-file scope walk starts there,
  not at `GlobalScope`**.
- `PopScope` stops at a module/prelude/global scope.
- `Scope.Define(Named)` errors on duplicate; `Lookup(name)` walks the chain; `LookupLocal(name)` does
  not.
- `ScopeTable` maps an AST node to the `*Scope` it introduced: block expressions, `if let` (the
  `Then` scope), lambda parameters (`ScopeFunction`, on the `*ast.LambdaExpr`), both loops, `with`
  blocks. Read by the typechecker's `enterScope` and purity's `scopeFrames.forLambda`; trait-method
  clauses have no recorded scope.

## Declaration keys and lookups

**Never index `Types`/`Functions`/`Traits` directly — use the `Lookup*` accessors.** Which
declaration a name means depends on which module is asking.

All three maps are keyed **`<module>::<name>`** (entry module: `::name`) from the declaration's own
file — `DeclKey`, the identity. `declKey(name, loc)` is resolution: the asking module's own
declaration, then its imports (aliases followed), the prelude, then any program-wide export (so a
value's type resolves past the import boundary; a *written* name is gated separately by
`ResolvedReachably`). One rule for bindings, types and traits (`FunctionKey`/`TypeKey` name the
resolution half). An unresolvable name passes through unchanged, which the backend's synthetic
instantiation symbols (`Maybe$i64`) rely on.

Which accessor is a correctness question:

| Accessor | Meaning |
|---|---|
| `LookupTypeFrom` / `LookupTraitFrom` / `LookupFunctionFrom(name, loc)` | as the file at `loc` sees it — what almost every pass wants |
| `LookupTypeIn` / `LookupTraitIn` / `LookupFunctionIn(module, name)` | a member of a named module; **finds private declarations** so the visibility check can refuse them |
| `LookupType` / `LookupTrait` / `LookupFunction(name)` | program-wide meaning: prelude export, then any module's export, then the entry module — only for callers with no asking position |
| `BindingIn(module, name)` | the binding a function's `pub` lives on; never `BindingOf`, which uses last-writer-wins `ModuleOf` |

- Registration rejects a cross-module duplicate and names the other file.
- Privacy is **structural** (a private declaration lives only in its module scope); the typechecker
  recovers `lyra-E028` on the not-found path (`reportPrivateType`, `DeclaringModulesOf`).
- **`RegisterType`/`RegisterTrait` write the module scope before registering**, because resolution
  reads that scope and types register mid-walk.
- **Don't memoize `declKey`.** Measured: no gain, and the memo is sound only if nothing registers or
  imports after `Collector.Finish` — an ordering constraint that fails silently. The real
  improvement would be resolving each reference to its declaration once.

## `Functions` and destructured names

- `Functions` (and its `PureFuncs` subset) holds only **top-level** `let`/`var name = <lambda>`
  bindings; nested same-named functions are not registered. The sugar forms (`let name(params) =>
  …`, `let pure name(…)`) register identically — `applyFunctionModifiers`
  (`declarations/var_decl.go`) lifts leading modifiers onto the `LambdaExpr`.
- Destructured names (`let (a, b) = …`, `if let`, `let … else`) register via
  `RegisterDestructuredName(name, decl)` pointing at the `*ast.DestructuringDeclStmt`; the
  typechecker's `checkDestructuringDecl` later replaces it with a typed synthetic `VarDeclStmt`.
  Scoping: plain `let`/`var` → current scope; `if let` → a scope around `Then` only; `let … else` →
  the enclosing scope, registered *after* collecting `Else`.

## Receiver-keyed overloading (`overload.go`)

Rule and set formation: the typechecker's README and `ast/overload_set.go`.

- **A scope holds an `*ast.OverloadSet`** in place of the declaration. The merge happens during the
  walk when the second declaration meets the first; `Finish` mirrors sets into `OverloadSets`
  rather than rebuilding them. A lookup returns the set, so a consumer asserting `*VarDeclStmt`
  fails instead of picking a member.
- **An overloaded name is absent from `Functions`** — there is no answer without a receiver. Passes
  that need the member read `typetable.TypeTable.Callee`.
- Members agree on `pub` (`ast.OverloadableWith`).
