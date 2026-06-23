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

### 06/22/26 (continued)
- **Fixed: `if let`/`let … else` were never type-checked at all** — `checkNode` had no case for these statements, so a type error inside an if-let branch passed silently. Added `checkIfDestructuringStmt`/`checkElseDestructuringStmt` (reusing `checkDestructuringDecl`), and fixed `Then`/`Else` to be pointer fields so the collector's recorded scopes resolve correctly. Tests: `if_let_else_test.go`.
- **Symbol-table-backed purity — foundation, part 2: register if-let/else names** (FP/Imperative #3 prereq) — if-let/let-else pattern names are now registered with correct scoping (if-let's local to `Then`; let-else's persisting into the enclosing scope). Tests: `scope_test.go`.
- **Purity checker: `await` is an impure effect** — `purity.go` now flags `await` inside a `pure` function (and infers impurity for functions that await), since it suspends on external I/O. Tests: `purity_test.go`.
- **Purity checker: if-let / else-destructuring / `with`-arena bindings are locals** — fixes false-positive "escaping effect" errors when a pure function mutates a name bound by one of these forms. Tests: `purity_test.go`.
- **Constant-folded division-by-zero** — `isLiteralZero` now folds constant integer expressions (e.g. `10 / (5 - 5)`), not just bare `0` literals. Tests: `math_binary_expr_test.go`.
- **Fixed: `DataPattern` as a lambda parameter mis-parsed (grammar)** — `(Some(x): Maybe<i64>) -> ...` lost to the constructor-call expression reading for lack of precedence. Added `PREC.DATA_PATTERN: 10`. Corpus + Go suite green.
- **Fixed: destructuring never bound names** — `let (a,b)=...`, `let {x,y}=p`, `let Some(x)=m`, etc. type-checked the value but never made the bound names resolvable; `walkDestructuredPattern` now handles all pattern kinds, including generic data constructors. Tests: `destructuring_test.go`.
- **Result/Maybe shape validation** (Pit-of-Success #1) — `resultOrMaybeKind` now checks a declared Result/Maybe's actual constructor shape, not just its name, so a same-named-but-different type no longer gets `?`/must-use treatment. Tests: `try_expr_test.go`, `mustuse_test.go`.
- **Confirmed generic type params are lowercase-only by design** (not a bug) — uppercase `<T,E>` mis-parses because a single uppercase letter lexes as `const_identifier`. Fixed two tests that relied on the invalid form; no grammar change.

### 06/21/26 (continued)
- **Purity: impurity inference for non-top-level functions** (FP/Imperative #3 slice) — calling an impure sibling function is now flagged regardless of nesting depth, keyed by lambda pointer so same-named functions in unrelated scopes aren't confused. Tests: `purity_test.go`.

### 06/21/26
- **Purity: track captured mutable bindings from non-top-level enclosing scopes** (FP/Imperative #3 slice) — reading a captured `var`/`let mut` is now flagged regardless of how many function scopes out it was declared. Tests: `purity_test.go`.

### 06/19/26
- **Purity: reject reading captured mutable globals** (FP/Imperative #3 slice) — a `pure` function reading a top-level `var`/`let mut` now errors (`lyra-E007`), since its value can change between calls. Tests: `purity_test.go`. Still deferred: mutable state captured from a non-top-level scope.

### 06/17/26
- **Folding ranges** (`textDocument/foldingRange`) — emits a fold for each multi-line struct/data/trait declaration and `match`/block expression. Tests: `foldingrange_test.go`.
- **Workspace symbols** (`workspace/symbol`) — returns top-level type decls, traits, functions, and constants across open documents, fuzzy-matched by name. Tests: `workspacesymbols_test.go`.
- **Signature help** (`textDocument/signatureHelp`) — resolves the active call and parameter index from the source text and the callee's `LambdaExpr`. Tests: `signaturehelp_test.go`.
- **Rename + prepare rename** (`textDocument/rename`, `prepareRename`) — renames every scope-aware occurrence of a binding. Tests: `rename_test.go`.
- **Document highlight** (`textDocument/documentHighlight`) — highlights every occurrence of the binding under the cursor. Tests: `documenthighlight_test.go`.
- **`@sizeof` on unknown types** now resolves its type argument and returns `u64`, erroring if unresolved. Tests: `sizeof_test.go`.

### 06/16/26
- **Fixed keyword carving in identifiers** (grammar) — keywords were being lexed out of longer names (`letter`→`let`+`ter`); raised `IDENTIFIER_TOKEN` precedence to fix, with a few small compensating grammar changes. Corpus + Go suite green.
- **Semantic tokens** (`textDocument/semanticTokens/full`) — classifies declarations and usages (variables, functions, types, members, constructors) for client-side highlighting. Tests: `semantictokens_test.go`.
- **Completion** (`textDocument/completion`) — offers struct field names after `.`, and all in-scope identifiers/types otherwise. Tests: `completion_test.go`.
- **Find references** (`textDocument/references`) — returns every scope-aware occurrence of the binding under the cursor, excluding shadowed/sibling same-named bindings. Tests: `references_test.go`.

### 06/15/26
- **Code actions / quick fixes** (`textDocument/codeAction`) — four diagnostic-driven fixes (missing match arms, missing struct fields, unused var/import removal) plus an inferred-type-annotation insertion. Tests: `codeaction_test.go`.
- **Fixed: nullary constructor swallowed the following statement** (grammar) — `let c = None` followed by `match c {…}` parsed as one expression for lack of a statement terminator; restricted constructor arguments to atomic value forms. Corpus updated. Residual: a nullary binding immediately followed by a bare call statement is still swallowed (separate, larger effort).
- **`const` requires a compile-time-constant initializer** (`lyra-E012`) — rejects any initializer not built purely from literals/consts. Tests: `const_initializer_test.go`.
- **Unsafe operations outside `unsafe` require an `unsafe` context** (`lyra-E011`) — flags a raw-pointer take/deref/write or an unsafe call outside an `unsafe` block/function. Tests: `unsafe_outside_unsafe_test.go`.
- **Wire `ref`/`mut`/`own` parameter modifiers into mutation/purity checks** (FP-blend #4) — bare/`ref` params are immutable borrows, `mut`/`own` permit interior mutation; purity now treats `mut`-param mutation as an escaping effect. Tests: `interior_mutation_test.go`, `purity_test.go`.
- **Struct field mutability: mutable by default, with a `readonly` freeze marker** for write-once invariant fields (deep — applies through nested struct fields too). Tests: `interior_mutation_test.go`, `let_mut_test.go`.
- **Fixed: named-struct field types weren't resolved**, breaking nested member access/literals (`l.start.x`, `Line { start: Point{…} }`). `inferMemberExprType` now resolves the type before use.
- **Safe mutable lvalues / three-level binding model** (pit-of-success FP #2) — added `let mut` as the middle rung between deeply-immutable `let` and fully-mutable `var`, making `p.x = v`/`arr[i] = v` expressible. Tests: `interior_mutation_test.go`, `let_mut_test.go`.

### 06/14/26
- **Allow same-scope sequential rebinding** (pit-of-success #7) — `let x = parse(x)` now compiles; same-scope `let`/`var` re-declaration re-registers via `RedefineVariable`, while `const` still may not be re-declared. Tests: `duplicate_declaration_test.go`, `rebind_test.go`.
- **Require initialization at declaration** (pit-of-success #6, interim) — an uninitialized `let`/`var` now errors (`lyra-E010`), closing the use-of-uninitialized hole without flow analysis. Tests: `var_decl_test.go`.
- **Fixed: nullary data constructors as values were being dropped** — `let c = None` silently lost the initializer (collector bug, missing `user_defined_type_name` case). Tests: golden `expr_data_constructor_nullary`.
- **One conversion syntax decided** (pit-of-success #5) — `f32(x)` is the single widening form; a loud spelling for intentional narrowing is still blocked on builtin-method registration.
- **Non-exhaustive `match` on closed types is now an error** (pit-of-success #4, `lyra-E009`) — `bool`/`data` matches must cover all cases or have a wildcard; open types keep the warning.
- **Restrict `??` to optional (`Maybe<T>`) operands** (pit-of-success #3) — a non-optional left operand now warns (`lyra-W007`) instead of treating every type as nullable. Tests: `null_coalescing_test.go`.

### 06/13/26
- **Must-use `Result`/`Maybe`** (pit-of-success #1, first half, `lyra-W006`) — dropping a Result/Maybe-producing statement without binding/match/`?` now warns; opt out via `let _ = expr`. Also fixed a latent bug where non-final statement expressions in a function body were never type-checked. Tests: `mustuse_test.go`.

### 06/12/26 (continued)
- **Subtraction parser bug fixed** — `let x = 0 - 200` was dropping the `- 200` due to a precedence conflict between the literal/operand choice and unary negation. Corpus regression test added; full suite green.
- **Constant-folded arithmetic overflow** (pit-of-success #2, static slice) — folds `+`/`-`/`*` over integer constants and flags overflow against the annotated type (e.g. `let x: i8 = 100 + 100`). Tests: `overflow_test.go`.
- **`?` (try) propagation operator** (pit-of-success #1, second half, `lyra-E008`) — flags `?` outside a Result/Maybe-returning function and enforces same-kind propagation. Also fixed a collector bug dropping generic return/annotation types (`-> Result<i64,E>`) to `nil`.
- **Removed platform-dependent `int`/`uint` and the bare `float` type** (determinism) — untyped integer literals now default to `i64`; all fixtures, tests, and golden files migrated (`float`→`f64`, `int`→`i64`, `uint`→`u64`).

### 06/12/26
- **Trait/impl conformance** — `checkTraitImpl` errors on missing required trait methods and arity mismatches, and warns on impl methods not declared in the trait. Also fixed `collectPatternParameters` to populate `LambdaClause.Patterns` for multi-clause functions.

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
