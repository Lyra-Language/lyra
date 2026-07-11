## To-Dos
---------

### Pit of Success — language design (make the safe path the default)

1. **Must-use `Result`/`Maybe` + `?` propagation**
   - **[DONE 07/09]** Canonical Result/Maybe identity — a `CanonicalKind` stamp (via a name-independent `@builtin` attr or an unmarked name+shape fallback) replaces per-site name matching. `@builtin` is prelude-only infra, not an app feature.
   - **[PARTIAL 07/09]** `?` now checks the operand's error type against the enclosing return (assignability-only). **Open:** From-style declared error conversion, once a conversion trait exists.

2. **Checked arithmetic by default; wraparound explicit**
   - **[DONE 07/10]** Explicit `wrapping_{add,sub,mul}` / `saturating_{add,sub,mul}` on integer receivers via the builtin-method registry (`typechecker/builtins.go`).
   - **[BLOCKED: backend]** Trap-on-overflow default for plain `+`. **[Open]** `checked_*` (returns `Maybe<T>`, needs a prelude).
   - **[TODO]** Range/interval analysis to catch overflow on non-constant `i8` *variables* — a separate value-range pass (would also subsume div-by-zero/always-true checks).

5. **Lossy conversions must be loud** — widening `f32(x)` is settled and already hard-errors on narrowing.
   - **[DECIDED]** Narrowing gets named methods `truncate`/`saturate`/`narrow` (not a cast keyword). The builtin-method home now exists (#2). **Open:** their return type is the narrower target with no argument, so it needs context-directed return-type inference (or a turbofish).

8. **Consistency cleanups**
   - **[DECIDED 06/19]** Keep `data` / `struct` / named `tuple` / anonymous tuple — they sit at different points on "does this grouping need a name + named fields?", not redundant. Rule of thumb: sum → `data` (inline record for one-off named payloads, promote to a `struct` when the payload earns a name); product → `struct` (named) / named `tuple` (positional nominal) / anonymous tuple (ad-hoc).
   - **[FIXED 06/19]** Inline-record construction (`Node { left: 1, value: 2 }` for a `data` variant) type-checks.
   - **[DONE 07/10]** Removed the `given` keyword (postfix where-clause was pure sugar for a block).

### Functional / Imperative blend — enforce the pure/mutable split

**Model:** purity = no observable effect crossing the function boundary (local mutation of owned values is fine). `ref`/`mut`/`own` tell the checker whether a mutation escapes. Payoff: license to memoize/reorder/auto-parallelize.

1. **[DONE]** Purity-enforcement pass (`checker/purity.go`, `lyra-E007`), wired into the LSP.
2. **[DONE 06/15]** Three-level binding: `let` (deeply immutable) / `let mut` (mutable interior) / `var` (mutable name + interior).
3. **[DONE]** Purity inference (bottom-up over functions + trait methods, transitive); `InferredPureFunctions` exposes it. Phase 2 (read the ScopeTable instead of re-walking the AST) done for lambdas + free functions; **method clauses still re-walk** (deferred — needs a collector change).
4. **[DONE 06/15]** `ref`/`mut`/`own` parameter modifiers constrain interior mutation and purity. **Deferred:** modifiers on non-parameter positions + driving move/copy/borrow semantics (needs a backend).
5. **[DECIDED 07/08 + landed]** Named-bound ladder `pure` ⊆ `det` ⊆ unannotated, plus orthogonal `noalloc` — grammar, collection, and enforcement all in (`lyra-E015`/`E016`); rand/time and unknown-call alloc taint detected. Raw effect-row annotation syntax ruled out until user-defined effects exist.
   - **Alloc as a storage flavor** (not nominal identity): **[DONE 07/10]** `stack`↔`shared` compatibility check (`lyra-E018`) at owning sites incl. args/`own`, returns, and tuple/array elements. **[DONE 06/24]** recursive-type well-formedness (`lyra-E014`). **[DECIDED 07/10]** backend representation — `stack` = inline value, `shared` = `ptr` to a ref-counted box `{rc, payload}`; retain/release driven by `own`/`ref`/`mut`; arena values pin the rc and bulk-free (spec: `pkg/backend/llvm/ALLOCATION.md`). **Open:** construction-site `shared T {…}` syntax; implicit-alloc / escape analysis; atomic refcounts (deferred to the job system).
6. **[TODO, BLOCKED: backend]** Use-after-move check for `own` params (definite-move analysis). `own` = consuming move (decided 06/15); not urgent until real move/copy codegen.
7. **[ROADMAP]** Explicit SIMD (`simd<T,N>` → LLVM `<N x T>`) for determinism + games: Layer 1 primitive vector type, then Layer 2 data-parallel map over `pure`/`det` component arrays (the auto-parallel payoff). SoA-for-components, distinct from `[N]T`. Sequenced after the scalar backend; spec: `pkg/backend/llvm/SIMD.md`.

## In Progress
--------------

### Backend (codegen) — target: LLVM IR (decided 07/10)
Groundwork done; this is where backend work now happens. Blocks #2 (traps), #5 (narrowing), #6 (use-after-move), and alloc representation.
- **[DONE]** `pkg/driver.Analyze(source) → Result` — one call returning the typed program + all tables + normalized diagnostics (the backend's input).
- **[DONE]** `pkg/backend` interface + `pkg/backend/llvm` skeleton; `cmd/lyrac check`/`build` on top. `build` emits a placeholder `main` module that compiles with `clang` and runs — toolchain path proven.
- **[DONE]** Entry point: top-level `let main = () -> i64` (exit code) or `-> void` (`driver.ResolveEntryPoint`, build-time only).
- **[DONE 07/11]** `github.com/llir/llvm` (v0.3.6, pure Go) set up: `Emit` builds a real `ir.Module`, `layout.go` returns llir types, runtime shims declared. Emitted IR compiles + runs. Note: typed pointers (`i8*`), not opaque. Lowering order: types → trivial `main` → expressions → control flow → runtime shims (`print`, overflow trap).
- **[DONE 07/10]** `cmd/lyra-lsp` migrated onto `driver.Analyze` — the pipeline lives in one place now.
- **[DECIDED 07/10]** `stack`/`shared` representation — inline value vs `ptr` to a ref-counted box; spec in `pkg/backend/llvm/ALLOCATION.md`.
- **[DECIDED 07/10]** `data`/sum-type layout — tagged union `{ tag, payload }` (monomorphized, niche-opt deferred); spec in `pkg/backend/llvm/DATA_LAYOUT.md`.
- **[DONE 07/10]** Layout helpers in code (`runtime.go`, `layout.go`): runtime-shim `declare`s (`emitRuntimeDeclarations`, wired into `Emit`), `SharedBoxType`, `TagType`, `DataUnionType`, and a `SizeAndAlign` engine (shared = ptr-sized). Ready for `lowerType` to call.
- **[DONE 07/11]** First lowering — `lowerEntry`/`lowerExpr` lower an integer-literal body, so `let main = () -> i64 => 42` exits 42 (`=> 7` → 7, `-> void` → 0). Unsupported bodies error loudly. Next: `let`/`if`/blocks.
- **[DONE 07/11]** Arithmetic — `+ - * / % %% -(unary)` all lower and are tested behaviorally (compile+run+check exit code, since IR isn't constant-folded). Signed vs unsigned `Div`/`Rem` chosen via a new `IsSignedInt` helper. **Decided (matches Odin's `%`/`%%` split):** `%` (Mod) is truncated — sign follows the dividend, exactly LLVM's native `srem`/`urem` (`11 % -3 = 2`). `%%` (Remainder) is floored — sign follows the divisor (`11 %% -3 = -1`), needing a branchless `select`-based sign-fixup after `srem` (`lowerFlooredSRem`); unsigned floored remainder is identical to truncated (nothing to floor), so `urem` covers both. Integer negation lowers as `sub 0, x` (LLVM has no dedicated int negate); float negation uses `fneg`. Also fixed a real gap: `inferExprType` only cached a type when a specific case called `Set` itself, so a bare literal used directly as a binary operand had no TypeTable entry — split into a caching wrapper + `inferExprTypeUncached` so every non-nil result is cached.
- **[DONE 07/11]** Int-width conversions — Lyra's one conversion syntax (`i8(x)`, `u32(x)`, …, Pit-of-Success #5) now lowers to `trunc`/`sext`/`zext`/identity, picked from the source's signedness (`IsSignedInt`) and the already-lowered operand's own bit width. This is the only way to exercise a non-i64 width in valid source today (bare literals default to i64; no implicit narrowing/widening between concrete int types). Verified with overflow-wrap cases (`u8(200)+u8(100)` → wraps mod 256) and a division-based test that actually distinguishes `sext` from `zext` (an additive check can't — the two candidate values for a negative narrow source always differ by exactly 256, invisible mod 256 in an exit code). **Float-target/source conversions deliberately deferred** — confirmed no valid, type-checked program can reach that path today (`main` must return i64, no `float→int` builtin, no `let`/blocks yet), so it errors explicitly rather than shipping an untestable instruction sequence.

## Completed
------------

### 07/11/26
- **First real lowering** — `lowerEntry` + `lowerExpr` (`llvm.go`) lower an integer-literal `main` body to a real `ret`, so `let main = () -> i64 => 42` compiles+runs to exit 42 (`=> 7` → 7; `-> void` → 0). `lowerExpr` returns an error for unhandled forms so the build fails loudly rather than emitting wrong code. Tests: `llvm_test.go` (`TestEmit_IntegerLiteralBody`/`_VoidEntry`/`_UnsupportedBody`).
- **llir/llvm set up for the backend** — added `github.com/llir/llvm` v0.3.6 (pure Go); `Emit`/`lowerEntry`/`declareRuntime` now build a real `ir.Module` instead of string assembly, and `layout.go`'s type helpers (`LLVMPrimitive`/`SharedBoxType`/`TagType`/`DataUnionType`) return llir `types.Type`. Placeholder `@main` module still compiles + runs via clang. Typed pointers (`i8*`), not opaque — fine for the scalar milestone. Tests updated to compare via `.String()`; full suite green.

### 07/10/26
- **Backend layout helpers scaffolded** — `pkg/backend/llvm/runtime.go` (shim names + `PinnedRC` + `emitRuntimeDeclarations`, now emitted into every module) and `layout.go` (`LLVMPrimitive`, `SharedBoxType`, `TagType`, `DataUnionType`, `SizeAndAlign`). `SizeAndAlign` implements C-style struct padding, static-array stride, and the sum-type union sizing, and treats a `shared` value as pointer-sized (so recursive `shared` types are finite). Emitted IR (now with the shim `declare`s) still compiles+runs. Tests: `layout_test.go` (12 cases). The type toolkit `lowerType` will call; expression/statement codegen is still the from-scratch work.
- **`data`/sum-type layout decided** (backend) — tagged union `%T = { tag, payload-blob }`: smallest-fitting int tag in declaration order, payload sized/aligned to the largest variant and accessed as the active variant's struct. Orthogonal to the alloc flavor (inline vs boxed). Monomorphized generics; recursive occurrence is `shared` (a `ptr`, finite size). Niche/tag-fold optimization (e.g. `Maybe<ptr>` = null) deferred. Spec: `pkg/backend/llvm/DATA_LAYOUT.md`.
- **`stack`/`shared` representation decided** (#5 (d)) — `stack` = inline value; `shared` = `ptr` to a ref-counted box `{rc, payload}` with retain/release driven by `own`/`ref`/`mut`, and arena values pinning the rc for bulk free. Full spec + runtime-shim signatures in `pkg/backend/llvm/ALLOCATION.md`. Non-atomic refcounts (parallel readers borrow, so no rc races); atomic/weak/COW deferred.
- **LSP migrated onto `driver.Analyze`** — `cmd/lyra-lsp`'s ~300-line inline pipeline replaced by a thin `driver.Analyze` + `diagToLSP` wrapper; pipeline now defined once. LSP suite green.
- **LLVM backend skeleton** — `pkg/backend` interface + `pkg/backend/llvm` (textual IR); `lyrac build` writes a placeholder `main` module confirmed to compile and run (exit 0).
- **Program entry-point convention** — `driver.ResolveEntryPoint`: a zero-param top-level `let main` returning `i64`/`void`; build-time only, enforced by `lyrac build`.
- **Builtin-method registration** — `typechecker/builtins.go`; `wrapping_/saturating_{add,sub,mul}` type-check on integer receivers. Primitives are now valid method receivers (missing → `T has no method "x"`).
- **Removed the `given` keyword** — retired the grammar rules, reserved word, AST node, and checker cases; corpus + suite green.
- **Purity scope Phase 2 (lambdas + free functions)** — purity reads the collector's ScopeTable instead of re-walking the AST; zero behavior change. Methods deferred.
- **Allocation-compatibility check (`lyra-E018`)** — owning a value across a `stack`↔`shared` boundary is an error at binding/reassign/lvalue sites; fires only on concrete differing flavors (`Unspecified` is polymorphic).
- **…args/returns** — E018 extended to `own` arguments and owned returns; borrowed (`ref`/`mut`) are polymorphic and skipped.
- **…element-level** — E018 recurses into tuple/array elements; fixed a pre-existing bug where named tuple/array element types weren't resolved by `resolveType`.

### 07/09/26
- **Per-trait-method effect bounds** — a trait method may be declared `pure`/`det`/`noalloc` as a contract every impl must satisfy (`E007`/`E016`; `E015` for `pure det`).
- **Bound-dispatched calls visible to purity** — a call through a `where` bound scores as the join over all impls (pure/det only if all are).
- **Impl method body return-type checking** — each impl method body is checked against the trait's declared return type (Self + trait params substituted).
- **Binding a trait's own type params** — `impl Get<t> for Box<t>`; `box.get()` on `Box<i64>` returns `i64`.
- **Bounded polymorphism in method bodies** — calling a trait method on a bare type-param value dispatches through its `where` bound (abstract dispatch).
- **Generic struct field access** — `b.value` on `Box<i64>` substitutes type args into field types → `i64`.
- **Generic impl dispatch + bound checking** — `impl Show<t> for Box<t>` unifies against a concrete receiver; `where` bounds constraint-checked.
- **`noalloc` catches unknown external calls** — an unresolvable call taints alloc too, so `noalloc` flags it.
- **ScopeTable population (Phase 1)** — collector records node→scope for lambda params, both loops, and `with` blocks.
- **Error-type checking for `Result?`** — `?` compares the operand's error type against the enclosing return (assignability-only).
- **Canonical Result/Maybe identity** — a `CanonicalKind` stamp replaces per-site name matching (`@builtin` or name+shape fallback).
- **`det` rand/time detection** — ambient `Random.global()`/`wallClock()` carry Rand/Time so `det` forbids all nondeterminism sources.

### 07/08/26
- **ML-style function sugar** — `let name(params) => body` (and modifier-led `let pure name(...)`) desugar to `let name = (params) => body`.

### 06/24/26
- **Recursive-type well-formedness check (`lyra-E014`)** — a by-value type cycle errors unless broken by `shared`.
- **Alloc as type identity (first step)** — types carry an `Allocation` flavor through the AST; `AllocationOf`/`WithAllocation` added.
- **Method-to-method call tracking** — impl method bodies are type-checked so inner `.`-calls dispatch into MethodTable, feeding the purity fixpoint.
- **Trait methods can be `pure`; real method dispatch** — `obj.method()` / `Trait::method()` resolve to an impl, type-check, and report ambiguity; recorded in a new MethodTable.
- **Purity inference for unannotated methods** — inferred via a joint fixpoint over functions + methods.
- **Impurity of imported/external functions** — unresolvable calls (not builtins/conversions) are conservatively impure.
- **Fixed: parser hang on a lambda-typed struct field** — nil-guard on an absent optional `parameter_types` node.

### 06/23/26
- **Purity inference records *pure* as queryable** — `InferredPureFunctions` exposes every top-level function's inferred purity by name.

### 06/22/26
- **Fixed: `if let`/`let … else` were never type-checked** — added the missing `checkNode` cases.
- **Purity foundation** — if-let/let-else names registered with correct scoping; if-let/else/`with`-arena bindings treated as locals.
- **Purity: `await` is an impure effect.**
- **Constant-folded division-by-zero** — folds constant int expressions, not just bare `0`.
- **Fixed: `DataPattern` as a lambda parameter mis-parsed** — added `PREC.DATA_PATTERN`.
- **Fixed: destructuring never bound names** — `walkDestructuredPattern` binds all pattern kinds.
- **Result/Maybe shape validation** — recognition checks constructor shape, not just name.
- **Confirmed generic type params are lowercase-only by design** (not a bug).

### 06/21/26
- **Purity: impurity inference for non-top-level functions** (keyed by lambda pointer).
- **Purity: track captured mutable bindings from non-top-level enclosing scopes.**

### 06/19/26
- **Purity: reject reading captured mutable globals (`lyra-E007`).**

### 06/17/26
- **LSP:** folding ranges, workspace symbols, signature help, rename + prepare-rename, document highlight; `@sizeof` on unknown types.

### 06/16/26
- **Fixed keyword carving in identifiers** (grammar) — `letter` no longer lexes as `let`+`ter`.
- **LSP:** semantic tokens, completion, find references.

### 06/15/26
- **LSP:** code actions / quick fixes.
- **Fixed: nullary constructor swallowed the following statement** (grammar) — residual: nullary binding + bare call still swallowed.
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
- **Removed platform-dependent `int`/`uint` and bare `float`** — untyped int literals default to `i64`.
- **Trait/impl conformance** — missing-method/arity errors, warns on extra methods.

### 06/11/26
- **Match arm validation + exhaustiveness** for boolean, tuple, and named-struct scrutinees; duplicate/overlapping arms.

### 06/10/26
- **Type checks:** division/modulo by literal zero; always-true/false conditions; float `==`/`!=` warning; for-loop condition must be `bool`; null-coalescing operand types; range operand types; for-in iterable must be iterable; tuple/array literal element types.
- **Unused function parameters; unused imports (`lyra-W004`).**
- **Diagnostic codes** attached to all diagnostics; **better parser error ranges** from CST ERROR/MISSING nodes.

### 06/09/26
- **Unreachable code; unused variables** (`TagUnnecessary`).
- **`_`-prefixed identifier grammar fix.**
- **`DiagnosticTag` + related-information support** in diagnostics.

### 06/04/26
- **Context checks:** `await`/`break`/`continue`/`yield`/`return` outside their valid context.
- **LSP:** document symbols, inlay hints, go-to-definition.

### 06/03/26
- **LSP: hover.**
- **Type checks:** member access; higher-order/non-identifier callees; unresolved identifiers; undefined functions; unknown type names; index access.

### 06/02/26
- **Regex engine:** Unicode properties (`\p{…}`) and performance (SIMD/DFA); wired into `RegexLiteralExpr` + `PatternConstraint`.

### 06/01/26
- Various bugfixes and improvements.

### 05/31/26
- **Integer-literal overflow/range checking; duplicate-declaration detection; shadowing detection; regex lookarounds.**

### 05/27–29/26
- **Match exhaustiveness** for array/string/number/data scrutinees.
- **Regex engine Phase 1** — derivative DFA (intersection, complement, flags, `IsMatch`/`FindAll`).

### 05/16–20/26
- **Struct-literal + record-update type-checking; function-declaration type-checking; if/else type-checking; scope-aware variable resolution.**

### 05/13–15/26
- **Comparison / boolean operators; string concatenation (`++`); type conversion; int-as-float literals.**
- **Added the typechecker; added the LSP server** (wired to the VS Code extension).
- **Collected:** arena statements, regex, pointers, negation, var reassignment, data constructors, unsafe blocks, compose/yield/generators; `@sizeof`.

### Earlier (01–05/26)
- **Grammar/collector foundation:** trait decls + impls, function/lambda types, modules + imports, match expressions, if-let destructuring forms, tuple/struct/array/data destructuring, postfix expressions, array literals + comprehensions, range expressions, tuple types, constrained types (range/precision/literal/step/pattern), for/for-in loops with labels, math-assignment operators, character literals, function guards + bodies, `i8`/`i16`/`f32`/etc.
- **Collector refactored** into subpackages; tests moved to the golden-print harness.
