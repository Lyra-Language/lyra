# `pkg/analyzer/collector` — CST → AST

Converts a tree-sitter CST into `*ast.Program` and `*symbols.SymbolTable`.

**Entry point:** `collector.NewCollector(source []byte)` → `c.Collect(rootNode) (program, table,
errors)`

**Dispatch:** `collector.go` owns `CollectStatement` and `CollectExpr` (switch on
`node.Kind()`), and `ParseType`. Subpackages call back into the root collector via the
`Collector` interface to avoid circular imports.

**Canonical-type resolution (`canonical.go`):** `Collect` finishes with `resolveCanonicalTypes`,
which stamps `TypeDeclStmt.CanonicalKind` ("Result"/"Maybe"/"") — the single source of truth for
whether a type is the compiler-known Result/Maybe that `?`, must-use, `??`, and the try-context
check key off. Identity is conferred by a `@builtin(Result)`/`@builtin(Maybe)` attribute
(collected onto `TypeDeclStmt.Builtin` via `collectBuiltin`, reusing the `@derive` attribute
grammar — no grammar change), which is *name-independent* (a type named `Either` can be the
canonical Result) but shape-validated; with no marker, an unmarked type literally named
"Result"/"Maybe" with the canonical constructor shape is stamped as a fallback — the path a
program with no prelude in its search roots takes, which is most tests, though `std/prelude.lyra`
now marks its own types so a normal build goes through the marker. A malformed marker (wrong
shape, unknown kind, duplicate claim) is `lyra-E017`. Recognition sites read the stamp via the
symbol table and keep a name+arity fallback only for a truly undeclared ambient annotation.

Once a marker claims a kind, a same-named *unmarked* declaration is an ordinary type — right,
but it used to surface as `` `?` operand must be a Result or Maybe, got Maybe ``. So the same
pass also stamps `ShadowedCanonical` (the kind the declaration looks like but is not) and
`ShapeMatchesCanonical` (whether it would otherwise have qualified), which is all `?` needs to
say whether the author re-declared the prelude's type or gave an unrelated type its name. It is
stamped here rather than re-derived at the diagnostic, so the shape test has one home.

Two traps that pass live in that stamp. It walks the statement list rather than reading
`c.table.Types[kind]`, because a declaration shadowing a prelude name is keyed `<module>::<name>`
so the prelude keeps the bare key — the lookup returns the prelude's declaration, the one this is
not about. And the advice it enables must never be "mark it `@builtin(Maybe)` too": that is a
duplicate claim, `lyra-E017`, so the message says remove or rename instead.

**Struct-pattern reclassification (`reclassifyStructPatterns`, `collector.go`):** after
`walkProgram`, `Collect` walks every pattern site (match arms, destructuring `let`s, `if
let`/`else`, lambda params/clauses — via `ast.WalkStmt`/`WalkExpr` to reach the containers) and
rewrites a `DataPattern` whose name is a declared **struct** type into a named `StructPattern`
(`reclassifyPattern`, recursing into sub-patterns). Needed because `Pt { x, y }` (a struct
pattern) and `Node { l, r }` (a data-constructor inline-record pattern) are syntactically
identical — both parse to a `DataPattern` with an inner `StructPattern` payload — so the split
is *semantic* and can only run once the symbol table is complete (a forward-referenced struct
still resolves). A name that is a data constructor (or unknown) stays a `DataPattern`.
Downstream (typechecker, backend) therefore sees `StructPattern` for structs and `DataPattern`
for data variants, no per-site "is this name a struct?" branching.

**Constructor-expression reclassification (`reclassifyConstructorExprs`,
`constructor_reclassify.go`):** the same post-pass idea for *expression* position. An all-caps /
single-capital constructor or named-tuple name (`data Dir = N | S | E | W`, `tuple POINT(…)`)
lexes as a `const_identifier` (the token reserved for constants, `/[A-Z][A-Z0-9_]*/`;
`user_defined_type_name` needs a lowercase letter to be unambiguous), so a bare use collects to
an `IdentifierExpr` and an applied use `FOO(3)` to a `FunctionCallExpr` — not the
`DataConstructorExpr` / named `TupleLiteralExpr` a PascalCase constructor yields, and the
typechecker then reported a misleading "undefined identifier". This pass (run after the symbol
table is complete, so a forward-referenced constructor resolves) rewrites a bare
nullary-constructor name into a `DataConstructorExpr` and an applied constructor/named-tuple
call into a named `TupleLiteralExpr` — the exact nodes PascalCase produces — so all downstream
passes handle them identically with no special-casing. It reassigns each expression slot in
place (mirroring `ast.walkExprChildren`, since a visitor can't). A value binding of the same
name (a `const N`, checked against the global scope) shadows the constructor and skips the
rewrite, so existing constant code is untouched. Pattern position already resolved these
constructors, so only expressions needed it.

**Nil-node hazard:** `cst.Field(node, ...)` returns a genuine Go `nil` `*sitter.Node` for
an absent *optional* grammar field (e.g. a zero-parameter `lambda_type`'s `parameter_types`).
Calling any accessor (`ChildCount`, `Child`, `Kind`, …) on that nil node **hangs inside the
go-tree-sitter CGO binding instead of panicking** — found via a real bug (`parseParameterTypes`,
fixed 06/24/26) where this silently froze the whole collector. Always nil-check before touching
the result of an optional field lookup, the same way `parseType`/`CollectExpression` already do.

**Never return a nil expression node into the AST:** an expression collector that hits an
unrecoverable value error (e.g. a numeric literal that overflows `int64`) must emit a diagnostic
**and return a placeholder node** (a zero-valued `IntegerLiteralExpr`/`FloatLiteralExpr`), never
`nil`. A `nil` returned up as an `ast.Expression` becomes a *typed-nil* interface (`(*T)(nil)`,
non-nil interface with a nil pointer) that slips past `expr == nil` checks and crashes a later
pass on the first field access — this is exactly how an out-of-range literal panicked
`propagateLiteralType` (fixed 07/24/26, `numeric_literals.go`). The error diagnostic keeps the
program from compiling, so the placeholder value is inert. **The statement analogue (block
bodies):** `CollectBlockExpr` skips a child that collects to nil (`isNilStmt` — untyped and
typed nils) rather than appending it, because a block's *value is its final statement* — a
trailing `comment` (a named CST child that collects to nil) would otherwise become the block's
value and miscompile (the backend returned garbage for `a + b // c`; fixed 07/25/26). A
comment-only body collects to an empty block.

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
- `node.ChildCount()` / `node.Child(i)` — all children including anonymous keyword tokens; use
  with `switch child.Kind()`
- `cst.Field(node, "field")` — first child with that field name. **Use this, not
  `node.ChildByFieldName`**: the two answer identically, but ChildByFieldName allocates a C
  string from the Go name, calls into C and frees it on *every* lookup, which made it about a
  quarter of all samples in an analysis run — the collector asks at nearly every node.
  `cst.Field` resolves the name to a grammar field id once and reuses it, and moving the
  collector onto it made the whole pipeline ~25% faster (`pkg/cst`, and the benchmarks in
  `pkg/driver/bench_test.go`)
- `node.FieldNameForChild(uint32(i))` — field name at index `i`; use when a rule repeats the
  same field name (e.g. multiple `value:` fields in `commaSep1`)

## Ranges: one notation, one strictness rule

The `..` notation has three sites — an expression (`0..<n`), a match pattern (`0..<=9`), and
a `newtype` range constraint (`range(0..<=100)`). Since 08/01 they share one grammar shape
(`rangeBounds` in `tree-sitter-lyra/include/helpers.js`) and one collector check here:

- **`ctx.RangeEndOperator(node, form)`** enforces that a range with an end bound says
  whether that bound is included (`lyra-E032`), and returns the operator. The operator is
  *optional in the grammar* at all three sites and required here, deliberately: every reader
  of the collected operator tests `== "<"`, so an omitted one fell through to **inclusive**
  and `0..9` silently meant `0..<=9`. A diagnostic naming both fixes beats a syntax error
  pointing at whichever token failed to shift — the same trade `lyra-E029` made for modifier
  order. The suggestion is spliced from the source at the first `..`, so it is right for
  open-start (`..9`) and stepped (`0..10:2`) forms too. Returns `"="` after reporting so the
  caller still builds a well-formed node (hazard 3).

- **`collector_ctx.RangeBound(node)`** answers "was this bound actually written?" — and it
  is not a nil check. Where the grammar *requires* a bound, tree-sitter's error recovery can
  **insert** one to keep parsing: `range(..)` yields a zero-width `decimal_int` sitting on
  the `)`. A plain nil check reads that insertion as a bound of value zero, which is how the
  long-standing "range constraint must have a start or end" check would have started passing
  silently. Both the missing flag and the empty span are tested, because the recovery does
  not always set the former.

A nil `Start` or `End` on a collected `RangePattern` therefore means an **open** range
(`10..`, `..<0`), never a malformed one — a bare `..` does not parse. Every consumer must
read it that way; the ones that exist are the backend's match lowering, the exhaustiveness
check's `armIntInterval`, and the range analysis's pattern refinement, and all three treat
an absent bound as the scrutinee type's own limit.
