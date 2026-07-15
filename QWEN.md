# lyra (Go) — Project Context

This is the main compiler infrastructure for the Lyra programming language. It contains the parser, AST, type system, collector, typechecker, a standalone semantic checker, and the LSP server.

Go module: `github.com/Lyra-Language/lyra`

The tree-sitter grammar is a local dependency via a `replace` directive pointing to `../tree-sitter-lyra`. After regenerating the grammar (`npx tree-sitter generate` in that directory), always run `go clean -cache` before `go test` — otherwise Go's build cache serves the stale compiled C parser.

## Data Flow

```
source text
  → pkg/parser                        tree-sitter CST (*sitter.Tree)
  → pkg/analyzer/collector            CST → *ast.Program + *symbols.SymbolTable
  → pkg/analyzer/checker              standalone AST passes (e.g. use-before-declaration)
  → pkg/analyzer/typechecker          AST → *typetable.TypeTable + []TypeError
```

The LSP server (`cmd/lyra-lsp`) runs this full pipeline on every document change and publishes diagnostics from all three analysis stages.

## Package Reference

### `pkg/parser`
Thin CGO wrapper around `go-tree-sitter`. Exports `Parse(source string) (*sitter.Tree, error)`. The compiled C parser is linked in from `../tree-sitter-lyra/src/parser.c`.

### `pkg/ast`
All AST node definitions. Key interfaces:
- `AstNode` — base interface (`node()`, `GetLocation() Location`)
- `Named` — extends `AstNode` with `GetName() string`
- `Statement`, `Expression`, `Pattern` — supertype interfaces (all nodes implement one)

All concrete nodes embed `AstBase` (which holds the `Location`). `Location` is 1-based `{StartLine, StartCol, EndLine, EndCol}`. `Location.Pretty()` formats a compact `line:col` or `line:col-line:col` string.

AST files are organized by node kind, e.g. `expr_math.go`, `expr_if.go`, `stmt_for_loop.go`, `decl_trait.go`.

### `pkg/ast/symbols`
Lexical `SymbolTable` with a tree of `Scope` nodes:

- Scope kinds: `ScopeGlobal → ScopeModule → ScopeFunction → ScopeBlock / ScopeLoop`
- `Scope.Define(Named)` — adds to current scope, errors on duplicate
- `Scope.Lookup(name)` — walks up the scope chain
- `Scope.LookupLocal(name)` — current scope only
- `ScopeTable` — a node→`*Scope` map (`Set`/`Get`) letting a later pass recover the scope a given AST node introduced without re-walking. The collector records: block expressions (`ScopeBlock`), if-let statements (the `Then` scope), lambda parameter scopes (`ScopeFunction`, on the `*ast.LambdaExpr`), both loop forms (`ScopeLoop`), and `with` blocks (`ScopeBlock`). Consumed by the typechecker's `enterScope` and, since 07/10/26, by the purity checker's `scopeFrames.forLambda` for lambdas/free functions (trait-method clauses still reconstruct by AST walk — they have no recorded scope; see `todo.md` FP/Imperative #3, Phase 2).
- `SymbolTable.Types` / `.Functions` — flat maps for fast global lookup by name; `.Functions` (and its `.PureFuncs` subset, the explicitly-`pure`-declared ones) is populated only for top-level `let`/`var name = <lambda>` bindings — a nested same-named function is deliberately not registered here (a name-keyed flat map can't disambiguate it from an unrelated binding elsewhere). The ML-style function sugar (`let name(params) => body`, no `=`) is the same binding — the grammar stores its lambda in the `value` field — so it registers identically. Modifiers may also lead the name (`let pure name(params) => body`); the collector (`declarations/var_decl.go` `applyFunctionModifiers`) lifts them off the declaration's `modifiers` field onto the `LambdaExpr`, so all three spellings yield the same AST.

Destructuring-declaration names (`let (a,b) = …`, `let {x} = …`, `if let [a,b] = … { }`, `let {x} = … else { }`) are registered via `RegisterDestructuredName(name, decl)`, which maps each bound name to its owning `*ast.DestructuringDeclStmt` (so the binding's `var`/`let mut` mutability is recoverable). The typechecker later overwrites that placeholder with a typed synthetic `VarDeclStmt` once it infers each leaf's type, via `checkDestructuringDecl` — shared by plain `let`/`var`, `if let`, and `let … else`. Scoping differs by form: plain `let`/`var` registers into the current scope; `if let … { Then } else { Else }` registers into a scope pushed around `Then` only (names visible there via the parent chain, not in `Else`); `let … = v else { Else }` registers into the *enclosing* scope only after collecting the diverging `Else` block, so `Else` never sees the names but code after the statement does.

### `pkg/types`
Type system. All types implement the `Type` interface (`typeNode()`, `String()`, `GetName()`).

Concrete types:

| Type | Notes |
|---|---|
| `PrimitiveType` | `i8`–`i64`, `u8`–`u64`, `f16`/`f32`/`f64`, `bool`, `string`, `char` (no platform-dependent `int`/`uint`; no bare `float` — untyped literals default to `i64`/`f64`) |
| `PrimitiveType` (internal) | `untyped_int`, `untyped_signed_int`, `untyped_float` — for numeric literal inference |
| `StructType` | named struct with fields |
| `DataType` | sum type (variants) |
| `LambdaType` | function type with param types and return type |
| `TupleType` | anonymous tuple |
| `ArrayType` | with element type and optional size |
| `ConstrainedType` | range/literal/pattern/precision/step constraints |
| `PointerType` | raw pointer |
| `GenericType` | type variable (e.g. `T`) |
| `SelfType` | the `Self` type inside trait impls |
| `VoidType` | `void` |
| `UnresolvedType` | placeholder for a named type not yet resolved |

Helper predicates: `types.IsNumeric(t)`, `types.IsString(t)`, `types.IsBoolean(t)`.

Allocation modifiers: `Unspecified` (`""`, the zero value / "resolve from default later"), `Stack`, `Shared` (the old `None` was retired — it conflated unspecified with stack and was applied only to arrays). `UnresolvedType` and `ParameterizedType` carry an `Allocation` field so usage-site modifiers (e.g. `let n: shared Node`) survive in the AST until resolution. Read a type's flavor via `types.AllocationOf(t)` (covers `NamedStructType`, `DataType`, `StaticArrayType`, `DynamicArrayType`, `ParameterizedType`, `UnresolvedType`; returns `Unspecified` for types that can't carry one — primitives, generics, lambdas, and tuples whose modifier lives on `TypeDeclStmt.Allocation`). Override a type's flavor via `types.WithAllocation(t, mod)` — returns a copy with `Allocation` set, or `t` unchanged when `mod == Unspecified` or the type can't carry a flavor. `resolveType`/`resolveTypeIfKnown` in the typechecker apply any `UnresolvedType.Allocation` override onto the resolved type after the name lookup, and recurse into array element and tuple element types so a named element (`[N]Node`, `(Node, Node)`) resolves too — without that, an annotation keeps an `UnresolvedType` element and assignability fails with a confusing "cannot assign ?(Node, Node) to ?(Node, Node)". Allocation is *not* part of nominal identity — `TypesEqual`/`isAssignable` ignore it (see todo #5's allocation-as-type-identity model). It is instead a *separate compatibility axis* checked alongside assignability: `firstAllocationMismatch` (`typechecker/assignable.go`) + `tc.checkAllocationCompat` flag *owning* a value across a concrete flavor boundary (`stack`↔`shared`) as `lyra-E018` ("converting allocation is an explicit operation"). The walker checks the top-level flavor and recurses structurally into array/tuple element types, returning the offending pair so the message names the actual clashing flavors even several levels down. Sites: annotated `let`/`var` init, destructuring-decl annotation, reassignment, interior lvalue write, plus call arguments and returns gated by mode (`paramOwnsArgument`/`isOwnedReturn`): only an `own` parameter adopts the argument (bare/`ref`/`mut` borrows are allocation-polymorphic and skipped), and a return is checked unless its type carries a `ref`/`mut` borrow modifier — matching FP/Imperative todo #5 Decision (b). Conservative: fires only when both sides carry a concrete, differing flavor; `Unspecified` is polymorphic (inherits context). Not covered: the `LambdaType`-callee path (`inferLambdaCallFromType`) — the collector never populates a param mode for lambda-*type* params, so there's no ownership info to gate on; and array element-level flavor isn't expressible in surface syntax yet (`[N]shared T` mis-parses). Type modifiers: `Mut`, `Ref`.

### `pkg/typetable`
- `TypeTable` — maps `ast.Expression` nodes → resolved `types.Type`. Populated by the typechecker; read by later passes. `Set(expr, typ)` / `Get(expr)`.
- `MethodTable` — maps a `*ast.FunctionCallExpr` (a `.`-call or `Trait::method` call resolved to a trait-impl method) → the matched `*ast.TraitMethodImpl`. Populated by the typechecker during dispatch (`typechecker_trait_dispatch.go`); read by the purity checker so it doesn't have to re-derive dispatch. `Get` is nil-receiver-safe (no resolutions) so callers without a typechecker pass can pass `nil`. A second map records **abstract bound dispatch** (a call on a bare type parameter resolved through a `where` bound): `SetBound`/`GetBound` associate the call with a `BoundMethodRef{Trait, Method}` — there is no single concrete impl, so the purity checker joins over all impls of that trait method.

### `pkg/analyzer/collector`
Converts a tree-sitter CST into `*ast.Program` and `*symbols.SymbolTable`.

**Entry point:** `collector.NewCollector(source []byte)` → `c.Collect(rootNode) (program, table, errors)`

**Dispatch:** `collector.go` owns `CollectStatement` and `CollectExpr` (switch on `node.Kind()`), and `ParseType`. Subpackages call back into the root collector via the `Collector` interface to avoid circular imports.

**Canonical-type resolution (`canonical.go`):** `Collect` finishes with `resolveCanonicalTypes`, which stamps `TypeDeclStmt.CanonicalKind` ("Result"/"Maybe"/"") — the single source of truth for whether a type is the compiler-known Result/Maybe that `?`, must-use, `??`, and the try-context check key off. Identity is conferred by a `@builtin(Result)`/`@builtin(Maybe)` attribute (collected onto `TypeDeclStmt.Builtin` via `collectBuiltin`, reusing the `@derive` attribute grammar — no grammar change), which is *name-independent* (a type named `Either` can be the canonical Result) but shape-validated; with no marker, an unmarked type literally named "Result"/"Maybe" with the canonical constructor shape is stamped as a fallback (the only path today — there is no prelude). A malformed marker (wrong shape, unknown kind, duplicate claim) is `lyra-E017`. Recognition sites read the stamp via the symbol table and keep a name+arity fallback only for a truly undeclared ambient annotation.

**Nil-node hazard:** `node.ChildByFieldName(...)` returns a genuine Go `nil` `*sitter.Node` for an absent *optional* grammar field (e.g. a zero-parameter `lambda_type`'s `parameter_types`). Calling any accessor (`ChildCount`, `Child`, `Kind`, …) on that nil node **hangs inside the go-tree-sitter CGO binding instead of panicking** — found via a real bug (`parseParameterTypes`, fixed 06/24/26) where this silently froze the whole collector. Always nil-check before touching the result of an optional field lookup, the same way `parseType`/`CollectExpression` already do.

**Subpackages** (all pass `*collector_ctx.Ctx` as their first argument):

| Subpackage | Files handle |
|---|---|
| `declarations/` | `let`/`var`/`const` decls, destructuring decls, `if let`, trait decls, trait impls, module decls |
| `typedecls/` | `struct`, `data`, named tuples, `newtype`, constrained types, attributes |
| `expressions/` | all expression kinds (one file per kind) |
| `statements/` | `for`, `for-in`, `return`, `break`, `continue`, `with`, var reassignment, deref assignment |

**`collector_ctx.Ctx`** — shared state passed to every subpackage function:
- `ctx.Source []byte` — raw source bytes
- `ctx.NodeText(node)` — extract text from a node
- `ctx.NodeLocation(node)` — convert to 1-based `ast.Location`
- `ctx.AddError(node, severity, format, args...)` — append a `CollectorError`
- `ctx.MustField(node, fieldName)` — get a required child field, emitting an error if missing
- `ctx.Collector` — embedded interface for recursive dispatch back to the root

**Tree-sitter traversal conventions:**
- `node.ChildCount()` / `node.Child(i)` — all children including anonymous keyword tokens; use with `switch child.Kind()`
- `node.ChildByFieldName("field")` — first child with that field name
- `node.FieldNameForChild(uint32(i))` — field name at index `i`; use when a rule repeats the same field name (e.g. multiple `value:` fields in `commaSep1`)

### `pkg/analyzer/checker`
Standalone AST-level semantic passes. Most (e.g. `use_before_declaration.go`, `shadowing.go`, `unused_variables.go`) run after collection but before typechecking and only need the AST. **`purity.go` is the exception** — `CheckPurity(program, methodTable)` takes the typechecker's `*typetable.MethodTable` (nil-safe) so a pure function/method calling a trait method can be checked against the method it actually dispatches to; it must run *after* `typechecker.Check`, not before (see `cmd/lyra-lsp/main.go`'s ordering).

- **`use_before_declaration.go`** — `CheckUseBeforeDeclaration(program) []UseBeforeDeclarationError`
  Two-pass algorithm: collect all names declared directly in a block, then walk in order flagging any use of a not-yet-seen name.
- **`purity.go`** — `CheckPurity` enforces `pure` (lambdas and, since 06/24/26, trait-impl methods): no captured mutation, no calls to non-pure functions/methods, no `await`. Both `CheckPurity` and the call-site "non-pure method" check consult `inferImpurity`'s bottom-up fixpoint (not just the explicit `pure` flag) for whether a callee is actually pure — it runs over free functions and trait-impl methods jointly (`collectMethodImpls`), since either can call the other. `InferredPureFunctions(program)` separately exposes that result by name for top-level functions only (a name-keyed map can't disambiguate methods across impls). Method-to-method calls are now tracked: `checkTraitImplMethodBody` (`typechecker_traits.go`) type-checks each impl method body — verifying it against the trait's declared return type (Self and the trait's own params substituted, mirroring `checkLambdaBody`) and populating `MethodTable` with any `.`-calls inside, so `methodEffects` finds them in the fixpoint.
- **`effects.go`** — `checker.Effect`, a bitmask generalizing the old impure/pure bool (`EffectMut`, `EffectInput`/`EffectOutput` — the split of the old `EffectIO`, kept as their `Input|Output` alias — `EffectAlloc`, `EffectRand`, `EffectTime`; `EffectNone` = pure). `inferImpurity` accumulates this per function/method (set-monotonic fixpoint) instead of a bool; `InferredEffects(program)` exposes it by name. Two named-bound masks over the row: **`PurityEffects = Mut|Input|Output|Rand|Time`** (everything but Alloc — `pure` and `InferredPureFunctions` are defined against it, so `EffectAlloc` is *orthogonal*, a `pure` function may allocate) and **`DetEffects = Input|Rand|Time`** (⊆ PurityEffects, so `pure` ⟹ `det`; `det` forbids only the non-determinism sources, permitting Mut/Alloc/Output — the input-vs-output IO split is what lets `det` allow logging). `EffectAlloc` detection (`purity.go`'s `allocContext`/`buildAllocContext`): constructing a `shared`-declared struct/data/named-tuple value, *unless* lexically inside a `with`-arena block (a hard-coded discharge — Lyra has no general effect handlers). Implicit allocation (dynamic arrays/strings, escaping closures) and precise arena escape are deferred to a future layout/escape pass. An **unresolvable external call** (no local lambda, builtin, or type conversion) conservatively taints `AllEffects` (`PurityEffects | EffectAlloc`) — everything, including Alloc, so `noalloc` flags it too (we can't verify it doesn't allocate). `builtinEffects`: print/println→Output, read→Input, write→Input|Output, `await`→Input, `Random.global()`→Rand, `wallClock()`→Time. Only *ambient* rand/time sources carry the bit — a threaded RNG's `rng.next()` or a passed-in `tick` (reached through a local binding) is ordinary `mut`/`own` data, which is what lets `det` permit seeded randomness and sim-time. User surface is the `pure`/`det`/`noalloc` ladder — see `todo.md` FP/Imperative #5.
- **`try_outside_result.go`** — `CheckTryOutsideResult(program, symTable)` (`lyra-E008`) flags a `?` whose nearest enclosing function doesn't return a canonical Result/Maybe. It reads the same canonical identity the typechecker uses via `canonicalKindOfName` (the read side of the collector's `resolveCanonicalTypes` stamp), so the context check and the typechecker's operand/kind checks agree — a function returning a same-named-but-differently-shaped `data Result` is *not* a valid `?` context.
- **`effect_bounds.go`** — `CheckEffectBounds(program)` (`lyra-E015`) errors when a lambda, a trait-impl method, or a **trait method declaration** (`trait X { pure det foo: … }`) carries both `pure` and `det`: two rungs of the same correctness axis (`pure` ⊆ `det`), so annotating both is contradictory. `noalloc` is an orthogonal resource bound and is never flagged. AST-only, wired into the LSP before typechecking. **`det`/`noalloc` enforcement** lives in `purity.go`'s `checkBoundedEffects` (run inside `CheckPurity`, `lyra-E016`): it checks each callable's full inferred (transitive) effect set — `det` against `DetEffects`, `noalloc` against `EffectAlloc` — reporting once at the callable's location (`pure` keeps its fine-grained per-op walk, `lyra-E007`). **Per-trait-method bounds are a contract:** a trait method may be declared `pure`/`det`/`noalloc` (`TraitMethod.IsPure/IsDet/IsNoAlloc`); `checkTraitMethodBounds` (`purity.go`) checks each impl of it against the *effective* bound — the impl method's own annotation OR the trait's — so an impl of a `pure`-declared method is enforced pure even without its own `pure` marker, and a `where t: Show` bound to a `pure`-method trait carries that guarantee.

### `pkg/analyzer/typechecker`
Walks the collected AST and infers/verifies types, writing results into a `TypeTable`.

**Entry point:** `typechecker.New(symTable, scopeTable, typeTable)` → `tc.Check(program) []TypeError`

**`TypeError`** has `Message string`, `Location ast.Location`, `Severity` (`SeverityError` / `SeverityWarning`).

Key methods:
- `inferExprType(expr)` — returns the `types.Type` for an expression and records it in the `TypeTable`
- `propagateLiteralType(expr, concrete)` — **context-directed literal-width inference.** Bottom-up inference computes each expression's result type but leaves an untyped literal (`5`, `3`) recorded as `untyped_int` until context fixes its width. This helper pushes a concrete numeric width *down* onto untyped int/float literal leaves, recursing only through width-preserving arithmetic (`+ - * / % %%`, unary `-`) and stopping at identifiers/calls/conversions (a conversion `i8(x)` is exactly where a new width begins). It narrows a leaf **only when the value fits** the target width — a literal that doesn't (`i8(x) < 300`) is left untyped, so overflow surfaces loudly (the fold-based `checkIntegerLiteralRange`, or a backend width mismatch) rather than silently wrapping, and propagation never double-reports the overflow the range check owns. Called from five context sites: annotated `let` (`checkVarDecl`), a `MathBinaryOp` with a concrete result (`inferMathBinaryExpr`), numeric comparisons/`==` (`propagateComparisonWidth`, using the operands' common type since a comparison's own result is bool), `var` reassignment (`checkVarReassignment`), and the lambda/entry return body (`checkLambdaBody`/`checkBlockReturn`). The backend reads these recorded leaf widths. **Not yet covered:** call-argument and match-arm context.
- `checkNode(node)` / `checkVarDecl` / `checkVarReassignment` / `checkExpressionStmt` — statement-level checks. **Sequential-rebind self-reference:** the collector's `RedefineVariable` overwrites a same-scope binding, so inside `let x = x + 1` the name `x` resolves in scope to the declaration being defined (itself, not yet typed). To type the RHS against the *prior* value, the collector records the replaced binding as `VarDeclStmt.Shadows`, and `checkVarDecl` sets `tc.currentVarDecl` around inferring the initializer; the `IdentifierExpr` case redirects a lookup that lands on `currentVarDecl` to its `.Shadows`. Without this the RHS inferred nil — silently masked elsewhere by nil-guards, but it broke any consumer that *reads* the recorded type (e.g. the LLVM backend's `getIntSignedness`).
- `checkIfDestructuringStmt` / `checkElseDestructuringStmt` — type-check `if let`/`let … else` bodies, reusing `checkDestructuringDecl` to bind pattern names with the right scope (if-let's names are local to `Then`, entered via `enterScope` against a scope the collector pushed and recorded against the `*ast.IfDestructuringStmt` node itself; let-else's persist in the enclosing scope, like a plain `let`)
- `assignable.go` — `effectiveType` and unification logic for type compatibility
- `resolveTraitMethod(receiverType, methodName, requiredTrait)` (`typechecker_trait_dispatch.go`) — finds every impl whose target type matches `receiverType` (`implTargetMatches`) providing that method, optionally restricted to one trait; multiple matches with no `requiredTrait` is the "two traits, same method name" ambiguity a fully-qualified `Trait::method(...)` call resolves. Drives both `inferMemberCall`'s fallback (after struct-field lookup fails) and `inferTraitMethodPathCall`. Records each resolution in `tc.MethodTable()` for the purity checker. **Generic impls dispatch** (`impl Show<t> for Box<t>`): a target containing lowercase `GenericType`s (Lyra's implicit type variables — an uppercase name is concrete) matches when it *unifies* with the receiver (`unifyGenericTarget`), each variable binding to the receiver's corresponding subterm, with binding-consistency (`Pair<t,t>` accepts `Pair<i64,i64>`, rejects `Pair<i64,string>`); targets can be parameterized, array, or tuple. `Self` is substituted with the concrete receiver, so a Show/Debug/Hash-style method (signature in terms of Self + concrete types) type-checks against the instantiation. **Bounded impls are constraint-checked:** for `impl Ord<t> for Box<t> where t: Ord` dispatched on `Box<Widget>`, `unifyGenericTarget` binds `t`→Widget and `checkImplConstraints` verifies each `where` bound holds for the binding (via `typeImplementsTrait`, itself an `implTargetMatches` search) — Widget with no `Ord` impl errors; `Box<i64>` with `impl Ord for i64` is accepted. The bound check is single-level (a satisfying impl's *own* `where` bounds aren't recursively re-verified). **Generic struct field access** works via `resolveGenericStruct` (`typechecker.go`): member access on a `ParameterizedType` naming a generic struct resolves it to the struct with its type arguments substituted into the field types (`substituteGenerics`) — `Box<i64>.value` → `i64`, and `self.value` inside a generic impl body → the parameter `t`. This is applied only to the field-lookup side of member access; trait dispatch keeps the original `ParameterizedType` (the unifier needs its type arguments). The struct's generic-parameter *names* are read from `decl.GenericParams` (the `TypeDeclStmt`), since `NamedStructType.GenericParams` is not populated by the collector. **Bounded polymorphism in method bodies** (`dispatchViaGenericBound`): calling a trait method on a value whose type is a bare parameter (`self.value.show()`, `self.value : t`) dispatches through the parameter's in-scope `where` bound. `checkTraitImpl` loads the impl's `where` constraints into `tc.genericBounds` (param name → trait names) around its body checks; `inferMemberCall`, on a `GenericType` receiver, looks up a bound trait declaring the method and type-checks the call against that trait's signature with Self = the parameter (so a Self-returning bound method yields `t`). This is *abstract* dispatch — no concrete impl exists here (it's chosen when the enclosing generic is instantiated, where `checkImplConstraints` has already verified the bound). It's recorded in the MethodTable as a `BoundMethodRef` (trait + method name) via `SetBound`/`GetBound`, so the purity checker scores it as the **join over every impl of the bound method** (`boundCallEffect` in `purity.go`: pure/det only if *all* impls are — the bound admits any of them) rather than as an unverifiable external call. No bound → an actionable error. **A trait's own type parameters are bound** (`trait Get<e> { get: (self) -> e }`, `impl Get<t> for Box<t>`, `box.get()` on `Box<i64>` → `i64`): the impl's trait arguments (`Get<t>` → `[t]`) are collected into `TraitImplStmt.TraitArgs`, and `resolveTraitMethod` builds a substitution from each trait param (`e`) to the impl's positional arg (`t`) resolved through the receiver bindings (`{t: i64}`), applying it to the method signature via `substituteSigGenerics` (params and return). So `-> e` becomes `-> i64`, and an `e`-typed parameter is checked against the concrete arg. (Note: the impl's `<…>` grammar field labels every child with one field name, so `TraitArgs` is collected with `FieldNameForChild` iteration; `impl.GenericParams` stays empty — it expects `generic_parameter` nodes — and the target's own type variables are read off the target itself. The `where`-clause bounds are in `impl.Constraints`.)

**Builtin methods** (`builtins.go`): compiler-provided methods on primitive receivers, e.g. `x.wrapping_add(y)` on integers. `builtinMethodSignature(recv, name)` returns a `*LambdaType` specialized to the receiver (parameters are the *call* args only — `self` is the implicit receiver), consulted by `inferMemberCall` **last**, after struct-field and trait-method resolution miss, so a user type or trait impl always shadows a builtin. Currently registered: the integer overflow-arithmetic ops `wrapping_{add,sub,mul}` / `saturating_{add,sub,mul}` (`(self: T, other: T) -> T` for a concrete integer T) — the "somewhere to live" for Pit-of-Success #2, a registry (NOT a prelude). A primitive is therefore a valid method receiver; a missing method on one reports `T has no method "x"`. The name set is what a backend must lower (two's-complement ±/× for wrapping; `llvm.{s,u}{add,sub}.sat` for saturating). `checked_*` and the `truncate`/`saturate`/`narrow` conversions are not registered yet (see `todo.md` #2/#5).

Files split by concern: `typechecker.go` (core + var decls + expressions), `typechecker_control_flow.go` (if/match), `typechecker_functions.go` (lambda/call/member-call dispatch), `typechecker_trait_dispatch.go` (trait-method resolution), `typechecker_traits.go` (impl conformance), `builtins.go` (builtin methods on primitives), `errors.go` (error helpers), `assignable.go` (type compatibility).

### `pkg/driver`
The single reusable entry point to the whole front-end. `driver.Analyze(source []byte) *Result` runs parse → collect → the standalone `checker.Check*` passes → `typechecker.Check` → `checker.CheckPurity` (in that order, purity last so it sees the resolved `MethodTable`) and returns a `Result{Program, SymbolTable, ScopeTable, TypeTable, MethodTable, Diagnostics}`. Every pass's errors are normalized to `[]diagnostic.Diagnostic` (CST parse errors converted from tree-sitter's 0-based positions to 1-based `ast.Location`). `Result.HasErrors()` / `Result.Errors()` filter by severity. This is where a backend (or any tool needing a typed program) starts, instead of re-implementing the pipeline. Both `cmd/lyrac` and `cmd/lyra-lsp` call it, so the pipeline is defined in exactly one place.

`driver.ResolveEntryPoint(res) (*EntryPoint, []diagnostic.Diagnostic)` (`entrypoint.go`) finds and validates the program's entry function: a top-level `let main` that is a zero-parameter function returning `u8` (the process exit code, `EntryReturnExitCode`) or `void`/no-annotation (`EntryReturnVoid`). `u8`, not a wider int — the OS truncates a process exit code to its low 8 bits regardless of what width the language lets you write (verified: even a real C `int main() { return 300; }` exits 44, not 300), so a wider return type only adds the silent-truncation surprise Lyra rejects elsewhere; matches Zig's `pub fn main() u8` / Rust's move to the narrow `std::process::ExitCode` over a raw wide int. Absent/non-function/parametered/wrong-return `main` → nil + diagnostics. It is a **build-time** requirement (a library or a `check` needs no `main`), so it is intentionally *not* part of `Analyze` — only `lyrac build` calls it. **Note:** `cmd/lyrac` calls this; `cmd/lyra-lsp` still has its own byte-identical inline copy of the pipeline and should be migrated onto `driver.Analyze` (then the pipeline lives in exactly one place).

### `pkg/backend`
The seam between the front-end and code generation. `backend.Backend` is the interface a code generator implements: `Name() string` and `Emit(res *driver.Result, entry *driver.EntryPoint) ([]byte, error)`. `Emit` is called by `cmd/lyrac` only after analysis is error-free and the entry point resolves, so an implementation may assume a well-typed program.

**`pkg/backend/llvm`** — the LLVM IR backend, **early status**, built on `github.com/llir/llvm` (v0.3.6 — pure Go, builds a structured `ir.Module`; note it emits *typed* pointers like `i8*`, not opaque `ptr`). `llvm.New()` returns a `*Backend` whose `Emit` builds an `ir.Module`, declares the runtime shims (`declareRuntime`), and defines `@main` — always `i32` at the LLVM/ABI level (the actual C runtime-expected signature, verified against real clang output) regardless of Lyra's own `u8`/`void` entry-point convention, with the `u8` body value coerced (`coerceIntWidth`: identity/`trunc`/`sext`/`zext`) and zero-extended into that `i32` slot. `lowerExpr` (called via `lowerEntry`) covers integer literals, arithmetic (`+ - * / % %% -(unary)`, incl. Odin-style floored `%%` vs truncated `%`), int-to-int numeric conversions (`i8(x)`, `u32(x)`, …), comparisons + `&&`/`||`, `if`/blocks (incl. one-armed `if` as a statement), **`let`/`var` bindings, identifier reads, `var` reassignment**, **`for` loops with `break`/`continue`** (`lowerForLoop`: the cond/body/post/exit CFG; a `loops []loopCtx` stack on the lowerer resolves break/continue targets, labeled ones by walking the stack), **compound assignment** `MathAssignOpExpr` (`i += 1` → load/op/store via `lowerMathAssignOp`, reusing the extracted `applyIntMathOp`), and **user-defined functions** with calls, `return`, and recursion; anything else errors loudly rather than emitting wrong code. **Block-termination discipline:** break/continue are the first constructs that seal a block mid-stream, so `lowerBlock` is split into a value-optional `lowerBlockStmts` (stops iterating once `block.Term != nil`) + a value-requiring wrapper + `lowerForEffect` (loop/one-armed-`if` bodies need no value), and every fall-through `br` at a CFG join is guarded by `end.Term == nil`. All three loop forms lower — infinite (`for {}`), condition-only (`for cond {}`), and the three-clause `for var i = 0; i < n; i += 1`. **One remaining loop limitation:** a `let`/`var` declared inside a loop body isn't visible there (loop var must live in an enclosing scope) — `ForLoopExpr.Body` is a value copy, so the typechecker can't enter the body's registered scope; see `todo.md`. **Functions** lower in two passes (`Emit`): every user function is `declareFunction`'d before any body, so a call (from main, between functions, or recursive) resolves against `l.funcs`; `defineFunction` then lowers each body. `main` stays special (`lowerEntry`, the `i32` ABI). Each body resets per-function state via `beginFunction` (fresh `locals`/`loops`, plus `retType`/`retSigned`/`entryABI`); params bind as entry-block allocas keyed by name. `emitReturn` is the one return path — coerces to the declared width, with main's `entryABI` doing the u8→i32 slot — shared by explicit `return` (`ReturnStmt`, which seals its block like break/continue) and the implicit tail return. Call arguments pass un-coerced (the typechecker propagated each param's width onto its literal args, so `add(200)` already lowers `200` at the param width). Deferred with loud errors: void/multi-clause functions, default params, destructuring params, and higher-order (lambda-value) calls. Locals are modeled as entry-block `alloca` + store/load (mem2reg builds SSA — no hand-written phi nodes for variables), tracked in `lowerer.locals` (name → its alloca, a pointer). `lowerVarDecl` allocas in the function's entry block and stores the initializer; a reassignment stores into the *existing* alloca and leaves the `locals` entry pointing at that alloca (it must stay the pointer, not the stored value, or the next `IdentifierExpr` load's `slot.(*ir.InstAlloca)` assertion fails). Alloca type is taken from the initializer's lowered `.Type()` (sidesteps the still-absent `lowerType`). **Integer literals lower at the width the typechecker recorded** (`literalIntType` reads `res.TypeTable`, fallback i64 for an untyped/absent entry) — context-directed literal-width inference (`typechecker.propagateLiteralType`) resolves narrow widths, so `i8(x) < 3` lowers `3` as i8 and u8 arithmetic wraps at u8. The comparison width-mismatch guard is now defensive-only: it fires when a literal too large for its context width was left untyped and fell back to i64 (`i8(x) < 300`), which is a loud error rather than miscompiled code. `lowerType` exists for scalars (Lyra `PrimitiveType` → llir type); it grows from here toward `match`, structs/data, and the non-scalar `lowerType` cases. Float literals/arithmetic and any conversion touching float are deliberately deferred — confirmed unreachable by any valid program today (no `let`/blocks, `main` can't return float, no `float→int` builtin). `layout.go` provides the llir type toolkit — `LLVMPrimitive`, `IsSignedInt`, `IsNumericConversionTarget`, `SharedBoxType`, `TagType`, `DataUnionType` (all returning `llir` `types.Type`) and the `SizeAndAlign` datalayout engine — that `lowerType` dispatches over. The builtin overflow-arithmetic methods (`typechecker/builtins.go`), the `stack`/`shared` representation (ALLOCATION.md), and the `data` tagged-union layout (DATA_LAYOUT.md) are the settled lowering decisions; SIMD is a roadmap (SIMD.md).

### `pkg/printer`
Reflection-based AST printer used only in tests. `printer.PrintAST(program)` walks exported struct fields; zero/nil/empty values are omitted. `printer.NewPrinter().Print(node)` pretty-prints a raw tree-sitter CST node (useful for debugging).

### `cmd/lyra-lsp`
LSP server. Uses `github.com/owenrumney/go-lsp` over stdio. On every `textDocument/didOpen` or `textDocument/didChange`:
1. Applies incremental edits to an in-memory doc store
2. Calls `driver.Analyze` (the shared pipeline) and persists the returned `docAnalysis` (program + tables) for hover/definition/etc.
3. Maps the returned `[]diagnostic.Diagnostic` to LSP via `diagToLSP` and publishes them

The `analyze` method is now a thin wrapper over `driver.Analyze` — it no longer re-implements the pass sequence.

Logs to `/tmp/lyra-lsp.log`. Build with `go build ./cmd/lyra-lsp`.

### `cmd/lyrac`
Compiler CLI, built on `pkg/driver`. Two subcommands: `lyrac check <file.lyra>` (parse + typecheck, print diagnostics, exit 1 on any error) and `lyrac build <file.lyra>` (check, resolve the entry point via `driver.ResolveEntryPoint`, then hand the typed program to the backend). Diagnostics print as `path:line:col: severity[code]: message` (the `line:col` is omitted for a program-level error with no location, e.g. a missing `main`). `build` runs the `pkg/backend/llvm` backend via `lowerAndEmit`, writing `<name>.ll` next to the source and printing the `clang` command to compile it. Codegen is early (literals/arithmetic/int-width conversions — see that package's doc comment), so a non-trivial `main` may still hit a form `lowerExpr` errors on rather than lowering incorrectly. Build with `go build ./cmd/lyrac`.

## Testing

### Collector golden tests (`pkg/analyzer/collector/tests/`)

```bash
go test ./pkg/analyzer/collector/tests/...          # run golden tests
UPDATE_GOLDEN=1 go test ./pkg/analyzer/collector/tests/...  # regenerate .golden files
```

Pattern:
```go
func TestSomething(t *testing.T) {
    source := `let x = 42`
    runGoldenTest(t, source, "golden_file_name")  // no extension
}
```

Golden files live in `testdata/*.golden`. First run with a new file creates it and fails; re-run to confirm. The printer omits zero/nil/empty fields, so only populated fields appear in the output.

`parseAndCollect(t, source)` is the lower-level helper when you want `program` and `table` directly without a golden file.

### Typechecker assertion tests (`pkg/analyzer/typechecker/tests/`)

```bash
go test ./pkg/analyzer/typechecker/tests/...
go test -run TestName ./pkg/analyzer/typechecker/tests/...
```

Pattern:
```go
res := parseCollectAndCheck(t, source, false)
assertNoErrors(t, res)
// or
assertErrorsAre(t, res, "expected error message 1", "expected error message 2")
```

`res` exposes `res.program`, `res.symTable`, `res.typeTable`, and `res.errors`.

### Running all tests

```bash
go test ./...
go test -run TestFunctionName ./pkg/...
```

## Current Development Focus

The typechecker is the active area. See `todo.md` at the project root for the full backlog.

Match exhaustiveness is done for all scrutinee kinds today: numbers (range patterns), strings, `data`, arrays, `bool`, tuples, and structs (`pkg/analyzer/typechecker/typechecker_control_flow.go`, `*MatchIsExhaustive` functions; tests in `pkg/analyzer/typechecker/tests/match_expr_*.go`).

The FP/Imperative purity work (`pkg/analyzer/checker/purity.go`) is the active area now. Purity inference (bottom-up, no `pure` keyword required) covers both free functions and trait-impl methods via one joint fixpoint (`inferImpurity`), including method-to-method call chains: `checkTraitImpl` (`typechecker_traits.go`) now calls `checkTraitImplMethodBody` on each impl method body, which sets up a param scope from the trait signature, checks the body against the declared return type, and infers the body so any `.`-call inside is dispatched into `MethodTable` — making the purity fixpoint's `methodTable.Get` lookups in `methodEffects` produce correct results for method-to-method chains. **Phase 2 landed for lambdas + free functions (07/10/26):** the purity checker now *consumes* the collector's `ScopeTable` rather than re-walking the AST — `scopeFrames.forLambda` (`purity.go`) flattens a lambda's recorded `ScopeFunction` subtree (pruning at nested function scopes) into the per-lambda `scopeBindings` frame, deriving mutability from declaring nodes and reconciling two collector quirks (`with`-arena handles read mutable, `for … in` loop vars skipped) for bit-for-bit fidelity. `CheckPurity`/`InferredEffects`/`InferredPureFunctions` take a `*symbols.ScopeTable` accordingly. **Still open:** impurity of imported functions; and the *trait-method* path (`directScopeBindingsForClause`) still re-walks — method clauses have no recorded scope (`CollectLambdaClause` pushes none), so converting them needs a collector change reconciled with `checkTraitImplMethodBody`. See `todo.md`'s FP/Imperative #3.
