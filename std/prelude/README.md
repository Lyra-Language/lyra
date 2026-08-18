# `std.prelude` — the implicitly imported module

Implicitly imported into every file, so everything declared here is reachable unqualified and
without an `import`.

It is an ordinary module — the compiler resolves it through the same search roots as any
other, and it compiles standalone (it does not import itself). Nothing here is special-cased in
the compiler, which is what lets it be read, tested and replaced.

## It is one module in several files

Every file in this directory begins with `module std.prelude` and joins **one** namespace: one
scope, one set of declaration keys, one overload set per name. The header is required and
checked — `pkg/modules` refuses a file here that declares anything else, or nothing.

That the split is *within* a module rather than into several modules is the whole point, and
two rules make it so:

- **Receiver-keyed overloading is per-module.** `unwrap_or` for `Maybe` and `unwrap_or` for
  `Result` may coexist only because they are declared in the same module. In separate modules
  they would be a cross-module duplicate.
- **Prelude shadowing is keyed on the prelude module.** A user declaration taking a prelude
  name warns (`lyra-W012`) and wins locally; that rule asks whether the name belongs to
  `std.prelude`, not to whichever file holds it.

So a name may move between these files freely, and nothing observable changes. Split by topic,
not by anything the language can see.

## Four constraints on what may go in it

- **Exports need `pub`.** A declaration without it is private to this module, and a reference
  from elsewhere is `lyra-E028`.

- **`Maybe` and `Result` are shape-validated** by the `@builtin(…)` attribute that gives them
  their compiler-known identity: `Maybe` takes one generic parameter with `Some` (one payload)
  and `None` (none); `Result` takes two with `Ok` (one) and `Err` (one). The names are free —
  the marker confers the identity, not the spelling — but the arities are not. **The argument
  is not optional:** a bare `@builtin` collects as no marker at all, and the type then gets its
  identity only from the fallback that reads the literal name `Maybe`/`Result`, which is
  precisely the coupling to spelling the marker exists to remove.

  **`Ord` and `Eq` are marked the same way** (08/08), and for a sharper reason: the compiler
  owns the operators that dispatch to them, so it has to *find* them. The gate here is that
  the trait declares the method the compiler will call (`compare`/`eq`, two parameters); the
  return type is left to the impl, since the backend reads `Ordering` off the matched impl's
  own signature rather than assuming it. Before the marker the name was the identity, so a
  program's own `trait Ord` was silently taken for this one.

- **Write free functions, not trait impls** — and name the receiver `self`, which is what makes
  `m.unwrap_or(0)` work as well as `unwrap_or(m, 0)` (UFCS, 08/03). The method spelling costs
  nothing: it is rewritten to the call form before anything downstream sees it.

  A trait impl is the thing to avoid here. It type-checks, and a *generic* one — which every
  combinator here would be — then fails in the backend, because an impl method's body is not
  specialized per instantiation ("match on Maybe<t> not implemented yet"). A non-generic impl
  method does lower; this module has no use for one.

- **A name may be declared twice, if the two take different receivers** — receiver-keyed
  overloading (08/03). Both declarations must take a `self` parameter and their receiver types
  must have different *heads*: `Maybe<t>` beside `Result<t,e>` is fine, a second `Maybe<…>` is
  refused where it is written. That is what lets `unwrap_or` mean both types rather than one of
  them getting a name the other did not need. Everything else about a redeclaration is
  unchanged — two functions without receivers are the error they always were. **The two
  declarations need not be in the same file**, only in the same module, which is why
  `maybe.lyra` and `result.lyra` can each have their own.

## Documentation

Every declaration here carries a `///` block, and each file's `//!` header contributes a
paragraph to the module's own documentation. `pkg/analyzer/collector/tests/prelude_docs_test.go`
enforces both against the real sources — an undocumented export in the prelude is the one
gap every user of the language sees.

Three conventions, the first of which is a rule and not a preference:

- **An implementation note (`//`) goes *above* the `///` block, never between it and the
  declaration.** Attachment is adjacency, so a comment in between detaches the
  documentation. It warns (`lyra-W017`) rather than failing silently — but note that a
  warning leaves `lyrac check` exiting 0, so the test above is what actually catches it.

- **`///` is the contract; `//` is the reasoning.** What a caller needs to know — what it
  returns, when it traps, what it costs — goes in the doc block. Why the code is written
  the way it is (why `parse_i64` accumulates negatively, why `below` rejects the top
  bucket) stays an ordinary comment: it is for whoever edits this file, not for whoever
  calls it. Several entries here have both, and the split is worth preserving.

- **Anything that traps needs a `# Panics` section.** A trap is invisible in a signature,
  so the doc is the only place it can be stated. `# Errors` is the sibling for a
  `Result`-returning function, and the two are deliberately different sections.

- **A `# Complexity` section is a table, and always this table:**

  ```markdown
  |            | Best | Average | Worst  |
  | ---------- | ---- | ------- | ------ |
  | **Time**   | O(m) | O(n)    | O(n·m) |
  | **Memory** | O(1) | O(1)    | O(1)   |
  ```

  Uniform across the prelude rather than left to each author, and
  `TestPrelude_ComplexitySectionsAreClassified` enforces it: a reference page is
  *scanned*, so a reader looking for the worst case wants it in the same cell every
  time, and `Worst` beside `Worst case` beside `Max` is how that erodes. A row of
  identical cells is a real answer — it says the cost does not depend on the input —
  and the cells that differ are the reason the table beats a sentence: `index` is O(n)
  on ordinary text and O(n·m) against a needle built to almost-match, and `below` has
  **no** worst case at all, which is exactly what rejection sampling trades for
  uniformity. Put the caveats in prose *after* the table.

  **Memory means how much, not whether.** `noalloc` in the signature already answers
  whether, and is checked; a row restating it is a second copy of a machine-checked
  fact. The section is optional — `O(1)` on a two-line combinator is noise — so add it
  where the cost is the reason the function exists.

`# Examples` blocks are prose. **Nothing compiles them**, so they can rot; keep them short
and obvious until a doctest runner exists.

## Shadowing

A name declared here can be shadowed by user code: that warns (`lyra-W012`) and the user's
declaration wins, so adding a name here cannot break a program that never mentioned it. The
shadow is confined to the module that declared it — another module still gets the version here
— and since 08/01 that holds for a **type** or **trait** too, which used to be replaced
program-wide.

Note what a shadow still means for the module that makes one: the markers here claim the
canonical kinds, so a user's own same-named `Maybe` is an ordinary type, and `?` on it reports
"`?` operand must be a Result or Maybe, got Maybe". That no longer reaches a module which never
mentioned `Maybe` — which is what made it indefensible — but the message is still poor for the
module that did. See `todo.md`.

## What belongs here rather than in the compiler

Anything expressible in Lyra. `read_line` is a builtin because the line comes from libc and
Lyra has no FFI; `parse_i64` is written here because it can be. `random_seed()` is a builtin
because OS entropy is not arithmetic; the generator built on it is `rand.lyra`. The builtin
registry stays whatever is genuinely primitive, and everything else lives here, where it is
readable, testable and replaceable.

## The files

| File | What is in it |
|---|---|
| `maybe.lyra` | `Maybe` and its combinators |
| `result.lyra` | `Result` and its combinators |
| `array.lyra` | `map`/`filter` over `[]t` |
| `ordering.lyra` | `Ordering`, the result of `<=>` |
| `show.lyra` | `Show`, so a bounded type parameter can be formatted |
| `parse.lyra` | `parse_i64` |
| `strings.lyra` | `is_ascii_space`, `trim`/`trim_start`/`trim_end` |
| `rand.lyra` | `Rng` and the draws built on `random_seed()` |
