## To-Dos
---------
### Regex engine (resharp-style)
- **Phase 3: Unicode properties** — `\p{Letter}`, `\p{Nd}`, etc.
- **Phase 4: performance** — SIMD prefix scan, byte-frequency-based acceleration, bounded DFA fast path
- Wire the engine into `RegexLiteralExpr` (typecheck regex patterns in match arms) and `PatternConstraint` (validate constrained string types)

### Typechecker

## In Progress
--------------

## Completed
------------

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
