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

**All three maps are keyed uniformly by `<module>::<name>`** (the entry module's empty path
gives `::name`), from the declaration's own file — `DeclKey`, the *identity* half. Resolution
is the other half: `declKey(name, loc)` answers with the identity key of the declaration
`name` at `loc` means, through the same rungs as the value scope chain — the asking module's
own declaration, its imports (following an alias to the source declaration), the prelude, and
finally any program-wide export (the rung that lets a value's type resolve past the import
boundary; a *written* name is gated separately, see `ResolvedReachably`). Until 08/27 the key
itself encoded visibility — bare when exported or in the entry module, qualified when private
or shadowing — so identity and resolution coincided through the shared bare key, and a lookup
asked with the wrong context *usually* worked; that near-correctness is what CLAUDE.md rule
4's four corollaries and the purity pass's two-month last-writer-wins bug were made of. It is
one rule for bindings, types and traits (`FunctionKey` and `TypeKey` are two names for the
resolution half), because "which declaration does this name mean" does not depend on what
kind of declaration it is. A name nothing resolves passes through unchanged — a guaranteed
miss for a real name, and load-bearing for the backend's synthetic instantiation symbols
(`Maybe$i64`), which share the keyspace and are registered raw.

Which accessor a site wants is therefore **not a style choice**:

- `LookupTypeFrom` / `LookupTraitFrom` / `LookupFunctionFrom(name, loc)` — resolve as the file
  at `loc` sees it. This is what almost every pass wants.
- `LookupTypeIn` / `LookupTraitIn` / `LookupFunctionIn(module, name)` — resolve as a member of a
  named module (`shapes.Point`). These find a **private** declaration deliberately: the
  visibility check needs it in order to refuse it, and a lookup that hid it would report "no
  such member" for a name the module really does declare.
- `LookupType` / `LookupTrait` / `LookupFunction(name)` — the **program-wide** meaning of a
  context-free name: the prelude's export, then any module's export (`GlobalScope`), then the
  entry module's declaration. Correct only for a caller that genuinely has no asking position.

`BindingIn(module, name)` is the same `In` form for the **binding** a function's `pub` lives
on, and exists for the same reason: its by-name sibling `BindingOf` finds the module through
last-writer-wins `ModuleOf`, so once two modules may each declare a `map` it answers about
whichever was collected last — which reported an imported module's exported function as
private to itself (08/08).

A private declaration lands only in its own module's scope, so privacy is **structural** rather
than a post-lookup check — a reference from elsewhere does not find it. The cost is the message:
"unknown type" reads as a typo for a name the author can see in another file, so the typechecker
recovers `lyra-E028` on the not-found path (`reportPrivateType`, via `DeclaringModulesOf`).
`RegisterType`/`RegisterTrait` write the module scope **before** registering, because
*resolution* reads that scope and types are registered mid-walk — there is no later point at
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

## Receiver-keyed overloading (`overload.go`)

A module may declare one name several times when every declaration takes a `self` receiver
and their receiver *type heads* differ (`types.HeadName`) — see the typechecker's README for
the language rule and `ast/overload_set.go` for what may form a set. Two things about it are
this package's business.

**A scope holds an `*ast.OverloadSet` where it would otherwise hold the declaration.** The
merge happens during the walk, when the second declaration of a name meets the first in its
module scope — the only moment both are in hand, and the moment the alternative (sequential
rebinding, `declarations/var_decl.go`) would otherwise silently keep one and drop the other.
So the scope is the authority; `Finish` mirrors the finished set into `OverloadSets`, rather
than the set being reconstructed from the registered members afterwards. Deriving it twice is
the drift invariant 4 exists to prevent.

That the set is what a lookup *returns* is deliberate: a consumer type-asserting to
`*VarDeclStmt` fails rather than taking whichever member came first. Choosing needs a
receiver type, and a pass without one should not choose.

**An overloaded name is absent from `Functions`.** That map answers "which declaration does
this name mean", and for a set there is no answer without a receiver. A member left under the
name's key would be silently right for one receiver and silently wrong for every other, so
the key holds no member and a by-name lookup reports the callee unresolved — which is the
conservative path every consumer already has. The passes that must do better read the
callee the typechecker resolved (`typetable.TypeTable.Callee`).

A set is one declaration with one key, and its members agree on `pub` —
`ast.OverloadableWith` refuses a set whose members disagree precisely so the export question
has an answer, since a half-exported set would be findable from another module for some
receivers and not others.

## `declKey` is not worth memoizing (measured 08/24)

Every `Lookup*From` goes through `declKeyIn`, from 54 call sites outside this package, on
the typechecker's hot path, re-run by the LSP on every keystroke. Each call runs a
prelude-shadow test, a walk of the module's imports asking `ModuleExports` of each, and a
module-scope lookup — and once collection has finished it is a pure function of
`(module, name)`. That reads like an obvious memo.

It was implemented — a `frozen` flag set by `Collector.Finish` after `PopulateImportScopes`
(the last thing that can change a key), with a `map[declKeyQuery]string` behind it — and
measured at **nothing**: −0.3% and −0.4% on a back-to-back run, inside noise. Hashing a
two-string key costs about what the computation it replaces costs. A CPU profile puts
`declKey` at 1.64% of the pipeline cumulatively, so that was the whole ceiling, and the
cache gives it back at the door.

It was reverted rather than kept, because it is not free to carry: the memo is only sound
while nothing registers or imports afterwards, which makes an ordering constraint between
`Collector.Finish` and this file that a future change could break silently — a wrongly
qualified key hides a declaration from every module including its own. A correctness
liability for a measured zero.

**What would actually move it** is not caching the key but not asking for it repeatedly:
resolving a reference to its declaration once, rather than recomputing a key at each
lookup. That is a different change, and a much larger one.
