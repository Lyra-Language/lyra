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
- **[DONE 08/06] String length, slicing and trimming.** `s.len()` (rune count — O(n)
  when this landed; **O(1)** since 08/12, when the count began riding the fat pointer:
  see Known bugs) and
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

    A negative `offset` is `None`: an offset here is a resumption point for a scan, and
    "resume at k going forward" has no negative reading. Python's `str.find` counts its
    `start` from the end and is a known confusion for it. (This entry originally
    contrasted that with `s[-1]`, which *did* read from the end at the time; since 08/12
    the two agree — negatives are refused everywhere, and the tension this note recorded
    is gone.)
  - **[SUPERSEDED 08/12 — see the from_end entry in Known bugs] Negative string
    indexing and negative `slice` bounds** landed 08/08 (`s[-1]`, `s.slice(1, -1)`) and
    were removed four days later: the audit's sharpest design finding was that they
    handed the most common off-by-one a valid read of the wrong element, in the language
    whose thesis is trap-over-silently-wrong. What this entry got *right* — the k-th rune
    from the end is a byte walk over continuation bytes that decodes nothing, where
    `s[s.len() - 1]` is two full O(n) decode walks (34272 µs against 18 µs) — survives
    intact in `from_end(k)`, which lowers to the same walk; the mistake was spelling an
    end-relative *operation* as a dual meaning of the index domain. The shared
    `lyra_str_rune_offset` and its backward branch remain, as from_end's implementation.
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
  - **[DONE 08/09] A type alias is transparent inside a generic argument** — nested in a
    tuple, as a defaulted parameter, through a trait method with guard-clause returns.
    `Maybe<(Index, Length)>` for `type Index = i64` is one instantiation with
    `Maybe<(i64, i64)>`, everywhere.

    **Five raw-annotation readers, failing serially** — each fix exposed the next, which
    is why the regression test is one composed shape rather than five cases:
    `checkReturnStmt` read `enclosingRet` as written (both set sites fill it before
    their own resolution runs), `solveTypeVars` unified the raw annotation and blamed
    the type variable, `instantiateSignature` rebuilt the checked signature raw so the
    argument check rejected what inference had accepted, `inferDotCallFromType` returned
    the trait signature's return type raw at all three exits — its own comment records
    the *parameter* half of that fix landing earlier, hazard 8's return-position
    asymmetry to the letter — and the backend's `instantiationSymbol` mangled the
    argument as written.

    Fixing the last one corrected an earlier claim of this file's: `resolveForLayout`
    is the *right* tool for symbol mangling, not the wrong one, because collapsing a
    newtype is correct there — a newtype is nominal to the typechecker and transparent
    to codegen, so two instantiations sharing a layout sharing a symbol is the truth of
    the matter. The `inferDotCallFromType` fix also closed the third bug from the
    `found_at` redesign: `Maybe<UserStruct>` returned from a trait method rejecting
    itself ("expected Maybe<Hit>, got Maybe<Hit>").
  - **[DONE 08/09] `split` on an empty separator traps**, naming the fix:
    *"split: empty separator; to split a string into its runes, use to_runes()"* — and
    `to_runes() -> []rune` exists, four lines over `for c in s` and `push`. It is what
    "split on empty" actually means, stated as what it returns rather than smuggled
    through a degenerate argument (Go's `strings.Split(s, "")`); `[]rune` rather than
    `[]string`, since a caller who wants characters wants code points, not a box per
    character.

    **A trap rather than a `Maybe<[]string]>`, and the reasoning is `slice`'s about its
    inverted range**: an empty separator is a caller bug, not a question with a
    None-shaped answer, and the empty result a soft path would return is
    indistinguishable from a real one. A rune separator *cannot* be empty, so the common
    call would have paid the unwrap for an impossibility — and `panic` is EffectNone, so
    `split` stays `pure`. Python raises here for the same reason. The guard lives in
    `split` (`span == 0` at match time), not in `Needle`: a zero-length match stays
    legitimate for `found_at`, since `"".index("")` answering `Some((0, 0))` is what
    makes a search for an empty needle terminate.

## Known bugs

- **[DONE 08/13] A fixed-array *binding* no longer takes a `[]T` slot** — it
  segfaulted.

  ```
  let take = (xs: []i64) -> i64 => xs[0]
  let ys: [3]i64 = [1, 2, 3]
  take(ys)      // checked clean; segfaulted at run time
  ```

  `[N]T` is stack storage and `[]T` a ref-counted box, so the callee indexed through
  a pointer that was really the array's first element. **The cause is this audit's
  recurring shape**: `isAssignable`'s rule said in its own comment "a static array
  **literal** is assignable to a dynamic array" and then tested only the *type*, so
  every `[N]T` value passed. The comment named the right rule.

  The two claims are now separate. `isAssignable` (types only) refuses it;
  `assignableValue` adds the one widening that depends on **what the expression is**,
  and is used at the ~14 sites where a value is checked against a type — binding,
  reassignment, argument, return, clause body, struct field, aggregate element,
  generic and trait-dispatch arguments. Removing the type-level rule first and
  reading the failures is what produced that list: *every* failure was a literal,
  which is the evidence literals were its only legitimate use.

  **The allowance walks the expression alongside the type**, which nesting forces:
  `[[1, 2], [3, 4]]` and `[y1, y2]` (two `[2]i64` bindings) have the *same* type,
  `[2][2]i64`, so only the expressions separate the legal case from the crashing one.
  It looks through a newtype target (`newtype Row = []i64` takes a literal) and into
  a tuple literal (`() -> (i64, []i64) => (1, [2, 3])`), and the tuple arm re-checks
  the *name* so it cannot become a second path past nominal typing — it briefly was,
  accepting a `Point` into a `Vector` slot until the suite caught it.

  What it deliberately does **not** do is convert a built array: there is no implicit
  copy from stack storage into a box, because that is a hidden allocation in a
  language that makes allocation explicit (and `noalloc` would have to charge it). A
  binding that must be dynamic is declared dynamic. See COMPLETED.md.

- **[DONE 08/13] A float literal in a comparison takes the operand's width**, and the
  program builds again. `let x: f32 = 0.1` then `x == 0.1` emitted
  `fcmp oeq float %1, 0x3FB999999999999A` — a **double** constant in a `float`
  compare — which clang rejected outright.

  **The cause was an `else if`.** In `checkBooleanBinaryOpExpr`'s `==`/`!=` arm the
  float-imprecision warning (`lyra-W008`) sat where `propagateComparisonWidth`
  belonged:

  ```go
  } else if isFloatType(leftType) || isFloatType(rightType) {
      …warn about precision…            // and return, never propagating
  } else {
      tc.propagateComparisonWidth(…)
  }
  ```

  So the operators the warning was *about* were exactly the ones whose width never
  propagated — a warning about floating-point precision that stopped the program
  compiling at all. The warning is advice, so it is emitted *alongside* the
  propagation now. The relational operators (`<`, `<=`, `>`, `>=`) never had the bug,
  their branch propagating unconditionally, which is why `x < 0.1` always worked and
  `x == 0.1` never did — a difference with no reason behind it, which is what an
  `else if` doing double duty looks like from the outside.

  Nothing else needed changing: `numericResultType` already answered
  `untyped_float + f32 → f32`, `propagateLiteralType` already had its float arm, and
  the backend's `literalFloatType` already reads the recorded type. One link in a
  chain that was otherwise complete. See COMPLETED.md.

- **[DONE 08/13] `f16` literals no longer log an llir "please submit a bug report"
  line.** `let a: f16 = 0.1` printed `unable to represent floating-point constant 0.1
  of type half exactly; please submit a bug report to llir/llvm` on an otherwise
  correct build — llir logs for *any* inexact half, which is most of them, and then
  emits the correctly rounded value anyway. Noise, not wrongness, but a compiler
  should not tell its users to file bugs against a library they did not choose.

  `floatConst` gained a `Half` arm that pre-rounds, so the value handed to llir is
  exactly representable and its exactness test has nothing to report. **The rounding
  goes through `binary16.NewFromFloat64` — llir's own conversion, the one
  `Float.Ident()` calls** — rather than being hand-written: Go has no float16, and a
  hand-rolled round-to-nearest-even with subnormal and overflow cases could disagree
  with the library it feeds, which is the one thing a rounding fix must not do.
  Agreement is structural this way. `binary16` was already in the module graph via
  llir, so this promotes a transitive dependency to a direct one rather than adding
  anything. The emitted value is unchanged (0.1 stays `0xH2E66`), which the test pins.

- **[DONE 08/13] Regex matching at run time**, which lifts the one-day-old restriction
  that a `pattern(...)` newtype could only be built from a literal. There is still no
  regex engine in the runtime, and there does not need to be: a constraint's pattern is
  part of a *type*, so it is known while compiling. `pkg/regex` runs then, its lazy DFA
  is flattened (`regex.Matcher`) into `Trans[state*256+byte]` with the text boundaries
  folded in, the tables become private constant globals, and one shared
  `lyra_regex_match` driver walks them — O(n), no backtracking, no allocation, and a
  trap is `EffectNone` so `pure noalloc` code may construct one.

  **Agreement was the risk and was attacked first.** `MultiLine` is on by default, so
  `^`/`$` fire at every `\n` and `IsMatch` omits the trailing beginning-of-line after
  the final byte; `stepByte` mirrors that call sequence rather than re-deriving it, and
  the trailing newline gets its own column so the difference lives in the table. The
  table is then checked against the engine over a corpus (curated newline cases,
  exhaustive short strings, all 256 byte values), and compiled programs are checked
  against the engine for 31 pairs — a Go test can verify the table, but only a running
  program verifies that the IR implements it.

  A **literal still costs nothing** (no table, no driver, no call — IR-pinned), and one
  pattern emits one table however many places use it. Measured: 3 KB for `^[0-9]+$`,
  8 KB for a realistic email pattern. Still refused: a lookbehind (its gate depends on
  text before the input) and a DFA past `regex.MaxTableStates` — both properties of the
  pattern, so lyra-E054 now names the pattern rather than the value. `lyra-E052`, a
  regex as a first-class *value*, is unchanged. See COMPLETED.md.

- **[DONE 08/13] A `where` constraint is enforced at run time**, so the ladder has its
  second rung. `range(...)`, `values(...)` and `step(...)` now trap on a value that
  violates them; `pattern(...)` refuses a value it cannot read. Details in
  COMPLETED.md; what follows is the decision record, since three of the four parts
  were choices rather than fixes.

  - **The typechecker publishes the sites, the backend emits the checks**
    (`typetable.ConstraintTable`). Only the typechecker knows what it managed to
    prove, so a **foldable constant is never recorded** — it was already decided, a
    bad one being a compile error and a good one needing nothing. The cost is one
    compare-and-branch exactly where the compiler could not do better, which is what
    arithmetic overflow already pays.
  - **`step(...)` became real.** It had been collected, validated for well-formedness
    and read by nothing, so `range(0..<360), step(15)` accepted 7. The grid is
    measured from the range's start (`start, start+step, …`, the meaning
    `types/step.go` already fixed for both spellings), so `range(5..<=95), step(10)`
    accepts 15 and refuses 10.
  - **`pattern(...)` refuses what it cannot read** (`lyra-E054`) rather than admitting
    it. It cannot have a runtime check without a regex engine, which `lyra-E052`
    records the absence of, so the two honest options were refuse or admit — and
    admitting is what let `Digits("abc")` build and print `abc`. A literal still
    works. The cost is that a pattern-constrained newtype cannot be built from runtime
    data until there is an engine; that is a feature waiting on the engine rather than
    a rule.
  - **One construction is one check.** `Percent(n)` reaches the checker twice — as the
    constructor's operand and again as the constructor node once the context
    propagates the newtype onto it — which emitted the range test twice and reported
    E054 twice. The constructor is a `TupleLiteralExpr` rather than a call (it is the
    named-tuple node), which is why the first guard against this matched nothing.

- **[SUPERSEDED 08/13 — see above] A `where` constraint is enforced only where the value is *provable*.**
  Found while probing the regex phantom (08/13), and it is about `range(…)` as much
  as `pattern(…)` — the two behave alike, so this is one gap, not a regex one. Both
  catch a literal and anything the value-range pass can pin to an interval, and both
  **silently accept everything else**:

  ```
  newtype Percent = u8 where range(0..<=100)
  newtype Digits  = string where pattern(r"^[0-9]+$")

  let mk  = (n: u8) -> Percent    => Percent(n)     // no diagnostic
  let mkd = (s: string) -> Digits => Digits(s)      // no diagnostic
  mk(200)      // builds, runs, prints 200
  mkd("abc")   // builds, runs, prints abc
  ```

  Inside `mk`, `n` is an opaque parameter, so nothing can prove anything and the
  constructor passes it straight through. The docs say constraints are "checked
  wherever the newtype flows … and through the constructor too", which is true of the
  *provable* cases and reads as a guarantee.

  **The decision is whether a constraint is a compile-time assertion or a runtime
  one**, and it is a real language call rather than a bug to patch: enforcing at
  runtime means the constructor emits a check and traps (the trap-over-silently-wrong
  ladder's usual answer, and what `range` already does for arithmetic), which costs a
  branch per construction — and for `pattern` it costs **a regex engine in the
  runtime**, which is exactly what lyra-E052 says Lyra does not have. So the two
  constraint kinds may not get the same answer. Until it is decided, the honest fix is
  narrower: say in the docs that constraints are compile-time-only, since the current
  wording promises more than the compiler delivers.

- **[DONE 08/13] A generic `[]t` parameter solves `t` from an array literal.**
  `first_of([1, 2, 3])` against `(xs: []t)` reported *"cannot infer type variable t
  from these arguments"* while the identical call with a `[]i64` binding worked, and
  so did a `[3]t` parameter — so the literal was the only thing between a legal call
  and a diagnostic naming the wrong problem.

  An array literal is the one expression whose *representation* is chosen by its
  context (`[1, 2, 3]` is a fixed `[3]T` or a heap `[]T` "told apart by what the
  literal is used as"), and the mechanism for that — propagating the target type onto
  the literal — cannot run at a generic call, because the target is `[]t` and `t` is
  precisely what is being solved. `arrayLiteralAsDeclared` reads the shape off the
  *declaration* instead, which is all unification needs; the ordinary propagation then
  runs against the substituted `[]i64` and records the literal as dynamic, so the
  callee gets a real box. Verified by running the compiled programs (including string
  elements under ASan), not only by checking them.

  **Only a literal is adapted, and that is the safety rule rather than a shortcut** —
  see the segfault entry above. A `[N]T` *binding* is stack storage, so accepting one
  where a box is expected is a misinterpretation of memory; the generic path refuses
  it, and a test pins that refusal so the open bug is not silently imported here.
  Only the outermost level is adapted: a nested `[][]t` from `[[1, 2]]` stays
  unsolved rather than guessed at. See COMPLETED.md.

- **[DONE 08/13] A printed float reads back as the same value**, and a narrow float
  constant is rounded rather than truncated — two bugs, the second hidden by the
  first. Printing was one `snprintf("%g")`, whose default is **six** significant
  digits, so `println(0.1 + 0.2)` printed `0.3`, `1.0 / 3.0` printed `0.333333`,
  `3.14159265358979` printed `3.14159`, and `1234567890.0` printed `1.23457e+09`.
  Every one is a different number from the one the program held, printed with nothing
  to say so — and reading a printed value back is the ordinary way to move data
  between programs.

  The formatter (`lyra_f{16,32,64}_to_str`, one emitted per printed width) renders at
  increasing precision and `strtod`s each candidate, stopping at the first that comes
  back equal. The ladder's top rung is the width's IEEE round-trip guarantee (17 / 9 /
  5 significant digits), so it always terminates with a faithful answer; the bottom
  rung is where `%g`'s trailing-zero stripping usually lands it in one iteration.
  **The comparison is made at the value's own width** — an f32 narrows back to `float`
  before comparing, since 0.1f32 widened to a double is 0.10000000149011612 and a
  double-width check would reject `0.1` and print all of that. Shortest *within the
  ladder*, not provably minimal (Ryu is the upgrade path); what mattered was that a
  printed float denote the value printed.

  **Then the faithful printer immediately exposed a shipped correctness bug**:
  `let x: f32 = 0.1` was emitting `float 0x3FB9999980000000`, one ULP below 0.1f32
  (`0x3FB99999A0000000`), because llir stores the float64 and *truncates* the mantissa
  on emission instead of rounding to nearest. The program held a number its source did
  not name. `floatConst` rounds to the target width in Go first, after which llir's
  truncation has nothing left to remove. It was invisible for as long as printing was
  lossy — at six digits the wrong constant and the right one both printed `0.1` — which
  is the general argument: **a lossy printer does not merely lose detail, it conceals
  other faults.** Two adjacent findings are recorded above (a float literal is not
  narrowed in a comparison, and f16 literals log an llir bug-report line). ASan clean
  on macOS and Linux. See COMPLETED.md.

- **[DONE 08/13] The regex-value phantom is closed** (`lyra-E052`), in **two**
  positions — the audit named the expression one and probing turned up its twin. A
  regex literal as a *value* (`let re = r"[a-z]+"`) inferred the built-in `regex`
  type and died in the backend as `expression lowering not implemented`; a regex
  *match pattern* (`match s { r"^[0-9]+$" => … }`) type-checked clean and died as
  `regex patterns deferred`. A string scrutinee was the only place the pattern form
  was accepted at all — every other scrutinee kind already refused it — so the two
  halves are now consistent instead of one being refused and the other silently taken.

  The `regex` primitive type had exactly **one** consumer, the literal's own
  inference, and is not even a spellable annotation: a lowercase type name parses as
  a type *variable*, so `(re: regex)` declares one named `regex` and behaves
  identically to `(re: zzzz)`. Implementing this is a project rather than a fix — a
  regex *value* needs an engine in the runtime, and the runtime is hand-written C
  with no FFI; the `regexp` the compiler uses to validate patterns runs at compile
  time and cannot ship into the program.

  **`where pattern(r"…")` is untouched and keeps working**, which is why the refusal
  is on the two value positions rather than on the literal syntax: a constraint
  stores the pattern's *source text* and compiles it at type-check time, so it never
  produces a `regex` value and never needs a runtime engine. Compile-time syntax
  validation moved out of the two refused positions with them — a second error about
  a malformed pattern's contents, on a construct that has no meaning at all, is the
  E011-and-E001 double report again — and stays where it does real work. Six tests
  inverted; the constraint suite is unchanged and still passes. See COMPLETED.md.

- **[DONE 08/13] The raw-pointer / `unsafe` phantom is closed** (`lyra-E051`), and its
  distinguishing feature was that **the compiler's own advice could not be followed**.
  `&x` drew `lyra-E011` — "taking a raw pointer with `&` requires an `unsafe` block or
  function" — and `unsafe { … }` was itself `unknown expression type "unsafe_block"`,
  so doing exactly as instructed produced a different error. `&x` also double-reported
  (E011 *and* E001 at one location), and a pointer *write* got only the misleading
  advice, since `WalkStmt` descends into a deref-assignment's operand rather than the
  `DerefExpr`, so it never reached the typechecker's default arm at all.

  This surface is much further along than the arena one: `^T` is a real type
  (`types.RawPointerType` unifies, substitutes and heads; a newtype may wrap one; it
  is a legal array element), the grammar and collector build every node, and E011's
  policy checker — a raw-pointer op or a call to an `unsafe` function needs an
  enclosing `unsafe` block or function, and unsafe-ness does **not** leak across a
  lambda boundary — is correct and has ten passing tests. Only the two ends are
  missing: nothing infers these expressions and nothing lowers them.

  So all four forms (`&x`, `p^`, `p^ = v`, `unsafe { … }`) are refused at the
  expression, in the register of "not implemented" rather than an internal-sounding
  "unknown expression type", and **E011 is no longer reported** (`driver.go` keeps the
  call site as a comment): its policy is right for the day pointers work, and until
  then it can only send a reader somewhere that does not exist. The checker and its
  tests stay, exercised directly rather than through the driver. A `^T` *annotation*
  still resolves — only the operations are gone — which is why the diagnostic names
  them rather than the type. **No soundness hole here**, unlike the arena discharge:
  all four standalone passes that special-case `UnsafeBlockExpr` descend into the
  body rather than skipping it. One test inverted: `ptr^ = 42` asserted *no errors*,
  which was the phantom in miniature. See COMPLETED.md.

- **[DONE 08/13] The `with`-arena phantom is closed** (`lyra-E050`). Arenas were
  designed early — grammar, collector, a reserved runtime shim (`lyra_arena_alloc`),
  the `PinnedRC` box sentinel — and never implemented. Three findings in one, and the
  middle one is why this outranked "inert surface":

  1. **Nothing type-checked the arena expression.** `checkNode` had no `WithStmt` arm,
     so `with a = 42` was as acceptable as `with a = Arena.new(1024)` — and the
     canonical spelling turns out to be doubly unreachable (no `Arena` type is
     declared anywhere, and `Type.method(…)` has been `lyra-E035` since 08/06). The
     phantom's own documented syntax could not have worked; nothing said so because
     nothing looked.
  2. **The purity pass *discharged* every allocation lexically inside a `with` body**,
     so wrapping a `shared` construction in `with a = 42 { … }` silently switched
     `noalloc` off — into an allocator that does not exist. A bound that quietly stops
     binding is worse than no bound (the `slice` and closure findings' lesson), and
     this was the statement's only observable effect.
  3. **Nothing lowered it** — `llvm: block statement lowering not implemented`.

  `with` is now refused at the statement (the E035 precedent: one diagnostic at the
  source, and a construct that cannot mean anything is refused where it is written),
  the discharge is deleted, and the arena expression and body are both checked.

  **The body check needed `WithStmt.Body` to become a `*BlockExpr`**, and that was the
  hole's second layer: held by value, its address was never the pointer the ScopeTable
  keyed the body's scope under, so checking it reported every name declared inside as
  undefined — and so nothing checked it. But allocation is a use-site property the
  *typechecker records*, so an unchecked body is one whose `shared` constructions
  `noalloc` cannot see: deleting the discharge alone left `noalloc` still blind inside
  a `with` block. Every other statement holding a block already used a pointer. If
  arenas are ever built, the discharge comes back **with an escape analysis** — the old
  note already conceded a `shared` value built inside a block and returned out
  escapes, and "everything lexically inside" was the approximation standing in for
  that analysis. See COMPLETED.md.

- **[DONE 08/13] `??` lowers.** It had type-checked and failed to lower since it was
  collected — the 07/30 `?` shape, found by the second audit sweep. It is `?`'s
  value-position sibling and lowers as the same match in disguise
  (`match a { Some(v) => v, None => b }`, `null_coalescing.go`), with everything that
  made `?` hard removed: nothing leaves the expression, so no rebuild at another type,
  no early return, both arms into one phi. The default is **lazy** — an arm, so
  `m ?? panic("missing")` diverges only on the None path. Ownership follows the match
  rules (scrutinee borrowed, default arm conditional and coerced to owned, merged value
  a uniformly-owned temp); the Some payload has no node of its own to mark, so its +1
  is emitted in the lowering directly, `?`'s failure-rewrap arrangement. The
  typechecker also propagates the unified type onto an untyped default (the phi needs
  the arms to agree: `?? 7` on a `Maybe<u8>` lowers at u8) and range-checks it
  (`?? 300` is refused — the 08/13 literal rule). A non-Maybe left operand is a hard
  error (`lyra-E049`; it had warned as lyra-W007 since the operator landed, and became
  an error the same day the operator started lowering): the `??` can never fire, so
  the default is dead code that reads as a handled case, and a construct that cannot
  mean anything is refused where it is written — the E034/E035 reasoning. The backend
  keeps its own loud refusal as a broken-guarantee defense. ASan on both paths, macOS
  and Linux. See COMPLETED.md.

- **[DONE 08/13] A pattern literal is value-checked against the type it is compared
  to** (`lyra-E048`), and a return-position literal joined the decl sites — the
  truncation family the second audit sweep found, and its worst member was a
  miscompile rather than a dead arm: patterns lower at the scrutinee's width, so
  `match x { 300 => … }` on a u8 **matched 44**, `-1` matched 255 (the negative-
  indexing bug's spirit reborn in pattern position, hours after it was removed from
  indexing), `Some(300)` on a `Maybe<u8>` matched `Some(44)`, and range bounds were
  equally unchecked. Everything in these positions is a compile-time constant by
  grammar, so the standing ladder collapses to its first rung: provable → compile
  error, no runtime half. Rust draws the same line ("range endpoint is out of range").

  The walk (`pattern_literals.go`) is a **conservative mirror** of
  `walkDestructuredPattern`'s pairing — it cannot live inside that walk because
  `withPatternBindings` runs it with errors *discarded* — recursing through binding,
  tuple, array, struct and data-payload patterns to reach every integer literal, and
  checking two flavors at the leaf: base width, and a newtype's range constraint
  (`pattern 200 is outside the range 0..<=100 of Percent`), through the shared
  `intOutsideRangeConstraint` predicate so pattern and expression positions cannot
  disagree about one constraint. An **exclusive range end checks its bound minus
  one** — `0..<256` on a u8 is exactly the full range, as the exhaustiveness
  analysis already reads it, so the one-past-the-end spelling stays legal and
  `0..<257` does not. Two adjacent holes closed in the same change: a **newtype
  scrutinee** matched none of `checkMatchExpr`'s kind branches, so its arms skipped
  *all* match policing (kind dispatch now strips to the base; the nominal branches
  keep the unstripped type, which E041 makes sufficient); and the long-open
  return-position gap below — `() -> u8 => 300` compiled and returned 44 —
  is the same family in expression position, fixed by giving `checkReturnValue` the
  same `checkIntegerLiteralRange` call every decl site makes. One test migrated:
  the trait-method narrowing exec test computed `200 + 100` in a `-> u8` return,
  which is now (correctly) a compile error, so it pins trap-parity with runtime
  operands instead. See COMPLETED.md.

- **[DONE 08/12] `s.len()` is O(1), and `for i, c in s` is the indexed traversal** —
  the audit's last standing tension resolved. The tension was not that len counted
  runes (it must — it agrees with `s[i]` and `for c in s`) but that it counted them
  with a walk, and that the docs *defended* that by endorsing
  `for i in 0..<s.len() { s[i] }` — a loop whose every `s[i]` decodes from the start
  (O(n²)) and whose `len()` calls alone once measured 99.7% of `starts_with`'s cost
  when the prelude fell into exactly this trap.

  Two halves. The **rune count rides the fat pointer** (`{ptr, byte_len, rune_count}`,
  STRING_LAYOUT.md), so `len()` is a field read; construction maintains it
  arithmetically — a literal counts at compile time, `++` adds, `slice` subtracts its
  rune bounds — and only the two byte-sourced producers (read_line, interpolation's
  formatted segments) pay one linear `lyra_utf8_count` pass over bytes they just
  produced. Five construction sites, a count-ledger test
  (`TestExec_StringRuneCountAgreesEverywhere`) that recounts every producer's answer,
  and an IR pin that len() contains no decode loop. Measured: 100k len() calls on an
  18000-rune string, 0 µs. The cost is a 24-byte fat pointer (was 16).

  And the **two-variable `for i, c in s`** — the deferred index/rune pairing — lowers:
  a rune counter beside the byte cursor, one linear walk, the array convention (first
  name the index, second the element). It needed no typechecker change at all
  (`bindForInLoopVars` was already generic; only the backend refused), and it is what
  the docs now hold up where they used to hold up the quadratic loop. `s[i]` in a loop
  remains legal and remains O(i) per access — the docs say so instead of endorsing it.
  See COMPLETED.md.

- **[DONE 08/12] Negative indexing is gone; `from_end(k)` is the end-relative
  accessor.** On strings and arrays alike, 1-based (`from_end(1)` is the last), a
  builtin like the index it mirrors, `pure noalloc` for free. The audit's design
  finding, acted on: an index that underflowed past zero — the most common off-by-one
  there is — got a valid read of the wrong element, which is the silently-wrong answer
  this language's whole thesis exists to rule out. The removal follows the standing
  ladder: a *provable* negative (a literal, a folded constant) is a compile error
  naming the from_end spelling with the right ordinal; a runtime one hits the bounds
  trap, which got *cheaper* — the from-the-end `select` adjustment is gone from all
  three index lowerings, leaving the single unsigned compare. The value-range pass got
  sharper too: any provably-negative index is now definite `lyra-E022`, where
  `[-size, -1]` used to be a valid read it had to let through.

  **The performance defense survives in full**, which is what made the old design
  merely wrong rather than a trade: `from_end` lowers to the exact backward byte walk
  `s[-k]` lowered to (`lyra_str_rune_offset`'s negative branch is now its private
  contract — every surface caller must reject a negative *before* the call, or the
  value would silently mean from-the-end again). Re-measured after the change: 2 µs
  against 6082 µs per 2000 last-rune reads on an ~1800-rune string. `slice`'s negative
  bounds went with it — same complexity class positionally, since slice already walks
  and copies — and `byte_offset`'s negative position is `None`, now *agreeing* with
  `index`'s negative offset instead of standing in recorded tension with it. What has
  no replacement spelling: an index-assignment target (`xs[i] = v` end-relative), which
  is `xs[xs.len() - 1] = v` — O(1) on arrays, the only place it exists. See COMPLETED.md.

- **[DONE 08/12] A `for-in` range can no longer loop forever.** Two silent infinite loops,
  both found by auditing the language against its own trap-on-overflow thesis, and neither
  was in this file — the first lived only in a code comment calling itself "the one edge to
  keep in mind", which is the C attitude toward wrap the language exists to reject.

  **The inclusive end at the counter type's max.** `for i in 0..<=hi` with `hi: u8 = 255`
  incremented 255 to 0 with a plain add and re-entered the range, forever — and a large
  step did the same to an *exclusive* end by leaping it (`0..<250:100` over u8: 200 + 100
  wraps to 44, still under 250). The advance is now guarded: the counter moves only when it
  can move by `step` and stay inside the range, measured as an **unsigned** distance to the
  end (the cond already held, so the raw two's-complement difference is the distance, and a
  signed subtraction could itself overflow across the full domain). The fix is an exit, not
  a trap — visiting the type's own max is what the author asked for and nothing in what
  they wrote overflows. The comprehension had been given exactly this treatment on 08/04
  ("the capacity bounds the loop by construction"); the `for-in` half was never filed.

  **A runtime non-positive step.** `types.InvalidStepReason` refuses a constant zero or
  negative step and only ever sees constants, so `0..<10:n` with `n` computed as 0 compiled
  clean and spun. It now rides the ladder a shift amount rides — provable → compile error,
  otherwise → trap (`lyra: range step must be positive`, exit 101) — and for the same
  recorded reason: the alternative is a silent wrong answer. One deliberate divergence: a
  comprehension answers the same degenerate step with an **empty array**, because its count
  is computed up front and "never advances" has a defined size there. See COMPLETED.md.

- **[DONE 08/12] Builtin arithmetic methods no longer bypass a newtype's nominal
  identity** (`lyra-E043`), and the opt-in path they are refused in favour of actually
  works now. With `newtype Cents = i64` and no impls, `a + b` was refused (correct —
  opting into arithmetic is what an operator impl is for) while `a.wrapping_add(b)`
  compiled, and so did `a.wrapping_add(plain_i64)` — a **mixed** operand, the thing the
  newtype exists to prevent — because the transparency fallback re-tried the builtin at
  the base. The whole overflow-arithmetic family (`wrapping_*`/`saturating_*`/
  `checked_*`) now stops at the wrapper: those methods are the *operators'* escape
  hatches, so reaching them through the fallback handed out exactly the arithmetic the
  operator rule withholds. Transparency itself is untouched — `len`/`slice`/`trim` on a
  wrapped string, `floor` on a wrapped float (a conversion's alternative, not an
  operator's) — and it reversed a recorded decision, the `checked_*` test that argued
  "a wrapped integer you cannot do arithmetic on is not a trade anyone would take"
  without noticing the operators were already making that trade.

  **The sharper option won because the other one dissolved on contact with the
  assignability rules.** "Require the argument to match the receiver's newtype" cannot
  be enforced: base → newtype is assignable *by construction* (assignable.go's own
  comment), so a `Cents` parameter accepts a plain `i64` everywhere in the language,
  and strictness only here would disagree with every other parameter.

  **Found on the way: `impl Add for Cents` was silently inert.** The E043 message names
  an operator impl as the opt-in, and it did not work — the dispatch guard
  newtype-stripped its receiver before refusing scalars, so a scalar newtype was
  operator-dead from *both* sides (the numeric rule refused the nominal type, the guard
  refused the base). The guard now tests the receiver unstripped; `impl Add for i64`
  stays inert, which is what the guard is for. Two `Cents`-annotated bindings now add,
  and chain — `x + y + x` needs the result to be a `Cents`, which is what pins it. See
  COMPLETED.md.

  Note the operator section's example is spelled `Cents(150) + Cents(275)`, which is
  **not** how a newtype is written: that section is about a *parse* (a constructor call
  as a math operand) and its test uses a `data` type. A newtype has no constructor —
  `lyra-E044` now says so rather than reporting "not a tuple type" — which is the
  question below.

- **[DONE 08/12] A newtype means something at a boundary**, in three parts. A
  constraint is now checked **wherever the type flows** rather than only at a binding —
  argument, return and array-element positions were silently unchecked — and
  `values(...)` is enforced at all for the first time (`lyra-E045`). A newtype has a
  **constructor**: `Cents(150)`, and `Cents 150` free with it, lowering to its operand
  and nothing else. And a **typed value now needs that constructor** (`lyra-E046`)
  while an untyped literal still converts implicitly — Ada's rule, because a literal
  has no unit yet and a typed value's provenance is where a unit mixup lives. So
  `take(plain_i64)` against `(c: Cents)` is an error, `let xs: []Percent = [10, 20]`
  still is not, and E043 became a case of a general rule instead of a lone patch at one
  boundary. See COMPLETED.md, including the two bugs building it turned up — the check
  reading a from-type the annotation had already overwritten, and the value-range pass
  losing sight of a violation written through the constructor.

  - **[DONE 08/12] The read-out direction is explicit too** (`lyra-E047`). Conversions
    look through a newtype on their operand, so `i64(c)` reads a `Cents` out (and
    `u8(cents)` behaves exactly as `u8(plain_i64)`); `string(...)`/`bool(...)` exist
    as identity-only targets so string and bool bases have the spelling; and the
    implicit form is refused wherever the base is *nameable* — `f(cents)` against
    `(x: i64)` was the same silent unit-discard E046 closed inbound, and Ada requires
    `Integer(M)` here too. A base the conversion cannot name (an array, a function
    type) keeps its implicit read-out rather than becoming write-only — pinned as the
    documented limit. Three passes needed to learn the transparent forms are their
    operand: the ownership pass (the ASan conservation test caught `string(e)`
    binding a box with neither retain nor matching release the day the spelling
    arrived), the value-range pass (the constructor, in the previous change), and
    purity — whose conversion list had **already drifted**: it was missing `rune`, so
    `rune(n)` in `pure` code was charged the unresolved-callee default. The four
    copies of "is this callee a conversion?" are now one, `types.ConversionTargetName`.
    See COMPLETED.md.
  - **[DONE 08/12] A generic newtype constructs by call.** `Boxed(5)` is `Boxed<i64>`,
    solved from the operand through the named-tuple solver (`solveDataTypeVars`, with
    the base as the one declared field), and `Boxed::<u8>(200)` binds the parameters
    explicitly — the same turbofish/solve ladder a named tuple's instantiation takes.
    The bound set resolves through the same expansion the annotation form uses, so
    everything downstream sees the substituted ConstrainedType it already knew, the
    solved result is fully nominal (E047 fires on its implicit read-out), and the
    backend needed nothing. What E044 still refuses here is a parameter the operand
    cannot solve (`newtype Weird<t> = i64` — only the turbofish can bind a parameter
    the base never mentions), naming that spelling. The refusal this replaces had
    called itself "cannot be constructed by call *yet*" — a missing solver, not a
    missing answer.

  Still open beneath it, smaller than it was: `println(c)` refuses a newtype over a
  printable scalar, so transparency covers arithmetic-adjacent methods and not the one
  harmless thing a wrapper's user asks for daily. A `show` impl per newtype works
  today; whether print should reach the base formatter uninvited is a design question
  (it is the one place transparency would *erase* the name the newtype exists to carry).

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

  - **[DONE 08/13] A return-position literal is range-checked.** `() -> u8 => 300` and
    `return 300` were accepted (and returned 44) — true of *every* return position,
    including the ones that were always checked, so it was a gap in `checkReturnValue`
    rather than in the routing. Fixed with the pattern-literal family (the E048 entry
    in Known bugs): `checkReturnValue` now makes the same `checkIntegerLiteralRange`
    call every decl site makes.


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
  identical. `noalloc` charges what ships (08/12): a **capturing** closure construction
  allocates its environment box and is refused, a capture-free one is a pinned static and
  is free — true under both tiers, so LSS's arrival can only *loosen* the rule (a
  non-escaping capturing closure could become free), never tighten it. The old doctrine —
  "`noalloc` is defined against the *release* lowering" — deferred the charge to a
  compiler that did not exist, and is retired. Hot-reload note:
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
instead of racing past the allocation. (The note that stood here — "a backwards `for-in`
range still loops forever" — went stale the day descending operators landed and was verified
false 08/12: `5..<1` runs zero times in `for-in` too, since direction is the operator's and
the loop predicate matches it. The `for-in` runaways that *were* real — the inclusive end at
the counter type's max, and a runtime non-positive step — are fixed as of 08/12; see Known
bugs. The one residual divergence is deliberate: a runtime non-positive step is an empty
array here and a trap in `for-in`, because a count computed up front gives "never advances"
a defined size.)

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
- **[DONE 08/12] Closures are charged, by capture.** The audit's last finding: a
  `noalloc` function containing a capturing lambda checked clean while its emitted body
  called `lyra_rc_alloc` for the environment on **every invocation** — the `slice` hole
  of 08/06 again, a bound that silently stopped binding, defended by defining `noalloc`
  against a release lowering (LSS) that is not built. The charge is now exact rather
  than conservative: a nested lambda that **captures** heap-boxes its environment per
  construction and is refused (`lyra-E016` names it — "a closure captures its
  environment into a heap box"), while a capture-free one is the shared pinned static
  (`emptyEnv`, the string-literal device) and stays free — under the dev lowering *and*
  under LSS, so the exemption is not a bet on the release tier. Receiving and calling a
  callback parameter stays free (the prelude's combinators live on that shape), a
  top-level function passed by name is capture-free, and the charge travels through
  inference, so an unannotated closure-maker refuses its `noalloc` callers. The captures
  pass moved ahead of purity in the driver to answer the capture question; the residual
  LSS decision is only ever a **loosening** (a non-escaping capturing closure could
  become free), which is the compatible direction. See COMPLETED.md.

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
  says false; the supertrait is enforced (`lyra-E040`, 08/07) and its methods are reachable
  through a bound (08/14), so that mechanism is no longer the open question here.
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
  - **[DONE 08/14] A supertrait's methods are in scope through a subtrait bound.** The
    bound had been *enforced* since 08/07 — `impl Nd for T` requires `impl Len for T` —
    and then `where t: Nd` still could not call `t.l()`: *"type parameter t has no method
    `l`; add a `where t: Trait` bound whose trait declares it"*. So a supertrait promised
    a thing exists and gave the one place that needs it no way to reach it, which is the
    half of the feature people actually write supertraits for.

    Fixed as this entry predicted: `closeOverSupertraits` (`typechecker_trait_dispatch.go`)
    takes the transitive closure at the **two** points a bound set enters scope —
    `pushGenericBounds` for a binding, `checkTraitImpl` for an impl — and all four readers
    (bound dispatch, the generic-argument check, operator overloading, `Show`) got it for
    free, which is hazard 8's shape. Two write sites rather than the one this entry
    guessed, and they are twins by `pushGenericBounds`'s own comment: a bound reaching
    `A`'s methods when written on a binding and not on an impl would mean different things
    depending on where it is written.

    Two things the fix had to get right beyond the closure. **Forwarding** is the second
    half — passing a `where u: B` value to a callee bounded `where t: A` — and its old
    diagnostic asked the author to add `where u: A`, a bound `B` already guarantees.
    And the walk carries a **visited set**: `trait A: B` alongside `trait B: A` is legal
    (it means the two are always implemented together, which is exactly what E040 then
    requires of every implementer), so assuming a DAG hangs the typechecker — a failure
    that reads as a frozen editor rather than as a compiler bug.

    Nothing was needed in the backend: dispatch publishes candidates for the trait that
    *declares* the method, so a supertrait call resolves to that trait's impls like any
    other. `TestExec_BoundDispatchReachesASupertraitMethod` pins it anyway — "resolves
    abstractly" and "calls the right function" are different claims.

    Found 08/09 while weighing a `Length` trait beside `Needle`. It is the reason
    `trait Needle: Length` would not have helped: the bound would be enforced and
    `split` still could not call the method. That option is now open.
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
  above. The mechanism is real as of 08/14 (enforced 08/07, reachable through a bound
  today); this stays a decision about `Ord`, not a gap in supertraits.

### [DONE 08/14] A trait with no methods of its own parses

`trait Arithmetic: Add + Sub + Mul + Div` — with or without a `{}` — and the
`impl Arithmetic for Vec2 {}` beside it. `memberList` is built on `commaSep1`, whose
non-emptiness was deliberate; its comment said so in as many words: *"which is what makes
`trait C {}` a syntax error rather than a trait with no methods"*. Right when a method-less
trait meant nothing, and supertraits are exactly what stopped that being true.

The fix makes the **body** optional (braces and all), not the member list: the list is
absent rather than empty, so `trait C { , }` is still an error. `impl_methods` had been
optional the whole time, so only the declaration was unwritable.

Of the three questions this entry raised, the code answered the first: **the bodiless form
is the one to support**, because it is what an author writes when there is no body to
delimit. Rust's mandatory `{}` is an artifact of having no statement terminator to end the
declaration; Lyra has one. Both spellings work.

**The ambiguity that creates, and why it is safe.** With the body optional, a `{` on the
next line could be absorbed as the trait's body. It is not — the terminator ends the
declaration first, so `trait Marker` ⏎ `{ 1 }` is a trait plus a block statement. This is
the same hazard that stopped the `for` condition taking `$.expression`; there the block was
genuinely ambiguous, here the terminator settles it. Pinned by `A brace on the next line is
not a trait body`.

Cost: 7,786 → 7,825 states (+0.5%), `parser.c` +41 KB.

The collector needed one change and it is the interesting one: `MustField` → `cst.Field`
plus a nil check, so an absent list is an **empty method list rather than a dropped
declaration**. MustField returns nil, which would erase the trait and then report
`unknown trait` at every impl of it — a diagnostic pointing everywhere except at the
declaration that caused it.

### [OPEN] Fixed-point types — `fixed<I, F>` is refused, not built

Refused as unimplemented since 08/14 (`lyra-E055`, reported in `parseFixedPointType`).
The annotation parses and collects into a real `types.FixedPointType`, and nothing after
the collector knows what one is — so the type was **uninhabitable**: `1`, `1.5`,
`f64(1.5)` and `i32(1)` are all refused, and no spelling constructs a value.

What an author met was a plain type error — *"cannot assign integer literal to
`fixed<16,16>`"* — which reads as a fixable mistake and invites three more attempts,
each answered by the same sentence with one noun changed. That is the lyra-E035/E052
situation exactly, and the diagnostic is the same answer: say the construct is
unimplemented rather than leaving it to be inferred from what fails.

Kept rather than deleted, because the intent is to build it. Two things to settle first:

- **What it is for.** The three motivations want different types. *Determinism* (lockstep
  simulation, replays, reproducibility) wants exact binary scaling, which is what
  `fixed<I, F>` already commits to. *Money* wants **decimal** scaling and is better served
  by what already exists — `newtype Cents = i64` with a range constraint. *No-FPU targets*
  are irrelevant until there is an embedded target. The grammar has already chosen; worth
  confirming that is the intent before building on it.
- **What arithmetic does to the parameters.** `fixed<16,16> * fixed<16,16>` naturally wants
  `fixed<32,32>`. A static array is already a value-parameterized type, so that much has
  precedent — but an array's size never changes under an operator, and this would be the
  first type whose parameters are themselves arithmetic. Division's rounding is the same
  question from the other side.

The surface is wide even after that: literals (there is no way to *write* one), defaulting,
conversions both ways, comparison, `Show`, `%`, and the value-range pass. The natural
driver is the mandelbrot viewer past ~1e15 zoom, where f64's mantissa runs out — but the
usual answer at that depth is arbitrary precision rather than a fixed width, so let the
program ask before assuming this is what it wants.

When it is built, `fixed_point_type` also needs adding to **both** highlight query files
(`tree-sitter-lyra/queries/highlights.scm` and `lyra-zed-ext/languages/lyra/highlights.scm`)
— it is missing from both today, which is harmless only because nobody can write one.

- **[DECIDED 08/14] An umbrella impl stays required — and loses its braces.**
  `impl Arithmetic for Vec2` is the spelling now; requiring it at all is the decision.

  **The compiler cannot tell a conjunction from a promise.**
  `trait Arithmetic: Add + Sub + Mul + Div` is "these four and nothing more";
  `trait Currency: Add` is "this is money, which also adds". Structurally identical — no
  methods, some supertraits — and auto-satisfaction makes the first convenient and the
  second *wrong*, since every `Add` type would silently become a `Currency`. No rule keyed
  on shape gets both right, and only the author knows which they wrote.

  **Nobody is ever blocked**, which is what would have forced the other answer. There is no
  orphan rule, so a library shipping `Vec2` with the four impls and no umbrella costs you
  one line rather than locking you out of your own bound.

  **The impl is a checked assertion rather than ceremony**: it claims "Vec2 is fully
  arithmetic" and lyra-E040 verifies it at that line — the same category as an explicit
  annotation on a binding whose type could have been inferred.

  One argument for it got *weaker* the same day and should not be leaned on: "the error
  lands at a declaration rather than at every use site" was true when a use-site failure
  named only the outer type, and `typeImplementsTraitWhy` now reports
  *"Vec2 does not implement Arithmetic — Vec2 does not implement Mul"*.

  **What would reopen it**: umbrella traits multiplying, so the cost stops being one line
  per type. The fix then is not auto-satisfaction but an opt-in *at the trait* —
  `@bundle trait Arithmetic: …`, on the existing `@builtin(...)` mechanism — which puts the
  declaration where the knowledge is instead of asking every implementer to have it.

### [DONE 08/14] A bound is satisfied by an impl whose own `where` clause fails

Fixed as this entry proposed: `typeImplementsTrait` now verifies a matched impl's own
constraints against the bindings the match produced, carrying the set of goals already
being proved so a chain that leads back to its own goal answers *no* rather than looping.
Only that branch dies — the impl loop continues, so a goal provable another way still is.

Two details the entry did not anticipate. **A binding that is itself a type variable is
skipped rather than failed**: `impl Arith for Box<t> where t: Arith` has its supertrait
obligation checked with `t` still abstract, and answering "t does not implement Add" there
would make every constrained generic impl an error — whether `t` holds is the enclosing
declaration's question, which is checkGenericBounds' own stated rule. And the recursion
**terminates on its own** in practice, since each step strips a layer of type argument
(`Box<Box<i64>>` → `Box<i64>` → `i64`); the in-progress set is a guard against the case
that does not, not the mechanism that makes nesting work.

The diagnostic names the inner failure, as the entry asked: *"u is instantiated at
`Box<string>`, which does not implement Arith — string does not implement Arith"*.

**It does not subsume the umbrella-impl question**, the other half of what this entry
guessed. Requiring `impl Arithmetic for Vec2 {}` is still where E040 fires, and the bound
check verifies constraints rather than supplying the impl.

The original report follows.

`typeImplementsTrait` matched an impl by its target's *head* and never verified the impl's
own bounds, so a generic impl satisfied a bound for **every** instantiation of its target —
including ones it explicitly excludes:

```lyra
impl Arithmetic for Complex<t> where t: Arithmetic {}

let twice<u> where u: Arithmetic = (v: u) -> u => v + v
twice(Complex { re: "a", im: "b" })   // type-checks clean
// → lyrac: llvm backend: llvm: type not found for *ast.IdentifierExpr
```

`Complex<string>` satisfies `where u: Arithmetic`, and the failure lands in the backend as
an internal message — hazard 5 inverted, the `Rng.seeded(42)` shape.

**The limit is deliberate and documented**, in `typeImplementsTrait`'s own comment: *"a
single level: the matched impl's own `where` bounds are not recursively verified here (a
deliberate first-cut limit — the recursive obligation surfaces when that impl is itself
dispatched)."* What that reasoning misses is the case where the impl **is** its constraint:
an umbrella impl has no methods, so there is no later dispatch to surface the obligation,
and the bound check is the only place the question is ever asked. `std/math/complex.lyra`
is the first code to write one, which is what made a documented limit into a reachable bug.

The fix is recursion with a visited set — checking a matched impl's constraints against the
bindings that matched it, the way `checkImplConstraints` already does at dispatch. The
visited set is not optional: `impl Arithmetic for Complex<t> where t: Arithmetic` is
satisfied *by itself* for `Complex<Complex<f64>>`, so a naive recursion does not terminate.

Two things to decide with it: whether the diagnostic names the failing inner bound (it
should — "Complex<string> does not implement Arithmetic" is useless without "because
`string` does not") and whether this subsumes the umbrella-impl question above, since a
bound that verifies constraints is most of what requiring the impl was buying.

### [DONE 08/14] An out-of-range float→int conversion is silently wrong

Fixed the same day it was found. `guardFloatToInt` (`pkg/backend/llvm/rounding.go`) traps
unless the rounded value is in `[-2^63, 2^63)`, which is emitted before every `fptosi`.

Three details worth keeping:

- **The upper bound is exclusive, and that is not a slip.** i64's minimum is exactly
  -2^63 and representable as a float; its *maximum* is 2^63-1, which is **not**
  representable in binary64 — the nearest float above it is 2^63 itself. An inclusive
  check against a float spelled `9223372036854775807` would compare against 2^63 and admit
  a value one past the end.
- **A NaN traps for free.** The check reads "trap unless in range" with *ordered*
  comparisons, which are false for a NaN, so it takes the trap edge with no test of its
  own. Written the other way round — trap *if* out of range, with unordered compares — a
  NaN would slip through to a conversion that is poison for it too.
- **Trapping rather than saturating**, of the two defensible answers (Rust's `as`
  saturates). The rest of this language's numeric ladder traps, and a saturated coordinate
  quietly rendering the wrong pixel is the failure the fix exists to prevent.

Of the three questions this entry raised, the third is still open: the value-range pass
does **not** elide the check, so a float bounded by construction still pays a compare and
a branch. Everything the pass already elides (array bounds, division) is integer-valued,
so this needs float range tracking rather than a new consumer of the existing one.

`std/prelude/format.lyra` keeps its own guard, now for the *message* rather than for
safety: `to_fixed` naming the value it could not render beats a generic conversion trap.

The original report follows.

### [WAS OPEN] An out-of-range float→int conversion is silently wrong

`floor`/`ceil`/`round` do not check their result's range, and the answers are not merely
imprecise — they are arbitrary:

```lyra
let huge: f64 = 1.0e20
let n: i64 = huge.floor()     // 0
let neg: f64 = -1.0e20
let m: i64 = neg.floor()      // 0
```

Zero, for both. That is LLVM's `fptosi` on an out-of-range operand, which is poison —
so the value is whatever the optimizer leaves behind, and `-O0` and `-O2` are free to
disagree. A NaN converts the same way.

**This is the language's own thesis inverted.** Integer arithmetic traps on overflow, an
array index traps out of bounds, a shift amount traps out of range, and a `newtype`
constraint traps at construction — and then the one conversion that cannot represent its
input quietly answers 0. Found 08/14 writing `to_fixed`, whose first draft printed
`9223372036854775807.9223372036854775807` for `1.0e20` and looked plausible doing it.

`std/prelude/format.lyra` guards its own call explicitly, which is the workaround and not
the fix: every other caller of `floor`/`ceil`/`round` is still exposed.

The fix is a range check before the conversion, trapping like the rest of the ladder — a
compare against the target width's bounds plus a NaN test, which is what Rust's
`as`-with-saturation and Zig's `@intFromFloat` both spend. Three things to decide with it:

- **Trap or saturate.** Rust saturates (`as` is total), Zig traps in safe builds. This
  language traps everywhere else, so trapping is the consistent answer — but saturation is
  defensible for a *rendering* path, which is where the need showed up.
- **Where the check goes.** In the backend's conversion lowering, so it covers every
  caller — not in the prelude, where only the callers who thought about it are covered.
- **Whether the value-range pass can elide it.** A float bounded by construction (a
  normalized coordinate, a clamped value) needs no check, and the pass already elides
  provable array-bounds and division checks.

### [OPEN] Frame-buffer ergonomics — what the measurements left standing

Measured 08/14 against a 200x60 escape-time render (12,000 pixels), a terminal viewer at a
realistic size. Both strategies are far inside a frame budget, so **nothing here blocks the
terminal version**; these are what a graphical one will hit.

| building a frame | 12,000 pixels |
|---|---|
| a row per `++` per pixel | 1440 µs |
| a preallocated `[]u32`, index-assigned | 606 µs |

The delta is the string building: about 70 ns per pixel, which is 0.8 ms at 200x60 and
~34 ms at 800x600 — the point where it stops being free.

**Two of the three gaps behind that are now closed** (see COMPLETED.md): `[v; n]` is
emitted as a loop above 64 elements rather than unrolling into n stores, and a **runtime
count** builds a dynamic array, so a buffer sized by a window resize needs no `push` loop.

What is left:

- **A string can only be built with `++` or interpolation**, and `join` (08/14) is an
  ergonomic answer rather than a performance one — it is the same quadratic, since each
  `++` copies everything accumulated and the language has no way to allocate a string of a
  known size and fill it. Measured at parity with a hand-written loop (323 µs against
  347 µs over 600 parts), which is the point: it saves the loop, not the copying.

  A **linear** join needs a primitive the language does not have, and the useful framing is
  not "add a string builder" — it is **should a string have a byte seam**.

  A builder beats `++` because it is mutable and *over-allocates*: spare capacity, doubling
  when full, one copy at the end. Lyra already has that half — `push` is amortized doubling
  and measures dead linear (80,000 in 61 µs). What is missing is the other half: the
  byte-level surface is `byte_len`, `byte_offset`, `compare_bytes`, `compare_bytes_at`, all
  read-only *comparisons*, and `to_runes` has no inverse. Everything goes string → data and
  nothing comes back, so you can build a `[]u8` efficiently and then have no way to call it
  a string.

  With a seam (`s.bytes()` and `[]u8 → string`, say), **both** a builder and a linear join
  are ordinary Lyra in the prelude rather than language features — which is the
  `starts_with` precedent exactly: quadratic in its natural form, fixed by adding
  `byte_len`/`compare_bytes_at` and rewriting it here (19.9 ms → 19 µs). The narrower
  alternative is `join` as a builtin that sums lengths, allocates once and memcpys; the
  heaviest is a `StringBuilder` type, which is a new nominal type for something `[]u8`
  already is.

  **Deferred on measurement, not on taste.** The quadratic is invisible below ~8,000
  characters in one string (1,332 µs) and hurts at 16,000 (4,985 µs). A terminal frame is
  ~12,000 characters but built as 60 rows of 200, and renders in 1,440 µs with plain `++`.
  What would cross the line is a *single* string that large — a whole frame joined and
  written at once, or a large buffer dumped to a file. If the viewer starts doing that, add
  the seam; `join` gets faster the same day for free.
- **The fixed-size `[v; n]` path still emits one `insertvalue` per element**, so a
  `[20000]u32` literal is 1.16 MB of IR. Bounded by a different problem arriving first — an
  array that size is 80 KB of stack — but it wants the same treatment eventually, which
  means an alloca and a store loop, changing the value from an SSA aggregate to a loaded
  one.

### [OPEN] A `[]t` combinator is unreachable from an array literal

`[1, 2, 3].map(f)` and `["a", "b"].join("")` are both errors: an array literal infers a
**fixed** `[3]T`, every prelude combinator takes `[]T`, and UFCS does not widen the
receiver. The workaround is an annotation — `let xs: []i64 = [1, 2, 3]`.

**The diagnostic half is done** (08/14): both shapes now name the edit rather than only the
mismatch — *"map takes a dynamic array — annotate the value as `[]i64` (a `[3]T` literal is
a fixed array, and widening it would allocate)"*. The suggested type is *defaulted* before
it is named, since an unannotated literal's elements render as "integer literal", a phrase
rather than a type.

**The rule itself stays, and auto-widening is the fix not to reach for**: a `[N]T` is a
stack value and a `[]T` is a heap box, so widening at a call allocates. The language's
position is that you get a heap array when you *ask* for one, and a UFCS call that
allocated silently would be invisible to `noalloc` exactly where a reader looks for it.

What is left is the ergonomic half, and it has one honest option:

- **Declare each combinator for `[N]t` as well.** Receiver-keyed overloading exists for
  precisely this and the heads differ, so it is legal — at the cost of a second copy of
  every body, which is the duplication this project keeps deleting. Worth it only if the
  annotation turns out to be a recurring irritation rather than a one-time surprise, which
  the mandelbrot program is the right thing to decide.

### [OPEN] `[v; n]` aliases a mutable element, silently

`[[' '; WIDTH]; HEIGHT]` builds **one** row referenced HEIGHT times, so every
`grid[py][px] = …` writes to the same place and every row prints identically. Found 08/14
in `examples/mandelbrot.lyra`, where it was the third of three bugs and the one that
survived fixing the other two — the image stayed uniform, which reads as an arithmetic
mistake rather than an aliasing one.

**The semantics are correct and deliberate**, and are not what should change: `[v; n]`
evaluates `v` **once**, and each of the n slots is an owner of that value. That is what
makes `[expensive(); 1000]` one call and `[0; 480000]` a loop rather than 480,000
evaluations. Changing it would be strictly worse.

What is missing is that nothing says so at the one moment it bites. The failure is silent —
a plausible image, not an error — and "n copies" reads as "n *independent* copies" to
almost everyone.

**The trigger is narrower than "managed", and measured** (08/14):

| element | `[v; n]` then mutate slot 0 | |
|---|---|---|
| `[]i64` | `buckets[0].push(7)` → all three length 1 | **aliases observably** |
| `string` | `words[0] = "bye"` → only slot 0 changes | safe: immutable, nothing to mutate *through* |
| `i64` | copied | safe |

So the predicate is **"can the element's contents be mutated through a reference"** — an
array, or a `shared` aggregate with mutable fields. A string is managed and immutable, so
its aliasing is unobservable; a scalar is copied. Warning on "managed" would fire on
`["hi"; 3]`, which is correct code and by far the commoner spelling.

The proposed diagnostic names the fix, on the pattern lyra-E046 and the array-literal hint
follow: build a fresh value per iteration —

```lyra
var plot: [][]rune = []
for py in 0..<HEIGHT {
  var row: []rune = [' '; WIDTH]   // evaluated once per row, not once in total
  …
  plot.push(row)
}
```

Two things to decide: whether it is a warning or an error (it is *correct* code, so a
warning — but a warning nobody reads is worth less than the trap it prevents), and whether
a deliberate alias wants an opt-out spelling, which it would need if this were ever an
error.

### [DONE 08/15] A terminal UI needs three builtins, not a library

`set_raw_mode(on)`, `read_key() -> Maybe<rune>` and `terminal_size() -> (i64, i64)`
landed 08/15 (`pkg/backend/llvm/tui.go`), and `std.tui` on top of them the same day —
`event.lyra` (the `Event` type and decoder), `key.lyra` (named keys), `mouse.lyra` (SGR
reporting), `screen.lyra` (alternate buffer, clear, cursor) and `style.lyra` (256-colour,
attributes). **The mouse needed no builtin either**: a terminal reports clicks as escape
sequences on stdin, so it is `print` to enable and `read_key` to receive. See
COMPLETED.md, including why SGR mode is the only encoding a rune-oriented `read_key` can
carry.

### [DONE 08/17] A timed `read_key`, for the one key three builtins cannot resolve

`wait_for_key_ms(timeout: i64) -> bool` landed 08/17 — a fourth terminal builtin, and
**not** the `read_key_timeout(ms) -> Maybe<rune>` this entry proposed. See COMPLETED.md
for why splitting the question beats a timed read: three outcomes (a key, nothing yet,
input ended) do not fit two answers, and a `Maybe` has to conflate two of them.

Both motivating cases are closed. A lone Escape is reported immediately rather than one
keypress late, and `tui_viewer.lyra` redraws on a window resize with no key pressed
(measured: 40x12 to 60x20, no input sent).

### [OPEN] `std.tui` above the decoder: frames, boxes, a status bar

What the original entry listed and is still unwritten: **frame diffing** (redrawing only
the cells that changed, which is what makes a full-screen redraw not flicker), box
drawing, and a status bar. None of it needs anything from the compiler.

Frame diffing is the one with a measurement behind it already: `todo.md`'s rendering
section found that a terminal frame is better assembled into one string and printed once
than positioned and printed per cell, so a diff wants to emit runs rather than cells.

### [OPEN] Inference should not depend on declaration order

`let (w, h) = contain_set(cols, rows)` fails when `contain_set` is declared **later** in
the file and its return type is **inferred** rather than annotated: destructuring needs
the element types where the pattern is walked, and the callee's return type has not been
inferred yet, so the value's type is nil and the pattern binds nothing.

`lyra-E058` names the fix as of 08/17 (add a return annotation), which turns a mystery
into a one-line change — before it, the only symptom was `undefined identifier` at every
*use* of the names, pointing at the line after the destructure while the cause was a
missing `->` fifty lines down. **The diagnostic is the workaround, not the fix.**

The gap is narrow and the boundary is known: a scalar return is fine, and binding the
whole tuple is fine — both defer the type. Destructuring is the one position that needs
it immediately. So the fix is to infer a callee's return type **on demand** when a
destructure asks for it, rather than relying on file order.

Two things to settle when doing it: mutual recursion between two un-annotated functions
needs a cycle guard (the same shape `resolveType`'s `resolvingTypes` guard has), and
on-demand inference must not double-report diagnostics from a body that is later checked
normally.

**Worth doing because the house style causes it.** Helpers-below-main is exactly the
arrangement that puts an un-annotated helper after its caller, so this is reachable by
writing ordinary code in the documented style, not by doing anything unusual.

### [OPEN] Return-type-directed dispatch — reopen when `From`/`Into` wants it

**Lyra dispatches on a receiver, and nothing else.** A trait method with no receiver
cannot be resolved: `trait Zero { zero: () -> Self }` with `let n: i64 = Zero::zero()`
reports *"Zero::zero: expected a receiver argument"*, and the annotation sitting right
there does not help — choosing an impl from the *expected* type is a different mechanism.

That is a design statement rather than a gap, and `lyra-E035` already says so from the
other side: the language has no type-namespaced associated functions, which is why the
prelude's constructors are bare (`rng_seeded`, not `Rng.seeded`).

**What it costs, concretely** (08/14): the conventional `Zero`/`One`/`Default` traits are
unwritable, because every one of them is `() -> Self`. The workaround is a receiver —
`Signed`'s `is_negative`/`abs` ask a *value* about itself, which is what `Complex`'s
formatter needed and is arguably clearer. It is a real workaround though: there is no way
to name the additive identity of a generic numeric type, so a generic `sum` has no seed and
must take one.

**The trigger to reopen this is `From`/`Into`, not another numeric trait.** A conversion is
inherently return-type-directed — `let x: Meters = From::from(3.0)` picks the impl by what
is being produced — and there is no receiver formulation that saves it, unlike a sign test.
So the day conversions are wanted is the day the mechanism has to exist, and it brings
`Zero`, `One`, `Default` and `parse::<T>()`-shaped APIs with it.

Three things to settle then, and they are why this is a feature rather than a fix:

- **Where the expected type comes from.** An annotation is easy; an argument position or a
  return position needs inference to flow *inward*, which the checker does for literal
  widths and not for impl selection.
- **What happens when it is absent.** `Zero::zero()` as a statement has no expected type,
  so it is either an error or needs a turbofish (`Zero::<i64>::zero()`), and the grammar
  already has `::<` for exactly this shape.
- **How it interacts with lyra-E035.** Either that diagnostic softens, or associated
  functions stay absent and only *trait* methods gain the mechanism — the narrower
  reading, and probably the right one.

### [DONE 08/14] A generic impl was never selectable as a candidate

**One cause, three symptoms**, and the diagnosis in the original report below was too
narrow: it is not about operators, and not about the bound. The candidate tables are keyed
by each impl's **written** target, which is exactly right for a concrete impl
(`impl Show for i64` keys `i64`, and a specialization looks up `i64`) and can never match
for a generic one — `impl Add for Box<t>` keys the literal string `Box<t>` while the
specialization looks up `Box<i64>`. So a generic impl was unreachable through a bound in
*both* forms, a method call and an operator, and nested besides.

The missing keys are published where a concrete type is first known — the instantiation —
by `publishCandidatesAt`, which is the only place both halves are in hand: the callee's
body holds the bound-dispatched sites, and the instantiation fixes the type. Matching
stays in the typechecker, which is the rule `Resolution` exists to keep.

Four things it needed, each of which looked like the whole fix until the next one appeared:

- **Publish under the monomorphized key too.** A generic struct is monomorphized before
  lowering, so the receiver's type inside a specialization is named `Box$i64`, not
  `Box<i64>`. The mangling is recursive — `Box<Box<i64>>` is `Box$Box_i64`, not
  `Box$Box_i64_` — because the backend substitutes inner arguments *before* naming the
  outer one. `typetable.MonoTypeKey` lives beside `TypeSymbol` and `mangleTypeName` so the
  scheme stays in one place.
- **Resolve against the trait that declares the method, not the bound's.** Under
  `where u: Arithmetic` the umbrella declares nothing and `(_+_)` comes from `Add`; asking
  for it on `Arithmetic` matches no impl, publishes nothing, and looks exactly like no fix.
  Supertraits are what make the two differ.
- **Filter sites by the supertrait closure**, for the same reason.
- **Recurse into the selected impl's body.** A published candidate may itself select a
  generic impl whose body has bound sites one level down — `measure(Box { v: Box { v: 7 } })`
  reaches `impl Depth for Box<t>` at `t = Box<i64>`, whose `self.v.depth()` needs a
  candidate at `Box<i64>`. Both concrete-dispatch paths hook it too (operator and method);
  fixing one leaves the identical failure under the other spelling.

The recursion carries an in-progress set. It terminates on its own for well-founded types,
each step stripping a layer, but a body that reaches itself would otherwise spin — which
reads as a frozen editor, not a crash.

Measured: no meaningful change to `BenchmarkAnalyze_*` (within noise at 3 iterations).

The original report follows.

Found 08/14 while verifying the fix above, and **pre-existing** — that fix only rejects
more, so it cannot push a program into codegen. `std/math/complex.lyra` worked for direct
operator use (`a + b`, `a * b`, `a / b` on a `Complex<f64>` all ran and printed correctly)
and failed in two:

```lyra
let twice<u> where u: Arithmetic = (v: u) -> u => v + v
twice(Complex { re: 1.0, im: 2.0 })      // llvm: type not found for *ast.IdentifierExpr

let n = Complex { re: Complex { re: 1.0, im: 0.0 }, im: Complex { re: 0.0, im: 1.0 } }
n + n                                     // llvm: type not found for *ast.MemberExpr
```

**The dimension is the generic target, not the bound and not the operator.** Both of these
work, so neither mechanism is broken on its own:

- `twice(21.0)` — a primitive through the same bound;
- `twice(Pt { x: 21 })` for a **non-generic** `struct Pt` with the four impls.

So it is monomorphizing an operator impl whose target is generic: through a bound
(instantiating `impl Add for Complex<t>` at `t = f64` selected abstractly), and nested
(instantiating it at `t = Complex<f64>`, whose body's own `+` must then resolve to the same
impl one level down). The second is the more interesting one — it needs the instantiation
set to close over a generic impl reaching itself, which is the ordering `pkg/driver`'s
`instantiations.go` already handles for generic *functions*.

Both are hazard 5 inverted: they type-check clean and fail with an internal message naming
an AST node. Until this is fixed, `Complex<t>` is usable directly and not through a generic
bound, which is worth knowing before the mandelbrot program leans on it.

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
