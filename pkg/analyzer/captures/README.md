# `pkg/analyzer/captures` — closure free variables

For every lambda, the bindings its body reads from an enclosing scope, which the backend copies
into the closure environment. `captures.Analyze(program, symTable, tt) *Table` runs after
typechecking (it reads each capture's type) and reports nothing; `Table.Of(lambda)` returns
captures sorted by name, so environment layout is deterministic.

**The rule:** a name read inside the lambda, not bound inside it, and not a global (top-level
binding, type or `data` constructor, primitive type keyword — `u8(x)` — or `print`/`println`) is a
capture.

- **Flow-insensitive**, which is sound: reading an outer binding later shadowed by an inner one is
  already a use-before-declaration error.
- **Binders are enumerated explicitly** (`addPatternNames` plus statement/expression cases) —
  match arms have no recorded scope, and the generic walker reaches a C-style loop's `Init` only as
  an expression. A binder missing here reads as a capture.
- **A global is subtracted only where nothing in the enclosing lambdas shadows it**
  (`analyzer.outer`); otherwise a parameter sharing a top-level name gets no slot.
- **Both failure directions are loud**: a spurious capture is a wasted copy or a backend error; a
  missed one errors because a lifted body starts with an empty local set.
- **A local generic that captures nothing is not captured** — a lambda calling it builds its
  closure at the call. One that captures stays a capture, expanded by the backend into one closure
  per instantiation the lambda calls (`local_generic.go`).

Captures are **by value**, copied at closure creation (no escape analysis, so by-reference would
dangle). Assigning to a captured binding, or taking `&mut` of one, is refused as `lyra-E024`
(`checker/captured_assignment.go`) rather than compiled into a write that vanishes.
