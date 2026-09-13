# `string` layout (LLVM lowering)

## Representation

A `string` is an **immutable fat pointer**, passed and returned by value
(`SizeAndAlign(string) = 24, 8`):

```llvm
{ i8* data, i64 byte_len, i64 rune_count }   ; StringLLVMType()
```

- **`data`** — first UTF-8 byte, always `rcHeaderSize` into a ref-counted box. The length
  is **authoritative**: interior NULs are legal bytes.
- **`byte_len`** — bytes; `s.byte_len()` reads it.
- **`rune_count`** — code points; `s.len()` reads it (O(1)). The language's `len()`
  counts runes because `s[i]` and `for c in s` do.

**Every construction site must fill `rune_count`**, and does so arithmetically: a literal
counts at compile time, `++` adds, `slice` subtracts. Only byte-sourced producers
(`read_line`, interpolation's formatted segments, `decode_utf8`) pay a
`lyra_utf8_count` pass (counts non-continuation bytes; no validation). A missed site is a
silently wrong `len()` — `TestExec_StringRuneCountAgreesEverywhere` is the ledger.
`makeString(block, data, byteLen, runeCount)` is the one constructor.

Every string value is a box, so refcounting is uniform (ALLOCATION.md):

- **Literal** — a private constant global holding a *pinned* box
  (`pinnedBoxConstant`, rc = `PinnedRC`), so retain/release no-op. No allocation.
- **Heap** — `++`, `slice`, interpolation, `read_line`, `decode_utf8`: a real box via
  `rcAllocStringPayload`.

## The trailing NUL

Every string has a **NUL at `data[byte_len]`**. Nothing in the language reads it; it
exists so `s.cstring_ptr()` (`unsafe`) can hand C the string's own bytes after one
`memchr` for an interior NUL (`lyra_panic_interior_nul`). `std.ffi`'s `with_cstring` is
one line over it and is `pure noalloc`.

Invariant: **every producer allocates and writes the extra byte.** Heap allocations
funnel through `rcAllocStringPayload`; a literal's global is `[N+1 x i8]`; `read_line`
tests `len + 1 < cap` as it grows. `TestExec_EveryStringProducerIsNULTerminated` checks
each producer with C's `strlen`.

A literal's bytes are genuinely read-only: a C function that writes through a
`cstring_ptr` of a literal faults.

## Operations

- **Equality** (`lowerStringEquality`) — branchless
  `len_a == len_b && memcmp(data_a, data_b, min(len_a, len_b)) == 0`; `memcmp` over the
  min never reads past either buffer, and `n = 0` is valid.
- **`match`** — a string scrutinee uses the scalar ladder (`lowerScalarMatch`); each
  literal arm is an equality test, a regex arm a DFA test (`regex_match.go`). Pattern
  text is raw-quoted, so quotes are stripped; **escaped string patterns** are a loud
  error (they would need Lyra's own unescaping).
- **`++`** (`lowerStringConcat`) — allocate a box, `memcpy` both operands, counts add.
  `memcpy` of length 0 is fine, so an empty operand needs no case.
- **`slice(start, end)` copies**, it does not borrow: the box header is at the box's
  *start*, so a pointer into the middle cannot be retained or released. Hence `noalloc`
  refuses `slice`.
- **Indexing, `from_end`, `byte_offset`, `compare_bytes_at`** — see README.md
  ("String indexing", "Byte-level string primitives").

## Deferred

- Escaped string patterns (above).
