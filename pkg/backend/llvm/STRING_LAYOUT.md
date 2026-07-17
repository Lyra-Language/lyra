# `string` layout (LLVM lowering)

How Lyra `string` lowers to LLVM. ALLOCATION.md lists the string representation as
"a separate lowering decision, not this item" — this is that decision.

## Decision summary

A `string` is an **immutable fat pointer**:

```llvm
{ i8* data, i64 len }   ; StringLLVMType()
```

- **`data`** — a pointer to the first UTF-8 byte. **Not** NUL-terminated (the
  length is carried explicitly), so embedded NULs are fine and the bytes can point
  into read-only memory shared with other strings.
- **`len`** — the length in **bytes** (`i64`), not code points. O(1) `.len`; code
  points / grapheme counts are a library concern over the bytes.

Passed and returned **by value** (16 bytes = two words) — like a small tuple, so a
`let`-bound string round-trips through `alloca`/`store`/`load` and mem2reg promotes
it. `SizeAndAlign(string) = 16, 8`.

### Why a fat pointer (not `i8*` / not a length-prefixed heap object)

- vs. NUL-terminated `i8*` (C): that costs O(n) length, forbids embedded NULs, and
  forces every literal to carry a terminator. A modern UTF-8 language wants O(1)
  length and NUL-clean bytes — matches Lyra's Unicode-scalar `char`.
- vs. a heap object `{ rc, len, [bytes] }`: that forces an allocation (and a
  refcount) even for a literal. The fat pointer lets a **literal** point straight
  into a global constant with **no allocation**, while a future **heap** string
  (concatenation) points into a ref-counted box (ALLOCATION.md) — the same
  `{ptr, len}` shape serves both; only the ownership/free story differs.

This is the Go `string` / Rust `&str` / Swift contiguous-UTF-8 representation.

## What lowers today (no allocator needed)

- **Literals** (`lowerStringConstant`) — the bytes are interned in a private,
  immutable global `[N x i8]`; the value is `{ getelementptr-to-first-byte, N }`.
  The collector already unescaped `StringLiteralExpr.Value`, so the global holds
  the exact runtime bytes.
- **Equality `==` / `!=`** (`lowerStringEquality`) — branchless:
  `len_a == len_b && memcmp(data_a, data_b, min(len_a, len_b)) == 0`. `memcmp` over
  `min` never reads past either buffer (so it's memory-safe even when the lengths
  differ — the length check then rejects), and `n = 0` is a valid no-op (two empty
  strings are equal). `memcmp` is libc (clang links it); no custom runtime. Strings
  have no ordering (`< > …` require numeric operands in the typechecker).
- **`match`** — a string scrutinee joins the shared scalar if-else ladder
  (`lowerScalarMatch`); each literal arm is a `lowerStringEquality` test, an
  identifier catch-all binds the whole fat pointer. A string pattern's source text
  is raw-quoted (unlike a `StringLiteralExpr`), so the quotes are stripped for the
  bytes.
- **Params / returns / `let`** — by-value aggregate, works via `lowerType` and the
  aggregate path in `emitReturn`.

## Deferred

- **Concatenation `++` and interpolation** — produce a *new* string, which needs a
  heap allocation (`lyra_rc_alloc` from ALLOCATION.md + `memcpy`) and the
  ownership/free story. This is the natural next task; it's where the runtime
  allocator first gets exercised for strings. Both error loudly today.
- **`print` / `println` of a string** — needs the output shim (`write(1, data,
  len)`); observable via stdout rather than the exit code.
- **Escaped string patterns** (`"a\tb" =>`) — the pattern text is raw-quoted and
  would need Lyra's own unescaping (distinct from Go's) to match the collector's
  literal bytes; deferred with a loud error rather than risking a silent mismatch.
- **Regex patterns** in a string `match` (`r/…/`) — the engine exists
  (`pkg/regex`) but isn't wired into codegen.
- **Ownership / freeing** — a literal points to static memory (never freed); once
  heap strings exist, release/retain follows the ALLOCATION.md `shared` box rules.
