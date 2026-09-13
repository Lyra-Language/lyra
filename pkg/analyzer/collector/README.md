# `pkg/analyzer/collector` — CST → AST

Converts a tree-sitter CST into `*ast.Program` and `*symbols.SymbolTable`.

**Entry point:** `collector.NewCollector(source []byte)` → `c.Collect(rootNode) (program, table,
errors)`.

**Dispatch:** `collector.go` owns `CollectStatement`, `CollectExpr` (switch on `node.Kind()`) and
`ParseType`. Subpackages call back through the `Collector` interface to avoid import cycles.

| Subpackage | Handles |
|---|---|
| `declarations/` | `let`/`var`/`const`, destructuring, `if let`, traits, impls, modules |
| `typedecls/` | `struct`, `data`, named tuples, `newtype`, constrained types, attributes |
| `expressions/` | every expression kind, one file each |
| `statements/` | `for`, `for-in`, `return`, `break`, `continue`, `with`, reassignment, deref assignment |

**`collector_ctx.Ctx`** is passed first to every subpackage function: `Source`, `NodeText(node)`,
`NodeLocation(node)` (1-based), `AddError(node, severity, format, args...)`,
`MustField(node, name)` (required field, errors if missing), and the embedded `Collector`.

## CST traversal

- `node.ChildCount()` / `node.Child(i)` — all children including anonymous tokens.
- **`cst.Field(node, "field")`, never `node.ChildByFieldName`** — same answer, but it caches the
  field id instead of round-tripping a C string per lookup (~25% of pipeline time).
- `node.FieldNameForChild(uint32(i))` — when a rule repeats a field name.
- **Nil-node hazard:** `cst.Field` returns a real Go `nil` for an absent *optional* field, and
  calling any accessor on it **hangs inside the CGO binding** rather than panicking. Nil-check
  first. An optional list (e.g. a bodiless `trait`) uses `cst.Field` + nil check, not `MustField`.
- **A comment is a named node** — test `cst.IsComment`, not `IsNamed()`, when telling list elements
  from separators.

## Never return nil into the AST

An expression collector hitting an unrecoverable error (e.g. an `int64`-overflowing literal) must
report **and return a placeholder** (a zero-valued literal). A nil returned as `ast.Expression` is
a typed nil that slips past `expr == nil` and crashes a later pass. `CollectExpression`,
`CollectStatement` and `CollectPattern` pass results through `ast.TrueNil` as a backstop, but the
placeholder is still what keeps one mistake to one diagnostic.

Statement analogue: `CollectBlockExpr` skips a child that collects to nil (`isNilStmt`), because a
block's value is its final statement — a trailing comment would otherwise become it. A
comment-only body is an empty block.

## Post-passes

**Canonical types (`canonical.go`).** `resolveCanonicalTypes` stamps `TypeDeclStmt.CanonicalKind`
(`"Result"`/`"Maybe"`/`""`), the single source of truth for `?`, must-use and `??`.

- Identity comes from `@builtin(Result)`/`@builtin(Maybe)` (`TypeDeclStmt.Builtin`,
  `collectBuiltin`) — name-independent, shape-validated. A malformed marker (wrong shape, unknown
  kind, duplicate claim) is `lyra-E017`. A bare `@builtin` is no marker.
- With no marker, a type literally named `Result`/`Maybe` with the canonical shape is stamped as a
  fallback (programs with no prelude — most tests).
- It also stamps `ShadowedCanonical` and `ShapeMatchesCanonical` on an unmarked same-named type, so
  `?` can say the author re-declared the prelude's type. The advice must be remove/rename, never
  "add `@builtin`" (that is E017).
- It walks the statement list rather than calling `LookupType(kind)`, which would answer with the
  prelude's declaration instead of the shadowing one.

**Struct patterns (`reclassifyStructPatterns`).** `Pt { x, y }` and `Node { l, r }` (a data
constructor's inline record) parse identically. Once the symbol table is complete, every pattern
site is walked and a `DataPattern` naming a **struct** is rewritten to a named `StructPattern`
(`reclassifyPattern`). Downstream never asks "is this name a struct?".

**All-caps constructors (`reclassifyConstructorExprs`, `constructor_reclassify.go`).** A name like
`N` or `POINT` lexes as `const_identifier`, so it collects to an `IdentifierExpr` /
`FunctionCallExpr`. This pass rewrites a nullary constructor to `DataConstructorExpr` and an
applied constructor/named tuple to a named `TupleLiteralExpr` — the nodes PascalCase produces —
using `ast.RewriteStmt`. A value binding of the same name (e.g. `const N`) shadows the constructor
and skips the rewrite. Patterns already resolved these.

## Spellings the collector erases

- `Some 42` builds the same named `TupleLiteralExpr` as `Some(42)`
  (`collectAppliedConstructorExpr`).
- `None => break` builds the single-statement `BlockExpr` that `None => { break }` builds
  (`collectMatchArmBody`), so `MatchArm.Body` stays an expression.

**The two spellings must stay identical in the AST**, or every downstream pass becomes a place they
can differ. `TestMatchArm_BareJumpCollectsAsTheBracedForm` pins it.

## Ranges

Expression (`0..<n`), match pattern (`0..<=9`) and newtype constraint (`range(0..<=100)`) share one
grammar shape (`rangeBounds` in `tree-sitter-lyra/include/helpers.js`) and these helpers:

- **`ctx.RangeEndOperator(node, form)`** requires an end bound to say whether it is included
  (`lyra-E032`; optional in the grammar so the diagnostic can suggest both fixes). Every reader
  tests `== "<"`, so an omitted operator would silently mean inclusive. Returns `"="` after
  reporting so the node stays well-formed.
- **`collector_ctx.RangeBound(node)`** answers "was this bound written?" — not a nil check, because
  error recovery can **insert** a zero-width bound (`range(..)`). Checks both the missing flag and
  an empty span.

A nil `Start`/`End` on a `RangePattern` means an **open** range, never a malformed one; consumers
(backend match lowering, exhaustiveness's `armIntInterval`, range analysis) treat it as the type's
own limit.
