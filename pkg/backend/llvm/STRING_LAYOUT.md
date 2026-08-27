# `string` layout (LLVM lowering)

How Lyra `string` lowers to LLVM. ALLOCATION.md lists the string representation as
"a separate lowering decision, not this item" — this is that decision.

## Decision summary

A `string` is an **immutable fat pointer**:

```llvm
{ i8* data, i64 byte_len, i64 rune_count }   ; StringLLVMType()
```

- **`data`** — a pointer to the first UTF-8 byte. The length is carried explicitly and is
  **authoritative**, so embedded NULs are fine and the bytes can point into read-only
  memory shared with other strings. Since 08/26 a NUL also sits **past** the end, at
  `data[byte_len]` — see below; no reader consults it, and it changes nothing about how a
  string is measured, indexed or compared.
- **`byte_len`** — the length in **bytes** (`i64`). `s.byte_len()` reads it.
- **`rune_count`** — the length in **code points** (`i64`), added 08/12 so that
  `s.len()` — which is rune-indexed, agreeing with `s[i]` and `for c in s` — is a
  field read rather than an O(n) decode walk. Maintained **arithmetically** at each
  construction: a literal counts at compile time, `++` adds its operands' counts,
  `slice` subtracts its rune bounds; only byte-sourced producers (read_line,
  interpolation's formatted segments) pay one `lyra_utf8_count` pass — a lead-byte
  counter, no decoding — over bytes they just produced. **Every construction site
  must fill the field**; a missed one is a silently wrong `len()`, which is what
  `TestExec_StringRuneCountAgreesEverywhere` (the count ledger) exists to catch.

Passed and returned **by value** (24 bytes = three words) — like a small tuple, so a
`let`-bound string round-trips through `alloca`/`store`/`load` and mem2reg promotes
it. `SizeAndAlign(string) = 24, 8`.

### Why a fat pointer (not `i8*` / not a length-prefixed heap object)

- vs. NUL-terminated `i8*` (C): that costs O(n) length, forbids embedded NULs, and
  makes the terminator load-bearing. A modern UTF-8 language wants O(1) length and
  NUL-clean bytes — matches Lyra's Unicode-scalar `char`. **Carrying a terminator is not
  the same choice**: the length still decides everything, so an interior NUL is still a
  legal byte and nothing is O(n) that was not before.
- vs. a heap object `{ rc, len, [bytes] }`: that forces an allocation (and a
  refcount) even for a literal. The fat pointer lets a **literal** point straight
  into a global constant with **no allocation**, while a future **heap** string
  (concatenation) points into a ref-counted box (ALLOCATION.md) — the same
  `{ptr, len}` shape serves both; only the ownership/free story differs.

This is the Go `string` / Rust `&str` / Swift contiguous-UTF-8 representation.

## The trailing NUL (08/26)

Every string carries a **NUL at `data[byte_len]`**, one byte past its own bytes. Nothing in
the language reads it: the length remains authoritative, so `len`, `byte_len`, indexing,
`slice`, equality and iteration are all exactly what they were, and a string may still hold
an interior NUL.

**It exists so a string can be handed to C without copying it.** `s.cstring_ptr()` (an
`unsafe` builtin) checks for an interior NUL with one `memchr` pass and yields `data`
directly; `std.ffi`'s `with_cstring` is one line over it and is now `pure noalloc`. Before,
crossing meant `encode_utf8` into a fresh array, a scan, and a `push(0)` — an allocation and
two passes at **every** call. Measured on 200,000 crossings of a 26-byte string through
`strlen`, same program either way:

| | per crossing |
|---|---|
| copy (`encode_utf8` + scan + `push`) | **146 ns** |
| the string's own bytes | **8 ns** |

**The invariant is "every producer allocates the extra byte and writes it"**, and it holds by
construction at six sites: the four heap allocations funnel through `rcAllocStringPayload`,
a literal's bytes get the NUL in their global constant (`[N+1 x i8]`), and `read_line`
reserves it by testing `len + 1 < cap` as it grows. A seventh producer that forgets is what
`TestExec_EveryStringProducerIsNULTerminated` is for — it asks C, through `strlen`, whether
the terminator sits exactly at the length.

**The bytes are immutable, and a literal's are genuinely read-only.** `cstring_ptr` yields a
`^u8`, so Lyra cannot write through it (lyra-E061), but C can — and for a literal that is a
`private constant` global, so it faults. This is not theoretical: it turned an FFI test that
had been passing `p` as `strtoul`'s `char**` endptr — writing eight bytes through it — from
quietly working into a segfault, because the buffer had been a heap copy and became the
literal itself.

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

## What lowers now (heap-allocating)

- **Concatenation `++`** (`lowerStringConcat`) — a concatenated string is the
  first value this backend puts on the heap: its bytes don't exist until run
  time, so it can't point into a constant global. It allocates a ref-counted box
  (`rcAllocPayload` → `lyra_rc_alloc`, ALLOCATION.md), `memcpy`s both operands'
  bytes into the payload, and returns a fat pointer `{ box+header, la+lb }`. The
  operands are ordinary fat pointers wherever their bytes live (literal global,
  another heap box, a parameter), so a chain `a ++ b ++ c` composes left-to-right,
  each step a fresh box; `memcpy` of length 0 is a valid no-op, so an empty
  operand needs no special case. **Ownership:** a heap string is freed by the
  ownership model (`pkg/analyzer/ownership` + the backend's retain/release
  lowering, ALLOCATION.md). Every string value is a box — a literal's is pinned so
  retain/release no-op on it, a `++`'s is heap — so refcounting is uniform: a
  binding is released at its scope exit, a copy retains, a `return`/`own`-arg
  transfers, a temporary is released after its statement. Verified memory-safe
  under AddressSanitizer.

## The `len` field is bytes; the *language's* `len()` is runes

Worth stating together, because they disagree on purpose and the disagreement is the
design rather than an oversight.

The fat pointer carries a **byte** count, for the reasons above — O(1), NUL-clean,
memcpy-able, the Go/Rust/Swift representation. The language's `s.len()` (08/06,
`string_methods.go`) returns the **rune** count and is therefore O(n).

The tie-breaker is not aesthetics. `s[i]` yields the i-th *code point*
(`lowerStringIndex`) and `for c in s` walks code points, both of which shipped first,
so a byte-based `len()` would make `for i in 0..<s.len() { s[i] }` — the most obvious
loop over a string — trap or skip on the first non-ASCII input, silently and only for
some inputs. Given an index that counts runes, the length has to count runes.

**`slice(start, end)` copies rather than borrowing**, which is the other place the
representation would suggest something the ownership model forbids. A substring is a
contiguous byte range, so `{data + off, n}` is a valid fat pointer and costs nothing —
that is how Go slices. Here every string is a ref-counted box whose header sits at the
box's **start**, so a pointer into the middle cannot find the header to retain or
release through: a borrowed slice would either leak the parent or free bytes it does
not own. Copying into a fresh box (exactly as `++` does) keeps the uniform rule that a
string value is a box, at the price of an allocation — which `noalloc` correctly
refuses.

## Deferred

- **Interpolation** — no longer blocked on the allocator (it exists); what it
  still needs is **value→string formatting** for its non-string segments
  (`"n=${n}"` with `n` an int → text), a separate feature from concatenation.
  Errors loudly today.
- **`print` / `println` of a string** — needs the output shim (`write(1, data,
  len)`); observable via stdout rather than the exit code.
- **Escaped string patterns** (`"a\tb" =>`) — the pattern text is raw-quoted and
  would need Lyra's own unescaping (distinct from Go's) to match the collector's
  literal bytes; deferred with a loud error rather than risking a silent mismatch.
- **Regex patterns** in a string `match` (`r/…/`) — the engine exists
  (`pkg/regex`) but isn't wired into codegen.
- **Ownership / freeing** is done for the common cases (above). Still leaking
  conservatively (safe, never a double free): a string stored in an aggregate
  field, and bindings on a break/continue path (ALLOCATION.md).
