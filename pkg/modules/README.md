# `pkg/modules` — import resolution, namespacing, the prelude

Resolves a program's import graph: `Resolve(entryFile, roots, opts) ([]Unit, []diag.Diagnostic)` turns
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

### A module is a file *or* a directory (08/07)

`std.prelude` is `std/prelude.lyra` **or** every `*.lyra` directly inside `std/prelude/`. Both
forms are the same module — one path, one namespace, one scope, one set of declaration keys —
so a module that outgrows a file splits without any of its declarations changing meaning.

**That equivalence is the feature, not a convenience.** Several rules are keyed on the
*module*: receiver-keyed overloading admits two `map`s only within one module, `pub` is exactly
the module boundary, prelude shadowing asks whether a name belongs to `std.prelude`, and
`SymbolTable.Imports` is keyed by module path. So the obvious alternative for a grown module —
split it into several modules — silently changes what its names mean, and for the prelude
specifically it would have un-done receiver overloading (`unwrap_or` for `Maybe` and for
`Result` would become a cross-module duplicate) and multiplied the shadowing rule by the number
of pieces. Splitting *within* a module leaves all of it alone.

Five details, each of which is a decision rather than an implementation accident:

- **Not recursive.** A subdirectory is the next module path down (`std/prelude/text/` is
  `std.prelude.text`), so recursing would swallow a module into its parent and make two
  spellings of a name mean the same thing.
- **Every file must declare its module**, and `checkHeader` enforces it. Membership by location
  alone would be less to type, but a file's own text would then no longer say which namespace
  its declarations join — in a namespace where a name may be a receiver overload of one three
  files away. A *single-file* module needs no header, since its path is its location; a header
  contradicting that location is still an error, because one of the two is wrong and picking
  which is not the compiler's to do.
- **Both forms in one root is an error, not a preference.** Which won would decide what half
  the program's names mean, and a reader looking at `std/prelude/strings.lyra` has no way to
  see that `std/prelude.lyra` beside it is quietly the real module. Across *different* roots
  there is no ambiguity — the earlier root wins, as everywhere else here.
- **Files are loaded in name order**, because a directory listing is not ordered on every
  filesystem and unit order feeds diagnostic order.
- **The entry file brings its module** (`entryGroup`): entering a compile at
  `std/prelude/strings.lyra` pulls in its siblings, or "the prelude compiles standalone" — the
  property that makes it an ordinary module — would hold only while it fitted in one file. The
  test is that the file sits in a directory *named by its own module path*; a file declaring
  `module app.util` in a directory called `src` is a single-file module that happens to have
  neighbours.

The overlay reaches into a module directory too (`isModuleDir`, `moduleFiles`), so an editor's
unsaved or never-saved file counts as a member of the module it declares — the same rule the
overlay already applied to a single-file module.

**One thing outside this package had to move with it.** Exports are recorded per *file*
(`collector.recordModuleBindings`), so a name that only becomes overloaded in a later file of a
module had already been exported as a bare declaration, and the set built when the second file
was walked collided with it — `symbol "area" already defined`. `exportToGlobal` now lets a set
supersede a global binding that is one of its own members. Within a single file the case never
arose, because the merge happens during the walk and both members export the same set object;
the shipped prelude never hit it either, because the prelude branch of that function discards
duplicate-definition errors outright. Two independent reasons the bug could not show up before
a user module spanned files.

### Roots, and the overlay (`roots.go`)
`DefaultRoots(entryFile)`, `DefaultOptions()` and `StdRoot()` answer "where does a compile look
for source?" once, for every front-end consumer. They lived in `cmd/lyrac` until the language
server needed the same answer and, not having it, analyzed each buffer as a lone unit with no
prelude — so every use of `Maybe`/`Some`/`Result` was an error in the editor while `lyrac check`
on the same file was clean. Two details in `StdRoot` each cost a debugging session and are
commented where they live: the root is the directory *containing* `std/`, and the executable's
path is symlink-resolved before its directory is taken (the LSP is normally reached through a
symlink on `PATH`).

`Options.Overlay` maps a filesystem path to in-memory source that **wins over the disk**, and
makes an overlaid path count as existing — that second half is what lets an import of a
never-saved file resolve rather than being reported missing. It exists for an editor, whose
buffer is by definition not what is on disk; keys are normalized with `filepath.Clean` on entry
so a later lookup is a plain map hit.

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

### An import's member list restricts visibility (08/18)

`import lib.{ listed }` admits `listed` and nothing else — not `lib`'s other exports, and not
its types. Until this landed the rule was "any `pub` name of any module you imported at all",
and the member list drove only the namespace binding and `lyra-W004`.

**It was architectural, not a missing check.** `exportToGlobal` put every `pub` declaration into
one global scope that sat on every module's parent chain, so *exported* and *visible* were the
same thing and no per-reference check was consulted. The fix is a scope, not a pass — an
attempt at a checker pass is recorded in `todo.md` as unworkable: reusing `collectRefsByFile`
looks right but it is a **syntactic** walk with no scope information, so it cannot tell a local
binding from a module member, and it reported a callback parameter named `f` in the prelude's
own `array.lyra` as belonging to a user module that happened to declare a top-level `f`.
Over-collection is safe for "is this name mentioned anywhere" and unsound for "what does this
name resolve to".

The chain is now **module → imports → prelude**, and it *stops there*:

- `SymbolTable.ImportScopeFor(module)` holds what that module's imports bring in.
  `PopulateImportScopes` fills it in `Collector.Finish`, which is the earliest it can run — a
  module's imports resolve against other modules' exports, and exports are recorded per file.
- `PreludeScope.Parent` is **nil**. GlobalScope is still written and still read, but for a
  different question: it is the program-wide registry that makes two modules exporting one name
  an error, and it is what `ExportingModule` consults to turn "undefined" into *"module `lib`
  exports it, but this file does not import it"*.
- Only a **selective** import binds a bare name. `import lib` binds `lib.listed` and nothing
  else: if it also admitted bare `listed`, the two import forms would mean the same thing. An
  alias binds only its local name.

**A type needed its own gate**, and the asymmetry is worth remembering: a value resolves through
the scope chain, so the imports scope gates it structurally, while a type goes through the
Types/Traits maps keyed by `declKey` — which answers "whose declaration is this" and says
nothing about who may see it. `importedAt` is that gate, and it asks the module of the
declaration's **file**, never `ModuleOf[name]`, which is last-writer-wins (invariant 4).

**UFCS is exempt**, structurally rather than by a rule: `b.doubled()` resolves against the
receiver's type, so a method the receiver justifies needs no import of the free function.

One diagnostic had to learn the difference. `reportPrivateType` fired for any name another
module declared and this one could not resolve; with visibility restricted, an *exported* name
that was simply not imported took that path and was reported as private — telling an author to
add a `pub` that was already there.

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
test suite) must still build. **The prelude does not import itself** — compared by *module
path*, so entering at one file of the multi-file prelude is recognised as being the prelude
just as entering at a single-file one was — so it can be compiled and
tested like any other module, and `LYRA_NO_PRELUDE` disables it outright for bootstrapping. And
**a user declaration may take a prelude name**: it warns (`lyra-W012`) and the local declaration
wins, rather than erroring the way two user modules' clash does — otherwise every name the
prelude exports would be permanently unusable, and adding one later would break programs that
never mentioned it.

**An imported name is shadowable on exactly the same terms** (08/08). `import util.seq`
(which exports a `map`) plus an ordinary `let map = …` used to be a hard error — *function
"map" is already defined at …/util/seq.lyra* — while the identical declaration over the
**prelude's** `map` merely warned and won. The explicit act punished and the implicit one
forgiven, and the only reading a user could take from it is that importing a module forbids
them a name, which is not a rule this language means to have.

It is one rule now (`noteAmbientShadow`): a module's own top-level declaration of a name
reaching it from elsewhere — the prelude's, or one exported by a module it imports — warns
(`lyra-W016`, W012's sibling) rather than erroring. Every declaration has a key of its own
(`<module>::<name>`, uniformly), and resolution tries the module's own declaration first, so
the local one wins every bare reference in that module, the shadowed one is still reached
through the namespace the import already binds (`seq.map`), and no other module is affected.

What stays an error is a **second claim on the program-wide name**: two modules exporting
one name, whether or not one imports the other, since a bare reference from a third module
could mean either and neither has a local declaration obviously meant to win. That is
`exportToGlobal`'s check, untouched. Names with no receiver are also still the reason the
key-level fix matters at all — receiver dispatch (08/03–08/04) disambiguates the names that
have something to dispatch on, and this settles the rest.

One ordering constraint comes with it, and it is why `ImportGraph` exists. A type's key is
computed **during** the walk, so the graph has to be complete before the first file is
walked (`Collector.SetImports`, called beside `SetPreludeModule` for the same reason).
Assembling it per file as each is walked works for a single-file module and quietly fails
for the rest: a module whose `import` sits in its second file would key the first file's
types as though nothing were imported, and every later lookup — with the graph complete —
would compute a key that misses them. The other two inputs need no such help:
`ModuleDeclares` is true from the moment a declaration lands in its own module's scope, and
`ModuleExports` asks the *imported* module, which `Resolve` returns in dependency order.

It surfaced two live bugs that a prelude shadow could already reach and nothing had, both
the by-name form of hazard 4 — a module question answered from a name:

- **The `pub` check on a namespace member read the wrong binding.** `visibilityIn` fell
  through to `BindingOf(name)`, which finds the module through last-writer-wins `ModuleOf`,
  so `seq.map` looked up the *entry* file's `map`, found no `pub`, and reported an exported
  function as *private to its own module*. `BindingIn(module, name)` is the binding half of
  `LookupTypeIn`/`LookupTraitIn`, which the same function had been using two lines above.
- **The backend could not lower the namespace call at all.** `namespaceCallee` tested
  membership with `DeclaringModule` and took the callee from `l.funcs[name]`, both sound
  only while a top-level name was program-wide unique. With a shadow in play the membership
  test rejected the call and it fell through to `llvm: unsupported method call` — a backend
  failure on a program the front end had checked clean, which is rule 5 inverted. It asks
  `ModuleDeclares` and keys from the **declaration** it resolved to.

Both were reachable before 08/08 by shadowing a *prelude* name and then calling into a
module through a namespace; nothing did both at once, so nothing found them.

**A shadow reaches exactly as far as the module that declared it** (07/30). The prelude's
exported *bindings* live in **`SymbolTable.PreludeScope`**, which sits **between** every module
scope and the global one, so a lookup runs module → prelude → global. That middle position is
the whole design: the prelude has to be reachable from every module, a module's own declaration
has to beat it, and one module taking the name must not change what it means anywhere else.
Exporting the prelude into the global scope instead — where every module's exports live and
every module falls through — is what made a shadow program-wide: the first module to declare
`unwrapOr` handed its own to everybody, and a **private** one was worse, because withdrawing the
prelude's registration to make room deleted the name for every module at once. Every
declaration has its own module-qualified key, and resolution (`FunctionKey`, asked from the
*referencing* location) tries the asking module's own declaration before the prelude's, so a
shadow reaches exactly as far as the module that declared it and every other module still
finds the prelude's. Consequently `ownership`/`use_after_move` resolve a callee through
`LookupFunctionFrom`, not a bare `Functions` read — those passes take the callee's parameter
modes as a memory-safety decision, and a bare lookup hands back another module's function.

**Types and traits reach exactly as far too** (08/01). They were the exception until then:
`SymbolTable.Types` was keyed by bare name, so a program had exactly one `Maybe` and the
shadowing declaration was it — `noteShadowed` withdrew the prelude's entry outright. The
reachable consequence was a module that never mentioned `Maybe` losing the canonical one and
reporting `` `?` operand must be a Result or Maybe, got Maybe `` about a declaration it had
never seen, and two modules being unable to each declare a private `Point`. (That message is
gone as of 08/03: the shadowing module still gets an error — its `Maybe` is genuinely not the
canonical one — but it now names the shadow and the fix. See `collector/README.md`.)

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
