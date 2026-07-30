# `pkg/ast/symbols` — scopes and the symbol table

Lexical `SymbolTable` with a tree of `Scope` nodes:

- Scope kinds: `ScopeGlobal → ScopePrelude → ScopeModule → ScopeFunction → ScopeBlock /
  ScopeLoop`. That order is the resolution order, read from a name outward: a module's own
  declarations, then the prelude's ambient ones, then every module's `pub` exports.
  `PreludeScope` sitting *between* the modules and the global scope is what confines a prelude
  shadow to the module that declared it (see Modules and namespacing); the entry file has a
  `ScopeModule` like any other module (`EntryScope`), so a per-file scope walk starts there, not
  at `GlobalScope`
- `PopScope` stops at a module/global/prelude scope rather than walking out of it — an
  unbalanced pop used to be harmless when the global scope was the root
- `Scope.Define(Named)` — adds to current scope, errors on duplicate
- `Scope.Lookup(name)` — walks up the scope chain
- `Scope.LookupLocal(name)` — current scope only
- `ScopeTable` — a node→`*Scope` map (`Set`/`Get`) letting a later pass recover the scope a
  given AST node introduced without re-walking. The collector records: block expressions
  (`ScopeBlock`), if-let statements (the `Then` scope), lambda parameter scopes
  (`ScopeFunction`, on the `*ast.LambdaExpr`), both loop forms (`ScopeLoop`), and `with` blocks
  (`ScopeBlock`). Consumed by the typechecker's `enterScope` and, since 07/10/26, by the purity
  checker's `scopeFrames.forLambda` for lambdas/free functions (trait-method clauses still
  reconstruct by AST walk — they have no recorded scope; see `todo.md` FP/Imperative #3, Phase
  2).
**Every read of these maps goes through `LookupType`/`LookupTrait`/`LookupFunction`, never by
indexing them directly** — a choke point, not sugar: with modules, which declaration a name
means depends on *which module is asking*, and a lookup scattered over 37 sites in 7 packages
cannot be taught that. Registration rejects a duplicate across modules (a type always did;
`RegisterFunction` used to overwrite silently, which with modules meant a program built against
whichever module was collected last), and the message names the other file.

- `SymbolTable.Types` / `.Functions` — flat maps for fast global lookup by name; `.Functions`
  (and its `.PureFuncs` subset, the explicitly-`pure`-declared ones) is populated only for
  top-level `let`/`var name = <lambda>` bindings — a nested same-named function is deliberately
  not registered here (a name-keyed flat map can't disambiguate it from an unrelated binding
  elsewhere). The ML-style function sugar (`let name(params) => body`, no `=`) is the same
  binding — the grammar stores its lambda in the `value` field — so it registers identically.
  Modifiers may also lead the name (`let pure name(params) => body`); the collector
  (`declarations/var_decl.go` `applyFunctionModifiers`) lifts them off the declaration's
  `modifiers` field onto the `LambdaExpr`, so all three spellings yield the same AST.

Destructuring-declaration names (`let (a,b) = …`, `let {x} = …`, `if let [a,b] = … { }`, `let
{x} = … else { }`) are registered via `RegisterDestructuredName(name, decl)`, which maps each
bound name to its owning `*ast.DestructuringDeclStmt` (so the binding's `var`/`let mut`
mutability is recoverable). The typechecker later overwrites that placeholder with a typed
synthetic `VarDeclStmt` once it infers each leaf's type, via `checkDestructuringDecl` — shared
by plain `let`/`var`, `if let`, and `let … else`. Scoping differs by form: plain `let`/`var`
registers into the current scope; `if let … { Then } else { Else }` registers into a scope
pushed around `Then` only (names visible there via the parent chain, not in `Else`); `let … = v
else { Else }` registers into the *enclosing* scope only after collecting the diverging `Else`
block, so `Else` never sees the names but code after the statement does.
