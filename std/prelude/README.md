# `std.prelude` — the implicitly imported module

Imported into every file, so its exports are reachable unqualified. It is an ordinary module:
resolved through the same roots, compiled standalone (it does not import itself), and not
special-cased by the compiler.

## One module, several files

Every file here begins with `module std.prelude` (checked by `pkg/modules`) and joins **one**
namespace. That matters because receiver-keyed overloading (`unwrap_or` for both `Maybe` and
`Result`) and prelude shadowing are both keyed on the module. A name may move between files
freely; split by topic.

## Constraints on what goes here

- **Exports need `pub`**; otherwise a reference from elsewhere is `lyra-E028`.
- **`@builtin(…)` markers need their argument.** `@builtin(Maybe)` requires one type parameter
  with `Some` (one payload) and `None`; `@builtin(Result)` two, with `Ok` and `Err`. A bare
  `@builtin` is no marker. `@builtin(Ord)`/`@builtin(Eq)` mark the traits the comparison
  operators dispatch to; the gate is that they declare `compare`/`eq` with two parameters.
- **Prefer free functions with a `self` receiver** — `m.unwrap_or(0)` and `unwrap_or(m, 0)` are then
  the same call. Trait impls are for satisfying a bound (`Ord`, `Add`, `Show` on the primitives).
- **Overloads need different receiver heads** (`Maybe<t>` beside `Result<t,e>`), and may live in
  different files of the module.
- **Only what Lyra cannot express is a builtin.** `read_line` and `random_seed()` are builtins;
  `parse_i64` and the `Rng` are written here.

## Documentation

Every declaration carries `///` and each file's `//!` contributes to the module doc;
`pkg/analyzer/collector/tests/prelude_docs_test.go` enforces both.

- **An implementation note (`//`) goes above the `///` block, never between it and the
  declaration** — that detaches the doc. It warns (`lyra-W017`), but `lyrac check` still exits 0;
  the test is what catches it.
- **`///` is the contract** (returns, traps, cost); **`//` is the reasoning** for whoever edits the
  file.
- **Anything that traps needs `# Panics`**; a `Result`-returning function uses `# Errors`.
- **`# Complexity` is always this table** (`TestPrelude_ComplexitySectionsAreClassified`), with
  caveats in prose after it:

  ```markdown
  |            | Best | Average | Worst  |
  | ---------- | ---- | ------- | ------ |
  | **Time**   | O(m) | O(n)    | O(n·m) |
  | **Memory** | O(1) | O(1)    | O(1)   |
  ```

  Memory means *how much*, not whether (`noalloc` already says whether). The section is optional —
  add it where cost is the point of the function.
- **`# Examples` are not compiled**; keep them short.

## Shadowing

A user declaration of a prelude name warns (`lyra-W012`) and wins in that module only. Because the
markers here claim the canonical kinds, a user's own `Maybe` is an ordinary type and `?` on it is
an error that names the shadow.

## The files

| File | Contents |
|---|---|
| `prelude.lyra` | module header and module doc |
| `maybe.lyra` | `Maybe` and its combinators |
| `result.lyra` | `Result` and its combinators |
| `array.lyra` | combinators over `[]t`, sorting |
| `seq.lyra` | `Seq<t>` combinators |
| `ordering.lyra` | `Ordering`, `Ord`, `Eq`, `min`/`max`/`clamp` |
| `math.lyra` | `Add`/`Sub`/`Mul`/`Div`, `Arithmetic` |
| `show.lyra` | `Show` |
| `format.lyra` | `to_fixed` and number formatting |
| `parse.lyra` | `parse_i64` |
| `strings.lyra` | ASCII rune classifiers, trimming, prefix/suffix, `index`, `split`, `Needle` |
| `rand.lyra` | `Rng` and the draws over `random_seed()` |
| `args.lyra` | `program_args()` |
