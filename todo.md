# Lyra — To-Do

What is **open**. Finished work and the reasoning behind it live in
[COMPLETED.md](COMPLETED.md); when an item below says something landed, that is where the
detail is. For what the compiler does *today*, read `CLAUDE.md` — this file is not a
status report.

Tags: **[OPEN]** not started · **[PARTIAL]** landed in part · **[DECIDED]** settled, not
built · **[IDEA]** not committed to · **[ROADMAP]**/**[DEFERRED]** deliberately later.

## I/O and the number-guessing program

**The program works.** Console input (`read_line() -> Maybe<string>`), string→int parsing
(`parse_i64`) and randomness (`random_seed()` plus the prelude's `Rng`) all landed 08/05, and
each follows the same rule: the builtin is only what cannot be written in Lyra, and everything
else is in `std/prelude.lyra`. See COMPLETED.md.

What writing it turned up, roughly in order of how much it costs a program someone wants to
write today:

- **[DONE 08/06] A member call on a type name is rejected in the typechecker**, rather than
  type-checking clean and crashing the backend with `llvm: unsupported method call`. The
  silence was a rung below the member call and so wider than this entry knew: a PascalCase
  name owning no constructor inferred as a *nil with no diagnostic*, which took a plain
  access (`Rng.field`) and a bare mention (`let x = Rng`) with it, and let
  `Nonexistent.make(1)` check clean too. `lyra-E035`, reported at the receiver in three
  cases — a type, a trait (which gets the `Trait::method(…)` spelling it was reaching for),
  and a name that is neither. See COMPLETED.md.
  - **[OPEN] Type-namespaced associated functions** remain the *feature* the spelling
    suggests, and `Trait::method` already half-exists for it. The diagnostic now says the
    language does not have them, which is honest and is what rule 5 wanted; building them
    is a separate decision, not a bug fix.
- **[DONE 08/06] `wall_clock_nanos()` names something.** `wallClock` was the last
  `builtinEffects` entry with no signature and no lowering — the `Random.global()` shape,
  still in place. Implemented rather than deleted, because deleting would have left
  `EffectTime` a bit nothing in the language could set. It is `clock_gettime` and nothing
  else, with everything derived from the answer left to the prelude; `pure`/`det` refuse it
  while a *threaded* timestamp stays ordinary data. Renamed to snake_case, with the unit in
  the name. See COMPLETED.md.
- **[DONE 08/06] A `match` arm may end in an assignment** — a `match` used as a
  *statement*, for its effect. The four arm-body sites lowered through `lowerExpr`, which
  requires a block value; they use `lowerBranchValue` now, the same value-optional helper
  `if` branches have always used. See COMPLETED.md.
- **[DONE 08/06] A bare jump may be a `match` arm body** — `None => break`,
  `_ => continue`, `v if … => return v`. The *braced* form (`None => { break }`) already
  worked, so only the spelling was missing; the collector erases the bare one into exactly
  that block, and nothing downstream changed. With this and statement arms, the
  read-until-EOF loop is finally the `match` it wanted to be. See COMPLETED.md.
- **[DONE 08/06] `for flag {}` parses.** The condition is `_bool_operand` now, so a name, a
  call (`for ready(n)`) and a member access (`for cfg.enabled`) all work, and `for done ==
  true {}` is no longer the only spelling. `$.expression` — matching `if` — does not
  generate, because a `block` is an expression and `for { … }` then cannot tell a condition
  from a body. It **shrank** the parser by one state, since `_bool_operand`'s states already
  existed for `&&`/`||`. Bool-ness is entirely the typechecker's now, which is the better
  diagnostic: `for n {}` over an integer was a syntax error and is `lyra-E001` naming the
  type. See COMPLETED.md and `tree-sitter-lyra`'s CLAUDE.md.
- **[DONE 08/06] `<=>` lowers**, yielding the prelude's `Ordering`
  (`Less | Equal | Greater`) rather than a bool or Ruby's -1/0/1 — so all three
  outcomes are handled in one exhaustiveness-checked `match`. Integers and runes only:
  **floats are refused**, because NaN is neither less than, equal to nor greater than
  anything and a three-way answer has to pick one. See COMPLETED.md.
  - **[OPEN] A partial ordering for floats.** C++ splits `strong_ordering` from
    `partial_ordering` (which has an `unordered` case) for exactly this. Adding a
    fourth `Ordering` variant would make every *integer* three-way match carry a case
    that cannot occur, so the choice is a second type versus a widened one, and it is
    deferred rather than guessed.
- **[OPEN] A `const` cannot be a range-pattern bound.** `const LOW = 1` … `LOW..<=HIGH`
  is a syntax error while `1..<=100` works, so a program must choose between naming its
  bounds and matching on them — and the numbers then get duplicated into the range check
  and the message describing it, which is where they drift.

  **Measured, and it is the lexical ambiguity rather than a missing alternative.**
  Admitting `const_identifier` in `range_pattern` is one line and it generates (after two
  GLR conflict entries, `_primary_expr` and `_constructor_value`), but every all-uppercase
  *data constructor* pattern — `A`, `MAX` — then misparses as a range bound with a
  `MISSING ".."`, because `const_identifier` and `user_defined_type_name` match that text
  identically and the lexer picks the constant once one is legal in the state. A further
  conflict entry is reported "unnecessary": the decision is made in the lexer, not the
  parser. This is the same ambiguity as the all-caps struct literal above and wants that
  grammar project, not this reflex. Backed out; the finding is recorded in
  `tree-sitter-lyra`'s `include/patterns/index.js`.
- **[DONE 08/06] A body ending in a loop infers `void`.** `let f = () => { for { … } }`
  reported "cannot infer the return type", because a loop is an expression in the AST with
  no recorded type — so a read-until-EOF `main` could not be written without an
  annotation. `blockValueExpr` answers void for a `for`/`for-in` tail, which is what a loop
  produces (`break` with a value is not implemented). The message also stopped blaming
  recursion outright: a tail `if`/`match` used for effect still trips it and is not
  recursive.
- **[DONE 08/06] String length, slicing and trimming.** `s.len()` (rune count, O(n)) and
  `s.slice(start, end)` (half-open rune range, allocating) are builtins;
  `trim`/`trim_start`/`trim_end` are ordinary Lyra in the prelude. Bytes-vs-runes was not
  an open question once looked at: `s[i]` and `for c in s` already walked code points, so a
  byte length would have made `for i in 0..<s.len() { s[i] }` silently wrong on non-ASCII.
  It exposed a live `noalloc` hole — a builtin method is charged no effect by all three of
  the purity pass's dispatch ladders, and `slice` is the first that allocates. See
  COMPLETED.md.
  - **[DONE 08/06] A literal is a postfix head.** `"abc".len()`, `[1, 2, 3].len()` and
    `1.wrapping_add(2)` parse. It was a *partition* rather than an addition — a literal kind
    reachable both directly from `expression` and through `_primary_expr` is derivable twice,
    which is an unresolved reduce-reduce at every operand position — and it **shrank** the
    parser by 4 states. See COMPLETED.md and `tree-sitter-lyra`'s CLAUDE.md.
  - **[DONE 08/08] `starts_with`/`ends_with`.** One line each in the prelude over two new
    builtins — `s.byte_len()` (O(1)) and `s.compare_bytes_at(offset, other)` (memcmp at a
    byte offset, comparing exactly `other`'s length, so `== 0` is a prefix test). Both are
    `pure noalloc`.

    **Written rune-indexed first, which is the obvious way and is quadratic.** `s[i]` is
    O(i), so a prefix test was O(m²) and a suffix test O(n·m) — and both paid an O(n)
    `len()` before comparing anything, so `s.starts_with("--")` on a 2000-rune string
    decoded all 2000 runes to answer a question about two bytes; measured, the length calls
    alone were 99.7% of it. `slice` + `compare_bytes` fixes the quadratic term and is the
    wrong trade: it allocates, so `noalloc` refuses it, and `slice` still walks runes to
    find the offset, so it stays O(n). The byte builtin is O(m) with no decoding —
    19.9 ms → 19 µs on that case. Byte-level answers the rune question exactly, because
    UTF-8 is prefix-free and self-synchronizing. See COMPLETED.md.
  - **[DONE 08/09] A rune needle, through `pub trait Needle`.** `index`/`contains` are
    generic over the needle rather than duplicated per needle type: `found_at` is
    implemented for `rune` (compare what `for c in s` decodes) and for `string` (memcmp at
    a byte offset), so neither path is pessimized. It replaced `index_rune`/`contains_rune`,
    which existed because both would take a `string` receiver and receiver-keyed
    overloading needs the heads to *differ*.

    Two things were measured wrong on the way and are worth remembering. The objection
    that a trait method could not carry the byte cursor is false — pass the cursor *and*
    the decoded rune, and each impl ignores what it does not need. And the dispatch cost
    that then looked decisive (4x) was 6% at `-O2`; the gap was `lyrac` linking at `-O0`,
    which is what turned up the optimization default.

    The trait is `pub`, so the payoff is real: user code implements `Needle` for its own
    type and gets both functions. The default `offset` sits on the generic wrappers because
    it is the only place the grammar admits one — a trait signature's `parameter_type` has
    no default slot, and an impl method's parameters are *patterns*, which carry neither a
    type nor a default.
  - **[DONE 08/08] `index`/`contains`.** `index(needle, offset = 0) -> Maybe<i64>` is a
    naive scan calling `compare_bytes_at` at each byte position, with `contains` one line
    on top. Both `offset` and the result are **rune** indices, so the answer feeds straight
    into `slice`; the scan is byte-level, reconciled by carrying a byte cursor alongside
    the rune counter (`utf8_len` on a rune) rather than converting afterwards, which
    nothing in the language could do. Scanning at byte positions needs no boundary check —
    a lead byte can never equal a continuation byte, so a match only lands on a rune
    boundary.

    Naive rather than Rabin–Karp deliberately: RK replaces a libc memcmp with
    byte-at-a-time arithmetic in Lyra (the thing measured three orders of magnitude slower
    when `starts_with` was written that way), and buys only an *expected* bound, its worst
    case being O(n·m) too. A real guarantee wants a `memmem` builtin (glibc's is Two-Way,
    O(n+m), constant space), not an algorithm written in the prelude.

    A negative `offset` is `None`, and **not** a from-the-end position even though `s[-1]`
    now is: an offset here is a resumption point for a scan, and "resume at k going
    forward" has no negative reading. Python's `str.find` counts its `start` from the end
    and is a known confusion for it.
  - **[DONE 08/08] A negative string index and negative `slice` bounds.** `s[-1]` is the
    last rune and `s.slice(1, -1)` drops it, matching arrays. Not sugar: the k-th rune from
    the end is a byte walk over continuation bytes that decodes nothing, where
    `s[s.len() - 1]` is two full O(n) decode walks — 34272 µs against 18 µs. The comment
    saying it "would require a full rune count first" is what had kept it closed. One
    shared `lyra_str_rune_offset` now answers "where does rune k begin" for both callers,
    which had carried the same forward walk twice. See COMPLETED.md.
  - **[DONE 08/08] `s.byte_offset(i) -> Maybe<i64>`**, the rune→byte conversion nothing
    else in the language could perform. It is what makes "does `sep` occur at rune i"
    allocation-free — `s.compare_bytes_at(s.byte_offset(i).unwrap_or(-1), sep) == 0`,
    composing without a `match` because `compare_bytes_at` is total. It maps *positions*,
    so the end position is `Some(byte_len)` rather than `None` (slice's rule, not `s[i]`'s,
    since a bound may name the end). Exposes the walk `s[i]` and `slice` already shared.
  - **[DONE 08/09] `split`**, generic over the needle through `Needle`. **The grow op is
    what settled its shape**: the obvious loop (find, cut, advance, push) rather than a
    comprehension over positions `0..<=n` guarded on "a part starts here", which was the
    workaround forced by a comprehension sizing its output from its *input* — k+1 parts
    from n runes meant allocating O(n) to hold O(k), permanently, since the box is never
    shrunk.

    The step past a match comes from **that match's span**, which is what `found_at`
    returning `Maybe<(i64, i64)>` is for and what a variable-length needle will need.
    Four things went wrong on the way, none of which a happy-path test catches: the
    trailing part after the last separator must be emitted (the `None` arm does work
    rather than merely ending the loop), empty parts are meaningful (`",a,"` is three
    parts), a haystack with no separator is one part rather than zero, and a step fixed
    from the first match loses `"a::b::c"` on `"::"`.
  - **[OPEN] `split` on an empty separator loops forever.** A zero-span match never
    advances the cursor, so `"abc".split("")` runs until killed (verified). Python raises
    `ValueError`; splitting into individual runes is the other common answer. The choice
    belongs to `split` rather than to `Needle`, since a zero-length match is meaningful
    to `index`/`contains` — `"".index("")` is `Some(0)`, which is what makes a search for
    an empty needle terminate.

## Known bugs

- **[DONE 08/08] A `return` nested inside an `if`, a loop or a match arm is type-checked.**
  It was not, at all: `checkBlockReturn` walks the body block's own statements, and a nested
  `return` reaches `checkNode`, which had no case for it. So its value was never compared
  against the declared return type — a nested `return "nope"` from a `-> Maybe<i64>` function
  was accepted silently — and, more consequentially, never *given* that type as context, so a
  data constructor in an early return reached the backend uninstantiated and died with
  `no type recorded for data constructor "None"`. Rule 5 inverted.

  **Scalars hid it**, which is why guard clauses looked like they worked: `return 7` lowers
  from the literal's own intrinsic type and needs no context. Only a value whose type is
  *decided* by its context notices that the context never arrived — so the idiom this blocked
  was exactly the common one, an early `return None`, and it is why `parse_i64` is written as
  one long tail if/else. Found writing the prelude's `index`. The fix routes through the
  existing `checkReturnValue` with the `enclosingRet` context that was already there and
  maintained, so a nested return gets the same assignability check, literal-width propagation
  and allocation stamping the top-level ones always had.

  - **[OPEN] A return-position literal is not range-checked.** `() -> u8 => 300` is accepted,
    and so is `return 300` in the same function. Noticed while testing the above and
    unrelated to it — it is true of *every* return position, including the ones that were
    always checked, so it is a gap in `checkReturnValue` rather than in the routing.


- **[DONE 08/07] Two `slice()` results in one expression no longer clobber each other.**
  `println("${s.slice(0,2)} ${s.slice(2,4)}")` printed `cd cd`: the first result was
  released before the second allocated, so the second allocation landed on the freed bytes.
  `flushStmtTemps` chose a temp's release block by asking whether it was produced in the
  statement's *start* block — a proxy for "was it produced unconditionally" that held only
  while every other block was a conditional branch. `slice`, `read_line` and `<=>` branch
  *unconditionally*, so their continuation blocks broke it. It asks dominance now, which is
  the question it always meant. See COMPLETED.md.

- **[DONE 08/07] A constructor call is a math operand, and a parenthesized expression is
  a postfix head.** `Cents(150) + Cents(275)` and `(a + b).x` both parse. Neither was a
  dispatch problem: `tuple_literal` lives in `_literal`, which `_math_operand` never
  reached, and a parenthesized *binary* expression is a `group`, which was reachable only
  from `_math_expr` and so was not a `_primary_expr`. Both fixes **removed** a path
  rather than adding one, and the parser got smaller (7730 → 7711 states). See
  COMPLETED.md.

- **[DONE 08/08] An array of anonymous tuples parses.** The grammar's `_non_allocated_type`
  — the element-type rule — was `type` minus the modifier forms and minus `void`, and the
  **anonymous tuple, the raw pointer and the anonymous struct had never been added**. All
  three are in now, which made the parser 3 states *smaller*. See COMPLETED.md.

- **[DONE 08/08] An anonymous struct is assignable to itself.** `isAssignable` had no
  anonymous-struct arm, so `{ x: 1 }` (whose field is `untyped_int`) fell through to
  `TypesEqual`, which compares field types *exactly*, and reported "cannot assign struct
  to struct". The anonymous *tuple* arm directly above it is the same rule and had been
  there all along — hazard 8, in a list of aggregate forms with one missing. Fields match
  by name, an untyped field narrows to the annotation, and `String()` now renders the
  fields so a genuine mismatch is readable. See COMPLETED.md.

- **[DONE 08/08] The anonymous struct lowers.** Construction (fields placed **by name**,
  in the type's order, since a literal may write them in any order), field access,
  `lowerType`, `resolveForLayout`, and all four ownership walks — `OwnsManaged`,
  `emitRetainValue`, `emitDropValue` and the ownership pass's *transfer* arm. That last
  one was the one that mattered: without it a temporary transferred into the struct was
  released at the end of its own statement while the struct kept the pointer, which is a
  use-after-free that **neither ASan nor LeakSanitizer reported**. See COMPLETED.md.

- **[DONE 08/08] `[0; 5]` — the array-repeat literal — is implemented.** `[v; n]` is
  `[n]T` in a fixed-size context and a heap `[]T` under a `[]T` annotation, with the value
  **evaluated once** and n-1 extra retains for a managed element. The count is a
  compile-time constant by construction (the grammar admits a literal or a
  `const_identifier`), and the typechecker rewrites a `const` count to the literal it
  folded to, so no later pass needs a const lookup of its own. See COMPLETED.md.

- **[DONE 08/07] A generic instantiated at a type declared in a named module lowers.**
  It failed with `llvm: unknown named type` for the identity function over a struct, in any
  program with a `module` header. The specialization path was the one function-lowering
  path that never called `enterModuleOf`, so `currentLoc` held whatever the previous item
  left behind and the type argument was looked up under its **bare** name — while a private
  module-scoped declaration is keyed `<module>::<name>` (rule 4). A `pub` type has a bare
  key and worked, which is what made it look like a bug about generics rather than about
  visibility, and why the existing tests missed it: they declare types `pub`, or in no
  module at all. See COMPLETED.md.

- **[OPEN] A struct literal with every field defaulted cannot be written.** `Person {}` is a
  syntax error — a literal body requires at least one field — so defaults stop being usable
  at exactly the point they are most useful, and the workaround is to name a field you
  wanted the default for. Found 08/06 by a test that claimed the opposite and passed:
  `TestTypeCheck_StructLiteralWithAllDefaults_Ok`. It is kept, inverted, so the day the
  grammar admits an empty body the test fails and says so.

- **[DONE 08/07] `let _ = expr` discards.** `wildcard_pattern` joined
  `destructuring_only_pattern`, so a bare `_` in binding position evaluates the value and
  binds nothing — the opt-out the must-use warning has always recommended in its own
  message (*"bind it (`let _ = ...`) to discard it intentionally"*) and the parser rejected.
  Zero parser states. `_` is still not an expression, so a discard cannot be read back.

- **[DONE 08/07] A trait-impl method may return its own receiver.**
  `same = (self) => self` against `(Self) -> Self` failed with **`expected Vec2, got
  Vec2`** — the return type was resolved and the parameter types were not, so
  assignability compared an `UnresolvedType` against a `NamedStructType`. Naming the same
  type twice is the signature of exactly that asymmetry, and it is hazard 8's shape
  without a switch: two sides of one question, only one of them resolved. Found 08/07 by
  an operator-overload test, since `(_+_) = (self, o) => self` is an ordinary impl.

- **[DONE 08/07] `a += b` with no impl is reported rather than crashing the backend.**
  `checkAssignToBinding` asks whether the right side is *assignable* to the left, which
  two structs of one type satisfy — so the compound form type-checked clean and failed to
  lower with "type not found for *ast.StructInstanceExpr", while `a = a + b` always
  reported it. Rule 5 inverted, pre-existing, and found while giving `+=` its dispatch.

- **[DONE 08/07] A `newtype` base must be structural, and is transparent to its methods.**
  `lyra-E041` refuses a base that already has nominal identity — a `struct`, a `data` type,
  a named tuple, and an *anonymous* tuple (which `tuple Name(...)` names, so the two would
  be two spellings of one thing). Scalars, `string`, arrays, raw pointers and function
  types keep working: nothing else names them. And a newtype now reaches its base's methods
  — `newtype Name = string` supports `len`/`slice`/`trim` — tried after every other rung,
  so a method written *for* the newtype still wins. See COMPLETED.md.

- **[DONE 08/07] A generic `newtype` works.** `newtype Boxed<t> = t`, and `Boxed<i64>`
  behaves as a newtype over `i64` — nominal to the typechecker, transparent to codegen.
  Three parts: the grammar gained the `generic_parameters` slot every other type
  declaration had, the collector attaches them, and `resolveType` **expands** a
  parameterized newtype into its substituted `ConstrainedType`. That last one is the
  asymmetry worth remembering: a parameterized struct stays a `ParameterizedType` for the
  instantiation machinery, but a newtype *is* its base plus a name, so it must become a
  `ConstrainedType` or `StripNewtype` finds nothing and every assignment to it is rejected.

- **[DONE 08/07] The non-parsing test sources are fixed and the class is closed.**
  Both `parseCollectAndCheck` and the collector's golden helper now check
  `tree.RootNode().HasError()`, so a source that does not parse is a test failure rather
  than a truncated AST every later assertion is vacuous against. Ten sources were mechanical
  — two statements sharing a line with no separator, invalid since 07/31 — one deliberately
  tests a parse error and opts out explicitly
  (`parseCollectAndCheckAllowingSyntaxErrors`), and **three were real bugs the vacuity was
  hiding**: the all-defaults struct literal, `let _ =`, and the generic `newtype`. Each is
  kept as an inverted test that fails when the gap closes.

  **Three were fixed 08/06** — the interior-mutation tests wrote `{ b.x = 99  a.x }`, two
  statements on one line with no separator, invalid since statements gained a terminator on
  07/31. They surfaced only because the literal-as-postfix-head change altered how the broken
  parse *recovers*, turning a hidden truncation into a visible extra type error. That is the
  argument for the guard rather than for fixing them one at a time: what these sources do
  today depends on error recovery, so any grammar change can move them, in either direction.

Two closed 08/05, and the second was the reason the first had been stuck. **An
all-uppercase type name could not be used in a struct literal** — `struct S` declared fine
and `S { v: 1 }` was a syntax error in every position, for any name with no lowercase letter
and no underscore (`S`, `AB`, `HTTP2`, `A1B`), because `user_defined_type_name` and
`const_identifier` match that text identically and the lexer picks the constant in expression
position. And **`if Point { 1 } else { 0 }` was a syntax error**, because precedence resolved
`Name • {` toward the struct literal before the parser could see that `{ 1 }` is a block
rather than a struct body. The first could not be fixed without the second: letting all-caps
names start struct literals extends that failure to `if MAX { 1 }`, which is ordinary code.
Both are fixed in tree-sitter-lyra; see COMPLETED.md and that repo's CLAUDE.md for the two
dead ends measured along the way.

`break`/`continue` no longer leak an enclosing statement's pending temporaries
(closed 08/03, measured with LeakSanitizer both ways — 18 bytes before, none after). The
jump records its obligation and `resolveExitReleases` settles it once the function's CFG is
final, releasing only the temporaries whose producing block dominates the jump
(`dominators.go`). See COMPLETED.md.

Two more closed 08/03, both hazard-8 misses found while making a `weak` field
constructible and both wider than the feature that surfaced them: `resolveForLayout` could
not size a `shared` struct holding *any* generic field (`Maybe<i64>`), and
`resolveTypeIfKnown` rejected a return annotation against itself. See COMPLETED.md.

The typechecker's infinite recursion on a definition cycle closed 07/31 — an
in-progress guard in `inferExprType`, which is also what stopped `lyra-lsp` dying
mid-keystroke (see COMPLETED.md).

The lambda-context bug closed 07/31 (a lambda literal now takes its missing
parameter and return annotations from the context it appears in — see COMPLETED.md).

Before that, three closed 07/30 (borrowed-`string` use-after-free, anonymous-tuple literal
width, `i128` multiply link failure on Linux) — see COMPLETED.md.

## In progress

### Backend — LLVM IR

Everything through closures, generics, strings, arrays, `match`, and interior assignment
lowers; `CLAUDE.md`'s `pkg/backend/llvm` section is the current inventory. Settled specs:
`ALLOCATION.md` (`stack` = inline, `shared` = ptr to a ref-counted box), `DATA_LAYOUT.md`
(tagged union), `STRING_LAYOUT.md`, `SIMD.md`. What is left:

- **[PARTIAL] Perceus reference counting** (PLDI 2021; the Koka/Lean technique). Stages 1–3
  are in — last-use precision, dup/drop fusion, and reuse/FBIP on `shared` values (a
  recursive `map` rebuilds every cell with zero allocation).
  - **Stage 4, reuse specialization** — token-conditional dup, a static-uniqueness fast
    path, skipping shared-field stores. Deferred on purpose: it buys refcount traffic, not
    allocations, and carries real double-free risk. Best done after a dynamic-array growth
    operation exists.
  - **Also open:** hoisting a *conditional* last use (one inside a branch still falls back
    to the scope frame); reuse through the ladder path (guards / value-testing payloads);
    struct and tuple reuse.
  - Committed consequences: `weak` must be real before shared object graphs (rc has no
    cycle collector); drop timing is last-use, so no user-observable finalizers unless
    separately decided; a checked FBIP annotation (Koka's `fip`) follows if the uniqueness
    cliff bites.
- **[PARTIAL] Closure lowering is tiered** — dev = uniform boxed closures (landed), release
  = **Lambda Set Specialization** (PLDI 2023). LSS is still to build, gated on the generics
  monomorphizer as decided: one specializer, two parameter axes. The tiers are semantically
  identical, so `noalloc` is defined against the *release* lowering. Hot-reload note:
  body-only edits keep lambda sets stable; adding/removing a lambda or changing captures
  means a full rebuild even in dev.
- **[DONE 08/07] `where` bounds are enforced at the instantiation** (`lyra-E036`), and a
  binding's bounds are in scope for its own body. Both halves were missing and the first
  is what made the second worth having: `tc.genericBounds` was populated only from an
  *impl's* `where` clause, so `let describe<t> where t: Show = (v: t) -> string => v.show()`
  reported *"type parameter t has no method `show`; add a `where t: Trait` bound"* — naming
  the fix the author had already applied. The bounds are lifted onto the `LambdaExpr` by the
  collector, exactly as the leading modifiers are, because they are written on the binding
  while every consumer holds only the lambda. A type argument that is itself a *type
  variable* is checked against the enclosing declaration's bounds rather than against any
  impl, which is what lets a bound be forwarded. See COMPLETED.md.
  - **[DONE 08/07] A bound-dispatched call lowers.** `v.show()` under `where t: Show`
    resolves *abstractly* at check time — the receiver is a type variable, so there is no
    single impl to name — and becomes concrete only when a specialization fixes it. The
    typechecker publishes one resolution per implementing type
    (`MethodTable.SetBoundCandidates`) and the backend picks by the receiver's
    *substituted* type, so impl matching stays in the one place that does dispatch. One
    generic body, two instantiations, two impls called. See COMPLETED.md.
  (A trait-impl method on a generic receiver **does** lower as of 08/03 — see COMPLETED.md
  and `pkg/backend/llvm/README.md`'s trait section. `Maybe<weak T>` parses and lowers as of
  08/03 too; the "does not parse" note that stood here was never true.)
- **[DONE 08/04] A generic body may call another generic at a variable-dependent
  instantiation.** `let get_or<t> = (o: Maybe<t>, d: t) -> t => o.unwrap_or(d)` compiles,
  and so does the free-function analogue. The fix is the one this entry called for:
  compose the caller's bindings into the callee's, done in the **driver**
  (`instantiations.go`) rather than the backend, because the per-specialization ownership
  pass runs off the instantiation set and a specialization discovered after it would fall
  back to the program-wide table — analyzed generically, where a type variable is not
  managed, so a `t = string` body would emit neither retains nor releases.

  The recursive-generic question this entry raised is answered by **bounding type depth**,
  not the count: polymorphic recursion (`f<t>` calling `f<Box<t>>`) is infinite, and what
  grows is the type. A count-only bound does terminate, but only after the set is both
  enormous and individually huge — measured at over a minute and a gigabyte, which is
  indistinguishable from the hang it was meant to prevent. Depth catches it in a few dozen
  cheap steps and reports it as what it is. Recursion at the *same* type is untouched.
  See COMPLETED.md.
- **[DONE 08/01] A binding's written generic parameter list is authoritative** — option (b)
  of the three that were on the table. A signature variable absent from a written list is
  `lyra-E031`; a declared parameter the signature never mentions is `lyra-W013`. The list
  stays **optional** (the lexical rule is unchanged, so `let unbox = (b: Box<t>, fb: t) -> t`
  is still generic with no list); what changed is that a written one must agree with its
  signature. `checker/generic_params.go`, reasoning in that package's README and in
  COMPLETED.md. The three type-variable walkers are now one (`types.CollectTypeVars`).

  - **[OPEN] The same reconciliation for *type* declarations, traits and impls.** A
    `struct`/`data`/named-`tuple` list, a `trait` list, and an `impl` list are each still
    unreconciled with the bodies they parameterize. Lower severity than the binding case was:
    a type declaration's list arity *is* load-bearing (checked against the type arguments at
    instantiation, `backend/generic_types.go`), so a mismatch there tends to surface as an
    arity error rather than as silence. Same pass, same walker; it needs the nominal-type
    question answered per declaration kind (a struct's own fields **are** its signature,
    unlike a signature that merely mentions the struct).

### Modules

Resolution, per-module scoping, `pub`, the implicit prelude, per-module name resolution and
symbol mangling all landed 07/30.

Types and traits got per-module identity 08/01 — `SymbolTable.Types`/`.Traits` are keyed
by the same `declKey` bindings already used, the typechecker's `resolvedTypes` cache and
the backend's `structTypes` registry with them. Two modules may each declare a private
`Point`, and a prelude type shadow no longer reaches another module. See COMPLETED.md.
- Out of scope by decision, none of it changing what a module's source looks like: package
  management, versioning, separate/incremental compilation.

**[DONE 08/08] An import can no longer make an ordinary name unusable.** `import util.seq`
(which exports a `map`) plus a perfectly ordinary `let map = (n: i64) -> i64 => n + 1` was a
hard error — *function "map" is already defined at …/util/seq.lyra* — while the identical
declaration over the **prelude's** `map` merely warned and won. The explicit act punished
and the implicit one forgiven; a user's only read of it was that the library owns `map` and
their program may not have one.

Option **(a)**, as scoped above: `shadowsPrelude` became `shadowsAmbient`, so a module's own
declaration of a name reaching it from *either* source — the prelude, or a module it imports
— is keyed `<module>::<name>` and warns (`lyra-W016`) instead of erroring. The local
declaration wins every bare reference in that module, the shadowed one stays reachable
through the namespace the import already binds (`seq.map`), and no other module is affected.
A genuine second claim on the program-wide name — two modules exporting one name — is still
an error, which is what (a) was chosen to preserve.

The import graph is handed to the collector **before the first file is walked**
(`modules.ImportGraph` / `Collector.SetImports`), beside the prelude path and for the same
reason: a type's key is computed during the walk, so a graph assembled file by file would
key the first file of a multi-file module as though its second file's `import` did not
exist. See COMPLETED.md, including the two by-name bugs it surfaced — a namespace member's
`pub` check reading the wrong module's binding, and the backend failing to lower the
namespace call at all.

**Still open, and the reason (b) has not gone away entirely.** A module that is *itself*
`pub`-exporting a name it also imports re-claims the program-wide name, and that is still
reported (`symbol "map" already defined`). Resolving it needs (b) — qualified `pub` keys
plus bare lookups consulting the importing module's bindings — because the answer depends on
who is asking, which a program-wide key cannot express. Nothing shipped needs it.

Worth having done before the standard library grew: every name added under `pub` used to be
a name taken away from anyone who imports that module. It was also the constraint that
decided where a combinator could live at all, back when the library was split (see the 08/02
discussion of `map`/`filter`); the whole standard library is one module as of 08/04, and
still one module as of 08/07 — it is seven *files* now (`std/prelude/`), which is the
multi-file-module feature and deliberately not a re-split into several modules, for exactly
the reason below.

**It was less pressing since UFCS landed 08/03, and was not fixed by it.** The combinators
are reached as `m.map(f)`, dispatched on the receiver, so the bare name `map` no longer has
to mean one type's version — which was the reason this decided where `map` could live. The
collision itself was untouched: importing a module still claimed every top-level name it
exported. What changed is that the workaround (don't import it) cost less, which is exactly
the kind of relief that lets a real bug sit for a year.

**Receiver-keyed overloading (08/03) removed the other half of the pressure, and again did
not fix this.** Two `self` functions of one name may now be *declared* in one module when
their receiver heads differ, so `Maybe` and `Result` no longer need separate modules merely
to have `unwrap_or` each — which was the standing argument for the split. `std.maybe` was
folded into the prelude on 08/04 and deleted; the whole standard library is now one module,
so the shipped code no longer trips this at all.

**And the way it got tripped on the way there is the sharpest statement of the bug.** Adding
`map` for `Result` to the prelude while `std.maybe` still declared `map` for `Maybe` did not
produce an error — it *silently removed a method*. The prelude keeps bare keys, so its `map`
took the program-wide one; that flipped `shadowsPrelude("std.maybe", "map")` to true, which
pushed `std.maybe`'s `map` to a qualified key; and the UFCS rung consults exactly one
candidate by name, so `m.map(f)` on a `Maybe` found the `Result` overload, failed to match
the receiver, and reported "member access on non-struct type Maybe<i64>". Two features that
each work correctly, composing into a disappearance. Reproduced on the commit *before*
overloading landed, so it is this bug and not that feature.

**[DONE 08/04] UFCS resolves against every declaration of the name the file can reach** —
its own module, the prelude, and each imported module — and picks by receiver, rather than
taking the single `declKey` winner (`ufcsFunction`). That is the cross-module form of the
dispatch overloading already does within a module; the import requirement is unchanged, so
an unimported module's method is still unreachable. A file's own module wins a tie, and a
tie that survives that is reported with a qualifier the reader can type
(`` `dup.map(m, …)` ``) rather than broken by map-iteration order. See COMPLETED.md.

**[DONE 08/04] The bare-call form resolves the same way.** `map(b, f)` now reaches what
`b.map(f)` reaches. A bare call still resolves its *name* through the scope chain first —
so a local declaration wins exactly as before — but when the name it lands on takes a `self`
receiver it does not accept, `receiverFallback` gathers every reachable declaration and
picks by receiver, which is what the method form always did. Additive by construction: only
calls that were errors change meaning. A plain (non-receiver) function whose first argument
does not fit is still an ordinary argument-type error, since dispatching there would turn a
typo into a call to something else. See COMPLETED.md.

**What (a)/(b) was for.** Names with **no receiver** cannot be disambiguated by dispatch at
all, which is why the key-level fix was the one that settled the rest. (a) landed 08/08: an
imported `pub` name no longer forbids the importer its own. Two modules exporting a plain
`helper` still collide on the bare key, which is the half deliberately kept — neither has a
local declaration obviously meant to win, so there is nothing for a shadowing rule to prefer.

The LSP resolves a document's whole import graph as of 08/02 (see COMPLETED.md), which leaves
two editor features single-file where the program no longer is:

- **Rename declines a cross-file declaration.** Renaming a prelude function from a use site
  would need every unit's occurrences and a multi-file `WorkspaceEdit`; today
  `resolveRenameAnchor` returns false rather than splicing the new name into this buffer at
  the *other* file's coordinates. Declining is right until the multi-file form exists — the
  alternative is a silent corruption — but the message the user gets is nothing at all.
- **References only searches the open document.** Uses of a name in sibling modules are
  missing from the result, which reads as "no other uses" rather than "not looked".

Both want the same thing: walking every unit's program, which the server now has, keyed by
each node's `Location.File`.

## Constructor syntax — juxtaposition

**[DECIDED 08/02; BUILT]** `Some 42` is back alongside `Some(42)`. **One operand, never
curried** — there is no `Rect 3 4`, because a constructor's positional payload is already a
single anonymous tuple internally (`Rect(f64, f64)` → one `TupleType` param), so `Rect(3, 4)`
re-reads as "Rect applied to the tuple `(3, 4)`": the parens belong to the tuple, not to a
call, and the tree is byte-for-byte what it was.

*Why now, when it was removed 06/18.* That commit is explicit that the machinery existed
"solely to prevent a nullary constructor from greedily consuming the next statement **in the
terminator-less grammar**" — `let c = None` ⏎ `match c {…}` parsing as `None(match …)`.
Statements gained a terminator on 07/31, six weeks later. The sole stated reason expired.
It also closes a real asymmetry: `Some 42` has always been legal in *pattern* position
(`data_pattern` is `Name pattern`), so the two positions disagreed about the language's own
constructor syntax.

### `Some -1` is `Some(-1)` — application, not subtraction

**[DECIDED 08/02]** Application binds tighter than binary operators, and `negation` is in the
operand set, so `Some -1` applies `Some` to `-1`. `Some 42 ?? d` is `(Some 42) ?? d` and
`Some a + b` is `(Some a) + b`, the ML reading.

*Why this is not the Haskell ambiguity.* In Haskell `Some -1` is genuinely ambiguous because
any identifier can be a value, so the subtraction reading has an operand. **Lyra's lexer has
already split the cases**: `identifier` is `/(_[a-zA-Z0-9_]+|[a-z][a-zA-Z0-9_]*)/`
(lowercase-leading) and `const_identifier` is `/[A-Z][A-Z0-9_]*/` (SCREAMING_CASE). A
PascalCase name in expression position is therefore *always* a constructor — never a
variable, never a constant — so the subtraction reading has nothing to bind and `MAX - 1`
(a constant) is untouched arithmetic. The previous incarnation of the rule reached the same
answer, keeping `negation` "so `Err -1` … still work[s]".

- **[OPEN] The residual hazard: an operator overload on a data type.** `-` is overloadable
  (`(_-_)` in a trait), so `Empty - 1` on a `data` type with a `Sub` impl now parses as
  `Empty(-1)` rather than as subtraction. It needs all of: a `-` overload on a *sum* type, a
  **nullary** constructor as the bare left operand, and an operand that is an atomic
  constructor operand. Contrived, and `(Empty) - 1` still says the other thing. Worth a lint
  if it ever bites — a `-` overload on a data type whose nullary constructor appears bare on
  the left — rather than a grammar rule, since the grammar cannot know about the impl.
- **[IDEA] Warn on `Some - 1` written with spaces on both sides.** Same parse as `Some -1`,
  but it *reads* as subtraction. Not built: it has exactly one valid meaning, so a warning
  is arguably noise. Revisit if it confuses anyone.

### The operand must be atomic

A juxtaposed operand is a literal, a name, a nullary constructor, a negated literal, a
struct/array literal, or another application. A **compound** operand — a call, member access,
index, `?`, deref, arithmetic — is parenthesized: `Ok(f(y))`, `Some(a.b)`. Those are the
spellings they already had, so nothing regressed, but `Ok f(y)` is a parse error rather than
sugar.

That is forced, not chosen. **Every postfix form is headed by `_postfix_expr`, which reaches
`parenthesized_expr`**, so admitting any of them as an operand also admits `Some (x)…` while
the parser looks for the `.`/`[`/`?`/`^` — which reopens a third reading of `Some(x)` and
tips the pre-existing parameter-position race, so `(Some(x): Maybe<i64>) -> i64` stops
parsing as a destructured lambda parameter. No conflict entry fixes it; the reading has to
not exist. Found by bisecting the operand set against the corpus — **the "unnecessary
conflict" warnings are unreliable in this region**, so verify against corpus, not warnings.

- **[IDEA] Admit compound operands after all**, if the parameter-position race can be settled
  another way (restructuring `pattern` the way the 07/16 fix did, rather than by conflicts).
  `Ok f(y)` reading as sugar would be nice; it is not worth a fragile parse.

*Cost:* 5,537 → 6,606 states, `parser.c` 9.4 MB → 12.0 MB (+19% / +28%). Juxtaposition is
genuinely expensive in an LR automaton — still an order of magnitude below the 62,663-state
`lambda_expr` incident, but this is now the second-largest single feature in the parser.
Re-measure with `--report-states-for-rule -` before adding anything else here.

*Implementation:* the collector erases the spelling — `collectAppliedConstructorExpr` builds
the **same named `TupleLiteralExpr`** the parenthesized form builds, so the typechecker,
purity, ownership, exhaustiveness and the backend never learn juxtaposition exists. Proof
that the erasure is exact: `Some None` and `Some(None)` fail with the identical
(pre-existing) nested-generic lowering error.

## Array comprehensions

**[DONE 08/04]** `[ x in xs | x % 2 == 0 | x * 2 ]` — generators, optional guards, result —
collects, type-checks and lowers. The grammar had them from the start; nothing else did, so
the expression reported `unknown expression type "array_comp_expr"`.

They matter beyond convenience: **a comprehension is the only way to build an array.** There
was no growth operation until 08/09 (`push`), and a spread in an array literal
(`[0, ...xs]`) parses but is still not collected, so before this the prelude could not have a `map` for `[]t` at all — the natural
recursive `[head, ...tail]` formulation needs both of the missing pieces. `map` for arrays
is now one line in `std/prelude.lyra`, and a third receiver head beside `Maybe` and
`Result`.

A comprehension is always `[]u`, never `[N]u`, even with no guard: a guard decides at run
time how many elements survive, and adding one to a comprehension should not change its
type. Capacity is the product of the source lengths and the box records the survivor
*count* as its length — the reasoning for over-allocating rather than counting twice or
growing is in `pkg/backend/llvm/array_comp.go`.

**[DONE 08/04] Range and string sources.** `[ x in 1..<=10 | x * x ]` and
`[ c in "héllo" | c ]` both lower, and mix with array sources in one comprehension.

A source now **drives its own loop** rather than answering "the value at index i". The
index model fitted arrays and ranges and not strings — UTF-8 is variable width, so the walk
is a byte cursor whose advance is whatever the decoder just consumed — and forcing all
three through one shape would have meant a special case in the nesting instead of in the
source.

The load-bearing rule is that **the capacity bounds the loop by construction**, since
writing past the box is memory corruption rather than a wrong answer. An array's is its
length; a string's is its **byte** length, which bounds the rune count because no encoded
rune is shorter than a byte; a range's is computed up front and the loop then runs exactly
that many times, so a degenerate range (`5..<1`, a non-positive step) yields an empty array
instead of racing past the allocation. Note that a backwards `for-in` range still loops
forever — a comprehension deliberately does not inherit that, because here it would be
unsafe rather than merely wrong.

Still open, each refused loudly rather than approximated:

- **[OPEN] A generator whose source depends on an earlier generator** —
  `[ row in grid, cell in row | cell ]`. Sources are materialized once before the loops,
  which is what makes the capacity computable; a dependent source would need
  materialization inside the enclosing loop and a capacity that is not known up front.
- **[DONE 08/04] `result_expr` is any expression.** It was a hand-maintained list of forms,
  so `[ x in xs | "a" ++ b ]` was a *syntax* error, as were an `if` and a `match` in result
  position. Widening it to `$.expression` made the parser **smaller** — 8,232 → 8,202 states
  and 35 KB off `parser.c` — and retired the `[result_expr, _primary_expr]` conflict entry,
  because the list had been competing with `_primary_expr` over what a bare name in result
  position reduces to and `$.expression` subsumes that. The `|` rule is untouched: a
  top-level `|` is still a section separator, and a bitwise-or meant as a value is still
  parenthesized. See COMPLETED.md.

**[PARTIAL] `noalloc` and implicit allocation.** Arrays landed 08/04: `allocContext.allocates`
now asks about **representation** rather than flavor, so a `[]T` literal and a comprehension
both count, while the same literal as a fixed `[N]T` is stack storage and does not. It stays
a question about value-*producing* forms — a `[]T` identifier is heap-represented and
allocates nothing — so the walk asks it of the construction cases only. See COMPLETED.md.

- **[DONE 08/04] Strings.** `"a" ++ b` and `"${x}"` are counted; a string *literal* is not,
  because it interns as a pinned static box. That is charged by **form** rather than by
  type (`allocatesByForm`) — a literal, a `++` and an interpolation are all `string`, so the
  type cannot separate them, which is the exact opposite of the array case where the type is
  what does. Keeping the two rules apart rather than folding them into one predicate is the
  point. See COMPLETED.md.

- **[DONE 08/04] `lyra-E016` names the offending expression.** The effect inference records
  each callable's **first** directly-allocating expression (`allocContext.lambdaSites` /
  `methodSites`), so the message reads *"an array comprehension builds a `[]T` at 2:46"*
  rather than listing every allocating form. An allocation arriving through a **callee**
  keeps the form-listing wording — the call is in this body and the allocation is not, so
  pointing at the call would name a line that does not allocate. See COMPLETED.md.
- **[OPEN] Escaping closures.** Boxed in the dev lowering, free under Lambda Set
  Specialization — so what `noalloc` should say depends on the tier, which is the reason
  `noalloc` is defined against the *release* lowering in the first place. Settle that before
  charging a closure.

## Lazy sequences — `gen` and `Seq<t>`

**[IDEA]** `xs.filter(p).map(f)` allocates an intermediate `[]t` per stage, because both
combinators in `std/prelude/array.lyra` are eager. The fix is a lazy sequence — and the
shape it should take is one the language already half-committed to.

**`gen`, `yield` and `yield from` already parse.** `gen_modifier` is a lambda modifier
(`tree-sitter-lyra/include/expressions/functions/lambda.js`), the collector sets
`LambdaExpr.IsGenerator` (`collector/expressions/lambda_expr.go`, `declarations/var_decl.go`),
and `lyra-E006` already polices a `yield` outside a generator body
(`checker/yield_outside_generator.go`). What is missing is one rung — the typechecker:

```
$ lyrac check t.lyra
t.lyra:3:5: error [lyra-E001]: unknown expression type "yield_expr"
```

That is verbatim the state `array_comp_expr` was in before 08/04, in the same words. This is
the same kind of phantom the sweep above is for, one rung short of the ones it found.

**The design.** A `Seq<t>` is what a `gen` call evaluates to, and every combinator is then
ordinary Lyra in the prelude — no trait, no adapter structs, no associated types:

```
pub let map<t,u> = pure gen (self: Seq<t>, f: (t) -> u) -> Seq<u> => {
  for x in self { yield f(x) }
}

pub let filter<t> = pure gen (self: Seq<t>, p: (t) -> bool) -> Seq<t> => {
  for x in self { if p(x) { yield x } }
}
```

**Collect is not a compiler feature, because the language already has one and it is
brackets.** `for y in s.filter(p).map(f) { … }` allocates nothing;
`[y in s.filter(p).map(f) | y]` materializes, and `noalloc` charges exactly the brackets.
08/04's "a comprehension is the only way to build an array" survives intact. This is the one
place the design is *better* than Rust's rather than a copy of it: `.collect::<Vec<_>>()`
exists because Rust has no distinguished materialization syntax, and Lyra does.

**So there are two *primitive* consumers — `for-in` and the comprehension — and everything
else is a prelude one-liner over them.** A chain ends in a terminal the ordinary way:

```
pub let to_array<t> = pure (self: Seq<t>) -> []t => [x in self | x]

pub let sum = pure (self: Seq<i64>) -> i64 => {
  var total = 0
  for x in self { total += x }
  total
}
```

`to_array` rather than `collect`, naming the result for the reason `seq()` names the value.
Only it inherits the grow-op prerequisite below; `sum`/`fold`/`any`/`count`/`first` need
nothing. The split is the project's usual one — the brackets are the primitive, the names
over them are prelude Lyra, exactly as `read_line` is to `parse_i64`.

### Both maps, and the head rule is what gives them

**`[]t` keeps its eager `map`/`filter` exactly as they are, and `xs.seq()` enters the lazy
world.** The programmer picks; neither is a mode the other has to be reasoned about through:

```
xs.map(f)                      // eager  -> []u, one allocation, f runs here
xs.seq().filter(p).map(f)      // lazy   -> Seq<u>, fused, f runs at the consumer
```

**This costs no new machinery, because `[]t` and `Seq<t>` are different receiver heads.**
Receiver-keyed overloading permits both `map`s in one module for exactly the reason it
permits `unwrap_or` for `Maybe` beside `unwrap_or` for `Result` — one axis, checked once at
the declaration. The alternative considered first, making `[]t`'s `map` *return* a `Seq`, is
the one thing the head rule forbids: two `map`s on one head, so lazy would have had to
replace eager rather than join it.

Three things follow, and each is a reason to prefer this over lazy-by-default:

- **`seq()` is the only new name.** The combinator vocabulary is written once, on the `Seq`
  head, and every future addition lands there. The eager side stays the two comprehensions
  it already is — which also keeps it clear of the grow-op prerequisite below, since a
  comprehension over `[]t` still knows its capacity.
- **Deferred side effects become opt-in and visible.** `f` running at the consumer rather
  than at the call is the one genuinely surprising consequence of laziness — an unconsumed
  `xs.map(log_it)` runs zero times, and `xs.map(g).filter(p)` interleaves `g` and `p`
  instead of running all of each. Combinator callbacks deliberately carry no effect bound
  (see Functional/imperative blend), so an impure `f` is legal and nothing would warn.
  With `seq()` in the source, the reader has been told.
- **It is Rust's `.iter()` without Rust's reason for three of them.** `iter`/`iter_mut`/
  `into_iter` exist to say which borrow you are taking; Lyra's refcounting answers that
  already, so one spelling suffices and the usual ceremony objection mostly does not apply.

**[DECIDED 08/09] The spelling is `seq()`.** It names the type it produces, which is the
convention the prelude's constructors already follow (`rng_seeded` → `Rng`). `lazy()` was
the alternative and reads better in a chain, but it names a *strategy* rather than a value,
so `xs.lazy()` says what the compiler will do while `xs.seq()` says what you now hold — and
the second is the thing the type annotation, the diagnostics and the receiver head will all
have to agree about anyway.

### Why a `gen` function rather than an `Iterator` trait

Three reasons, none of them taste:

- **The prelude rule.** "Anything expressible in Lyra belongs in the prelude"
  (`std/prelude/README.md`). A `gen` function makes `filter` a three-line prelude entry. An
  `Iterator` trait makes it expressible only *after* building trait dispatch over generic
  adapter structs and something standing in for associated types — and the prelude README
  already records that a **generic** trait impl is the thing to avoid writing there.
- **The effect system would poison it.** Effect polymorphism is precise for a callback
  arriving as a parameter and conservative (`AllEffects`) for one "reached through anything
  but a parameter or a binding — a struct field, a call result, an array element" (see the
  Functional/imperative blend section). An adapter struct stores its closure in a **field**,
  so `xs.iter().map(f)` would infer impure and `pure noalloc` code — the code that most wants
  fusion — could not call it. A `gen` function takes `f` as a parameter, which is the case
  07/31 already handles exactly.
- **It is the source-driver abstraction, generalized.** 08/04: "A source now drives its own
  loop rather than answering 'the value at index i'." There are three drivers (array, range,
  string) and they are hard-coded at `isIterableType` /
  `typechecker_control_flow.go`'s `checkForInLoopExpr`. `Seq` is a fourth — and the only one
  a user can write. `for-in` and the comprehension both gain it from one source driver.

### Lowering — push first, state machine only if forced

- **[IDEA] Stage 1: inline the generator into its consumer.** A `Seq` is only ever consumed
  by `for-in` or a comprehension, and both consume it in a single loop, so `for x in g(a) { B }`
  can lower as g's body with each `yield e` rewritten to `let x = e; B`. Internal iteration:
  no coroutines, no state machine, no suspension, no heap. **The chain fuses into one loop by
  construction rather than by an optimizer**, which is the whole point of the entry. Buys
  `map`, `filter`, `flat_map`, `take`, `take_while`, `enumerate`, `chain`, `scan`, and
  infinite sequences.
- **[OPEN] A terminal puts the loop behind a call boundary, and stage 1 has to get through
  it.** `s.sum()` has its `for-in` inside `sum`, so the generator is not statically visible
  to the loop consuming it — and terminals are the *normal* way to end a chain, so this is
  not an edge case. The question underneath is **whether the element type is the whole
  type** — whether `xs.seq()`, `xs.seq().filter(p)` and `xs.seq().filter(p).map(f).take(3)`
  are all `Seq<i64>`, or whether the construction is recorded in the type the way Rust's
  `Map<Filter<slice::Iter<'_, i64>, {closure}>, {closure}>` records it. That decides the
  fusion, because specialization is keyed on types: a chain-shaped type monomorphizes `sum`
  once per chain, and each specialization knows whose code sits on the other side of its
  `for-in`, so it can inline it.
  - **Keep the element type as the whole type, and inline the terminal at the call site.**
    These are three-line functions; inline first and the `for-in` is local again, so the
    stage-1 rewrite applies unchanged — fusion driven by what is *syntactically visible*
    where the inlining happens rather than by monomorphization. Where that does not happen,
    the loop degrades to a real pull through the boxed `{fn, env}` closure representation
    (`pkg/backend/llvm/closures.go`): an indirect call per element, but still O(1)
    allocations rather than O(n). **Recommended.**
  - **Put the chain shape in the type**, Rust-style. Fusion then crosses any call boundary
    for free, and the cost is paid everywhere else. Two are obvious — every diagnostic
    naming a sequence becomes unreadable, and two `if` branches yielding different chains
    have different types, which Rust answers with `Box<dyn Iterator>` or `impl Trait` and
    Lyra has neither of. **The third is what settles it: there is then no way to *write
    down* "a sequence of i64" as a parameter type**, so abstracting over the chain types
    needs a trait — which drags the `Iterator` trait back in, with the closure-in-a-field
    effect hole that was the second argument against it above. The two options are not
    symmetric.
- **[OPEN] What stage 1 must refuse, loudly.** `zip` (two producers interleaved needs pull),
  and a `Seq` reaching a consumer that cannot be inlined through — which under the first
  option above is the general form of the previous bullet.
- **[IDEA] Stage 2: state-machine transformation** (Rust async / C# iterators / Kotlin) for
  whatever stage 1's refusals turn out to cost. Worth noting the machinery lands *in the
  compiler*, so the language still never grows associated types and the prelude functions do
  not change — which is the argument for stage 1 not being a throwaway.

### Open decisions and prerequisites

- **[PARTIAL] Iterating a `Seq` needs nothing new; *collecting* one needs a grow op** —
  which landed 08/09 as `xs.push(v)`, so what remains here is the `Seq` side rather than
  the missing primitive. The
  comprehension allocates capacity up front — "the capacity bounds the loop by construction"
  — and a `Seq` has no length to compute it from. That is the dynamic-array **growth**
  already open under (#5). Same blocker `split` sits behind, which is the other half of the
  argument: as a `Seq<string>` it is not blocked at all.
- **[OPEN] The return annotation's spelling.** `-> Seq<u>` (recommended) says what the
  function returns and gives `yield` something to check against; `-> u` annotates the element
  and reads like a normal return while meaning something else. Pick before anything is
  written down.
- **[IDEA] Implicit `Seq` → `[]t`, considered and not taken.** One `map`, returning a `Seq`,
  materializing wherever context wants an array (`let ys: []i64 = xs.map(f)`) — with a real
  Lyra precedent, since `[1, 2, 3]` is already `[3]T` or `[]T` "told apart by what the
  literal is used as". Rejected because the precedent does not reach: that is one syntactic
  *form* reading its annotation, not a value coercing in every position it flows through. It
  would put an allocation somewhere `noalloc` charges nothing (allocation is charged by form
  and by representation — a coercion is neither), and make *when a side effect runs* a
  product of inference rather than of something written down. Worth revisiting only if
  `seq()` proves to be noise in practice.
- **A word collides.** "Generator" already means the `x in xs` clause of a comprehension in
  the section above. Whatever the docs settle on, these are not the same thing.

**What not to do:** recognize `map`/`filter` chains by `@builtin` marker and rewrite them into
a comprehension. It is the cheapest thing on the table and it has a syntactic cliff — hoisting
one stage into a `let` silently loses the fusion, with no diagnostic. That is GHC's
rewrite-rule unpredictability, and the opposite of refusing loudly.

**It also unblocks the `0..` idea below**, which names "a lazy/infinite iterator" as its
prerequisite. This is that.

## Ranges

The three range grammars were unified 08/01 (`rangeBounds`, one `range_end_operator`,
`lyra-E032` for a missing end operator at all three sites, open-ended patterns,
`lyra-E033` for an ill-formed step). See COMPLETED.md. What is left:

- **[OPEN] A `step()` constraint is not enforced against values.** Nothing reads
  `types.StepConstraint` after collection, so `newtype Quarter = f32 where range(0..<=100),
  step(0.25)` validates the *step* but still accepts 0.3. Unlike `range(…)`, which the
  value-range pass checks (`lyra-E023`), a step is a divisibility test — cheap for a
  compile-time constant, a runtime check otherwise, which is the decision to make first.
- **[DONE 08/04] Descending ranges.** `5..>1` and `5..>=1` count down, in `for-in` and in a
  comprehension. The inclusive end moved from `..=` to `..<=`, so the four operators are
  `..<` `..<=` `..>` `..>=` — two axes, direction and whether the end is included, each
  named by the operator.

  **Direction is the operator's, never the bounds'.** `5..<1` is an *ascending* range that
  happens to be empty, not a descending one. The alternative on the table was to keep a
  single inclusive `..=` meaning "whichever way the bounds point", which is one token fewer
  and was rejected: direction would then be a property of the operand *values*, so a range
  over variables could run the opposite way from the way it reads, with no diagnostic
  anywhere. Making it a parse-time fact is also what lets the step be a plain magnitude —
  so `InvalidStepReason` now judges a negative step, which this entry was waiting on.
  Descending is refused where a range is a **set** rather than an iteration (`lyra-E034`:
  a match pattern, a `newtype` constraint), with the message naming the ascending spelling
  of the same set. See COMPLETED.md.
- **[IDEA] Open-ended expression ranges** (`0..`), which need a lazy/infinite iterator. The
  pattern and constraint spellings have open bounds; the expression one deliberately does
  not, and that asymmetry is documented in `tree-sitter-lyra`'s `rangeBounds` rather than
  left to be rediscovered. The iterator it is waiting on is the `gen`/`Seq` section above.

## Language design — Pit of Success

Make the safe path the default.

### 1. Must-use `Result`/`Maybe` + `?` propagation

Canonical Result/Maybe identity is settled (a `CanonicalKind` stamp via the
name-independent `@builtin` attribute), and `?` checks the operand's error type against the
enclosing return.

- **[OPEN]** From-style **declared error conversion**, once a conversion trait exists.
  Today `?` is assignability-only.
- **[DONE 08/01] `?` lowers.** It had type-checked and then failed the build
  (`expression lowering not implemented for *ast.TryExpr`), so no program could use the
  language's primary error-propagation operator; found by exercising the prelude, 07/30.
  `pkg/backend/llvm/try.go` lowers it as the match it is — tag test, unwrap on success,
  and on failure rebuild the failure variant **at the enclosing function's return type**
  (the operand and the return are different instantiations, so the union cannot be
  forwarded) and `emitReturn`. See COMPLETED.md; the ownership half is below.
  - **[DONE 08/03] A temporary produced by a *sub-expression* of the operand no longer
    leaks on the propagating path.** `f(g())?`, where `g`'s owned result was consumed by a
    borrowing parameter. The propagating path now releases those temporaries into its own
    block (`releaseTempsOnExit`) instead of holding the whole pending list back. Measured
    both ways with LeakSanitizer on Linux: 19 bytes in 1 allocation before, none after.
    See COMPLETED.md.
- **[DONE 08/03] Shadowing a marked canonical type now explains itself.** `?` on a
  user's own `data Maybe` reported `` `?` operand must be a Result or Maybe, got Maybe ``
  — true, and useless, because it names the answer as the problem. The rule was kept (the
  marker confers the kind; a same-named unmarked type is ordinary) and the message
  replaced: the collector stamps `ShadowedCanonical`, and `?` says whether the shadow
  re-declares the prelude's type or is a different type wearing its name, each with the
  fix that fits.

  **The advice this entry used to recommend does not work**, which is the part worth
  keeping: "mark it `@builtin(Maybe)`" is `lyra-E017` (duplicate claim), because the
  prelude already holds the kind. A program can have exactly one canonical Maybe. The
  shipped message therefore never mentions `@builtin` — it says remove the declaration or
  rename it, and both are covered by tests that run the suggested fix. See COMPLETED.md.
### 2. Checked arithmetic by default; wraparound explicit

Trap-on-overflow covers all integer arithmetic, `wrapping_*`/`saturating_*` are the lowered
escape hatches, and the value-range pass both diagnoses definite faults (`lyra-E020`–`E023`,
`W011`) and elides the traps it can prove unnecessary. That backlog is clear.

- **[DONE 08/08] `checked_*`** — `checked_add`/`checked_sub`/`checked_mul`/`checked_div`,
  each `(self: T, other: T) -> Maybe<T>` on any concrete integer width. It turned out
  **not** to share #5's return-type-from-context problem: the receiver fixes the width, so
  the return type is determined rather than inferred from context. `checked_div` is in the
  set because its two failures — a zero divisor and `INT_MIN / -1` — are exactly the two
  cases `/` traps on. Branchless. See COMPLETED.md.
  - **[OPEN] No `checked_rem`.** Lyra has *two* remainder operators (`%` and `%%`), so
    the name would have to say which, and `checked_rem`/`checked_mod` is a naming decision
    rather than a lowering one — the guard is the same select `checked_div` uses.
- **[DONE 08/02] Bitwise and shift operators** — `& | ~ << >>`, prefix `~`, and the five
  compound assignments. An out-of-range shift amount traps
  (`lyra_panic_shift_overflow`), which is the same call div-by-zero makes and for the
  same reason: LLVM's shifts are UB there, so the alternative is a silently
  target-shaped answer. See COMPLETED.md. Two follow-ups:
  - **[DONE 08/02] The value-range pass tracks bitwise results.** `andI`/`orI`/`xorI`/
    `shlI`/`shrI` sit beside `addI`/`subI`/`mulI`, so `(x & 0x0F) + 1` now proves its
    addition safe and drops the trap. Each rule widens rather than guess: `&` needs one
    operand known non-negative (which is the masking case, and holds whatever the sign
    of the masked value), `|`/`~` need both, and the shifts need a bounded count.
    Soundness is checked by exhaustive brute force over every interval of a small
    width. See COMPLETED.md. Still imprecise on purpose: `|`/`~` over a possibly
    negative operand, and `&` where *both* sides may be negative, all widen to ⊤.
  - **[DONE 08/02] A variable shift amount elides its check when the range pass can
    bound it.** `NoShiftOverflow(e)` joins `NoDivZero`/`NoOverflow`; the proof
    obligation mirrors the emitted check exactly (an *unsigned* compare against the
    width, so the count needs a lower bound of 0 and a finite upper below the width).
    A constant in range was already folded at lowering; this covers the variable case,
    e.g. a count refined by `if n < 8`.
  - **[DONE 08/02] `x <<= n` types like `x = x << n`.** `checkAssignToBinding` split
    into `resolveAssignTarget` (the target's existence, mutability and type) plus the
    value check, so the shift path can apply the first and type its count as a count.
    A rejected target still returns its type, because every caller checks the value
    either way — a refused assignment must not hide the errors inside its value.
- **[IDEA] Type-level overflow policy on a `newtype`** — an overflow behaviour
  (`wrapping`/`saturating`) as a new constraint kind in the existing
  `newtype N = Base where …` grammar, so arithmetic on `N` uses that policy instead of the
  checked default.
  - *Why:* for types whose wrapping is definitional — hash/checksum accumulators, PRNG
    state, ring counters, hardware registers — the per-op `wrapping_*` methods are noise and
    a footgun; one missed call is a spurious trap or a bug.
  - *Why it does not contradict "wraparound explicit":* `newtype` is nominal, so the policy
    is opted into at the boundary (`Wrapping8(x)`, or an annotation) — locality lives at the
    conversion site rather than at every operation.
  - *What justifies it over the existing methods:* a `newtype` already carries a `range`, so
    `saturating` can clamp to the **domain** (`newtype Volume = u8 where range(0..<=100),
    saturating`) — something `saturating_add`, which is full-width only, cannot express.
  - *Open decisions:* (a) precedence — an explicit `.wrapping_add()` on a `saturating`
    newtype should override the type default, keeping the escape hatch meaningful;
    (b) mixing — `wrappingVal + plainU8` forces a conversion (nominal), sidestepping "whose
    policy wins"; (c) range-saturating semantics — does every intermediate clamp, or only
    bind/store (`(x+60)+60` for `0..<=100`)?
  - *Sequencing:* full-width wrap/saturate is one native op or an `llvm.*.sat` intrinsic;
    arbitrary-range saturation is compare+select after each op. **Wrapping-only** is the
    cheap, unambiguous first slice.

### 5. Lossy conversions must be loud

Widening is settled and narrowing already hard-errors.

- **[DECIDED]** Narrowing gets named methods — `truncate` / `saturate` / `narrow`, not a
  cast keyword. The builtin-method registry to host them exists.
- **[OPEN]** Their return type is the narrower target with no argument, so they need
  context-directed return-type inference (or a turbofish). Same blocker as `checked_*`.

### 8. Consistency cleanups

Settled: keep `data` / `struct` / named `tuple` / anonymous tuple — they sit at different
points on "does this grouping need a name and named fields?", not redundant. Rule of thumb:
sum → `data` (inline record for one-off named payloads; promote to a `struct` when the
payload earns a name); product → `struct` (named) / named `tuple` (positional nominal) /
anonymous tuple (ad hoc). Nothing open.

## Functional / imperative blend

**Model:** purity = no observable effect crossing the function boundary; local mutation of
owned values is fine. `ref`/`mut`/`own` tell the checker whether a mutation escapes. Payoff:
license to memoize, reorder, auto-parallelize.

The original item numbers (#1–#8) are kept on the bullets below, since code comments and
`CLAUDE.md` cite them; a number that no longer appears here belongs to a finished item and
is findable in COMPLETED.md.

Landed: the purity pass and bottom-up purity inference (`lyra-E007`); the three-level
binding ladder `let` / `let mut` / `var`; `ref`/`mut`/`own` parameter modifiers with
`lyra-W010` for an inert one; the `pure` ⊆ `det` ⊆ unannotated ladder plus orthogonal
`noalloc` (`lyra-E015`/`E016`); the use-after-move check for `own` (`lyra-E019`); and the
whole allocation-flavor axis — `stack`/`shared` compatibility (`lyra-E018`), recursive-type
well-formedness (`lyra-E014`), `shared`/dynamic arrays, for-in across arrays/ranges/strings,
interior assignment, and deep retain-on-copy.

- **[PARTIAL] Effect polymorphism over function-typed parameters.** Both halves landed
  07/31; details in `checker/README.md`, reasoning in COMPLETED.md.
  - The **inferred** half: a function's stored effect is its *base* plus its callback
    parameters, and a call site pays base ∪ the effects of the arguments supplied for them.
    `unwrap_or_else`, `ok_or_else` and every prelude combinator are annotated `pure noalloc` and
    callable from `pure` code, with an impure callback rejected at the call site.
  - The **declared** half: `lambda_type` takes the same `pure`/`det`/`noalloc` modifiers a
    lambda value does (`f: pure () -> t`), carried on `types.LambdaType` and enforced by
    `checkDeclaredCallbackBounds` at *every* call site. A bounded parameter is not
    polymorphic, so its function is pure for every caller. The standard library
    deliberately does not use it: a bound on `unwrap_or_else` would forbid a fallback that
    logs, and the inferred half already keeps pure callers pure.

  What is left:
  - **[OPEN] Callbacks reached through anything but a parameter or a binding** — a struct
    field, a call result, an array element — stay conservative (`AllEffects`). Multi-clause
    lambdas are no longer among them: they are desugared into a single-body match before the
    effect passes run, so their parameters are an ordinary indexed list.
  - **[DONE 07/31] Trait-impl methods** are polymorphic over their callbacks, and a bound
    written in a trait signature (`apply: (Self, pure () -> i64) -> i64`) is enforced at
    call sites. Note the receiver offset: signature parameter 0 is `Self`, which sits
    outside `call.Arguments` (`methodArgumentAt`).
  - **[OPEN] A declared bound is not inferred.** Passing an unconstrained parameter into a
    bounded slot is rejected rather than propagating the requirement outward, so a wrapper
    must declare its own bound by hand. Inferring it (a caller's parameter *becomes*
    bounded because it is forwarded into a bounded slot) is the natural next step and is
    what would make bounds composable without annotation churn.

- **[OPEN] (#3) Purity inference phase 2 for trait-method clauses.** Lambdas and free functions
  read the collector's `ScopeTable`; method clauses still re-walk the AST, because
  `CollectLambdaClause` records no scope. Needs a collector change reconciled with
  `checkTraitImplMethodBody`.
- **[OPEN] (#4) `ref`/`mut`/`own` outside parameter position**, and driving move/copy/borrow
  semantics from them.
- **[OPEN] (#5) Allocation, remaining pieces:** a `shared` construction in a bare
  argument/return position is not stamped with its flavor (only annotated bindings and
  `shared` payload args are); a *nested* `shared data` sub-pattern — destructuring a tail
  through its own box — errors loudly; construction-site `shared T {…}` syntax;
  implicit-alloc / escape analysis; atomic refcounts (deferred to the job system).
  - **[DONE 08/09] Dynamic-array growth.** `xs.push(v)`, amortized doubling. It needed a
    **representation change**: the elements were inline in the box (`{rc, weak, len,
    [0 x T]}`) and a `[]T` value *is* the box pointer, so growth would move the box and
    dangle every alias — and aliasing is observable, so that is a use-after-free rather
    than a semantics choice. They now sit behind a pointer (`{rc, weak, len, cap, T*}`),
    which costs one load per element access language-wide and is what every growable
    reference container pays. See COMPLETED.md, including the ownership bug where a
    builtin method's recorded signature made a pushed temporary read as *borrowed*.
- **[DONE 08/03] An array element carries an allocation or `weak` modifier** —
  `[]shared Node`, `[3]weak Observer`, `[16]stack Vec3`. A `tree-sitter-lyra` change only
  (`_element_type`); **nothing in this repo needed changing**, because the checking had been
  written for a syntax that did not exist — `firstAllocationMismatch` already recursed into
  array elements. See COMPLETED.md.
- **[DONE 08/03] A `weak` field is constructible** — `Maybe<weak T>`, so a cycle back-edge
  is optional and "no back-edge" stays distinct from "the referent is gone". **The premise
  this item was filed under was wrong twice over**, which is the part worth keeping: the
  grammar parses `Maybe<weak Node>` and always did (`parameterized_type`'s arguments are
  `$.type`, which includes `weak_type`), so no `tree-sitter-lyra` change was needed, and the
  real blockers were two missing switch cases with nothing to do with `weak` — a `shared`
  struct holding *any* generic field (`Maybe<i64>`) failed identically. See COMPLETED.md.
- **[DECIDED 07/11] Command-line args are ambient, not a `main` parameter.** `main` stays
  parameter-less always (one uniform entry-point shape); args are read through a builtin
  accessor tagged `EffectInput` — the same ambient-effect pattern as `random_seed()` /
  `wall_clock_nanos()`, so it composes with the `pure`/`det` ladder for free, including for
  a callee other than `main` that wants args. Matches Rust/Go/Zig/Swift over Java/C#. Not
  implemented; it is a convention recorded to prevent later signature churn.

  **The spelling this entry proposed — `CommandLine.args()` — is not available**, and both
  names it cited as precedent have since been replaced by bare ones. A member call on a type
  name is `lyra-E035` as of 08/06: the language has no type-namespaced associated functions,
  which is exactly why the prelude's constructors are `rng_seeded` rather than
  `Rng.seeded`. Whatever this becomes, it is a bare name (`command_line_args()`), or it
  waits on associated functions being built.
- **[ROADMAP] (#7) Explicit SIMD** — `simd<T,N>` → LLVM `<N x T>`, for determinism and games.
  Layer 1 is the primitive vector type; layer 2 is a data-parallel map over `pure`/`det`
  component arrays (the auto-parallel payoff). SoA-for-components, distinct from `[N]T`.
  Sequenced after the scalar backend; spec in `pkg/backend/llvm/SIMD.md`.

### Borrow model (#8) — targeted checks, not a Rust borrow checker

**[DECIDED 07/18]** Refcounting already carries memory safety, so the compiler closes only
the holes RC leaves. No lifetime annotations, ever. Use-after-move on `own` (a) is done, and
by-reference `mut`/`ref` parameters (d) landed 07/29.

- **[OPEN] (b) Borrows are second-class.** A `ref`/`mut` value may be read and passed down
  as a borrow, but never stored in a field, captured by an escaping closure, or returned —
  except the blessed **borrow-from-self accessor** (`(self: ref T) -> ref F`, whose result
  is treated as a borrow of the receiver). That is Rust's elision rule #3 as the *only*
  legal borrow-return form, which is what removes the need for lifetime syntax. Ambiguous
  cases (a borrow returned from a multi-`ref`-param function) are rejected: return `shared`
  or restructure.
- **[DEFERRED] (c) Exclusivity (`mut` XOR alias)** until dynamic arrays or the job system
  force it, and scoped there. Interior borrows into resizable containers are the one
  use-after-free RC does not cover. Leaning toward **statement-scoped projections**
  (Hylo/Swift copy-out/write-back subscripts — no holdable element reference, in-place via
  the optimizer) over a static container freeze. Parallel `ref` borrows get their
  no-mutation guarantee from `pure`/`det`, not a new checker.

## Wider integers — `i128`/`u128`

**[DECIDED 07/25; MVP LANDED 07/27]** Add `i128`/`u128`; do **not** add arbitrary-precision
bignums or Zig-style arbitrary fixed widths. These read as one question but are three, with
nothing in common in cost or fit.

- **`i128`/`u128` — yes.** The most on-brand numeric addition: fixed-width and identical on
  every target, exactly the determinism thesis that removed `int`/`uint`. LLVM lowers them
  natively and the checked-overflow model extends by width for free.
- **Arbitrary precision — no, not as a primitive; revisit as a stdlib `BigInt`.** It breaks
  two load-bearing decisions at once: fixed-width primitives, and trap-on-overflow (a bignum
  *grows* instead of trapping, so it cannot be what plain `i64 + i64` does). No systems
  language ships this as a builtin (Rust `num-bigint`, C++ Boost, Go `math/big`). Lyra is
  well placed to do the same later — a bignum is
  just another managed, ref-counted value, so the runtime shape is already solved; what is
  missing is a stdlib to host it, plus a small-int tagged-inline optimization before it is
  fast enough to want.
- **Zig-style arbitrary widths (`i7`, `u3`, `i256`) — skip** unless a concrete use case
  (bit-level wire protocols, packed hardware registers) shows up. Non-power-of-two widths
  force load/store legalization and mask/`sext`/`zext` sequences everywhere, and Lyra's
  primitives are *named constants* (`primitives.go`), not a numeric width field — `iN` would
  mean reworking that representation entirely.

Types, checked arithmetic, division via the builtins library, `match`, conversions and
`print` all landed. One gap remains:

- **[DONE 08/08] >64-bit literals.** A 128-bit constant can be *written*:
  `let mx: i128 = 170141183460469231731687303715884105727`. The magnitude lives in a
  `Wide *big.Int` on the literal node, nil for everything that fits 64 bits — so no golden
  output changed and every existing `.Value` reader stayed correct for existing inputs.
  See COMPLETED.md.
  - **[DONE 08/08] Compile-time folding is arbitrary precision.** `ast.FoldBigExpr` folds
    in `big.Int` and `FoldIntExpr` narrows at the end, so a consumer that needs an int64
    still gets ok=false rather than a wrapped value while the range check gets the true
    magnitude. It was not merely incomplete: a *declined* fold is a silent one, so
    `let d: u8 = 10^20 + 1` reached the backend unchecked and emitted invalid IR. See
    COMPLETED.md.

## Traits

### [DONE 08/08] `Show` — a bounded type parameter can be formatted

`"${v}"` and `println(v)` work on a value whose type is a type parameter, given a
`where t: Show` bound. The prelude ships the trait and an impl for every printable scalar,
all of it **ordinary Lyra** — `"${self}"` on a concrete primitive is exactly the formatter
`print` already picks, so no builtin was added.

The mechanism is a **desugar**: the operand is rewritten to `v.show()` before anything
downstream sees it, which is bound dispatch (landed 08/07) and needed no backend work. The
trait is recognized by its **method**, not by its name, so a program may define its own —
the same rule arithmetic operator overloading follows, and why no `@builtin(Show)` marker
is needed for this. See COMPLETED.md.

- **[DONE 08/08] A concrete type with a `Show` impl prints directly.** Same desugar, keyed
  on `resolveTraitMethod` instead of the bound. The coherence question answered itself: the
  alternative was not "print calls no user code" — the bounded-generic path already did —
  but "print calls user code only when laundered through a generic". A **self-recursion
  guard** came with it, because `impl Show for Pt { show = (self) => "${self}" }` is what
  the prelude's scalar impls say and would now call itself; it compiled and
  stack-overflowed. See COMPLETED.md.

### [DECIDED 08/07] `Eq` and `Ord`

**`==` stays structural; a trait impl overrides it.** Equality already works on scalars,
strings and — this is the part that decides the design — on a **bare type variable**:
`let same<t> = (a: t, b: t) -> bool => a == b` compiles and runs, monomorphized per
instantiation. So a trait is not needed to *make* generic equality possible, and requiring
one would remove working capability to gain a bound. `Eq` exists for the minority of types
whose equality is genuinely not field-wise — case-insensitive text, a struct with a cache
field to ignore.

**`Ord` is the opposite case and must exist.** There is no defensible structural default:
lexicographic-by-declaration-order is a *choice*, and a footgun, since reordering fields
would silently change the order. Nothing can order a user type today (`<` on a struct is
"operands must be numeric") and `<=>` is numeric+rune only.

```lyra
@builtin(Eq)  trait Eq  { eq: (Self, Self) -> bool }
@builtin(Ord) trait Ord: Eq { compare: (Self, Self) -> Ordering }
```

- **Recognized by marker, not by name** — the `@builtin(Maybe)`/`@builtin(Result)`
  precedent. Keeps `Eq`/`Ord` usable as ordinary names and makes recognition explicit.
- **One method each.** `!=` is `!eq`; `<` `<=` `>` `>=` are `compare` plus a match on the
  existing `Ordering`. An impl then cannot make `<` and `<=>` disagree — the failure mode
  C++ and Java both carry. `Ord: Eq` is what stops `compare` answering `Equal` where `eq`
  says false; supertrait syntax parses, and whether the bound is *enforced* is unverified.
- **Floats: `Eq` yes (IEEE, keeping `lyra-W008`), `Ord` no.** Consistent with `<=>` already
  refusing floats, and it avoids a fourth `unordered` variant that would make every integer
  three-way match carry a case that cannot occur. `total_cmp` in the prelude (bit-pattern
  order, Rust's approach) covers sorting. `PartialOrd` deferred until something needs it.
- **`@derive(Ord)`** drives the structural synthesis — field-wise for structs, tag-then-payload
  for data. `@derive(...)` already parses and collects onto `TypeDeclStmt.Derives`, and is
  read by nobody: the same collected-and-unread shape the `where` bounds had before 08/07.
  Eq needs no derive, being structural by default.

**The override needs a coherence rule**, or `==` means different things in different files —
the action-at-a-distance this language rejects elsewhere. Two candidates: the `Eq` impl must
live in the type's own module, or an impl anywhere is program-wide and duplicates are an
error. The second is simpler and is what the prerequisite below establishes anyway.

Prerequisites, both real bugs found while designing this:

- **[DONE 08/07] Duplicate impls are rejected** (`lyra-E037`), once, at the second impl and
  naming the first. Accepted silently before, which looked harmless while a trait only
  *added* methods — whichever impl won, the call had a body — and stops being harmless the
  moment one **overrides** something. It also closed a rule-5 inversion:
  `publishBoundCandidates` requires exactly one match, so a duplicated impl published no
  candidate and surfaced as a *backend* error at a call site far from the two declarations
  that caused it. **Identical targets only** — `impl Show for Box<t>` beside
  `impl Show for Box<i64>` overlaps without being identical, and ranking them needs the
  specificity ordering the language deliberately does not have (see receiver-keyed
  overloading), so genuine overlap is left open rather than half-answered.
- **[DONE 08/07] Structural `==` on an aggregate lowers** — struct, tuple, `data`, inline
  array, nested and all. A per-type glue function rather than an inlined comparison, for
  the reason drop.go gives for its own: a `data` value's equality branches on the tag, and
  a branching *call site* returns a merge block the pending-temporaries machinery does not
  handle. See COMPLETED.md.
- **[DONE 08/07] `lyra-W008` survives substitution.** It fires at the *instantiation* —
  `same(1.0, 2.0)` on a generic whose body compares `t` — and reports at the call, since
  the comparison is correct where it is written and the call is the line to change.

**[DONE 08/07] The `Eq` override.** `pub trait Eq { eq: (Self, Self) -> bool }`; `==`/`!=`
stay structural and an impl replaces them for that type. A primitive is never routed
through one, so `1 == 1` stays a machine comparison. An impl reaches through a *generic*
call too — the operand is a type variable at check time, so candidates are published per
implementing type and the backend picks by the substituted type, exactly as bound dispatch
does. Without that, `p == q` used the impl and `same(p, q)` silently used structural
equality: one operator meaning two things depending on whether it was written inside a
generic.

**Design correction:** the decided `trait Ord: Eq` is *not* built, and should not be.
The supertrait made sense under full dispatch; under the override model equality is always
available structurally, so requiring an `Eq` **impl** of every ordered type would break
`@derive(Ord)`, which synthesizes none. A type implementing both and letting them disagree
is a bug the compiler cannot see — the residual cost of equality not being a bound.

**Sequencing:** coherence → `Ord` (real gap, zero migration) → `@derive` consumption →
the `Eq` override → the two `==` bugs above. Ord first means the dispatch machinery is
exercised by something nobody has to migrate to.

**[DONE 08/07] `Ord` lands** (`std/prelude/ordering.lyra`): `compare: (Self, Self) -> Ordering`,
with `<=>` returning it directly and `<` `<=` `>` `>=` derived from its tag, so an impl
cannot make them disagree. A numeric or rune operand is never routed through it, so `1 < 2`
stays an `icmp` and a wrong impl cannot change the built-in types. Floats stay out, `<=>`
still refuses them. See COMPLETED.md.

- **[DONE 08/08] `Ord` and `Eq` are recognized by `@builtin(…)`.** An attribute list
  parses on a trait declaration, `TraitDeclStmt` carries `Builtin`/`CanonicalKind`, and
  the collector's canonical pass gained a trait half following the type half's two rules
  (marker wins; an unmarked, correctly-shaped trait of that name is the fallback when
  nothing claims the kind). A program's own `trait Ord` is now an ordinary trait.
  Dispatch filters by the resolved **declaration**, not the name — filtering by name is
  what let the shadow through in the first place. See COMPLETED.md.
- **[DONE 08/07] `@derive(Ord)` synthesizes the structural ordering** — lexicographic in
  field-declaration order, built as an ordinary `ast.TraitImplStmt` and appended to the
  program by the collector. Nothing downstream learns derives exist: the typechecker checks
  the synthesized body (so deriving over an unorderable field is an ordinary error naming
  that field's type), the coherence check refuses a derive beside a hand-written impl for
  free, and the backend lowers it through the path that already exists. `@derive(...)` had
  parsed and been collected onto `TypeDeclStmt.Derives` from the start and read by nobody.
  - **[DONE 08/07] `@derive(Ord)` on a `data` type** — by constructor declaration order
    first, then payload. It is **3n arms, not N-squared**: for each constructor in order,
    `(Ci(a…), Ci(b…))` compares payloads, `(Ci(_…), _)` is `Less` and `(_, Ci(_…))` is
    `Greater`; past those three no later arm can see `Ci` on either side, and the last
    constructor needs only the first. That estimate is what had made this look not worth
    building.
  - **[DONE 08/07] `string` has `Ord`.** `"a" < "b"` works and a string payload or field
    derives. The primitive is `s.compare_bytes(other) -> i64` (memcmp's convention) and the
    prelude's `impl Ord for string` maps it to `Ordering` — the builtin is the part that
    cannot be written in Lyra, everything shaped on top of it is. **Byte order is code-point
    order in UTF-8**, so one memcmp answers what a rune walk would; written in the prelude
    with `s[i]` it would have been O(n²), since indexing is O(i). Not locale-aware: `"Z"`
    sorts before `"a"`, and collation needs tables that belong in a Unicode library.
  - **[DONE 08/07] A single wildcard stands for a multi-field payload.** `Rect _` lowers,
    expanding to one wildcard per field — exact rather than approximate, since a wildcard
    binds nothing and tests nothing. Binding a whole multi-field payload as one value
    (`Rect pair`) is still unimplemented and now says so, naming the spelling that works
    instead of reporting "not implemented" about a form that was.
  - **Declaration order is the ordering**, which is why it is opt-in: reordering a struct's
    fields changes how its values sort, and a type that silently acquired an order nobody
    chose would be worse. Rust makes the same trade.
- **[DONE 08/07] A supertrait is enforced** (`lyra-E040`): `impl B for T` where
  `trait B: A` requires an `impl A for T`. `TraitDeclStmt.Bounds` was collected and read by
  nobody, so the promise was never checked — found by the AST sweep below.
  - **[OPEN] A supertrait's methods are not in scope through a subtrait bound.** The
    bound is *enforced* — `impl Nd for T` requires `impl Len for T` — and then
    `where t: Nd` still cannot call `t.l()`: *"type parameter t has no method `l`; add a
    `where t: Trait` bound whose trait declares it"*. So a supertrait today promises a
    thing exists and gives the one place that needs it no way to reach it, which is the
    half of the feature people actually write supertraits for.

    **The receiver's concreteness is what decides it**, not the supertrait: a concrete
    `n.l()` resolves fine, because ordinary dispatch finds the `impl Len for i64` by the
    receiver's type. Only the *bound* path fails.

    Why is specific: `tc.genericBounds[param]` holds the literal trait names from the
    `where` clause (`typechecker_traits.go`, one write), and the four sites that read it
    — bound dispatch, the generic-argument check, operator overloading and `Show` —
    iterate that list directly. None walks the named traits' own `TraitDeclStmt.Bounds`,
    which is the field E040 already reads for enforcement. So the fix is a **transitive
    closure at the one point genericBounds is populated**, after which all four readers
    get it for free — the shape hazard 8 recommends over teaching four consumers the same
    rule.

    The workaround is to name both (`where t: Nd + Len`), which works: multiple bounds
    are supported in both spellings (`A + B` and `A, B`). It is also what makes this
    merely awkward rather than blocking — and why it can wait.

    Found 08/09 while weighing a `Length` trait beside `Needle`. It is the reason
    `trait Needle: Length` would not have helped: the bound would be enforced and
    `split` still could not call the method.
- **[OPEN] A trait *default method* is never dispatched to.** `trait G { name: …
  twice: (Self) -> i64 = (self) => self.name() * 2 }` parses, collects, and calling
  `n.twice()` on a type with an `impl G` reports *"i64 has no method `twice`"*. An impl
  cannot override one either, since nothing looks for it.

  Independent of the supertrait gap above — it reproduces with no supertrait in sight —
  and the **fifth** instance of the shape this file keeps cataloguing: a surface that
  parses, collects, and is read by nobody, after `wallClock`, the `where` bounds,
  `@derive` and the operator-named methods. `walk.go` descends into
  `TraitDeclStmt.Methods[i].DefaultMethod.Body` and the return checker gives it a
  function scope (08/09), so the body is *walked* and checked — it simply cannot be
  called. Found 08/09 alongside the supertrait gap.
- **[DECIDED 08/07] `Ord: Eq` is deliberately *not* declared** — see the design correction
  above. Supertrait syntax parses and the bound is collected onto `TraitDeclStmt.Bounds`;
  whether anything enforces it is still unverified, and no longer on this path.

### [DECIDED 08/07] Operator-named trait methods

`(_==_)`, `(_+_)`, `(-_)`, `(_++)` parse and collect, and **nothing dispatches to them**:
every consumer (`resolveTraitMethod`, `findTraitMethod`, the purity pass) filters on
`MethodNameKindIdentifier` and skips the rest, so a `(_==_)` impl on a struct is never
called and `==` keeps its built-in meaning. The grammar reserves twenty binary spellings
plus the prefix and suffix forms. The fourth collected-and-unread surface found in two
days, after `wallClock`, the `where` bounds and `@derive`.

**Split, rather than kept or removed wholesale.**

- **The seven comparison operators are refused** (`lyra-E039`), naming the trait that owns
  each: `==`/`!=` → `Eq`, `<`/`<=`/`>`/`>=`/`<=>` → `Ord`. The compiler owns them as of
  08/07, so a second mechanism is a coherence question with no answer — and declaring them
  one at a time reintroduces exactly the `<`-disagrees-with-`<=>` failure `Ord`'s single
  `compare` exists to prevent, which is the C++/Java shape.
- **Everything else warns** (`lyra-W015`) that nothing dispatches to it. Arithmetic has no
  canonical trait and no other design on the table, and `(_-_)` is load-bearing for a
  hazard already recorded (`Empty - 1` parses as `Empty(-1)`, which only bites a `data`
  type overloading `-`). Removing the syntax would discard the only plan; keeping it silent
  is what this project keeps paying for.

- **[DONE 08/08] An operator dispatches through a `where` bound.** `a + b` where `a: t`
  and `t: Add` resolves against the trait's declared signature, records the abstract
  resolution for the purity join, and publishes one concrete resolution per implementing
  type for the specialization to pick — the three steps a bound *call* has taken since
  08/07, for a node that is not a call. See COMPLETED.md.

- **[DONE 08/07] The arithmetic half is implemented.** Ten binary operators
  (`+ - * / % << >> & | ~`), the prefix `-` and `~`, and the compound assignments,
  dispatch to a trait method named for the operator — keyed on the **method name**, with
  the trait whatever the author declared, since `+` on a matrix and `+` on a duration
  share no invariant the way `<` and `<=>` do. Everything left warns with the *reason* it
  is inert: `&&`/`||` cannot short-circuit through a call, `!` is boolean negation, `**`
  is a spelling with no operator, and the suffix forms name operators the language does
  not have. See COMPLETED.md.

### Trait machinery

Trait-method lowering landed 07/30: an impl method lowers to a function taking the receiver
first, and dispatch is static. That entry used to end "and a generic impl needs no extra
machinery" — it needed exactly the machinery a generic function needs, built 08/03: one
emitted function per binding set, the body lowered under those bindings, and an ownership
table per specialization. See COMPLETED.md.

- **[PARTIAL] Borrow modifiers on trait signatures.** `ref` and `mut` landed 07/31:
  `bump: (mut Self) -> void` writes through to the caller, and `peek: (ref Self) -> i64`
  borrows without copying. The grammar always accepted them — `trait_method_signature` is an
  aliased `lambda_type` whose `parameter_type` has always carried an optional
  `type_modifier`; what dropped them was `Collector.parseParameterType`, plus the absence of
  a field to hold them (`types.ParameterType.Borrow` now exists beside the allocation
  `Modifier`).

  - **[DONE 08/03] `own` is supported**, on parameters and on the receiver. lyra-E030 is
    retired. The restriction had named its own prerequisite — teach the ownership pass
    about method bodies — and that was the smaller half of what it turned out to be
    guarding; see COMPLETED.md for the two resolution gaps behind it.
  - *Watch for*: the rule that any code rebuilding a `types.ParameterType` field-by-field
    silently drops new fields. Three sites did (`substituteSelf`, the lambda→signature
    conversion in `typechecker_traits.go`, and `lambdaSignature`), and the symptom was a
    `mut` receiver that parsed, type-checked, and quietly wrote to a copy.

## Method syntax for free functions (UFCS)

**[DECIDED 08/04] Overloading stays receiver-keyed — no general overloading on argument
types.** Two same-named functions without a `self` receiver remain a redeclaration error.
Revisited deliberately; the reasons are Lyra-specific rather than a general position:

- **The specificity ordering comes back.** Receiver-keyed overloading works *because*
  overlap is refused at the declaration — different type heads, one axis, checked once in
  one place. Whether two arbitrary signatures overlap depends on the whole type system
  across N parameters, so it cannot be refused there and needs ranking at every call site,
  which is exactly what the head rule exists to avoid.
- **It fights context-directed literal inference**, which is the sharpest one. `5` stays
  `untyped_int` and a width flows *down* onto the leaves (`propagateLiteralType`, nine call
  sites). Overloading needs the callee's type to flow *up* from the arguments, so `f(5)`
  against `f(i64)` and `f(u8)` has no answer without a preference rule — and any such rule
  is pulling against a mechanism the whole typechecker leans on. Generics (`f<t>(x: t)` vs
  `f(x: i64)`) and default arguments make it worse.
- **Traits already give ad-hoc polymorphism**, with one canonical name, static dispatch, and
  composition with generics through `where` bounds. `trait Abs` with impls for `i64` and
  `f64` compiles and runs today (verified 08/04). This is the Haskell/Rust trade: typeclasses
  instead of overloading, because overload resolution and type inference pull against each
  other.

Receiver-keyed overloading is the narrow exception on purpose: it is method *syntax* rather
than ad-hoc polymorphism, the receiver is a single privileged position, and the head rule
keeps it decidable where it is written.

**Corollary worth stating, since it is the tempting shortcut:** the cross-module name
collision (Modules, above) must **not** be fixed with overloading. It is a namespacing
problem — letting `import a; import b` silently merge two unrelated `helper`s into an
overload set is worse than today's error. The key-level fix is the right shape.

**[DECIDED 07/31; BUILT 08/03]** UFCS — `x.f(y)` resolving to a free function `f(x, y)` —
**opt-in via a first parameter named `self`**. A function written `(self: Maybe<t>, …)` is
callable both ways; every other function stays call-only. See COMPLETED.md, and
`pkg/analyzer/typechecker/README.md`'s last section for the mechanism.

Two decisions taken at build time, neither of which the design below had settled:

- **An import is required** to call into another module method-style. A file's own module
  and the prelude need none. The alternative — any `pub` function in the program being
  reachable through a value of its type — reads better in the abstract and worse in
  practice: whether your call compiles would depend on whether some *other* file imported
  that module.
- **An `own` receiver is refused**, so a move always looks like a call.

Still open here: **the two spellings are one call, but only the desugared one is what later
passes see.** That is what makes the feature cheap, and it means a diagnostic reported
against an argument index can name a position the reader did not write (argument 1 of
`m.f(x)` is `m`). Nothing does that today — the messages in play name parameters, not
indices — but a future one could, and the fix belongs wherever that message is written.

*Why it earns its place here, rather than being sugar.* A free function's name is a
program-wide land grab today, which is the whole reason the standard library splits
`maybe.map` from `result.map` and why putting either in the prelude claims the name `map`
for one type forever. UFCS disambiguates on the **receiver type** — precisely the axis that
distinguishes them — so both are reachable as `m.map(f)` / `r.map(f)` from free functions in
different modules, with no overloading. The pressure to put combinators in the prelude at all
goes away: a module's functions become reachable through values of its type without an import
binding their names.

It also **routes around trait type-parameter binding**, which is what blocks method-form
combinators today (`typechecker_trait_dispatch.go`: a method returning the impl's element
type "is not yet fully instantiated"). UFCS delivers the same ergonomics without traits, and
is much the cheaper of the two — which is the sequencing argument: do this before anyone
reaches for the trait feature to get `m.unwrap_or(0)`.

*Why opt-in rather than universal.* The author decides what is a receiver, so adding a helper
to a module cannot change what `x.f()` means elsewhere in it; `self` already spells "receiver"
in trait impls, so the language gains no second word for the same idea; LSP completion on `m.`
stays a curated set rather than every function in scope; and nothing existing changes meaning,
so there is no migration. Note that **Odin, which Lyra borrows from elsewhere (`%%`, the
`rune` naming), deliberately rejected UFCS** on the grounds that it obscures where a procedure
comes from — the `self` opt-in is the answer to exactly that objection.

*Resolution order:* struct field → trait method → UFCS → builtin. A real impl beats a free
function, and user code still shadows a compiler builtin, both matching the existing ladder in
`inferMemberCall` (and its mirror, the backend's `lowerBuiltinMethodCall`).

*Open sub-decision:* **whether `own self` may be called method-style.** `x.consume()` moving
`x` is caught by use-after-move (`lyra-E019`), but the receiver syntax hides the move, which
cuts against making costs visible. Leaning toward refusing UFCS for an `own` receiver, so a
move always looks like a call.

*One hazard to build against, not discover.* **The purity pass indexes arguments
positionally** — `callableParams` maps a parameter name to its position and
`checkDeclaredCallbackBounds`/`callEffect` read `call.Arguments[idx]`. At a UFCS call site the
receiver sits *outside* `call.Arguments`, so every index shifts by one: without handling,
`m.unwrap_or_else(f)` checks `f` against the wrong parameter's declared bound — silently, since
both are function-typed. The backend's `lowerDirectCall` argument coercion has the same shift.

*Sequencing (superseded).* This said "after the two open trait gaps above, since UFCS adds a
fourth caller into the same member-call resolution path". It went first instead, and the
reasoning was wrong in a way worth keeping: those gaps are `own` receivers and ownership
analysis of method bodies — orthogonal to the ladder, which UFCS extends by inserting one
rung that returns early. What actually made the order right is that UFCS rides the path that
*works*: a generic free function monomorphizes today, while a generic trait impl's method
does not (it reaches the backend with `Maybe<t>` unspecialized — "match on Maybe<t> not
implemented yet"). Method ergonomics through traits needs that built first; through UFCS it
needed nothing.
