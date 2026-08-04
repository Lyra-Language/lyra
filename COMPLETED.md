# Lyra — Completed

The dated record of what has been built and, more to the point, **why it ended up the
way it did**: the constraint that forced a design, the measurement that disproved a
diagnosis, the bug a change turned out to be fixing. Open work lives in
[todo.md](todo.md); this file is the archive it points back to.

Newest first.

## Dated log

### 08/04/26
**A method call resolves against every declaration of the name the file can reach, not
against the one its key resolves to.** `ufcsFunction` now gathers candidates by name
(`SymbolTable.FunctionsNamed`) and filters them by the three things that actually decide a
method call — it takes a `self` receiver, this file can reach it, and it accepts this
receiver — instead of resolving the name to one declaration and asking whether *that* one
fits.

**The bug it fixes had no diagnostic, which is the whole reason it is worth recording.**
Adding `map` for `Result` to the prelude, while `std.maybe` still declared `map` for
`Maybe`, did not produce an error — it silently removed a method. The prelude keeps bare
declaration keys, so its `map` took the program-wide one; that flipped
`shadowsPrelude("std.maybe", "map")` to true, which pushed `std.maybe`'s `map` onto a
qualified key; and the UFCS rung consulted exactly one candidate by name, so `m.map(f)` on
a `Maybe` found the `Result` overload, failed to match the receiver, and reported "member
access on non-struct type Maybe<i64>". Two features that are each individually correct,
composing into a method that quietly was not there. Nothing was ambiguous and nothing was
shadowed in a sense a reader would recognise — one lookup could not see the other
declaration, and the failure surfaced three layers away as a type error about the receiver.

Confirmed against the commit *before* receiver-keyed overloading landed, with only `map`
added to the prelude: identical failure. So this is the cross-module name-claiming bug
recorded under todo.md's Modules section, not a fault in that feature — which matters,
because the tempting reading was that overloading had broken UFCS.

**What did not change is as deliberate as what did.** The **import requirement** still
gates reachability: gathering every declaration of a name must not quietly become "every
`pub` function in the program is a method on values of its type", which is the design that
was explicitly declined when UFCS landed. An unimported module's same-named method stays
unreachable, and there is a test that fails if that stops being true.

Two decisions inside the new rule:

- **A file's own module wins a tie.** Two reachable candidates accepting one receiver is
  otherwise ambiguous, and the module doing the asking is the one whose intent is least in
  doubt — the same precedence the scope chain applies everywhere else. This is what lets a
  file declare its own `map` for `Maybe` over the prelude's, which is the soft-shadowing
  rule the prelude already promised for bindings.
- **A tie that survives that is reported, not broken.** Candidates are gathered from a map
  of modules, so "whichever came first" would not be stable between runs — a call whose
  meaning depends on iteration order is worse than one that refuses. The message names the
  modules and suggests a qualifier the reader can actually type
  (`` `dup.map(m, …)` ``), taken from the import's own namespace alias; a test asserts the
  suggestion, and it was checked by running it.

One thing this turned up on its own: the **entry module** takes bare keys like any other,
*except* when it declares a name the prelude also has — `declKeyIn` then qualifies it to
`::name`, a key nothing else produces. Skipping the empty module path while enumerating
candidates therefore hid a file's own declaration from its own call, which is how the
local-wins test first failed.

Also fixed here, and caused by the overloading work rather than found by it: **LSP
completion dropped every overloaded name.** `ufcsCompletions` enumerates module scopes and
type-asserted each symbol to `*ast.VarDeclStmt`, so a scope holding an `*ast.OverloadSet`
was skipped — `m.` stopped offering `map`, `flat_map`, `unwrap_or` and `unwrap_or_else` the
moment those became sets. It now offers each member and keeps the one the receiver can call,
so the name appears once, described by the overload that applies.

**Still open, and narrowed:** only the *method* form resolves this way. A **bare call**
(`map(m, f)`) still goes through the scope chain, where the prelude shadows an imported
module's name, and two modules exporting one name still collide on the bare key. The
receiver is available there too — it is argument 0, which is the premise of the UFCS desugar
— so extending the same gathering to `inferIdentifierCall` is the natural next step. The
key-level fix in todo.md is what settles the names with no receiver at all.

### 08/04/26
**`std.maybe` folded into the prelude and deleted.** `map`, `flat_map` and `filter` for
`Maybe` moved beside the `Result` ones; the standard library is a single module again.

The split existed for one reason, and receiver-keyed overloading removed it the day before:
a name could be declared only once per module, so `Maybe` and `Result` could not both have a
`map` written in one place. `map` and `flat_map` are now overload sets exactly as
`unwrap_or`/`unwrap_or_else` already were. `filter` stays `Maybe`-only and single — rejecting
an `Ok` would have to invent the `e` to fail with, which only the caller can supply, and that
is `ok_or`'s job.

The user-visible half: the combinators need **no import at all** now, so the shipped-prelude
tests dropped their `import std.maybe`. Comments across the checker, typechecker and LSP that
described the split as current structure were corrected; the dated entries in this file are
left as the record of what was true when written.

### 08/03/26
**Receiver-keyed overloading: one module may declare a name several times, if the
declarations differ in what they take as `self`.** The prelude now declares `unwrap_or` and
`unwrap_or_else` twice each — once for `Maybe<t>`, once for `Result<t,e>` — so the two types
share a vocabulary instead of the second one getting a name it did not need.

This is the **declaration-side half of UFCS**, and only makes sense read against it. UFCS
(earlier today) made `m.map(f)` dispatch on the receiver's type, which settled *call* sites;
what it did not touch is that a second `let map` in one module was a redeclaration error. So
the two halves of "`Maybe` and `Result` both want `map`" had opposite answers: reachable
from a call, unwritable in a module. That is the whole reason the standard library split
`maybe.map` from `result.map` into separate modules, and the reason putting either in the
prelude claimed the name `map` for one type forever.

**The rule is the receiver's *head*** — the type constructor with its arguments dropped
(`types.HeadName`, one definition, shared with the backend's symbol mangling). `Maybe<t>`
beside `Result<t,e>` is two heads and is admitted; `Maybe<i64>` beside `Maybe<string>` is one
head twice and is refused **where it is written**. That refusal is the design decision worth
recording: two members that can both match a receiver need a specificity ordering to rank,
and the language has none — so the choice was between inventing one and forbidding the
overlap. Forbidding it makes the error a fixed thing in one place, with the clashing receiver
named, instead of an ambiguity reported at every call site that the author cannot resolve
without changing a declaration anyway. Relaxing it later means *adding* an ordering, not
reinterpreting anything. A bare type variable (`self: t`) has no head at all — it accepts
every receiver — so it can never be one candidate among several, which is the case the head
rule has to reject to stay coherent rather than a case it happens to miss.

**Resolution is in one place because the desugar had already earned it.** UFCS rewrites
`m.f(x)` to `f(m, x)` before anything downstream runs, so the receiver is argument 0 whichever
way the call was written, and one predicate (`receiverAccepts` — `unifyGenericTarget` again,
the same one trait dispatch and UFCS use) serves both spellings. The UFCS rung still has to
resolve *before* desugaring, since it is what decides whether `m.f` is a method call at all:
asking an arbitrary member would answer "no method" for a receiver a different member accepts.

**Where the cost actually landed: four passes that resolved a callee by name.** Ownership,
use-after-move, purity and the backend each looked a callee up in the symbol table to read its
parameter modes, and that question has no answer for an overloaded name. Two ways to fix it,
and the tempting one is wrong — re-deriving dispatch in each pass, from a receiver type each
would have to recover, is three more chances to disagree with the front end about which
function a program calls. So the pass that *did* resolve it publishes the answer
(`typetable.TypeTable.SetCallee`), which is exactly what `MethodTable` already does for trait
dispatch, and each consumer reads it first and falls back to name lookup. **Only overloaded
calls are recorded**: filling it in for every call would be a second answer to a question the
symbol table already answers, and a second answer is a thing that can drift.

Two structural choices that make the omissions loud rather than silent:

- **A scope holds an `ast.OverloadSet` in place of the single declaration**, so every pass
  that type-asserts a looked-up symbol to `*VarDeclStmt` now *fails* instead of quietly taking
  a member. Picking one needs a receiver; a pass without one has no business picking, and a
  failed assertion sends it down its not-found path where the worst case is a missing feature.
  This is CLAUDE.md hazard 8's reasoning run in the other direction — make the gap visible on
  purpose.
- **An overloaded name is absent from `SymbolTable.Functions` entirely**, for the same reason.
  Leaving a member under the bare key would have made every existing reader silently correct
  for one receiver and silently wrong for the rest.

**It turned up a shipped miscompile in the same code path.** The backend wrote `funcParams`
under the module-qualified key a private declaration gets and read it back under the *bare*
name, so a private function's parameter list came back empty — and with no parameters to
consult, `paramIsByRef` is never asked and a `mut` argument is passed **by value where the
callee expects an address**. A private function taking `mut` and called from inside its own
module segfaults; the arity guard that might have caught it is skipped by the same empty list.
Fixed here because overloading made the keying load-bearing, and pinned by
`TestExec_PrivateMutParamPassedByReference`, which was confirmed to fail (exit -1, a signal)
against the unfixed backend rather than merely asserted to.

Also closed a gap the tests had: nothing analyzed the **shipped** `std/prelude.lyra` — every
prelude test built its own fixture — so the real file could have stopped compiling with the
suite still green. `TestPrelude_ShippedStdlibAnalyzes` runs it through the ordinary resolve.

What this does **not** fix is the neighbouring bug in todo.md's Modules section: a `pub` name
still claims the bare program-wide key, so `import std.maybe` still forbids the importer its
own `map`. The two do not compose — an overload set is confined to one module, and that
collision is cross-module by construction. What did change is the *motive*: splitting a module
to give two types the same method name is no longer a reason to, so that bug is now the only
one left.

### 08/03/26
**Shadowing a canonical type explains itself instead of reporting the answer as the
problem.** `?` on a user's own `data Maybe` said `` `?` operand must be a Result or Maybe,
got Maybe ``. The rule behind it is right — `std/prelude.lyra` marks its types
`@builtin(Maybe)`/`@builtin(Result)`, the marker confers the kind, and a same-named
*unmarked* declaration is therefore an ordinary type — so what changed is only the message.

It now distinguishes the two mistakes, because they want opposite fixes:

- **Same shape** (`None | Some(t)`) — the author re-declared the prelude's type, almost
  always without knowing it was already in scope: *"`?` works on the prelude's Maybe, and
  "Maybe" here is your own declaration at 1:6, not that one. Remove it to use the prelude's
  Maybe, or rename it if you meant a separate type."*
- **Different shape** (`Nothing | Just(t)`) — a genuinely different type wearing the name,
  which `?` was never going to accept; the shared name is what made that read as a
  contradiction: *"… a different type that happens to share the name. Rename it, or return
  the prelude's Maybe instead."*

An operand that is simply the wrong type keeps the original wording, which reads correctly
there ("got Foo").

**The advice the plan called for turned out to be wrong, and that is the useful part.** The
recorded suggestion was to say "mark it `@builtin(Maybe)` or rename it". Marking it is
`lyra-E017` — *duplicate `@builtin(Maybe)`* — because the prelude already claims the kind,
so that message would have walked the author straight into a second error. A program can
have exactly one canonical Maybe, and it is the prelude's. The shipped message never
mentions `@builtin`; both fixes it *does* offer are covered by tests that actually run them
(`TestCanonicalShadow_AdviceResolvesIt`), and a third pins why the tempting fix is refused
(`TestCanonicalShadow_MarkingTheShadowIsAnError`) so no future edit reintroduces it.

The other option on the table — letting a shadow *inherit* the kind it shadows — was
declined: `@builtin` exists to give the kind exactly one owner (claiming it twice is already
an error), and silently granting canonical identity to an unmarked same-named type
re-creates the ambiguity the marker was introduced to remove.

`ShadowedCanonical`/`ShapeMatchesCanonical` are stamped by `resolveCanonicalTypes` beside
`CanonicalKind`, not re-derived at the diagnostic site, for CLAUDE.md rule 4's reason — the
shape test has one home. One trap on the way: the stamp walks the statement list rather than
looking up `c.table.Types[kind]`, because a declaration shadowing a prelude name is
registered under a *qualified* key so the prelude keeps the bare one, and the lookup
therefore returns the prelude's declaration — precisely the one this is not about.

**`break` and `continue` no longer leak the pending temporaries of the statements they jump
out of.** `for { if ("a" ++ "b") == "ab" { break } }` leaked the concatenation — 18 bytes,
measured with LeakSanitizer on Linux both before and after. `lowerBreak`/`lowerContinue`
called `flushTemps()`, which releases only from `pendingBase` up, and the concat belongs to
the enclosing `if` statement's scope, whose flush the jump skips entirely.

**The jump cannot answer the question it needs to answer, which is what shapes the fix.**
It may only release a temporary that is live where it stands — an SSA value is defined in
one block, so it is available at the jump exactly when that block dominates the jump's
block. Release one that is not dominated and the taken path frees a value it never
produced: a double free, the failure direction that actually matters here (skipping one
merely leaks). And dominance is a property of the *whole* CFG, which does not exist yet
while the jump is being lowered — later blocks can still add edges, and an edge added later
can only *remove* dominators, so computing it early against a partial CFG could report a
dominance that is subsequently false. That is precisely the unsound direction.

So the jump records the obligation and `resolveExitReleases` settles it once the body is
complete, against a dominator tree that can no longer change (`dominators.go`, the standard
iterative Cooper/Harvey/Kennedy fixpoint — functions here have tens of blocks, so the simple
algorithm is the right trade against a Lengauer-Tarjan nobody would maintain). Deferring
works because llir keeps a block's `Insts` and its `Term` in separate fields and prints the
instructions first: appending to a sealed block lands *before* its jump. Every release
emitted is straight-line (a store and a call), so none needs a block of its own.

`loopCtx.tempBase` is the other half, and the one a too-eager fix gets wrong. Only
temporaries recorded at or above the loop's entry are the jump's to release; one below it
belongs to a statement enclosing the whole loop, whose flush still runs after the loop
exits, so releasing it at the jump as well would double-free. It is the temporary-side
mirror of `frameDepth`.

**What I could not do, stated plainly: I could not construct a program where the dominance
check actually rejects a temporary.** The obvious candidates do not reach it — a temporary
produced in a conditional sub-expression (an `&&`/`||` right operand) is released in its own
block by the existing statement flush, so it is no longer pending by the time a `break` in a
sibling branch is lowered, and a temporary belonging to an enclosing statement is produced
on a path the jump is nested inside, which dominates it. The check is therefore defensive
rather than demonstrated: it is cheap, it is computed once per function that jumps, and the
alternative to having it is relying on that structural argument holding for every lowering
shape added later, in the region this project already treats as carrying real double-free
risk. It stays, and this paragraph is here so the next person knows it has never been seen
to fire.

Coverage: `TestEmit_BreakReleasesPendingTemp` pins two release sites in the loop (one per
way out; one is the leak, and it fails with that message without the fix), five
`TestExec_ExitReleases` cases over break/continue/labeled-break plus the two negative
shapes — a temporary enclosing the loop and one after it, which a too-eager fix would
double-free — each under ASan, and `TestDomTree` over a hand-built diamond CFG, including
that an unknown block answers false (the direction that leaks rather than double-frees).

**A `?` no longer leaks a temporary produced by a sub-expression of its operand.**
`f(g())?`, where `g`'s owned result is consumed by a borrowing parameter: the temporary was
released on the success path and not on the propagating one. Measured both ways with
LeakSanitizer on Linux (macOS has no LSan) — **19 bytes in 1 allocation before, none
after**.

`lowerTryPropagate` held the *whole* pending list back from its return's flush. The reason
was sound: that flush releases each temporary in the block that produced it, and the
operand's block is the one before the branch, so a release emitted there would sit ahead of
the tag test and free the value on the **success** path too. Holding everything back avoided
that, at the cost of also suppressing temporaries that genuinely die on the propagating path.

The fix is that the propagating path releases them itself, into its own block —
`releaseTempsOnExit`, which differs from `flushStmtTemps` in the one way that matters: **it
does not truncate the pending list.** An early exit is one path out of a statement that still
has another, and that other path must still reach the statement's own flush. Truncating would
move the release rather than add one, leaking on every non-exiting path instead. The operand's
own temporary stays excluded, since its reference transfers into the rebuilt error.

**The residue is bounded by dominance, and that is what makes it stop here.** Only
temporaries produced in the exiting branch's *predecessor* are released — that block is known
to dominate the exit, so the value is live. One produced inside a conditional sub-expression
(an `&&` right operand, a match arm) is not dominated, and releasing it would touch a value
undefined on the path that branch did not take. Those still leak, which is the safe direction.

**A prediction this disproved, worth recording.** The old note said the general fix was to
give `pendingTemp` a *release* block rather than a production block, and that one change
would stop `?`, `break` and `continue` leaking alike. Checking `break` with LSan showed it
leaks too (18 bytes, confirmed) — but not in a way that idea reaches. A `?` propagates from
a block whose predecessor produced the temporaries, so block equality is enough. A `break`
sits at the end of a *branch*: the producing block dominates it without being its
predecessor, and widening the test to "release everything pending" is unsound, because a
temp produced in a sibling branch is undefined on the path the `break` takes — a double free
rather than a leak. Closing that one needs real dominance information, which the backend does
not compute today; it is now an open item saying so, instead of an estimate that was too
cheap.

**An array *element* may carry an allocation or `weak` modifier** — `[]shared Node`,
`[3]weak Observer`, `[16]stack Vec3`. Measured while closing the `Maybe<weak T>` item below,
which is where the gap surfaced: the two look like one problem and are not.

**A grammar change only; this repo needed no code at all.** `array_type`'s `element_type` was
`_non_allocated_type`, so a modifier had no way in — but every pass downstream was already
built for one. `firstAllocationMismatch` (`assignable.go`) recurses into array elements and
its comment names the case it exists for: "a `stack` element assigned into a `[N]shared`
slot". That rule shipped, and the syntax needed to reach it did not, so it sat unreachable
and untested until now. The backend was the same story — an element flavor flows through the
existing layout and ownership paths, and a `shared` element is pointer-sized — so `lyra-E018`
fires on `[2]stack Node` → `[2]shared Node`, and `[]shared Node`, `[N]weak T`, `for-in` over
managed elements, and the whole tree shape all worked on the first build.

*Why it mattered:* `kids: []shared Node` is the obvious spelling for a tree's children, so
the natural shape for the very object graph `weak` exists to support was unwritable. The
cycle work below had to use `kid: Maybe<shared Node>` for exactly this reason.

*The rule that shaped it:* exactly **one** modifier deep. `_element_type` is a `choice` of
`_non_allocated_type | allocated_type | weak_type` whose operand stays `_non_allocated_type`,
so `[]shared shared Node` is still an error, and the *other two* users of that rule
(`weak_type`'s inner, `allocated_type`'s type) are deliberately untouched — widening them
would make `shared weak T` and `weak shared T` writable, and `weak T` already means
"non-owning reference to a `shared T`". Two `:error` corpus tests pin that.

Cost: 8,237 → 8,240 states (+3, +0.04%), `parser.c` +5 KB — cheap because the element sits
after a closing `]`, leaving no prefix for the automaton to track. Coverage: six corpus tests
in `tree-sitter-lyra`, `TestAlloc_ArrayElement*` / `TestAlloc_DynamicArrayElement*` /
`TestAlloc_ArrayElementModifier_Ok` here, and five `TestExec_ArrayOfManagedElements` cases
ending in the full shape — `shared` children, a `weak` parent edge — run under ASan and on
Linux.

**A `weak` field is constructible — `Maybe<weak T>` — and the two things blocking it had
nothing to do with `weak`.** A cycle back-edge is the reason `weak` exists (refcounting
leaks cycles and there is no collector, ALLOCATION.md), and it could not be written: a
field must be initialized, there is no empty weak, so the edge has to be *optional*.

**Both premises in the todo item were wrong, which is the useful part.** It said the fix
needed a grammar change because `Maybe<weak Node>` "does not parse". It parses, and always
did — `parameterized_type`'s arguments are `$.type`, and `$.type` includes `weak_type`. The
claim was never tested, only reasoned from the neighbouring gap that *is* real (`[N]shared T`
mis-parses, because `array_type`'s element is `_non_allocated_type`). Checking cost one
`tree-sitter parse` and would have redirected the work at the start; the whole item was
filed against the wrong repo.

What actually blocked it was **two hazard-8 misses**, and neither is weak-specific — a
`shared` struct holding a plain `Maybe<i64>` failed identically, which is the tell that the
feature was never the variable:

- **`resolveForLayout` had no `ParameterizedType` case.** A generic instantiation used by
  value inside another type reached `SizeAndAlign` as a shape none of its cases match, so
  boxing the enclosing value failed with "cannot size a `shared Node` payload yet". The fix
  is to normalize through `resolveInstantiation` — the choke point `recordedType` and
  `resolveDataType` already use, and whose own comment predicted this: "adding a case to
  each would be a dozen places to keep in agreement". A `shared` instantiation
  short-circuits before the recursion, exactly as the `UnresolvedType` arm does, which is
  what keeps resolution finite on a recursive generic.
- **`resolveTypeIfKnown` had drifted from `resolveType` by exactly two composites** —
  `ParameterizedType` and `*LambdaType`, the argument-list pair every such switch forgets.
  It resolves the *return* annotation (`checkLambdaBody` uses it so an unknown name is not
  reported twice), so the symptom appeared only in return position: `-> Maybe<weak Node>`
  kept `Node` unresolved while the body's value resolved it, and the two spellings compared
  unequal — "return type mismatch: expected `Maybe<weak Node>`, got `Maybe<weak Node>`".
  `resolveType` had been fixed for this same pair earlier; its twin was not, and the file
  documenting that fix (`named_type_in_composite_test.go`) is now where both live.

That is the fifth and sixth instance of hazard 8, and the first where the drifted switch was
a *twin in the same file* rather than a copy in another package — so the rule's "check the
others in the same file" now has a companion: check the function it was copied from.
`resolveType`/`resolveTypeIfKnown` differ only in what they do at an unknown leaf and
duplicate the whole recursion for it; folding them into one walk is the durable fix and is
now an open item.

Coverage: five `TestExec_WeakOptionalField` cases — the two no-`weak` regressions (a generic
field, and a nested `Maybe<Maybe<i64>>` so resolution has to recurse rather than stop at the
first normalization), a back-edge read through the field, a real parent/child cycle with the
`shared` edge one way and the `weak` edge back, and the dead-referent path where the field
is `Some` but the upgrade fails — that last one being why the field is `Maybe<weak T>` and
not a nullable weak: "no back-edge" and "the referent is gone" stay distinct branches. All
five run under ASan and on Linux (`./asan.sh`), where the older clang's typed pointers would
catch a payload built at the wrong width.

**Diagnostics render literals as source, not as Go structs.** A real message read
`expected array pattern, got IntegerLiteralExpr(0, Base: 10)..= IntegerLiteralExpr(10,
Base: 10)`; it now reads `expected array pattern, got 0..=10`.

`GetName` on an expression is a **source rendering** — parents compose them, so a match arm
builds `match <pattern> { <body> }` out of its children's — and the literals were the family
that returned their Go type and fields instead. Their parents dutifully composed *that*,
which is how a `RangePattern` came to hand someone the compiler's internals about their own
program. The fix is small; the interesting part is why it lasted.

**It reaches no golden file.** Every other rendering in the compiler is pinned by the
collector's golden tests, so drift is caught the next time anyone regenerates. `GetName`
is interpolated into diagnostics and nowhere else, which means the only reader who ever
sees it is a user, and the only reviewer is whoever happens to read a failing message
closely. `RangePattern` printing `0..9=` for `0..=9` survived the same way (fixed 08/01).
There is now a test that no expression rendering contains `Expr` or `Pattern` — neither
substring can occur in the Lyra source these are meant to produce, so a node added later
fails there rather than in someone's terminal.

Fixed across the family rather than at the reported site, per the note the bug carried:
the literals, the postfix forms (`xs[0]`, `xs.len`, `xs.1`, `xs?`, `Show::show`), array
repeat (`[0; 3]`), and the pattern lists — a tuple or array pattern was formatting its
element slice with `%v`, printing Go's list of pointers.

Two things corrected in passing, both stale rather than wrong-by-design: a regex rendered
as `r/…/`, the spelling that stopped parsing on 07/29, and it rendered through `%q`, which
doubles every backslash — `r"\\d+"` for what the author wrote as `r"\d+"`. A regex is
mostly backslashes, so that one is worth the verbatim form even though `%q` is right for
strings.


### 08/03/26
**Trait and impl methods may be separated by newlines.** Statements gained a terminator on
07/31 and member lists did not, so `trait Ops { a: … ⏎ b: … }` failed — and failed in the
expensive way: "missing }" pointed at the end of the **first** signature, several lines above
anything a reader would suspect, then cascaded through the file. A misdirecting parse error
costs minutes rather than seconds, which is the whole reason this was worth fixing rather
than documenting.

`memberList` (`include/helpers.js`) is the shared shape, and two of its details are
decisions rather than mechanics. Its separator is `_statement_separator`, not a bare
`_newline`, so `;` works here too and the language keeps one answer to "what ends a thing on
its own line". And it keeps `commaSep1`'s structure rather than `statementList`'s, because
that is what makes the list non-empty — `trait C {}` stays a syntax error, preserved on
purpose rather than by accident.

A signature wrapped across lines is unaffected, and that is the property the design rests
on: the scanner only runs where tree-sitter marks the terminator valid, so a newline inside
an unfinished parameter list never reaches it. Verified rather than assumed.

Cost: 8,208 → 8,224 states (+0.2%), `parser.c` +21 KB.

**Struct declarations followed** (`struct_type_body`, `anonymous_struct_type`), which were
the last users of the comma-only shape. Held back from the first commit on purpose — they
were not what it set out to fix — and done as its own change once that one was in.

**A struct literal's fields still require commas, deliberately.** That list sits inside the
literal-vs-block ambiguity — `Point { … }` is contested between a struct literal and a name
followed by a block, which the postfix-head change touched earlier the same day — so a
newline separator there is a question about *that* conflict rather than the same one-word
change. There is a test pinning the current behaviour, so if it ever changes it changes on
purpose.

Total cost of both: 8,208 → 8,237 states (+0.35%), `parser.c` +32 KB.


### 08/03/26
**A struct literal is a postfix head.** `Node { n: 7 }.n`, `Node { n: 7 }.a()` and
`Grid { cells: […] }.cells[0]` parse; before this *no* postfix attached to a struct literal
— not a method call, not plain field access, not an index — while every other
value-producing expression already worked as one (`mk().a()`, `(Node { n: 7 }).a()`, a
literal in argument position). The grammar change is `named_struct_literal` joining
`_primary_expr`; the reasoning is in `tree-sitter-lyra`'s CLAUDE.md.

Two things worth keeping from doing it as a measured prototype rather than a guess.

**The cost was 26 states.** 8,182 → 8,208 (+0.3%), and +69 KB of `parser.c`. The estimate
going in was "unknown, possibly large" — juxtaposition had cost +19% states for less, and
`lambda_expr` once owned 91% of a 62,663-state parser, so the honest answer before measuring
was that it might not be affordable. It was, by two orders of magnitude, and the measurement
took one `generate`.

**Lyra needs no "no struct literal in an `if` header" rule.** Rust and Go both have one,
because the `{` of `if Node { n: 7 }.n > 0 {` cannot be told from the body's opening brace
with bounded lookahead. The plan here assumed Lyra would need the same restriction and said
so; GLR keeps both readings alive until a token decides, so the form simply works and is now
in the corpus. That is a real advantage of this grammar's architecture over theirs, and it
was found by trying rather than by reasoning — the prediction was wrong in the direction of
caution.

Found while writing `own`-receiver tests, where it read as a puzzling test failure.

### 08/03/26
**`own` on a trait method's parameter — and on its receiver — is supported; lyra-E030 is
retired.** The restriction existed because the ownership pass analyzed no trait-method body,
so `take: (Self, own string) -> string` compiled to a heap-use-after-free (measured, 07/31).
Method bodies are analyzed per specialization as of earlier today, which is the condition
the restriction itself named for lifting.

**Deleting the check was the first ten minutes.** Two other passes could not resolve a
*method* callee, and both mattered:

- **Use-after-move went unchecked through a method call.** `resolveCallee` handled an
  identifier callee only, so a method call recorded no moves whatever its signature said —
  `h.take(msg)` followed by `println(msg)` drew nothing, while the free-function spelling of
  the same mistake was a clean lyra-E019. That is the diagnostic that makes `own` safe for
  the *caller*, so lifting E030 without it would have reintroduced the same use-after-free
  one layer up. It now resolves through the MethodTable, with the receiver as parameter 0.
- **The ownership pass did not transfer the receiver.** Its argument loop had carried the
  +1 offset from the start, but the receiver is not in `e.Arguments`, so an `own Self` method
  left the caller lending a value the callee had adopted. Both released it: an ASan
  heap-use-after-free inside `lyra_rc_release`, which is how it announced itself — the plain
  run of the same program exits 0 and prints the right answer.

That is the receiver-offset hazard for the **third and fourth time in one day** (purity's
`methodArgumentAt`, the UFCS desugar, then these two). The pattern is worth naming: every
pass that reads a callee's parameter modes has to know that a `.`-call's receiver is
parameter 0, and each has discovered it separately, late, and with a silent failure mode.
UFCS avoids it by rewriting the receiver into the argument list before any later pass runs,
which is the shape the others would benefit from.

Two limitations found while testing and left alone, both unrelated to `own`: a trait or impl
with several methods needs **commas** between them (a newline does not separate them), and a
struct literal cannot be a method receiver directly (`Node { n: 0 }.a()` does not parse).

### 08/03/26
**Generic trait-impl methods are monomorphized.** `impl Unwrap<t> for Maybe<t>` type-checked
and then died in the backend — `match on Maybe<t> not implemented yet` — because the body
was emitted once with `t` still abstract. This is the gap that decided UFCS's sequencing
earlier the same day: method ergonomics through traits needed this built, while UFCS rode
the free-function path that already monomorphized.

**It was worse than an unimplemented error, which is what the investigation turned up.**
Where a generic impl's body *did* lower — one that never touches the type variable — the
emitted symbol was keyed by `impl.Type` (`Box<t>`), so every instantiation shared one
function:

```llvm
define i64 @Box$Sized$size(%Box$i64 %self)
  call i64 @Box$Sized$size(%Box$i64 %4)
  call i64 @Box$Sized$size(%Box$boolean %6)   ; ← the same function
```

Apple clang accepts that and runs it: opaque pointers make the two function types
indistinguishable, which is precisely the class of invalid IR `asan.sh` and its
typed-pointer clang exist to catch. A silent miscompile, not a missing feature.

**The bindings existed the whole time.** `resolveTraitMethod` unifies the impl's target
against the receiver and keeps the result to check the impl's `where` bounds; it simply
never left the typechecker. They now travel on `typetable.Resolution`, and
`Resolution.SpecKey()` names the specialization for the three consumers that have to agree —
the emitted symbol, the method cache, and the ownership table. Those three disagreeing *was*
the bug, so they get one string.

Lowering is then the existing monomorphizer, not a second one: `pushTypeSubst` with the
bindings, and the body's types come out concrete through the two accessors every type
already funnels through. One wrinkle worth recording: method bodies are **deferred to a
queue** (a method calling another would otherwise be lowered re-entrantly, corrupting the
lowerer's per-function state), so a substitution pushed while declaring is long gone by the
time the body is lowered. The queue entry carries the bindings.

**Ownership was the larger half.** `pkg/analyzer/ownership` walks top-level declarations,
which impl methods are not — they hang off a `TraitImplStmt` — so **no method body had ever
been analyzed**, generic or not: a method holding a `string` emitted neither retains nor
releases. `driver.OwnershipByMethod` now holds one table per specialization, built by the
same `ownership.AnalyzeLambda` the generic-function path uses. Whether a value is
reference-counted is a property of the type *argument*, so `t = string` retains its returned
payload and `t = i64` does nothing, from the same source line and the same AST nodes — which
is why the tables cannot be merged. Verified under `LEAKS=1 ./asan.sh`, which is where a
wrong answer here shows up as a fault rather than as a number.

Two things deliberately not changed. A generic body calling a generic impl method
(`getOr<t>` calling `o.unwrap(d)`) is still refused with "type variable t has no concrete
type here" — substitutions are not composed, and the free-function analogue was already
refused identically; it is now in todo.md as one limitation rather than two. And
`traitMethodLambda` moved out of the backend onto `Resolution.Lambda`, because the ownership
pass needs the same synthesized function and two constructions of it would be two answers to
"what are this method's parameters".

### 08/03/26
**A negated literal and a plain one now have a common type.** `if c { -1 } else { 2 }` did
not compile. Neither did `match n { 0 => -1, _ => 2 }` or `[-1, 2]` — ordinary code, in
three constructs, rejected with a message comparing a type to itself: *then is integer
literal, else is integer literal*. The match form managed *i64 vs i64*.

The cause is that a negated literal is `untyped_signed_int` while a plain one is
`untyped_int`, and `branchCommonType` only knew two moves — equality, and assignability in
either direction. Neither untyped kind is assignable to the other, so it gave up, and both
print as "integer literal", which is why the diagnostic read as nonsense. The join is the
signed kind: a set containing a negative value cannot settle to an unsigned one. The result
stays *untyped*, so an annotation narrows it exactly as it would either operand, and the
range check still applies to whatever it narrows to.

Deliberately narrow: it joins two untyped integers and says nothing about concrete widths,
which remain a real disagreement worth reporting. Found by a boundary case in the tests for
the range-check fix below (`[-128, 127]`), which is a reminder that a test written for one
rule is a decent probe for its neighbours.

### 08/03/26
**Composite narrowing range-checks its literals.** `let t: (u8, u8) = (300, 1)` and
`let a: [2]u8 = [300, 1]` were both accepted, silently, putting 300 into a u8 slot — while
the scalar `let n: u8 = 300` was rejected. `checkIntegerLiteralRange` reads the *declared*
type, and for those the declared type is a tuple or an array rather than an integer, so it
returned immediately and nothing else looked.

The check now sits where the narrowing happens, in `propagateLiteralType`'s tuple and array
branches, which means it covers every context that narrows rather than only an annotated
`let`: a return body (`() -> (u8, u8) => (300, 1)`) and an argument position are now caught
too. It carries a guard keyed by literal node, because a leaf can be narrowed by more than
one context on the way down — a struct field holding a tuple is narrowed by the field's
declared type and again by the enclosing annotation — and one too-large literal is one
mistake however many times it is checked.

### 08/03/26
**An annotation now narrows a data constructor's untyped payload.** `let m: Maybe<u8> =
Some 7` was rejected — *cannot assign Maybe<i64> to Maybe<u8>*, against an annotation
sitting right there. Solving binds `t` from the payload, and to unify it promotes the
untyped 7 to its i64 default; the result was recorded as a settled `Maybe<i64>`, and the
annotation never got a say. The scalar, tuple and array spellings of the same narrowing all
worked, which is what made it read as a quirk of data types rather than as the general rule
failing in one place.

**The missing distinction is between a width the program determined and one the expression
guessed.** `Some(x)` where `x: i64` is a `Maybe<i64>` because the program says so;
`Some 7` is one only by defaulting. Both were recorded identically, so the second could not
be told from the first after the fact. Such nodes are now marked
(`markDefaultedConstruction`), their payload leaf is left untyped for a context to narrow,
and `stampDataConstruction` accepts them alongside the bare declarations it already
accepted. Everything else stays closed to the context, which is the half that keeps a real
mismatch from being overwritten.

**The line runs through the declared field, not the payload.** A field that is a type
variable takes its width from the substitution and may be deferred; a concrete one
(`Wrapped(u8)`) takes it from the declaration and must be narrowed immediately. The first
version of the fix deferred both — it type-checked cleanly, and announced itself in the
backend as `aggregate element type mismatch: cannot store i64 into double`. A type rule
whose failure surfaces two layers away is the argument for `fieldTakesWidthFromSolve`
existing as a named predicate rather than as an inline condition.

Two consequences worth noting. A narrowed literal is now **range-checked against what it
was narrowed to** — `Maybe<u8> = Some 300` is an error, where previously the question could
not arise because the annotation was rejected wholesale first, and where assignability
alone cannot help (after narrowing, a u8 payload holding 300 is assignable to u8 and
wrong). And a payload type mismatch now reports at the payload — "Some: cannot assign
integer literal to string" rather than "cannot assign Maybe<i64> to Maybe<string>" — which
is the same preference the surrounding code already had for reporting the precise site.

Found while writing UFCS tests, where it first read as a UFCS failure. The tuple and array
narrowing paths still skip the range check; that is now in todo.md's Known bugs with the
two cases that demonstrate it.

### 08/03/26
**UFCS: `m.unwrap_or(0)` is `unwrap_or(m, 0)`.** A free function opts in by naming its first
parameter `self`; everything else stays call-only, so adding a helper to a module cannot
change what `x.f()` means elsewhere in it. The rung sits in `inferMemberCall`, making the
ladder field → trait method → UFCS → builtin.

**It went before the trait work, and the reason is a measurement.** The 07/31 design
sequenced this after the open trait gaps. But a generic *free function* over `Maybe<t>`
monomorphizes and runs today, while a generic *trait impl's* method type-checks and then
dies in the backend — `match on Maybe<t> not implemented yet`, because an impl method's body
is not specialized per instantiation. So the trait route to `m.map(f)` needs monomorphization
built first, and the UFCS route needed nothing: it desugars to the call that already works.

**The whole feature is one rewrite.** A matching call becomes `f(receiver, args…)` in place —
receiver into `Arguments[0]`, callee into an ordinary identifier — so generic solving,
purity, ownership, captures and the backend all see a direct call and none of them learned
anything. The two spellings emit byte-identical IR, which is the test that says so.

That is not a shortcut, it is the defence against a silent bug. Purity indexes arguments
*positionally* against the declaration's parameters, so a receiver left outside `Arguments`
shifts every index by one and each callback is checked against the bound of the parameter to
its right — and a function-typed argument satisfies the wrong function-typed parameter
without complaint, so a declared `pure` bound would simply stop being enforced, reporting
nothing. Trait methods pay exactly that tax through `methodArgumentAt`. `applyDefaultArguments`
was already rewriting calls for the same reason, which is what made this feel like the
grain of the code rather than against it.

**What the build turned up that the plan had not:**

- **The unused-import warning became wrong.** A UFCS call never writes the module's name, so
  the syntactic check called the import that permitted it unused — advice that breaks the
  program. `TypeChecker.UFCSModules()` now carries the resolutions to it. The lesson is
  general: a syntactic "is this used" test is only as good as the ways a thing can be used,
  and adding a resolution path silently invalidates it.
- **The obvious multi-clause exclusion was order-dependent.** Refusing candidates with
  `len(LambdaClauses) > 0` looks safe and is not: `desugarClauses` *consumes* the clauses, so
  the same function would qualify after its declaration had been checked and not before. The
  test is on the declared parameter instead, and multi-clause functions are candidates like
  any other (their heads must bind plain names, so one can be `self`).
- **`structFieldsOf` in the LSP was passing a zero Location**, correct only while the server
  analysed one unnamed file at a time — which stopped being true when it started resolving
  the import graph the day before. It now passes the document's own path, which is what
  decides whose declaration a name means.

Two decisions the design had left open, both taken toward the conservative reading: an
**import is required** to reach another module method-style (so what a file may call depends
on its own import list, not on what an unrelated file imported), and an **`own` receiver is
refused** with an error naming the call form (so a move always looks like a call).

The standard library's `Maybe`/`Result` combinators now name their receiver `self`, so they
read both ways. Also fixed a stale note in `std/prelude.lyra` claiming no method lowers —
that stopped being true on 07/30; the real limit is *generic* impls.

### 08/02/26
**The language server analyzes a program, not a buffer.** `cmd/lyra-lsp` called
`driver.Analyze` on the single open document, and a single unit is not a smaller version of
the real thing — it is a *different program*. It has no prelude in it, so `Maybe`, `Some`,
`Ok` and every standard-library name were undefined **in the editor only**: the reported
symptom was `undefined tuple type "Some"` on `std/maybe.lyra`, a file `lyrac check` compiled
cleanly. A program's own modules were unresolved the same way.

The cause was a fork nobody had noticed: `modules.Resolve`, the search roots and `stdRoot`
lived in `cmd/lyrac`, and `stdRoot` had no other caller. Those moved to
`pkg/modules/roots.go` as `DefaultRoots`/`DefaultOptions`/`StdRoot`, so the two tools cannot
disagree about where the standard library is — the same "one definition, so two callers
cannot drift" rule the type-variable walk was consolidated under.

**The buffer is not the file**, which is what an editor adds to the compiler's problem.
`Options.Overlay` maps a path to in-memory source that wins over the disk, and — the half
that is easy to miss — makes an overlaid path count as *existing*, so an import of a file
that has never been saved resolves instead of being reported missing. Every open document
goes in, not just the one being analyzed.

**What resolving a program forced back out again.** With several files in one AST, a line
and column no longer identify a position: the prelude's line 40 would answer a request about
the user's line 40. So `diagnosticsFor` filters diagnostics by file before publishing (one
naming no file is kept — it is program-level and has nowhere else to go), and `docProgram`
narrows the stored AST to this document's own top-level statements, which is what every
position-based handler walks. Two handlers needed more than filtering, both because they
return locations rather than consume them: a definition resolving into another file is now
returned against *that* file's URI, and a rename whose declaration lives in another file is
declined outright rather than applied at those coordinates in this buffer — that one would
have been a silent corruption. The remaining single-file edges are in todo.md.

`docAnalysis` also gained the module scope of the file it holds. A file declaring
`module std.maybe` puts its declarations there, not in the unnamed entry scope every handler
was asking for, so a top-level position in such a file resolved against none of its own names.

### 08/02/26
**Shift-check elision, and `x <<= n` typed like `x = x << n`.** The two follow-ups the
bitwise work left open.

**`NoShiftOverflow`** joins `NoDivZero`/`NoOverflow` in the safety table. A constant count
was already folded away at lowering, so this is the variable case — a count refined by
`if n < 8`, or a bounded loop counter. The proof obligation is written to mirror the
emitted check rather than to approximate it: that check compares the count **unsigned**
against the width, so a negative count reads as an enormous one, which is why marking
requires *both* a lower bound of 0 and a finite upper below the width. Getting that half
wrong would elide a check a negative count needs.

**The compound-shift asymmetry came from one function doing two jobs.**
`checkAssignToBinding` resolved the assignment *target* — does the name exist, may it be
written, what type is it — and checked the *value* against that type, in one pass. For
every other operator that is right. For a shift it is not: the right operand is a count,
not a value in the target's domain, so `x <<= count32` on a `u8` demanded a conversion
that `x = x << count32` never asked for. Splitting out `resolveAssignTarget` lets the
shift path take the target rules and supply its own value rule.

One detail preserved through the split, because it is load-bearing and easy to lose: a
*rejected* target still hands back its type. Every caller checks the value regardless, so
that a refused assignment reports its own diagnostic without swallowing the errors inside
its right-hand side.

`isIntegerOperand` now strips a constrained newtype first — `newtype Mask = u8` is a u8
wearing a name, and masking one is exactly what such a type is for.

### 08/02/26
**The value-range pass tracks bitwise results.** `andI`/`orI`/`xorI`/`shlI`/`shrI` join
`addI`/`subI`/`mulI`, so a masked value carries its bound into the arithmetic that
follows: `(x & 0x0F) + 1` is now proved in range and drops its overflow trap, where
before the mask widened to ⊤ and the addition kept a check it could never need.

**The rule that does the work is the masking one, and it is stronger than the obvious
version.** For a mask `m >= 0`, `x & m` lies in `[0, m]` **whatever the sign of x** — the
result can only have bits that m has, so it is non-negative and no larger. Stating it as
"both operands non-negative" would have missed exactly the case worth having, a signed
value masked down to a small range. `|` and `~` do need both operands non-negative (their
bound is the all-ones ceiling over the wider one), and `&` with both sides possibly
negative has no useful bound at all (`-1 & -1 == -1`), so it widens.

**None of this goes through `checkArith`, and that is the substantive decision.** Those
operators do not trap on overflow, so there is no E020 to report and no `noOverflow` to
record — and `<<` **wraps**. Routing a too-wide shift through the arithmetic path would
have reported "this operation always overflows", which is simply false: the value is
defined, it just lost bits. For the same reason `shlI` *refuses* rather than clamps when
the mathematical product could leave the type — clamping would assert a range the wrapped
value need not be in, which is the unsound direction.

**Soundness is checked by exhaustive brute force, not by argument.** These intervals feed
trap elision, so an interval that is too narrow is a miscompile — the backend drops a
check the program needed — while one that is too wide only costs precision. That asymmetry
does not tolerate hand-picked examples, so `bitwise_interval_test.go` enumerates *every*
interval over a 4-bit type, both signednesses, every value in each, and every non-trapping
shift count, comparing the abstract answer against what the machine would really produce
(including the truncation `<<` performs). 18,496 interval pairs are proved for `u4 &`
alone. The first attempt tried this at i16 and did not finish — 65,536 values per interval
— which is why the exhaustive layer is small-width and the real widths are covered by
targeted boundary tests instead. The per-rule counter is an anti-vacuity guard: a rule
that always gave up would otherwise pass having checked nothing.

The ±∞ sentinels needed care throughout. A `u64` upper bound is `+∞` because its true
maximum does not fit an int64, so any rule that *computes* with a bound (the all-ones
ceiling, a shift count) refuses an infinite one rather than treating `MaxInt64` as a real
value — while `&`, which only needs to know a bound is non-negative, still profits from it.

### 08/02/26
**Bitwise and shift operators.** `& | ~ << >>`, prefix `~` for complement, and the five
compound assignments. A systems language aimed at games had no way to touch a bit, which
made every mask, flag set and packed field unwritable; the trait `binary_operator` list had
reserved `<< >> & | ^` for overloads since before any of them existed in expression
position.

**Xor is `~`, not `^`, and that is forced rather than stylistic.** `^` is already prefix
raw-pointer syntax (`^T`) and postfix deref (`ptr^`), so a binary `^` is ambiguous with a
deref in operand position — `ptr^ ^ mask` has no good reading. `~` was completely free, and
already reserved in the trait `prefix_operator` list. Odin, which this language borrows from
elsewhere (`%%`, the `rune` naming), spells xor `~` for its own reasons and gets the same
result. Complement is the same token in prefix position, exactly as `-` is both subtraction
and negation.

**Precedence is deliberately not C's**, and the tests check the grouping by what the program
*computes*, since a parse-tree assertion would not catch a lowering that ignored it. Bitwise
binds tighter than comparison, so `flags & MASK == 0` means `(flags & MASK) == 0` — in C it
means `flags & (MASK == 0)`, which is why C codebases parenthesise every masked comparison.
It binds looser than arithmetic (Python/Ruby, not Go, which ties `|`/`^` to `+`), and shifts
sit above addition as in Go. `&` > `~` > `|` matches everyone except Go.

**An out-of-range shift amount traps.** `shl`/`lshr`/`ashr` are undefined behavior when the
amount reaches the operand's width, so this is the same call div-by-zero already makes: the
alternative is whatever the target's shift hardware does (x86 masks, ARM saturates), which
is exactly the divergence the fixed-width primitives exist to rule out. The comparison is
*unsigned*, which catches a negative count in the same instruction — as a two's-complement
pattern, -1 is enormous — and it runs on the count **before** it is coerced to the shifted
width, because coercing first could truncate an out-of-range count into range and hide the
thing being checked. A compile-time constant already in range emits no check at all, which
covers `x << 3`.

**A shift's count is typed independently of the value being shifted.** It is a *distance*,
not a value in the left operand's domain, so `u8 << i64` is well-typed and the backend
narrows the count. Unifying them the way `+` does would demand a conversion that buys
nothing — the count is not stored anywhere, and the trap is what makes it safe. The compound
form `x <<= n` is stricter, because it routes through `checkAssignToBinding`; that asymmetry
is recorded in todo.md rather than papered over.

**Three `|` collisions, resolved two different ways.** `|` was already the struct-update
separator (`P { base | f: v }`) and the array-comprehension delimiter. The struct-update and
generator races are shift/reduce and take `conflicts:` entries. The comprehension needed
something else: `[ x in R | A | B ]` fits two *complete* parses — guard `A` with result `B`,
or no guard and the single result `A | B` — which is an ambiguity between finished trees,
the one thing `prec.dynamic` resolves and `conflicts:` does not. Getting it wrong was not a
parse error: every guarded comprehension silently became an unguarded one whose result was a
bitwise-or, and only the pre-existing corpus test caught it.

**Cost, measured before accepting it** (the grammar's CLAUDE.md says to run
`--report-states-for-rule -` before adding anything here): 6,606 → 8,182 states, `parser.c`
12.0 → 15.3 MB. The alternative of collapsing the three bitwise bands into Go's two saves
only **424 states (5%)**, so the distinct bands are nearly free and buy the conventional
`&` > `~` > `|` ordering — the bulk of the growth is having the operators at all. `>>` does
not break nested generics (`Maybe<Result<i64, string>>`): tree-sitter's lexer only considers
tokens valid in the current parse state, verified before and after.

The compound-assignment → binary-operator mapping moved onto `ast.MathAssignOp.BinaryOp`,
replacing a table in the backend. Adding an operator used to mean editing two lists nothing
checked against each other, and the typechecker needs the same mapping to carry the binary
operand rules onto a compound assignment.

**The value-range pass is untouched and that is the sound outcome, not an oversight.** Its
operator switch leaves an unrecognised operator's `ok` false, which falls back to the type's
full interval — so bitwise results are ⊤ rather than mistracked, and no trap elision can go
wrong on them. Tracking them properly (a mask is *the* idiom for producing a known-small
value) is in todo.md.

### 08/01/26
**One `..` notation, three sites.** The range notation appears in an expression (`0..<n`),
a match pattern (`0..=9`) and a `newtype` constraint (`range(0..=100)`). They were three
independent grammar rules that had drifted on four axes — whether the end operator was
required, whether either bound could be omitted, what an operand may be — plus a fifth:
the same two characters `<`/`=` were `range_end_operator` in two rules and
`less_than_comparator`/`equal_to_comparator` under a `comparator` field in the third.
`rangeBounds` in `tree-sitter-lyra/include/helpers.js` is now the one shape.

**Two of those axes are real and stayed parameters; the rest were drift.** The *operand*
must differ — a pattern needs a compile-time literal (exhaustiveness and the jump-ladder
lowering depend on it), a constraint a constant expression, an expression arbitrary runtime
values; unifying them would either let a match arm hold a call or break `for i in 0..<n`.
*Open-endedness* must differ too: `range(0..)` and the pattern `10..` are useful, while an
open-ended expression range needs the lazy iterator the language does not have. What is now
uniform: one node kind for the operator, and — where bounds may be omitted — at least one
required, so `range(..)` and a bare `..` pattern are unspellable.

**The defect worth the whole change was a silent default.** `range_pattern` made the end
operator optional, and every reader of `RangePattern.EndOperator` tests `== "<"`, so an
omitted one fell through to *inclusive*: `0..9` meant `0..=9`. Not cosmetic — that extra
value is exactly the boundary the exhaustiveness checker and the emitted comparison would
disagree on. It is now `lyra-E032` at all three sites through one collector check, and the
suggestion is spliced from the source so it is right for every form the notation takes,
including open-start (`..9`) and stepped (`0..10:2`).

*Where each rule is enforced, and the line between them.* The operator is optional in the
grammar everywhere and required by the collector everywhere; a bare `..` is refused by the
grammar. **Enforce in the collector when the construct has a plausible intended meaning
that must be disambiguated** — `0..9` is what a Rust or Python programmer writes *meaning*
something, and a message naming both fixes beats a syntax error pointing at whichever token
failed to shift (the `lyra-E029` trade) — **and in the grammar when it has no meaning at
all.** Only one existing line in the whole tree used the ambiguous form, in a test whose
subject was rune-scrutinee rejection.

**Patterns gained open bounds, and exhaustiveness got better for it.** `..<0`, `0..=9`,
`10..` with no wildcard now compiles: `armIntInterval` reads an absent bound as the
scrutinee type's own limit, which is what writing it means. The backend *omits* the
comparison for an open side rather than emitting one against the type's limit — on an
unsigned scrutinee `x >= 0` is not merely redundant but the always-true compare the range
analysis reports as `lyra-W011` elsewhere. Ten exec cases cover it, weighted toward the arm
*not* taken and the boundary values, because dropping the wrong side of a two-sided test
still passes for one input.

**The two step spellings now answer to one rule.** An expression's `:step` and a newtype's
`step()` are deliberately separate — the constraint composes with `precision()` and the
newtype's domain, the expression drives a loop counter — but they did not agree on what a
legal step *is*: the expression form was checked for numeric type-compatibility and nothing
else, and the constraint form was collected and validated by nothing at all.
`types.InvalidStepReason` is the shared rule (`lyra-E033`): zero never advances, and a
fractional step cannot be represented over an integer domain. Type compatibility does not
subsume it — `0..<10:0` type-checks perfectly and is a loop that cannot terminate. A
negative step is deliberately *not* judged there: which direction a range runs is unsettled,
and inventing an answer inside a well-formedness check is how the two would drift apart
again. Still open: a step constraint is not enforced against values at run time, so
`step(0.25)` documents and validates but does not yet reject 0.3.

**`RangePattern.GetName` printed the operator after the bound it qualifies** — `0..9=` for
`0..=9`. It reaches users (diagnostics interpolate it) but no golden file, which is how it
survived. Fixing it surfaced a larger one in the same messages, left alone and noted in
todo.md: a literal renders as `IntegerLiteralExpr(0, Base: 10)`, so a real diagnostic reads
"expected array pattern, got IntegerLiteralExpr(0, Base: 10)..=IntegerLiteralExpr(10, Base:
10)".

One thing this cost that is worth writing down: **a recovered parse is not an absent
bound.** Where the grammar requires a bound, tree-sitter inserts one to keep going —
`range(..)` yields a zero-width `decimal_int` on the `)` — and a nil check reads that as a
bound of value zero. `collector_ctx.RangeBound` tests missing-or-empty, and the pre-existing
"range constraint must have a start or end" check only kept working because of it.

**Types and traits have per-module identity.** Two modules can each declare a private
`Point`, and a module declaring its own `Maybe` no longer takes the prelude's away from
everybody else. Both were one missing piece, and `todo.md` had said so: their namespace was
program-wide *by construction*, because `SymbolTable.Types` was keyed by bare name.

**Nothing new was invented — types were made to follow the rule bindings already had.**
`declKey` (the old `functionKey`) gives a declaration its own name when it is `pub` or in
the entry module, and `<module>::<name>` when it is private, or when it takes a prelude
name whatever its visibility — so the prelude keeps the bare key for every module that did
not shadow it. `FunctionKey` and `TypeKey` are now two names for that one function rather
than two rules that happen to agree, which is hazard 4's whole point: they would have to
agree anyway, since "whose declaration is this" does not depend on the kind of declaration.
`noteShadowed` consequently does nothing but record the warning; withdrawing the prelude's
entry was only ever a way to make one `Maybe` fit in a namespace that had room for one.

**The reachable symptom was a diagnostic about a declaration you had never seen.** A module
that never mentioned `Maybe` lost the canonical one the moment *another* module shadowed
it, and its `?` reported `` `?` operand must be a Result or Maybe, got Maybe ``. `todo.md`
called that message indefensible; it was the namespace, not the message. What is left is
the same message in the module that actually did shadow — poor, but about something its
author did (todo.md, Pit of Success #1).

**Three things had to move with the key, and each was a separate way to be wrong.**
Registration writes the module scope *before* computing the key (the key is read out of
that scope, and unlike functions — registered in `Finish`, after every file — types
register mid-walk, so there is no later point at which it is populated) and no longer
defines into the global scope at all: publication is `exportToGlobal`, the same path a
`pub` binding takes, which is what stops a private type competing for a program-wide name.
The typechecker's `resolvedTypes` cache was keyed by bare name, which would have let the
first module to mention a type answer for every other one — precisely the hazard that had
already kept the *visibility* check out of that cache. And the backend's `structTypes`
registry needed the same key.

**The backend could not thread a location, so it carries one.** `funcKey(name, loc)` works
because a call site is a node; a resolved `NamedStructType` reaching `lowerType` is just a
name, and `lowerType` is reached from nearly every expression. The lowerer therefore holds
`currentLoc`, set per type declaration/definition and inside `declareFunctionAs` /
`defineFunctionInto`. **Those two shared bodies, not `declareFunction`/`defineFunction`** —
a trait method and a generic specialization lower through them too, and putting it on the
outer pair left those resolving names against whichever module was lowered last. A location
rather than a module path so the backend deals in the same currency as everything else and
the symbol table keeps sole authority over file → module.

**Measured, not predicted:** two same-named private structs emitted a single `%Point = type
{ i64, i64, i64 }` — the union of both field lists — which clang rejected as a redefinition.
Loud, but at clang rather than as a Lyra diagnostic. A generic instantiation's symbol
(`Box$i64`) is derived from the bare name and is a separate path, so it would still have
collided after the plain-declaration fix; it is qualified too.

**Privacy for types became structural, and took the message down with it.** A private
declaration simply is not found from another module, exactly as a private binding is not in
the global scope. That is the right mechanism and, alone, the wrong message: "unknown type"
reads as a typo for a name plainly visible in another file. `lyra-E028` is recovered on the
not-found path (`reportPrivateType`, via the new `DeclaringModulesOf`, which is the exact
form of the question `ModuleOf` answers approximately).

**One bug this surfaced immediately, and it was the old one wearing a new hat.** `impl Size
for Point` in module one reported *its own* `Point` as "private to module two". `visibilityOf`
found a declaration by name through `DeclaringModule` — last-writer-wins — so with two
private `Point`s it answered for whichever was collected last. That is the identical mistake
the bare-**call** path made before privacy became structural (asking whether *some*
declaration of that name was private, rather than whether *the one this reference resolved
to* was visible), and it has the same fix: `declVisibility` asks about the declaration in
hand. A namespace member asks `visibilityIn(imp.Path, …)`, about the module the import names.

**A map key is not a source name.** Two user-facing readers iterated `SymbolTable.Types` and
used the *key*: LSP completion would have offered `one::Point` as a type to type, and
`captures` would have recorded it as a declared name while missing the real one. Both read
`decl.Name` now. Worth stating as a rule (CLAUDE.md hazard 4) because the map is the obvious
place to enumerate declarations from and the keys look like names until one is qualified.

**Coverage.** `pkg/modules/type_identity_test.go` pins the three front-end cases;
`pkg/backend/llvm/llvm_type_identity_test.go` compiles and *runs* four two-module programs
whose same-named private types have deliberately different shapes and field orders, so a
collision cannot accidentally produce the right answer. The LSP tests are honest about being
smoke tests: the server analyses one document through `driver.Analyze`, which builds a unit
with no `Path`, so the collector sees module `""` and a qualified key never arises there
yet — reverting the fix still passes them. They become load-bearing when the server resolves
a real import graph. Whole suite green on macOS and on Linux (`./asan.sh ./...`), ASan clean.

**Still program-wide:** two modules exporting the same type name is an error, as it is for
two exported functions — a bare reference could mean either, so it is a genuine clash rather
than something privacy resolves.

**A written generic parameter list is authoritative.** `let mismatch<t> = (a: u) -> u => a`
compiled and ran: it declares `t`, is generic in `u`, and nothing reconciled the two. The
list is now checked in both directions — a signature variable absent from a written list is
`lyra-E031`, a declared parameter the signature never mentions is `lyra-W013`
(`checker/generic_params.go`, before typechecking, AST-only).

**The list stays optional, and that is not a compromise.** Type variables are *lexical* — a
lowercase type name is a variable wherever it appears — so `let unbox = (b: Box<t>, fb: t)
-> t` is generic with no list and stays legal. That much follows from the lexical rule.
What never followed from it is the list being unchecked *when written*, which is the only
thing that changed. This was option (b) of three: (a) warn on both mismatches, (c) require
the list outright. (c) is the least ML-ish and buys little over (b).

**Why an error rather than the cheaper warning.** Both catch the typo, and the typo is the
motivating case — a misspelled lowercase type name does not fail, it silently becomes a
*new* type variable, so the function turns generic in something its author never meant and
the diagnostic (if any) lands at a call site or in the backend. That is how the prelude's
`ok`/`err` shipped without their `<t, e>` and drew nothing. But only an error settles the
part that outlives the typo: the list is the only place a **bound** can be written
(`<t: Show>`), so a list that need not agree with its signature means a constraint can
silently constrain nothing. An unenforced bound and a bound on a variable nothing solves
are indistinguishable from outside, and only the first stops being a problem when bound
enforcement lands — which is the argument for doing this *before* that, not after. The
warning half says so explicitly when the unused parameter carries a bound.

**The `where` half is enforced in the collector, and had to be.**
`Collector.MergeWhereConstraints` merges `where u: Show` into the matching list entry and
*silently discarded* a name matching nothing — so a bound on an undeclared variable was
already gone by the time there was an AST to check. Reported at the point the name still
exists, under the same code. This also covers trait declarations, which share the merge.

**Three copies of "which type variables does this signature mention?" became one.** The
pass needed that walk, and adding a third switch was the one thing todo.md asked whoever
took this not to do: the existing two — the typechecker's `collectTypeVars` and the
backend's `mentionsTypeVar` — had already drifted, the backend's missing
`ParameterizedType`, which is the 07/30 build failure in this log. Both now delegate to
`types.CollectTypeVars` (`pkg/types/typevars.go`); `MentionsTypeVar` is defined *over* it
rather than as its own short-circuiting switch, trading a map allocation for the guarantee
that a case added in one place cannot be missing in another. Taking the union turned up two
composites **neither** copy had: `AnonymousStructType` (structural — `(p: { v: t }) -> t`
writes its field types out in the signature) and `RangeType`. Nominal types are
deliberately *not* walked, and the unified walker says why: a `struct Box<t>` binds its own
`t`, so descending into it from a signature that merely mentions `Box<i64>` would report a
use and make every function touching a generic type spuriously generic.

Scope: bindings only. Type declarations, traits and impls have the same unreconciled list
and are tracked in todo.md — a type declaration's arity is at least load-bearing at
instantiation, so a mismatch there tends to surface as an arity error rather than silence.

The full suite, `std/prelude.lyra` and `std/maybe.lyra` reconcile with no new diagnostic —
the prelude's lists were corrected when `ok`/`err` were fixed, so this pass confirms them
rather than finding them. Verified non-vacuous the way this project verifies things: with
`CheckGenericParams` stubbed to return nil, 13 of its tests fail.

**`?` lowers.** The language's primary error-propagation operator type-checked — including
the enclosing-return kind and error-type checks — and then failed the build with
`expression lowering not implemented for *ast.TryExpr`, so no program could actually use
it. `pkg/backend/llvm/try.go` closes that. Verified end to end against the real
`std/prelude.lyra` (not just the single-file test harness): Result and Maybe, success and
propagating paths, through `lyrac build`.

`x?` is a `match` in disguise and lowers as one — tag test, unwrap on success, propagate on
failure. **The one thing that is not a plain match is the error arm**, and it is the whole
reason this needed a lowering of its own rather than a desugaring: the propagated value has
a *different LLVM type* from the operand. `?` on a `Result<i64, string>` inside a
`-> Result<bool, string>` function cannot forward the operand's union, because those are
two distinct monomorphizations, so the error payload is extracted and a fresh `Err` is
built around it at the enclosing type. That is what `retLyra` (the unlowered return type,
new on the lowerer) is for — the LLVM return type alone cannot say which constructor to
build or what the payload's Lyra type is. `TestExec_TryRebuildsErrorAtEnclosingType` is
pointed at exactly this, since a skipped rebuild emits a type-confused union rather than a
wrong answer.

**The bug found on the way there was in the ownership pass, and it was the more serious
half.** `pkg/analyzer/ownership` had no `*ast.TryExpr` case at all, so a `?` operand was
never visited — and that package's own doc is explicit that skipping a node is *not* the
leak-safe direction the rest of its bias assumes: a missed retain at an owning position
dangles rather than leaks. This was reachable without any of the lowering above: in
`parse(name)?` the operand's own sub-expressions went unannotated, so a managed value
inside one missed its retain. The case added mirrors `MatchExpr`/`MemberExpr` — the operand
is borrowed like a scrutinee, and the payload read out of it is duplicated in an owning
position, never moved.

**Whether the propagated payload is duplicated or moved is decided by how the operand's own
reference is disposed of**, and it has to be, because both mistakes are real bugs in
opposite directions. A *borrowed* operand (a binding) still owns its copy and will release
it — at scope exit, or via the `releaseAllManagedFrames` the propagating return itself runs
— so the rebuilt error needs a reference of its own. An owned *temporary* is not released
on that path, so its reference is what the error carries away and a dup would leak it. That
distinction was not taken on faith: inverting it (always transfer) makes
`TestExec_TryBorrowedOperand` print fifteen NUL bytes out of freed memory and
`TestASan_TryManagedPayload` report a fault, which is the check that these tests are worth
having — this suite has passed vacuously before.

**The propagating return deliberately does not flush the enclosing statement's
temporaries.** `emitReturn` releases each pending temp *in the block that produced it*, and
the operand's producing block is the one before the branch — so flushing from the error
block would place a release ahead of that block's own terminator, freeing the operand ahead
of the tag test that reads it, on the **success** path as well. Raising `pendingBase` leaves
the release where it belongs (the enclosing statement's flush, on the success path). The
residue is any temp produced by a *sub*-expression of the operand, which leaks on the
propagating path — the same conservative bias break/continue take, and tracked in todo.md
with the fix that would serve all three: a release block on `pendingTemp` rather than a
production block.

`buildDataValue` (aggregates.go) is new and shared: the write-side mirror of
`extractDataPayload`, and now the one place DATA_LAYOUT.md's `{ tag, payload-blob }`
encoding is *written*. `lowerDataConstruction` reaches it with lowered argument
expressions, `?` with values extracted from another instantiation's union — two callers
from opposite directions, which is precisely the shape that drifts if each keeps its own
copy of the layout.

`TestConservation_TryReleasesEnclosingScope` covers the other direction ASan cannot see: a
managed binding allocated *before* the `?` must be released on the propagating path too,
since `?` leaves every enclosing scope at once exactly as `return` does. A leak on one edge
of a branch is invisible to a count of allocations against releases.

### 07/31/26
**Return-type inference for a function written without `-> T`.** `let sum = ((a, b): (i64,
i64)) => a + b` now builds. It type-checked before and then failed the *build* with
"function needs a return type annotation" — the same front-end-accepts-what-the-backend-
refuses split that default params, multi-clause functions and destructuring parameters
each had, and the last one on the list from the higher-order-readability discussion.

The fix is an elaboration, not a new pass: `inferLambdaReturnType` writes the body's type
**onto the AST node**, exactly as `contextual_lambda.go` fills a lambda literal's missing
annotations. Everything downstream reads `ast.LambdaExpr.ReturnType`, so filling the blank
once means ownership, captures and the backend need no notion of an un-annotated function.

**Scope, chosen rather than stumbled into.** The body's *value* is the return type. A body
containing an explicit `return` is refused with a diagnostic asking for an annotation,
because inferring it means joining several candidates, and what happens when they disagree
— or when one arm diverges through `panic` — is a design question that deserves its own
decision rather than whatever a first implementation happens to do. The refusal is still an
improvement on what it replaces: a front-end diagnostic naming the fix, where there was a
backend error naming an internal requirement.

Recursion mostly works and does so for a reason worth knowing: `fact` infers fine because
`if n == 0 { 1 } else { … }` takes its type from the first arm, so the recursive call is
never consulted. When the recursive branch does come first, inference cannot finish and
says so — the cycle guard added earlier the same day is what makes that a diagnostic
instead of a stack overflow.

**The interesting interaction was the entry point.** `let main = () => { 0 }` is a
*documented* spelling of a void entry point. Inference fills the blank from the trailing
`0`, so `main` became `i64` and the entry check rejected it — "must return u8 or void, got
i64" — breaking a program that compiled the day before, via a feature with nothing to do
with it. Caught by `TestBuild_Clean`, whose fixture is exactly that shape.

The resolution keeps the convention: only a *written* annotation decides, so
`ReturnTypeInferred` marks the filled-in case and the entry point discards it, resetting
the node to `void` rather than merely classifying it — otherwise the backend would build a
return value nobody reads. `-> u8` is still an exit code, `-> i64` is still the error it
always was, and an inferred *u8* is honored, since there the inference and the convention
agree. That flag is the only place in the compiler that needs to tell a written signature
from a derived one, which is the argument for it being one narrow flag rather than a
general "was this elaborated?" facility.

### 07/31/26
**Transparent `type` aliases.** `type Op = ((i64, i64)) -> i64`. The name and the type are
interchangeable — no conversion at the boundary, no identity of its own. Grammar in
`tree-sitter-lyra` 0741c3c; this side carries the collector and the three places that had
to learn about them.

The motivating case is a function type, which is where the language reads worst. A
callback parameter spelled out is `(g: ((i64, i64)) -> i64, p: (i64, i64)) -> i64`, and
the double parens are *not* removable — single parens would be a two-argument function, so
a single tuple parameter has no shorter form. `newtype` could not serve: it is nominal, so
it makes the value un-callable without unwrapping. Which is the argument for both
declarations existing rather than one flag: an alias removes repetition, a `newtype` adds
meaning at a boundary.

**The implementation is one decision — register the aliased type *itself* under the
alias's name** — after which most of the compiler needs no notion of aliases at all.
Three places did:

- **`resolveType` had to stop after one hop.** `type Point = Pt` registers
  `UnresolvedType{Pt}`, so returning `decl.Type` handed back a *name*, and assignability
  then rejected a real `Pt` with "cannot assign Pt to Point". It now resolves what the
  declaration holds, which walks alias chains and leaves everything else alone (a struct
  or data declaration lands on the switch's default and returns as-is).
- **A cycle guard came with it.** `type A = B; type B = A` would otherwise recurse until
  the stack ended — the type-level twin of the `inferExprType` guard added the same day,
  and worth noticing that the *second* piece of work in a row needed one. Any resolver
  that follows a user-controlled edge needs an in-progress set; the pattern should be
  reached for by default now rather than after the crash.
- **The backend had to skip the declaration and expand the name.** An alias holds the
  aliased type itself, so without an explicit `IsAlias` marker `type Point = Pt` would
  declare *and define* Pt's LLVM struct a second time under the name `Point`. `IsAlias` is
  on the AST node because `Type` genuinely cannot distinguish the two. And since the
  typechecker resolves types without rewriting the AST, an annotation still says `Op` when
  it reaches codegen, so `lookupNamedType` expands an alias there — the one place a named
  type is resolved.

Validation happens at the **declaration**, not the use: an alias nothing mentions is still
checked, so an unknown target or a cycle is reported even when unused. A declaration that
cannot mean anything should not need a use site to be told so.

One consequence accepted on purpose: the alias name is gone from diagnostics. A mismatch
on an `Op` parameter names the function type, not `Op`. That is correct for a transparent
alias — the name is a spelling, not an identity, and claiming otherwise in a message would
be claiming an identity the type does not have — but it is the thing to revisit if the
messages read badly.

Tests: `typechecker/tests/type_alias_test.go` (transparency at each comparison site,
chains, the cycle, declaration-site validation, and an explicit "is not nominal" case that
would fail if someone later made aliases nominal) and `llvm_type_alias_test.go`, including
an IR assertion that a struct alias emits exactly one type.

### 07/31/26
**Fixed: a definition cycle crashed the compiler, and with it the language server.**
`let f = f(1)`, or the mutual `let a = b(1); let b = a(1)`, sent inference around a loop
until the Go stack was exhausted. Not an error — a **process death**: `lyrac` printed a
runtime traceback instead of a diagnostic, and `lyra-lsp`, which runs the same
`driver.Analyze` on every keystroke, simply vanished. In an editor that reads as
completions and diagnostics going quiet with no explanation, and it fires exactly when a
half-written cycle exists, which is most of the time you are typing one.

The loop is `inferIdentifierCall` → `inferExprType(decl.Value)` → back to
`inferIdentifierCall`: a binding whose type must come from its initializer, whose
initializer names the binding. **The cache could not break it** — it is written *after* the
recursive call returns, so a cycle never finds an entry. Marking the node in-progress on
the way *in* is the whole fix, four lines in `inferExprType`.

Two things about where it went. It is in `inferExprType` rather than in the call path that
exposed it, because a cycle is a property of the graph and not of any one route through
it — that is the single entry point every recursion passes, so any shape is caught. And it
returns nil, "cannot be determined yet", which every caller already handles.

*How it was found is the part worth keeping.* The original repro needed a **syntax error**:
`{ let f = mk(1); u8(f(3)) }` before semicolons were legal, where error recovery produced
two call nodes that inferred through each other. That framing — "reachable from a malformed
AST" — made it look like an error-recovery problem and lower priority than it was. When the
semicolon change made that program valid, the repro stopped reproducing, and reducing it
from scratch produced `let f = f(1)`: two tokens, no syntax error, a plain typo. The bug
was always reachable from ordinary source; the first repro just disguised it.

Also fixed on the way past: the diagnostic reached for a nil type and rendered
`identifier "f" is not callable (type %!s(<nil>))` — a Go format verb in a user-facing
message. It now says the definition depends on itself and suggests breaking the cycle, and
a test asserts no diagnostic ever contains `%!`.

Tests: `typechecker/tests/definition_cycle_test.go` (the three cycle shapes, plus the two
cases a too-broad guard would break — a legitimate `let add5 = makeAdder(5)`, and a
genuinely non-callable binding keeping its own typed message) and
`driver/driver_test.go`, which asserts through `Analyze` because "always returns, for any
input" is the property the language server needs. Verified the tests catch it: with the
guard stashed, the test binary dies with `fatal error: stack overflow`.

### 07/31/26
**Fixed: a negative literal in a `match` pattern now parses.** `-1 => …` and
`-128..=127 => …` never did. `_number_literal` carries no sign and both `literal_pattern`
and `range_pattern` were defined over it, so the `-` landed in an `ERROR` node.
Pre-existing; found because the statement-terminator work changed how the wreckage looked.

**Why it survived this long is the interesting part.** The error swallowed the *whole*
match, so the collector never built a match expression, the exhaustiveness check never ran,
and `TestTypeCheck_NumericMatch_I8_FullRange_Ok` — which asserts *no* errors on
`match x { -128..=127 => "ok" }` — got none and passed. **Vacuously.** A test that asserts
an absence is satisfied by a parse failure, which is the failure mode to remember: an
"assert nothing went wrong" test cannot tell "it worked" from "it never ran". Better error
recovery under the new grammar made the check start running, and it correctly objected to
the `128..=127` it could see, which is what surfaced this.

The fix is a signed form for both pattern rules, **aliased to `negation`** so the CST shape
is one the tree already contains: `collectRangePattern` reads `start`/`end` with
`CollectExpr`, which handles a `negation` with an `operand` field and would not know a new
node kind. Two details that cost a cycle each. It has to be a *named* rule that is then
aliased — `alias(seq(…), $.negation)` inline is not a node of its own, so its
`operator`/`operand` fields hoisted onto the enclosing `range_pattern` and displaced
`start`/`end`, leaving the collector with nothing. And the sign cannot live in the token:
`decimal_int` swallowing a `-` would lex `a-1` as `a` then `-1` instead of subtraction.

Two conflict declarations, both mirrors of entries already there for the unsigned case
(`[expression, _signed_number_literal]` replacing the old `[expression, literal_pattern]`,
which now warns as unnecessary, and `[_math_operand, _negated_number_literal]`). The
grammar's own comments flag this as the finely balanced region, so `0 - 200` still parsing
as subtraction — not as `0` plus a dangling `negation(-200)` — was checked explicitly.

Tests are deliberately of the kind that cannot go vacuous: exec cases in
`llvm_match_test.go` where a dropped sign is a wrong exit code (`-5` must take the
`-128..=-1` arm, not `0..=127`), and two typechecker tests that assert a diagnostic is
*produced* rather than absent.

### 07/31/26
**A line break now ends a statement, and `;` is the explicit form.** A `tree-sitter-lyra`
change; the reasoning lives in that repo's commit and `CLAUDE.md`. What matters on this
side is *why it was worth a breaking grammar change*, and what it cost here.

It started as an ergonomics question — whether to allow `;` so several statements could
share a line — and the premise turned out to be inverted. Several statements per line
**already worked**: `block` was `seq("{", repeat($.statement), "}")` with no separator at
all, and newlines were only `extras`. So `;` would have added no expressiveness. What the
missing separator *did* add was a silent misparse, since a line break meant nothing to the
parser and the parse was maximally greedy:

```
let b = a          let f = add3        let n = xs
-2                 (4)                 [1]
```

Each ran as one statement — `a - 2`, `add3(4)`, `xs[1]` — with no diagnostic. That is why
the change is "newlines become significant, `;` is the explicit form" rather than "`;` is
now allowed": optional `;` alone fixes nothing, because nobody writes a terminator on the
line they did not know needed one.

**Fallout here was mechanical and small.** About twenty test fixtures put two statements on
one line separated only by spaces (`{ n = n + 1  n }`), which is exactly what no longer
parses; they now use `;`, and read better for it. One `lyrac` golden moved: the syntax
error for `let x = = 5` went from column 7 to column 9, pointing at the *second* `=` — the
better answer, since `let x =` is fine.

**One test-fixture note worth keeping.** `std/` needed no changes at all, and neither did
any multi-line construct: the terminator is only offered where the grammar accepts one, so
a newline inside an unfinished expression never produces it. Match arms, argument lists and
multi-line `data` declarations are all untouched.

**And it exposed a pre-existing bug**, now in `todo.md`: a negative literal in a pattern
(`-1 => …`, `-128..=127 => …`) has never parsed. Two numeric-match tests were passing
*vacuously* — the old parser wrapped the whole match in an `ERROR`, so the collector never
saw a match expression and the exhaustiveness check never ran. Better error recovery under
the new grammar makes the check run, and it correctly objects. Those two tests are red
until the pattern gap is fixed; that is the honest state, not a regression.

**Process note, and it cost real time:** A/B-ing the old and new parsers by stashing
`src/parser.c` repopulates Go's build cache with the *old* compiled parser, so the next
`go test` silently runs against it — presenting as "the semicolons I just added are syntax
errors." `go clean -cache` after **every** parser swap, including a temporary one done only
for diagnosis. This is hazard 1 in `CLAUDE.md`, walked into from the one direction the note
did not spell out.

### 07/31/26
**Fixed: the shared AST walker never descended into a tuple index, and `pure` was unsound
because of it.** `p.0` is a `*ast.TupleIndexExpr`, a different node from `p.x`
(`*ast.MemberExpr`), and `ast.walkExprChildren` had no case for it. So every pass built on
`WalkExpr` was blind to anything reached through a tuple index — and each consequence read
as a bug in the pass that suffered it, not in the walker:

- **`pure` accepted an impure call**: `pure () -> i64 => noisy().0` drew no diagnostic,
  while the identical program through a struct field (`noisy().x`) was correctly rejected
  with `lyra-E007` the whole time. A soundness hole in the effect system, reachable by
  typing two characters.
- **A closure capture was missed**, which is a *build failure*: a lambda whose only use of
  `p` was `p.0` got no environment slot and died in lowering with `unbound identifier
  "p"` — on a correct program, and only when no other use of `p` happened to be present.
- **Use-before-declaration missed `b.0`**, and both "never used" warnings (`lyra-W005`
  parameter, `lyra-W003` variable) fired on names that were plainly used.
- Ownership last-use lost precision, which is the one harmless case: a missed last use
  falls back to the scope-exit frame release, so it defers a free rather than double-freeing.

One `case *TupleIndexExpr` fixes all of them, and no existing test changed — nothing had
come to depend on the gap. Found while probing why higher-order signatures read badly,
which is the second time a readability question has surfaced a correctness bug.

**Fixed: `resolveType` left named types unresolved inside function types and inside a
parameterized type's arguments.** The symptom is assignability rejecting a type against
itself, because one side expanded the name and the other did not:

```
cannot assign (Pair(i64, i64)) -> i64 to (Pair) -> i64      // *types.LambdaType
cannot assign Box<Pt> to Box<Pt>                            // types.ParameterizedType
```

The static-array, dynamic-array, tuple and weak cases in that switch each carry a comment
saying this is precisely what they exist to prevent; these two composites had no case. The
`LambdaType` one only bit *through* a function type — a plain `p: Pair` parameter always
worked — so naming a type failed exactly where a signature is long enough to want the name,
which is why it went unnoticed. `ParameterizedType` bit a plain parameter too. The
`LambdaType` case returns a **copy**: it is the one type held by pointer, so resolving in
place would rewrite the declaration every other reference shares.

Both are the hazard now written up as rule 8 in `CLAUDE.md` — the same omission
`mentionsTypeVar` had, in a third switch. Tests:
`typechecker/tests/named_type_in_composite_test.go`,
`checker/tuple_index_use_test.go`, and an exec test for the capture failure in
`llvm_closure_test.go`.

### 07/31/26
**Destructuring parameters lower.** `let sum = ((a, b): (i64, i64)) -> i64 => a + b`,
`({ x, y }: Pt)`. Parsed, collected and type-checked since long before; the backend refused
them in two places ("destructuring parameters are not implemented yet"), which is the same
front-end-enforces-what-the-backend-can't-build gap default params and multi-clause functions
had. Like those, it was not a codegen project: a destructuring parameter is the **fourth
destructuring form**, and the machinery the other three drive (`patternMatcher` →
`aggPatternTest`/`aggPatternBind`) was already there. Routing it through the same helper is
the point — two implementations of "does this value match this pattern" would drift.

It is the **irrefutable** form, and that is *checked*, not assumed. A parameter has no failure
path — no `else`, no next arm, and a function cannot decline to be called — but the typechecker
happily admits a value-testing sub-pattern in one (`((1, b): (i64, i64))`, and
`(Just(v): Opt)`, which the grammar accepts outright). Both are now refused with a message
naming the fix, the same way `lowerDestructuringDecl` refuses `let Some(v) = m`.

The two parameter-binding loops became one, `bindParameters`, and that is what made the feature
reach every shape of function at once: a plain function, a generic specialization, a lifted
closure (its `ir.Param` slot 0 is the environment, so the Lyra parameters carry an offset), and
a **trait-impl method**, whose clause patterns *are* its parameters via `traitMethodLambda`.
The trait case needed a front-end change to match: `checkTraitImplMethodBody` bound only
identifier patterns, so `total = ({ x, y }) => x + y` reported `x` and `y` undefined. It now
walks the pattern against the trait signature's parameter type with the same
`walkDestructuredPattern` `withParamScope` uses — and the impl is the one place an *unannotated*
destructured parameter works, because the signature supplies the type a free function has to
write.

**Ownership follows the rule the other pattern forms already use:** a bound name is a
**borrow**, never framed, because it is a field copy out of a value someone else owns. For a
bare or `ref` parameter that owner is the caller; for an `own` one it is the callee, so the
*whole* incoming aggregate is framed for one release that `drop.go` walks into every managed
field. That is deliberately not one release per bound name — a pattern need not name them all,
and `({ age }: own Person)` must still free `name`. A field that escapes gets a retain for
free: a pattern name has no declaration inside the function, so it is never last-use-eligible.
Both directions are ASan-clean, and the refcount shape itself is pinned (exactly one release
for the aggregate; a retain for the escaping field), because "it exits with the right code" and
"it frees the right number of times" are different claims.

Two refusals, both stated rather than incidental. A **`mut`** parameter cannot be destructured:
its bindings would be copies, so a write could not reach the caller — which is the whole content
of `mut`, and lowering it would be a mutable borrow that silently is not one. **`ref` is
supported**, by loading the pointee and destructuring that: it is read-only, so copying the
fields out is unobservable, and a destructuring parameter asked for them by value anyway. The
load is the copy by-reference exists to avoid, which is an argument about cost, not correctness.
An **array**-pattern parameter still fails, with the same message `let [a, b] = arr` gives —
static-array patterns are unimplemented everywhere (`match` on one is refused too), so that is
not a gap in this feature.

Tests: `llvm_destructuring_param_test.go` — 16 exec cases across all four function shapes,
`shared`/`ref`/`own` parameters and managed fields, every one repeated under ASan, plus the two
refusals and the refcount-shape assertions.

### 07/31/26
**Default parameter values work.** `add(5)` against `(a: i64, b: i64 = 10)`. Like multi-clause
functions, they were already parsed, collected, and honoured by the arity check — which counts
required parameters and allows a call to omit the rest — so the only thing missing was that
the **call site never received them**. The backend saw a call shorter than the parameter list
and refused the function outright.

They are filled in the front end (`typechecker/default_args.go`): the declaration's default
expressions are appended for any trailing arguments the call omits, before arity is counted or
the generic path is taken. Everything downstream then sees a call that passes every argument
explicitly, so the defaults are type-checked against their parameters like any other argument
and the backend needed only to *stop refusing* — in two places, since specializations have
their own parameter-lowering loop.

Two decisions inside it. The appended expression is the **same AST node** as the declaration's
default rather than a copy, so two call sites that omit it share one node; that is sound for
everything keyed by node, since a default is evaluated against the parameter's declared type
and cannot vary by caller, and cloning would need a deep AST copy this compiler avoids
everywhere else. The case that would expose a problem — a heap-allocating default at several
call sites — is covered by an exec test and is ASan-clean.

And a defaulted parameter followed by an undefaulted one is now **rejected**. It used to be
silently accepted: the arity check counts required parameters without checking their *order*,
so `(a: i64 = 1, b: i64)` called as `f(5)` bound 5 to `a` and left `b` unfilled — a call the
programmer plainly did not mean, accepted without a word.

Still refused, and now for a stated reason rather than for want of lowering: a default on a
lambda used as a **value**. Defaults are filled from the callee's declaration and an indirect
call has none — a function type records that a parameter has a default, not what it is.

### 07/31/26
**Multi-clause functions lower.** They always parsed, collected and type-checked — only the
backend refused, with "multi-clause functions are not implemented yet". So this was never a
codegen project: a multi-clause function *is* a match on its parameters, and the match
machinery it needs (the if-else ladder, tuple destructuring, guards, the sealed fall-through)
was already there and tested. Verified before writing anything, by running the hand-written
equivalent: `match (n, a, b) { … }` compiled and returned fib(10) = 55.

So it is a **front-end desugaring**, in `checkLambdaBody` before the body is walked. It has to
be: the backend reads every type by AST-node identity, so a match synthesized *there* would
have no TypeTable entry for any of its nodes. Synthesized in the typechecker, it is typed like
any other match and the ownership, capture and lowering passes needed no changes at all.

Four details that are decisions rather than mechanics. A **one-parameter** function matches its
parameter directly rather than through a one-element tuple, so it reaches the scalar ladder.
The clauses are **consumed**, or `checkLambdaBody` would check every clause body a second time
and turn one mistake into two diagnostics. **Arity is checked in the desugaring**, with the
counts named, because left to the synthesized match it surfaces as a tuple-shape error about a
tuple nobody wrote. And **no clause matching traps** (exit 101, `lyra: match not exhaustive`)
rather than being undefined — the right semantics for a function-clause error, and what Erlang
and Elixir do.

**Generic multi-clause functions work too**, which needed a second, unrelated fix. The backend
had refused them twice — once for being multi-clause, once in `declareSpecialization` — and
with the body desugared, a third failure appeared: `data pattern on non-data value of type
Opt<i64>`. `resolveDataType` had no `ParameterizedType` case, so a data pattern *nested inside*
an aggregate pattern (`(Just(v), _)`) failed where a top-level one worked, because the
top-level path normalizes its scrutinee and the sub-pattern path reads the element type
straight off the tuple. Pre-existing and independent of clauses — the hand-written tuple match
fails identically, which is how the regression test is written.

### 07/31/26
**A lambda literal now takes its missing annotations from context.** It used to take
nothing: `(x) => x` reported `undefined symbol "x"` because an unannotated parameter never
reached `tc.paramTypes`, and `() => 7` was rejected against `() -> i64` because the body's
untyped leaf never learned the expected width. Only a fully annotated lambda worked, which
meant every call site of every lazy combinator had to restate types the signature already
gave — `maybe.map(m, (x) => x * 2)` was not writable.

**The fix is elaboration, not a second inference mode.** When a lambda literal meets an
expected function type, its blanks are filled *on the AST node* before its body is inferred,
so everything downstream sees the lambda the user would otherwise have had to write by hand:
`withParamScope` seeds the parameters because they now have types, `checkLambdaBody` checks
and width-propagates the body because there is now a declared return, and the backend — which
reads `ast.Parameter.Type` to lower a parameter — needed no change at all. One mechanism, both
halves of the bug. It only ever fills what was left blank, so an explicit annotation still
wins and can still be diagnosed as wrong.

Wired at the three sites that know what they want: an annotated binding (which had to go in
the *lambda-valued* branch of `checkVarDecl`, which returns before the general path), a
direct call's arguments, and a generic call's.

The generic case is the one with a real ordering problem, and it took three passes to get
right:

1. A bare lambda cannot be inferred until it knows what is expected, but `(t) -> u` is not
   concrete until the *other* arguments solve `t` — so `solveTypeVars` defers incomplete
   lambdas to a second pass. A **fully annotated** lambda is not deferred, since it can solve
   variables itself (`or_else(Nil, () -> i64 => 0)` binds `t` from the callback's return) and
   deferring it would lose that.
2. A type still mentioning a variable must **not** be planted: in `map(m, (x) => x * 2)`,
   `u` is solved from *this lambda's own body*, so writing `u` as the declared return leaves
   it unsolved forever — the symptom was "cannot convert u to u8" at the use site.
3. Which means the return type can only be filled *after* solving completes, in
   `inferGenericCall`, or the lambda reaches the backend as a value with no return type.

One existing diagnostic changed, deliberately. `apply((n: u8) => n, 0)` against
`(u8) -> string` used to report "cannot assign (u8) -> u8 to (u8) -> string", from inferring
the lambda in isolation and comparing signatures; it now reports "return type mismatch:
expected string, got u8" against the body. Both are true and the second points at the
expression that has to change — and it is the same mechanism that makes `apply(() => 7, 0)`
work at all, since a context that can supply a return type has to supply it before the body
is walked.

### 07/31/26
**Borrow modifiers on trait signatures — `ref` and `mut`, with `own` rejected.**
`bump: (mut Self) -> void` now writes through to the caller and `peek: (ref Self) -> i64`
borrows without copying. The grammar was never the gap, contrary to the entry this replaces:
`trait_method_signature` is an aliased `lambda_type` and its `parameter_type` has always
carried an optional `type_modifier`, so `(mut Self, own i64) -> void` parsed all along.
`Collector.parseParameterType` read only the `type` field, and `types.ParameterType` had no
field to put a borrow in — `Modifier` there is the `stack`/`shared` allocation flavor, a
different axis. It gained `Borrow`.

Four passes had to agree, and the interesting part is that three of them are only correct
*together*:

- **collector** reads the modifier; **`traitMethodLambda`** carries it onto the synthesized
  parameter, which is the line the old comment warned about ("or the call site and the body
  will disagree about who owns the receiver");
- **backend** passes the receiver and each argument by address when its parameter is a
  by-reference borrow (`methodOperand`), mirroring `lowerDirectCall` — it cannot share that
  loop, because a method call's receiver is not in `call.Arguments`, and that offset is the
  whole difference;
- **typechecker** applies the same `checkMutArgument`/allocation checks a free call gets, so
  a `mut` argument must be a mutable lvalue rather than a temporary whose writes vanish;
- **ownership** learned to read a method's modes at all — `resolveCallee` returns nil for a
  `.`-call, so every method argument previously fell to the conservative transfer. It now
  resolves the trait signature through the `MethodTable` (threaded into `Analyze`), again
  with the receiver at index 0 and arguments from 1.

**`own` is rejected (`lyra-E030`), and that is a measurement rather than caution.**
Implemented alongside the rest, `take: (Self, own string) -> string` compiled to a
heap-use-after-free — ASan report, read-after-free in the `print` of the returned value.
The cause is that `pkg/analyzer/ownership` **does not analyze trait-method bodies at all**,
so nothing records that a returned `own` parameter was transferred rather than dropped: the
backend dutifully drops it at scope exit and the caller uses the corpse. `ref`/`mut` are
immune by construction — a borrow is retained and released by nobody, so the pass needs to
know nothing about the method. Lifting the restriction means teaching ownership about method
bodies, which is its own slice; the diagnostic says so and names the modes that do work.

One class of bug worth remembering from building it: **rebuilding a `types.ParameterType`
field-by-field silently drops new fields.** Three sites did (`substituteSelf`, the
lambda→signature conversion, and `lambdaSignature`), and the symptom was a `mut` receiver
that parsed, type-checked, lowered, ran — and wrote to a copy. It was found by an exec test
asserting the caller's value changed, not by anything that could have been caught earlier.

### 07/31/26
**Trait-impl methods are effect-polymorphic, and a trait signature can bound a callback.**
The last conservative corner of the effect work: `methodEffects` charged a call through a
method's own parameter the full `AllEffects` taint, so a trait method taking a callback was
as poisoned as every function was before effect polymorphism landed, and the taint spread to
its callers. It now returns a base effect plus callback parameters exactly as `lambdaEffects`
does, and `methodCallEffect` charges each call site for the arguments it actually supplies.

A method's parameter *types* live only in the trait declaration — an impl binds patterns
(`show = (self) => …`), not typed parameters — so `collectMethodSignatures` maps each impl
method to its declared signature. That is also what makes a bound written in a trait
signature enforceable: `apply: (Self, pure () -> i64) -> i64` now binds every caller,
including impure ones, via `signatureBound`.

**The receiver offset is the trap in this path, and it is a silent one.** A trait signature
counts `Self` as parameter 0, but `x.foo(a)` puts the receiver *outside* `call.Arguments` —
so signature index i is `Arguments[i-1]` (`methodArgumentAt`). Reading `Arguments[i]` would
check every callback against the argument one place to its right and report nothing, because
two function-typed arguments type-check against each other's parameters perfectly well. The
regression test uses two callbacks in different positions, since a single-callback test
passes either way. This is the same hazard already written into the UFCS decision entry,
which is where it will surface next.

**What was *not* done, and why not partially:** borrow modifiers on trait signatures. The
grammar was never the gap — `trait_method_signature` is an aliased `lambda_type` and its
`parameter_type` has always taken `ref`/`mut`/`own`, so `(mut Self, own i64) -> void` parses
today. The collector drops it, and three passes would have to learn about method parameter
modes: the typechecker performs no `checkMutArgument` for method calls, the ownership pass
contains no reference to trait methods at all, and only the backend is close to ready.
Collecting the modifier without teaching ownership would have the backend pass a *pointer*
where the ownership pass still believes a copy was made — the borrowed-`string`
use-after-free shape from 07/30 with a different origin. It wants one vertical slice with
ASan over it; todo.md carries the corrected scope.

### 07/31/26
**The parser shrank 9×, and `src/parser.c` left Git LFS.** 62,663 states → 6,475; 116 MB →
12.8 MB. It started as a storage question — every grammar change cost 116 MB of LFS quota,
and `.git/lfs` was 2.5 GB across 17 revisions against a 1 GB allowance — but the storage was
the symptom.

`tree-sitter generate --report-states-for-rule -` attributed **57,026 of the 62,663 states to
`lambda_expr` alone (91%)**. The cause was seven independent `optional()` modifiers in
sequence: an LR automaton tracks every distinct prefix through such a chain (2^7 = 128 before
the parameter list), and because the GLR conflicts around `(` keep the lambda-parameter-list,
tuple and parenthesized-expression readings alive simultaneously, each prefix grew its own
family of states across the whole expression grammar. `LARGE_STATE_COUNT` agreed: 35% of
states, where a few percent is normal.

Worth recording what *didn't* work, since it is the obvious first move: ablating each of the
17 declared GLR conflicts one at a time. Every one failed to generate — they are each
load-bearing for a specific ambiguity, exactly as their comments claim. The conflicts are not
the problem; what the conflicts *multiply* is.

Three forms were measured before choosing:

| Form | States | `parser.c` |
|---|---|---|
| Seven ordered `optional()`s | 62,663 | 116 MB |
| Ordered, mutually-exclusive ones grouped (5 optionals) | 37,687 | 70 MB |
| `repeat(choice(…))` — order-free | 6,475 | 12.8 MB |

The 10× needs the third, and it costs modifier **order and repetition as parse errors** —
one corpus test out of 373. Those moved to the collector (`lyra-E029`,
`expressions/modifier_order.go`), which is a better home than a trade-off: a syntax error
could only point at whichever token failed to shift, where the collector names the offending
modifier and the canonical order. The semantic sibling — `pure` and `det` conflicting — was
already a checker diagnostic, so the rules now sit together. Field labels survive a
`repeat(choice(field(…)))`, so no collector read changed.

Consequences beyond size, all of them the point: `git-lfs` is no longer a prerequisite for
cloning (`setup.sh`/`setup.ps1` lost the skip-with-install-hint path, `README.md` the
prerequisite row, CI its `lfs: true`), `parser.c` is diffable in review, and a grammar change
costs a large text diff instead of an LFS revision. **A commit from before this still needs
git-lfs** — `asan.sh` keeps its pointer-file guard for exactly that, and `lyra-zed-ext`'s pin
reintroduces the requirement if it names an older commit.

### 07/31/26
**Effect polymorphism — the declared half: `f: pure () -> t`.** The inferred half (below)
decides a higher-order function's purity per call site from the argument, which is what makes
a standard library usable but leaves a signature unable to *promise* anything: `pure` on a
combinator claims only that its own body is clean. A parameter's **type** may now carry the
same `pure`/`det`/`noalloc` modifiers a lambda value does, and a parameter carrying one is no
longer polymorphic — what calling it can do is known from the signature, so the function is
pure **for every caller**.

- **Grammar** (`tree-sitter-lyra`): `lambda_type` takes the three modifiers as labelled
  fields, matching `lambda_expr` so a type and the value inhabiting it are written the same
  way. No new node kind — `pure_modifier` already existed — so no highlight query changed and
  `lyra-zed-ext` needed nothing.
- **Enforced at every call site**, not only inside `pure` functions
  (`checkDeclaredCallbackBounds`): the bound is a property of the callee's signature, so an
  impure program may not quietly hand an impure callback to a `pure`-declared slot.
- **The argument's *inferred* effect is what is compared, not its annotation.** Requiring the
  word `pure` on every lambda literal a program writes would cost more than the bound is
  worth, and inference is exactly what this pass has and the typechecker does not. That is
  also why `isAssignable` deliberately passes two function types differing only in bounds:
  a shape mismatch there would report "cannot assign `() -> i64` to `pure () -> i64`", which
  explains nothing, where the checker says "this argument mutates state outside itself".
  `TypesEqual` *does* distinguish them, so identity questions still see two types — and it
  has to, or `isAssignable`'s equality short-circuit would fire first and the annotation
  would be decorative.
- **Bounds compose one way.** A constrained parameter forwarded into a constrained slot is
  verified from its own declared type (a parameter has no body to inspect); an unconstrained
  one is *rejected*, since it promises nothing. A bound the compiler cannot check is not a
  bound. Propagating the requirement outward instead — inferring that a wrapper's parameter
  becomes bounded — is the obvious next step and is open in todo.md.

**The standard library deliberately does not use it.** A `pure` bound on `unwrap_or_else`
would forbid a fallback that logs, which is a legitimate thing to want from a lazy default,
and the inferred half already keeps pure callers pure without taking that choice away from
impure ones. The declared half is for APIs that genuinely require purity — something that
memoizes, reorders, or parallelizes a callback — where the restriction is the feature.

**Effect polymorphism over function-typed parameters — the inferred half.** A higher-order
function's effects are not a property of the function alone: what `unwrap_or_else(m, f)`
does depends entirely on `f`. The pass charged the *definition* for a call it could not
see — an unresolvable callee tainted `AllEffects` — so every combinator was maximally
impure and the taint spread to its callers. **No callback-taking function was callable from
`pure` code at all**, which is the entire std.maybe/std.result combinator layer, and
dropping the annotation did not help: it moved the error to every caller.

A function's stored effect is now its **base** (what its own body does) plus its **callback
parameters**, the function-typed ones it calls. A call site pays base ∪ the effects of the
arguments actually supplied for them, so one definition gives `unwrap_or_else(m, () -> i64
=> 0)` pure and an effectful callback impure. The callback set is part of the same fixpoint
as the effects, because finding a callback changes a caller's effect and finding an effect
can reveal a callback a round later.

Two consequences that are the point, not side effects:

- **An annotation constrains a function's own body.** `pure` on a higher-order function
  claims "contributes no effects of its own", not "no effect can occur through me" — the
  second is not the function's to promise while it cannot constrain its callback, which
  needs the declared half (`f: pure () -> t`) the grammar cannot spell. That is what finally
  let the prelude annotate `unwrap_or_else` and `ok_or_else` `pure noalloc`, with all of
  `std.maybe` alongside them; a caller passing an impure callback is still rejected, at the
  call site, and the diagnostic names the **argument** rather than the innocent callee.
- **A callback passed onward stays polymorphic.** `(f) => or_else(m, f)` is polymorphic in
  `f` too. Without that, a combinator built from another combinator — which is most of a
  standard library — would be exactly as poisoned as before.

One thing had to be fixed for any of it to be observable: **a namespace-qualified callee had
no resolution at all**, so *every* cross-module call from a `pure` function was reported
impure, and `maybe.map(…)` — the whole point of the namespaced-module split — could not be
called from pure code however pure it was. `resolveCallee` resolves the last segment against
the merged program's top-level functions, and only when the object segment names no binding,
mirroring the backend's `namespaceCallee`: with a local `math` in scope, `math.double` is a
field read, and resolving it elsewhere would attribute another body's effects to it.

Deliberately still conservative, all sound and all noted in todo.md: a callback reached
through a struct field, a call result or an array element; multi-clause lambdas, whose
per-clause patterns give no index to match an argument against; and trait-impl methods,
where `methodEffects` is unchanged.

**`never` and `panic(msg)`.** A program had no way to reach the trap machinery on purpose:
the four traps (overflow, divide by zero, bounds, match fallthrough) are all emitted on
conditions the compiler checks, and nothing in the builtin registry exposed one. So
`expect`-shaped functions — anything that has to produce a `t` from a case that has none —
could not be written in Lyra at all, in the standard library or out of it.

- **`types.NeverType`**, the bottom type, assignable to **everything** (`isAssignable`) and
  equal only to itself (`TypesEqual` — subtyping belongs in assignability, and making it
  *equal* to everything would make two unrelated types equal through it). That one rule is
  what puts a diverging expression in value position: `None => panic("…")` as a match arm
  satisfies the arm's `t` without inventing a value, and `branchCommonType` folds it away
  from either side because it already goes through `isAssignable`. No syntax spells it, so a
  user cannot annotate with it.
- **`panic(msg: string) -> never`**, resolved like `print`/`println` — by name, only after
  scope resolution misses, so a user binding of the same name shadows it and adding this
  cannot break a program that already had its own. The message is a runtime `string`, not a
  literal: an interpolated one ("negative index ${i}") is the case that makes a panic
  message worth writing, so the runtime `void @lyra_panic(i8*, i64)` takes the fat pointer
  rather than baking the text in like the other four.
- **EffectNone** — callable from `pure`, `det` and `noalloc`. Tagging it Output would have
  made `pure` mean "cannot panic *on purpose*" while `a + b` and `xs[i]` panic implicitly
  from inside the same function, to the same fd, with the same exit code. Purity is about
  what a function returns and mutates, not whether it terminates. Reasoning and the Koka
  counterpoint are in `checker/README.md`.

Two things fell out that were not the point. The backend needed **no control-flow work at
all** — `lowerPanicCall` seals its block with `unreachable`, and every phi incoming and
fall-through `br` was already guarded on `Term == nil` for `return`/`break`/`continue`. What
it *did* need was a guard at each site that **consumes** an operand's value, because those
dereference what they get back: `let x = panic(…)`, a reassignment, a call argument, a
numeric conversion, and an array element each crashed `lyrac` with a nil dereference — a Go
panic rather than the loud error the backend is supposed to produce. `diverged(v, block)`
(nil value *and* sealed block, since several lowerings produce no value while still
reaching the next statement) is the shared test.

And the purity checker turned out to consult **`builtinEffects` before scope**, the opposite
of the typechecker's order, so a user's own `print`/`panic` was classified by the builtin's
entry instead of by its body. Harmless-but-noisy for `print` (a pure one reported impure);
unsound the moment `panic` joined the table as EffectNone, since a user `panic` that mutates
would have been waved through as pure. Fixed at all three call sites. The name is not the
callee.

### 07/30/26
**A generic function whose type variable appears only inside a composite is now recognized
as generic.** `mentionsTypeVar` (`backend/llvm/functions.go`) — the predicate behind
`isGenericLambda` — recursed through arrays, tuples and `weak` but had **no case for
`ParameterizedType`**, so `is_some<t> = (m: Maybe<t>) -> bool` answered "not generic".
`forEachUserFunction` then stopped skipping it, the backend tried to emit it under its bare
name, and lowering died on `cannot lay out data type "Maybe" yet` — a message naming the
*type* for a bug about the *function*.

Cost: **no program could build**, including programs that never mention `Maybe`, because the
prelude is implicitly imported. It went unnoticed for exactly as long as every generic
function happened to take a bare `t` parameter — `unwrap_or(m: Maybe<t>, fallback: t)` hits
the `GenericType` case through `fallback` and lowers fine, which is why the prelude worked
right up until it gained a predicate. The bisect is worth keeping: of the prelude's ten
functions, the three with a bare-`t` parameter built and the other seven did not.

Cases for `LambdaType`, `RawPointerType` and `ConstrainedType` went in on the same
reasoning rather than from an observed failure — a boxed closure is a pointer, so `() -> t`
happens to need no layout under the dev-tier lowering, and that is not a property to depend
on once Lambda Set Specialization lands. Every composite that can hold a type needs a case
here; a miss is not a missing feature but a wrong answer, and the symptom appears far from
the cause.

**A generic function is now callable through a module namespace.** `opt.wrap(7)` was rejected
with "cannot assign integer literal to t" while `import util.opt.{ wrap }` and the same call
inside its own module both checked — so the namespace form, which is the one the `std.maybe` /
`std.result` split is built on, was the only broken way to call a generic. Found while
exercising `maybe.map`.

Two independent halves, each verified load-bearing by reverting it alone. The **front end**
checked the call against the *declared signature*: `moduleMemberType` handed back a
`*types.LambdaType` whose type variables are still free, and nothing downstream could solve
them, because the solver (`inferGenericCall`) works from the declaration. It now returns the
`*ast.LambdaExpr` too, and `inferMemberCall` calls the same `inferLambdaCall` a direct call
does — which is what the comment there already claimed happened. The **backend** then failed
one step later: a generic function has no emitted body (a type variable has no
representation), so `namespaceCallee`'s `l.funcs` lookup found nothing, the call fell out of
the namespace path entirely, and it died as `unsupported method call "unwrap"` *after* type-
checking cleanly. It now asks `specializedFuncFor(call)` first, exactly as the by-name path
does — the instantiation is keyed by call node, so the specialization the typechecker solved
is already the right answer.

Worth naming: the two failures are the same omission at two layers, and the second was
invisible until the first was fixed. That is also why the test is an exec test —
`pkg/backend/llvm` had **no multi-module harness at all** (`driver.Analyze` resolves no import
graph), so nothing in that package could have caught a cross-module call regardless;
`buildAndRunModules` is that gap closed.

**Modules landed.** Design was already settled by the grammar (`module a.b`, `import a.b` / `as`
/ `.{ X, Y as Z }`, `pub` — with `IsPublic` already collected); what was missing is resolution
semantics and implementation. Decided: the prelude is a **normal module, implicitly imported**
(so it is written once, with `pub`, against the real mechanism); a plain `import a.b` binds a
**namespace** under the last segment (`.{ }` brings names in unqualified, `as` renames); module
paths map to files **by directory convention**. The implicit prelude import is the one exception
to the namespace rule — it brings names in unqualified, since `prelude.Maybe` defeats the
purpose, and ambient-ness is a concept a prelude needs under any design.

- **Resolution + merge** (`pkg/modules`, `driver.AnalyzeUnits`) — transitive imports,
  dependency-first order, shared dependencies emitted once, cycles (`lyra-E027`) and
  unresolvable imports (`lyra-E026`) reported. Diagnostics carry their file everywhere via
  `ast.Location.File`. Multi-file programs compile and run.
- **Per-module scoping / namespacing.** Every read of the symbol table's
  `Types`/`Functions`/`Traits` maps goes through **`LookupType`/`LookupTrait`/`LookupFunction`**
  rather than indexing them — the 37 sites across 7 packages became one choke point.
  Cross-module **collisions are rejected** (a duplicate type already errored; `RegisterFunction`
  overwrote silently, which with modules meant a program built against whichever module was
  collected last — the collector also dropped the error), with messages naming the *other* file.
  **`import util.math` now binds a namespace**, used as `math.double(…)`, in both the
  typechecker (`typechecker_modules.go`) and the backend (`namespaceCallee`). The asking module
  is recovered from the node's own `Location.File`, so no pass needed a module context threaded
  through it — the second time stamping the file onto every location paid off. Membership is
  *checked*, not assumed: names are program-wide unique today, so a bare lookup would resolve
  `math.secret` to another module's `secret`, binding a reference the source never made. A local
  value shadows a namespace, so `math.double` stays a field read when `math` is a binding; the
  namespace check runs before the object is inferred, since a namespace is not a value and
  inferring it would report an undefined identifier. Fixing this also stopped the unused-import
  lint misfiring on a used import.

- **`pub` enforcement** (`lyra-E028`). Within a module everything is visible; `pub` crosses the
  boundary. One check (`visibilityOf`/`checkVisible`) wired at the three places a cross-module
  reference resolves — a namespace member, `resolveType`, and a **bare call**. The bare-call
  case is the one that matters: top-level names still share one namespace, so `helper()` reaches
  another module's function without naming it, and a namespace-only check would leave private
  functions private in name only. Needed two fixes first: `VarDeclStmt` had no `IsPublic` field
  at all (so every binding was implicitly public), and `visibility` is an anonymous grammar
  child rather than a labelled field, so reading it by field name returned nil silently.
  Single-file programs are unaffected — file and names alike have no module, so every reference
  is same-module.
- **Implicit prelude** (`std.prelude`). An ordinary module — `pub` exports, same roots — that
  the compiler imports for you, so it stays readable, testable and replaceable rather than baked
  into the compiler. Names are available **unqualified** (the one exception to the namespace
  rule; `prelude.Maybe` defeats the purpose). A missing prelude is not an error, the prelude
  does not import itself, `LYRA_NO_PRELUDE` opts out, and a user declaration taking a prelude
  name **warns** (`lyra-W012`) with the local one winning — erroring would make every exported
  name permanently unusable and adding one later would break existing programs. Two ordering
  traps found while building it: the prelude module must be named before any file is walked
  (types register during the walk), and prelude names need their own set because `ModuleOf` is
  last-writer-wins, so a user module declaring the same name erases the record before functions
  are registered in `Finish`.
  - **The shadow is confined to its own module.** It used to replace the prelude's declaration
    *program-wide*: a second module that never mentioned the name got the first module's, or —
    when the shadowing declaration was **private** — got nothing at all, since withdrawing the
    prelude's registration deleted it for everyone. A module's *own* private helper made a
    prelude name undefined in every other module. The fix is where the prelude's exports live: a
    **`PreludeScope`** between every module scope and the global one (`ast/symbols`), so the
    prelude stays reachable everywhere, a module's own declaration beats it *there*, and nothing
    about that reaches another module's chain. Below the global scope would not do — that is
    where every module's exports live and every module falls through to it, so the first
    declaration of `Maybe` would again win for the whole program. `FunctionKey` gained the
    matching key (a prelude-shadowing declaration is module-qualified whatever its visibility,
    so the prelude keeps the bare key and every non-shadowing module still finds it), which the
    backend and the ownership pass already ask for from the *referencing* location. Two things
    had to follow: the **entry module needs a scope of its own** — sharing the global scope was
    the reason an entry-file `let unwrapOr` rebound the prelude's program-wide — so anything
    walking scopes for one file starts from `EntryScope` (the LSP's completion, definition,
    references, rename and highlight walks all did start from global, which is exactly why
    sharing looked simpler); and `ownership`/`use_after_move` now resolve a callee with
    `LookupFunctionFrom`, since a bare `Functions` lookup hands back another module's function
    and those passes read the callee's parameter modes to decide where a reference is retained.
    Tests: `TestPrelude_ShadowIsConfinedToItsModule` (entry file / private / exported, each
    asserting both directions by giving the shadow a different signature, so either resolution
    going the wrong way is a type error rather than a different answer).
    - **Still program-wide: a shadowed type or trait.** Their namespace is program-wide by
      construction — `SymbolTable.Types` is keyed by bare name, and so is the backend's registry
      of emitted LLVM struct types, which resolves a type reference carrying no location to say
      who is asking. A program therefore has exactly one `Maybe`, and the shadowing declaration
      is it. Confining a type shadow means per-module type *identity* end to end (mangled type
      symbols plus a location-aware `LookupType`), which is the same work two modules declaring
      unrelated same-named types needs.
- **Cross-module symbol mangling.** A user function is emitted as `lyra.<module>.<name>`, a
  specialization as `lyra.identity$i64`; `main` keeps the C entry-point name. It turned out to
  fix a **present bug** rather than only prepare for privacy: emitted functions took their
  source name verbatim, so a program with a function named `malloc`, `write`, `memcmp` or
  `lyra_rc_alloc` produced a module clang rejected outright against libc or the emitted runtime.
  The dot after `lyra` means a user symbol can never spell one of the runtime's `lyra_` names.
  - **Open when this landed, closed the same day:** two modules could not each declare a private
    `helper`, because the *front end* rejected duplicate top-level names program-wide. Mangling
    removed the backend's objection; per-module name resolution (below) removed the remaining
    one.
- **Per-module name resolution.** Two modules may now each declare a **private** name of the
  same spelling, and each reaches its own. Four parts: each file is walked inside its module's
  scope (so every nested scope parents under it — the keystone; without it a function body's
  chain ran past its own module); a declaration always lands in its module's scope and only a
  `pub` one *also* lands globally; `SymbolTable.Functions` keys a private function by module;
  and the backend's `l.funcs` uses the same key, asked for from the *referencing* location so
  the two cannot disagree. The entry file's module scope **is** the global scope — a program
  root has nothing to be private from, and giving it a child pushed every single-file program's
  declarations out of sight of the LSP's scope walks. One check had to go: `inferIdentifierCall`
  verified visibility *after* a successful lookup, asking whether *some* declaration of that
  name was private (via a last-writer-wins map) rather than whether the resolved one was visible
  — so one module's call to its own function reported another module's privacy. Scoping now
  enforces privacy structurally; the diagnostic only improves the not-found message.

- **Trait-method lowering.** An impl method lowers to a function taking the receiver first; a
  method call is a direct call. Static dispatch, no vtables. Emitted **lazily at the first
  call**, which is what makes a **generic impl** work with no extra machinery — dispatch has
  already substituted `Self` with the concrete receiver, and `typetable.Resolution` now hands
  the backend the impl and that signature so Self substitution is never re-derived. The
  synthesized function is a real `*ast.LambdaExpr` lowered through the shared
  `defineFunctionInto`. Symbols name type + trait + method (neither pair is unique). Bodies are
  queued rather than lowered re-entrantly, so a method calling another — or itself — works.
  Covers data and struct receivers, arguments, managed receivers and returns, and two traits on
  one type. **Open:** trait signatures carry no borrow modifier, so every parameter including
  the receiver is by value.

- **A generic constructor takes its unsolved type parameters from the context.** `Some(v)` fixes
  `t` and always lowered; `None` fixes nothing and `Ok(v)` fixes `t` but not `e`, so both stayed
  the bare declaration — deliberately, since inventing an instantiation from a partial
  substitution would claim precision the construction did not supply. The cost was that they
  lowered *only* under an annotated `let`, the one site that stamped its type onto the value:
  `-> Maybe<i64> => None` failed the build with `unknown named type "Maybe"`, and the prelude's
  `Result` was unusable outright, since neither constructor determines both parameters. Fixed by
  `propagateInstantiation` (`typechecker/propagate_instantiation.go`), the generic-type analogue
  of `propagateLiteralType`/`propagateAllocation`, wired at the same choke points — annotated
  `let`, the three return-body sites, the call-argument site, and the *generic* call's argument
  site, which is the one that makes `unwrap_or(None, 42)` work (the parameter is only
  `Maybe<i64>` once another argument has solved `t`). **It checks rather than assumes**, and
  that turned out to matter more than the lowering: a partly solved construction's payload was
  not verified against the context at all, so `let r: Result<i64, string> = Ok("x")` passed the
  front end and was caught only by the backend refusing to store a string into an i64 payload —
  a type error found by the code generator is one found in the wrong place, and it survived only
  because the value could not lower, so making these lower is exactly what would have turned it
  silent. The payload is now re-checked under the context's substitution, and the node is left
  bare on a mismatch so a wrong payload can never lower as that instantiation. One ordering fix
  went with it: solving promotes an untyped literal to its default in order to unify, so
  `Ok(42)` fixed the payload at i64 and then rejected the `Result<u8, string>` it was returned
  as; an untyped leaf is now left untyped when the payload alone did not pin down every
  parameter, for the context to narrow. **Still open:** the same gap for a generic struct or
  named tuple with a parameter no field can pin down. Tests:
  `TestExec_ContextSuppliesGenericInstantiation` (seven compile-and-run cases) and
  `TestGenericContext_*`, both mutation-verified.

- **Context-directed instantiation extended to generic structs and named tuples, and struct
  field inference made structural.** The data-constructor half (above) left aggregates open.
  They fail differently, which is why they needed their own pass: a bare `DataType` is
  assignable to any instantiation of itself, so a partly solved data construction reached the
  backend and died there, while a bare `NamedStructType`/`TupleType` is *not*, so a partly
  solved one was rejected by the front end with "return type mismatch: expected Tagged<i64,
  boolean>, got Tagged" — a spurious error on correct code. That meant propagating **before**
  the assignability check rather than after it, so every context site now goes through
  `contextualType`, which propagates, re-reads the record, and reports whether it already
  emitted a diagnostic (without that flag one mistake produced two errors: the precise
  "Tagged.value: cannot assign string to i64" plus a coarse return mismatch). Two independent
  causes were behind the aggregate failures. A **phantom** parameter appears in no field at all,
  so only the context can supply it — that is what the propagation handles. A parameter
  appearing only *inside* another type (`inner: Opt<t>`, `items: [2]t`, `pair: (t, t)`) was
  unsolvable for a different reason: field inference matched only a field declared as a *bare*
  parameter, so `struct Wrapper<t> { inner: Opt<t> }` could not be solved from its own fields.
  It now unifies structurally with `unifyGenericTarget` — the same unifier data constructors and
  generic calls use — and the field types are substituted structurally too, rather than by
  looking the field's type *name* up in the solution. The latter caught a silently-accepted
  error: `Holder { tag: 1, inner: Just("x") }` compared against the raw `Opt<t>`, which the
  "still generic, check leniently" guard swallowed, so a wrong value went unreported while the
  surrounding instantiation looked complete. That guard is now `mentionsGenericParam`, which
  walks the type instead of testing its name. **Still open:** nothing known — the named-tuple
  element check now also defers to the context when the elements alone leave a parameter
  unsolved, so `Pair<t, u>(t, t)` built as `Pair(40, "x")` no longer blames the second element
  for a binding the first one guessed. Tests: `TestExec_ContextSuppliesAggregateInstantiation`
  (eight compile-and-run cases) and `TestGenericContext_*`, each mutation-verified against
  reverting the propagation, the structural solve, the structural substitution, and the
  duplicate suppression independently.

- **A generic function solves its type variables through a *function-typed* argument.**
  `unifyGenericTarget` had no `LambdaType` case, so a declared `() -> t` matched against a
  supplied `() -> i64` bound nothing and the call reported "cannot infer type variable t from
  these arguments"; `substituteGenerics` had the same omission, so even once `t` was solved the
  parameter stayed `() -> t` and the argument was rejected as "cannot assign () -> i64 to () ->
  t". Both halves are required, and between them they are what makes any callback-taking
  combinator expressible at all — `unwrap_or_else`, `map`, `and_then`. Parameters unify in the
  same direction as the return type: a function type is contravariant in its parameters, but
  this is unification against a *pattern* rather than a subtyping test, so direction only
  decides which side a variable may be read from and either is correct. `collectGenericNames`
  gained the matching case, so a variable appearing *only* inside a function type is still
  recognised as one in play, and the substitution returns a **copy** — `LambdaType` is the one
  type here held by pointer, so rewriting in place would mutate the declaration every other call
  site shares. Found because the prelude gained `unwrap_or_else`, which type-checked standalone
  and then could not be called: nothing had exercised a higher-order generic. Tests:
  `TestExec_GenericSolvedFromFunctionArgument`,
  `TestGenericContext_FunctionArgumentUnificationStillRejects` (inconsistent bindings, wrong
  arity, and the solved return type enforced at the use site), and
  `TestShippedPrelude_CombinatorsAreCallable`; both halves mutation-verified independently.

**Bugs fixed.**

- **`heap-use-after-free` when a *borrowed* `string` parameter is reassigned.** `let f = (s:
  string) -> string => { s = "l" ++ "1"  s }` freed the caller's argument, which the caller then
  released again. A by-value `string`/`ref` parameter is a **borrow** — the callee's copy shares
  the caller's reference — but `lowerVarReassignment` released the overwritten value whenever
  the type needed a drop, without asking whether the binding owned it (its own comment claimed
  it checked "the same condition lowerVarDecl framed the binding"; nothing did). Now guarded by
  `slotIsOwning`, shared with the interior-lvalue path's `releaseOldTarget` so the two cannot
  disagree — framed slot (a local, or an `own` param) or a by-reference `mut` param (whose slot
  *is* the caller's owning storage, so writing through it still releases, correctly). Surfaced
  the moment ASan started instrumenting; note the original diagnosis blamed `mut`, which was
  wrong — `mut` was the case that already worked. Tests:
  `TestExec_BorrowedParamReassignment_NoUseAfterFree`.
  - **Residual leak: gone (07/30).** Reassigning a borrowed parameter is now `lyra-E025`, so the
    program that leaked (`(s: string) => { s = "a" ++ "b"  0 }`) no longer compiles — the
    language rule removed the codegen problem rather than the backend having to frame the slot.
    The `slotIsOwning` guard stays as defense in depth, on the backend's standing rule that it
    errors or does nothing rather than emitting a wrong release.

- **An anonymous tuple literal was built at the untyped default, not the context's width.** `let
  f = (t: (u8, u8)) -> u8 => …` called as `f((10, 40))` emitted `call i8 @f({ i64, i64 } …)`
  against a `{ i8, i8 }` parameter — invalid IR. `propagateLiteralType` narrowed the tuple's
  *leaves* but never re-recorded the tuple **node**, which is what the backend builds the
  aggregate from; the array case beside it had always re-recorded, with a comment explaining
  why. Fixed by re-recording an anonymous tuple literal at the context's element widths (named
  tuples are excluded — nominal, and already recorded against their declaration, possibly as a
  generic instantiation). Covers the argument, return, struct-field, and data-payload positions.
  Invisible to Apple clang 21 (opaque pointers make the two function types indistinguishable,
  and arm64 passes small structs in registers so the value was right anyway); found by
  `./asan.sh`. Tests: `TestExec_AnonymousTupleTakesContextWidth` plus an IR assertion, since on
  macOS only the emitted text shows it.

- **`i128` overflow-checked multiply did not link on Linux.** `llvm.smul.with.overflow.i128` is
  not lowered inline — LLVM expands it into a call to compiler-rt's `__muloti4`, and clang links
  compiler-rt by default on macOS but **libgcc on Linux**, which has no such symbol. So `lyrac
  build` of a signed i128 multiply failed at link time there while the identical IR was fine on
  macOS. Fixed by emitting the helper into the module (`lyra_i128_mul_overflow`, `trap.go`,
  routed at `overflowIntrinsic`'s single choke point so the call site is unchanged) rather than
  by adding `--rtlib=compiler-rt` to the link line: the same reason the ref-counted runtime and
  the 128-bit formatter are emitted as real bodies — one `clang out.ll` stays self-contained
  everywhere. Defining `__muloti4` under its own name would also have worked but squats on
  another runtime's ABI. Unsigned multiply is unaffected (LLVM expands it inline), as are
  division and addition (`__divti3` is in libgcc too). Tests: `TestExec_I128MulOverflowHelper`
  drives every branch directly, including `-1 × INT128_MIN` — which has no Lyra spelling
  (128-bit literals aren't representable yet) and is the case that must *not* be checked via
  division, since `sdiv INT128_MIN, -1` is itself undefined.

### 07/29/26
- **Generic types** (`Box<t>`, `Maybe<t>`, recursive `List<t>`, generic named tuples) — the
  keystone the prelude, `checked_*`, `?` error conversion, and a stdlib `BigInt` were all queued
  behind, and they **compose** with generic functions (`(x: t) -> Box<t>`). Typechecker: a
  construction evaluates to the *instantiation* it denotes (a `ParameterizedType` carrying the
  solved arguments) instead of the bare declaration — the substitution was already being solved
  to check the fields and then discarded, which is why a field read returned the type variable
  and an annotated binding reported "cannot assign Box to Box"; a data constructor and a named
  tuple solve theirs positionally with the same unifier a generic call uses. Backend
  (`generic_types.go`): one LLVM type per instantiation (`%Box$i64`), materialized **lazily** by
  `lowerType` rather than from a collected table (no single site "uses" a type, and every site
  that can name one already funnels through `lowerType`), with the declare-then-define split
  making a recursive `shared List<t>` terminate, and `resolveInstantiation` normalizing an
  instantiation at the same choke point that strips newtypes so no aggregate-reading site needed
  a new case. A **managed** type argument was a **double free**: `OwnsManaged` had no
  `ParameterizedType` case, so the pass minted no retain for a copy while the backend released
  both bindings — macOS ASan missed it, and the regression test compares retain/drop-glue counts
  against the equivalent concrete declaration.
- **Generic functions work end to end — instantiation plus monomorphization.** The biggest item
  on the board, and further back than the todo implied: a generic function did not even
  *type-check*. `identity(7)` reported "cannot assign integer literal to t", because nothing
  solved `t` — the existing generic machinery (`substituteGenerics`, `unifyGenericTarget`,
  bounded-impl dispatch) all served trait dispatch, and there was no path that solved a type
  variable from a value. Chosen slice: generic **functions**, end to end, over generic *types*
  first — the narrowest complete vertical (one calling form, one specializer), and it builds the
  monomorphizer that generic types and release-tier closures (LSS) both need next.
  **Instantiation** (`typechecker/instantiate.go`): each declared parameter type is unified
  against its argument's inferred type to solve the variables, the call is checked against the
  *substituted* signature, and the result is the substituted return type. The unifier is the one
  trait dispatch already uses — sharing it keeps "what does this type variable match" a single
  definition rather than two that drift. An untyped literal argument settles to its default
  width before binding (`identity(7)` → `t = i64`): a type variable is a real type in the
  specialization, deciding an alloca's width and an instruction's signedness, so leaving it
  untyped would push an unresolved literal type into codegen — the same class of bug as an int
  literal in a float slot. A variable appearing only in the return type is unsolvable from
  arguments and is reported at the call, not discovered during lowering; arity is checked first,
  since a missing argument is just a variable with nothing to bind it.
  **Monomorphization** (`backend/llvm/monomorphize.go`): one function per distinct instantiation
  (`identity$i64`, `identity$boolean`), shared between call sites that solve alike, with the
  bare generic name never emitted and an uninstantiated generic costing nothing. **By
  substitution, not by cloning the AST** — the one shared body is lowered per binding set with a
  substitution installed on the lowerer, consulted by the two accessors every lowering decision
  already funnels through (`lowerType` for a source-written type, `recordedType` for a TypeTable
  one). That is enough to make the body concrete down to its locals and arithmetic. Cloning
  would mean deep-copying every node and re-typechecking each copy or hand-patching a parallel
  TypeTable: much more machinery, and two ways for a specialization to disagree with the body it
  came from. `defineFunctionInto` is shared with the ordinary function path, so parameter
  binding, `own`-param framing, and the void/typed return split cannot drift between a generic
  function and a plain one.
  **Two boundaries, both found by writing the tests, both deliberate.** An **unbounded** type
  variable supports only what every type supports: `x + x` on a `t` is rejected, and that is the
  sound answer — `t` could be `bool`. Arithmetic needs bounded polymorphism over an operator
  trait (`where t: Add`), which does not exist; the test pins the refusal rather than the wish.
  And a **managed** type argument is refused loudly: the ownership pass analyzes the generic
  body *once*, where a type variable is not managed, so it records no retain, release, or drop
  anywhere in it — at `t = string` those decisions are simply wrong. Measured before the refusal
  went in: an ASan abort, 2 allocations against 3 releases. Substituting types cannot repair it,
  because the ownership *decisions* were made generically. Running the pass per instantiation —
  teaching it to take a substitution and produce a table per specialization — is the fix and the
  natural next slice.
  Tests: `backend/llvm/llvm_generic_test.go` (8 exec cases — one and two instantiations, a
  variable inside an array type, two variables, one variable in several positions, a body-local
  of the variable's type, called from another function, instantiated at a narrow width — plus
  the emitted-specialization shape, that two identical call sites share one function, that an
  unused generic emits nothing, and the managed-type refusal),
  `typechecker/tests/generic_call_test.go` (8: solving from arguments, the substituted result
  flowing on, solving through a composite, two variables, inconsistent binding rejected, an
  unsolvable return-only variable, arity first, and the unbounded-arithmetic boundary).
  **Still deferred:** generic *types* (`Box<T>`/`Maybe<T>` construction, inference, and
  monomorphized layout — which is what a prelude, and with it `Maybe<weak T>`, `checked_*`, and
  `?` error conversion, are all still waiting on), multi-clause generic functions, and a generic
  function calling another at a variable-dependent instantiation. **The managed-type refusal is
  gone** — see the per-instantiation ownership entry.
- **The ownership pass runs per generic instantiation — a managed type argument works.** The
  limitation generics landed with, and the reason it was a *refusal* rather than a miscompile:
  every decision the ownership pass makes turns on whether a value is reference-counted, and
  that is a property of the type *argument*, not of the generic body. Analyzed once with `t`
  abstract, nothing looks managed, so `pick(a: t, b: t) -> t` recorded no retain on its result
  and no release for the caller's temporaries — correct at `t = i64`, a double free at `t =
  string` (measured: an ASan abort, 2 allocations against 3 releases).
  **The fix is one table per instantiation, not one table.** `ownership.AnalyzeLambda(lam,
  symTable, tt, subst)` analyzes a single body under an instantiation's bindings; the driver
  runs it per specialization into `Result.OwnershipBySpec`, keyed by the instantiation's
  `Key()`. They genuinely cannot be merged: the tables are keyed by AST node, and the *same*
  node carries different annotations in different instantiations — precisely the information one
  shared table could not hold. Two choke points made it small: the pass gained a single type
  lookup (`analyzer.typeOf`) that applies the substitution, so all four of its type reads became
  correct at once; and the backend reads the table through one accessor (`l.ownership()`), which
  returns the specialization's table inside a generic body and the program-wide one everywhere
  else. An annotated binding inside a generic body (`let copy: t = x`) needed the same
  substitution applied to its *declared* type.
  Verified by mutation: reverting the accessor to the program-wide table makes the managed case
  fail again (wrong exit code and an ASan crash), so the per-instantiation table is load-bearing
  rather than incidental. Tests: `llvm_generic_test.go` gained 4 exec+ASan cases — the shape
  that aborted, identity at a string, identity at a `[]string` (a managed *container* argument,
  where the element drops are the box's and must not be doubled), and a managed and a scalar
  instantiation side by side in one program, which is the case that most directly needs two
  tables — plus an assertion that the *scalar* specialization contains no refcount traffic at
  all, so the managed one's decisions cannot have leaked across.
- **`weak` gets runtime semantics — created, upgraded, and released (phase 2).** `weak T` was a
  type and nothing more: it collected, resolved, and broke E014 size cycles, but no expression
  produced one, so a struct with a weak field could be declared and never built. Three pieces
  landed, in the order the design forced.
  **The two-count box header.** A weak reference must be able to ask "is the referent alive?"
  *without* dereferencing freed memory, so a box's memory has to outlive its strong count: the
  header is now `{ i64 strong, i64 weak }`, the payload's drop glue runs at strong 0, and the
  memory is freed only once weak reaches 0 too. Uniform across every box kind — string, dynamic
  array, closure environment, `shared` value — which costs 8 bytes per heap value and buys one
  protocol with no per-kind branching. Two alternatives were rejected and recorded: giving only
  weakly-referenced types a wide header makes the *header size type-dependent*, the same silent
  split-by-representation this backend has been bitten by twice (by-value `mut` params, newtype
  managed-ness); packing two 32-bit counts into one word puts mask/shift arithmetic in the
  hottest runtime functions plus an unenforced overflow assumption.
  **The delicate part was the field indices, so it was done in two steps.** A GEP index is a
  bare integer — nothing type-checks that field 2 is the payload — so every box access first
  moved behind named constants and helpers (`boxPayloadPtr`, `dynArrayLenPtr`,
  `dynArrayElemPtr`, `pinnedBoxConstant`) as a pure refactor, verified green, and only then was
  the layout flipped in one edit. That ordering paid immediately: the one site the refactor
  missed — a `shared` struct's field *read* — surfaced as a panic ("cannot index into type
  *types.IntType using gep") rather than as silently wrong memory, which is exactly what the
  two-step was for.
  **The create and upgrade forms.** `x.weak()` is a builtin method on a `shared` receiver (no
  grammar change; `weak` is only a type in the grammar), and the upgrade is `if let s = w { … }
  else { … }` — decided today over a `Maybe`-returning `upgrade()`, which needs generics, and
  over an `alive()`/`get()` pair, which is a two-call footgun. The upgrade calls
  `lyra_rc_upgrade`: strong != 0 → increment and hand back the box as a real `shared T`, else
  null. So the then-branch holds a genuine owning reference (framed and released like any
  other), the value cannot die under it, and **there is no other way to read a weak reference at
  all** — a weak value has no fields and no dereference, which is what makes a dangling read
  unexpressible rather than merely discouraged. The pattern must be a plain name: a
  destructuring pattern would conflate "the referent is gone" with "it didn't match".
  **A weak reference has its own lifecycle.** It stopped being "never retained/released":
  `IsManaged` covers it, so a copy takes another weak count and a death gives one back — but
  through the weak shims, never the strong ones. Getting that wrong leaks the box's *memory*,
  which is invisible: the payload is already gone, nothing misbehaves, and macOS ASan cannot see
  leaks. The counts are the detector, and the test asserts weak retains == weak releases.
  **The payoff, verified:** a helper creates a `shared Node`, returns a weak reference to it,
  and the strong reference dies at the helper's exit — the upgrade in the caller then *fails*
  and takes the else branch, ASan-clean. Reading the strong count out of a
  dead-but-not-yet-freed box is safe precisely because the weak count kept the header alive.
  That is the property the whole header change exists for.
  **Found and fixed on the way:** a **collector panic** on `if let s = w` — a bare-name binding
  takes the *identifier* branch of `declaration`, which has a `name` field and no `pattern` one,
  and reading the absent pattern field indexed straight into a nil node. It panicked on any `if
  let <name> = …`, not just a weak one. It now synthesizes the equivalent identifier pattern, so
  every downstream pass sees one shape.
  Tests: `backend/llvm/llvm_weak_runtime_test.go` (5 exec cases, each also under ASan — upgrade
  succeeding, upgrade *failing* after the referent died, no-else, upgraded twice, passed through
  a function — plus the weak-count accounting and a check that the upgrade goes through the
  runtime rather than a dereference), `typechecker/tests/weak_test.go` (5: the downgrade, the
  upgrade binding a `shared`, and the three refusals — no field access, no stack receiver, no
  destructuring pattern). Several IR-shape tests were updated for the wider header, and
  `layout_test.go` now pins the field indices against the header shape, since nothing else
  type-checks that correspondence.
  **Still open, and the honest limit:** a `weak` **field** cannot be constructed. A field must
  be initialized at construction, there is no empty weak, and every candidate initializer is a
  self- or forward-reference (rejected by use-before-declaration) — so the parent-pointer shape
  that motivates `weak` in the first place still needs `Maybe<weak T>` (generics) or a nullable
  weak. **Cycle leaks are therefore not closed yet**: the mechanism is in place and works for
  local weak references, but the field case that would actually break a cycle is still
  unexpressible.
- **Destructuring statements lower — `if let` compiles at last (phase 1 of `weak`).** All three
  forms type-checked but none compiled: `let (a, b) = v`, `if let pat = v { … } else { … }`, and
  `let pat = v else { … }` each hit "block statement lowering not implemented" — the same
  front-end-enforces-what-the-backend-cannot-build gap `newtype` had. Landed first because the
  **`weak` upgrade is going to be spelled `if let strong = w { … }`** (decided today over a
  `Maybe`-returning `upgrade()`, which needs generics, and over an `alive()`/`get()` pair, which
  is a two-call footgun), so this had to exist anyway — and it is worth having on its own.
  **One mechanism, three failure paths.** All three drive the *same* pattern machinery `match`
  is built on: `patternMatcher` hands back the `aggPatternTest`/`aggPatternBind` pair for a
  single pattern, so a pattern means exactly the same thing in a match arm and in an `if let`.
  Two implementations of "does this value match this pattern" would drift, which is the whole
  reason for the indirection. Being statements, they need no merge phi, only a control-flow
  join. A plain `let` requires an **irrefutable** pattern — one that imposes a test is a loud
  error pointing at `let … else`, rather than binding on a path where the match may not hold; an
  `if let` binds in the then-block, which is what scopes the names to that branch; a `let …
  else` binds into the *continuation* block, sound precisely because its else branch must
  diverge (a fall-through would be a use of unbound names, so that too is a loud error). A
  `shared` scrutinee unboxes first, as in a match. Deferred with a loud error: destructuring an
  **array** with a pattern, whose length-test-plus-element-tests shape belongs to the array
  match driver rather than a single test/bind pair.
  **Two front-end fixes it needed.** Both `if let` branches (and a `let … else`'s) are now
  checked *for effect* — they are statements, so neither is in value position, and treating the
  last one as a value rejected an ordinary one-armed `if` at the end of a branch (the same fix
  loop bodies got earlier today). That in turn exposed a real hole: `checkExpressionStmt` had
  **no default arm**, so an expression kind it did not name went completely unchecked in
  statement position — a bare `a` naming nothing reported no error at all. It was invisible
  while every block's trailing value inference happened to check the final statement as a side
  effect; three existing if-let tests caught it the moment a branch stopped being inferred as a
  value. It now infers by default, which closes the hole everywhere rather than just here.
  Tests: `backend/llvm/llvm_destructuring_test.go` (11 exec cases — irrefutable tuple and
  struct, if-let matching and failing, no-else, a nested tuple payload, a `shared` scrutinee,
  let-else both ways, if-let in a loop body, the one-armed-`if` branch — plus a managed-payload
  case under ASan with exact alloc/retain/release accounting, and the refutable-plain-`let`
  refusal), `typechecker/tests/if_let_else_test.go` (+3).
- **A `let` inside a loop body is visible there — two loop bodies become pointers.** Open since
  07/13 and a papercut on every loop: `for var i = 0; i < 3; i += 1 { let doubled = i * 2  … }`
  reported `doubled` undefined, so *nothing* could be declared inside any loop body. It also
  blocked the closure work's most natural shape (a closure bound in a loop). **Cause:** the
  collector puts body-locals in a child block scope keyed on the body `*BlockExpr`, and both
  loop nodes stored that block **by value** — the copy has a different address, so `enterScope`
  missed. What made it a silent wrong answer rather than an error is that `enterScope`'s miss
  path just runs the body in the *enclosing* scope; the loop variable lives there, so loops
  looked like they worked. **Fix:** `ForLoopExpr.Body` and `ForInLoopExpr.Body` are
  `*BlockExpr`, exactly as `IfDestructuringStmt.Then/Else` already were and for the same
  recorded reason.
  **The change surfaced two more defects, one in each direction.** (a) `assignedNames(node any)`
  silently accepted the now-`**BlockExpr` argument and returned the empty set, so a loop havoc'd
  *nothing* — a stale interval survived the loop and the range analysis then elided a downstream
  safety check on it. Two existing range tests caught it immediately
  (`TestRange_Safety_HavocInNestedBlockNotElided` and its false-positive twin), which is exactly
  what they were written for. Its parameter is now `ast.AstNode`, so the same mistake is a build
  error rather than an empty map. (b) A loop body was being type-checked as though its last
  statement were the block's *value*, so a one-armed `if` at the end of a loop body — an
  ordinary conditional side effect — was rejected with "`if` used as a value must have an `else`
  branch". Loop bodies now go through `checkBlockForEffect`, which is the same walk minus that
  step; every statement is still checked and its types recorded, so nothing downstream loses
  information.
  Tests: `backend/llvm/llvm_loop_local_test.go` (8 exec cases across both loop forms — including
  a `var` body-local, a body-local reading an outer binding, a closure bound in a loop body, the
  one-armed `if`, and nested loops with their own locals — plus a managed body-local checked
  under ASan and the path-sensitive conservation check, proving each iteration's string is freed
  on that iteration), `typechecker/tests/for_loop_test.go` (+6, incl. that a body-local does
  *not* escape the body). The conservation corpus's "allocation in a loop" case had its
  allocation moved back into the loop body, where it belonged — it lived in a helper only
  because of this bug.
  **Found here, fixed next:** the backend's `l.locals` had no scope discipline, so a shadowing
  binding clobbered the outer one permanently — see the shadowing entry.
- **Backend locals are lexically scoped — shadowing no longer clobbers the outer binding.**
  Found while testing the loop-body fix and confirmed pre-existing (the same program misbehaves
  on the pre-change compiler): `l.locals` was a single flat name→slot map for an entire
  function, so `let n = 100; let inner = { let n = 5  n }; n + inner` returned 10 instead of 105. Every construct that binds a name for the duration of a sub-tree leaked that binding into
  everything after it — a block's `let`, a loop variable, a C-style loop's counter, a match
  arm's pattern. Silently, and with a wrong *value* rather than an error, since the typechecker
  resolves all of these correctly and only codegen disagreed.
  **Fix:** `pushLocalScope` snapshots the visible bindings and returns the restore (`defer
  l.pushLocalScope()()`), applied at a block (`lowerBlockStmts`, next to the managed frame it
  already pushes), each of the four loop lowerings, and each match-arm loop. **The match needs a
  reset *per arm*, not just a restore after the match** — the sharp case is an arm that reads an
  outer binding an earlier arm's pattern shadows (`let v = 100; match Right(5) { Left(v) => v,
  Right(x) => v + x }`): without the reset the second arm reads the first arm's slot, which on
  that path was never stored to, so the result is whatever was on the stack (measured: 6 instead
  of 105). The restore installs a fresh copy on each call so repeated resets can't leak writes
  back into the snapshot.
  Name scoping is deliberately independent of the ownership bookkeeping, which tracks *slots*:
  two same-named managed values in nested scopes are two allocations, each released exactly once
  (pinned by an ASan + path-sensitive-conservation case). Tests:
  `backend/llvm/llvm_shadowing_test.go` (10 exec cases — nested block, `if` branch, loop
  body-local, loop counter, for-in variable, `data` match arm, scalar catch-all, arm-to-arm
  non-leakage, the outer-read-after-an-earlier-arm case, two-level nesting — plus the managed
  pair). Each scope site was mutation-checked: removing the block scope fails 3 subtests,
  removing the loop scopes 6, removing the per-arm reset exactly the one case written for it.
- **Closures lower — a function is a value at last (the boxed dev tier).** The largest missing
  language capability: a lambda could be *written* but never used as a value, so `apply(double,
  3)` failed with "unknown type: (i64) -> i64" and a nested lambda with "expression lowering not
  implemented for *ast.LambdaExpr". This is the dev tier of the two decided on 07/17; Lambda Set
  Specialization stays gated on the generics monomorphizer, as decided.
  **Representation:** a function value is `{ i8* fn, i8* env }`. One shape for every function
  type — which is the point, since it lets a `(i64) -> i64` parameter accept a named function, a
  captureless lambda, and a capturing closure with no per-call-site specialization, and is what
  a stable hot-reload ABI requires. `fn` is the lifted body, always `ret (i8* env, params...)`;
  `env` points at the *payload* of a ref-counted box `{ i64 rc, { i8* dropFn, captures... } }`,
  exactly `rcHeaderSize` past its header — deliberately the same relationship a string's data
  pointer has to its box, so `managedBox` recovers it by subtracting the header and
  **retain/release/drop needed no new machinery**: `IsManaged` gained one case and closures
  became ordinary managed values. A **captureless** closure shares one *pinned* static
  environment, the same device string literals use, so a plain function value costs no
  allocation while still flowing through the ownership model unchanged.
  **Three design choices worth recording.** (1) A **named** function used as a value gets a
  thunk (`@name.closure`) rather than every function growing an environment parameter — a direct
  call by name keeps its plain signature, so every existing call site and its pinned IR are
  untouched, and the forwarding call exists only for functions actually used as values. (2)
  Nested lambdas are collected up front and their bodies lowered **last**, never re-entrantly at
  the creation site: lowering a body mid-expression would mean saving and restoring the whole
  per-function state (locals, loop stack, managed frames, pending temporaries), and one missed
  field there is a leak discovered three features later. (3) The environment's captures are
  freed through **one generic trampoline** that reads the per-capture-set glue out of the
  environment's first slot — a release site knows only the static type `(i64) -> i64` and never
  which lambda produced the value, so the glue has to travel *with* the value rather than be
  chosen at the release.
  **Captures are by value** (`pkg/analyzer/captures`): copied at creation, which is what lets a
  closure outlive the frame its captured bindings live in — `makeAdder(5)` returns a closure
  over `n` and calling it later is valid. Capturing the slot by reference would dangle the
  moment that frame returned, and there is no escape analysis to tell the safe case apart. The
  pass is deliberately simple — a name read inside the lambda, not bound inside it, not a global
  — and **flow-insensitive**, which is sound because reading an outer binding later shadowed by
  an inner one is already a use-before-declaration error. What makes that simplicity safe is
  that both failure directions are *loud*: an over-capture costs a wasted copy (the body's own
  binding shadows it) or errors if no enclosing binding exists, and an under-capture errors too,
  since a lifted body starts with an empty local set. The visible consequence of by-value
  capture is that **assigning to a captured binding is rejected** (`lyra-E024`) instead of
  silently writing to the closure's own copy — the same lost-write failure the by-value `mut`
  parameter had, and refusing it is the same call.
  **Four front-end gaps had to close.** A binding holding a *call result* was "not callable"
  (`let add5 = makeAdder(5)`) — its type says otherwise. An **annotated** lambda in value
  position never had its body checked, so its expressions had no recorded types at all and
  neither the capture pass nor the backend could read them. A nested lambda could not see the
  enclosing lambda's **parameters** (`withParamScope` replaced the map rather than extending
  it), which stayed hidden precisely because those bodies were never checked — so `(n) -> … =>
  (x) -> … => x + n` reported `n` undefined the moment they were. And any expression evaluating
  to a function is now callable, which is what makes `fs[1](5)` and a closure-valued struct
  field work.
  **One leak found by accounting, not by ASan.** An indirect call had no `LambdaExpr` to
  resolve, so the ownership pass treated it as an *unknown* callee — whose result is
  conservatively borrowed — and every string a closure returned leaked. It now reads the
  callee's static `LambdaType` (`calleeLambdaType`): parameters of a function type are borrows
  (a function type cannot express `own`), and the result follows the declared return convention.
  Separately, dropping the environment's own retain on a captured managed value is a genuine
  **double free** — the enclosing binding releases the box and the environment's glue releases
  it again — and the program still runs clean under ASan (the second release reads a refcount
  out of freed memory, gets a poison pattern rather than 1, and never reaches the second free).
  The `alloc + retain == release` count catches it instantly; that is the assertion the test
  carries, with the mutation verified to fail it.
  Tests: `backend/llvm/llvm_closure_test.go` (11 exec cases — named function as a value, local
  lambda, captured local, returned closure, lambda literal as an argument,
  called-twice-through-a-parameter, closure in a struct field, array of closures, multi-width
  captures, capture through a nested closure, void closure; 3 managed-capture cases on stdout +
  ASan; 4 IR-shape cases pinning the fat pointer, the lifted signature, no-allocation for
  captureless, and that a direct call keeps its plain signature; and the two accounting cases
  above), two closure entries in the path-sensitive conservation corpus,
  `analyzer/captures/captures_test.go` (12), `checker/captured_assignment_test.go` (6),
  `typechecker/tests/closure_test.go` (6). The `cmd/lyrac` backend-error fixture moved again —
  to struct record-update syntax, since higher-order calls now lower.
  **Deferred, loud errors:** a `mut`/`ref` parameter on a lambda used as a value (a function
  type carries no borrow mode, so the call site would pass by value while the body expected a
  pointer — a disagreement that is a miscompile, not an error), multi-clause lambdas, and a
  lambda with no return annotation used as a value. Still open and pre-existing: a `let`
  declared inside a loop body is not visible there, so a closure cannot be bound inside one.
- **`newtype` lowers — the typechecker no longer enforces a feature the compiler can't build.**
  Nominal isolation landed earlier the same day, but a `newtype` declaration hit `llvm:
  unsupported type`, so *no* program using one could be compiled. **The lowering is to emit
  nothing.** A newtype is nominal to the typechecker and *is* its base at run time, so it
  registers no LLVM type — deliberately not an alias, which would force every arithmetic,
  comparison, and coercion site to reconcile two llir types for one machine value, for no gain,
  since nominal identity has already done its work by the time codegen runs. Transparency then
  runs through **two choke points**: `lowerType` strips the wrapper for a type read off an
  *annotation* (parameter, return, field, element), and a new **`recordedType`** — replacing all
  ~24 direct `TypeTable.Get` calls in the backend — strips it for a type read off the TypeTable.
  Both use `stripNewtype`, which also resolves a type written as a *name* just far enough to
  answer "is this a newtype?" (a field declared `Email` is recorded as an `UnresolvedType`, so
  the lookup is the only way to reach the newtype at all) and leaves every other name alone,
  since `UnresolvedType` is load-bearing for
  `lookupNamedType`/`namedStructFields`/`resolveDataType`. **What that reveals is how many
  questions are representation questions**: which LLVM type, is the value refcount-managed, an
  untyped literal's width, how `print` formats it, which drop glue, whether an overwritten slot
  owns what it holds. Each is answered against the base now (`types.StripNewtype`, shared with
  `ownership.IsManaged`/`OwnsManaged`), so a newtype over a *managed* base is managed — retained
  on copy, released on death, a move into an `own` parameter (`lyra-E019`) — exactly as the base
  is. Verified end to end: construction and read-out, through parameters and returns, copies,
  struct fields, fixed and dynamic array elements, `match` scrutinees, bool and signed bases,
  and managed bases under ASan with alloc/retain/release accounting. **Three front-end gaps had
  to close for any of it to be usable**, each of which had been silently making newtypes
  unusable or wrong:
  1. **Literal width.** The recorded type of an initializer annotated with a newtype *is* the
     newtype, so `propagateLiteralType` bailed and nothing narrowed the leaves: `let s: Small =
     200 + 100` computed 300 in signed i64, tripped no check, and truncated to 44 — where the
     identical expression against a bare u8 traps. A newtype context now propagates its base.
     Both halves are pinned: the constant form is a compile error, the runtime form traps at the
     base's width (`uadd.with.overflow.i8`).
  2. **The base's own range went unchecked.** `checkIntegerLiteralRange` skipped a
     `*ConstrainedType` entirely on the reasoning that a range constraint subsumes base overflow
     — true when there *is* one, but the commonest newtype (`newtype Meters = i64`, no
     constraints) then had no range check at all. It now checks the base unless the newtype
     declares its own `range(…)`, in which case `lyra-E023` still owns the report, so one
     mistake still yields one diagnostic.
  3. **A named type couldn't survive a function or field boundary.** A call's declared return
     type and a struct field's declared type were returned raw, and a raw `UnresolvedType`
     compares unequal to the same type resolved from an annotation — so `let p: Point = mk()`
     reported the tell-tale **"cannot assign Point to Point"**. Not a newtype bug at all: it hit
     every named type, and structs equally. Both are resolved now (`resolveTypeIfKnown`,
     matching how parameter types already were). Distinctness is untouched — resolving both
     sides is what lets `TypesEqual` compare them at all, so a `Meters`-returning call is still
     rejected against a `Feet` annotation.
  **One leak found and fixed along the way:** the lvalue walk carries the assignment target's
  declared type, so a managed field declared `Email` failed the managed test and the overwritten
  box was **never released** — one leak per assignment, and invisible (macOS ASan doesn't report
  leaks, the program prints the right answer, and the release counts stay plausible). Fixing
  only the managed test would have been *worse* than the leak: the release path asks that same
  type whether the value is a string fat pointer (recover its box) or already a box pointer, so
  it would have released a fat pointer as a box. Normalized in one place instead —
  `lvalueAddress` strips before returning — so the two questions can't disagree. Tests:
  `backend/llvm/llvm_newtype_test.go` (9 exec cases, the base-width arithmetic pair, 3
  managed-base cases with stdout, an ASan + conservation case, 2 IR-shape cases, and the
  managed-assignment leak case whose release *counts* are the detector), a newtype case in the
  path-sensitive conservation corpus, `typechecker/tests/constrained_type_test.go` (+9:
  call/field round trips incl. the struct one, base-range overflow, and the
  constraint-owns-the-report split), `ownership/ownership_test.go` (+2),
  `checker/use_after_move_test.go` (+1). Two tests that asserted the old deferral were
  repointed: the backend one now pins that a newtype emits *no* LLVM type, and `cmd/lyrac`'s
  backend-error fixture moved to a higher-order call (closures are the long-lived deferral).
  **Still open:** a *chained* newtype (`newtype UserId = Id` where `Id` is one) doesn't
  type-check — `isAssignable` has no symbol table, so it compares against the unresolved base
  name; codegen already handles the chain, so this is a front-end fix whenever it's wanted.
  Arithmetic on a newtype value stays rejected (`types.IsNumeric` excludes it) and so does
  `print` — reading out to the base is the one way to operate on one, which is the nominal
  discipline working as intended, not a gap.
- **A path-sensitive conservation check over the emitted IR.** Four bugs this month shared one
  shape: accounting that looks balanced but isn't *per path*. The `[head, ...tail]` guard leak
  was the sharpest — one allocation, one release, perfectly balanced totals, with the
  guard-false edge carrying the box past its only release. Nothing caught it: not the counting
  conservation test (totals are the wrong granularity), not the behavioral tests (the program
  returned the right answer), not ASan (which on macOS reports use-after-free and double-free
  but *not* leaks). It took reading the CFG by hand. This makes that reading mechanical: from
  each `lyra_rc_alloc`, follow the box forward — through bitcasts, GEPs, phis, selects,
  insert/extractvalue, and stores into local slots — and report if a `ret` is reachable with it
  neither released nor escaped (`conservation_check_test.go`). **Tuned for no false positives**,
  since a noisy assertion gets deleted: any use it doesn't fully model — passing the box to a
  user function (which may take ownership), storing it through a computed pointer, returning it
  — marks the value *escaped* and drops it from consideration. It admits false negatives by
  design; it is a net for one specific, repeatedly costly shape, not a verifier. `Backend.Emit`
  was split over a new `emitModule` so the check gets the real `*ir.Module` instead of
  re-parsing the printed text (the alternative, llir's `asm` parser, would have added module
  dependencies for a test). **Two guards keep it honest, and both earned their place
  immediately.** (1) A hand-built leaky module it must flag: while matching names with llir's
  `Ident()` — which prefixes the `@` sigil — it matched *nothing*, so every corpus program
  tracked zero allocations and the whole suite passed vacuously; the self-test is what exposed
  that. (2) A per-program assertion that at least one allocation was genuinely path-checked,
  which then caught three corpus programs that proved nothing (two passed their box to a call,
  one returned it — all legitimately escaping). **Validated against the real bug:** with the
  guard fix temporarily reverted, the check fires on `pick` and names the leaking exit;
  restored, it is silent. Corpus of 10 branching programs (guarded and unguarded tail bindings,
  concat in a branch, an if merging two fresh boxes, allocation in a loop, early return and
  break past a live box, match on a fresh string, interpolation in a branch, a managed dynamic
  array).
- **`[head, ...tail]` array patterns lower — the recursive list idiom works end to end.** The
  last deferred array-match form. `tail` is the one array binding that is *not* a borrow: the
  elements it needs are a suffix of a box whose header describes the whole array, so there is no
  existing storage to alias. `bindTailSubArray` (`match_array.go`) allocates a box sized at run
  time (`length - fixedCount`), copies the suffix in a loop, and binds it; the arm's length test
  becomes `>=`. **Managed elements are retained per element** — the tail's drop glue releases
  them when it dies, so the reference is duplicated rather than moved out of the source, or the
  two would both free them. **It needed no ownership-pass change**, which is what it had been
  filed as blocked on for weeks: the pass keys managed-ness off the recorded type, so it already
  sees `tail` as managed, and because a pattern binding is not a `VarDeclStmt` it is never
  last-use-eligible — so every *owning* use inside the arm (returning the tail, passing it to an
  `own` parameter) records a plain `Retain`, which is exactly the +1 an escape needs. The frame
  release balances the box's own reference. Cost is one retain/release pair versus a transfer:
  refcount traffic, not a leak. **The one real design catch** was *where* to release it: framing
  it in the enclosing scope faults, because those releases run on every path and an arm that
  never matched has an uninitialized slot (found by a BUS error in `lyra_rc_release`). It lives
  in an **arm-scoped frame** instead, putting the release exactly on the path that allocated —
  and `emitReturn`'s release-all-frames still covers a body that returns. Tests:
  `backend/llvm/llvm_match_array_test.go` (recursive sum, two fixed elements before the rest, a
  one-element array yielding an *empty* tail, a literal element in front of the rest, the tail
  escaping as the arm's value, and `[]string` managed elements; plus an ASan case where a
  managed tail outlives the match and the source must stay intact). The deferral test that
  asserted the loud error was replaced. **Guard edge (fixed in the same change):** a guard is
  tested *after* the pattern's bindings exist, so a `[h, ...t]` arm has already allocated by the
  time it runs — and a failing guard falls through to the next arm, skipping the body's release,
  leaking a box per failure. Confirmed against the emitted CFG: the allocation sat in the
  pre-guard block, the release only in the body block. The guard's false edge now gets its own
  block releasing the arm frame before branching on. The *pattern*'s own failure edges need no
  such treatment (the length and element tests all branch before anything is allocated), and an
  unguarded arm gains no extra release. Covered by an exec+ASan case driving 0, 1, and 2 failed
  guards per iteration across 150 calls with managed elements — a missed release leaks a box per
  failure, an over-release is a double free — plus IR assertions pinning one allocation against
  two releases for a guarded arm and exactly one for an unguarded one.
- **Array-match exhaustiveness understands length unions.** `arrayMatchIsExhaustive` only
  recognized a *single* covering arm (a bare `[...rest]` or a catch-all), so the canonical `[]
  => …, [h, ...t] => …` — the very idiom the tail-binding work above enables — drew a spurious
  "not exhaustive" warning demanding an unreachable wildcard. Same corrosive shape as the
  tuple/struct false warning fixed earlier: it trains users to ignore the warning class that
  also covers genuinely non-exhaustive matches. An array match is over *lengths*, so the arms'
  coverage is now unioned: `[e1..en]` covers exactly n, `[e1..en, ...rest]` covers every length
  ≥ n, and the match is exhaustive when every length below the smallest open-ended arm is
  covered exactly. Only arms whose element sub-patterns are all irrefutable count (reusing
  `patternIsIrrefutable`) — `[1, ...rest]` matches only arrays starting with 1, so it proves
  nothing — and a guarded arm contributes nothing, as everywhere else. Tests:
  `typechecker/tests/match_expr_arrays_test.go` (the idiom and a three-arm full cover accepted;
  a gap below the rest arm, a literal-test arm, no rest arm at all, and a guarded arm each still
  warn).
- **Use-after-free: the ownership pass didn't recurse into arithmetic.** Went looking for the
  ownership-pass extension that `[head, ...tail]` was blocked on, and found the blocker was a
  live memory-safety bug rather than a mere prerequisite.
  `MathBinaryOpExpr`/`MathAssignOpExpr`/`NegationExpr` fell through the pass's `expr` walker to
  a `default:` that records nothing, documented as safe because "a missed release only leaks".
  Arithmetic genuinely has no managed *operands* — they're numbers — but a managed value can sit
  **inside** one, and `consume(p.name) + 1` passes a managed field to an `own` parameter, an
  owning position that needs a retain. With none recorded, the callee released a reference the
  caller never granted: the box was freed while the struct still held it, so the struct's own
  drop freed it a second time and any read of the field in between was a **heap-use-after-free**
  (ASan abort, exit 134, on `if p.name == "xy"`). The premise was wrong the same way the
  stack-aggregate use-after-free's was — a missed *retain* at an owning position dangles, it
  doesn't leak — so skipping a node is not the conservative default it was written up as; the
  safety bias only applies to nodes the pass actually *visits*. **Fix:** recurse into all three
  forms with `needOwned=false` (the arithmetic borrows its numeric operands; any call beneath is
  classified by the existing `FunctionCallExpr` case), and rewrite the `default:` comment to
  state the real condition — a form may be skipped only if nothing beneath it can hold a value.
  Verified the whole-*binding* shape (`consume(a) + 1`) still transfers exactly once rather than
  gaining a double retain: it worked before only because the last-use walker, which does
  recurse, happened to see it. **Note the shielding:** the double-*move* shape (`consume(a)`
  twice in a loop) never reached the bug because `lyra-E019` rejects it at compile time; only a
  *field* argument slipped through, since E019 deliberately doesn't count `p.name` as a move
  (that's a partial move, its own design question). Tests:
  `backend/llvm/llvm_ownership_arith_test.go` (the reduced repro, all three arithmetic forms
  consuming a field then reading it back, and the binding-transfers-once control — each plain +
  ASan). **Unblocks** `[head, ...tail]`, which now needs only its allocate-and-copy lowering.
- **`rune` becomes an ordered, convertible scalar — classification logic is expressible at
  last.** A `rune` supported `==`/`!=`, `match` on literal arms, and `print`, and nothing else:
  `c < 'z'` errored ("operands must be numeric"), `i32(c)` errored ("cannot convert rune to
  i32"), and `rune(n)` wasn't even a conversion target ("undefined function"). Net effect:
  is-digit / is-alpha / digit-value could not be written *at all*. **Design (Rust's `char`
  split):** rune is now **ordered** — the four comparisons work between two runes, by code point
  — and **convertible** to and from the *integer* types explicitly (`i32(c)`, `i64(c)`,
  `rune(n)`; rune↔float and rune↔string stay rejected, since a code point has no float reading).
  It deliberately stays **out of `types.IsNumeric`, so it still has no arithmetic**: `c + 1` is
  rejected and the idiom is `i32(c) - i32('0')`, which writes the code-point/number crossing
  down rather than letting rune silently behave as an i32. Comparing a rune against an integer
  likewise needs the conversion. **One representation decision:** `IsSignedInt(rune)` flipped to
  true, so widening sign-extends and ordering uses the signed predicate — a rune lowers as i32
  and Go's rune *is* int32. That was unobservable while nothing consulted it for a rune (an
  existing test asserted the opposite and was updated with the rationale); valid code points are
  non-negative, so the readings differ only for a rune built by `rune(n)` from a negative
  integer, where sign-extension preserves the bit pattern's meaning as Go's does. Tests:
  `typechecker/tests/rune_type_test.go` (ordering, rune-vs-int rejected, arithmetic rejected,
  conversions both ways incl. an untyped literal, float/string rejected),
  `backend/llvm/llvm_rune_test.go` (6 exec cases — is-digit both ways, is-alpha across both
  ranges, digit-value via conversion, a `rune(n)` round trip, and a multibyte `é` ordering +
  widening; IR: `icmp slt i32` + `sext i32`). **Still open:** char *range* patterns (`'a'..'z'`)
  don't parse — `range_pattern`'s bounds are number-literal-only — so `match`-based
  classification still needs literal arms or an `if` chain. Now ergonomics rather than a
  blocker, since ordering makes the `if` form expressible.
- **Newtypes are nominally isolated — `Meters` and `Feet` no longer interconvert.**
  `isAssignable` had two individually-correct rules — a value satisfying the base is assignable
  *to* a constrained type (construction, `let m: Meters = 5`), and a constrained value is
  assignable to its base (`let raw: i64 = m`, the only way to read it, since a newtype has no
  field accessor) — but nothing stopped them **chaining**: `Meters` → `i64` → `Feet`
  type-checked silently, so every newtype over a common base was mutually assignable and the
  unit-mixup a newtype exists to prevent went uncaught. Fixed by rejecting the pair up front
  when both sides are `*ConstrainedType` with different names, leaving both single-step rules
  intact — the minimum that restores nominality without making newtypes unbuildable or
  unreadable. Holds at annotations, call arguments, and returns. Tests:
  `typechecker/tests/constrained_type_test.go` (distinct newtypes rejected via return type and
  call argument; construction, read-out, and same-type all still accepted). **Note this was
  check-only** — a `newtype` declaration couldn't be lowered (`llvm: unsupported type`), so the
  isolation was enforced where it matters (the typechecker) but no program using one could be
  built. That gap closed the same day; see the newtype-lowering entry.
- **`ref` parameters are passed by reference too, and a `mut` borrow is now exclusive.** `ref`
  is a borrow like `mut`, but it was still copied at every call: a `ref [8]i64` was passed as a
  64-byte first-class `[8 x i64]`, a `ref` struct as a whole struct. Since an immutable borrow
  can't write, this was a **codegen waste rather than a correctness bug** — the reason it sat
  open — and `paramIsByRef` now covers `Ref` alongside `Mut` (scalars still excluded via the
  shared `types.IsCopiedScalar`, as `lyra-W010` calls the modifier inert there). **The catch,
  found before implementing:** by-reference makes a `ref` see the caller's *live* value instead
  of a snapshot, which is observable when the same binding also reaches a `mut` parameter of the
  same call. `both(p, p)` with `(a: ref Pt, b: mut Pt)` compiled silently and returned 1
  (snapshot); by-reference it returns 99. So this is not a pure optimization, and Lyra has no
  borrow checker to reject the aliased call. **Paired fix:** `checkExclusiveMutableBorrow`
  rejects passing a binding to a `mut` parameter *and* to any other argument of that call —
  Rust's exclusivity rule, scoped narrowly to argument roots within one call, which is exactly
  the aliasing by-reference passing introduces. That also closes a hole the `mut` change opened
  the same day: `two(p, p)` with two `mut` parameters let each write clobber the other, silently
  (measured: returned 20, not 10). Two `ref` arguments may still share a binding (neither can
  write), and scalars are exempt. **One shape `mut` didn't need:** a `ref` argument may
  legitimately be a **temporary** (`f(Pt { x: 1 })`, `f("a" ++ "b")`) — lending one out is fine,
  where writing to one is meaningless — so `argumentAddress` materializes a non-lvalue into an
  entry-block alloca and passes that; an owned temporary is still released after the statement
  by the ordinary pending-temp machinery (ASan-verified for a managed field and a managed
  temporary through `ref`). Tests: `backend/llvm/llvm_mut_param_test.go` (5 exec cases —
  binding, temporary, fixed array, forwarded ref→ref, managed temporary; IR: `[8 x i64]*` for
  the aggregate, by-value `i64` for the scalar), `typechecker/tests/interior_mutation_test.go`
  (5 exclusivity cases incl. the two allowed shapes and the scalar exemption).
- **Regex literals are `r"…"`, not `r/…/` — killing the division ambiguity outright.** `r` is an
  ordinary identifier and `/` is division, so the old delimiters made `let ratio = r/2 + a/b`
  lex as the regex `r/2 + a/` plus a stray `b`, silently and with no diagnostic from any pass.
  Earlier that day the token was bounded to one line, which stopped an unterminated literal from
  swallowing the rest of the file but left the same-line case intact — a mitigation, not a fix.
  **There is no lexical fix for slash delimiters:** the deciding context (is this expression
  position or after a value?) is arbitrarily far to the right, and a regex may legally contain
  spaces, digits, and operators — the phone-number pattern in the corpus has spaces — so no
  heuristic on the content can separate the two readings. The cure has to be a delimiter that
  cannot follow an identifier. **`"` is exactly that:** Lyra has no juxtaposition application
  (calls require parens), so `r"` can only ever start a regex, and `r/2` is unambiguously
  division again. Kept the `r` sigil, so the node and highlight query are unchanged and the form
  matches how a Python programmer already writes a pattern (`r"\d+"`). **Two bonuses:** `/`
  needs no escaping inside a pattern — `r"/usr/local/bin"` where the old form demanded
  `r/\/usr\/local\/bin/` — and an unterminated literal now degrades to an identifier plus an
  unterminated string, still a loud parse error, rather than running on. The delimiter itself
  escapes as `\"` (verified through `pkg/regex`, which reads it as a literal quote).
  **Migration:** every `r/…/` becomes `r"…"`, dropping any `\/`; the two collection sites
  (expression and pattern position), `regexPatternBody`, and three diagnostic messages were
  updated with it. Tests: corpus (phone pattern, unescaped slashes, escaped quote, `r/2 + a/b`
  parsing as arithmetic, unterminated-is-an-error), `typechecker/tests/regex_test.go` (division
  not shadowed — same-line, cross-line, and before a `//` comment; slashes need no escape;
  escaped quote), regenerated collector goldens. **Note this is a user-visible syntax change** —
  the only one in this batch — so any existing `r/…/` in user code or docs must be migrated.
- **Assignment to a parameter was never type-checked (unreported errors + a backend panic).**
  Found while fixing #8(d): `n = n + 1` on a parameter failed the build with `llvm: type not
  found for *ast.IdentifierExpr`. The cause was upstream of the backend — `checkAssignToBinding`
  resolves the target via `tc.scope.Lookup` + a `*ast.VarDeclStmt` assertion, but a parameter is
  neither (it lives in `tc.paramTypes`), so the function returned at the failed lookup and the
  statement was **skipped entirely**. Consequences, all silent: no assignability check (`n =
  "hello"` on an `i64` parameter — accepted), no literal-range check (`n = 9999` on an `i8` —
  accepted), and, because the RHS was never *inferred*, not even an undefined-identifier report
  (`n = undefinedVar` — accepted). The backend then either failed loudly (integer arithmetic,
  whose `getIntSignedness` needs the recorded type — a bare literal RHS doesn't, and the float
  path doesn't consult signedness, which is why only that one shape surfaced) or **panicked
  outright** (`panic: store operands are not compatible: src=i1; dst=i64*` for `n = true`),
  violating the backend's "never emit wrong code, error loudly" discipline — a panic is a crash,
  not a diagnostic. **Fix:** `checkAssignToBinding` resolves `paramTypes` first (parameters
  shadow outer bindings, mirroring `IdentifierExpr` resolution and `checkLValueAssignment`'s
  ordering), and the shared tail — infer RHS, assignability, allocation flavor — moved into
  `checkAssignedValue` so the variable and parameter paths cannot diverge. **Semantics:**
  reassignment stays permitted for every parameter mode; by value it rebinds the callee's own
  copy, and through a by-reference `mut` parameter it now writes to the caller, which is what
  that modifier means. A `mut` *scalar* remains by value and so doesn't propagate — the one
  split, and precisely the case `lyra-W010` already warns is inert. Tests:
  `typechecker/tests/interior_mutation_test.go` (the four previously-unreported diagnostics +
  well-typed acceptance across bare/`own`/`mut`/float),
  `backend/llvm/llvm_param_reassign_test.go` (7 exec cases incl. narrow width, reading another
  parameter, by-value rebind *not* reaching the caller vs a `mut` aggregate rebind that does;
  plus an ASan case for managed reassignment on both sides of the convention).
- **`mut` parameters are passed by reference — the silent lost-write miscompile (borrow-model
  #8(d)).** `mut` is a *mutable borrow* and the typechecker's own diagnostic tells users it
  mutates "the caller's value", but `lowerParameter`/`defineFunction` copied **every** argument
  into a fresh alloca, so the callee mutated a private copy and the write vanished. Worse, it
  split on the value's representation with **no diagnostic either way**: `mut []T` and `mut
  shared T` propagated (already pointers) while `mut Person`, `mut [2]string`, and even `mut
  Counter { n: u8 }` silently dropped the write — nothing to do with managed values. **Fix:**
  `paramIsByRef` (functions.go) makes a `mut` parameter a pointer to the parameter's type;
  `defineFunction` binds that incoming pointer *directly* as the binding's slot (the
  alloca+store *was* the bug); and the call site passes the argument's address through the
  existing `lvalueAddress` walker, so a bare binding, a path (`f(ps[0])`, `f(o.inner)`), and
  forwarding a by-ref parameter onward all name the caller's storage. `own` stays by value (it
  transfers — the copy is the point); `ref` stays by value (an immutable borrow can't write, so
  by-reference is unobservable — a future optimization for large aggregates, not a semantic
  fix). **Two things the change forced:** `arrayLValue` had to address a by-ref array parameter
  *in place* (its fall-through materializes the array into a fresh alloca, which would have
  reintroduced the copy for `mut [N]T` — the one site where the fix could have silently
  half-worked), and the seven `slot.(*ir.InstAlloca)` assertions became `slotElemType`, since a
  by-ref parameter's slot is an `ir.Param`, not an alloca. **Scalars are exempt and that is not
  a silent split:** a `mut` on a copied scalar stays by value, which is exactly the case
  `lyra-W010` already warns is inert — both now read one shared predicate
  (`types.IsCopiedScalar`) so the diagnostic and the lowering cannot drift. A scalar has no
  interior, and the only construct that could observe a by-ref scalar — whole-parameter
  reassignment (`n = n + 1`) — **doesn't lower for integers at all** (verified pre-existing:
  identical failure before and after this change, `getIntSignedness` finds no recorded type for
  the identifier); passing them by reference would change their ABI and reject `f(5)` for no
  observable gain. If that ever lowers, `paramIsByRef` and W010 should be revisited together.
  **Call-site enforcement (new):** `checkMutArgument` requires a `mut` argument to be an
  **lvalue rooted at a mutable binding**, sharing `rootBindingIsMutable` with
  `checkLValueAssignment` so passing-to-`mut` and writing-through-the-path obey one rule.
  Neither half was checked before: a temporary (`poke(Pt { x: 1 })`) silently discarded its
  writes, and a deeply-immutable `let` could be mutated through a call — the mutability system
  laundered by a function boundary. Forwarding a `ref` parameter into a `mut` one is now
  rejected for the same reason. **Also closes a leak:** `releaseOldTarget`'s "borrowed root"
  refusal existed precisely because the callee's copy shared the caller's reference; a by-ref
  slot *is* the caller's storage, so the overwritten managed value is a genuine reference to
  drop (`lvalueRootIsOwning` consults `byRefParams`), ASan-verified over repeated writes. Tests:
  `backend/llvm/llvm_mut_param_test.go` (8 exec cases — stack struct, `[N]T`, nested field,
  two-level forwarding, struct-in-array, plus the always-worked `[]T`/`shared` shapes and an
  lvalue-path argument; IR: the parameter is a pointer with no entry-block copy, `own` stays by
  value, `mut` scalar stays by value; ASan: a managed field renamed through a `mut` param is
  leak-free), `typechecker/tests/interior_mutation_test.go` (7 call-site cases incl. the
  `ref`→`mut` launder). Two existing tests asserted the bug — the "mut parameter callee"
  aliasing case (which expected the write to be *lost*) and the release-IR case expecting 0
  releases in the callee — and were rewritten to the corrected behavior.
- **Scanner: comments are no longer recognized *inside* string literals (silent code-swallowing
  fix).** Comments are `extras`, so `BLOCK_COMMENT` is valid at essentially every token boundary
  — including each string content-chunk boundary — and the scanner's comment branch ran *before*
  its in-string branch (`tree-sitter-lyra/src/scanner.c`). A string whose content began with
  `/*` therefore lexed as a **comment running to the next `*/` anywhere later in the file**:
  `let open = "/*"` followed by `let close = "*/"` collected `open` as a two-line string
  containing code, made `close` vanish entirely, and **`lyrac check` exited 0 with no
  diagnostic**. It fired wherever a fresh chunk starts — after the opening quote, right after a
  `${…}` interpolation, and (because `scan_block_comment` skips leading whitespace as token
  padding) after a leading space, `" /* note */ price"`. **Fix:** gate the comment scan on
  `!in_string(scanner)`. An interpolation is an expression context where comments are
  legitimate, and `in_string()` is already false for `CTX_INTERPOLATION`, so that distinction
  came for free. `//` was never affected (the content scan consumes it as ordinary bytes).
  **Bonus fix, same root cause:** the padding skip was also eating a content chunk's *leading
  whitespace* from the CST — `"${prefix} ${name}"` emitted its middle space nowhere, and one
  corpus test had encoded that loss as expected output. The CST is now byte-exact; the
  collector's raw-source re-slice (which already produced correct values) stays as the
  authoritative path. Tests: `tree-sitter-lyra/test/corpus/literals/string.txt` (openers/closers
  as content at each boundary, leading-whitespace case, `//` cases, mid-content `path/*.txt`,
  and real comments still parsing outside strings — incl. nested),
  `lyra/pkg/analyzer/collector/tests/string_whitespace_test.go` (exact values, plus "the
  following declaration still exists"). 370/370 corpus, full Go suite green after `go clean
  -cache`.
- **Regex literals can no longer span a newline.** `r/…/` is one token that outranks the
  identifier rule and `r` is a valid identifier, so `let ratio = r/2` (no spaces) starts
  something shaped like a regex; the content classes had no newline bound, so the token ran to
  the next `/` **anywhere later in the file** — including the first slash of a `//` comment —
  swallowing the intervening code into one literal with zero diagnostics. Excluding `\n`/`\r`
  from the content classes (`include/literals/regex.js`) bounds the damage to a single line and
  restores `r/2` at end-of-line as ordinary division. **Not removed**, despite the token being
  pure cost in expression position: `regex_literal` also backs `pattern(r/…/)` constraints on
  `newtype` and `regex_pattern` in match arms, and the constraint path is *implemented*
  (`pkg/regex` is a full DFA engine; the typechecker enforces `PatternConstraint`) — only the
  match-arm pattern form is unlowered. **Still open:** a same-line `r/2 + a/b` is mis-lexed; the
  real cure is a delimiter that can't collide with identifier-plus-division, which is a language
  design decision. Tests: corpus `A regex literal cannot span a newline`,
  `typechecker/tests/regex_test.go` (`r/2` + `a/b` on consecutive lines type-check as division).
- **Non-exhaustive `match` traps instead of running off `unreachable` (UB fix), and irrefutable
  aggregate patterns count as exhaustive.** Exhaustiveness is a hard error (`lyra-E009`) only
  for `bool` and `data`; for int/string/rune/float/array/tuple/struct it is a **warning**, and
  warnings never gate a build (`driver.Result.HasErrors` filters `SeverityError`) — so a
  fell-through match was reachable in a program that compiled clean, and every match ladder
  ended in a bare `unreachable`: **undefined behavior** (SIGTRAP/exit 133 at -O0, arbitrary
  under optimization), outside the language's own trap discipline. A **fully-guarded** match
  (`match x { y if y > 100 => … }`) reached that edge *deterministically*, since a guarded arm
  never seals the ladder. **Fix:** a new `sealMatchFallthrough` (`trap.go`) terminates the edge
  with the standard trap — `lyra: match not exhaustive` on stderr, exit 101, via the same
  `panicFunc` machinery as overflow/divide-by-zero/bounds — wired into all **four** fall-through
  sites: the scalar ladder (`match.go`), the aggregate ladder (`match_aggregate.go`), the array
  ladder (`match_array.go`), and the `data` tag-switch default (which is unreachable in a
  well-typed program thanks to E009, so there it is defense in depth costing one basic block).
  An exhaustive match emits no trap at all (pay-for-what-you-use, pinned by a test). **Second
  half — the false warning that trained users to ignore this class:** `match pair { (a, b) => …
  }` warned "not exhaustive" and demanded an unreachable wildcard, because tuple/struct
  exhaustiveness was just `hasUnguardedCatchAll`. A tuple/struct is *single-shape* (no tag to
  discriminate), so a destructuring arm whose every sub-pattern binds rather than tests is
  **irrefutable** and complete: new `patternIsIrrefutable`/`aggregateMatchIsExhaustive`
  (`typechecker_control_flow.go`), recursing through nested tuple/struct patterns and `name @
  inner` bindings, treating a shorthand field (`{ x }`, nil sub-pattern) as a binding leaf. This
  deliberately mirrors the backend's `aggPatternTest`, which returns a nil condition for
  precisely those patterns — "emits no runtime test" and "counts as exhaustive" are now the same
  judgment. A literal sub-pattern (`(0, b)`, `{ age: 30 }`) is still refutable and still warns;
  a guarded irrefutable arm still warns. **Also:** the six open-type exhaustiveness warnings
  carried the generic `lyra-E001` instead of `lyra-E009` (only the bool/data errors used the
  code) — now all eight are `lyra-E009`, so the diagnostic is filterable by code. **Deliberately
  unchanged:** the error-vs-warning split itself, which `diag.CodeNonExhaustiveMatch`'s doc
  comment documents as intentional (closed types error, open types warn) and which is now backed
  by defined runtime behavior rather than UB. Tests: `backend/llvm/llvm_match_trap_test.go`
  (exec: scalar fall-through traps with exit 101 after the matched arm ran; all-guards-fail
  traps; string/rune/float/dynamic-array/tuple/struct scrutinees each trap; IR: an exhaustive
  match emits no `lyra_panic_match_failed`, a non-exhaustive one defines and calls it;
  irrefutable aggregate destructuring runs trap-free), plus rewritten tuple/struct
  exhaustiveness tests (`match_expr_tuples_test.go`, `match_expr_structs_test.go` — the two that
  asserted the old false warning now pin irrefutable-is-exhaustive, with nested-irrefutable,
  literal-sub-pattern, and guarded-arm counterparts).
- **Int literal in a float slot — miscompile fixed (typechecker adaptation + backend float
  constant).** `let x: f64 = 5` type-checked (untyped int is assignable to floats) but no
  conversion was ever materialized: `propagateLiteralType` bailed on a float context ("handled
  by assignability, not here") and the backend's `literalIntType` fell back to i64 — an integer
  value in a float slot. `print(x)` on `let x: f64 = -5` printed 18446744073709551611 (print
  dispatches float-vs-int on the lowered LLVM type but signedness on the Lyra type → `%llu`),
  and `x + 0.5` emitted invalid IR (`@llvm.uadd.with.overflow.i64(i64, double)`) that `lyrac
  build` "succeeded" on and only clang rejected. Blast radius: every context that accepts an
  untyped int against a float type — annotation, call argument, struct field, data payload,
  return body, match arm, comparison sibling, reassignment. **Fix, two sides:**
  `propagateLiteralType`'s `IntegerLiteralExpr` case records the float type onto an untyped int
  literal in a float context (all nine context sites inherit it), and the backend's literal
  lowering emits a float constant at the recorded width when the typechecker adapted the literal
  (`literalRecordedFloatType`, `arithmetic.go`), keeping the i64 fallback otherwise.
  **Range-analysis companion:** the interval pass no longer tracks a float-adapted literal
  (`literalAdaptedToFloat`) — it's an *integer* analysis, and a float's runtime value can
  diverge from the source integer (f32 rounds 16777217 → 16777216), so a source-text interval
  would be wrong, not just imprecise; this also stops a spurious W011 on float comparisons (`let
  a: f64 = 5; a > 4`). Unchanged: mixed-kind literal unification still errors loudly (`if flag {
  3 } else { 4.5 }` against `-> f64` — untyped int and float literals don't unify), and an
  unannotated literal still defaults to i64. Tests:
  `typechecker/tests/float_literal_context_test.go` (recorded leaf types across annotation /
  nested arithmetic / f32 / negation / call arg / return / struct field / data payload /
  comparison, + the no-context default), `backend/llvm/llvm_float_literal_test.go` (IR: `alloca
  double`/`fadd double`/`fmul float`, no `with.overflow`; exec: the original print-garbage case
  now prints -5, plus arg/arithmetic/comparison and
  field/payload/if-through-return/compound-assign programs).
- **Deep-retain-on-copy — ownership becomes deep, closing the stack-aggregate use-after-frees
  and their leaks.** Every *owning-position* decision now asks `ownership.OwnsManaged(t,
  symTable)` ("does this value transitively own anything refcounted?") instead of `IsManaged`
  ("is this value itself a box"). A `struct Person { name: string }` is not itself managed, but
  a stack aggregate is a *value*: `let q = p` copies it and the copy points at the same string
  box. Treating that as uninteresting left the copy holding a reference nobody had counted —
  which was **not merely a leak**, since an uncounted alias is freed the moment the counted
  owner dies. Two ASan-confirmed use-after-frees, both now fixed: `let q = ps[0]` on a
  `[]Person` then letting the array die (the box's drop glue freed the struct's `name` out from
  under `q` — the reported bug), and interior assignment through one copy (`let q = p; p.name =
  …; q.name`, patched defensively on 07/28 by suppressing the release; now fixed at the root).
  **New `retain.go`** generates a cached `@lyra_retain_T(i8* payload)` per type — the exact
  mirror of `drop.go`'s glue — retaining every managed reference reachable *by value* from T,
  with the same "by value" stopping rule and the same termination argument (a recursive cycle
  must pass through a `shared` field, lyra-E014, which is managed and stops the walk). Both deep
  retain and deep release route through a **glue call** rather than inlining, because a `data`
  value's walk has to switch on its tag and copy sites are everywhere — the call keeps every
  site straight-line, so no CFG threading was needed at the ~6 hook sites. The backend's
  `needsDrop` now delegates to `OwnsManaged`, so the pass (which mints the +1) and the backend
  (which releases it) cannot drift apart — a divergence there is a leak or a double free.
  Framing extended from "managed LLVM value" to "owns managed" at `lowerVarDecl`, `own` params
  (`defineFunction`), var reassignment's release-old, the scope-frame releases, last-use drops,
  and temp flushes; `isManagedSlot`/`isManagedLLVMType` retired from that role. Match arm
  bindings and for-in loop variables stay **borrows** (unframed), as before. **Also re-enabled
  the 07/28 stopgap:** `releaseOldTarget` now permits the release for an inline target rooted at
  an *owning* binding (`lvalueRootIsOwning`/`slotIsFramed`), since copies carry their own +1 —
  closing the leak that the stopgap accepted. It still refuses for a **borrowed root** (a
  `mut`/`ref` param), whose by-value copy shares the *caller's* reference; that leaks instead,
  and is moot anyway because a by-value `mut` param's mutation never reaches the caller (the
  real fix there is by-reference `mut` params, a separate design item). Tests:
  `llvm_deep_retain_test.go` — 21 exec+ASan programs covering every copy site (bindings,
  nesting, tuples, fixed/dynamic arrays, aggregate-field init, array-literal elements, borrow vs
  `own` params, `data` payloads and the tag-switch glue, match arm bindings, for-in,
  reassignment, if-merges, reads out of `shared`/`[]T` boxes, owned temporaries, and a
  copy-and-reassign loop), plus `TestEmit_DeepRetainConservation` (an exact `alloc + retain ==
  release` accounting that walks glue call sites, since macOS ASan can't see leaks and `leaks`
  only reports *unreachable* memory — a never-freed box still on the stack goes unreported),
  `TestEmit_RetainGlueMirrorsDropGlue` (the two glues must cover the same fields), and
  `TestEmit_NoGlueForUnmanagedAggregate` (still pay-for-what-you-use).

### 07/28/26
- **Use-after-free fix: interior assignment through an aliased stack aggregate.** Copying a
  plain **stack** aggregate (struct/tuple/`[N]T`) duplicates the fat pointers of its managed
  fields with **no retain** — the ownership pass has no deep-retain-on-copy, and a stack
  aggregate is not a managed slot. But managed-target interior assignment (landed 07/27)
  *released* the overwritten value, so every other copy of the aggregate dangled. Three
  ASan-confirmed shapes, all compiling with zero warnings: `let q = p; p.name = …; q.name`
  (struct); `let ys = xs; xs[0] = …; ys[0]` (a `[2]string` element); and the worst — **no copy
  visible in the source at all** — a callee taking `p: mut Person` doing `p.name = …`, where the
  by-value parameter copy *is* the alias and the callee freed the *caller's* string. **Fix:**
  `lvalueAddress` now returns an `lvalueLoc{ptr, ty, viaBox}` recording whether the hop that
  reached the slot crossed a ref-counted box, and `lowerLValueAssignment` emits the release-old
  only when it did (`releaseOldTarget`, `lvalue.go`). The reasoning: *every* way of reading a
  managed value out of a container goes through the ownership pass's retain, so it yields its
  own +1 — the one unretained copy is a whole inline aggregate. A slot reached through a box has
  no such alias (copying a `shared` value or a `[]T` copies the box **pointer**, which is
  managed and therefore retained), so every copy names the same slot and overwriting it is
  ordinary aliasing. Only the **final** hop is consulted, since crossing into a box
  re-establishes that invariant — `p.arr[i] = v` on a stack struct holding a `[]string` still
  releases the element. Cost of saying no is a leak (the standing safety bias), and
  inline-aggregate managed values were never freed anyway. `[]string`/`shared
  [N]string`/`shared` struct fields stay fully leak-free. Chose this over deep-retain-on-copy
  (the real fix, below): that redesign must get *every* copy site right — bindings, args,
  returns, aggregate fields, array elements, match bindings, reassignment — and a single missed
  retain is another use-after-free, i.e. exactly the bug class being fixed. Tests:
  `llvm_managed_assign_test.go` — `TestExec_ManagedAssignment_AliasedStackAggregate` (all three
  shapes, plain + ASan) and `TestEmit_ManagedAssignmentReleaseIR` (pins the release present for
  box-interior targets, absent for inline ones, including the mixed `h.items[0]` case). Also
  corrected the stale "a leak, never a double free" claim for managed-in-stack-aggregates in
  `ALLOCATION.md`, `drop.go`, `ownership.go`, and both `CLAUDE.md`s — that invariant died when
  managed-target interior assignment landed.
- **[RESOLVED 07/29 by deep-retain-on-copy] Reading a stack aggregate by value out of a box is a
  use-after-free.** `let q = ps[0]` on a `[]Person` copies the `Person` out of the box by value,
  duplicating its `name` with no retain; when the box dies, the per-type drop glue frees `name`
  and `q` dangles (ASan-confirmed; pre-dates the interior-assignment work, and unaffected by the
  fix above since no assignment is involved). Not fixable the same way — *not* emitting the glue
  would reintroduce the leak it exists to close (freeing a list would leak the spine). **Only
  deep-retain-on-copy closes it**, which makes that item a correctness fix rather than the leak
  cleanup it has been filed as.

### 07/27/26
- **`shared` struct in an lvalue path (`p.x = v` on a `shared` struct) — completes interior
  assignment.** `memberFieldAddress` (`lvalue.go`) previously errored on a `shared` struct
  object; now it addresses the field *through the box* — `box → payload (field 1) → field idx`,
  reusing `lvalueBoxPtr` to load the box pointer — the write counterpart to how
  `lowerMemberExpr` reads a shared field. Because `lvalueAddress` recurses on the object, stack
  fields nested inside a shared struct (`ln.start.x` on a `shared Line`) fall out for free, and
  a **managed field of a shared struct** (`n.name = v` on a `shared Named`) is **fully
  leak-free** — the assignment's release-old frees the overwritten string and the box's drop
  glue frees the final field (unlike a *stack* struct, whose final managed field leaks). Tests:
  `backend/llvm/llvm_shared_member_assign_test.go` (exec: shared struct field, a stack Point
  nested in a shared Line, a managed string field; ASan: heap-string field of a shared struct —
  leak-free). The member-assignment deferral test (which expected a shared struct to error) was
  removed. With this, **interior assignment is complete across stack/shared/dynamic ×
  index/member × managed/non-managed targets**; only an *optional* member target (`p?.x = v`) is
  still a loud error.
- **Managed-target interior assignment (`xs[i] = "s"`, `p.name = "s"`) — release-old +
  own-new.** Previously a managed assignment target (a `string`/`shared`/`[]T` array element or
  struct field) was a loud error; now it lowers. **Ownership pass:** added an
  `LValueAssignmentStmt` case to the `stmt` handler (`a.expr(s.Value, a.isManaged(s.Value))`) —
  it was the one assignment form the pass didn't visit, so the RHS never got its +1; now a
  managed target's RHS is an owning position (retain a borrowed value / transfer an owned temp),
  exactly like managed `var` reassignment. **Backend** (`lowerLValueAssignment`): for a managed
  target, load the old value and `lyra_rc_release` it *before* storing the new (+1) one — and
  the new value is computed before the release, so a self-referential `xs[i] = xs[i] ++ y`
  (which reads the old element) is safe. **Memory accounting:** for a `[]string` / `shared
  [N]string` this is fully leak-free — the assignment frees the overwritten element and the
  array's drop glue frees the final ones (each element freed exactly once); for a `string` field
  of a *stack* struct (or a stack `[N]string`), the overwritten value is freed but the final one
  still leaks — the pre-existing stack-aggregate-managed-field leak, memory-safe (never a double
  free). Tests: `backend/llvm/llvm_managed_assign_test.go` (exec: `[]string` element, struct
  `string` field, self-referential concat; ASan: heap-string element overwrite + double-reassign
  of one element — no double-free/UAF); the array/member deferral tests dropped their
  now-working managed cases. **Deferred:** a `shared` struct in the assignment path.
- **`[]` empty array pattern (grammar) — the list-match base case.** `match xs { [] => …, [a,
  ...rest] => … }` previously failed to parse (the parser inserted a MISSING node for `[]` in
  pattern position). Fixed with a small grammar change: `array_pattern` uses `commaSep`
  (zero-or-more) instead of `commaSep1` (`tree-sitter-lyra/include/patterns/index.js`), plus an
  `[$.array_literal, $.array_pattern]` entry in the `conflicts:` array — `[]` is ambiguous
  between an empty array *literal* (expression) and *pattern*, so GLR keeps both alive until the
  surrounding position decides. The collector's `collectArrayPattern` no longer rejects a
  zero-element pattern (returns an empty `ArrayPattern`). **No backend change** —
  `lowerArrayPatternMatch` already treats `fixedCount=0, no rest` as a `len == 0` test, so `[]`
  matches a zero-length array for free. Tests: `tree-sitter-lyra` corpus (`Match empty array
  pattern`), `backend/llvm/llvm_match_array_test.go` (`TestExec_ArrayMatch_EmptyPattern`:
  `[]`→base, `[5]`→one-element, `[1,2]`→catch-all). Regenerated `parser.c` (363 corpus tests
  green). Note: this completes the array-match *pattern* surface except `[head, ...tail]`
  (blocked on the ownership-pass extension).
- **Mixed index+member interior assignment (`grid[i].y = v`, `p.arr[i] = v`, `m[i][j] = v`) —
  unified recursive lvalue.** Folded the array-index and struct-field assignment paths (both
  landed the same day) into **one** recursive `lvalueAddress` (`lvalue.go`) that walks any
  assignable path: an identifier root → its alloca; a `.field` hop → gep into the object's
  stack-struct storage; an `[i]` hop → gep to the array element (bounds-checked,
  negative-from-end, via `boundsCheckedIndex`) — a fixed-size array through the object's own
  storage, a `shared`/dynamic array through its box (loaded from the object's slot by
  `lvalueBoxPtr`, which avoids the ownership retain/release hooks since the write only mutates
  the box, taking no reference). Because `lvalueAddress` recurses on the *object* of each hop,
  index and member hops nest in any order and to any depth. `lowerLValueAssignment` collapses
  to: address the target, reject a managed target (deferred), lower + coerce + store. Tests:
  `backend/llvm/llvm_mixed_lvalue_test.go` (`grid[0].y` — field of an array element; `b.data[1]`
  — element of a struct's fixed array field; `v.items[1]` — element of a struct's *dynamic*
  array field, mutating the shared box; `m[0][1]` — a 2-D array); the earlier single-hop
  array/member tests still pass unchanged. **Deferred, loud errors:** a `shared` struct in the
  path, and a managed target type (needs release-old + retain-new). **Note:** a `[]T` field of a
  *stack* struct still leaks its box at scope exit (the pre-existing
  stack-aggregate-managed-field leak — memory-safe), unrelated to the assignment.
- **Struct-field assignment (`p.x = v`, nested `p.a.b = v`) lowers (backend).** Extends the
  `LValueAssignmentStmt` path (whose array-index case landed the same day) to a `MemberExpr`
  target. `lowerLValueAssignment` now dispatches to `lowerIndexAssignment` (arrays) or
  `lowerMemberAssignment` (structs), and a shared `storeLValue` does the value lower + coerce +
  store. `lowerMemberAssignment` uses a recursive **`lvalueAddress`** (`lvalue.go`) that walks
  an identifier root + `.field` hops, gep-ing down through the stack struct's storage to the
  target field's address, then stores in place (so a later read of the struct sees the
  mutation). Nested chains work because `lvalueAddress` recurses (`ln.start.x` → address of `ln`
  → gep `start` → gep `x`). The typechecker (`checkLValueAssignment`) already enforced the root
  binding is mutable (`var`/`let mut`) and the value matches the field type, and rejects a
  `readonly` field in the path — so the backend just computes the address and stores. Tests:
  `backend/llvm/llvm_member_assign_test.go` (single field, mutate-one-read-another, a nested
  `Line { start: Point }` chain, `let mut`; deferral errors); the array-assignment deferral test
  dropped its now-working `p.x = v` case. **Deferred, loud errors:** an array index *inside* a
  member path (`grid[i].y = v`, `p.arr[i] = v` — needs a unified recursive lvalue over both
  index and member hops), a `shared` struct/array in the path, and a *managed* field/element
  type (needs release-old + retain-new).
- **String indexing (`s[i]` → rune) lowers (backend).** The typechecker types `s[i]` as `rune`
  (any integer index, no compile-time bound); the backend had no string-index case. Now
  `lowerStringIndex` (`strings.go`, dispatched from `lowerIndexExpr` before the array cases)
  yields the i-th **rune**: since a string is UTF-8 and runes aren't randomly addressable, it
  walks from the front decoding one rune per step (reusing the `lyra_utf8_decode` shim the
  string for-in added) until the rune counter equals `i`, then yields that code point. **O(i)**
  — for a full traversal `for c in s` is the right tool; this is for occasional access. Running
  off the end before reaching `i` (which includes *any* negative index — the rune counter only
  grows, so there is no from-the-end form for a string, unlike an array) traps out-of-bounds via
  `lyra_panic_index_out_of_bounds`. Reading the string borrows it (no ownership action). Tests:
  `backend/llvm/llvm_string_index_test.go` (first/third/runtime rune; rune-indexing *past* a
  2-byte `café[3]='é'` and a 4-byte `a😀b[1]='😀'`, proving it's rune- not byte-indexed;
  out-of-bounds trap). **Deferred:** none for reads — this completes string/array element
  *access*; a from-the-end negative string index would need a full rune count first (not done).
- **Array element assignment (`xs[i] = v`) lowers (backend).** The front-end already
  type-checked interior-mutation statements (`checkLValueAssignment` — enforces the root binding
  is mutable: a `var`, `let mut`, or a `mut`/`own` parameter; checks the value against the
  element type); the backend had no `LValueAssignmentStmt` case. Now `lowerLValueAssignment`
  (`lvalue.go`) handles an `IndexExpr` target: it computes the element's address — a fixed-size
  array through its own alloca (`arrayLValue`), a `shared` array through its box payload
  (`sharedArrayPayloadPtr`), a dynamic array through its box payload — bounds-checks the index
  (negative-from-end, trapping via `lyra_panic_index_out_of_bounds`, through a new shared
  `boundsCheckedIndex` helper; a write target isn't marked by the value-range pass so the check
  is always emitted), and stores the (defensively width-coerced) value in place. Wired into
  `lowerBlockStmts`'s statement dispatch. Tests: `backend/llvm/llvm_lvalue_test.go` (exec:
  fixed-size constant/runtime/negative index, mutate-one-read-another, `shared` array, dynamic
  array, dynamic via a `mut []u8` parameter mutating the caller's box; bounds trap; deferral
  errors). **Deferred, loud errors:** a member target (`p.x = v` — struct-field write), a nested
  path (`grid[i].y = v`), and a *managed* element type (`[]string` — needs release-old +
  retain-new). **Note:** a static array is a value type, so mutating a *copy* (e.g. a by-value
  `[N]T` parameter) doesn't affect the caller — only a dynamic array (a shared box) or a
  `mut`/`own` reference propagates; the tests reflect that.
- **String for-in (`for c in s`) lowers (backend) — a rune walk, completing for-in.** A string
  iterable — the last for-in gap — now lowers to a UTF-8 rune walk (`lowerForInString`,
  `control_flow.go`): `bi = 0; while bi < byteLen { c = decode(data, bi); <body>; bi += n }`.
  Each iteration decodes one rune via a new runtime shim **`lyra_utf8_decode(i8* data, i64 pos,
  i32* cpOut) -> i64`** (`strings.go`, the inverse of the `lyra_rune_to_utf8` encoder — reads
  the lead byte's length class (1–4) and its continuation bytes, writes the code point, returns
  the byte count) and advances the byte index by that count, so a multibyte character is one
  iteration (not one per byte). Like the encoder it's unvalidated (rune's contract); well-formed
  UTF-8 — the only kind Lyra can build — never straddles the byte length, so the continuation
  reads stay in bounds. The byte-advance `n` is computed at the top of the body block, which
  dominates the continue/increment block, so `bi += n` is valid on both the fall-through and
  `continue` paths. The rune loop variable is a plain i32 (not a borrow — nothing to free).
  Tests: `backend/llvm/llvm_forin_string_test.go` (rune counts for ASCII, a 2-byte `café`→4, a
  4-byte `a😀b`→3; last-rune binding; a 2-byte rune decodes to the right code point (`'é'`);
  break); the for-in deferral test now covers only the two-variable-over-string form.
  **Deferred:** the two-variable form over a string (`for i, c in s` — the index/rune pairing
  isn't defined).
- **Range for-in (`for i in START..<END`) lowers (backend).** A numeric-range iterable —
  previously a loud error — now lowers to a counter loop (`lowerForInRange`, `control_flow.go`):
  `i = START; while i </<= END { <body>; i += step }`, reusing the C-style loop's break/continue
  machinery. `..<` is an exclusive end (`i < END`), `..=` inclusive (`i <= END`); an optional
  `:step` (grammar `START..<END:STEP`) sets the stride, default 1. The **counter is the loop
  variable** (a plain integer value, not a borrow, and immutable so never re-stored by the
  body). Its **width** is the first concrete-integer bound's type (End, then Start, then Step),
  else i64 — `rangeIntType`, mirroring the typechecker's `iterableElementType` so the counter
  matches the loop variable's declared type — and the bounds/step are `coerceIntWidth`'d to it
  (so `0..<n` with a u8 `n` runs at u8, an untyped `0..<3` at i64). The comparison predicate is
  signed/unsigned per the counter type. The increment is a **plain (wrapping) add**, so an
  inclusive `..=` whose end is the counter type's max loops forever (the increment wraps past
  it) — the one documented edge; no two-variable form over a range. Tests:
  `backend/llvm/llvm_forin_range_test.go` (exclusive/inclusive sums, variable end, typed-u8
  bounds, `:2` step, break, continue, and the `for i in 0..<xs.len() { sum += xs[i] }` indexing
  idiom); the for-in deferral test now covers only a **string** iterable (the last for-in gap).
  **Deferred:** a string for-in iterable in the backend.
- **Two-variable for-in (`for i, x in xs`) — index + element.** The backend previously errored
  on the two-variable form; `lowerForInLoop` now binds the loop counter as the index `i` (i64)
  in addition to the element `x`. The collector puts the first name in `Key` (the index) and the
  second in `Value` (the element) — the single-variable form leaves `Value` empty (`Key` is then
  the element) — so the backend resolves `elemVar`/`indexVar` from that and, each iteration,
  stores `arr[i]` into the element slot and the counter into a separate index slot (both borrows
  of the loop state, not framed). The typechecker already typed both (`bindForInLoopVars`: index
  → i64, value → element type). Tests: `backend/llvm/llvm_forin_test.go`
  (`TestExec_ForIn_TwoVar` — `i*x` over a dynamic array, `i+x` over a fixed array, and the index
  reaching the last position); the deferral test now covers only the still-deferred range/string
  iterable. **Deferred:** a non-array for-in iterable (range/string) in the backend.
- **`.len()` on arrays — a compiler-provided builtin.** `xs.len()` on any array (fixed-size or
  dynamic) returns its length as `i64` (signed so it composes with the negative-from-end index
  arithmetic). Registered in `typechecker/builtins.go`'s `builtinMethodSignature` for any array
  receiver (`types.IsArray`), consulted last like the other builtins so a user method shadows
  it; the backend `lowerArrayLen` (`dynarray.go`, dispatched from `lowerBuiltinMethodCall`)
  returns the compile-time size for a `[N]T` (lowering the receiver for effect in case it has
  one, e.g. `makeArray().len()`) and loads the box's `len` field for a `[]T`. Reading the length
  **borrows** the array (no reference consumed), so there's no ownership action on the receiver.
  Tests: `backend/llvm/llvm_len_test.go` (exec: dynamic / empty / fixed / `shared` fixed
  lengths, and the practical `for var i = 0; i < xs.len(); i += 1 { … xs[i] … }` index-loop
  idiom), `typechecker/tests/array_len_test.go` (acceptance on all array kinds + composing in
  arithmetic; `.len()` on a non-array errors). **Context:** chosen as a safe, self-contained
  pivot after finding the recursive-list idiom's `[head, ...tail]` blocked on an ownership-pass
  extension (see that item's Open note).
- **`match` on a dynamic array `[]T` lowers (backend) — first slice.** The front-end already
  type-checked array patterns (length, literal elements, bindings, rest, exhaustiveness); the
  backend previously errored on an array scrutinee. Now
  `lowerArrayMatch`/`lowerArrayPatternMatch` (`match_array.go`) lower it as an if-else ladder —
  the array analogue of `lowerScalarMatch` — where each `[...]` arm is a **length test** (`len
  == fixedCount`; a lone `[...rest]` matches any length) followed by per-element literal/range
  tests (reusing `scalarMatchTest`), first-match-wins into a merge phi. **In-bounds by
  construction:** the element tests/bindings run in a block reached only *after* the length test
  succeeded, so an `xs[i]` in a pattern is never an out-of-bounds read. **Bindings are
  borrows:** an element binding (`[a, b]`) and a whole-array `[...rest]` bind into `l.locals`
  but are *not* framed for release — reading an element or aliasing the whole array consumes no
  reference (the scrutinee's own binding owns the storage), the same borrow treatment as the
  for-in loop variable, so a `[]string` match is memory-safe. Tests:
  `backend/llvm/llvm_match_array_test.go` (exec: length dispatch `[a]`/`[a,b]`/catch-all,
  literal elements `[1,2]`/`[3,4]` incl. right-length-wrong-element fall-through, `[...rest]`
  binds-whole; `[]string` element read under ASan; `[head, ...tail]` deferral error).
  **Deferred, loud errors:** a `[head, ...tail]` pattern binding a *tail sub-array* (needs an
  alloc+copy), a rest not last, a nested non-scalar element pattern, and a fixed-size-`[N]T`
  scrutinee. **Found in passing (grammar gap, not fixed here):** an `[]` *empty* array pattern
  doesn't parse (the parser inserts a MISSING node) — so the recursive-list base case `[] => …`
  isn't expressible; it pairs with the deferred `[head, ...tail]` for the full recursive idiom.
- **`for x in <array>` iteration lowers (backend) — the first for-in codegen.** The backend
  previously had no `ForInLoopExpr` case (loud error); now `lowerForInLoop` (`control_flow.go`)
  lowers `for x in <array>` as an index-counter loop — `i = 0; while i < len { x = arr[i];
  <body>; i++ }` — over a fixed-size array (`[N]T`, stack or `shared`; length = the compile-time
  size, elements via `arrayLValue` / `sharedArrayPayloadPtr`) or a dynamic array (`[]T`; length
  = the box's runtime `len`, elements gep'd through the box payload), reusing the C-style loop's
  `loops` stack so break/continue resolve. The element source and length are materialized once
  before the loop (they dominate the body). **The loop variable borrows each element** — bound
  into `l.locals` but *not* framed for release: reading an element consumes no reference, and
  for a managed element type the array frees it when the array itself dies (matching the C-style
  loop, the ownership pass does not recurse into loop bodies; managed values *declared inside*
  the body are framed per iteration by the ordinary block machinery). **Typechecker
  prerequisite** (`typechecker_control_flow.go`): `checkForInLoopExpr` now types the loop
  variable from the iterable's element type (`bindForInLoopVars`/`iterableElementType` — an
  array's element, a string's `rune`, a range's numeric type) — before this the loop variable
  resolved in scope but had *no recorded type*, so a body use of it (`println(x)`, `sum += x`)
  couldn't lower. A range over *untyped* bounds keeps the loop variable untyped (assignable to
  any int width) rather than defaulting to i64, so `for i in 0..<3 { t: u8 = i }` still binds
  `i` to `t`'s width (fixing `TestAnalyze_ForInLoopVariableResolves`, which the naive i64
  default broke). Tests: `backend/llvm/llvm_forin_test.go` (exec: fixed / dynamic / `shared`
  array accumulate, break, continue, empty array; println-each order; `[]string` element read
  under ASan; two-var + range deferral errors). **Deferred, loud errors:** the two-variable
  index form (`for i, x in xs`) and a non-array iterable (range/string) in the backend.
  **Ergonomic note found in passing:** a trailing one-armed `if` in a loop body is rejected (the
  body's last statement is checked as a value) — pre-existing, shared with the C-style loop; not
  fixed here.
- **Managed-element dynamic arrays (`[]string`, `[][]T`) — the drop-glue extension of the `[]T`
  slice.** A `[]T` whose element type is itself managed (a `string`, a nested `[]T`, a `shared`
  value) previously errored loudly at construction (element drop glue deferred); now it lowers
  and frees its elements. **`dynArrayDropFn`** (`dynarray.go`) generates, once per element type
  and cached, the box's `drop_fn`: it receives the box payload (`box + rcHeaderSize` = `{ i64
  len, [0 x T] }`, per `lyra_rc_release`'s contract), loads `len`, and **loops** releasing each
  element via `emitDropValue` — the dynamic-length counterpart to a fixed `shared [N]T`'s
  *unrolled* `emitDropArray`. Routed via a new `DynamicArrayType` case in `boxDropFn` (null when
  the element owns nothing managed, so a `[]i64` still just frees its box). No ownership-pass
  change was needed: the `ArrayLiteralExpr` case already transfers each element's reference into
  the array, so the box owns them and the loop frees each exactly once. **Testing note:** a
  looped drop makes a static release-*site* count unable to express conservation (one call site
  runs `len` times), so the managed-element leak-side check is *structural* (assert the drop
  glue is generated, loops, and is passed as a non-null `drop_fn`) plus an ASan run for
  double-free/UAF — the unrolled `shared [N]T` case keeps its exact static count. Tests:
  `backend/llvm/llvm_dynarray_test.go` (exec: index a `[]string` element, nested `[][]i64`
  double-index; ASan: `[]string` of heap strings + `[][]i64`; IR: drop-glue loop + non-null
  drop_fn). **Deferred:** iteration, `match` on `[]T`, `.len()`, growth.
- **Dynamic arrays (`[]T`) — first backend slice (construction, indexing, ownership).** The
  front-end already type-checked `[]T` (annotate, build from a literal incl. empty, index,
  iterate, match, pass/return); this lands the codegen. **Representation** (`dynarray.go`,
  `DynArrayBoxType`): a `[]T` is a `ptr` to a ref-counted box `{ i64 rc, i64 len, [0 x T] }` — a
  *single box pointer*, chosen over a `{ data, len }` fat pointer precisely so it reuses the
  shared-value managed machinery **unchanged**: the value is a pointer, so `ownership.IsManaged`
  covers `[]T`, `managedBox` bitcasts it, and retain/release/last-use/drop act on it exactly
  like a `shared` value (no new managed-dispatch code). `lowerType` maps `[]T` → the box pointer
  *before* the `shared`-strip (a dynamic array is inherently heap-boxed regardless of flavor).
  **Construction** (`lowerDynArrayConstruction`): alloc a box sized `rcHeaderSize + 8 +
  N*stride`, store the length (field 1) and each element into the `[0 x T]` flexible tail (field
  2) — an empty `[]` still allocs a len-0 box, so every `[]T` is a uniform managed box (no null
  special case). **Indexing** (`lowerDynArrayIndex`): load the runtime `len`, apply the same
  negative-from-end + unsigned-`>=`-bound trap as a fixed array but against `len` (always
  checked — the value-range pass doesn't track dynamic lengths), then GEP+load. **By-value
  flow** through `let`/params/returns is the ordinary pointer path (`emitReturn`'s pointer
  case). **Typechecker fix** (the one real bug): a `[]T` **return-body / arg** literal was
  recorded as a `StaticArrayType` (lowered to an inline `[N x T]`, so `() -> []i64 => [1,2,3]`
  returned `[3 x i64]` from a box-pointer function → clang rejected it) — `propagateLiteralType`
  re-recorded only the *static* context, relying on `checkVarDecl` to record the dynamic type,
  which happens for a `let` but not a return/arg; now it re-records the *dynamic* case too (kept
  dynamic, never rewritten to static, so the dynamic→static assignment error is preserved).
  **Verified** under AddressSanitizer across construction, indexing, an aliased copy (retain, rc
  1→2→1→0, no double-free), and scope-exit free, with a static alloc+retains==releases
  conservation check. Tests: `backend/llvm/llvm_dynarray_test.go` (exec: construct +
  constant/runtime/negative index, pass-to-callee, return a `[]T`, empty array, move-copy;
  bounds trap; IR box `{ i64, i64, [0 x i8] }` + alloc/release balance; managed-element deferral
  error; two ASan cases). **Deferred, loud errors:** a *managed* element type (`[]string` — the
  box's drop glue must loop over `len` to release each element; errored at construction),
  iteration (`for x in xs`), `match` on `[]T`, `.len()`, and growth (no grow operation exists in
  the language — the one append-shaped syntax, `[...xs, v]` spread, isn't even type-checked).
  This is the second of the three parts of the arrays/reuse backend area (after `shared`
  arrays); Perceus stage 4 remains.
- **`shared` arrays (`shared [N]T`) lower end to end — the foundation slice of the arrays/reuse
  backend work.** A `shared`-flavored fixed-size array is now a heap-boxed, ref-counted value,
  reusing the whole `shared`-box machinery (alloc / retain / release / drop glue) rather than
  new plumbing. **Representation:** `lowerType` already mapped `shared [N]T` to a `ptr` to `{
  i64 rc, [N x T] }` (the generic `shared`-box path); this slice wires construction, indexing,
  and element drop. **Front-end (1 line + ordering):** `propagateAllocation` gained an
  `*ast.ArrayLiteralExpr` case so a `shared [N]T` annotation stamps the flavor onto the
  array-literal construction node (it runs *after* `propagateLiteralType`, which re-records the
  literal as a flavorless `StaticArrayType` — `checkVarDecl` already orders it that way, so the
  stamp survives). **Construction** (`lowerArrayLiteralExpr`, `arrays.go`): build the inline `[N
  x T]` as before, then `lowerBoxShared` it when the recorded type is `shared` — the exact
  mirror of `lowerStructInstanceExpr`. **Indexing** (`lowerIndexExpr` + new
  `sharedArrayPayloadPtr`): a `shared` array is a box pointer, so gep to the payload (`box →
  field 1`) and index through it — a constant index is a bare gep+load (typechecker
  range-checked it), a runtime/negative index keeps the from-the-end adjustment + bounds trap,
  all through the box; reading through the box *borrows* it (no reference consumed), so the
  binding's own release still fires at scope exit. **Drop glue** (`drop.go`):
  `needsDrop`/`emitDropValue` gained `StaticArrayType` cases (`emitDropArray` — N is constant,
  so element drops are unrolled via `extractvalue`), so a `shared [N]String` frees each element
  when the box dies. **Ownership** (`ownership.go`): added an `ArrayLiteralExpr` case to the
  `expr` classifier so array-literal elements are owning positions (transfer, mirroring
  tuples/structs) — *necessary*, since without it a managed element flowing into a `shared`
  array would be freed both by its own binding and by the box's drop glue (double free); a stack
  array's managed elements leak conservatively, matching tuples/structs. `IsManaged` already
  covered `AllocationOf == Shared`, so the pass frames the binding with no other change.
  **Verified** under AddressSanitizer with a static allocations==releases conservation check.
  Tests: `backend/llvm/llvm_shared_array_test.go` (exec: construct + constant/runtime/negative
  index, borrowed-param index in a callee, return a `shared` array, bounds trap; IR: box `{ i64,
  [3 x i8] }` + alloc/release balance; ASan: managed-element `shared [2]string` frees each
  element with 3==3 conservation), `typechecker/tests/shared_array_test.go` (acceptance in every
  position + the construction-flavor stamp). **Deferred:** dynamic arrays (`[]T`), `match` on a
  `shared` array (the element-wise array pattern isn't lowered), and Perceus stage 4 — the other
  two parts of this backend area.
- **`i128`/`u128` — the MVP slice (change-set steps 1 + 2b + 4 + 5), end to end.** The two
  128-bit integer types now lower and run: annotate a binding, do checked arithmetic, convert,
  `match`, and `print`. **Front-end (step 1):** `types.Int128`/`UInt128` added
  (`primitives.go`), and the by-hand width/signedness/numeric enumerations extended to include
  them — `types.IsNumeric`, `assignable.go`'s
  `numericPrimitiveByName`/`isAnyConcreteSignedInt`/`isAnyConcreteUnsignedInt` (so
  assignability, `numericResultType`, and `checkIntegerLiteralRange` treat them like any
  concrete int), the backend `layout.go` tables (`LLVMPrimitive` → `i128`, `IsSignedInt`,
  `IsNumericConversionTarget`, `primitiveSizeAndAlign` → 16/16), `purity.go`'s
  `isTypeConversionCall`, and the grammar (`number_types.js`: `i128`/`u128` added to
  `signed_/unsigned_integer_type`). **Literal storage (step 2b, MVP):** unchanged —
  `IntegerLiteralExpr.Value` stays `int64`, so a 128-bit value is reached via arithmetic or an
  `i128(x)`/`u128(x)` conversion of an i64/u64-range operand (`inferTypeConversion` now lets an
  unsigned literal in `(int64max, u64max]` target `u128` as well as `u64`); a true >64-bit
  literal is still unrepresentable (step 2a, open). **Backend arithmetic (step 4):** free by
  width — `add`/`sub`/`mul`/`icmp`, the `llvm.{s,u}{add,sub,mul}.with.overflow.i128` trap
  intrinsics, and `coerceIntWidth` all key off the operand bit width; division and floored `%%`
  lower to `udiv`/`sdiv`/`urem`/`srem i128`, which clang resolves against **compiler-rt**
  (`__divti3`…). **Correctness fix the plan didn't call out:** the `INT_MIN`/`INT_MAX` trap and
  saturating-mul-bound constants were built as `1 << (BitSize-1)`, which yields **0** for a
  128-bit width (the untyped `1` is Go's 64-bit `int`, so the shift falls off the end; it only
  worked at i64 by two's-complement wraparound). Introduced `intMinConst`/`intMaxConst`
  (`intconst.go`) that build the bound with `big.Int` — correct at every width — and rewired the
  three sites (`emitCheckedDivOp`'s `INT_MIN÷-1` guard, `lowerNegationExpr`'s `-INT_MIN` guard,
  `emitSaturatingMul`'s bounds). **Print (step 5):** there is no printf length modifier for 128
  bits, so `lyra_i128_to_str` (`print.go`, lazily defined like `lyra_rune_to_utf8`) formats by
  repeated `udiv`/`urem` by 10 — taking the magnitude as the *unsigned* bit pattern
  `select(isNeg, 0-val, val)` so it is correct even for `INT_MIN` (whose negation wraps to the
  right unsigned magnitude, 2^127) — and `formatForPrint` routes an i128/u128 value to it.
  **Range analysis:** unchanged and sound — `intBounds` returns `ok=false` for i128/u128 (their
  bounds don't fit int64), so they widen to ⊤ (untracked), the same conservative treatment
  `u64`'s upper half already had; a diagnostic can only be missed, never invented. Tests:
  `backend/llvm/llvm_i128_test.go` (exec: exit-code via i128→u8 narrowing, big multiply
  exceeding i64/u64 formats correctly, large negative, zero, u64-max via conversion, division
  via compiler-rt; IR shape), `typechecker/tests/i128_test.go`
  (annotation/arithmetic/conversions, `u128` rejects a negative, no implicit i64↔i128 widening),
  `tree-sitter-lyra` corpus (`assignments.txt` 128-bit annotations). **Still open:** step 2a
  (widen the literal node to `big.Int`/hi-lo so a >64-bit constant is writable) + step 3
  (128-bit compile-time folding in `typechecker/overflow.go`), both deferrable until a large
  literal constant is first needed.

### 07/26/26
- **for…in range widening — the last item in the value-range backlog, plus a latent for-in
  scope-bug fix.** A `for i in START..<END` (or `..=`) over a numeric range now binds its loop
  variable to the range interval in the body (`forInRangeKey` in `checker/range_analysis.go`),
  the for-in analogue of the C-style loop counter — but with no fixpoint, since the range gives
  the bounds directly. So `for i in 5..<10 { xs[i] }` on a size-3 array is a definite
  out-of-bounds (E022), `for i in 0..<3 { xs[i] }` elides its bounds check (`IndexInBounds`),
  and an inclusive `0..=2` bounds to `[0,2]`. **Sound via a provably-non-empty guard:** the
  variable is bound (enabling body diagnostics) only when `start.hi < end.lo` (`..<`) /
  `start.hi <= end.lo` (`..=`), so the loop definitely runs ≥ 1 iteration and a body diagnostic
  — which holds for the whole widened `[start.lo, end.hi]`, hence at the first iteration `i =
  start` — is genuinely definite, not a maybe-empty false positive. A variable-length range
  (`0..<n`, not provably non-empty), a stepped range, a two-variable form, or a non-range
  iterable (array/string, whose element/index we don't track) still havocs. **Prerequisite bug
  found + fixed:** the for-in **loop variable didn't resolve in the body** —
  `checkForInLoopExpr` (`typechecker_control_flow.go`) type-checked the body without entering
  the loop's scope (unlike `checkForLoopExpr`), so every use of the loop variable was an
  "undefined identifier"; no existing test exercised a non-empty for-in body, so the gap went
  unnoticed. Now it enters the scope. (The backend doesn't lower `for … in` yet, so the widening
  is a front-end diagnostic/elision feature — the `IndexInBounds` mark is computed and ready for
  when for-in codegen lands.) With this, the value-range analysis's original deferred list is
  fully cleared. Tests: `checker/range_analysis_test.go` (range index past-end E022, in-bounds +
  inclusive-range no-diagnostic, variable-length no-false-positive, `SafetyTable` marks a for-in
  range index in-bounds), `driver/driver_test.go` (for-in E022 through the pipeline; the loop
  variable now resolves).
- **Flow-sensitive `RangeConstraint` enforcement — the non-constant twin of the E023 constant
  check.** The value-range analysis now catches a *non-constant* value proven entirely outside a
  range-constrained newtype's range, extending the same `lyra-E023` from the typechecker's
  constant-only check to flow-proven variables: `if x > 100 { let p: Percent = x }` (x ∈
  [101,255] entirely outside [0,100]) and `let y = 150; let p: Percent = y` (constant-propagated
  through a binding the typechecker can't fold) are now errors (`checker/range_analysis.go`
  `checkConstraintViolation`, fired from the `VarDeclStmt` case). **How the target constraint
  reaches the range pass:** the typechecker stamps the resolved annotation type onto the
  assigned value node (`checkVarDecl` → `tt.Set(decl.Value, resolvedDeclType)`), so `tt.Get(x)`
  for `let p: Percent = x` recovers the `*ConstrainedType`; the check reads the
  `RangeConstraint` bounds from it (folding literal/negated-literal bounds, honoring
  `..<`/`..=`/open-ended, replicated from the typechecker's folder). **Scoped to an *identifier*
  value** — the typechecker's `checkRangeConstraints` folds and owns literal/constant values,
  and an identifier is exactly what it can't fold, so restricting to a variable both avoids a
  double report (verified: a literal `let p: Percent = 150` yields exactly one E023, the
  typechecker's) and captures the value-add (a variable refined by flow, or bound to a
  constant). Definite-only, zero-false-positive like every other range diagnostic — a value that
  *might* be out of range (a full-range param) is left to the runtime. Only the annotated-`let`
  site is covered (that's where the target type is stamped; a plain reassignment's non-constant
  case isn't, and is noted). Tests: `checker/range_analysis_test.go` (refined-var violation
  showing `[101, 255]`, constant-propagated-binding violation, refined-in-range +
  possible-not-definite no-diagnostic), `driver/driver_test.go` (flow-sensitive E023 through the
  pipeline; a literal violation reported exactly once).
- **`RangeConstraint` enforcement on `newtype … where range(…)` — the constant-value case
  (`lyra-E023`).** A range-constrained newtype's constraint was collected by the front-end but
  *never checked* against actual values — `let p: Percent = 150` for `newtype Percent = u8 where
  range(0..=100)` compiled clean. Now a compile-time numeric constant assigned or annotated to a
  range-constrained newtype is checked against the declared range: `checkRangeConstraints`
  (`typechecker/range_constraint.go`), the numeric analogue of the existing string
  `checkPatternConstraints`, wired into the three value sites (`checkVarDecl`,
  `checkVarReassignment`, member-assign). It folds a constant value (int or float literal, incl.
  a negated one / a folded arithmetic constant) and the constraint's literal/negated-literal
  bounds, honoring inclusive start, `..<` exclusive vs `..=` inclusive end, and open-ended
  bounds (`0..`, `..=100`); an unfoldable identifier/compound bound leaves that side unenforced
  (conservative — no false positive). Covers both integer and float base types (`newtype Ratio =
  f64 where range(0..=1)`). **No double report:** `checkIntegerLiteralRange` skips a
  `*ConstrainedType` (it matches only a bare `PrimitiveType`), and a range constraint is
  normally ⊆ the base type, so a range violation subsumes any base overflow. Definite-only, like
  the literal integer range check — a *non-constant* value (`let f = (x: u8) -> Percent => x`)
  is left to the runtime / a future flow-sensitive pass. New code `lyra-E023`. Tests:
  `typechecker/tests/constrained_type_test.go` (int above/below, exclusive vs inclusive
  boundary, open lower/upper bound, negative start, float over/in-range, non-constant no-error,
  reassignment), `driver/driver_test.go` (E023 through the pipeline).
- **Per-match-arm scrutinee refinement — the `match` analogue of `if`-branch refinement.** A
  `match` on a tracked integer *variable* now refines the scrutinee, per arm, to the values that
  arm's pattern matches — a literal (`0 => …` → `[0,0]`) or a numeric range (`1..=10`, `0..<3`),
  extracted by `patternInterval` (mirroring the typechecker's exhaustiveness reader
  `armIntInterval`, so the two agree on what a pattern covers; guards are irrelevant to the
  refinement). `evalMatch` intersects that with the scrutinee's current interval in the arm's
  env (`refineScrutinee`); an empty intersection makes the arm **unreachable** (skipped — no
  value, env, or diagnostics), and a catch-all / identifier / non-numeric pattern refines
  nothing (the arm sees the full range — sound). This lets every range-analysis conclusion fire
  inside a constraining arm: `match x { 100..=127 => x + 100 }` on an i8 → E020, `match x { 0 =>
  a / x }` (scrutinee *is* the divisor) → E021, `match i { 5..=10 => xs[i] }` on a size-3 array
  → E022, and an in-range arm elides its checks (`match i { 0..=2 => xs[i] }` →
  `IndexInBounds`). The post-match env still unions with the pre-match state, so the scrutinee's
  range after the match is unchanged (sound). No new false positives (an in-range arm and a
  catch-all arm both stay clean); full suite green. Tests: `checker/range_analysis_test.go`
  (arm-range overflow E020, arm-literal divide-by-zero E021, arm-range + exclusive-range OOB
  E022, in-range/catch-all no-diagnostic, `SafetyTable` marks a match-refined in-bounds index),
  `backend/llvm/llvm_elision_test.go` (`TestExec_MatchRefinementElisionPreservesResults` — a
  match-refined elided index reads the right element).
- **u64 tracking — the last untracked integer type, now tracked with a +∞ upper sentinel.**
  `u64` was the sole integer type the value-range analysis skipped, because its true max
  (2^64-1) overflows the `int64` the interval bounds are stored in. Rather than a full dual
  `uint64` domain (a large refactor), `intBounds(UInt64)` now returns `[0, posInf]` where
  `posInf = math.MaxInt64` is a **+∞ sentinel**: a sound over-approximation of the true `[0,
  2^64-1]`. **Why this is sound** (the load-bearing argument): the *exact* lower bound of 0
  drives every u64 diagnostic — `x < 0` → always-false W011, a refined `x >= size` index → E022,
  a subtraction proven below 0 → E020 underflow — while the fake upper is only ever consumed in
  ways that stay conservative (the interval arithmetic `addI`/`subI`/`mulI` are
  int64-overflow-guarded, so any u64 op that could reach the real upper half overflows the int64
  computation → untracked; elision's "entirely within" test then can't fire for it). The one
  place the fake upper *would* be unsound is `compareConst` (`x > MaxInt64` on a u64 is
  genuinely satisfiable and must not fold to always-false), so it gained **sentinel guards** — a
  fold never treats a ±∞ bound as a finite separator
  (`upperFin`/`lowerFin`/`finiteSingle`/`disjoint`); this changes nothing for i8..u32 (their
  bounds are never the sentinels) and only suppresses folds keyed off a u64 +∞ or an i64 extreme
  (sound — it can only *remove* a warning). Diagnostics print a sentinel as `+∞`/`-∞`
  (`fmtBound`) rather than the misleading `9223372036854775807`. Empirically grounded first
  (probed the four diagnostic kinds on u64 before/after). ASan-clean, full suite green — the
  behavioral tests confirm no wrong elision (a full-range `u64` `a - b` given `5 - 10` still
  traps; a proven-safe `100 - 58` elides and yields 42). **Not done** (a genuine dual `uint64`
  domain would add): precise tracking of u64 values in the upper half `(2^63, 2^64-1]`, and
  near-2^64 overflow *detection* (unreachable with int64 arithmetic — the interesting overflow
  cases self-untrack). Tests: `checker/range_analysis_test.go` (u64 `>= 0`/`< 0` const
  comparisons, refined-index E022 showing `+∞`, underflow E020, the `x > MaxInt64` soundness
  non-warning, full-range-add no-FP, u64 div-by-zero), `backend/llvm/llvm_elision_test.go`
  (`TestExec_U64_ElisionSoundness`).
- **Definite out-of-bounds diagnostic (`lyra-E022`) — the range analysis's error-reporting twin
  of the array-bounds elision.** Completes the diagnostic/elision symmetry across all three
  range facts (overflow E020, div-by-zero E021, bounds E022). An `xs[i]` whose index range is
  proven *entirely outside* `[-size, size)` (a negative index counts from the end) on a
  reachable path is a guaranteed runtime bounds trap, now caught at compile time (`evalIndex` in
  `checker/range_analysis.go`, folded into the same index handling that marks `IndexInBounds`
  for elision). **Scoped to a *non-singleton* index range** (`if i >= size { xs[i] }`, an
  arithmetic/loop-derived range) — this is the key difference from E021's *identifier* scoping:
  the typechecker's own constant-index check (`inferIndexExpr` via `resolveConstantInt`)
  resolves an index to a *single* constant and **looks through let-bindings** (`let i = 5;
  xs[i]` is already its error, unlike the div case), so restricting E022 to a non-singleton
  range provably means that check didn't fire (a resolvable constant always yields a singleton
  `[k,k]`) — no double report, while still catching the flow-proven range case constant-folding
  can't see. Same definite-only, zero-false-positive bias as E020/E021 (a merely *possible* OOB
  — a full-range index — is left to the runtime trap; an unreachable branch reports nothing). No
  existing test needed changing: every runtime-OOB-trap test already passes the bad index
  through a full-range parameter (which intersects the valid range, so E022 correctly doesn't
  fire). Tests: `checker/range_analysis_test.go` (E022 via positive `i >= size` and negative `i
  < -size` refinement; none for a possible/refined-in-bounds index), `driver/driver_test.go`
  (E022 flows through the pipeline; a constant `xs[5]` is reported once, by the typechecker,
  with no duplicate E022).
- **Definite divide-by-zero diagnostic (`lyra-E021`) — the range analysis's error-reporting twin
  of the divide-by-zero elision.** Symmetric with `lyra-E020` (definite overflow): a
  `/`/`%`/`%%` whose divisor is proven *always zero* on a reachable path is a guaranteed runtime
  trap, now caught at compile time (`checkDivision` in `checker/range_analysis.go`, folded into
  the same div handling that marks elision safety). **Scoped to an *identifier* divisor proven
  `[0,0]`** — a literal/folded-constant zero (`5 / 0`, `10 / (5-5)`) is already the
  typechecker's own constant-fold check, so restricting E021 to a variable both avoids a double
  report *and* captures exactly this pass's value-add: the *non-constant* divisor proven zero by
  flow (`let b = 0; a / b`, or the flagship `if b == 0 { a / b }` via branch refinement — which
  constant-folding can't see). Same definite-only, zero-false-positive bias as E020 (a merely
  *possible* zero divisor is left to the runtime trap; an unreachable branch reports nothing).
  Float division is untracked (not an integer trap). Error, not warning — a definite
  divide-by-zero is as fatal as a definite overflow. The four constant-zero-divisor runtime-trap
  cases in `llvm_checked_div_test.go` were switched to param divisors (a constant zero is now a
  compile error, so exercising the *runtime* trap needs a runtime-opaque divisor — the same fix
  the elision IR test took). Tests: `checker/range_analysis_test.go` (E021 for a
  constant-assigned var + a refinement; none for a possible/nonzero/refined-nonzero divisor),
  `driver/driver_test.go` (E021 flows through the pipeline; a literal `5/0` is reported once, by
  the typechecker, with no duplicate E021). Deferred: the symmetric definite-OOB *bounds*
  diagnostic (the twin of bounds elision), not yet built.
- **Divide-by-zero / array-bounds trap elision — the range analysis's second optimizer slice.**
  After overflow-trap elision (07/25) the same `SafetyTable` channel now removes two more
  provably-dead runtime traps. The pass gained two facts alongside `NoOverflow`:
  **`NoDivZero`/`NoDivOverflow`** for a `/`/`%`/`%%` (via `markDivSafety`, wired into the
  `MathBinaryOpExpr`/`MathAssignOpExpr` div cases) — nonzero when the divisor interval excludes
  0, non-overflowing when the dividend can't be the type min *or* the divisor can't be -1
  (unsigned division is always non-overflowing); and **`IndexInBounds`** for `xs[i]` (via a new
  `evalIndex` case) — marked when the index interval is provably within `[0, size)`, which the
  loop-widening fixpoint proves for a counter (`for i = 0; i < size; i += 1`) and branch
  refinement proves for a guarded param (`if i < len { xs[i] }`). The `SafetyTable` grew from
  one map to four, each with a nil-safe accessor. **Backend:** `applyIntMathOp` now threads the
  AST node to `emitCheckedDivOp`, which skips the divide-by-zero `icmp`+trap when `NoDivZero`
  and the signed `INT_MIN/-1` `icmp`+trap when `NoDivOverflow`; `lowerIndexExpr` skips the
  negative-from-end `select` adjustment *and* the unsigned-`>=`-size bounds trap when
  `IndexInBounds` (a proven index is non-negative and in range, so a bare `getelementptr`+`load`
  is correct). **Sound by construction** (same argument as overflow elision): only *proven*-safe
  ops are in the table, absence keeps the trap, a nil table reports false — so a real
  divide-by-zero / OOB is never elided (verified: full ASan suite green; a param-fed `d(84,0)`
  and `get(arr,5)` still trap with exit 101). `TestEmit_CheckedDivisionIR` was switched to
  param-passed operands (a constant divisor is now elided, so the checked shape it pins needed a
  runtime-opaque divisor — same fix the overflow IR test took). Deferred: a range-based
  div-by-zero *diagnostic* (the error-reporting twin, symmetric with E020). Tests:
  `checker/range_analysis_test.go` (`NoDivZero`/`NoDivOverflow`/`IndexInBounds` marked for
  provable ops incl. a loop-counter index, not for full-range params),
  `backend/llvm/llvm_elision_test.go` (elided vs kept IR for div + bounds, results preserved,
  real div-by-zero / OOB still trap).
- **Precise loop widening — the value-range analysis tracks loop counters instead of havoc'ing
  them.** A C-style `for` is now analyzed with a textbook **widening/narrowing fixpoint**
  (`evalForLoop`) rather than dropping every loop-assigned variable to ⊤. The counter is tracked
  **precisely on both sides** — the init gives the lower bound, the loop guard the upper — so
  `for var i: u8 = 200; i < 250; i += 1 { … i + 100 … }` now catches the definite overflow (i ∈
  [200,249] → i+100 ∈ [300,349]) that havoc missed (with i ∈ [0,255] it merely straddled), a
  comparison on the counter can be proven constant, and **counter arithmetic inside the loop
  elides its overflow trap** (`i + 1` on i ∈ [0,2] is provably safe). **How:** the body is
  analyzed *silently* (a new `rangeChecker.silent` flag gates `report` + safe-marking) to
  compute the loop-head invariant H — widen to a fixpoint (each unstable bound jumps to ±∞ so an
  unbounded/million-iteration counter converges in a handful of steps, not N), then narrow
  (replace ∞ bounds with the finite values the guard now implies, monotone → terminates) — then
  **once loudly** with H so diagnostics fire once and elision keys off the precise ranges. Both
  phases capped at `maxFixpointIters` as a safety net. **Sound because H over-approximates:** a
  wider interval only makes E020/elision fire *less* often, never wrongly (verified: the full
  suite — many loop programs — stays green, no false positives; a 1,000,000-iteration loop
  analyzes in ~9ms). An **accumulator** with no bounding guard still widens to ⊤ (correctly — it
  genuinely can grow); the **after-loop** state havocs the loop-assigned vars (sound under
  `break`, which carries a mid-body state H needn't cover); a **`for … in`** loop (no
  counter/guard to narrow) still havocs. Nested loops each run their own fixpoint. Tests:
  `checker/range_analysis_test.go` (counter overflow both-sided, up/down counters make a
  comparison constant, accumulator no-false-positive, large-bound termination, nested),
  `backend/llvm/llvm_elision_test.go` (in-loop counter arithmetic elided + result preserved).

### 07/25/26
- **Overflow trap elision — the range analysis's optimizer half.** With the diagnostics slice
  validated, the same engine now *removes* provably-unnecessary runtime overflow traps. The pass
  returns a **`checker.SafetyTable`** — the set of `+`/`-`/`*` (and `+=`-style) ops whose
  operand ranges prove the result fits its type on every path — populated in `checkArith`'s
  "result entirely within the type" case (the third branch, between definite-overflow and
  possible-overflow). The driver stores it on `Result.RangeSafety`; the backend's
  `applyIntMathOp` consults `NoOverflow(e)` and emits the **plain** instruction (via the
  existing `emitWrappingOp`, the `wrapping_add` codegen) instead of
  `llvm.{s,u}{add,sub,mul}.with.overflow` + trap. Keyed by the AST expression node — the same
  object both passes walk (one `*ast.Program`), so the lookup is a pointer match. **Sound by
  construction:** membership is conservative (only a *proven*-safe op is present; anything
  uncertain is absent and keeps its trap), the interval is an over-approximation (if even the
  widened range fits, the real value can't overflow), and a nil table reports false — so a wrong
  entry, the only thing that could turn a real overflow into a silent miscompile, never occurs.
  Elision fires for constant/refined-range operands (`let a: u8 = 5; let b: u8 = 3; a + b` →
  plain `add i8`) and correctly does **not** for full-range ones (they keep the trap and still
  fire it at runtime). One IR test (`TestEmit_CheckedArithmeticIR`) was switched to param-passed
  operands so the checked shape it asserts is actually emitted (the constant version it used is
  now elided). Div/rem and array-bounds elision are the next slice (same channel, different
  facts). Tests: `backend/llvm/llvm_elision_test.go` (elided vs kept IR, results preserved, real
  overflow still traps), `checker/range_analysis_test.go` (`SafetyTable` marks a provable add,
  not an unprovable one).
- **Collector miscompile fixed: a trailing comment on a block's final expression dropped that
  expression.** Found while testing the value-range pass (an overflow went undetected only when
  the line had a trailing `// comment`). `CollectBlockExpr` appended `CollectStatement(child)`
  for every *named* CST child, and a `comment` is named but collects to **nil** — so a nil
  statement landed at the end of the block. The block's value is its final statement, so the
  value became that nil: the backend returned garbage (`a + b // c` in a `-> u8` body exited 1,
  not the sum) and the typechecker mis-typed the block. A comment-only body (`() => { // do
  stuff }`) likewise produced a block of nils. Fix: `CollectBlockExpr` now skips nil/typed-nil
  results, mirroring the top-level program collector's existing guard (`isNilStmt`). Six
  collector goldens that had baked-in `nil` block statements were regenerated (comment-only
  blocks now correctly collect to empty). Pre-existing and orthogonal to range analysis, but a
  genuine correctness bug. Tests: `backend/llvm/llvm_comment_test.go` (a trailing comment on a
  block value returns the right result), `collector/tests/block_comment_test.go` (no nil
  statement leaks into the block).
- **Value-range (interval) analysis — a flow-sensitive front-end pass
  (`checker/range_analysis.go`).** The engine tracks each integer variable's interval `[lo, hi]`
  at every program point and reports two things the literal-only checks can't: **`lyra-E020`
  (error)** a *definite* integer overflow — an `+`/`-`/`*`/unary-`-` whose operand ranges prove
  the result can't fit its type on any path — and **`lyra-W011` (warning)** a *constant
  comparison* (always true/false). The value-add over `checkIntegerLiteralRange` is **flow
  sensitivity**: branch refinement narrows a variable inside a branch (`if x > 100 { x + 100 }`
  on an i8 → x∈[101,127], x+100∈[201,227] → definite overflow), which constant-folding can't
  see. **Product chosen: diagnostics first** (user pick, over the check-elision optimizer) —
  pure front-end, so the engine is validated as a diagnostic before it's ever trusted to
  *remove* a runtime safety check (where an unsound narrowing would be a miscompile).
  **Soundness bias — zero false positives:** anything not precisely trackable widens to ⊤ (the
  type's full range), which can only miss a diagnostic, never invent one — an absent variable is
  ⊤; int64-overflowing interval math is ⊤ (so i8..u32 are precise, i64 mostly ⊤; u64 untracked,
  2^63 doesn't fit int64); a loop **havocs** every variable it assigns (a var modified across
  0..N iterations can be anything — sound, imprecise); a contradictory branch refinement marks
  that branch unreachable (its diagnostics suppressed); blocks restore shadowed/locally-declared
  names on exit. Interval arithmetic (`addI`/`subI`/`mulI`/`negI`) is overflow-guarded in int64;
  branch refinement handles `<`/`<=`/`>`/`>=`/`==`/`!=` against a pure constant side (variable
  on either operand), `&&` (then-branch) and `||` (else-branch), and `!`. A *possible* overflow
  (`a + b` on two full-range i8s) is deliberately left to the runtime trap. Wired into the
  driver after typechecking (needs the TypeTable for widths/signedness). **Found in passing:**
  three backend tests wrote *statically-provable* overflow (`let a: u8 = 200; let b: u8 = 100; a + b`) to exercise the runtime trap — now correctly caught at compile time by E020 — so their
  operands were routed through function parameters (runtime-opaque, so the runtime trap still
  fires on the same values). **Deferred:** trap elision (the optimizer half), precise loop
  widening, per-match-arm refinement, range div-by-zero, and `RangeConstraint` enforcement.
  Tests: `checker/range_analysis_test.go` (overflow via
  refinement/const-propagation/sub/mul/compound-assign; no-false-positive on
  possible-overflow/refined-safe/i64/loop-counter; always-true/false via type-bounds and
  refinement; no-false-warning on genuine variables and loop conditions),
  `driver/driver_test.go` (pipeline wiring).
- **Fixed-size array lowering (backend) — construction, indexing, bounds checks.** The biggest
  backend gap: the front-end already type-checked arrays
  (`inferArrayLiteralType`/`inferIndexExpr`, layout via `SizeAndAlign`/`resolveForLayout`), but
  `lowerExpr` had no `ArrayLiteralExpr`/`IndexExpr` case (errored `not implemented for
  *ast.ArrayLiteralExpr`). Now `[N]T` lowers end to end (`arrays.go`): a literal builds an `[N x
  T]` aggregate (undef + `insertvalue`, elements coerced to the element type; `lowerType` gained
  the `StaticArrayType` case), and `xs[i]` reads an element — a **constant** index is a bare
  `extractvalue` (the typechecker already range-checked it, no runtime guard), a **runtime**
  index is bounds-checked (`lowerIndexExpr`: widen the index to i64 by signedness, then a
  `getelementptr`+`load` guarded by a new `lyra_panic_index_out_of_bounds` trap). **Negative
  indices count from the end** (Python-style, added on request): `i < 0` → `i + size` via a
  `select`, so `-1` is the last element and `-size` the first; the single unsigned `>= size`
  compare on the *adjusted* value catches both `i >= size` and `i < -size` (an out-of-range
  negative stays negative → large unsigned). A constant index (positive or negative) is
  range-checked against `[-size, size)` at compile time — `resolveConstantInt` now folds a
  `NegationExpr`, and the typechecker's range check widened accordingly (`inferIndexExpr`). A
  local/param array is indexed through its own alloca (no copy, `arrayLValue`); any other array
  value is materialized into a temp. Arrays flow through `let`/params/args and **returns**
  (`emitReturn` gained an `ArrayType` by-value case). **Width fix found in passing:**
  `propagateLiteralType` had no `ArrayLiteralExpr` case, so an annotated narrow array's element
  leaves stayed i64 — a `let` tolerated it (checkVarDecl sets the node type +
  `coerceAggregateElem` fixes each element), but a function return (which fixes the type from
  the *signature*) miscompiled (`() -> [3]u8 => [4,5,6]` built `[3 x i64]` and `ret`'d it from a
  `[3 x i8]` function). Added the case: narrow the leaves and, **static context only**,
  re-record the literal as a concrete `[N x elem]` (a `[]T` dynamic annotation must keep its
  `DynamicArrayType`, or a later dynamic→static assignment error is masked — that regression
  caught two existing tests). **Deferred, loud errors:** dynamic arrays (`[]T`), string indexing
  (`s[i]`), element assignment (`xs[i] = v`). The `cmd/lyrac` backend-error fixture was
  repointed (array literal → a `newtype` decl, still unsupported). Tests:
  `backend/llvm/llvm_array_test.go` (exec: const/runtime index, param/arg/return,
  i32/bool/no-annotation elements; bounds trap on past-the-end + negative; IR:
  insertvalue/extractvalue, runtime bounds trap defined once, array-return signature),
  `typechecker/tests/array_literal_test.go` (annotated element narrows to u8; dynamic annotation
  stays dynamic).
- **`weak` type — from grammar-only crash to a usable type.** The grammar had parsed `weak T`
  (`weak_type`, e.g. `parent: weak Node`) for a while, but the collector's `parseType` had no
  case for it, so it hard-errored `unknown type node kind: weak_type` — `weak` was unusable in
  any program. Now a real type end to end: **`types.WeakType{Inner}`** (a non-owning reference,
  `pointer.go`, next to `RawPointerType`; nil-safe `String()` → `weak <inner>`), collected by
  `parseWeakType` off the `inner_type` field. **E014** (`collectByValueNames`) treats a `weak`
  field as pointer-indirection, so it breaks a recursive *size* cycle exactly like `shared`
  (`struct Node { parent: weak Node }` and `data List = Nil | Cons(i64, weak List)` are now
  well-formed). **Typechecker** `resolveType`/`resolveTypeIfKnown` resolve the inner (so `weak
  Node` isn't left an UnresolvedType); `TypesEqual` compares two weaks by inner. **Backend**
  lowers `weak T` to an opaque `i8*` (`lowerType`) and sizes it pointer-sized (`SizeAndAlign`),
  so a `weak`-broken recursive type declares and *builds* (`%Node = { i64, i8* }`). Crucially
  weak is **non-managed**: `AllocationOf(WeakType)` is Unspecified (the default), so the
  ownership pass and per-type drop glue never retain/release/drop it — the whole point of a weak
  reference, and the reason it can't double-free. **Deferred (and unconstructible today):** the
  non-owning runtime semantics — a separate weak count + upgrade-to-strong — which is what
  actually breaks refcount *cycle leaks* (ALLOCATION.md); no surface syntax creates a `weak`
  value yet, so the concrete pointee representation is intentionally left unspecified. Tests:
  `checker/recursive_type_test.go` (weak field/param/mutual-recursion break the cycle),
  `collector/tests/weak_type_test.go` (collects to WeakType, named + parameterized inner),
  `driver/driver_test.go` (`TestAnalyze_WeakType_TypeChecks` — full pipeline clean),
  `backend/llvm/llvm_weak_test.go` (pointer field IR + build/run).

### 07/24/26
- **String interpolation (`"… ${expr} …"`) end to end** — collector, typechecker, backend.
  Unblocked now that per-type value→string formatting exists (07/24 print work). **Backend**
  (`lowerInterpolatedString`, strings.go): the N-segment generalization of `++` — each segment
  is formatted to bytes by the same `formatForPrint` machinery print uses (literal chunk = a
  string; an interpolated int/float/bool/rune/string rendered per type), then concatenated into
  one fresh ref-counted box, so the result is an owned heap string like `++`. The ownership pass
  already modeled `InterpolatedStringExpr` as an owned producer whose segments are borrowed, so
  it's freed with no new pass work (IR test: exactly 1 alloc / 1 release; ASan-clean).
  **Typechecker** (`inferInterpolatedStringExpr`): each segment is now type-checked as a
  printable scalar (the print set) and an untyped numeric-literal segment is settled to its
  default width; the whole expression is `string`. This is a real check — an undefined name or a
  non-printable aggregate inside `${…}` is now an error (segments were previously unchecked;
  three `string_concat` tests that interpolated undeclared names were updated to declare them).
  **Collector whitespace fix (the surprise):** a `string_content` chunk's *leading* whitespace
  was silently dropped — tree-sitter, with `/\s/` in `extras`, strips it as token padding, so
  `"a ${x} b"` lost the space before `b`, and a plain `"  x"` lost its leading spaces, and `" "`
  collected as **empty**. Confirmed via an instrumented scanner (the scanner *is* called at the
  space and consumes it, but the node's start byte is advanced past it) — not fixable from the
  scanner (a `mark_end`-at-top JS-template-style rewrite didn't help). Fixed in the collector
  (`expressions/string_literal.go`) by reconstructing each literal chunk from the **raw source
  between** interpolation nodes (their byte ranges are exact and start at `$`), which also fixed
  the latent plain-string bug. Two collector goldens gained the previously-dropped space chunk
  (`"${prefix} ${name}"` now has a `" "` segment; `show(x) ++ " " ++ show(x)`'s `" "` is no
  longer empty); golden comparison normalizes whitespace, so the fix is guarded by exact-value
  unit tests instead (`string_whitespace_test.go`). The backend-error CLI fixture (`cmd/lyrac`,
  interp.lyra) was repointed to an array literal (still unsupported); `TestEmit_StringDeferred`
  dropped its interpolation case. Tests: `typechecker/tests/interpolation_test.go`,
  `backend/llvm/llvm_interpolation_test.go` (exec across all scalar kinds + whitespace +
  adjacency + `++` composition, IR alloc/release balance, ASan),
  `collector/tests/string_whitespace_test.go`.
- **Checked division + negation — the second checked-arithmetic slice.** After `+`/`-`/`*` (same
  day), the remaining integer operations LLVM leaves as undefined behavior are now trapped:
  `/`/`%`/`%%` guard the divisor against **zero** (both signs → a new
  `lyra_panic_divide_by_zero` trap) and, when signed, against the one overflowing division
  **`INT_MIN / -1`** (→ the overflow trap; `srem` is UB on the same inputs); and unary `-`
  guards against **`-INT_MIN`**. `trap.go` was generalized — a `panics` map +
  `panicFunc(name,msg)` (two messages now) and a shared `emitTrapIf(block, cond, fn) → cont`
  helper that all the checks (including the refactored `+`/`-`/`*` path) use. **Negation
  subtlety:** the check fires only for a *non-literal* operand — `-9223372036854775808` (and
  every narrow min) is the canonical way to *write* INT_MIN, lowers to `sub 0, INT_MIN_bits ==
  INT_MIN`, and is already range-checked by the typechecker, so trapping it would make INT_MIN
  unwritable; a runtime `-x` on a variable holding INT_MIN still traps. **Found in passing:** a
  narrow signed-min *literal* (`let x: i8 = -128`) lowered at i64 width (propagateLiteralType
  checked the positive magnitude 128 against i8's max 127, didn't fit, bailed to i64) — latent
  for a plain read but broke typed arithmetic against a real i8; **fixed same day** (see the
  narrow-signed-min entry below). Tests: `llvm_checked_div_test.go` (div-by-zero across
  `/`/`%`/`%%` + signed; `INT_MIN/-1` and `INT_MIN%-1`; `-INT_MIN`; non-trapping
  division/negation stay transparent incl. `INT_MIN / 1`; IR: unsigned `/` gets only the zero
  check, signed gets both, negation guards, trap defined once).
- **Wrapping / saturating integer methods lowered (`wrapping.go`) — the escape hatches from
  checked arithmetic.** `x.wrapping_{add,sub,mul}(y)` and `x.saturating_{add,sub,mul}(y)`
  type-checked since 07/10 but weren't lowered; now they are, dispatched from
  `lowerBuiltinMethodCall` (the same `MemberExpr`-callee path as float rounding). **wrapping** =
  LLVM's raw `add`/`sub`/`mul` (modular two's-complement — the exact op plain `+`/`-`/`*` used
  before the overflow trap wrapped them). **saturating add/sub** = the
  `llvm.{s,u}{add,sub}.sat.iN` intrinsics (signedness from the receiver). **saturating mul** has
  no native intrinsic (LLVM only has fixed-point `.fix.sat`), so it's composed: a
  `{s,u}mul.with.overflow` multiply, then on overflow a `select` to the saturation bound —
  unsigned → max (all ones); signed → min/max chosen by the product's sign (`(a<0) ^ (b<0)`).
  The argument is coerced to the receiver's width defensively. Tests: `llvm_wrapping_test.go`
  (wrap wraps, saturate clamps in both directions and both signs incl. mixed-sign mul → min and
  same-sign mul → max, the escape hatch never traps where plain `+` would, and IR: wrapping is a
  raw op with no `with.overflow`/trap, saturating add/sub uses `.sat`).
- **Checked arithmetic by default — integer `+`/`-`/`*` trap on overflow (Pit-of-Success #2).**
  The language's defining safety promise, unblocked once the backend had integer arithmetic +
  `print`/`println` (for the trap message). `applyIntMathOp` now lowers `+`/`-`/`*` to the
  matching `llvm.{s,u}{add,sub,mul}.with.overflow.iN` intrinsic (signedness from the operand,
  width per type), extracts `{result, overflow}`, and cond-branches overflow → a trap block; the
  fall-through carries the result (so `applyIntMathOp` returns the continuation block, threaded
  through `lowerMathBinaryOpExpr` and `lowerMathAssignOp` — `i += x` is checked too). The trap
  is one per-module noreturn `lyra_panic_overflow` (`trap.go`, emitted lazily like the rc
  runtime): it `write(2, …)`s "lyra: arithmetic overflow" to stderr and calls libc `exit(101)`
  (Rust's panic-code convention — deterministic and testable). `/`/`%`/`%%` and unary `-` are
  **not** checked yet (division overflow / div-by-zero / `-INT_MIN` are a separate slice,
  grouped with the range-analysis pass); `wrapping_*`/`saturating_*` remain the explicit escape
  hatches (they type-check but still need their own backend lowering to the raw ops /
  `llvm.*.sat`). Six existing tests that asserted the *old* silent-wrap behavior
  (`u8(200)+u8(100)`→44, etc.) were migrated to assert the trap — the narrow-width overflow that
  used to wrap now traps, and the trap still proves the narrow width (a wide-width op wouldn't
  overflow). Tests: `llvm_checked_arith_test.go` (overflow traps exit 101 across signed/unsigned
  × add/sub/mul × i8/u8/i32/i64 + compound `+=`; non-overflow returns the real value; IR:
  `with.overflow` + trap present, division not checked, lazy — a non-arithmetic program carries
  none).
- **`i64` min literal (`-9223372036854775808`).** The magnitude `2^63` overflows `int64` as a
  *positive* literal, so the collector records it as an `Unsigned` (u64) literal — and negating
  a u64 was rejected ("cannot negate unsigned type u64"), making i64 min unwritable.
  `inferNegationExpr` now special-cases it: a negated `Unsigned` literal whose magnitude is
  exactly `2^63` is `i64` min (a valid signed value), typed `untyped_signed_int`. No backend
  change — the literal's bit pattern already *is* i64 min (`0x8000000000000000`) and `sub 0,
  i64min == i64min` in two's complement, so `lowerNegationExpr`'s `sub 0, x` emits the right
  bits; `println(id(-9223372036854775808))` prints `-9223372036854775808`. A narrower signed
  target (`let x: i32 = <i64min>`) is still caught by `checkIntegerLiteralRange` ("overflows
  i32"), and a magnitude *above* `2^63` negated (`-18446744073709551615`) is a clean "below the
  minimum i64" error. `-9223372036854775807` (int64max negated) is unchanged (its magnitude fits
  int64). Tests: `typechecker/tests/negation_test.go` (`TestTypeCheck_Negation_I64Min*`),
  `backend/llvm/llvm_print_test.go` (i64-min exec).
- **Narrow signed-min literals (`i8 -128`, `i16 -32768`, `i32 -2147483648`) — the narrow-width
  analogue of the i64-min fix above.** Each type's minimum written as a negated literal was left
  at i64: `propagateLiteralType` narrows a literal leaf "only when the value fits", and for
  `-128` it checked the *positive* magnitude 128 against i8's max 127 — doesn't fit → left
  untyped → i64 default. Latent for a plain read (the i64 value truncates to the right i8 bits)
  but the value in typed integer arithmetic against a proper-width operand emitted an `sdiv`
  mixing i64 and i8, which clang rejects. Fix: `propagateLiteralType`'s `NegationExpr` case now
  narrows the operand leaf directly when its magnitude is exactly `2^(bits-1)` for the signed
  context type (new `signedTypeMinMagnitude` helper in `overflow.go`, covering i8/i16/i32 —
  i64's `2^63` overflows int64 and stays on the `inferNegationExpr` Unsigned path). The backend
  then reads i8 off the leaf, and `sub i8 0, 128` yields the min bit pattern (llir renders the
  `2^(bits-1)` constant as unsigned hex `u0x8000` for i16/i32 — clang accepts it); a negated
  literal is trap-exempt, so the INT_MIN negation trap is unaffected. Out-of-range (`i8
  -129`/`-200`, positive `i8 128`) still gives a clean `checkIntegerLiteralRange` overflow
  error; i64-min unchanged. Tests: `typechecker/tests/negation_test.go` (leaf-width assertions
  for i8/i16/i32 min, below-min/positive-magnitude errors, `-128` in a wider i16 context),
  `backend/llvm/llvm_narrow_min_test.go` (IR width + exec `-128 / -2 == 64`),
  `collector/tests/expr_negation_test.go` (golden documenting the plain non-Unsigned `128`
  operand the fix keys on).
- **Fixed a typechecker panic on an out-of-range numeric literal.** A literal that overflowed
  `int64` (a valid `u64` value like `18446744073709551615`, or larger) made the collector's
  `collectIntegerLiteralExpr` return **nil**, which entered the AST as a *typed-nil* expression
  and later crashed `propagateLiteralType` with a nil dereference (`e.Value` on a nil
  `*IntegerLiteralExpr`). Root-caused at the source: an expression collector must never return
  nil into the tree — `collectIntegerLiteralExpr`/`collectFloatLiteralExpr` now emit a clear
  diagnostic and a **placeholder node** (`Value 0`) on parse failure, so no downstream pass ever
  dereferences a typed-nil. Messages distinguish the cases: a value in `(i64max, u64max]` →
  "exceeds the range of i64; unsigned literals above 9223372036854775807 are not yet supported"
  (large `u64` literals still aren't representable — `IntegerLiteralExpr.Value` is an `int64` —
  a separate feature); beyond `u64` → "too large to represent"; a bad float → "out of range for
  f64". Tests: `driver/driver_test.go` (full-pipeline).
- **Large `u64` literal support** (all four bases — decimal, `0x`, `0o`, `0b`). A literal in
  `(int64max, u64max]` (e.g. `18446744073709551615`, `0xFFFFFFFFFFFFFFFF`, `0b1…1`) now
  type-checks instead of erroring. `IntegerLiteralExpr` gained an `Unsigned` flag: the
  collector's int64-overflow fallback re-parses via `strconv.ParseUint` with the *same base* (so
  it's base-agnostic), stores the value's **bit pattern** (`int64(uint64value)`, so `Value`
  reads negative) and sets the flag, and `GetType()` reports a concrete **`u64`** — the
  literal's *only* valid type, so it isn't adaptable like an ordinary untyped literal. That's
  what makes it correct in every direction with no other typechecker change: `let x: u64 =
  <max>` assigns (u64→u64), `let x: i64 = <max>` is a clean `cannot assign u64 to i64` error
  (**never** a silent negative), `u32` too-small likewise, and `println(<max>)` formats unsigned
  (`snprintf %llu` over the bit pattern → `18446744073709551615`). The backend needs nothing new
  — `constant.NewInt` from the bit pattern gives the right bits, and the recorded `u64` type
  drives width/signedness. Beyond `u64` is still the clean "too large to represent" error.
  Tests: `collector/tests/large_unsigned_literal_test.go` (the `Unsigned` flag + magnitude
  across all bases), `driver/driver_test.go` (`TestAnalyze_LargeU64Literal_*`),
  `backend/llvm/llvm_print_test.go` (u64-max decimal/hex/binary exec). An explicit narrowing
  conversion of such a literal (`i8(<max>)`, `u32(<max>)`) is also flagged out-of-range against
  the true magnitude (`inferTypeConversion` special-cases an `Unsigned` literal — it fits only
  `u64` — before the bit-pattern `extractIntLiteralValue` path; `u64(<max>)` is fine). Tests:
  `typechecker/tests/type_conversion_test.go` (`TestTypeCheck_Conversion_LargeU64*`).
- **Non-string `print` formatting — `print`/`println` now format every scalar type.** Building
  on the string-only print (07/23), `print`/`println` are now **polymorphic over the printable
  scalars** (string, any integer/float, bool, rune → void). **Typechecker:**
  `builtinFunctionSignature` (the fixed `(string) -> void`) is replaced by `inferPrintCall` +
  `isPrintableType` (`builtins.go`) — it checks the one argument is printable and settles an
  untyped numeric literal to its default width (`propagateLiteralType(arg,
  promoteToDefault(argType))`) so the backend has a concrete type; an aggregate errors clearly
  ("cannot print a value of type …"). **Backend** (`print.go` `formatForPrint`): **int** → libc
  `snprintf` `"%lld"`/`"%llu"` by signedness (widened to i64) into an entry-block stack buffer;
  **float** → `snprintf` `"%g"` (promoted to double); **bool** → a pointer/length `select`
  between interned `"true"`/`"false"`; **rune** → UTF-8 encoded (1–4 bytes by magnitude) by a
  new lazily-defined runtime `lyra_rune_to_utf8`; **string** unchanged (fat-pointer `write`).
  snprintf formats into memory (not stdio), so numeric output stays in program order with the
  raw writes (verified: `print("count: "); println(7)` → `count: 7`). Signedness verified
  (`u8(200)` → `200`, not `-56`), negatives, u64-range, 3- and 4-byte UTF-8, all ASan-clean.
  **First-cut limitations:** float uses `%g` (so `1.0` prints `1`; shortest-round-trip is a
  future refinement); aggregates need a Show/Display trait. **Found in passing (separate task,
  not print-related):** a numeric literal exceeding i64 range (`big(18446744073709551615)`)
  panics `propagateLiteralType` (`typechecker.go:1436`) — pre-existing, spawned as its own task.
  Tests: `typechecker/tests/builtin_print_test.go`, `backend/llvm/llvm_print_test.go` (per-type
  stdout capture + snprintf IR).

### 07/23/26
- **`print`/`println` — the backend's first observable output, plus void-function lowering.**
  `println("Hello, world!")` now compiles and runs. **Typechecker:** a new builtin
  *free-function* registry (`builtins.go` `builtinFunctionSignature`, the free-function analogue
  of the builtin-method registry) resolves `print`/`println` as `(string) -> void` in
  `inferIdentifierCall` — but only after scope resolution misses, so a user `let print = …`
  shadows it (verified). Their effect classification (`EffectOutput` — allowed in `det`,
  forbidden in `pure`) already lived in `checker/effects.go` `builtinEffects`. Also fixed a
  latent gap: a **void single-expression** lambda body (`() -> void => print("x")`) was never
  inferred (silently unchecked); it now is, so the call is validated. **Backend:**
  `print`/`println` lower to libc `write(1, data, len)` (lazily declared, like memcmp/malloc)
  over the string fat pointer, with a second `write` of an interned `"\n"` byte for `println`
  (`print.go`); intercepted in `lowerFunctionCallExpr` *after* the user-function lookup (user
  shadowing wins, matching the typechecker). **Void functions now lower** (previously a loud
  "not implemented"): `lowerType` maps `VoidType` → LLVM `void`, `declareFunction` drops the
  void guard, `emitReturn` emits `ret void` (discarding any body value), and both
  `defineFunction` and `lowerEntry`'s void case lower the body for *effect* (`lowerForEffect` —
  handles an empty or non-expression-terminated block) then return; routing the void entry
  through `emitReturn(nil)` also flushes owned temporaries, so `println("a" ++ b)` frees its
  heap concat instead of leaking. **Ownership:** `print`/`println` **borrow** their argument
  (`calleeIsBorrowingBuiltin`), so an owned temporary argument is released after the call rather
  than conservatively transferred (alloc==release verified, ASan-clean). **Deferred:**
  formatting a non-`string` value (int/float/bool/rune → text) — the value→string machinery
  interpolation also waits on; `print` currently takes only `string`. Tests:
  `typechecker/tests/builtin_print_test.go`, `backend/llvm/llvm_print_test.go` (stdout capture +
  IR + alloc/release balance).
- **Rune literals in `match` arms + character-literal lowering (backend) — closes the grammar
  gap the 07/21 char→rune entry spawned.** A character-literal pattern (`'a' => …`) now parses,
  type-checks, and lowers end to end, and a `CharacterLiteralExpr` lowers as an expression at
  all (it previously had no `lowerExpr` case — `let c = 'a'` couldn't compile). **Grammar:**
  added `$.char_literal` to `literal_pattern` (`patterns/index.js`); the existing `[expression,
  literal_pattern]` GLR conflict already covers it (char_literal reaches `expression` via
  `_literal`), so no new conflict entry. **Collector:** a `literal_pattern` wrapping a
  `char_literal` stores its **decoded code point** as a new `ast.RunePatternValue` (a `rune`
  with a quoting `String()`), reusing the expression collector's `CharacterLiteralExpr` decode
  so escape handling (`\n`, `\x41`, `\U…`) lives in one place; every other literal keeps its raw
  source text. The Stringer keeps `%s`/`%v` diagnostics and golden output readable (`'a'`, not
  `97`). **Typechecker:** `literalPatternKind` returns `Rune` for a `RunePatternValue`; a new
  `isRuneType`/`checkRuneMatchArm` branch in `checkMatchExpr` (rune is deliberately *not*
  `IsNumeric`, so a rune scrutinee previously fell through every branch, unchecked) accepts
  char-literal/identifier/wildcard arms, rejects number/string/range/regex arms, and warns on a
  missing catch-all (like strings — code points aren't enumerated). **Backend:**
  `CharacterLiteralExpr` → `constant.NewInt(i32, codepoint)`; a rune scrutinee (i32) already
  routed to `lowerScalarMatch`, so `scalarMatchTest` just gained a `RunePatternValue` case
  emitting `icmp eq i32` against the pre-decoded point — guards and identifier catch-alls work
  unchanged. Verified: char/escape/unicode arms, wildcard fallthrough, inline char scrutinee,
  and a `x if x == 'a'` guard all compile+run to the right exit code. **Deferred:** char *range*
  patterns (`'a'..'z'`) — the `range_pattern` grammar bounds are still `_number_literal`-only;
  would round out rune match but adds grammar + exhaustiveness surface. Tests: grammar corpus
  (`match.txt`), `typechecker/tests/match_expr_runes_test.go`, `backend/llvm/llvm_rune_test.go`
  (exec + IR).

### 07/21/26
- **`char` primitive type: fixed collection + renamed to `rune`.** `char` had no grammar rule in
  `_primitive_type`, so it collected as a `GenericType` (a type variable), silently accepting
  anything (`let c: char = 5` type-checked). Added a `char_type` grammar rule + collector case,
  making it a real `PrimitiveType` (the backend already mapped it to i32). Then **renamed `char`
  → `rune`** (grammar `rune_type`, `types.Rune`, collector, backend, tests) to match Go/Odin and
  Lyra's UTF-8 byte-length string model — the honest name for an unvalidated i32 code point
  (Rust's `char` implies scalar-value *validation* this doesn't do). The character *literal*
  `'a'` keeps its syntax and its `CharacterLiteralExpr` node (now typed `rune`), exactly as Go's
  `token.CHAR` yields a `rune`. Separately discovered: character *literal patterns* in `match`
  arms (`'a' => …`) don't parse — a distinct grammar gap, spawned as its own task. Tests:
  `typechecker/tests/rune_type_test.go`, collector golden `rune_type_annotation`, grammar
  corpus.
- **Use-after-move on `own` parameters (`lyra-E019`).** New
  `pkg/analyzer/checker/use_after_move.go`: a flow-sensitive definite-move analysis flagging a
  read of a binding after its value was moved into an `own` parameter. A *move* is exactly one
  thing — a bare identifier naming a **managed** value (string or `shared`, via
  `ownership.IsManaged`) passed to an `own` param; an `own` scalar or stack struct is copied,
  and a field argument (`p.name`) is a partial move, so neither counts. Joins take the **union**
  (moved in either `if`/`match` branch → moved after), a loop body is seeded with every move
  inside it (so a move on one iteration is visible to the next iteration's reads, with a message
  that says so), and a `let`/`var` declaration or reassignment of the name clears the move.
  Every uncertain case resolves toward *not* reporting — an unresolvable callee records no move
  — so a new hard error can't fire on shapes the analysis doesn't understand. Reports dedupe by
  (binding, move site), which collapses a loop-carried move to one error instead of one per
  read. Runs after typechecking (it needs the TypeTable to identify managed values). **Framing
  correction:** the todo called this "the only unsound hole today"; measuring it disproved that
  — the ownership pass retains a managed value flowing into a non-last-use `own` argument, so
  use-after-`own` is memory-safe (verified ASan-clean with the correct result). The real payoffs
  are enforcing the `own` contract, surfacing the otherwise-invisible **reuse/FBIP perf cliff**
  (the defensive retain leaves rc = 2, `lyra_rc_drop_reuse` reports shared, and a rebuild that
  should be zero-allocation starts allocating per cell), and unblocking removal of that retain
  so `own` becomes a true move. Trait-impl and trait-default method bodies are covered too, each
  from a fresh state (the generic AST walker reaches them, so without an explicit case one state
  would thread through every method in an impl and a move in one would flag a read in the next).
  Tests: `use_after_move_test.go` (20 cases — base/borrow/field-read/repeated-arg, non-moves,
  branch+match+loop flow, reassign/rebind clearing, unresolvable-callee and per-function
  conservatism, trait methods), mutation-checked against both the join-union and loop-seeding
  rules.
- **Aggregate-field drop — a `shared` box now frees what its payload owns.** Release passed a
  null `drop_fn`, so a managed value inside a box (a string field, a nested `shared` value, a
  list's tail) was abandoned when its owner died — freeing a list freed the head cell and leaked
  the spine. New `drop.go` generates a cached `@lyra_drop_T(i8*)` per payload type, releasing
  every managed reference reachable *by value* from `T`, and `lowerManagedRelease` passes it as
  the box's `drop_fn`; "by value" is both the stopping rule and the termination argument (a
  managed field is released, never walked into, and a recursive cycle must pass through a
  `shared` field per lyra-E014). Two consequences: arm-binding *transfer* (`armTransfer`) is
  gone — a moved field would now be freed twice, so arm bindings always dup, costing refcount
  traffic but not allocations (reuse still reclaims the shell, so FBIP stays
  zero-alloc-per-cell) — and the reclaimed box's old payload is dropped at the match's merge
  (`dropReclaimedPayload`), past every arm, not inside `lyra_rc_drop_reuse`, which would free a
  field the arm hasn't dup'd yet. **Still leaking (safe):** a managed value inside a plain
  *stack* aggregate, which needs deep-retain-on-copy first. Tests: `llvm_aggregate_drop_test.go`
  (11 programs, exec + ASan + IR conservation).

### 07/18/26
- **Perceus stage 3 — reuse analysis / FBIP for `shared` values.** When a `match` destructures
  an owned `shared data` value at its last use, its ref-counted box is *reclaimed* in place
  instead of freed-then-reallocated. **Runtime:** `lyra_rc_drop_reuse(box)` (`runtime.go`)
  returns the box as a *reuse token* when unique (`rc==1`, not freed), null when shared
  (decrements) or pinned. **Construction:** `lowerBoxSharedReuse` (`shared.go`) branches at
  runtime on the token — write the new payload into the reclaimed box, else a fresh
  `lyra_rc_alloc`. **Pass** (`pkg/analyzer/ownership`): `ReuseMatch`/`ReuseTarget` mark a
  reuse-source match (owned scrutinee at last use, `shared data`, plain tag switch, ≥1
  same-type-constructing arm) and its target constructions; `lowerDataMatch` drop-reuses the box
  once, retires the scrutinee's slot (suppressing its ordinary drop), and hands the token to
  each arm (a target consumes it, a non-constructing arm frees it). **The recursion enabler:**
  `armTransfer` — a field binding used exactly once in a consuming match *moves* (no dup,
  `LastUseTransfer`) into an owning position, so the reused cell stays unique and a recursive
  `map` reclaims every cell (zero allocation per cell) instead of leaking the tail. **Supporting
  pieces:** a `shared`-value return path (`emitReturn`'s pointer case) and the typechecker's
  `propagateAllocation` (a `shared` return/annotation stamps construction leaves inside
  `match`/`if` arms `shared`, so the arm's value is heap-boxed — also closes the alloc-detection
  half of the "`shared` construction in return position" gap). A **borrowed** scrutinee is never
  reused (caller still owns it). ASan-verified across linear in-place update, recursive FBIP
  map, the token-free path, and the borrow safety boundary. **Deferred:** stage 4 specialization
  (skip shared-field stores + static-uniqueness fast path), the ladder-fallback path
  (guards/value-tests), struct/tuple reuse. Tests: `llvm_reuse_test.go` (runtime primitive +
  exec + ASan + IR), `ownership_test.go` (`TestReuse_*`).
- **`match` on a `shared` aggregate value (backend) — Perceus reuse prerequisite.** A `shared`
  data/struct/tuple scrutinee lowers to a box pointer, not a first-class aggregate, so `match`
  failed with "did not lower to a struct". New `unboxSharedData` (`shared.go`) loads the inline
  payload out of the box (`box → field 1`);
  `lowerDataMatch`/`lowerStructMatch`/`lowerTupleMatch` unbox up front and the existing
  tag/pattern machinery (incl. the payload-test/guard ladder fallback) runs unchanged on the
  union. An identifier catch-all binds the *box pointer* (its declared type), so
  `lowerAggregateMatch` now threads a `whole` value separately from the unboxed `scrut`. The
  box's own drop is the ordinary last-use release (reading through it consumes no reference —
  ASan-verified, 1 alloc/1 release). This is the destructuring foundation Perceus stage 3
  (reuse/FBIP for `shared` values) builds on — next up is drop-reuse (a reuse token when `rc ==
  1`) + reuse-aware construction. **Deferred (errors loudly):** a *nested* `shared data`
  sub-pattern (destructure a tail through its own box). Tests: `llvm_shared_match_test.go`
  (data/nullary/ident-catch-all/recursive-list/payload-test/guard/struct — exec + ASan + IR
  conservation + nested-deferred).

### 07/17/26
- **Clear diagnostic for SCREAMING_CASE struct names (`lyra-W009`).** Diagnosing the "shared
  multi-field struct copy fails" report turned up the real cause: it wasn't `shared` or
  multi-field at all — an all-uppercase type name (`P`, `AB`, `NODE`, …) matches the
  `const_identifier` lexer rule (`[A-Z][A-Z0-9_]*`) instead of `user_defined_type_name` (which
  needs a lowercase letter to win the longest-match tie), so a struct literal `NAME { … }` won't
  parse and every use surfaces a confusing "undefined symbol". New checker pass `CheckTypeNames`
  (`checker/type_names.go`) warns at the *declaration* (where the fix lives). Scoped to
  **structs** only — a `data` type constructs via its constructors and a named tuple via
  `Name(…)`, both of which work with a SCREAMING_CASE name (verified), so those aren't flagged.
  A warning, not an error, since the type is still usable by reference (e.g. a `data` payload
  `Wrap(P)`). Tests: `checker/type_names_test.go`. (Confirms the `shared` lowering has no
  multi-field limitation — `struct Pair { x, y }` with a `shared` copy runs, exit 42.)
- **`shared`-value lowering (backend).** A `shared T` now lowers to a **pointer to a ref-counted
  box `{ i64 rc, T }`** (`lowerType` + `SharedBoxType`), reusing the string runtime + ownership
  machinery. **Construction** (`lowerStructInstanceExpr`, `lowerDataConstruction`): a
  `shared`-flavored construction builds the inline payload and `lowerBoxShared` (`shared.go`)
  allocs `header + sizeof(payload)` via `lyra_rc_alloc` (rc=1) and stores it — the value is the
  box pointer. The flavor is read from the construction's recorded type (the typechecker stamps
  `Shared` on a `shared`-annotated binding's initializer and, transitively, on a `shared`
  payload arg, so a recursive `Cons(1, Nil)` boxes the nested `Nil`). **Field access**
  (`lowerMemberExpr`): a `shared` object is a box pointer, so a field is `getelementptr` through
  the box + load. **`shared` fields** lower to pointers (`lowerType`), which is also what makes
  a recursive `shared` field finite. **Ownership**: `IsManaged` now covers `shared`
  (`AllocationOf == Shared`), so a `shared` binding gets the full
  retain/release/last-use/transfer/drop treatment; retain/release dispatch on the value's
  representation (`lowerManagedRetain`/`Release` + `managedBox`: a string recovers its box via
  `stringBox`, a `shared` value *is* the box pointer). Verified memory-safe under
  AddressSanitizer with release==allocation conservation. Removed the old `shared`-payload
  "deferred, loud error". **Still deferred:** `shared` arrays, `shared` construction in bare
  arg/return position, `match` on a `shared data` value, and recursive drop of a managed value
  inside an aggregate field (leaks — the aggregate-drop follow-on). Tests: `llvm_shared_test.go`
  (construct/transfer/i64-payload/recursive-data, exec + ASan + IR conservation), updated
  `llvm_data_test.go`.
- **Perceus stage 2 — drop fusion (scalars).** Completes stage 2 by fusing the last-use *drop*
  (the transfer half landed earlier the same day). Replaced the sentinel + pending-slot-action
  mechanism with `dropLastUsesInStmt`: after each statement, `lowerBlockStmts` walks it for
  last-use-borrow identifier nodes and, for each binding declared in the current scope (top
  frame), releases it and retires the slot — emitted in the statement's **end block, which
  post-dominates** the statement's internal branches, so a *conditional* last use (inside an
  `if`/`match` arm) is freed correctly on every path. Doing drops at statement boundaries (not
  via a cross-statement pending list) is what makes it robust against the "steal" hazard: a
  statement that seals (an early return) is skipped, so its bindings are freed by the seal's
  frame release on that path, while the fall-through frees at the boundary — exactly once each.
  Removed the pinned-sentinel machinery entirely (`stringSentinel`, `pendingSlotActions`,
  `flushSlotActions`). A copy chain `a → b → c` now compiles to **one allocation and one
  release, zero retains** (was one no-op per binding). Verified under AddressSanitizer across
  conditional last-use, nested-return-in-the-last-use-statement, nested-block, and
  early-return-before-last-use cases, plus static release==allocation conservation checks (macOS
  ASan can't see leaks). Tests: `llvm_ownership_test.go` (four new `TestExec_Ownership` cases +
  `TestEmit_OwnershipIR` single-binding=1 / chain=1 / conservation).
- **Perceus stage 2 — transfer fusion (scalars).** A last-use *transfer* moves the reference to
  the consumer at the use point, so the transferred binding's scope-exit release was a pure
  no-op (a load + `lyra_rc_release` on a pinned sentinel). The backend now retires the binding
  from its managed frame *immediately at the move* (`retireManagedSlot` in `ownership_lower.go`,
  called from the `lowerExpr` last-use hook) instead of sentinelling it — no scope-exit release
  emitted at all. A copy chain `let b = a; let c = b; …` now emits **zero** per-transfer
  releases (was one sentinel no-op each); only the final binding's real drop (+ its backstop
  no-op) remains. Safe because a transfer is unconditional (the pass only marks a non-branch
  use) and the removal is compile-after any earlier seal, so an early return still saw the
  binding in-frame and released it on its own path — ASan-verified. **Still open in stage 2:**
  the last-use *drop* keeps its sentinel + frame backstop (its release must follow the borrow,
  so it's deferred, which entangles with the seal/pending timing — one residual no-op per
  dropped binding). Tests: `llvm_ownership_test.go` (`TestEmit_OwnershipIR` transfer-chain = 2
  releases, `TestExec_OwnershipASan`).
- **Perceus stage 1 — last-use precision (scalars).** The ownership pass
  (`pkg/analyzer/ownership`) evolves from scope-exit release toward Perceus's garbage-free
  last-use dup/drop. `computeLastUse` finds each eligible managed binding's *final textual
  reference* (a sound over-approximation of its dynamic last use; a binding that is shadowed, a
  parameter, reassigned, or referenced inside a loop is ineligible and keeps scope-exit release
  — the leak-safe direction). At the last use the pass emits **`LastUseTransfer`** (owning
  position — `let b = a`, `return a`, an `own` arg: the reference *moves*, so **no dup**, the
  tightness win over the old always-dup-then-scope-drop; applied only to an *unconditional* use
  so it happens on every path) or **`LastUseDrop`** (borrowing last use — released at that
  statement instead of scope exit). Backend (`ownership_lower.go` + `lowerExpr`/`emitReturn`):
  at a last use it retires the binding's slot with a **pinned empty-string sentinel**
  (`stringSentinel`) so the scope-exit frame release — kept as a leak-safe **backstop** — no-ops
  on already-handled slots and still frees anything the pass didn't reach (a missed last use
  only defers a free, never double-frees). Slot actions flush after *every* statement (before
  the frame release) so a transferred/returned binding is sentinelled before the frame could
  free it — the bug the ASan test caught. Also **closed the break/continue leak** (a loop's
  managed frames are released on those edges via a recorded `loopCtx.frameDepth`). **Verified
  memory-safe under AddressSanitizer** (`TestExec_OwnershipASan`) across copies, transfer
  chains, an early-return-before-last-use, and conditionals. **Deferred to later stages:**
  aggregate-field drop, hoisting a *conditional* last use, and the residual sentinel no-op
  releases (Perceus stage 2 = drop specialization + dup/drop fusion). Tests: `ownership_test.go`
  (transfer/drop/last-use decisions), `llvm_ownership_test.go` (new last-use/early-return/chain
  cases + ASan + IR), `runtime_test.go` unchanged.
- **Ownership model — heap strings are freed (front-end pass + backend retain/release).** The
  full ownership model ALLOCATION.md describes, realized for strings. **Uniform
  representation:** every string value is a ref-counted box, so retain/release are total and
  safe on any string — a **literal** now interns as a *pinned* static box `{ i64 PinnedRC, [N x
  i8] }` (`data` at `box+8`, still zero allocation; retain/release no-op via the PinnedRC
  sentinel), a `++` value is a heap box. This is the enabler: no site has to distinguish
  literal-vs-heap. **Ownership pass** (`pkg/analyzer/ownership`, runs after typecheck, produces
  a `Table` the backend consumes, no diagnostics): ARC over managed (string) values — a binding
  / `own` param holds one owning +1 released at scope exit; the pass computes the
  context-dependent adjustments — `Retain` (a borrowed value into an owning slot: a copy `let b
  = a`, an owned `return`, an `own` arg) and `ReleaseTemp` (an owned temporary into a borrowing
  slot: a `==`/match/`++` operand, a discarded statement, a borrowed arg) — using the same
  `paramOwnsArgument`/`isOwnedReturn` semantics as the typechecker. An `if`/`match` is treated
  as one merged owned value (branches coerced to +1), released once at the phi rather than
  per-branch (which would free the value the phi still refers to). Safety-biased: an
  unresolvable callee's args and values entering an aggregate are *transferred* (leak —
  memory-safe), never released. **Backend** (`ownership_lower.go` + hooks in
  `lowerExpr`/`emitReturn`): a stack of managed-scope frames releases bindings at scope exit (a
  `return` releases every live frame before it seals; the retain-on-escape the pass emitted
  makes that safe); each temporary is released **in the block it was produced in** — llir lets a
  release be appended before a sealed block's terminator — so a temp built inside an `&&`
  right-hand or `if` branch is freed there (dominating its uses), fixing the "instruction does
  not dominate all uses" hazard a merge-block release would hit. `own` string params are
  released by the callee; bare/`ref`/`mut` are borrows the caller keeps. Wired into
  `driver.Analyze` (`res.Ownership`). **Verified memory-safe under AddressSanitizer** (no
  double-free / use-after-free across copies, returns, chains, reassignment, own-params,
  conditionals, loops). Still leaking conservatively (safe, never a double free): strings inside
  aggregate fields, and bindings on break/continue paths. Tests:
  `pkg/analyzer/ownership/ownership_test.go` (retain/temp decisions), `llvm_ownership_test.go`
  (`TestExec_Ownership` behavioral, `TestExec_OwnershipASan`, `TestEmit_OwnershipIR` balance),
  `llvm_string_test.go` (`TestEmit_StringLiteralIsPinnedBox`).
- **Ref-counted heap runtime + string concatenation `++` (backend).** "The heap allocator" — the
  runtime that string `++`/interpolation and (later) `shared` values need. Architecture call:
  since there's no separate runtime object and `lyrac build`/the test harness both just run
  `clang out.ll`, the shims are emitted as **real function definitions into the module itself**
  (`runtime.go`, `ensureRCRuntime`) on libc `malloc`/`free` (declared like `memcmp`/`memcpy`),
  replacing the old dead `declareRuntime` externs. `lyra_rc_alloc` (malloc a box, rc=1),
  `lyra_rc_retain` (rc+=1), `lyra_rc_release` (rc-=1, `drop_fn(payload)` + `free` at 0) — all
  three defined together, all no-op on a `PinnedRC` (arena) box, box header a single `i64`
  refcount (payload at `box+8`). Emitted **lazily** — a non-allocating program carries none of
  it. First consumer: `lowerStringConcat` (`++`) — a concatenated string can't point into a
  constant global (its bytes are runtime), so it allocates a box via `rcAllocPayload`, `memcpy`s
  both operands into the payload, and returns a fat pointer `{ box+8, la+lb }`; operands are
  ordinary fat pointers wherever their bytes live, so chains and empty operands (`memcpy` n=0)
  compose with no special cases. **Ownership deferred (leaks):** nothing frees a heap string yet
  — `retain`/`release` exist and are correct but no call/scope site emits them (needs
  `own`/`ref`/`mut` reading + scope-liveness); this is the next allocation slice. Interpolation
  is no longer allocator-blocked — it now needs value→string formatting. Verified end-to-end
  (`lyrac build` + `clang` on a real `.lyra` using `++`) plus: `runtime_test.go` (white-box —
  hand-built `main` checking rc=1 after alloc, 2 after retain, 1 after release, pinned no-ops,
  and the `drop_fn`-runs-before-free path, all compiled+run via clang), `llvm_string_test.go`
  (`TestExec_StringConcat`
  literals/empties/left-associated-chain/param-strings/matching-a-heap-string,
  `TestEmit_StringConcatIR`, `TestEmit_NoRuntimeWhenUnused`).
- **Float→int rounding (typechecker + backend) — `x.floor()`/`.ceil()`/`.round()`.** The
  explicit, non-lossy escape hatch from the numeric-conversion rejection (`i64(x)` on a float is
  a typecheck error). Registered in `builtins.go`'s `floatRoundingOps` (float-receiver-only, 0
  args, fixed `i64` return — same "default width, narrow explicitly" approach as an unannotated
  literal, since context-directed *return*-type inference doesn't exist yet; this is the same
  open problem the still-unregistered `truncate`/`saturate`/`narrow` builtins have). This is
  also the first method call the LLVM backend lowers at all (`wrapping_add`/`saturating_add`
  type-check but were never lowerable) — `lowerFunctionCallExpr` gained a
  `*ast.MemberExpr`-callee branch routing to `rounding.go`'s `lowerBuiltinMethodCall`, which
  picks the receiver-width `llvm.<op>.<width>` intrinsic (lazily declared + cached, same shape
  as `memcmpFunc`) and `fptosi`s the result to i64. `round` uses `llvm.round`
  (half-away-from-zero) over `rint`/`nearbyint` (round-to-even). Tests exercise both positive
  and negative inputs and all three float widths, so `fptosi`/intrinsic-width selection is
  actually verified, not just the happy path. Tests:
  `typechecker/tests/builtin_rounding_test.go`, `llvm_float_test.go`.

### 07/16/26
- **Strings (backend): literals, equality, `match`, params/returns.** Representation decided —
  immutable fat pointer `{ i8* data, i64 len }` (byte length, not NUL-terminated;
  Go/Rust/Swift-standard, O(1) len, UTF-8/NUL-clean, literals need no allocation). Spec in
  `STRING_LAYOUT.md` (ALLOCATION.md had deferred this decision). Literals intern bytes in a
  private immutable global + fat-pointer struct (`lowerStringConstant`); `==`/`!=` branchless
  `len_eq && memcmp(min)==0` via libc `memcmp`; string `match` on the shared scalar ladder
  (byte-equality literal arms, identifier binds the fat pointer); by-value params/returns
  (`emitReturn` aggregate path); `lowerType`/`LLVMPrimitive`/`SizeAndAlign` map `string` → the
  struct (16/8). Concatenation `++`/interpolation (need a heap allocator), `print`, and
  escaped/regex patterns deferred with loud errors. Tests: `llvm_string_test.go`, all built +
  run via clang.
- **Float scalar `match` (backend).** `lowerScalarMatch` and the `lowerMatch` dispatch accept a
  float LLVM type (not just integer); `scalarMatchTest` delegates a float scrutinee to
  `floatScalarMatchTest` — `fcmp oeq` for a literal arm, a two-sided ordered range check
  (`oge`/`olt`/`ole`) for a range arm, `constFloatFromExpr` folding float/int/negated bounds.
  Identifier catch-alls bind the float, guards work unchanged, and a float match always needs a
  wildcard (the reals can't be enumerated; typechecker warns otherwise). Also added a
  typechecker warning on a float *literal* pattern (`checkNumericMatchArm`) — it lowers to `fcmp
  oeq`, the same exact-equality hazard as the `==`/`!=` operator warning; both now share
  diagnostic code `lyra-W008` ("imprecise float equality"). Only string/array match scrutinees
  stay deferred. Tests: `TestExec_FloatMatch` (literal/wildcard/range/binding/f32/guard),
  `TestEmit_FloatMatchIR`, `match_binding_test.go`
  (`TestMatch_FloatLiteralPatternWarns`/`_FloatRangePatternNoWarn`).
- **Floats (backend): literals, arithmetic, comparisons, conversions, params/returns.** Float
  literals lower at their context-recorded width (`literalFloatType`, default f64).
  `applyFloatMathOp` handles `fadd`/`fsub`/`fmul`/`fdiv`, `frem` for truncated `%`, and a
  `select`-based floored `frem` (`lowerFlooredFRem`) for `%%` — the float mirror of the integer
  path. `lowerFloatComparison` emits `fcmp` (ordered predicates, `une` for `!=` so `NaN != x`
  holds). `lowerNumericConversion` gained int→float (`sitofp`/`uitofp`) and float widening
  (`fpext`); `emitReturn` handles a float `retType`. The three arithmetic/comparison entry
  points dispatch on the already-lowered operand's LLVM type. Since there's no float→int
  conversion (typecheck error → `floor`/`ceil`/`round`, unimplemented), a float is observed via
  a comparison; **float→int rounding and float `match` stay deferred** (the scalar-match ladder
  is int-gated — a small follow-on). Tests: `llvm_float_test.go` (arithmetic, int→float/widening
  conversions, float function, IR shape), all built + run via clang.
- **Match arm guards (typechecker + backend) — `Some(x) if x > 0`.** A guard is a bool test
  evaluated after the pattern matches and its variables are bound; when false, control falls
  through to the next arm. Typechecker checks the condition with bindings in scope and requires
  `bool` (`checkMatchExpr`); guarded arms already didn't count toward exhaustiveness. Backend
  `lowerGuardedArmBody` cond-branches on the guard to the body or the next arm, plugged into
  both ladders (`lowerScalarMatch`/`lowerAggregateMatch`); a guarded arm never seals the ladder,
  and a `data` match with any guard takes the ladder fallback (`matchHasGuard`) rather than the
  tag `switch`. Only string/float/array scrutinees remain deferred for `match`. Tests:
  `TestExec_MatchGuards` (data/scalar/struct/tuple), `TestEmit_MatchGuardIR`,
  `match_binding_test.go`.
- **Value-testing `data` payload sub-patterns (backend) — `Some(0)`.** A tag `switch` can't
  discriminate two arms that share a variant tag but test different payloads. Nested in an
  aggregate (`(c, Some(0))`): `aggPatternTest`'s `DataPattern` case now ANDs the tag check with
  a branchless test per value-testing payload field (extract + recurse; safe on a tag mismatch
  since the AND is already false). Top-level (`match m { Some(0) => .., Some(x) => .., None =>
  .. }`): `lowerDataMatch` falls back to the shared if-else ladder (`lowerAggregateMatch`) when
  `dataMatchHasPayloadTest` finds one, keeping the compact `switch` otherwise. Arm guards and
  string/float/array scrutinees remain the only deferred match forms. Tests:
  `llvm_match_test.go` (`TestExec_DataLiteralPayload`, `TestEmit_DataLiteralPayloadIR`, nested
  cases).
- **Narrow tuple-literal payload width propagation (typechecker + backend) — fixes the
  spawned-task panic.** `propagateLiteralType` now recurses element-wise into a
  `TupleLiteralExpr` against a `TupleType` context, so a tuple data payload/struct
  field/annotation (`Wrapped((20, 22))` with `(u8, u8)`, `let a: (i32, i32) = (1, 2)`) narrows
  each leaf. Enabled by leaving an anonymous tuple's element leaves untyped in
  `inferTupleLiteralExpr` (deferred promotion, like arrays) with
  `promoteToDefault`/`isAssignable` gaining `TupleType` cases; the backend also runs each
  aggregate element through a defensive `coerceAggregateElem` so a residual width mismatch
  coerces instead of panicking `insertvalue`. Arrays remain the open half of the old gap. Tests:
  `data_ctor_width_test.go` (tuple payload + annotation narrowing), `llvm_data_test.go`
  (`TestExec_DataNarrowTuplePayload`, exit 42 end-to-end).
- **Nested `data` sub-patterns (backend).** Integrated into `aggPatternTest`/`aggPatternBind`: a
  data sub-pattern's test is its tag check (`extractvalue`-the-tag == index, ANDed into the
  aggregate condition — `(c, Some(x))` discriminates on the nested tag), and its bind
  reinterprets the payload (`extractDataPayload`, store-to-slot + `bitcast`) and recurses.
  `bindDataPayload` (top-level data arm) also recurses via `aggPatternBind`, so `Wrapped((a,
  b))`/`Boxed({ x, y })` destructure. Deferred: a value-testing payload sub-pattern (a literal
  `Some(0)` or nested data pattern — `patternHasTest`), since a tag switch can't
  test-and-fall-through. Found a separate pre-existing panic (spawned task): constructing a data
  value with a narrow tuple-literal payload (`Wrapped((20,22))` with `(u8,u8)`) doesn't
  width-propagate → `insertvalue` panic. Tests: `llvm_match_test.go`
  (`TestExec_NestedDataPatterns`, literal-payload deferred).
- **Nested sub-patterns in struct/tuple match (backend).** Generalized the struct/tuple
  test+bind into mutually-recursive `aggPatternTest`/`aggPatternBind` that walk a pattern
  against the first-class aggregate value: a literal sub-pattern ANDs an `icmp`/range check, a
  nested struct/tuple sub-pattern recurses via `extractvalue` (`((a, b), c)`, `{ inner: { v }
  }`, `(Pt { x, y }, c)`) — always safe with no branch, since a single-shape aggregate has no
  tag. Threads the Lyra type down to resolve nested field/element types. Replaced the four flat
  `structPatternTest`/`bindStructPattern`/`tuplePatternTest`/`bindTuplePattern` helpers.
  Deferred: a nested `data` sub-pattern in an aggregate (needs tag+memory), data-payload
  destructuring (`W((a,b))`). Tests: `llvm_match_test.go` (`TestExec_NestedAggregatePatterns`,
  data-payload-destructure deferred).
- **`match` on a tuple (backend) + shared aggregate ladder.** `lowerTupleMatch` is the
  positional counterpart to struct match: `(a, b)` binds elements by index (`extractvalue`),
  `(0, b)` tests element 0. Refactored the struct/tuple ladders into one shared
  `lowerAggregateMatch` (single-shape aggregate, no tag) parameterized by `test`/`bind` closures
  — the struct-vs-tuple difference (named fields vs positional elements) is all that differs.
  Now every match scrutinee kind lowers (data/struct/tuple/scalar); remaining deferred:
  string/float (types don't lower), guards, nested sub-patterns. Tests: `llvm_match_test.go`
  (`TestExec_TupleMatch`, nested-pattern deferred).
- **`match` on a struct (backend).** `lowerStructMatch`: a struct has one shape (no tag/switch),
  so `{ x, y }` matches unconditionally and binds fields (`extractvalue`→alloca), while a
  literal field sub-pattern (`{ x: 0, y }`) makes the arm conditional (`structPatternTest`,
  reusing `scalarMatchTest`) — same if-else ladder, `_`/identifier catch-all. Struct patterns
  are brace-only or type-named. Tuple/string/float scrutinees, guards, nested field sub-patterns
  deferred. Tests: `llvm_match_test.go`.
- **`Pt { x, y }` struct patterns (unbundled from data patterns).** A struct pattern may now
  name its type (symmetric with construction `Pt { x: 1 }`), not just the brace-only `{ x, y }`.
  Since `Pt { … }` and a data variant `Node { … }` are syntactically identical (both parse to
  `DataPattern`), the collector's new `reclassifyStructPatterns` finishing pass splits them
  semantically: a name that's a declared struct type → named `StructPattern`; a data constructor
  → stays `DataPattern`. So struct and data-constructor patterns are now distinct AST nodes
  everywhere downstream. Typechecker verifies a named pattern's type matches the scrutinee/value
  (a new safety check the brace-only form couldn't express); backend unchanged (a named
  `StructPattern` lowers like a bare one). Tests:
  `collector/tests/reclassify_struct_pattern_test.go`,
  `typechecker/tests/match_struct_named_test.go`, `llvm_match_test.go`.
- **`match` on a bool/integer scalar (backend).** `lowerScalarMatch`: an if-else ladder — each
  arm a comparison (`icmp eq` for a literal, two-sided range check for a range pattern) that
  cond-brs to the body or the next test, first-match-wins; `_`/identifier arm ends it
  (identifier binds the scalar), arms feed a merge phi, fall-through is `unreachable`. Uniform
  ladder (not a switch) so range arms fit. Tuple/struct/string/float scrutinees and guards
  deferred. Tests: `llvm_match_test.go`.
- **`match`/destructuring on a `data` value (typechecker + backend).** Typechecker now binds
  match-arm pattern variables in the arm body
  (`checkMatchExpr`/`withPatternBindings`/`walkDestructuredPattern` via `paramTypes`;
  `bindDataPatternPayload` handles flat `Rect(w, h)` and tuple `MkPair((x, y))` forms), and
  propagates arm-body widths (10th `propagateLiteralType` site +
  `MatchExpr`/`IfExpr`/`BlockExpr` recursion) so bare-literal arms adapt to a sibling or the
  return type. Backend `lowerDataMatch`: store scrutinee → load tag → `switch`; data-pattern
  arms `bitcast`+`load` the payload and bind fields (`extractvalue`→alloca), `_`/identifier arm
  is the default, arms feed a merge phi, exhaustive → `unreachable` default. Closes the
  observability loop (construct→match→extract). Guards, non-`data` scrutinees, and tuple/nested
  payload sub-patterns deferred with loud errors. Tests: `llvm_match_test.go`,
  `typechecker/tests/match_binding_test.go`.
- **`data` value construction (backend) + by-value-payload sizing gap closed.**
  `resolveForLayout` deep-resolves `UnresolvedType` payload leaves through the symbol table
  (short-circuiting `shared` refs, which keeps it finite), so a by-value named-type payload
  (`Wrap(P)`) lays out. `lowerDataConstruction` materializes the tagged union through memory
  (alloca, store tag, `getelementptr`+`bitcast`+`store` the payload, load back) for nullary +
  positional variants. `types.DataTypeConstructor.FieldTypes()` unwraps the collector's
  single-anonymous-tuple param, shared by the backend and a new typechecker width-propagation
  site (9th) so narrow ctor args take the field width. `shared`-payload and inline-record
  construction deferred with loud errors. `match` is the remaining step. Tests:
  `llvm_data_test.go`, `typechecker/tests/data_ctor_width_test.go`.
- **`data` type-declaration layout (backend).** A `data` decl lowers (same two-pass type-decl
  machinery) to its tagged union `%T = { iTAG, [K x iA] }` via `lowerDataDef` + the existing
  `DataUnionType`/`SizeAndAlign`: an `i8` tag then a payload blob sized to the largest variant.
  Enums → `{ i8 }`; recursive types finite via the `shared` (pointer) field. Not-yet-sizeable
  payloads (string, generic) defer with a loud error. Tests: `llvm_data_test.go`.
- **Struct instances (backend) — construction + field access.** `lowerStructInstanceExpr` (undef + `insertvalue`, fields keyed by name and built in declared order) and `lowerMemberExpr`
  (`extractvalue`, field position from the object's declared struct type via
  `namedStructFields`, resolving `UnresolvedType` fields through the symbol table for nested
  access). Typechecker: `inferStructInstanceExpr` now propagates the declared field width onto
  untyped literal field values (8th `propagateLiteralType` site) so narrow fields lower
  correctly. Record-update, default fields, and inline-record data constructors deferred with
  loud errors. Tests across backend (exec + IR) and typechecker. Next: `match`/`data` layout.

### 07/15/26
- **Type-declaration lowering (backend) — tuple/struct shapes to named LLVM struct types.**
  Two-pass declare-then-define (like functions) so fields can reference other named types in any
  order, forward refs included; `lowerType` resolves named refs via `structTypes`. Fields lower
  by value; `shared`/boxed, `data`/sum, and instances (construction/field access) deferred.
  Fixed the switches that had made the whole path dead code (value-vs-pointer type match, empty
  tuple case, name-keying panic). Tests: `llvm_typedecl_test.go`.
- **Grammar: positional tuple access (`pair.0`).** New `tuple_index_expr` postfix rule, distinct
  from `member_expr`; no float-token collision even for nested `pair.0.1` (context-sensitive
  lexer). Grammar + corpus only — the collector→backend wiring for tuple instances is the
  follow-on.
- **Tuple instances end to end (collector → typechecker → backend).** `TupleIndexExpr` AST node + collector; typechecker `inferTupleIndexExprType` (element type, bounds/non-tuple errors,
  named + anonymous); backend lowers construction via `insertvalue` and access via
  `extractvalue` as first-class struct values, with an anonymous-tuple structural `lowerType`
  case. Data-constructor literals still error loudly. Struct instances are next. Tests across
  all three layers + exec.
- **Allocation is now a use-site flavor only — removed declaration-level `stack`/`shared`
  modifiers.** Rationale (matches Lyra's already-made "allocation isn't nominal identity"
  decision + the explicitness ethos; Rust is the closest neighbor — no "this struct is always
  heap"): flavor is a property of a value's storage, chosen where it's used, not baked into the
  type declaration. **Grammar:** dropped the `optional(allocation)` field from
  `struct_type`/`data_type`/`named_tuple_type` (kept `allocated_type`, the use-site `let n:
  shared Node` / `field: shared Node` form). **Types/collector:** removed
  `TypeDeclStmt.Allocation` and the decl collectors' flavor collection; **added
  `TupleType.Allocation`** (now required — a use-site `shared Point` on a named tuple had
  nowhere to land, so `WithAllocation`/`AllocationOf` gained a `TupleType` case). **E014
  (`recursive_type.go`):** dropped `declIsSharedType` — a recursive cycle is broken only by a
  `shared` *field* now (`Cons(i64, shared List)`); the error message updated to match.
  **`noalloc`/EffectAlloc rebuilt use-site (the substantive part):** `buildAllocContext` no
  longer scans for `shared`-declared type names (there are none); instead
  `allocContext.allocates` reads the construction's recorded `TypeTable` flavor
  (`AllocationOf(typeTable.Get(expr)) == Shared`) — an annotated binding `let n: shared Node =
  Node{…}` records the flavor onto the construction via `checkVarDecl`, so the alloc is detected
  there. `CheckPurity` gained a `*typetable.TypeTable` param (threaded from the driver); the
  AST-only `InferredEffects` helper has no TypeTable and so no longer sets `EffectAlloc` (its
  only consumer, `InferredPureFunctions`, masks `PurityEffects` and ignores the alloc bit — no
  impact). **Still deferred:** a `shared` construction in return/argument position (flavor not
  recorded on the construction node there) is not yet detected — future escape pass. Tests:
  rewrote the E018/`noalloc`/recursive-type/collector-golden tests to source flavor from
  binding/param annotations and field-level `shared` instead of declaration modifiers; the
  AST-only `InferredEffects` alloc tests were removed (behavior now tested via the
  `CheckPurity`/E016 path). ~30 files across grammar + Go; full suite green.
- **Named tuples are actually nominal now, closing a gap in the 06/19 "positional nominal"
  decision.** `types.TypesEqual`'s `TupleType` case previously ignored `Name` entirely,
  comparing every tuple — named or anonymous — by element shape alone, so `tuple Point(i32,i32)`
  and `tuple Vector(i32,i32)` were wrongly interchangeable. Fixed: a named tuple
  (`!types.IsAnonymousTupleName(t.Name)` — the sentinel is `""` or `"?"`, both meaning
  anonymous) now compares by name alone, matching `NamedStructType`; both-anonymous stays
  structural (element-wise) as before. `isAssignable` needed no separate fix — it delegates to
  `TypesEqual` on its first line. This alone would be unsound (two unrelated literals sharing a
  name could compare equal despite different shapes), since named-tuple *construction* never
  validated a literal (`Point(3, 4)`) against its declaration the way struct literals do — so
  also fixed `inferTupleLiteralExpr`/new `inferNamedTupleLiteralExpr`: a named literal now
  requires a declared `tuple Point(i32, i32)` in `symTable.Types` and its arity + positional
  element types are checked against it (turbofish substitutes generics; no turbofish leaves a
  still-generic position unconstrained — per-position generic *inference* from supplied values,
  the way structs infer from named fields, is deliberately not attempted, a smaller scope than
  structs). Elements needed the same literal-width propagation as calls (`propagateLiteralType`,
  the 7th site) — without it, an untyped literal promotes to i64 before the assignability check
  runs and spuriously fails against a narrower declared element type (e.g. `i32`). Tests:
  `pkg/types/unify_test.go` (direct `TypesEqual` cases) +
  `pkg/analyzer/typechecker/tests/named_tuple_test.go` (construction validation, generics, the
  motivating cross-name-same-shape case). **Found along the way, left alone (pre-existing, out
  of scope):** an anonymous tuple/array literal's elements still don't propagate width against a
  `let` annotation's element types (`let a: (i32,i32) = (1,2)` errors) — a distinct gap from
  named-tuple nominality. *(The tuple half was fixed 07/16 — see that day's tuple-payload entry;
  arrays are still open.)*

### 07/14/26
- **User-defined functions (backend): definitions, calls, `return`, recursion.** Two-pass `Emit`
  (declare all → main → define all) so calls resolve before bodies exist; per-function state via
  `beginFunction`; params bound as entry-block allocas; a single `emitReturn` path shared by
  main (u8→i32 ABI), explicit `return` (`ReturnStmt`, reusing the block-sealing discipline), and
  the tail return; calls via `lowerFunctionCallExpr` with args passed un-coerced (param widths
  propagated onto literal args in `inferLambdaCall`, the sixth `propagateLiteralType` site).
  Scalar params/returns only; void/multi-clause/default-param/destructuring-param/higher-order
  forms deferred with loud errors. Tests: `TestExec_Functions` (simple call, narrow-arg wrap,
  early return, recursion, mutual recursion, call-in-loop).

### 07/13/26
- **`for` loops (backend) + three-clause form end to end.** Backend: `lowerForLoop`
  (cond/body/post/exit CFG), `break`/`continue` via a `loops []loopCtx` stack (labeled ones walk
  it), one-armed `if` statements, `MathAssignOpExpr` (`i += 1`) as load/op/store, and a
  block-termination discipline (`lowerBlockStmts` stops at a sealed block; fall-through `br`s
  guarded by `end.Term == nil`). Typechecker unblocked the full `for var i = 0; i < n; i += 1`
  form: a `MathAssignOpExpr` `inferExprType` case (void, with RHS width propagation) +
  `checkForLoopExpr` entering the loop's registered scope so the init variable resolves. Still
  open: body-local declarations don't resolve inside a loop body (needs `ForLoopExpr.Body` to
  become a pointer). Tests: `TestExec_ForLoop`, `TestExec_ForLoopThreeClause`.
- **Context-directed literal-width inference.** An untyped integer literal now takes its
  concrete width from context instead of always defaulting to i64. New typechecker helper
  `propagateLiteralType(expr, concrete)` pushes a concrete numeric width onto untyped literal
  *leaves*, recursing through width-preserving arithmetic (`+ - * / % %%`, unary `-`) and
  stopping at identifiers/calls/conversions. Wired into five context sites: annotated `let`, a
  `MathBinaryOp` with a concrete result, numeric comparisons/`==`, `var` reassignment, and the
  lambda/entry return body. It only narrows a leaf that *fits* the target width — a value that
  doesn't (`i8(x) < 300`) is left untyped rather than silently wrapped, so overflow surfaces
  loudly (the fold-based `checkIntegerLiteralRange` at decl/reassign sites, or a backend width
  mismatch) instead of miscompiling; this keeps propagation from double-reporting the overflow
  the range check already owns. Backend reads the recorded leaf width (`literalIntType`,
  fallback i64) instead of hardcoding i64, so mixed-width comparisons (`i8(x) < 3`) and narrow
  arithmetic (u8 wraps at u8, distinguishable from i64-then-truncate) now compile and run. This
  closes the deferrals threaded through `pkg/backend/llvm` (the mixed-width comparison guard is
  now defensive-only) and the `/`÷`%` truncation concern in `lowerEntry`'s doc comment.
  **Deferred:** call-argument and match-arm context, and a nicer typechecker diagnostic for a
  literal that exceeds its context width (today it's caught at lowering).
- **`let`/`var` lowering + sequential-rebind typing fix.** Backend: `let`/`var`/rebinding and
  identifier reads lower via entry-block `alloca` + store/load; `var` reassignment stores into
  the existing alloca (fixed a bug that overwrote the `locals` slot with the stored value, which
  would panic the next read). Typechecker: a rebind's initializer that reads its own name (`let
  x = x + 1`) resolves the self-reference to the prior binding — the collector now records it as
  `VarDeclStmt.Shadows` and `checkVarDecl` tracks `currentVarDecl` so the identifier redirects
  there instead of to the not-yet-typed decl (previously left nil, silently masked by
  nil-guards).

### 07/11/26
- **Comparisons + `&&`/`||` lowering.** The six comparison ops lower to a single `icmp` with the
  right signed/unsigned predicate; `&&`/`||` short-circuit via a cond-br + phi diamond reusing
  the `if` machinery (the constant branch is a phi edge, no extra block). Enables non-constant
  `if` conditions. Comparisons are int-only (float/mixed-width deferred, loud error). Tests:
  `TestEmit_BoolBinaryOp`, `TestExec_BoolShortCircuit`.
- **`if`/`else` + blocks lowering.** `lowerIf` builds the standard cond-br → then/else →
  merge-phi diamond; `lowerBlock` lowers to its last statement's value. `lowerExpr` now returns
  *(value, endBlock, err)* so a branching form can move the insertion point. Conditions were
  still bool literals only at this point (comparisons landed later same day). Tests:
  `TestExec_If`.
- **Typechecker: one-armed `if` as a value is now an error.** A one-armed `if` used as a value
  has no result when false, so `checkIfExpr` now requires a terminal `else`; as a statement it's
  still fine. Prereq for the backend `if` lowering above.
- **Int-width conversions — `i8(x)`, `u32(x)`, etc.** Lyra's one conversion syntax lowers to
  `trunc`/`sext`/`zext`/identity based on the source's signedness and lowered bit width — the
  only way to exercise a non-i64 width today. Verified with an overflow-wrap case and a
  division-based test that actually distinguishes `sext` from `zext`. Float conversions deferred
  (unreachable from valid source yet).
- **Arithmetic — `+ - * / % %% -(unary)`.** All lower and are tested behaviorally
  (compile+run+check exit code). Matches Odin's split: `%` (Mod) is truncated (native
  `srem`/`urem`), `%%` (Remainder) is floored via a branchless `select` sign-fixup
  (`lowerFlooredSRem`); unsigned floored/truncated remainder are identical. Also fixed a real
  gap where a bare literal used directly as a binary operand had no TypeTable entry.
- **`main`'s entry-point convention changed from `i64` to `u8`** — the OS truncates a process
  exit code to its low 8 bits regardless of declared width (verified: even real C `int main() {
  return 300; }` exits 44, not 300), so `i64` only added the silent-truncation surprise Lyra
  rejects elsewhere; `u8` makes the 0–255 constraint visible in the type itself (matches Zig's
  `pub fn main() u8` and Rust's move to the narrow `std::process::ExitCode`).
  `driver.ResolveEntryPoint`/`EntryReturn` updated. Also fixed an adjacent, unrelated-but-real
  ABI bug found while researching this: `@main` at the LLVM level was declared `i64`, but the
  actual C runtime signature is `i32` (verified against real clang output) — it happened to
  produce correct results only by x86-64 register-truncation coincidence. `@main` is now
  correctly `i32`, with the Lyra-level `u8` body value coerced (new `coerceIntWidth` helper,
  shared with `lowerNumericConversion`) and zero-extended into it. Tests + all affected
  exec-test sources updated (some needed an explicit `u8(...)`/`i8(...)` wrapper, since e.g.
  negating a literal produces `UntypedSignedInt`, which `isAssignable` correctly refuses to
  assign directly to unsigned `u8`).
- **First real lowering** — `lowerEntry` + `lowerExpr` (`llvm.go`) lower an integer-literal
  `main` body to a real `ret`, so `let main = () -> i64 => 42` compiles+runs to exit 42 (`=> 7`
  → 7; `-> void` → 0). `lowerExpr` returns an error for unhandled forms so the build fails
  loudly rather than emitting wrong code. Tests: `llvm_test.go`
  (`TestEmit_IntegerLiteralBody`/`_VoidEntry`/`_UnsupportedBody`).
- **llir/llvm set up for the backend** — added `github.com/llir/llvm` v0.3.6 (pure Go);
  `Emit`/`lowerEntry`/`declareRuntime` now build a real `ir.Module` instead of string assembly,
  and `layout.go`'s type helpers (`LLVMPrimitive`/`SharedBoxType`/`TagType`/`DataUnionType`)
  return llir `types.Type`. Placeholder `@main` module still compiles + runs via clang. Typed
  pointers (`i8*`), not opaque — fine for the scalar milestone. Tests updated to compare via
  `.String()`; full suite green.

### 07/10/26
- **Backend layout helpers scaffolded** — `pkg/backend/llvm/runtime.go` (shim names + `PinnedRC` + `emitRuntimeDeclarations`, now emitted into every module) and `layout.go` (`LLVMPrimitive`,
  `SharedBoxType`, `TagType`, `DataUnionType`, `SizeAndAlign`). `SizeAndAlign` implements
  C-style struct padding, static-array stride, and the sum-type union sizing, and treats a
  `shared` value as pointer-sized (so recursive `shared` types are finite). Emitted IR (now with
  the shim `declare`s) still compiles+runs. Tests: `layout_test.go` (12 cases). The type toolkit
  `lowerType` will call; expression/statement codegen is still the from-scratch work.
- **`data`/sum-type layout decided** (backend) — tagged union `%T = { tag, payload-blob }`:
  smallest-fitting int tag in declaration order, payload sized/aligned to the largest variant
  and accessed as the active variant's struct. Orthogonal to the alloc flavor (inline vs boxed).
  Monomorphized generics; recursive occurrence is `shared` (a `ptr`, finite size).
  Niche/tag-fold optimization (e.g. `Maybe<ptr>` = null) deferred. Spec:
  `pkg/backend/llvm/DATA_LAYOUT.md`.
- **`stack`/`shared` representation decided** (#5 (d)) — `stack` = inline value; `shared` =
  `ptr` to a ref-counted box `{rc, payload}` with retain/release driven by `own`/`ref`/`mut`,
  and arena values pinning the rc for bulk free. Full spec + runtime-shim signatures in
  `pkg/backend/llvm/ALLOCATION.md`. Non-atomic refcounts (parallel readers borrow, so no rc
  races); atomic/weak/COW deferred.
- **LSP migrated onto `driver.Analyze`** — `cmd/lyra-lsp`'s ~300-line inline pipeline replaced
  by a thin `driver.Analyze` + `diagToLSP` wrapper; pipeline now defined once. LSP suite green.
- **LLVM backend skeleton** — `pkg/backend` interface + `pkg/backend/llvm` (textual IR); `lyrac
  build` writes a placeholder `main` module confirmed to compile and run (exit 0).
- **Program entry-point convention** — `driver.ResolveEntryPoint`: a zero-param top-level `let
  main` returning `i64`/`void`; build-time only, enforced by `lyrac build`.
- **Builtin-method registration** — `typechecker/builtins.go`;
  `wrapping_/saturating_{add,sub,mul}` type-check on integer receivers. Primitives are now valid
  method receivers (missing → `T has no method "x"`).
- **Removed the `given` keyword** — retired the grammar rules, reserved word, AST node, and
  checker cases; corpus + suite green.
- **Purity scope Phase 2 (lambdas + free functions)** — purity reads the collector's ScopeTable
  instead of re-walking the AST; zero behavior change. Methods deferred.
- **Allocation-compatibility check (`lyra-E018`)** — owning a value across a `stack`↔`shared`
  boundary is an error at binding/reassign/lvalue sites; fires only on concrete differing
  flavors (`Unspecified` is polymorphic).
- **…args/returns** — E018 extended to `own` arguments and owned returns; borrowed (`ref`/`mut`)
  are polymorphic and skipped.
- **…element-level** — E018 recurses into tuple/array elements; fixed a pre-existing bug where
  named tuple/array element types weren't resolved by `resolveType`.

### 07/09/26
- **Per-trait-method effect bounds** — a trait method may be declared `pure`/`det`/`noalloc` as
  a contract every impl must satisfy (`E007`/`E016`; `E015` for `pure det`).
- **Bound-dispatched calls visible to purity** — a call through a `where` bound scores as the
  join over all impls (pure/det only if all are).
- **Impl method body return-type checking** — each impl method body is checked against the
  trait's declared return type (Self + trait params substituted).
- **Binding a trait's own type params** — `impl Get<t> for Box<t>`; `box.get()` on `Box<i64>`
  returns `i64`.
- **Bounded polymorphism in method bodies** — calling a trait method on a bare type-param value
  dispatches through its `where` bound (abstract dispatch).
- **Generic struct field access** — `b.value` on `Box<i64>` substitutes type args into field
  types → `i64`.
- **Generic impl dispatch + bound checking** — `impl Show<t> for Box<t>` unifies against a
  concrete receiver; `where` bounds constraint-checked.
- **`noalloc` catches unknown external calls** — an unresolvable call taints alloc too, so
  `noalloc` flags it.
- **ScopeTable population (Phase 1)** — collector records node→scope for lambda params, both
  loops, and `with` blocks.
- **Error-type checking for `Result?`** — `?` compares the operand's error type against the
  enclosing return (assignability-only).
- **Canonical Result/Maybe identity** — a `CanonicalKind` stamp replaces per-site name matching
  (`@builtin` or name+shape fallback).
- **`det` rand/time detection** — ambient `Random.global()`/`wallClock()` carry Rand/Time so
  `det` forbids all nondeterminism sources.

### 07/08/26
- **ML-style function sugar** — `let name(params) => body` (and modifier-led `let pure
  name(...)`) desugar to `let name = (params) => body`.

### 06/24/26
- **Recursive-type well-formedness check (`lyra-E014`)** — a by-value type cycle errors unless
  broken by `shared`.
- **Alloc as type identity (first step)** — types carry an `Allocation` flavor through the AST;
  `AllocationOf`/`WithAllocation` added.
- **Method-to-method call tracking** — impl method bodies are type-checked so inner `.`-calls
  dispatch into MethodTable, feeding the purity fixpoint.
- **Trait methods can be `pure`; real method dispatch** — `obj.method()` / `Trait::method()`
  resolve to an impl, type-check, and report ambiguity; recorded in a new MethodTable.
- **Purity inference for unannotated methods** — inferred via a joint fixpoint over functions +
  methods.
- **Impurity of imported/external functions** — unresolvable calls (not builtins/conversions)
  are conservatively impure.
- **Fixed: parser hang on a lambda-typed struct field** — nil-guard on an absent optional
  `parameter_types` node.

### 06/23/26
- **Purity inference records *pure* as queryable** — `InferredPureFunctions` exposes every
  top-level function's inferred purity by name.

### 06/22/26
- **Fixed: `if let`/`let … else` were never type-checked** — added the missing `checkNode`
  cases.
- **Purity foundation** — if-let/let-else names registered with correct scoping;
  if-let/else/`with`-arena bindings treated as locals.
- **Purity: `await` is an impure effect.**
- **Constant-folded division-by-zero** — folds constant int expressions, not just bare `0`.
- **Fixed: `DataPattern` as a lambda parameter mis-parsed** — added `PREC.DATA_PATTERN`.
- **Fixed: destructuring never bound names** — `walkDestructuredPattern` binds all pattern
  kinds.
- **Result/Maybe shape validation** — recognition checks constructor shape, not just name.
- **Confirmed generic type params are lowercase-only by design** (not a bug).

### 06/21/26
- **Purity: impurity inference for non-top-level functions** (keyed by lambda pointer).
- **Purity: track captured mutable bindings from non-top-level enclosing scopes.**

### 06/19/26
- **Purity: reject reading captured mutable globals (`lyra-E007`).**

### 06/17/26
- **LSP:** folding ranges, workspace symbols, signature help, rename + prepare-rename, document
  highlight; `@sizeof` on unknown types.

### 06/16/26
- **Fixed keyword carving in identifiers** (grammar) — `letter` no longer lexes as `let`+`ter`.
- **LSP:** semantic tokens, completion, find references.

### 06/15/26
- **LSP:** code actions / quick fixes.
- **Fixed: nullary constructor swallowed the following statement** (grammar) — residual: nullary
  binding + bare call still swallowed.
- **`const` requires a compile-time-constant initializer (`lyra-E012`).**
- **Unsafe ops outside `unsafe` require an `unsafe` context (`lyra-E011`).**
- **Wire `ref`/`mut`/`own` parameter modifiers into mutation/purity checks.**
- **Struct field mutability** — mutable by default, with a deep `readonly` freeze marker.
- **Fixed: named-struct field types weren't resolved** (broke nested member access/literals).
- **Safe mutable lvalues / three-level binding** — added `let mut`.

### 06/14/26
- **Allow same-scope sequential rebinding** (`let x = parse(x)`).
- **Require initialization at declaration (`lyra-E010`).**
- **Fixed: nullary data constructors as values were dropped.**
- **One conversion syntax decided** — `f32(x)` is the single widening form.
- **Non-exhaustive `match` on closed types is an error (`lyra-E009`).**
- **Restrict `??` to optional operands (`lyra-W007`).**

### 06/13/26
- **Must-use `Result`/`Maybe` (`lyra-W006`)** — dropping one without binding/match/`?` warns.

### 06/12/26
- **Subtraction parser bug fixed** (`let x = 0 - 200` dropped `- 200`).
- **Constant-folded arithmetic overflow** (static slice, annotated types).
- **`?` (try) propagation operator (`lyra-E008`).**
- **Removed platform-dependent `int`/`uint` and bare `float`** — untyped int literals default to
  `i64`.
- **Trait/impl conformance** — missing-method/arity errors, warns on extra methods.

### 06/11/26
- **Match arm validation + exhaustiveness** for boolean, tuple, and named-struct scrutinees;
  duplicate/overlapping arms.

### 06/10/26
- **Type checks:** division/modulo by literal zero; always-true/false conditions; float
  `==`/`!=` warning; for-loop condition must be `bool`; null-coalescing operand types; range
  operand types; for-in iterable must be iterable; tuple/array literal element types.
- **Unused function parameters; unused imports (`lyra-W004`).**
- **Diagnostic codes** attached to all diagnostics; **better parser error ranges** from CST
  ERROR/MISSING nodes.

### 06/09/26
- **Unreachable code; unused variables** (`TagUnnecessary`).
- **`_`-prefixed identifier grammar fix.**
- **`DiagnosticTag` + related-information support** in diagnostics.

### 06/04/26
- **Context checks:** `await`/`break`/`continue`/`yield`/`return` outside their valid context.
- **LSP:** document symbols, inlay hints, go-to-definition.

### 06/03/26
- **LSP: hover.**
- **Type checks:** member access; higher-order/non-identifier callees; unresolved identifiers;
  undefined functions; unknown type names; index access.

### 06/02/26
- **Regex engine:** Unicode properties (`\p{…}`) and performance (SIMD/DFA); wired into
  `RegexLiteralExpr` + `PatternConstraint`.

### 06/01/26
- Various bugfixes and improvements.

### 05/31/26
- **Integer-literal overflow/range checking; duplicate-declaration detection; shadowing
  detection; regex lookarounds.**

### 05/27–29/26
- **Match exhaustiveness** for array/string/number/data scrutinees.
- **Regex engine Phase 1** — derivative DFA (intersection, complement, flags,
  `IsMatch`/`FindAll`).

### 05/16–20/26
- **Struct-literal + record-update type-checking; function-declaration type-checking; if/else
  type-checking; scope-aware variable resolution.**

### 05/13–15/26
- **Comparison / boolean operators; string concatenation (`++`); type conversion; int-as-float
  literals.**
- **Added the typechecker; added the LSP server** (wired to the VS Code extension).
- **Collected:** arena statements, regex, pointers, negation, var reassignment, data
  constructors, unsafe blocks, compose/yield/generators; `@sizeof`.

### Earlier (01–05/26)
- **Grammar/collector foundation:** trait decls + impls, function/lambda types, modules +
  imports, match expressions, if-let destructuring forms, tuple/struct/array/data destructuring,
  postfix expressions, array literals + comprehensions, range expressions, tuple types,
  constrained types (range/precision/literal/step/pattern), for/for-in loops with labels,
  math-assignment operators, character literals, function guards + bodies, `i8`/`i16`/`f32`/etc.
- **Collector refactored** into subpackages; tests moved to the golden-print harness.

## Backend lowering log (07/10 – 07/17)

The LLVM backend built up in slices, each landing end to end (emit → clang → run → check
the exit code) rather than as a layer. Kept as one list because that is what it is: the
order the language became executable in.

- `pkg/driver.Analyze(source) → Result` — one call returning the typed program + all tables +
  normalized diagnostics (the backend's input).
- `pkg/backend` interface + `pkg/backend/llvm` skeleton; `cmd/lyrac check`/`build` on top.
  `build` emits a placeholder `main` module that compiles with `clang` and runs — toolchain path
  proven.
- Entry point: top-level `let main = () -> u8` (exit code) or `-> void`
  (`driver.ResolveEntryPoint`, build-time only). `u8`, not i64 — see the 07/11 "u8 entry point"
  Completed entry for why.
- **[07/11]** `github.com/llir/llvm` (v0.3.6, pure Go) set up: `Emit` builds a real `ir.Module`,
  `layout.go` returns llir types, runtime shims declared. Emitted IR compiles + runs. Note:
  typed pointers (`i8*`), not opaque. Lowering order: types → trivial `main` → expressions →
  control flow → runtime shims (`print`, overflow trap).
- **[07/10]** `cmd/lyra-lsp` migrated onto `driver.Analyze` — the pipeline lives in one place
  now.
- **[07/10]** Layout helpers in code (`runtime.go`, `layout.go`): runtime-shim `declare`s
  (`emitRuntimeDeclarations`, wired into `Emit`), `SharedBoxType`, `TagType`, `DataUnionType`,
  and a `SizeAndAlign` engine (shared = ptr-sized). Ready for `lowerType` to call.
- **[07/11]** First lowering — `lowerEntry`/`lowerExpr` lower an integer-literal body, so `let
  main = () -> i64 => 42` exits 42 (`=> 7` → 7, `-> void` → 0). Unsupported bodies error loudly.
  Next: `let`/`if`/blocks.
- **[07/11]** Arithmetic — `+ - * / % %% -(unary)` all lower and are tested behaviorally
  (compile+run+check exit code, since IR isn't constant-folded). Signed vs unsigned `Div`/`Rem`
  chosen via a new `IsSignedInt` helper. **Decided (matches Odin's `%`/`%%` split):** `%` (Mod)
  is truncated — sign follows the dividend, exactly LLVM's native `srem`/`urem` (`11 % -3 = 2`).
  `%%` (Remainder) is floored — sign follows the divisor (`11 %% -3 = -1`), needing a branchless
  `select`-based sign-fixup after `srem` (`lowerFlooredSRem`); unsigned floored remainder is
  identical to truncated (nothing to floor), so `urem` covers both. Integer negation lowers as
  `sub 0, x` (LLVM has no dedicated int negate); float negation uses `fneg`. Also fixed a real
  gap: `inferExprType` only cached a type when a specific case called `Set` itself, so a bare
  literal used directly as a binary operand had no TypeTable entry — split into a caching
  wrapper + `inferExprTypeUncached` so every non-nil result is cached.
- **[07/11]** Int-width conversions — Lyra's one conversion syntax (`i8(x)`, `u32(x)`, …,
  Pit-of-Success #5) now lowers to `trunc`/`sext`/`zext`/identity, picked from the source's
  signedness (`IsSignedInt`) and the already-lowered operand's own bit width. This is the only
  way to exercise a non-i64 width in valid source today (bare literals default to i64; no
  implicit narrowing/widening between concrete int types). Verified with overflow-wrap cases
  (`u8(200)+u8(100)` → wraps mod 256) and a division-based test that actually distinguishes
  `sext` from `zext` (an additive check can't — the two candidate values for a negative narrow
  source always differ by exactly 256, invisible mod 256 in an exit code). **Float-target/source
  conversions deliberately deferred** — confirmed no valid, type-checked program can reach that
  path today (`main` must return u8, no `float→int` builtin), so it errors explicitly rather
  than shipping an untestable instruction sequence.
- **[07/11]** `if`/`else` + blocks lowering — `lowerIf` builds the standard cond-br → then/else
  → merge-phi diamond; `lowerBlock` lowers a block to its last statement's value (only
  `ExpressionStmt` for now — no `let` yet). `lowerExpr` now returns *(value, endBlock, err)*: a
  branching form moves the insertion point, so callers keep lowering into the block control ends
  in (every non-branching case returns its block unchanged). Phi predecessors use the block each
  branch *ends in* (thenEnd/elseEnd), verified with nested-if tests where a branch's control
  moves into an inner merge block. Conditions are bool literals for now (comparisons/`&&`/`||`
  not lowered → no non-constant conditions yet); `-O0` keeps the branch so it's still exercised
  at runtime. Tests: `llvm_test.go` `TestExec_If`.
- **[07/11]** Typechecker: a one-armed `if` used as a *value* is now an error ("`if` used as a
  value must have an `else` branch") — it has no result when the condition is false. As a
  *statement* it's still fine (conditional side effect). Correctly requires a terminal `else` on
  an `if…else if…` chain in value position. `checkIfExpr`; tests in `expr_if_test.go`. (Prereq
  for the backend `if` lowering, which can now assume both branches exist.)
- **[07/13]** `for` loops lowering (backend) — the C-style cond/body/post/exit CFG with a
  back-edge (`lowerForLoop`), plus `break`/`continue` (a `loops []loopCtx` stack on the lowerer;
  labeled break/continue walk it), and one-armed `if` statements (needed for `if cond { break
  }`). Introduced the block-termination discipline the earlier lowerings didn't need:
  `lowerBlockStmts` stops at a sealed block and every fall-through `br` is guarded by `end.Term
  == nil`, since break/continue are the first constructs that terminate a block mid-stream.
  `lowerBlock` split into a value-optional `lowerBlockStmts` + a value-requiring wrapper +
  `lowerForEffect` (loop/one-armed-if bodies need no value). Tests: `TestExec_ForLoop`
  (accumulator, break, continue, nested labeled break).
- **[07/13]** Three-clause `for var i = 0; i < n; i += 1` form now type-checks and lowers end to
  end. Fixed the two frontend gaps: (a) `MathAssignOpExpr` (`+=`) got an `inferExprType` case
  (delegates to `checkMathAssignOp`, result `void`), so a `+=` in value position (the loop
  `Post`, or a block's last statement) no longer hits "unknown expression type";
  `checkMathAssignOp` also now propagates the target's width onto the RHS (`i += 1` with a
  narrow `i`). (b) `checkForLoopExpr` enters the loop's own registered scope (`RecordScope(loop,
  loopScope)`) around the init/condition/post/body checks, so the init variable resolves
  everywhere (and the init-clause condition is now genuinely checked, not skipped). Backend
  lowers `MathAssignOpExpr` as load/op/store (`lowerMathAssignOp`, reusing the extracted
  `applyIntMathOp`). Tests: `TestExec_ForLoopThreeClause` (upward, `+=`-in-body, downward `-=`,
  narrow-u8 counter) + typechecker tests. **[FIXED 07/29]** a `let`/`var` declared *inside* a
  loop body wasn't visible there — the collector puts body-locals in a child block scope keyed
  to the original body pointer, but `ForLoopExpr.Body` was a value copy so `inferBlockType`'s
  `enterScope` couldn't reach it. Both loop bodies are pointers now; see the Completed entry.
- **[07/14]** User-defined functions + calls + recursion (backend). Two-pass `Emit`: every
  top-level `let name = <lambda>` is `declareFunction`'d (signature into `l.funcs`) before any
  body, so a call from main, between functions, or a recursive self-call resolves;
  `defineFunction` then lowers each body. Per-function state reset via `beginFunction` (fresh
  `locals`/`loops` + `retType`/`retSigned`/`entryABI`); params bind as entry-block allocas keyed
  by name (like `let`/`var`). `emitReturn` is the single return path (coerces to the declared
  width; main's `entryABI` does the u8→i32 ABI slot), shared by explicit `return` (`ReturnStmt`,
  sealing its block via the break/continue discipline) and the implicit tail return; `main`
  (`lowerEntry`) now routes through it too. Calls lower via `lowerFunctionCallExpr` (resolve
  `l.funcs`, lower args, `NewCall`); args pass un-coerced because the typechecker propagates
  each param's width onto its literal args (the sixth `propagateLiteralType` site,
  `inferLambdaCall`). Scalar params/returns only (`lowerType` handles `PrimitiveType`).
  **Deferred, loud errors:** void/multi-clause functions, default params, destructuring params,
  higher-order (lambda-value) calls. Tests: `TestExec_Functions`.
- **[07/13]** `let`/`var` bindings + identifier reads + `var` reassignment lowering — locals
  modeled as entry-block `alloca` + store/load (mem2reg builds SSA), name→alloca in
  `lowerer.locals` (`lowerVarDecl`/`lowerVarReassignment`, `IdentifierExpr` in `lowerExpr`).
  Prereq typechecker fix: a same-scope rebind's initializer (`let x = x + 1`) now types its
  self-reference against the prior binding (`VarDeclStmt.Shadows`) instead of leaving it nil.
- **[07/11]** Comparisons + `&&`/`||` lowering — the six comparison ops lower to a single `icmp`
  with the right signed/unsigned predicate (`eq`/`ne` signedness-agnostic, incl. bools);
  `&&`/`||` short-circuit via a cond-br + phi diamond (`a && b` ≡ `if a { b } else { false }`,
  `||` the mirror), reusing the `if` machinery — the constant branch is virtual (a phi edge, no
  block), so only one rhs block + merge are created. Enables non-constant `if` conditions (`if x
  < 3 && y > 0`). Comparisons are int-only (float + mixed-width deferred → explicit error, not
  invalid IR). Tests: `TestEmit_BoolBinaryOp`, `TestExec_BoolShortCircuit`,
  `TestEmit_ComparisonMixedWidth_Error`.
- **[07/15]** Type-declaration lowering (backend) — top-level `tuple`/`struct` decls lower to
  named LLVM struct types in two passes (`lowerTypeDeclarations` then `lowerTypeDefinitions`,
  mirroring functions): `declareNamedStruct` registers an empty placeholder per decl (keyed by
  declared name) before any body, so fields may reference other named types in any order incl.
  forward refs (`struct Line { a: Point }` → `%Line = type { %Point, %Point }`). `lowerType` now
  resolves named refs (`TupleType`/`NamedStructType`/`UnresolvedType` → the registered struct).
  Fields lower by value; `shared`/boxed is deferred to ALLOCATION.md lowering.
  `data`/`newtype`/constrained decls error loudly. **Instances (construction, field access) are
  not lowered yet** — this is only the type shapes. Tests: `llvm_typedecl_test.go`.
- **[07/15]** Grammar: positional tuple access (`pair.0`) — new `tuple_index_expr` postfix rule
  (`tree-sitter-lyra/include/expressions/postfix.js`), distinct node from `member_expr` (index
  is `decimal_int`, not an identifier). No float collision (tree-sitter's context-sensitive
  lexer never offers `float_literal` after `obj .`), so even nested `pair.0.1` lexes as two
  indices. Grammar + corpus only (`test/corpus/expressions/postfix.txt`);
  collector/typechecker/backend wiring for tuple instances is the follow-on.
- **[07/15]** Tuple instances end to end (collector → typechecker → backend). New
  `TupleIndexExpr` AST node (collector `collectTupleIndexExpr` off `tuple_index_expr`, parsing
  the index). Typechecker `inferTupleIndexExprType` resolves the object to a `TupleType`,
  bounds-checks the index, returns the element type (named + anonymous tuples;
  out-of-range/non-tuple errors). Backend lowers construction (`lowerTupleLiteralExpr` → undef +
  `insertvalue` per element) and access (`lowerTupleIndexExpr` → `extractvalue`) as first-class
  struct SSA values, so a `let`-bound tuple round-trips through the alloca/store/load path;
  `lowerType` gained an anonymous-tuple structural case (`lowerAnonymousTupleType`); a
  data-constructor literal (`Some(42)`) still errors loudly (DataType, not a tuple). Tests:
  `llvm_tuple_test.go` (exec + IR), `typechecker/tests/tuple_index_test.go`, `collector/tests`
  golden. **Struct instances (construction + field access) are still the next step.**
- **[07/16]** Struct instances (backend) — construction + field access.
  `lowerStructInstanceExpr` builds the declared struct via undef + `insertvalue`, keying literal
  fields by name and building in *declared* order (out-of-order literals lower correctly);
  `lowerMemberExpr` reads a field via `extractvalue`, finding the field's position from the
  object's declared struct type (`namedStructFields`, which resolves an `UnresolvedType` field
  via `res.SymbolTable.Types` so nested `line.start.x` works). Typechecker fix:
  `inferStructInstanceExpr` now propagates the declared field width onto an untyped literal
  field value (8th `propagateLiteralType` site) — without it a `u8` field's `3` stayed
  `untyped_int` and lowered at i64, mismatching the i8 field. Deferred, loud errors:
  record-update (`P { base | f: v }`), default-valued missing fields, inline-record data
  constructors. Tests: `llvm_struct_test.go` (exec incl. nested + through-call, IR,
  record-update-deferred), `typechecker/tests/struct_field_width_test.go`. **Next: `match` and
  `data` layout.**
- **[07/16]** `data` type-declaration layout (backend) — a `data` decl now lowers (in the same
  two-pass type-decl machinery) to its tagged-union struct `%T = { iTAG, [K x iA] }` via
  `lowerDataDef` + the existing `DataUnionType`/`SizeAndAlign` helpers: an `i8` tag
  (declaration-order variant indices) followed by a payload blob sized/aligned to the largest
  variant. Enum → `{ i8 }`; positional/mixed payloads → blob at the widest variant; recursive is
  finite because the recursive field is `shared` (pointer-sized, lyra-E014). **Layout/shape only
  — construction and `match` are the next slices.** Deferred, loud error: a not-yet-sizeable
  payload (`string`, un-monomorphized generic, or a by-value named-type ref stored as an
  `UnresolvedType` that `SizeAndAlign` can't size — a *recursive* `shared` ref is fine). Tests:
  `llvm_data_test.go` (emit layout, clang-validity, by-value-payload deferred).
- **[07/16]** `data` value construction (backend) + closed the by-value-payload sizing gap.
  **Sizing gap:** `resolveForLayout` deep-resolves a payload's `UnresolvedType` leaves through
  the symbol table (short-circuiting a `shared` ref, which also keeps it finite), so a by-value
  named-type payload (`Wrap(P)`) now lays out. **Construction** (`lowerDataConstruction`):
  materializes the union through memory (per DATA_LAYOUT.md) — alloca, store the tag, and for a
  payload variant `getelementptr` the blob + `bitcast` to the payload struct + `store` — then
  loads it back as a first-class value; nullary (`DataConstructorExpr`) and positional
  (`TupleLiteralExpr` recording a `DataType`) both lower. Added
  `types.DataTypeConstructor.FieldTypes()` (unwraps the collector's single-anonymous-tuple
  param) — used by both the backend and a new typechecker propagation site (9th) so a narrow
  ctor arg (`Wrap(200)` with `u8`) takes the field width instead of promoting to i64. Deferred,
  loud error: `shared` payload construction (needs ref-counted alloc), inline-record data
  constructors. **`match`/destructuring is the remaining step** (a constructed value can't be
  read back yet). Tests: `llvm_data_test.go` (exec construction, IR shape, shared/by-value
  cases), `typechecker/tests/data_ctor_width_test.go`.
- **[07/16]** `match`/destructuring on a `data` value (typechecker + backend). **Typechecker:**
  match-arm pattern variables are now bound in the arm body (`checkMatchExpr` →
  `withPatternBindings` → `walkDestructuredPattern`, reusing the `paramTypes` map, which also
  records each resolved identifier into the TypeTable); `bindDataPatternPayload` rewritten to
  accept both flat (`Rect(w, h)`) and single-tuple-param (`MkPair((x, y))`) forms via
  `FieldTypes()`. Arm-body width propagation added (10th `propagateLiteralType` site + new
  `MatchExpr`/`IfExpr`/`BlockExpr` recursion) so a bare `0` arm adapts to a concrete sibling or
  the declared return type instead of defaulting to i64. **Backend**
  (`lowerMatch`/`lowerDataMatch`): store scrutinee → load tag → `switch` per arm; a data-pattern
  arm `bitcast`+`load`s the payload struct out of the blob and binds fields (`extractvalue` →
  alloca → `l.locals`); `_`/identifier arm is the switch default; arms feed a merge phi;
  exhaustive matches get an `unreachable` default. This closes the observability loop —
  construct → match → extract → return. Deferred, loud errors: arm guards, non-`data` scrutinee,
  tuple-payload destructuring, nested payload sub-patterns. Tests: `llvm_match_test.go` (exec:
  enum/single/multi/wildcard/identifier/inline; IR; guard+non-data deferred),
  `typechecker/tests/match_binding_test.go`.
- **[07/16]** Value-testing `data` payload sub-patterns (backend) — `Some(0)`. A single tag
  `switch` can't route two same-tag arms to different payload tests, so: nested in an aggregate
  (`(c, Some(0))`), `aggPatternTest`'s `DataPattern` case ANDs the tag check with a branchless
  test per value-testing payload field (extract the payload, recurse — harmless when the tag
  mismatches, the AND is already false); top-level (`match m { Some(0) => .., Some(x) => .. }`),
  `lowerDataMatch` detects a payload test (`dataMatchHasPayloadTest`) and falls back to the
  shared if-else ladder (`lowerAggregateMatch`) instead of the `switch`, keeping the compact
  `switch` for the no-test case. Only arm guards and string/float/array scrutinees remain
  deferred. Tests: `llvm_match_test.go` (`TestExec_DataLiteralPayload`,
  `TestEmit_DataLiteralPayloadIR`, nested cases in `TestExec_NestedDataPatterns`).
- **[07/16]** Floats (backend) — literals, arithmetic, comparisons, conversions, params/returns.
  Float literals lower at their recorded width (`literalFloatType`, default f64);
  `applyFloatMathOp` covers `fadd`/`fsub`/`fmul`/`fdiv`, `frem` (`%`), and a `select`-based
  floored `frem` (`%%`, `lowerFlooredFRem`); `fneg` was already there; `lowerFloatComparison`
  emits `fcmp` (ordered, `une` for `!=`); `lowerNumericConversion` gained int→float
  (`sitofp`/`uitofp`) and float-widening (`fpext`); `emitReturn` handles a float return.
  `lowerMathBinaryOpExpr`/`lowerMathAssignOp`/`lowerBooleanBinaryOpExpr` dispatch on the
  operand's LLVM type. At the time, a float reached the u8 exit code only through a comparison
  (no float→int conversion); explicit rounding (`floor`/`ceil`/`round`) landed the next day —
  see the 07/17 entry. Tests: `llvm_float_test.go` (arithmetic, conversions, float function,
  IR), all compiled + run via clang.
- **[07/16]** Strings (backend) — literals, equality, `match`, params/returns. Representation
  decided: immutable fat pointer `{ i8* data, i64 len }` (byte length, not NUL-terminated;
  `StringLLVMType`, spec in `STRING_LAYOUT.md`). Literals intern their bytes in a private
  immutable global + build the struct from a `getelementptr` + length (`lowerStringConstant`, no
  allocation). `==`/`!=` are branchless `len_eq && memcmp(min)==0` (libc `memcmp`, lazily
  declared; `lowerStringEquality`/`lowerStringComparison`). String `match` joins the scalar
  ladder (`stringScalarMatchTest`, literal arms = byte-equality, identifier binds the fat
  pointer; escaped/regex patterns deferred). By-value params/returns (`emitReturn` aggregate
  path). At the time, concatenation `++` and interpolation were deferred (need a heap
  allocator); `++` landed 07/17 — see that entry. Still deferred: interpolation (value→string
  formatting), `print`, escaped/regex patterns. Tests: `llvm_string_test.go`
  (equality/function/match/IR/deferred), all built + run via clang.
- **[07/16]** Float scalar `match` (backend). `lowerScalarMatch` and the `lowerMatch` dispatch
  now accept a float LLVM type, not just integer; `scalarMatchTest` delegates a float scrutinee
  to `floatScalarMatchTest` (`fcmp oeq` for a literal, ordered two-sided range check for a range
  arm, `constFloatFromExpr` folding float/int/negated bounds). Identifier catch-alls bind the
  float; guards work unchanged; a float match always needs a wildcard (typechecker warns
  otherwise). Only string/array match scrutinees remain deferred. Tests: `llvm_float_test.go`
  (`TestExec_FloatMatch` literal/wildcard/range/binding/f32/guard, `TestEmit_FloatMatchIR`).
- **[07/16]** Match arm guards (typechecker + backend) — `Some(x) if x > 0`. Typechecker checks
  each guard condition with the pattern's bindings in scope and requires `bool`
  (`checkMatchExpr`); guarded arms already didn't count toward exhaustiveness. Backend
  `lowerGuardedArmBody` evaluates the guard after the pattern matches and its vars are bound,
  cond-branching to the body (true) or the next arm (false); it plugs into `lowerScalarMatch`
  and `lowerAggregateMatch`, a guarded arm never seals the ladder, and a `data` match with any
  guard takes the ladder fallback (`matchHasGuard`) instead of the `switch`. Only
  string/float/array scrutinees remain deferred for `match`. Tests: `llvm_match_test.go`
  (`TestExec_MatchGuards` across data/scalar/struct/tuple, `TestEmit_MatchGuardIR`),
  `typechecker/tests/match_binding_test.go`.
- **[07/17]** Float→int rounding (typechecker + backend) — `x.floor()`/`.ceil()`/`.round()`.
  Registered as float-receiver-only builtins (`typechecker/builtins.go`'s `floatRoundingOps`,
  zero args, fixed `i64` return — narrow further via `i32(x.floor())`), the explicit escape
  hatch the numeric-conversion error already pointed to. Backend: this is also the first *method
  call* (`MemberExpr` callee) the LLVM backend lowers at all — `lowerFunctionCallExpr` now
  dispatches a `MemberExpr` callee to `rounding.go`'s `lowerBuiltinMethodCall`, which resolves
  the receiver's Lyra float type, calls the matching lazily-declared `llvm.<op>.<width>`
  intrinsic (`round` = half-away-from-zero, C/Rust-style, not `rint`/`nearbyint`), then
  `fptosi`s to i64. Out-of-range/NaN is left as ordinary `fptosi` poison (no range check —
  matches arithmetic's still-deferred checked-by-default). Tests:
  `typechecker/tests/builtin_rounding_test.go`, `llvm_float_test.go` (`TestExec_FloatRounding`,
  `TestEmit_FloatRoundingIR`).
- **[07/17]** Ref-counted heap runtime + string concatenation `++` (backend). The runtime
  (`runtime.go`, `ensureRCRuntime`) is now emitted as **real function definitions into the
  module** — `lyra_rc_alloc` (malloc + rc=1), `lyra_rc_retain` (rc+=1, `PinnedRC` no-op),
  `lyra_rc_release` (rc-=1, `drop_fn(payload)`+`free` at 0, pinned no-op) — built on libc
  `malloc`/`free` declared like `memcmp`, so there's no runtime object to link and `clang
  out.ll` stays self-contained (the old `declareRuntime` dead externs are gone). Emitted lazily
  (a non-allocating program carries none). First consumer: `lowerStringConcat` — a concatenated
  string heap-allocates a box (`rcAllocPayload` → `lyra_rc_alloc(header+total)`), `memcpy`s both
  operands into the payload, and returns a fat pointer `{ box+8, la+lb }`; empty operands
  (`memcpy` n=0) and chains (`a ++ b ++ c`, left-associated fresh box per step) just work.
  Interpolation stays deferred (now on value→string formatting, not the allocator). Tests:
  `runtime_test.go` (white-box: rc transitions, pinned no-op, `drop_fn` path, clean
  free-at-zero), `llvm_string_test.go` (`TestExec_StringConcat` —
  literals/empties/chain/param-strings/match-a-heap-string, `TestEmit_StringConcatIR`,
  `TestEmit_NoRuntimeWhenUnused`), plus an end-to-end `lyrac build` + `clang` run. (Freeing
  landed same day — see the ownership entry.)
- **[07/17]** Ownership model — heap strings are freed (`pkg/analyzer/ownership` + backend
  retain/release). **Representation:** every string value is a box, so retain/release are total
  — a **literal** interns as a *pinned* static box `{ i64 PinnedRC, [N x i8] }` (`data` at
  `box+8`, no allocation, retain/release no-op on it), a `++` value is a heap box. **Pass**
  (`pkg/analyzer/ownership`): ARC over managed (string) values — a binding/`own`-param holds one
  owning ref released at scope exit; the pass computes `Retain` (borrowed value → owning slot: a
  copy, an owned `return`, an `own` arg) and `ReleaseTemp` (owned temporary → borrowing slot:
  `==`/match/`++` operand, discarded stmt, borrowed arg), mirroring the typechecker's
  `paramOwnsArgument`/`isOwnedReturn`; an `if`/`match` is one merged owned value (branches
  coerced to +1), released once at the phi. Safety-biased: unresolvable callees + aggregate
  elements *transfer* (leak-safe), never release. **Backend** (`ownership_lower.go` +
  `lowerExpr`/`emitReturn`): a managed-frame stack releases bindings at scope exit (a `return`
  releases all live frames before it seals); retains apply at production; each temp is released
  in the block it was produced in (so an `&&`/`if`-branch temp is freed there, dominating its
  uses, not at a non-dominating merge). Verified memory-safe under **AddressSanitizer**
  (`TestExec_OwnershipASan`). **Still leaking conservatively (safe):** strings inside
  aggregates, and bindings on break/continue paths. Tests:
  `pkg/analyzer/ownership/ownership_test.go` (pass decisions), `llvm_ownership_test.go`
  (behavioral + ASan + IR retain/release balance), `llvm_string_test.go`
  (`TestEmit_StringLiteralIsPinnedBox`).
