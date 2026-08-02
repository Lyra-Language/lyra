# `pkg/modules` — import resolution, namespacing, the prelude

Resolves a program's import graph: `Resolve(entryFile, roots) ([]Unit, []diag.Diagnostic)` turns
an entry file into the ordered source units a compile needs, following `import` statements
transitively. Module paths map to files **by directory convention** — `std.io` is `std/io.lyra`
beneath a search root — so a module's name and location agree by construction with no manifest
to keep in sync; roots are searched in order (the entry file's own directory, then `LYRA_STD` or
a `std/` beside the compiler binary), which is what lets a project's modules take precedence
over the standard library. Units come back **dependency-first** so a module's diagnostics
precede its dependents'; a module reached from two places is emitted once. Import **cycles are
rejected** (`lyra-E027`) rather than broken at an arbitrary edge — with no lazy or partial
initialization semantics defined, "which half observes the other" has no predictable answer —
and an unresolvable import (`lyra-E026`) names the paths it tried, since the likely mistake is a
misplaced file. The import graph must be known *before* collection (every unit is collected into
one program), so `scan.go` reads the two module constructs straight off the CST rather than
going through the collector.

**Deliberately out of scope**, none of it changing what a module's source looks like: package
management, versioning, and separate/incremental compilation.

### Modules and namespacing
`import util.math` binds a **namespace** under the path's last segment (or its `as` alias), so
the module's contents are reached as `math.double(…)`; `.{ X, Y as Z }` binds the listed names
directly. Resolution lives in `typechecker/typechecker_modules.go` (`moduleMemberType`) and the
backend's `namespaceCallee`, and both share three rules worth knowing:

- **The namespace check runs before the object is inferred.** A namespace is not a value, so
  inferring `math` as an expression would report it as an undefined identifier and the real
  resolution would never be reached.
- **Which module is asking comes from the node's `Location.File`.** That is what let namespacing
  land without threading a module context through every pass — the same field that gives
  diagnostics their file.
- **Membership is checked, not assumed.** Top-level names are program-wide unique (a
  cross-module duplicate is rejected by `RegisterType`/`RegisterFunction`/`RegisterTrait`), so a
  bare lookup would happily resolve `math.secret` to *another* module's `secret` — binding a
  reference the source never made, silently. The backend repeats the check rather than trusting
  the front end, per its standing rule that it errors rather than guesses.
- **A namespace call resolves to the callee's *declaration*, not to its signature.** A generic
  callee's type variables are free until a call's arguments solve them, so checking
  `opt.wrap(7)` against the declared `(v: t) -> Opt<t>` rejected it ("cannot assign integer
  literal to t") — while `import util.opt.{ wrap }` and the same function called in its own
  module both worked, since those go through `inferIdentifierCall` → `inferGenericCall`.
  `moduleMemberType` therefore returns the `*ast.LambdaExpr` alongside the type, and
  `inferMemberCall` hands it to the same `inferLambdaCall` a direct call uses. The backend needs
  the matching half: a generic function has no emitted body of its own, so `namespaceCallee`
  asks `specializedFuncFor(call)` **before** `l.funcs` — which holds only functions emitted as
  themselves, and so returned nothing, dropping the call out of the namespace path entirely to
  die as `unsupported method call`. Both halves are load-bearing and separately mutation-tested
  (`modules/generic_namespace_call_test.go`, `backend/llvm/llvm_module_call_test.go`).

A local binding **shadows** a namespace, so `math.double` is an ordinary field read when `math`
names a value.

**`pub` is enforced across module boundaries** (`lyra-E028`, 07/30). The rule is exactly the
boundary: within a module everything is visible, and `pub` is what crosses — enforcing it
*inside* a module would make a private helper unusable by the module that declared it.
`visibilityOf`/`checkVisible` (`typechecker_modules.go`) are the one check, wired at the three
places a cross-module reference resolves: a namespace member, `resolveType` (the single point
every named type reaches its declaration), and a **bare call**. The last is not optional —
top-level names still share one namespace, so `helper()` reaches another module's function
without ever naming the module; checking only namespace access would leave private functions
private in name only. A single-file program is unaffected: its file and its names both have no
module, so every reference is same-module by definition.

Two things `pub` needed first. `VarDeclStmt` had **no `IsPublic` field** — the grammar has
always allowed `pub` on a binding, but it was never collected, so every top-level binding was
implicitly public the moment modules arrived. And visibility is spelled `optional($.visibility)`
rather than a labelled field, so it is an *anonymous child*: `ChildByFieldName("visibility")`
returns nil silently, which would have made every binding private instead of failing loudly (the
struct and tuple collectors already scan for it by kind, which is why they worked). **The
prelude is implicitly imported** (`modules.PreludeModule` = `std.prelude`, 07/30). It is an
*ordinary module* — `pub` exports, resolved through the same roots — that the compiler happens
to import for you, rather than a set of names baked in; that is what lets it be read, tested,
and replaced like any other code. It is resolved ahead of the entry file's own imports and its
names are available **unqualified** (`prelude.Maybe` would defeat the purpose), which is the one
exception to the namespace rule and the reason ambient-ness is a concept the module system needs
regardless.

Three rules keep it from being hostile. **A missing prelude is not an error** — the standard
library is found by searching the roots, and a program compiled where there is none (most of the
test suite) must still build. **The prelude does not import itself**, so it can be compiled and
tested like any other module, and `LYRA_NO_PRELUDE` disables it outright for bootstrapping. And
**a user declaration may take a prelude name**: it warns (`lyra-W012`) and the local declaration
wins, rather than erroring the way two user modules' clash does — otherwise every name the
prelude exports would be permanently unusable, and adding one later would break programs that
never mentioned it.

**A shadow reaches exactly as far as the module that declared it** (07/30). The prelude's
exported *bindings* live in **`SymbolTable.PreludeScope`**, which sits **between** every module
scope and the global one, so a lookup runs module → prelude → global. That middle position is
the whole design: the prelude has to be reachable from every module, a module's own declaration
has to beat it, and one module taking the name must not change what it means anywhere else.
Exporting the prelude into the global scope instead — where every module's exports live and
every module falls through — is what made a shadow program-wide: the first module to declare
`unwrapOr` handed its own to everybody, and a **private** one was worse, because withdrawing the
prelude's registration to make room deleted the name for every module at once. A shadowing
declaration also gets a module-qualified `FunctionKey` whatever its visibility, so the prelude
keeps the bare key and every module that did not shadow still finds it; the backend and the
ownership pass already ask for that key from the *referencing* location, so nothing else had to
learn about the prelude. Consequently `ownership`/`use_after_move` resolve a callee through
`LookupFunctionFrom`, not a bare `Functions` read — those passes take the callee's parameter
modes as a memory-safety decision, and a bare lookup hands back another module's function.

**Types and traits reach exactly as far too** (08/01). They were the exception until then:
`SymbolTable.Types` was keyed by bare name, so a program had exactly one `Maybe` and the
shadowing declaration was it — `noteShadowed` withdrew the prelude's entry outright. The
reachable consequence was a module that never mentioned `Maybe` losing the canonical one and
reporting `` `?` operand must be a Result or Maybe, got Maybe `` about a declaration it had
never seen, and two modules being unable to each declare a private `Point`.

Both were the same missing piece — per-module type *identity* — and both are closed by keying
`Types`/`Traits` with the same `declKey` bindings already used, so nothing new was invented:
a private or prelude-shadowing declaration is filed under `<module>::<name>` and the two
coexist. `noteShadowed` now only records the warning. Three things had to move with the key:

- **Registration.** `RegisterType`/`RegisterTrait` write the module scope *before* computing
  the key (the key is read out of that scope, and types register mid-walk), and no longer
  define into the global scope — publication is `exportToGlobal`, the same path a `pub` binding
  takes, so a private type never competes for a program-wide name.
- **The typechecker's resolution cache.** `resolvedTypes` was keyed by bare name, which would
  have let the first module to mention a type answer for every other one — the hazard that had
  already kept the visibility check out of that cache. It is keyed by the resolved key now.
- **The backend.** `l.structTypes` is keyed the same way, and the lowerer carries the position
  it is lowering from (`currentLoc`, set per type definition and per function body) because the
  resolved types reaching `lowerType` carry no location of their own. Without it two modules'
  private `Point`s emitted one `%Point` holding the union of both field lists, which clang
  rejected outright. A generic instantiation's symbol (`Box$i64`) is qualified the same way.

One consequence worth knowing: the `pub` check now asks about **the declaration a reference
resolved to** (`declVisibility`), not about whichever declaration shares its name.
`visibilityOf` went through `DeclaringModule`, which is last-writer-wins, so with two private
`Point`s it answered for whichever module was collected last — module one's own `impl Size for
Point` reported *its own* type as private to module two. That is the identical mistake the bare
**call** path made before privacy became structural (see the end of this file), and it has the
same fix. A namespace member likewise asks `visibilityIn(imp.Path, …)`, about the module the
import names.

Two ordering traps that shape the implementation, both worth knowing before touching it. The
prelude module must be named *before* any file is walked, since type registration happens during
the walk. And prelude names are tracked in their own set rather than inferred from `ModuleOf`,
which is last-writer-wins: a user module declaring a prelude name overwrites that entry, so by
the time functions are registered (in `Finish`, after every file) `ModuleOf` no longer remembers
the prelude had it. `ModuleDeclares` is the precise form of the same question — a module's own
scope holds every top-level declaration it made, and nobody else's — and is what
`shadowsPrelude` and namespace-member resolution ask. Withdrawal of a shadowed type is likewise
guarded on the entry *actually* belonging to the prelude — deleting by name alone removed the
user's own declaration at the `Finish` stage, leaving a program reporting its own function
undefined.

**Names resolve per module.** Each file is walked inside its own `ScopeModule`
(`ModuleScopeFor`), so every scope the walk creates parents under it — that is the keystone:
without it a function body's chain ran straight past its own module and a module could not see
its own private declarations. A declaration always lands in its module's scope; only a `pub` one
*also* lands in the global scope, so a private name is invisible outside its module and never
competes for the global name. Two modules may therefore each declare a private `helper`, with
`SymbolTable.Functions` and the backend's `l.funcs` both keying a private function by module
(`FunctionKey`, asked for from the *referencing* location so front end and backend cannot
disagree about which declaration a name means).

The **entry file gets a module scope of its own** (`SymbolTable.EntryScope`), even though it
declares no module path. It briefly shared the global scope, on the grounds that a program root
has nothing to be private from — true of privacy, but it also put the entry file's declarations
in the scope every *other* module falls through to, so an entry-file `let unwrapOr` rebound the
prelude's for the whole program. The cost is one thing to remember: **anything walking scopes
for a single file starts from `EntryScope`, not `GlobalScope`** — the LSP's completion,
go-to-definition, references, rename and highlight walks all did the latter, which is exactly
why sharing the global scope looked like the simpler choice. `findScopeAtPos` takes that scope
as its top-level fallback.

Privacy is now enforced **structurally** rather than by a check: a name that resolves is either
this module's own or one the global scope holds, and the global scope holds only exports. The
post-lookup visibility check in `inferIdentifierCall` had to be removed — it asked whether
*some* declaration of that name was private (through a name-keyed, last-writer-wins module map)
instead of whether *the declaration the reference resolved to* was visible, so two modules each
declaring `helper` made one module's call to its own function report the other's privacy.
`lyra-E028` survives on the not-found path, where "not yours to call" is still a better message
than "undefined".
