## To-Dos
---------

### Pit of Success — language design (ordered by importance)
These make the *safe/correct* path the *easy/default* path, and force the unsafe path to be loud and explicit. Listed highest-leverage first.

1. **Must-use `Result`/`Maybe` + a `?` propagation operator** — *Right now the easy thing (ignore a returned error) is the wrong thing.* Two coordinated changes:
   - Mark `Result<T,E>` and `Maybe<T>` as **must-use**: a statement that discards one (no binding, no match, no propagation) emits a warning/error. Implement as a new `checker/` pass that flags `ExpressionStmt`s whose inferred type is `Result`/`Maybe` and whose value is dropped; allow an explicit `_ = expr` opt-out.
   - Add a `?` (try) postfix operator: `let x = parse(s)?` unwraps `Ok`/`Some` or early-returns the `Err`/`None` from the enclosing function. Requires the enclosing lambda's return type to be a compatible `Result`/`Maybe` (mirror the `await`-outside-async check for the "used outside a Result-returning fn" error). Goal: propagation must be *less* typing than ignoring.

2. **Checked arithmetic by default; wraparound must be explicit** — overflow is currently only caught for *literals* assigned to annotated types (`overflow.go`); `a + b` on two `i8` wraps silently at runtime. This contradicts the determinism goal behind `fixed<I,F>`. Decide the default = trapping/checked arithmetic, and make modular math the explicit opt-in: `wrapping_add`/`saturating_add` methods or a `wrapping { ... }` block. At minimum, add a typechecker warning for arithmetic on small fixed-width ints whose result provably can overflow the operand type.

3. **Restrict `??` to optional (`Maybe<T>`) operands** — `null-coalescing.lyra` currently accepts `let x: i64 = 0; x ?? y`, which teaches that *any* value is nullable (the billion-dollar mistake by the side door). Change `inferNullCoalescingExpr`: the left operand must be `Maybe<T>` (result type `T`); a non-optional left operand is a warning ("left operand is never null"). Keep nullability expressible *only* through `Maybe<T>`, never as an ambient property of every type.

4. **Non-exhaustive `match` on closed types is an error, not a warning** — warnings are ignorable, which defeats exhaustiveness. For `data`/sum types and `bool`, a missing arm should be `SeverityError` (the author can still write an explicit `_ =>` to opt out). Keep the warning only for genuinely open types (wide integers, strings). The exhaustiveness machinery already exists — this is a severity change in the match checkers.

5. **One conversion syntax; lossy conversions must be loud** — today both `x as f32` and `f16(x)` exist (two ways to do one thing), and `as` silently truncates (`i64 as i8`, `f64 as f16`). Pick one form for lossless/widening conversions, and give lossy/narrowing conversions a distinct, named spelling (`narrow`/`truncate`/`saturate`) so precision/range loss is never invisible. Warn (or error) on any `as` that loses bits.

6. **Definite-assignment analysis** — verify that `var x: T` with no initializer (if the grammar allows it) cannot be read before being assigned on every path; otherwise require initialization at declaration. Prevents use-of-uninitialized, a classic pit of failure. (First confirm whether uninitialized declarations are even grammatically reachable.)

7. **Reconsider shadowing severity** — same-scope sequential rebind (`let x = parse(x)`) is idiomatic and safe in ML-family languages; warning on it pushes users toward worse `x2` names. Narrow `shadowing.go` to warn only on *nested-scope* shadowing (the genuinely confusing case) and allow same-scope rebind, OR document the decision explicitly.

8. **Consistency cleanups (lower-stakes pit-of-failure removal)**
   - Unify `Void` vs `void` casing on one spelling across grammar + typechecker.
   - Write down "use a tuple vs named `tuple` vs `struct` vs inline `data` record" guidance so the product-type choice isn't a coin flip.

### Functional / Imperative blend — enforce the pure/mutable split (ordered)
Goal: give the FP half real *guarantees* (determinism, referential transparency, safe auto-parallelism) while keeping ergonomic in-place mutation for games. Current state: `pure` is parsed (`LambdaExpr.IsPure`) but **unenforced** — it's a no-op annotation; `ref`/`mut`/`own` modifiers are parsed but unchecked; and safe aggregate mutation (`p.x = v`, `arr[i] = v`) isn't even expressible (the only assignment LHS forms are identifier/destructuring/deref), so today interior mutation requires `unsafe` pointers. Both halves need teeth.

**Guiding model:** purity = *no observable effect crossing the function boundary* (NOT "no mutation happens"). Local mutation of owned values inside a `pure` function is allowed and encouraged (the ST-monad / Rust-`fn` / "functional but in-place" trick) — that's what lets pure code stay fast. The membrane is one-way: pure may not call impure; impure may freely call pure. The `ref`/`mut`/`own` modifiers are the machinery that tells the checker whether a mutation escapes (`own`/`ref` ok in pure, `mut` borrow = external effect = forbidden in pure). Frame the whole thing as *the compiler's license to memoize, reorder, and auto-parallelize* (ECS systems as pure functions over component arrays → job-system scheduling with no data-race analysis) — that's the games payoff that justifies the work.

1. **Purity-enforcement checker pass** (`checker/purity.go`) — walk each `pure` lambda body; error on observable effects: reassignment/math-assign/deref-assign of anything not declared locally within the function, calls to non-pure functions, `await`/I/O/effectful builtins, and (later) mutation through a `mut`-borrowed parameter. Mirror the `CheckAwaitOutsideAsync` enclosing-context walk. Landed in `checker/purity.go` and **wired into the LSP** (`cmd/lyra-lsp/main.go` analyze pass; verified end-to-end over stdio). Start strict-but-simple, expand the effect surface incrementally. Diagnostic code `lyra-E007` (impure op in pure fn). Demo fixture: `lyra-vscode-ext/test/purity.lyra`.
2. **Safe mutable lvalues** (independent of #1; do in parallel) — add `member_expr`/`index_expr` to the assignment grammar (`statements/assignments.js`), then enforce in the typechecker: mutation is legal only through a `var` binding (or a `mut`/`own` path); `let`/`ref` give **deep** immutability (cannot mutate an interior reached through an immutable binding). This is the missing half of the `let`/`var` story and removes the need for `unsafe` for ordinary gameplay mutation.
3. **Purity inference** — infer purity bottom-up over the call graph so `pure` is the cheap default and the keyword becomes a *checked assertion*, not a tax. A function that only calls pure functions, mutates only locals, and does no I/O is pure; record it on the symbol so callers can use it. *Partial:* `inferImpureFunctions` in `purity.go` already infers impurity of **top-level** function bindings transitively (fixpoint) so a `pure` fn calling a user-defined impure fn is flagged. Still to do: non-top-level/methods/imported functions, symbol-table-backed scope info, and making *pure* (not just impure) the recorded default.
4. **Wire `ref`/`mut`/`own` into the typechecker** — make the modifiers actually constrain mutation (immutable-borrow can't be mutated; mutable-borrow can; owned is local) and feed the result into the purity check from #1.
5. **Generalize `pure` to a coarse effect row** — once 1–4 exist, model effects as a set `{io, alloc, rand, time, mut}` where `pure` == empty set. Lets you express "deterministic but allocates" (valuable for netcode/replay even when not fully pure) instead of a single boolean. Keep immutable data as plain owned **value-semantics** types (copy/move/COW), not heap-allocated persistent structures — stays layout-friendly for `fixed<>`/packed/arena.

### LSP — Table-stakes editor features

### Checker — Control-flow validity (new `checker/` pass)
- **Unsafe operations outside `unsafe` blocks** — `AddressOfExpr`, `DerefExpr`, raw pointer access, and calls to `IsUnsafe` lambdas should require an enclosing `UnsafeBlockExpr` or unsafe function

### Typechecker — Constant and value-level checks
- **`const` requires a compile-time-constant initializer** — walk the initializer and reject anything that isn't a literal, constant identifier, or purely constant expression
- **`@sizeof` on unknown types** — `SizeofExpr` should emit `unknown type %q` when its type argument doesn't resolve

### LSP — Additional navigation and editing features
- **Code actions / quick fixes** (`textDocument/codeAction`) — "Add missing match arms" (reuse the `missing` slice from `dataMatchIsExhaustive`), "Add missing struct fields", "Remove unused variable/import", "Insert inferred type annotation"
- **Completion** (`textDocument/completion`) — identifiers in scope, type names, struct field names after `.`; reuse `Scope.Lookup` chain and `SymbolTable.Types`
- **Signature help** (`textDocument/signatureHelp`) — show the lambda signature while typing inside `()`; parameter info is already on every `LambdaExpr`
- **Find references** (`textDocument/references`) — walk the AST collecting every `IdentifierExpr` / `SpreadExpr` that matches the target symbol's declaration
- **Rename** (`textDocument/rename`) — compute all references (see above) and return a `WorkspaceEdit`
- **Semantic tokens** (`textDocument/semanticTokens`) — emit per-token type/modifier classifications (constant, mutable variable, type name, function, deprecated, etc.) using `SymbolTable`; the TextMate grammar can't distinguish these
- **Workspace symbols** (`workspace/symbol`) — fuzzy-search across all type decls and functions in the workspace
- **Document highlight** (`textDocument/documentHighlight`) — highlight all occurrences of the symbol under the cursor; cheap once references work
- **Folding ranges** (`textDocument/foldingRange`) — emit fold regions for `match`, `data`, struct, trait, and block expressions

## In Progress
--------------

## Completed
------------
### 06/12/26 (continued)
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
