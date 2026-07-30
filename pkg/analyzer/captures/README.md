# `pkg/analyzer/captures` — closure free variables

Computes, for every lambda, the bindings its body reads from an enclosing scope — its free
variables, which the backend copies into a closure environment. `captures.Analyze(program,
symTable, tt) *Table` runs after typechecking (it reads the TypeTable for each captured
binding's type) and produces no diagnostics of its own; `Table.Of(lambda)` returns the captures
in a stable order (sorted by name), so the environment layout is deterministic.

The rule is deliberately simple: a name read anywhere inside the lambda that is not bound
anywhere inside it, and is not a global (a top-level binding, a declared type or `data`
constructor, a primitive type keyword — `u8(x)` is a call on a bare name — or
`print`/`println`), is a capture. Both halves are **flow-insensitive**, which is sound because a
read of an outer binding later shadowed by an inner declaration is already a
use-before-declaration error, so no valid program distinguishes the two readings. Binder forms
are enumerated explicitly (`addPatternNames` plus the statement/expression cases) because the
collector records no scope for a match arm, and because the generic AST walker reaches a C-style
loop's `Init` only as an *expression* — missing that made every `for var i = …` inside a lambda
read as a capture.

Both failure directions are loud rather than silent, which is what makes the simplicity safe:
capturing a name that was really local costs a wasted copy at worst (the body's own binding
shadows it) and errors in the backend if no enclosing binding exists, while missing a genuine
capture errors too — a lifted body starts with an empty local set, so a name that is neither a
parameter, a capture, nor a global has nowhere to come from.

Captures are **by value**, copied when the closure is created, which is what lets a closure
outlive the frame its captured bindings live in (the alternative, capturing the slot by
reference, is a dangling pointer the moment that frame returns, and Lyra has no escape analysis
to tell the safe case apart). The visible consequence is that assigning to a captured binding
inside a lambda cannot affect the enclosing one — rejected as `lyra-E024`
(`checker/captured_assignment.go`) rather than compiled into a write that vanishes, the same
failure a by-value `mut` parameter had.
