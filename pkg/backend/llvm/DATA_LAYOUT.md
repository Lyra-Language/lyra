# `data` / sum-type layout (LLVM lowering)

How Lyra `data` (sum) types lower to LLVM. Companion to ALLOCATION.md: this fixes
the **payload** representation of a `data` value; the `stack`/`shared` flavor
(ALLOCATION.md) independently decides whether that payload sits inline or behind a
ref-counted `ptr`.

## Model (from `pkg/types`)

`DataType` = `Name` + `Constructors []DataTypeConstructor`; each
`DataTypeConstructor` = `Name` + `Params []Type` (its payload fields, in order).
Variant kinds are just different `Params` shapes:

- **nullary** — `Params` empty (`None`, `Red`)
- **positional** — `Params = [i64]` / `[i64, i64]` (`C i64`, `C (i64, i64)`)
- **inline record** — `Params = [AnonymousStructType{…}]` (`C { x: T }`)
- **struct-reference** — `Params = [NamedStructType]` (`C MyStruct`)

## Decision summary

A `data` value is a **tagged union**: `%T = type { iTAG, <payload blob> }`.

- **tag** = the smallest unsigned integer that holds the variant count (`i8` for
  ≤256 variants, which is all realistic cases), placed first. Tags are assigned in
  declaration order (0, 1, 2, …).
- **payload** = a blob sized/aligned to the **largest** variant payload, accessed
  as the active variant's payload struct via a typed load/store at the payload
  offset (field 1).

The tagged union is the payload; the allocation flavor wraps it — `stack data` is
the union **inline**, `shared data` is a **`ptr`** to a ref-counted box whose
payload is the union (see ALLOCATION.md). The two decisions compose and are
orthogonal.

## Layout

- A variant's **payload type** is the struct of its `Params` in order: nullary →
  `{}` (zero-size); inline-record → the anonymous struct; struct-ref → a field of
  that struct (in its own flavor); positional → `{ p0, p1, … }`.
- **Union size/align** = the max size and max alignment across all variant payload
  structs.
- Emit `%T = type { iTAG, [N x i8] }` where `[N x i8]` is over-aligned to the union
  alignment (or, equivalently, use the largest-payload struct as the storage member
  so it carries the alignment). With opaque pointers, variant access is: `getelementptr`
  to field 1, then `load`/`store` typed as that variant's payload struct.

## Construction

- `Cons(1, tail)` → materialize `%T` (stack slot or `lyra_rc_alloc` box per flavor);
  `store` tag = index(`Cons`); GEP the payload; `store` the payload struct `{ 1, tail }`.
- `None` → store the tag; leave the payload undefined (or zero it).

## Match / destructuring

- `load` the tag, `switch` on it; per arm, GEP the payload and `load` it typed as
  that variant's payload struct, then extract the bound fields. The front-end already
  guarantees exhaustiveness (`lyra-E009`), so a well-typed `match` needs no default —
  emit an `unreachable` default defensively.

## Drop

- A generated drop function `switch`es on the tag and drops the **active** variant's
  payload fields (release for `shared` fields, recurse into aggregates). A data type
  whose payloads are all trivial (no `shared`/string/array) needs no drop.

## Recursive & `shared`

- A recursive occurrence must be `shared` (`lyra-E014`), i.e. a `ptr` in the payload,
  so the union has finite size. `shared data List = Nil | Cons(i64, List)`: the
  `Cons` payload is `{ i64, ptr }`, and a `List` value is a `ptr` to a box of the
  union.

## Generics

- **Monomorphize per instantiation** (chosen — value semantics, layout-friendly,
  matches the packed/`fixed` goals): `Box<i64>` and `Box<f32>` get distinct `%T`s,
  same as generic structs. (Type-erased/boxed generics are the alternative; not
  chosen.)

## Deferred

- **Niche / tag-fold optimization** (Rust-style: drop the tag when a variant has a
  spare bit-pattern) — most valuable for `Maybe<shared T>` / `Maybe<ptr>` (None =
  null) and for `Result`, which are hot. Deferred; always emit an explicit tag first.
- Sub-byte tag packing, field reordering to cut padding, and C-ABI union
  compatibility are later optimizations.
