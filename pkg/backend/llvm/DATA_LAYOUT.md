# `data` / sum-type layout (LLVM lowering)

Fixes the **payload** representation of a `data` value. The `stack`/`shared` flavor
(ALLOCATION.md) independently decides whether that payload sits inline or behind a box
pointer; the two compose.

## Model (`pkg/types`)

`DataType` = `Name` + `Constructors []DataTypeConstructor`; each constructor = `Name` +
`Params []Type`. Variant kinds are `Params` shapes:

- **nullary** — empty (`None`, `Red`)
- **positional** — `[i64]`, `[i64, i64]`. The collector wraps positional fields in one
  anonymous tuple; read the flat list via `DataTypeConstructor.FieldTypes()`.
- **inline record** — `[AnonymousStructType{…}]`
- **struct-reference** — `[NamedStructType]`

## Layout

`%T = type { iTAG, [K x iA] }` (`DataUnionType`):

- **tag** — smallest unsigned int holding the variant count (`i8` in practice), first;
  assigned in declaration order. Read the index with `findConstructor`, never hard-code it.
- **payload blob** — sized to the largest variant payload and carrying the largest
  alignment. A variant's payload type is the struct of its `Params` in order (nullary →
  `{}`). An all-nullary enum is just `{ i8 }`.
- A by-value named type in a payload is sized by resolving it first (`resolveForLayout`,
  which also normalizes a `ParameterizedType` via `resolveInstantiation`).

## Construction and match

- **Construct** (`lowerDataConstruction`) goes through memory: alloca the union, store the
  tag, GEP field 1, bitcast to the variant's payload-struct pointer, store the payload,
  load the union back. A nullary variant stores only the tag; its blob is undef.
- **Match** loads the tag and `switch`es; each arm reinterprets the blob as its variant's
  payload struct the same way. Exhaustiveness is `lyra-E009`; the default block still
  traps (`sealMatchFallthrough`).

## Drop

Generated drop glue switches on the tag and drops only the **live** variant's fields
(`emitOwnedValue`, owned_walk.go), so a nullary variant's undefined blob is never read. A type whose
payloads own nothing gets no glue.

## Recursion and generics

- A recursive occurrence must be `shared` (`lyra-E014`) — a pointer in the payload — so
  the union is finite: `data List = Nil | Cons(i64, shared List)` has payload `{ i64, ptr }`.
- **Monomorphized per instantiation**: `Box<i64>` and `Box<f32>` are distinct `%T`s.

## Deferred

- Niche / tag folding (e.g. `Maybe<shared T>` as a nullable pointer).
- Sub-byte tag packing, field reordering to cut padding.
