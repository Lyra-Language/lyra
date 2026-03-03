## To-Dos
- Refactor collector tests to use new "capture_program_print" function

- Collect if expressions
- Collect postfix expressions (i.e. foo.blah[3].baz())
- Collect function types (lambdas)
- Collect patterns
- Collect destructuring statements
- Collect match expressions
- Collect modules
- Collect trait declarations
- Collect trait implementations
- Collect math assignment operators (i.e. +=, *=, etc)
- Collect character literals (runes?)
- Collect for loops
- Collect for/in loops
- Collect arena statements
- Collect regex
- Add sizeof() function to query type sizes

## Completed

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