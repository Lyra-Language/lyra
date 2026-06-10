## To-Dos
---------

### LSP — Table-stakes editor features

### Checker — Control-flow validity (new `checker/` pass)
- **Unsafe operations outside `unsafe` blocks** — `AddressOfExpr`, `DerefExpr`, raw pointer access, and calls to `IsUnsafe` lambdas should require an enclosing `UnsafeBlockExpr` or unsafe function

### Checker — Unused symbol detection
- **Unused imports** — walk `ImportStmt.Members` and warn for any alias/name that never appears as an identifier
- **Unused function parameters** — same as unused variables, scoped to the lambda body

### Typechecker — Collection-level type checking
- **Array literal element type homogeneity** — infer element type as the common type of all elements via `branchCommonType`; error on mismatches (e.g. `[1, "two", true]`)
- **Tuple literal element type checking** — record element types into `TypeTable`; surface mismatches in destructuring
- **Range expression operand types** — both ends of `start..end` must be numeric and compatible; step must match; used in `for x in 0..n`
- **For-in iterable must be iterable** — `for x in 42 { ... }` should error
- **For loop condition must be `bool`** — `for var i = 0; i; i += 1` should error
- **Null coalescing types** — both sides of `??` must unify via `branchCommonType`
- **Division/modulo by literal zero** — flag `x / 0`, `x % 0`, `x %% 0` with a literal-zero RHS in `inferMathBinaryExpr`
- **Always-true/always-false conditions** — warn on `if true`, `if false`, and loop conditions that are obviously constant
- **Float `==`/`!=` comparison warning** — emit a warning when two float-typed values are compared with `==` or `!=`

### Typechecker — Match expression polish
- **Boolean match exhaustiveness** — for `bool` scrutinees, check that both `true` and `false` arms are present; reject non-bool literal patterns
- **Tuple and struct match arm validation** — `checkMatchExpr` currently falls through for tuple and named-struct scrutinees without validating patterns
- **Duplicate/overlapping match arms** — detect identical literal arms or overlapping numeric ranges using the existing interval logic
- **Identifier pattern shadowing a constructor** — warn when `match foo { Some => ... }` binds `Some` as a variable instead of matching the constructor

### Typechecker — Trait/impl conformance
- **Impl must provide all required trait methods** — walk `TraitDeclStmt.Methods` minus default methods; report each unimplemented method on the `TraitImplStmt`
- **Impl method signatures must match the trait** — compare `LambdaType` parameters and return type with `types.TypesEqual`, substituting `Self` for the impl's concrete type
- **Extraneous methods in impl** — warn when `TraitImplStmt` provides a method not declared in the trait

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
### 06/10/26
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
