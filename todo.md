## To-Dos
---------

### A Note to Claude: Please don't make the completed to-do items so verbose. Ideal length is 1-2 sentences. Look at items done on 06/11/26 or earlier as examples.

### Pit of Success — language design (ordered by importance)
These make the *safe/correct* path the *easy/default* path, and force the unsafe path to be loud and explicit. Listed highest-leverage first.

1. **Must-use `Result`/`Maybe` + a `?` propagation operator** — *Right now the easy thing (ignore a returned error) is the wrong thing.*
   - **TODO — Canonicalize Result/Maybe (stop name-matching)**: `?`, must-use, and (planned) the `??` restriction (#3) still recognize Result/Maybe **by name** at the usage site (`resultOrMaybeKind` matches `ParameterizedType.Name`) — there's no module system, so a true *coexisting* clash (two different `data Result`s in scope at once) can't happen yet; that part really is "not urgent" until a prelude/module system exists. **Partial (06/22/26):** `resultOrMaybeKind` now also confirms the *declared* shape, when a program declares its own `data Result`/`data Maybe` — exactly two single-payload `Ok`/`Err` (or single-payload `Some`/zero-payload `None`) constructors with the right generic arity — via `hasCanonicalResultOrMaybeShape`. A same-named-but-differently-shaped `data Result<a,b> = Foo a | Bar b` no longer gets `?`/must-use treatment. Still open: a *real* identity independent of the declaration's shape (a `SymbolTable`/`@builtin`-marker style identity), needed once there's any chance of two declarations coexisting.
   - **TODO — Error-type conversion for `Result?` propagation**: today `?` requires the same kind only and does **not** compare the `E` in `Result<T,E1>` vs the enclosing `Result<_,E2>`. Once a conversion trait exists, check `E1`→`E2` convertibility (à la Rust's `From`) instead of ignoring it; see the TODO note in `inferTryExpr`.

2. **Checked arithmetic by default; wraparound must be explicit** — `a + b` on two `i8` wraps silently at runtime. This contradicts the determinism goal behind `fixed<I,F>`.
   - **TODO — Trapping-by-default runtime semantics + explicit `wrapping_add`/`saturating_add`** — blocked on a **backend** (there's no codegen/runtime yet, so a trap on overflow can't be emitted). The wrapping/saturating methods also need *somewhere to live*, but that's builtin-method registration, **not a prelude** (see #1: canonical identity ≠ stdlib) — don't let it imply a stdlib is the prerequisite. Lean: methods, not a `wrapping { ... }` block — local and greppable at the exact op, matches "unsafe path must be loud" better than a block that silently changes semantics for everything inside it, and composes with `checked`/`saturating` variants (Rust's model). No grammar change needed (ordinary method calls).
   - **TODO — Range/interval analysis for non-constant operands** — catching `a + b` on two concrete `i8` *variables* needs a value-range abstract-interpretation pass; a blanket "any small-int arithmetic might overflow" warning would fire on essentially every expression (too noisy). Separate, larger pass (would also subsume div-by-zero and always-true-condition checks). Not the constant-fold slice above.

5. **One conversion syntax; lossy conversions must be loud** — *Widening syntax is already settled: `f32(x)` is the one form (decided 06/14/26).* The todo's old premise was stale — there is **no `x as f32` cast** in the grammar (`as` is reserved but wired only to `import_alias`), so there were never "two ways"; only `f32(x)` exists, and `inferTypeConversion` (`typechecker.go`) already **errors** (not silently truncates) on float→int, narrowing float (`f64→f16`), and non-numeric. So the remaining work is NOT picking a widening form — it's that lossy/narrowing has **no escape hatch at all** today (you literally can't express an intentional `f64→f16`). Give narrowing a distinct, named, loud spelling so it's *expressible but explicit*. **Decision: methods, not a cast keyword** — `x.truncate()` / `x.saturate()` / `x.narrow()` — to stay consistent with #2's "methods, not a `wrapping {}` block" choice (local, greppable at the op, composes with `wrapping_add`/`saturating_add`, no grammar change). **Blocked on the same dependency as #2**: the methods need somewhere to live (builtin-method registration — NOT a prelude; see #1: canonical identity ≠ stdlib). Until then `f32(x)` correctly hard-errors on narrowing, which is a safe (if strict) interim state.

8. **Consistency cleanups (lower-stakes pit-of-failure removal)**
   - **Product-type choice — guidance (decided 06/19/26).** Lyra keeps all of these on purpose; they sit at different points on "does this grouping need a name, and named fields?", not "N ways to do one thing." Pick by this rule of thumb:
     - **Alternatives (a sum)? → `data`.** For each variant's payload:
       - no payload → bare constructor (`None`, `Red`)
       - positional payload → `C i64` / `C (i64, i64)`
       - named-field payload, **one-off** (only built/matched through this variant) → **inline record** `C { field: T, … }`
       - named-field payload you'll **name, reuse across variants/types, pass around, or `impl` on** → reference a `struct`: `C MyStruct`. *Rule: start inline; promote to a named struct the moment the payload earns a name — same instinct as extracting a function.*
     - **Always all fields together (no alternatives)? → a product.**
       - fields benefit from names → `struct` (the default for records)
       - purely positional but you want a distinct nominal type → named `tuple Foo(...)`
       - ad-hoc / local / structural, no name needed → anonymous tuple `(a, b)`
   - **Inline vs. struct-reference are genuinely different** (anonymous-to-the-variant vs. own reusable named type), mirroring Rust's struct-variants vs. variants-wrapping-a-struct — so both stay.
   - **Inline-record construction (FIXED 06/19/26).** `data Tree = Nil | Node { left: i64, value: i64 }` then `Node { left: 1, value: 2 }` now type-checks. `inferStructInstanceExpr` (typechecker.go) falls back, when `expr.Name` isn't a `symTable.Types` struct, to `findInlineRecordConstructor(Name)` — which finds the constructor whose single payload is an `AnonymousStructType` and returns its fields + the owning data type. The fields are checked with the existing struct-literal machinery (missing/extra/mismatched fields all reported), the data type's generic params drive the same inference as a generic struct (`data Box<t> = Wrap { value: t }`; `Wrap { value: 42 }` infers `t`), and the literal evaluates to the data type. Struct-reference construction (`C(MyStruct { … })`) already worked via the constructor-call path. Tests: `data_constructor_call_test.go` (`TestInlineRecordConstructor_*`).
   - **OPTIONAL / UNDECIDED — consider removing the `given` keyword.** `X given { let a = …; let b = … }` (postfix where-clause, Haskell-style — body first, supporting bindings after) is pure sugar for a plain block, which Lyra already has: `{ let a = …; let b = …; X }`. The parse trees are equivalent (`given_expr{body, bindings}` vs `block{decls…, expr}`); the only difference is ordering/emphasis (result-first vs result-last). So it's a keyword + grammar rules (`given_expr`, `given_bindings`, `PREC.GIVEN`, the `given` reserved word) earning their keep only on the "punchline-first" reading. **Not decided** — keep for now; revisit if trimming surface area. If removed, every `X given { B }` mechanically rewrites to `{ B; X }`.

### Functional / Imperative blend — enforce the pure/mutable split (ordered)
Goal: give the FP half real *guarantees* (determinism, referential transparency, safe auto-parallelism) while keeping ergonomic in-place mutation for games. Current state: `pure` is parsed (`LambdaExpr.IsPure`) but **unenforced** — it's a no-op annotation; `ref`/`mut`/`own` modifiers are parsed but unchecked at *parameter* boundaries. Safe aggregate mutation (`p.x = v`, `arr[i] = v`) **is now expressible and checked** for local bindings via the three-level `let` / `let mut` / `var` model (#2 below, done 06/15/26); the remaining teeth are purity enforcement and the `ref`/`mut`/`own` *parameter* modifiers.

**Guiding model:** purity = *no observable effect crossing the function boundary* (NOT "no mutation happens"). Local mutation of owned values inside a `pure` function is allowed and encouraged (the ST-monad / Rust-`fn` / "functional but in-place" trick) — that's what lets pure code stay fast. The membrane is one-way: pure may not call impure; impure may freely call pure. The `ref`/`mut`/`own` modifiers are the machinery that tells the checker whether a mutation escapes (`own`/`ref` ok in pure, `mut` borrow = external effect = forbidden in pure). Frame the whole thing as *the compiler's license to memoize, reorder, and auto-parallelize* (ECS systems as pure functions over component arrays → job-system scheduling with no data-race analysis) — that's the games payoff that justifies the work.

1. **Purity-enforcement checker pass** (`checker/purity.go`) — walk each `pure` lambda body; error on observable effects: reassignment/math-assign/deref-assign of anything not declared locally within the function, calls to non-pure functions, `await`/I/O/effectful builtins, and (later) mutation through a `mut`-borrowed parameter. Mirror the `CheckAwaitOutsideAsync` enclosing-context walk. Landed in `checker/purity.go` and **wired into the LSP** (`cmd/lyra-lsp/main.go` analyze pass; verified end-to-end over stdio). Start strict-but-simple, expand the effect surface incrementally. Diagnostic code `lyra-E007` (impure op in pure fn). Demo fixture: `lyra-vscode-ext/test/purity.lyra`.
2. **Safe mutable lvalues — three-level binding model** (DONE 06/15/26; see Completed) — *the binding keyword answers "may the **name** be rebound?", the `mut` modifier answers "may the **interior** change?", giving three useful levels instead of the old single axis:*
   - `let x` — frozen name, **deeply immutable** interior (nothing inside ever changes, transitively).
   - `let mut x` — frozen name, **mutable interior**. This is JavaScript's `const` (`const obj` blocks `obj = …` but allows `obj.x = …`) and is the *common* mutable case — it grants the least, so prefer it over `var`.
   - `var x` — mutable name **and** interior (reassign the whole binding *and* mutate fields).
   *Rationale (vs. the old two-level `let`-deep / `var`-mutable plan): forcing any field mutation all the way to `var` over-granted (you also got name reassignment you didn't ask for). Lyra's **deep** `let` is exactly what opens this gap — the `let`→`var` jump is a cliff from "nothing mutates anywhere" to "everything mutates + rebindable"; `let mut` is the missing middle rung. JS leans `const`-heavy because that keyword is the shallow/off-diagonal one; the instinct points at `let mut`, not `let`.* Interior mutation is now expressible without `unsafe`. Nested structs work end-to-end (the pre-existing `inferMemberExprType` / struct-literal `UnresolvedType` non-resolution that broke `l.start.x` and `Line { start: Point{…} }` was fixed alongside, 06/15/26). **Still deferred:** wiring `mut`/`own` **parameter** paths into this check (that's FP/Imperative #4 below — today a path rooted at a non-local binding is left permissive).
3. **Purity inference** — infer purity bottom-up over the call graph so `pure` is the cheap default and the keyword becomes a *checked assertion*, not a tax. A function that only calls pure functions, mutates only locals, and does no I/O is pure; record it on the symbol so callers can use it. *Partial:* `inferImpureLambdas` in `purity.go` infers impurity of function bindings at **any** nesting depth transitively (fixpoint, keyed by lambda pointer) so a `pure` fn calling a user-defined impure fn — sibling or top-level — is flagged; reading a mutable (`var`/`let mut`) binding captured from any enclosing scope is flagged too (referential transparency). Both resolve names via a capture stack of `scopeBindings` frames threaded through the walk (done 06/19/26, extended to non-top-level scopes 06/21/26). Still to do: impurity of methods and imported functions, symbol-table-backed scope info (vs. the current AST-walk reconstruction), and making *pure* (not just impure) the recorded default.
4. **Wire `ref`/`mut`/`own` into the typechecker** (DONE 06/15/26; see Completed) — parameter modifiers now constrain interior mutation: bare/`ref` = immutable borrow (can't mutate), `mut` = mutable borrow (can), `own` = owned local (can). The purity check treats interior mutation through a `mut` param (and through captured bindings) as an escaping effect; `own`/local mutation stays allowed. **Still deferred:** `ref`/`mut`/`own` on *non-parameter* positions (return types, struct fields) and using the modifiers to drive copy/move/borrow semantics once a backend exists.
5. **Generalize `pure` to a coarse effect row** — once 1–4 exist, model effects as a set `{io, alloc, rand, time, mut}` where `pure` == empty set. Lets you express "deterministic but allocates" (valuable for netcode/replay even when not fully pure) instead of a single boolean. Keep immutable data as plain owned **value-semantics** types (copy/move/COW), not heap-allocated persistent structures — stays layout-friendly for `fixed<>`/packed/arena.
6. **Use-after-move check for `own` parameters** — **Decision (06/15/26): `own` is a move that *consumes* the caller's binding** (not a pass-by-value copy). Rationale: it's the one non-redundant mode (copy-`own` ≈ `ref` + an explicit local copy; move is the only way to get single-ownership/zero-cost transfer), a silent copy would violate "expensive path must be loud" (keep copying an explicit op), and it's the right model for single-owner resources (`shared`/`weak`/arena) + the aliasing guarantee auto-parallelism needs. **The teeth this requires:** a definite-move analysis that errors on *use-after-move* — reading or re-passing a binding after it was moved into an `own` parameter. Sibling of the definite-assignment pass (Pit-of-Success #6); flow-sensitive. Until it lands, `own` is safe-but-incomplete (front-end allows it; no backend means no actual move yet), so this is **not urgent** — natural trigger is a backend with real move/copy codegen.

## In Progress
--------------

## Completed
------------

### 06/22/26
- **Result/Maybe shape validation** (Pit-of-Success #1) — `resultOrMaybeKind` (`typechecker_try.go`) now checks a declared `data Result`/`data Maybe`'s actual constructors (`hasCanonicalResultOrMaybeShape`), not just its name+arity, so a same-named-but-differently-shaped type no longer gets `?`/must-use treatment. Programs that never declare Result/Maybe at all (the common case, no prelude) are unaffected. Tests: `try_expr_test.go`, `mustuse_test.go`.
- **Confirmed generic type params are lowercase-only by design** (not a bug) — investigated an apparent "generic data types lose params/drop constructors" issue; root cause is that Lyra's type params are ML-style lowercase (`<t, e>`), and a single uppercase letter (`T`, `E`) lexes as `const_identifier`, not a type name, so `<T, E>` mis-parses. Fixed two typechecker tests that relied on the invalid uppercase form by mistake; added a clarifying grammar comment. No grammar behavior change — decided to keep lowercase-only rather than take on `const_identifier`-precedence surgery to support uppercase.

### 06/21/26 (continued)
- **Purity: impurity inference for non-top-level functions** (`checker/purity.go`, FP/Imperative #3 slice) — calling an impure sibling function is now flagged regardless of nesting depth, not just top-level bindings. `inferImpureFunctions`/`topLevelFunctions` became `inferImpureLambdas`/`collectFuncBindings`, which walk the whole program once recording each lambda literal together with the capture stack (`scopeBindings`, now holding both mutability *and* function bindings per frame) visible at its definition site, then run the same fixpoint over that — keyed by lambda pointer rather than name, so same-named functions in unrelated scopes are never confused with each other. Tests: `purity_test.go` (`TestPurity_CallsNonTopLevelImpureFunction_Error`, `TestPurity_CallsSiblingDeclaredAfter_Error`, `TestPurity_NameCollisionAcrossScopes_NotConfused`).

### 06/21/26
- **Purity: track captured mutable bindings from non-top-level enclosing scopes** (`checker/purity.go`, FP/Imperative #3 slice) — reading a captured `var`/`let mut` is now flagged regardless of how many function scopes out it was declared, not just top-level globals. A capture stack of per-lambda mutability frames (`directScopeMutability`) is threaded through the walk, pushed at every lambda boundary (pure or not) and searched innermost-out, so a closer `let` of the same name correctly shadows a farther mutable one. Tests: `purity_test.go` (`TestPurity_ReadsNonTopLevelCapturedVar_Error`, `TestPurity_ReadsCapturedVarThroughIntermediateScope_Error`, `TestPurity_ShadowedImmutableLocal_Ok`).

### 06/19/26
- **Purity: reject reading captured mutable globals** (`checker/purity.go`, FP/Imperative #3 slice) — a `pure` function reading a top-level `var`/`let mut` binding now errors (`lyra-E007`): its value can change between calls, breaking referential transparency. Assignment-target roots are suppressed to avoid double-reporting writes (a mutable global used as an array *index* is still flagged), and `inferImpureFunctions` treats such a read as impure so it propagates to callers. Tests: `purity_test.go` (`TestPurity_ReadsCaptured*`, etc.). Still deferred: mutable state captured from a non-top-level enclosing scope (needs scope info).

### 06/17/26
- **Folding ranges** (`textDocument/foldingRange`) — `foldingrange.go` walks top-level statements for struct/data/trait declarations and expressions for `match` and block expressions, emitting a fold for each multi-line region. Auto-registers via `FoldingRangeHandler`. Tests: `foldingrange_test.go`.
- **Workspace symbols** (`workspace/symbol`) — `workspacesymbols.go` iterates all open documents in `analysisStore`, converts top-level type decls, traits, functions, and constants to `SymbolInformation`, and filters by case-insensitive subsequence fuzzy match. Auto-registers via `WorkspaceSymbolHandler`; plain `var` bindings excluded as too noisy. Tests: `workspacesymbols_test.go`.
- **Signature help** (`textDocument/signatureHelp`) — `cmd/lyra-lsp/signaturehelp.go` scans the source prefix backwards from the cursor to find the innermost unclosed `(`, counts top-level commas for the active parameter index, resolves the callee name via scope lookup to a `LambdaExpr`, and builds a `SignatureInformation` with `[start, end]` byte-offset label ranges per parameter. Auto-registers via `SignatureHelpHandler`; `Initialize` advertises `(` and `,` as trigger/retrigger characters. Tests: `signaturehelp_test.go`.
- **Rename** (`textDocument/rename` + `textDocument/prepareRename`) — `rename.go` reuses `referenceOccurrence`/`resolveDeclLocation`/`walkExprs` from `references.go`; collects all scope-aware occurrences (declaration + usages) and returns a `WorkspaceEdit` with one `TextEdit` per occurrence; `PrepareRename` validates the cursor is on a bound identifier and returns its range/placeholder. Both capabilities auto-register via the `RenameHandler`/`PrepareRenameHandler` interfaces. Tests: `rename_test.go`.
- **Document highlight** (`textDocument/documentHighlight`) — `documenthighlight.go` reuses `referenceOccurrence`/`resolveDeclLocation` from `references.go`; always includes the declaration site; auto-registers via `DocumentHighlightHandler`. Tests: `documenthighlight_test.go`.
- **`@sizeof` on unknown types** — added `case *ast.SizeofExpr` to `inferExprType`: calls `resolveType` on the type argument (emits `unknown type %q` if unresolved) and returns `u64`. Tests: `sizeof_test.go`.

### 06/16/26
- **Fixed keyword carving in identifiers** (grammar) — raised `IDENTIFIER_TOKEN` -1→0 so keywords stop being lexed out of longer names (`letter`→`let`+`ter`, `mutable`→`mut`+`able`, `matcher()`, `weakness`). Needed three zero-regression compensations: float exponent `[eE]` precedence, a data-ctor payload alias for bare lowercase params (`Some t`), and correcting two pre-existing malformed-syntax tests (array-comp arrow→`in`, trait default `+`→`++`). Corpus + Go suite green.
- **Semantic tokens** (`textDocument/semanticTokens/full`) — `cmd/lyra-lsp/semantictokens.go` classifies both declaration names and usages: variable/parameter/function/type (`readonly` modifier for `const` and immutable `let`), member-access property, data constructor (`enumMember`), and struct-literal type name, delta-encoded for the client. Walks statements (decl names) + expressions (usages, lambda params); usages classified via scope resolution. Auto-registers via `SemanticTokensFullHandler`; `Initialize` carries the legend. Added `NameLocation` (tagged `print:"-"`) to `VarDeclStmt`/`TypeDeclStmt`/`TraitDeclStmt` so decl names have precise spans; the AST printer now skips `print:"-"` fields (keeps goldens location-free). Also fixed `collectMemberExpr` to give `MemberExpr.Property` its own location. Tests: `semantictokens_test.go`.
- **Completion** (`textDocument/completion`) — `cmd/lyra-lsp/completion.go` offers field names after `.` (receiver chain resolved via scope→struct fields, source-text prefix scan so a broken `p.` parse still works) and, otherwise, every identifier in the scope chain plus all declared type names, each classified to an LSP item kind. Auto-registers via the `CompletionHandler` interface; `Initialize` advertises `.` as a trigger character. Tests: `completion_test.go`.
- **Find references** (`textDocument/references`) — `cmd/lyra-lsp/references.go` returns every occurrence resolving to the same binding as the identifier under the cursor; matching is scope-aware (per-occurrence `findScopeAtPos`+`Scope.Lookup`, compared by declaration location) so shadowed/sibling same-named bindings are excluded, and `IncludeDeclaration` adds the decl. Handles `IdentifierExpr` and `...spread`; capability auto-registers via the `ReferencesHandler` interface. Unblocks Rename + Document highlight. Tests: `references_test.go`.

### 06/15/26
- **Code actions / quick fixes** (`textDocument/codeAction`) — `cmd/lyra-lsp/codeaction.go` offers four fixes off the diagnostics in range plus a range-driven refactor: "Add missing match arms" (lyra-E009; reuses exported `typechecker.MissingMatchConstructors`, emits `Ctor _` for payload constructors / bare `Ctor` for nullary), "Add missing struct fields" (new code lyra-E013, dedupes the per-field diags into one edit), "Remove unused variable/import" (lyra-W003/W004; whole-line deletion, import removal only when the whole statement can go), and "Insert inferred type annotation" for unannotated `let`/`var` (reuses the inlay-hint inferred type). Capability auto-registers via the `CodeActionHandler` interface. Tests: `codeaction_test.go`.
- **Fixed: nullary constructor swallowed the following statement** (grammar) — `let c = None\nmatch c {…}` parsed as `None(match …)` because `data_constructor_expr`'s argument was the full `expression` set and Lyra has no statement terminators. Restricted the argument to a new `_constructor_value` = atomic/primary value forms only (literals minus `anonymous_struct_literal`, numbers, postfix, nested constructor, negation, group, address-of, sizeof, array-comp); control-flow/block/binary-op forms are excluded, so application now binds tighter than binary ops (`Some 42 ?? d` = `(Some 42) ?? d`) and `match`/`if`/`{…}` after a nullary stay separate statements. `Some 42` / `Some foo(x)` / `Err -1` still construct. Corpus: `test/corpus/expressions/data_constructor.txt`. **Residual:** a nullary binding immediately followed by a bare *call/identifier* expression-statement (`let c = None\nfoo()`) is still swallowed — postfix args can't be distinguished from a following call without statement terminators (separate, larger effort).
- **`const` requires a compile-time-constant initializer** — `checkConstInitializer` (`typechecker_const.go`, `lyra-E012`) rejects a `const` whose value isn't a literal, another `const`, or an expression built purely from those (unary, math/boolean binary, `++`, array/tuple of constants); reports the first non-constant sub-expression. Tests: `const_initializer_test.go`.
- **Unsafe operations outside `unsafe` require an `unsafe` context** — new `checker/unsafe_outside_unsafe.go` pass (`lyra-E011`) flags a raw-pointer take (`&x`), deref (`p^`), pointer write (`p^ = v`), or a call to an `unsafe` function when not inside an `unsafe { }` block or `unsafe` function body. Mirrors the `CheckAwaitOutsideAsync` enclosing-context walk; unsafe-ness resets at each function boundary (a plain lambda inside an `unsafe` block is its own safe context). Wired into the LSP. Tests: `unsafe_outside_unsafe_test.go`; demo `lyra-vscode-ext/test/unsafe.lyra`.
- **Wire `ref`/`mut`/`own` parameter modifiers into mutation/purity checks** (FP-blend #4) — a parameter's modifier now governs interior mutation: bare and `ref` are immutable borrows (`p.x = v` errors), while `mut` (mutable borrow) and `own` (owned local) permit it. Typechecker tracks per-param modifiers (`tc.paramMods`) and `checkLValueAssignment` consults them before the scope lookup; `checkBlockVoidReturn` now also dispatches body statements through `checkNode` (previously interior mutation inside a void body was never checked). The purity pass (`checker/purity.go`) treats interior mutation through a `mut` param — and through any captured binding — as an escaping effect (new `LValueAssignmentStmt` handling), while `own`/local mutation stays pure. Tests: `interior_mutation_test.go` (param section), `purity_test.go`; demo `lyra-vscode-ext/test/purity.lyra`.
- **Struct field mutability: default mutable + `readonly` freeze marker** — struct fields are now mutable by default (a field follows the mutability of the binding holding the struct); a field declared `readonly id: u64` is **frozen** — writable once at construction, immutable forever after, even through a `var`/`let mut` instance, for invariant fields. Composition is intersection (most-restrictive-wins): `p.f = v` is legal iff the root binding permits interior mutation **and** the field isn't `readonly`; the `let`-binding row stays absolute. `readonly` is also **deep** (can't mutate *through* a readonly struct-typed field). Grammar: struct_member `var` keyword replaced by `optional(field("frozen","readonly"))` (the old `var` field keyword was parsed-but-ignored); `readonly` added to the reserved-identifier list. `types.StructField.Frozen`; collector sets it; `checkFrozenFieldPath` (typechecker) walks member hops. Tests: `interior_mutation_test.go`, `collector/tests/let_mut_test.go`, corpus `types/struct.txt`.
- **Fixed: named-struct field types weren't resolved** — a struct field declared with another named-struct type is stored as an `UnresolvedType` (just the name), so nested member access (`l.start.x`) mis-reported "member access on non-struct type Point" and a nested struct literal (`Line { start: Point{…} }`) mis-reported "cannot assign Point to Point". Fix: `inferMemberExprType` now `resolveType`s the object type before the struct switch, and the struct-literal field check resolves both expected/actual before `isAssignable`. Genuine mismatches still reported. Surfaced by the nested-struct interior-mutation tests.
- **Safe mutable lvalues / three-level binding model** (pit-of-success FP #2) — added `let mut` (frozen name, mutable interior; = JS `const`) as the middle rung between deeply-immutable `let` and fully-mutable `var`, and made `p.x = v` / `arr[i] = v` / `grid[i].y = v` expressible (new `member_assignment`/`index_assignment` grammar rules → `LValueAssignmentStmt`). The typechecker (`checkLValueAssignment`) walks the path back to its root binding and rejects interior mutation unless that binding is `var` or `let mut` (a plain `let` is deeply immutable, even several hops down). `let mut` still forbids reassigning the name. `var mut` warns as redundant. Grammar: `optional(field("mutability","mut"))` on both declaration branches; `VarDeclStmt.IsMut` + `CanMutateInterior()`. Tests: `typechecker/tests/interior_mutation_test.go`, `collector/tests/let_mut_test.go`, corpus `assignments.txt`. Deferred: `mut`/`own` param-path wiring (FP #4) and the nested-struct-field type-resolution gap in `inferMemberExprType`.

### 06/14/26
- **Allow same-scope sequential rebinding** (pit-of-success #7) — `let x = parse(x)` now compiles: the blocker was the *collector*'s hard error in `var_decl.go` (not `shadowing.go`, which already only warns on nested-scope shadowing); same-scope re-declaration of a `let`/`var` is now allowed silently and re-registers via new `RedefineVariable` so later references resolve to the newest binding, while `const` still may not be re-declared. Tests: rewrote `duplicate_declaration_test.go`, added `typechecker/tests/rebind_test.go`.
- **Require initialization at declaration** (pit-of-success #6, interim) — `checkVarDecl` (`typechecker.go`) now errors (`lyra-E010` `CodeUninitializedDeclaration`) when a `let`/`var` has no initializer, instead of silently accepting it. Closes the use-of-uninitialized hole the simple way (no flow analysis); allowing uninitialized `var` behind a definite-assignment pass is left for later. For-loop `Init` is unaffected (it's checked via `inferBlockType`, not `checkVarDecl`). Tests in `var_decl_test.go`. (Surfaced and then fixed the nullary-constructor collector bug below.)
- **Fixed: nullary data constructors as values were being dropped** — `let c = None` / `let c = Red` silently lost the initializer. It was a *collector* bug, not grammar (the parse correctly tags `field=value user_defined_type_name`): `CollectExpression` had no `user_defined_type_name` case and returned nil. Added one (`collectNullaryConstructorExpr` → `DataConstructorExpr` with nil `Value`); the typechecker already resolves these by constructor name. Also fixed the same drop for static-call objects (`Arena.new()`'s object was nil). Tests: collector golden `expr_data_constructor_nullary`, updated two `with_statement` goldens.
- **One conversion syntax decided** (pit-of-success #5) — settled `f32(x)` as the single widening form (no `as`-cast exists; the "two ways" premise was stale). `inferTypeConversion` already hard-errors on lossy conversions. Remaining work (a loud named spelling for intentional narrowing — `.truncate()`/`.saturate()`/`.narrow()` methods) is blocked on builtin-method registration, same as #2. See todo #5 for details.
- **Non-exhaustive `match` on closed types is now an error** (pit-of-success #4) — `checkMatchExpr` (`typechecker_control_flow.go`) emits `SeverityError` (new code `lyra-E009` `CodeNonExhaustiveMatch`) instead of a warning when a `match` on `bool` or a `data`/sum type leaves cases uncovered; open types (numeric, string, array, tuple, struct) keep the ignorable warning. Author opts out with an explicit `_ =>`. Bool/data exhaustiveness tests renamed `_Warning`→`_Error` and switched to `assertErrorsAre`; VS Code `match.lyra` fixture comments updated.
- **Restrict `??` to optional (`Maybe<T>`) operands** (pit-of-success #3) — `inferNullCoalescingExpr` (`typechecker_control_flow.go`) now requires the left operand to be a `Maybe<T>`: nullability stays expressible *only* through `Maybe<T>`, never as an ambient property of every type (closing the billion-dollar-mistake side door where `let x: i64 = 0; x ?? y` taught that any value is nullable). On a `Maybe<T>` left operand the `??` unwraps to the payload `T`, then unifies with the default via `branchCommonType` (so `lookup() ?? default` checks the payload against the default, not the whole `Maybe`). A non-optional left operand can never be null → warning `lyra-W007` (`CodeNonOptionalCoalescing`); we recover by treating the left type as the payload so one mistake doesn't cascade into a spurious incompatible-type error. Recognition reuses `resultOrMaybeKind` (same by-name `Maybe` matching as the `?`/must-use work — stop-gap until canonical identity). Updated the VS Code demo fixture `lyra-vscode-ext/test/null-coalescing.lyra` to teach the Maybe-only semantics. Tests rewritten in `tests/null_coalescing_test.go`. Full suite green.

### 06/13/26
- **Must-use `Result`/`Maybe`** (pit-of-success #1, first half) — a statement that produces a `Result`/`Maybe` and drops it (no binding, no `match`, no `?`) now emits warning `lyra-W006`. Implemented in the **typechecker**, not a standalone `checker/` pass as the TODO suggested: the check is fundamentally type-dependent and the typechecker is the only pass holding the inferred `TypeTable`. `typechecker_mustuse.go` `checkMustUseResult` inspects the inferred statement-expression type via `resultOrMaybeKind` (same by-name recognition as the `?` checks — mis-fires on a user `data Result` until a prelude exists). Wired into `checkExpressionStmt` for the `FunctionCallExpr` and `TryExpr` cases (covers `foo()`, `obj.method()`, and a `?` whose unwrapped payload is itself a Result/Maybe). Control-flow statement forms (`if`/`match`/loops) are deliberately not flagged to avoid noise. **Opt-out is `let _ = expr`** (or any binding), NOT the `_ = expr` the TODO proposed — bare `_ =` does not parse (`_` alone isn't a valid lvalue), and any binding form is a `VarDeclStmt` that never reaches the must-use check, so no grammar change and no separate suppression were needed. **Latent bug fixed along the way:** `checkBlockReturn` (function bodies) only type-checked the *last* `ExpressionStmt` (the implicit return); non-final expression statements were never checked at all — now routed through `checkExpressionStmt` (matches `inferBlockType`'s per-statement behavior). New diag code `CodeUnusedResult = "lyra-W006"` and a `addErrorCode` helper (addError with an explicit code). LSP needs no change — typechecker diagnostics already map `te.Code` via `codeToLSP`. Tests: `tests/mustuse_test.go`. Full suite green.

### 06/12/26 (continued)
- **Subtraction parser bug fixed** — `let x = 0 - 200` was dropping the `- 200` (the bare integer-literal left operand of `-` reached `expression` via the `prec.right(PREC.LITERAL)`-wrapped `_literal`, so precedence resolved the literal/operand choice toward a unary `negation` statement). Fix: route `_number_literal` directly into `expression`/`_bool_operand`/`_comparison_operand`, bypassing the wrapper (kept for composite literals vs patterns), plus a `[$.expression, $.literal_pattern]` conflict. No dynamic precedence; `Err -1` still parses as construction. Corpus regression test added; full Go suite + corpus green.
- **Constant-folded arithmetic overflow** (pit-of-success #2, static slice) — `extractIntLiteralValue` (`typechecker/overflow.go`) now recursively folds `+`/`-`/`*` over integer-constant operands using overflow-checked int64 ops (`checkedAdd/Sub/MulInt64`); an int64-range overflow during folding is treated as non-constant (no wrapped-value message). The existing `checkIntegerLiteralRange` sites now flag `let x: i8 = 100 + 100`. Div/mod not folded. Tests in `tests/overflow_test.go`. Runtime trapping-default + `wrapping_add`/`saturating_add` deferred (need a backend + prelude); non-constant `a + b` overflow deferred (needs range analysis).
- **`?` (try) propagation operator** (pit-of-success #1, second half) — `checker/try_outside_result.go` (`lyra-E008`) flags `?` outside a Result/Maybe-returning fn; `typechecker/typechecker_try.go` `inferTryExpr` requires a Result/Maybe operand, enforces same-kind propagation, and returns the unwrapped payload. Result/Maybe matched by name for now. Also fixed a pre-existing collector bug where `parseParameterizedType` dropped every generic return/annotation type (`-> Result<i64,E>`) to `nil`.
- **Removed platform-dependent `int`/`uint` and the bare `float` type** (pit-of-success / determinism) — `int`/`uint` removed from the grammar (`number_types.js` `signed_integer_type`/`unsigned_integer_type`) and from `pkg/types` (`Int`/`UInt` `PrimitiveTypeName` constants deleted); `IsNumeric`, `numericPrimitiveByName`, `isAnyConcreteSignedInt`/`Unsigned`, and `integerFitsInType` updated. Untyped integer-literal default promotion now resolves to `i64` (`promoteToDefault` in `typechecker.go`), preserving the 64-bit range a platform `int` had. There was never a bare `float` keyword in the grammar; example `.lyra` files used it as an unresolved type — migrated all `float` → `f64`, `int` → `i64`, `uint` → `u64` in the example fixtures (code only, comment prose preserved). All Go test sources/expected messages and the collector golden files migrated and regenerated (`UPDATE_GOLDEN=1`); full suite green. `.zed/rules.md` (root + `lyra/`) primitive-type listings updated.

### 06/12/26
- **Trait/impl conformance** — `checkTraitImpl` in `typechecker/`; (1) errors for each required (non-default) trait method absent from the impl, (2) arity check: clause pattern count must match trait signature parameter count, (3) warning for impl methods not declared in the trait; `Traits map[string]*ast.TraitDeclStmt` added to `SymbolTable` and populated during collection; `collectPatternParameters` bug fixed: now handles concrete pattern node kinds (tree-sitter inlines the `pattern` rule) so `LambdaClause.Patterns` is correctly populated for multi-clause functions and trait impl clauses; golden files updated

### 06/11/26 (continued, part 2)
- **Boolean match exhaustiveness** — `checkBoolMatchArm` validates only `true`/`false` literal patterns, wildcards, identifiers; `boolMatchIsExhaustive` requires both arms or a wildcard; non-bool literals emit errors
- **Tuple match arm validation** — `checkTupleMatchArm` validates arity and element patterns; requires wildcard for exhaustiveness
- **Named-struct match arm validation** — `checkStructMatchArm` validates struct pattern field names against the declared struct; requires wildcard for exhaustiveness; `resolveToNamedStructType` follows `UnresolvedType` indirection
- **Duplicate/overlapping match arms** — `checkDuplicateMatchArms` detects identical unguarded literal patterns (any scrutinee type) and overlapping `RangePattern` intervals (numeric types); duplicate literals are errors; range overlaps are warnings

### 06/10/26 (continued)
- **Division/modulo by literal zero** — `inferMathBinaryExpr` checks if operator is `/`, `%`, or `%%` and RHS is an integer/float literal with value 0; emits error `operator X: division by zero`; variable divisors pass through unchecked
- **Always-true/always-false conditions** — `checkIfExpr` checks if the condition is a `*ast.BooleanLiteralExpr` and emits a warning `condition is always true/false`; grammar restricts for-loop conditions to binary expressions so bare literals there are not reachable at runtime
- **Float `==`/`!=` comparison warning** — `checkBooleanBinaryOpExpr` emits a warning when either operand of `==` or `!=` is a float type (including untyped float literals and concrete f16/f32/f64)

### 06/10/26
- **For loop condition must be `bool`** — `checkForLoopExpr` added; validates `&&`/`||` operands in conditions that reference outer-scope variables; init-clause loops skip condition checking (scope table pointer-copy limitation documented); `ForLoopExpr` and `NullCoalescingExpr` wired into `checkExpressionStmt` and `inferExprType`
- **Null coalescing types** — `inferNullCoalescingExpr` infers both `??` operands and unifies via `branchCommonType`; emits "null coalescing operands have incompatible types" when they don't match
- **Range expression operand types** — `inferRangeExpr` validates both ends are numeric and compatible; step (if present) must also be numeric; returns `RangeType`; `RangeType.GetName()` fixed to handle nil fields
- **For-in iterable must be iterable** — `checkForInLoopExpr` validates the iterable is array, string, or range; emits error for numeric/bool/struct iterables; `isIterableType` helper covers all valid cases
- **Tuple literal element type checking** — `inferTupleLiteralExpr` now stores each element's type in `TypeTable`; `checkDestructuringDecl` catches non-tuple RHS and arity mismatches for tuple patterns
- **Array literal element type homogeneity** — `inferArrayLiteralType` infers element type as the common type of all elements via `branchCommonType`; errors on mismatches (e.g. `[1, "two", true]`)
- **Unused function parameters** — same as unused variables, scoped to the lambda body
- **Unused imports** — `CheckUnusedImports` in `checker/`; walks all top-level `ImportStmt` nodes and warns for any member name/alias, module alias, or plain last-path-component that never appears as an `IdentifierExpr` in the program; emits `lyra-W004` with `TagUnnecessary`; `_`-prefixed names silently ignored
- **Diagnostic codes** — attach a stable code (e.g. `lyra-E001`, `lyra-W014`) to each `TypeError` / `ShadowingWarning` / `UseBeforeDeclarationError`; map into the LSP `Diagnostic.Code` field
- **Better parser error ranges** — walk the tree-sitter CST for `ERROR`/`MISSING` nodes and report them with real source ranges instead of the current `lsp.Range{}` (line 0:0) fallback

### 06/09/26
- **Unreachable code** — `CheckUnreachableCode` in `checker/`; scans each block for `return`/`break`/`continue` terminators and emits `TagUnnecessary` warnings for any statements that follow in the same block; recurses into all nested scopes including lambda bodies
- **Unused variables** — `CheckUnusedVariables` in `checker/`; checks each `BlockExpr` scope for `VarDeclStmt`/`DestructuringDeclStmt` declarations that have no identifier reference; emits `TagUnnecessary` warnings; conservative ref collection includes nested lambdas to avoid false positives on closures; top-level bindings are skipped; `_foo`-prefixed names are silently ignored (conventional "intentionally unused" marker)
- **`_`-prefixed identifier grammar fix** — tree-sitter grammar now preserves the leading `_` on identifiers in all contexts: identifier regex updated to `(_[a-zA-Z0-9_]+|[a-z][a-zA-Z0-9_]*)`, `decimal_int` regex tightened to `/[0-9][0-9_]*/` (require leading digit), and argument-list partial-application wildcard `_` given `PARTIAL_WILDCARD_TOKEN: -2` priority (below identifier) so `print(_x)` parses `_x` as a single identifier token
- **`DiagnosticTag` support** — `Tag` type with `TagUnnecessary`/`TagDeprecated` constants added to `pkg/diagnostic`; `Tags []Tag` field added to `Diagnostic` and `TypeError`; `tagsToLSP()` helper in LSP server maps tags to `lsp.DiagnosticTag` for collector and typechecker diagnostics so VS Code renders them greyed-out or struck-through
- **Related information** — `Diagnostic.RelatedInformation` and `TypeError.RelatedInformation` fields added; shadowing warnings carry `OriginalLocation`; duplicate variable declarations and duplicate struct fields populate related info pointing to the prior declaration; LSP `analyze()` maps related info via `toLSPRelatedInfo` for all diagnostic sources

### 06/04/26
- **`await` outside an async function** — `CheckAwaitOutsideAsync` in `checker/`; mirrors `CheckYieldOutsideGenerator`; `LambdaExpr.IsAsync` now gates await validity
- **`break`/`continue` outside a loop** — `CheckBreakContinueOutsideLoop` in `checker/` walks with loop-depth counter and label set; errors at depth 0 or for unknown labels
- **`yield`/`yield from` outside a generator function** — `CheckYieldOutsideGenerator` in `checker/`; also fixed `is_generator` → `is_gen` field name in lambda collector so `LambdaExpr.IsGenerator` is now correctly populated
- **`return` outside a function body** — `CheckReturnOutsideFunction` in `checker/` walks the AST with a depth counter; errors at depth 0
- **Document symbols** (`textDocument/documentSymbol`) — walk top-level statements and emit symbols for type decls (`struct` → Struct, `data` → Enum, constrained → Class), traits (Interface), lambda-valued `let`/`var` bindings (Function), `const` bindings (Constant), and other bindings (Variable); powers the breadcrumb/outline view
- **Inlay hints** (`textDocument/inlayHint`) — show inferred types inline for unannotated `let`/`var` bindings (e.g. `: int`) using `TypeTable`
- **Go-to-definition** (`textDocument/definition`) — resolve the name under the cursor via `Scope.Lookup` / `SymbolTable.Types` and return its `Location`

### 06/03/26
- **LSP: Hover** (`textDocument/hover`) — `Handler.Hover()` added; persists `docAnalysis` (program, symTable, typeTable) per URI; `findExprAtPos` walks the AST depth-first to find the tightest expression at the cursor; typechecker now stores IdentifierExpr types in TypeTable. Doc-comments not yet surfaced (not collected from CST).
- **Member access type-checking** — `*ast.MemberExpr` routed through `inferExprType` via `inferMemberExprType`; validates object is a struct, field exists, records field type in `TypeTable`.
- **Higher-order and non-identifier callees** — `inferFunctionCallExpr` now handles `LambdaExpr` (direct lambda calls like `((n) => n*2)(5)`), variables holding lambdas, and `MemberExpr` callees (method calls like `obj.method(args)`). Added `inferLambdaCall`, `inferLambdaCallFromType`, `inferLambdaExprType`, and `inferMemberExprType` helpers. Member access on non-struct types, unknown fields, and non-callable fields all produce errors.
- **Unresolved identifier references** — `inferExprType` silently returns `nil` when an `IdentifierExpr` lookup fails; emit `cannot find variable %q in this scope`
- **Undefined function calls** — `inferFunctionCallExpr` silently returns `nil` for unknown identifiers; emit `undefined function %q`
- **Unknown type names in annotations** — `resolveType` silently returns the unresolved type when `symTable.Types[name]` misses; emit `unknown type %q`
- **Index access type-checking** — `*ast.IndexExpr` is unhandled; index must be numeric, target must be array/string/tuple, result is the element type

### 06/02/26
- **Regex Engine Phase 3: Unicode properties** — `\p{Letter}`, `\p{Nd}`, etc. Full `\p{X}` / `\P{X}` support: general categories (L, Lu, Nd, Z, …), long aliases (Letter, Number, …), script names (Latin, Han, …), binary properties (White_Space, …); inside character classes (positional & negated); 20 new tests.
- **Regex Engine Phase 4: performance** — SIMD prefix scan, byte-frequency-based acceleration, bounded DFA fast path
- Wire the engine into `RegexLiteralExpr` (typecheck regex patterns in match arms) and `PatternConstraint` (validate constrained string types)

### 06/01/26
- Various bugfixes and improvements 

### 05/31/26
- **Overflow/range checking for integer literals** — flag literal values that don't fit the annotated type (e.g. `let x: i8 = 200`)
- **Duplicate variable declaration detection** — error when the same name is declared twice in the same scope
- **Shadowing detection** — warn when a variable is shadowed by a local variable
- **Regex engine: lookarounds** — `(?=...)`, `(?!...)`, `(?<=...)`, `(?<!...)` compiled into the automaton

### 05/29/26
- **Match expression exhaustiveness** — for `array` types, warn when a match arm is missing; type-check the scrutinee and arm patterns

### 05/27/26
- **Match expression exhaustiveness** — for `string` types, warn when a match arm is missing; type-check the scrutinee and arm patterns. Allow regex patterns
- **Match expression exhaustiveness** — for `number` types, warn when a match arm is missing; type-check the scrutinee and arm patterns. Allow range patterns
- **Match expression exhaustiveness** — for `data` types, warn when a match arm is missing; type-check the scrutinee and arm patterns. Infer data type from constructor expressions
- **Phase 1: core derivative DFA** — symbolic-byte-set expression algebra, Brzozowski derivatives, lazy DFA, intersection (`&`), complement (`~(...)`), wildcard `_`, standard syntax, multiline anchors, `(?i)`/`(?s)`/`(?m)`/`(?-m)`/`(?x)` flags, `IsMatch` + `FindAll` with leftmost-longest semantics

### 05/20/26
- for struct record update syntax, include fields in the base struct when checking for missing fields

### 05/19/26
- **Struct literal type-checking** — verify field names exist on the struct, field value types match field declarations, no missing required fields

### 05/16/26
- **Function declaration type-checking** — verify that return expressions match the declared return type; check parameter types against call-site arguments
- **If/else expression type-checking** — branches must have compatible types; condition must be `bool`
- **Scope-aware variable resolution** — variables declared inside blocks (if, for, match) should not be visible outside them; detect use-before-declaration

### 05/15/26
- **Comparison operators** — type-check `==`, `!=`, `<`, `>`, `<=`, `>=`; operands must be compatible, result is `bool`
- Added LSP server and hooked it up to vscode extension
- **String concatenation** — allow `++` on two `string` operands as a special case of binary expressions

### 05/14/26
- Removed "float" type (still have f16, f32, and f64)
- Added type converting
- Allowing int literal to be treated as a float
- **Boolean operators** — type-check `&&`, `||`, `!`; operands must be `bool`, result is `bool`

### 05/13/26
- Collect arena statements
- Collect regex
- Collect pointer syntax
- Collect negation
- Collect var reassignment
- Collect data_constructor_expr
- Collect unsafe blocks
- Collect given expr
- Collect compose expr
- Collect yield expr
- Collect yield/from expr
- Collect generators
- Add @sizeof() attr to query type sizes
- Added typechecker

### 05/11/26
- Add tests for trait declarations with default methods
- Collect math assignment operators (i.e. +=, \*=, etc)
- Collect character literals
- Collect for loops
- Add for loop labels and break/continue statements
- Collect and test for/in loops

### 04/18/26

- Collect trait declarations
- Collect trait implementations

### 04/17/26

- Collect function types (lambdas)
- Collect and test null coalescing expressions (??)
- Collect module declarations and import statements

### 04/11/26

- Test very complex nested patterns
- Collect and test postfix expressions (i.e. foo.blah[3].baz())

### 04/10/26

- Collect and test match expressions

### 04/09/26

- Collect and Test "If Let Destructuring Expressions"
- Collect and Test "If Let Else Destructuring Expressions"
- Collect and Test "If Else Destructuring Expressions"

### 03/16/26

- Collect array destructuring and write tests
- Collect struct destructuring and write tests
- Collect data type destructuring and write tests

### 03/14/26

- Refactor collect directory into sub-directories and sub-packages
- Collect and test tuple destructuring

### 03/13/26

- Collect and test array literal collection
- Collect and test if expressions

### 03/09/26

- Collect and Test struct literal collection

### 03/08/26

- Refactor collector tests to use new "capture_program_print" function

### 03/02/26

- Collect range expressions
- Collect array comprehensions
- Refactor collect_expression.go - break up into smaller files

### 02/23/26

- Collect tuple types

### 01/31/26

- Collect Array literals
- Handle i8, i16, f32, etc
- Store allocation modifiers in AST

### 01/30/26

- Add step constrained type decl
- Add pattern constrained type decl

### 01/29/26

- Add range constrained type decl
- Add precision constrained type decl
- Add literal union constrained type decl

### 01/19/26

- Parse function guards and body (expressions)
