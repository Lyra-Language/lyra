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
**Every read of these maps goes through the `Lookup*` accessors, never by indexing them
directly** — a choke point, not sugar: with modules, which declaration a name means depends on
*which module is asking*, and a lookup scattered over 37 sites in 7 packages cannot be taught
that. Registration rejects a duplicate across modules (a type always did; `RegisterFunction`
used to overwrite silently, which with modules meant a program built against whichever module
was collected last), and the message names the other file.

**All three maps are keyed by `declKey`**, not by bare name: a declaration keeps its own name
when it is `pub` (or in the entry module), and gets `<module>::<name>` when it is **private**,
or when it takes a name the prelude exports — whatever its visibility, so the prelude keeps the
bare key for every module that did not shadow it. That is one rule for bindings, types and
traits (`FunctionKey` and `TypeKey` are two names for it), because "whose declaration is this"
does not depend on what kind of declaration it is, and a second copy of the rule is exactly the
drift hazard 4 warns about. Types and traits joined it on 08/01; before that their namespace
was program-wide, so two modules could not each declare a `Point` and a shadowed prelude type
was replaced for the entire program.

Which accessor a site wants is therefore **not a style choice**:

- `LookupTypeFrom` / `LookupTraitFrom` / `LookupFunctionFrom(name, loc)` — resolve as the file
  at `loc` sees it. This is what almost every pass wants. A bare read from inside a module that
  declares its own `Point` returns *another* module's.
- `LookupTypeIn` / `LookupTraitIn` / `LookupFunctionIn(module, name)` — resolve as a member of a
  named module (`shapes.Point`). These find a **private** declaration deliberately: the
  visibility check needs it in order to refuse it, and a lookup that hid it would report "no
  such member" for a name the module really does declare.
- `LookupType` / `LookupTrait` / `LookupFunction(name)` — the bare key only, i.e. a name that is
  program-wide. Correct for a caller that genuinely has no asking position.

A private declaration lands only in its own module's scope, so privacy is **structural** rather
than a post-lookup check — a reference from elsewhere does not find it. The cost is the message:
"unknown type" reads as a typo for a name the author can see in another file, so the typechecker
recovers `lyra-E028` on the not-found path (`reportPrivateType`, via `DeclaringModulesOf`).
`RegisterType`/`RegisterTrait` write the module scope **before** computing the key, because the
key is read out of that scope and types are registered mid-walk — there is no later point at
which it is already populated.

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
