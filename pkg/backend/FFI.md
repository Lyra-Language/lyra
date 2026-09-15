# Foreign functions — implementation notes

`extern` declares, type-checks, is charged effects, lowers to a `declare`, and `@link`
reaches the link line. Language semantics (FFI-safe types, unions, aggregates by value,
`@symbol`, `nullptr`, `std.ffi`'s surface) are in [`LANGUAGE.md`](../../LANGUAGE.md); the
settled design is in `todo.md` (Foreign functions — `extern`). Binding-module conventions
are in [`bindings/README.md`](../../bindings/README.md).

## Front end

- **`ExternDeclStmt.Func()` is the body-less function an extern is**, registered in
  `SymbolTable.Functions` so calls go through the ordinary machinery (as
  `TraitMethod.DefaultImpl()` does). `LambdaExpr.IsExtern` marks it: otherwise purity would
  call it pure and the backend would emit a blockless `define`.
- **Effects are its bound's** (`externEffects`), default `AllEffects`. Writing a bound is
  `unsafe` — it is recorded, not checked.
- **`lyra-E011`'s "call to an unsafe function" half is in the typechecker**
  (`requireUnsafeCall`), not the syntactic pass: a name does not identify a declaration
  (rule 9).
- **Private to its module, global as a symbol.** `declIsPublic` is false for an extern; two
  modules may each declare `strlen`. Modules export Lyra wrappers.
- **Parameters are named** (`lyra-E067`); a plain function *type* is refused a name, and a
  callback's own signature stays unnamed. Names are unchecked against the header but appear
  in diagnostics (`argument 2 (destLen)`).
- **Integer widths are Lyra's fixed ones, LP64 assumed.** `layout.go`'s `pointerSize` is where
  the assumption lives (also `clock.go`'s `timespec` and i128's 16/16 ABI).

| C | Lyra | C | Lyra |
|---|---|---|---|
| `char` | `i8` | `long`, `long long` | `i64` (`CLong` for `long`) |
| `unsigned char` | `u8` | `unsigned long` | `u64` (`CULong`) |
| `short` | `i16` | `size_t`, `uintptr_t` | `u64` |
| `int` | `i32` | `float` | `f32` |
| `unsigned int` | `u32` | `double` | `f64` |
| `void` | `void` | `T*`, `void*` | `^T` / `^u8` |
| `NULL` | `nullptr` | `...` | `...` (extern only) |

  `_Bool` is `bool`: declared and called as `i1 zeroext`, clang's own spelling
  (`markBoolCrossings`, `callWithDeclaredAttrs`); refused only inside a callback's signature
  (`lyra-E063`), whose thunk carries no attributes. A borrow modifier is refused.
  `CLong`/`CULong` are `pub type` aliases (not newtypes) so the LLP64 width is a grep target.
- **C variadics (`...`)** are extern-only (`lyra-E065`); `...` must come last after at least
  one named parameter, and variadic arguments must still be FFI-safe.
  `checkVariadicArguments` decides C's default promotions (narrow int → `int`, signed by the
  Lyra type; `float` → `double`) and publishes them (`TypeTable.VariadicPromotion`); the
  backend emits sext/zext/fpext and keeps no table. Declaring a variadic at fixed arity links
  and prints garbage on Apple aarch64 (stack vs registers).
- **Aggregates**: the front end admits a C-layout aggregate **unconditionally**; the backend
  refuses on a target with no classifier, so `lyrac check` answers the same everywhere.
- **Callbacks**: a function type in *parameter* position is a C function pointer (each type
  checked FFI-safe); in return position it is refused. Only a top-level function qualifies —
  `declareFunctionAs` has no environment word, so its signature *is* the C one. A closure,
  including a local that shadows a top-level function, is `lyra-E066`.
- **`@link`** on the module header or the declaration, unioned across the compile, sorted,
  deduplicated, emitted as `-l` (`lyrac`'s `linkFlags`, also in every "compile with" hint).
  `@symbol` has no module form and is refused on a header by name.

## Backend

- **`pkg/abi` decides, `backend/llvm/abi_lower.go` emits.** Only externs take this path;
  Lyra's own convention (`declareFunctionAs`) is untouched and an extern with no aggregate
  gets no plan. Coercion goes through memory (alloca, store, bitcast, load parts).
- **`planExtern` must enter the extern's module** before resolving its signature, or a named
  type stays unresolved and the declaration comes out as `%main__Big` (invisible without a
  `module` header).
- **`pushExternSignature`** makes `lowerType` read a function type as C's for one
  declaration; the call site recognises the slot by lowered type.
- **`l.externs` is keyed by C symbol** (`ExternDeclStmt.CSymbol()`), so two Lyra names for one
  function collapse to one `declare`.
- **Externs and the compiler's own libc use share one symbol space.** `l.externs` and
  `l.libc` consult each other; a signature disagreement is refused by name — `declareExtern`
  returns the error, `declareLibc` records it in `l.symbolConflict` and `emitModule` fails.
  Adding a libc function the compiler calls makes its signature a claim a program's `extern`
  must match.
- **`aggregateSpill`** checks a value against its slot before `ir.NewStore` (which panics),
  naming the argument and C function (rule 5).
- **Ownership never crosses.** A `^T` into a live array dangles at the next `push`.

### Unions

A type kind on `TypeDeclStmt` (`types.UnionType`, `typechecker/union.go`,
`backend/llvm/union.go`), so rule 8's *type* tax:

- `SizeAndAlign` → `unionSizeAndAlign` (max, not sum). Arms in `Substitute`, `TypesEqual`
  (nominal), `HeadName`, `resolveForLayout`, `recursive_type.go`, ownership's
  `eachComponent`/`hasWritableField`. `CollectTypeVars` deliberately has **none** (nominal).
- **`lowerUnionDef` resolves before it measures** — its body is computed from its size.
- Lowers to memory: `alloca`, bitcast to the member's type (as `buildDataValue` does). LLVM
  body is `{ <widest-aligned member>, [pad x i8] }` so alignment is real.
- `TestExec_FFIFixture_UnionLayoutMatchesC` checks size/align/offsets against C.

### Pointers across

- `s.cstring_ptr()` (unsafe builtin) `memchr`s for an interior NUL and yields the data
  pointer (strings carry a trailing NUL — `llvm/STRING_LAYOUT.md`). A literal's bytes are
  read-only; a C write faults.
- `p.decode_utf8(len)` copies C memory into a string; negative length traps.
- `xs.data()`/`data_mut()` copy nothing, trap on empty, dynamic arrays only.
- **A libc function expressible in Lyra is written in Lyra** (`cstring_len`). There is no
  `std.libc`.

## Tests

- `llvm_extern_test.go` calls **libc and libm**.
- `llvm/testdata/ffi_fixture.c` covers narrow widths, `float`, mixed register classes,
  spilling argument lists, `CLong`/`CULong`, out-parameters, struct by pointer, `data()`.
  Expected values must come from **`testdata/ffi_oracle.c`** (a pure-C caller), and the
  compile cache must stay salted with the fixture's bytes.
- `pkg/abi`'s `abi_diff_test.go` checks the classifier against clang (19 shapes × 3 targets,
  parameter and return).
- `TestExample_ZlibRoundTrips` runs `examples/zlib.lyra` through the **CLI** (the harness
  hardcodes `-lm`). It self-skips without zlib, which is safe only because `asan.Dockerfile`
  and CI install `zlib1g-dev` — keep the comment beside each `apt-get`.
