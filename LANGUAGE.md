# Lyra Language Semantics

The reference for Lyra's semantics as implemented. Compiler internals live in `lyra/CLAUDE.md` and the package `README.md`s; the reasoning behind decisions is in `lyra/COMPLETED.md`.

**Contents**

1. [Types and Literals](#1-types-and-literals) — primitives, literal checking, ranges, newtypes
2. [Operators and Assignment](#2-operators-and-assignment) — bitwise, overflow, compound and tuple assignment, overloading
3. [Collections and Strings](#3-collections-and-strings) — arrays, sorting, strings, HashMap, JSON, lazy sequences
4. [Traits, Generics and Dispatch](#4-traits-generics-and-dispatch) — supertraits, defaults, UFCS, Show, type arguments
5. [Effects](#5-effects)
6. [Modules and Documentation](#6-modules-and-documentation)
7. [I/O and Runtime Builtins](#7-io-and-runtime-builtins) — console, files, args, terminal, randomness, float math, clock
8. [FFI and Unsafe](#8-ffi-and-unsafe) — raw pointers, `nullptr`, unions, aggregates, `@must_release`, `@symbol`

**Builtin vs. prelude rule:** anything expressible in Lyra goes in the prelude (`std/prelude/*.lyra`); a compiler builtin exists only for what is genuinely primitive (libc, argv, libm, allocation).

---

## 1. Types and Literals

### Primitives

| Kind | Types | Notes |
|---|---|---|
| Signed int | `i8` `i16` `i32` `i64` `i128` | no platform `int`/`uint`; untyped int literal defaults to `i64` |
| Unsigned int | `u8` `u16` `u32` `u64` `u128` | |
| Float | `f16` `f32` `f64` | no bare `float`; untyped float literal defaults to `f64` |
| Other | `bool`, `string`, `rune` | `rune` is a Unicode code point (i32) |
| Internal | `never`, `untyped_int`, `untyped_signed_int`, `untyped_float` | no syntax |

- **`never`** is the bottom type, the result of `panic(msg)`: assignable to every type, so `match m { Some(v) => v, None => panic("…") }` works. `panic` is EffectNone (legal in `pure`/`det`/`noalloc`).
- **`i128`/`u128`** lower natively (LLVM `i128`): checked arithmetic, `match`, comparisons, conversions; division and `%%` via compiler-rt; `print` via `lyra_i128_to_str`. A literal's magnitude lives in a `big.Int` on the node (nil if it fits 64 bits) and stays untyped where both could hold it. Folding is arbitrary-precision (`ast.FoldBigExpr`; `FoldIntExpr` returns ok=false rather than wrapping). Value-range analysis leaves 128-bit (and `u64`) values untracked.
- **`==`/`!=` on floats warns** (`lyra-W008`), except a bare name compared with itself: `x != x` is exact and true for NaN alone, so it is the NaN test and draws nothing.
- **An integer literal beside a float is a float**, whether the float is typed (`x < 1`, `x + 1`) or a literal (`5 < 5.0`, `5 == 5.0`, `5 * 2.5`), in every operator; a context still narrows the pair (`let f: f32 = 5 * 2.5`). A *typed* integer against a float is refused (`n < 2.0`; convert with `f64(n)`).
- **Float narrowing is allowed and rounds to nearest** (`f32(x)`), as integer narrowing truncates (`u8(x)`). Refused: a compile-time constant that would become infinity (`f32(1.0e40)`). Precision loss is never an error.

### Struct literals and record update

`P { base | f: v }` is a copy of `base` with the listed fields replaced; `{ base | f: v }` is the anonymous form. **The base is any postfix expression** — a name, a call (`Duration { duration() | days: 1 }`), a field, an index, or a parenthesized expression for anything else; an operator needs those parentheses, since `|` is also bitwise or and only a postfix base keeps the two readings apart. The base must have the literal's type, each update must name one of its fields (no field is added), and a kept managed field is a copy with its own reference. A literal gives each field one value, and a struct pattern names each field once (`lyra-E075`). There is no `...base` spread in a struct literal: `...` is an array literal's element (`lyra-E068`).

### Literals must fit

A literal that cannot hold its value is a compile error **in every position, floats included**: match arm `300` on a `u8`, range-pattern bounds, `Some(300)` for `Maybe<u8>`, a newtype constraint, a return-position `() -> u8 => 300` (`lyra-E048` for patterns). Grace: an exclusive range end is a position, so `0..<256` on `u8` is legal, `0..<257` is not.

### Ranges

| Operator | Direction | End |
|---|---|---|
| `..<` | ascending | exclusive |
| `..<=` | ascending | inclusive |
| `..>` | descending | exclusive |
| `..>=` | descending | inclusive |

- **Direction is the operator's, never the bounds'**: `5..<1` is an empty ascending range.
- Step: `0..<10:2`, a **magnitude**. Negative literal step is an error; a non-positive step known only at run time traps (`lyra: range step must be positive`). A comprehension with a degenerate step yields an empty array.
- `for-in` **terminates at the type's edge**: `0..<=hi` with `hi` at the type max visits max and exits; a large step cannot leap an exclusive end.
- As a match pattern or newtype constraint a range is a set: `..>`/`..>=` there are `lyra-E034`.
- **A range pattern on a `rune` is written in runes** (`'0'..<='9'`), and numeric bounds
  there are refused: `48..<=57` is the same set with its meaning removed, so the scrutinee's
  type decides how its patterns are spelled. An open bound names the type's own edge and so
  has no units to be wrong in.
- **A pattern may name alternatives with `|`** — `1 | 2 | 3`, `"get" | "post"`,
  `'a'..<='f' | 'A'..<='F'` — matching when any of them does, and counting as all of them
  for exhaustiveness (`true | false` covers `bool`). **Literals and ranges only**: an
  alternative that binds raises a rule every language with or-patterns states explicitly
  (Rust requires every alternative to bind the same names at the same types) and nothing has
  needed it, so `Some(x) | None` is a syntax error rather than an undecided meaning. `|` is
  also the bitwise operator; position tells them apart, so `match a | b { 3 | 7 => … }` is a
  bitor scrutinee matched against an alternation.

### Newtypes

`newtype Meters = f64` gives nominal identity to a **structural** base: scalars, `string`, arrays, raw pointers, function types. `lyra-E041` refuses a `struct`, `data`, named tuple or anonymous tuple (suggest `tuple Rgb(u8, u8, u8)`).

- **Constructor:** `Cents(150)` or juxtaposed `Cents 150` (same node); lowers to its operand. Generic: `Boxed(5)` is `Boxed<i64>`, `Boxed::<u8>(200)` binds explicitly. Malformed forms: `lyra-E044`.
- **Into (`lyra-E046`):** an untyped literal converts implicitly (`let c: Cents = 150`, `let xs: []Percent = [10, 20]`); a typed value needs the constructor. An array literal/repeat written in place — or an `if`/`match`/block every branch of which is one — converts implicitly (a typed *binding* holding one does not). A lambda literal converts implicitly to a function-type newtype, including in argument position; its parameters are elaborated from the base and the signature checked. Scalar/string operations (`x + y`, `a ++ b`) still need the constructor.
- **Out (`lyra-E047`):** always explicit — base name where it has one (`i64(c)`, `string(e)`, `bool(f)`; these exist only for this, no stringification/truthiness), else `base(v)`. `base` strips exactly one layer; newtype→newtype has no path. Conversions look through a newtype (`u8(cents)` = `u8(plain_i64)`). `base` is a builtin resolved after scope (a user binding shadows it); later passes recognise it via `TypeTable.IsBaseReadout`, never by spelling.
- **Constraints** (`range(...)`, `step(...)`, `pattern(...)`) are checked wherever the newtype flows and through the constructor (`lyra-E023`). `values(...)` is `lyra-E045`. `step` measures from the range start (`range(5..<=95), step(10)` accepts 15, refuses 10: `lyra-E053`). Unprovable values trap at construction (`lyra: value violates its newtype's constraint`); only unsettled sites pay a compare.
- **Transparent to the base's methods** (builtins and prelude `self:` functions), tried after every other rung, so a newtype's own method wins. A function-type newtype is callable (`h(5)`) in every position.
- **Except** `wrapping_*`/`saturating_*`/`checked_*` (`lyra-E043`): use an operator impl or convert to the base. Float `floor`/`ceil`/`round` stay transparent. `println(c)` is refused — write `impl Show for Cents`.

### Regex literals

`r"…"` is one engine everywhere: a DFA compiled **at compile time** (RE2 discipline, O(n), no backtracking/allocation; flattened tables + one shared driver).

- Used as a newtype `pattern(...)` argument or as a **pattern against a string** at any depth — an arm (`w @ r"^[a-z]+$" => …`), a tuple element, a struct field, an array element, a payload (`Some(r"a.*")`). It matches the whole string. Regex arms never make a match exhaustive.
- `lyra-E054`: lookbehind or DFA past `regex.MaxTableStates`. As a value (`let re = r"…"`) it is `lyra-E052`.
- There is no `regex` type: `(re: regex)` declares a type variable.

### Allocation flavor (`shared`, `stack`, `weak`)

**A flavor is written at the use, never at the declaration.** `shared struct Node { … }`
does not parse. `shared` goes on a field, a parameter, a binding's annotation, an array
element or a type argument — so one `Node` may be shared in one place and plain in another,
and the type itself has no opinion.

The difference is what a second name gets. A plain aggregate is an inline value and
**copies**; a `shared` one is a reference-counted box and **aliases**:

```lyra
var a = Counter { n: 0 }
var b = a                       // a copy: writing through b leaves a at 0
var s: shared Counter = Counter { n: 0 }
var t: shared Counter = s       // the same object: writing through t is visible in s
```

Everything else follows from that:

- **A type that contains itself needs one** — `struct Node { next: Node }` is `lyra-E014`,
  unbounded size — **unless the cycle already passes through an array**, which is a box:
  `struct Tree { children: []Tree }` is fine as written. So `shared` is for a single
  recursive child, not a list of them. `weak T` breaks the size cycle the same way and
  takes only the weak half of the refcount, which is how a cycle avoids leaking.
- **Two names that must see one value need one.** If a write through one has to be visible
  through the other, they have to be the same object.
- **Nothing else does.** A scalar, a small struct, a value with one owner, a value being
  returned — a refcount is a cost, and copying two `i64`s is not.

**Flavor is not part of a type's identity** — the type check ignores it, so `Node` and
`shared Node` unify — and a mismatch is reported by a separate check, `lyra-E018`.

- **A binding inherits, a parameter cannot.** `let p: Node = s` takes a `shared` value and
  unboxes it, because the binding has no representation of its own to conflict with; a
  function is compiled once and its parameter has exactly one, so there is nothing to
  inherit from at a call and a crossing there is refused. **Inheritance runs one way
  today**: `shared` into a plain annotation unboxes, and a plain binding into a `shared`
  annotation does not box — it fails in the backend (todo.md).
- **Two positions stay polymorphic and are never refused**: a **construction written in
  place** (including an `if` or `match` whose arms are all constructions), which has no
  flavor of its own and takes the position's, and a **generic parameter**, which takes
  whatever the instantiation binds. `f(Node { … })` is always legal; `let n = Node { … }`
  followed by `f(n)` may not be.
- **So pick a flavor at each boundary and keep it.** In a recursive tree, once one edge is
  `shared` every reference to that node type wants to be — `examples/collector/ast.lyra`
  writes `shared Expr`, `shared Pattern` and `shared TypeNode` everywhere for that reason,
  and the uniformity was forced by E018 rather than chosen.

Every position that stores a value reports the crossing — an annotated binding, a
reassignment, an interior write, an argument, a return, a struct field, a data
constructor's payload, an array element, a tuple element.

---

## 2. Operators and Assignment

### Bitwise

`&`, `|`, `~` (**xor** — `^` is pointer syntax), `<<`, `>>`, prefix `~` (complement), plus compound forms. Integers only.

- Precedence is not C's: bitwise binds **tighter than comparison** (`flags & MASK == 0` does what it reads as) and looser than arithmetic; shifts bind above addition.
- An out-of-range shift amount **traps**. A shift's count is typed independently; the **target's** signedness picks the shift (`u8` 200 `>>= 1` is 100).

### Overflow

Integer `+ - * /` **trap** on overflow. Explicit alternatives, builtin on every concrete width, all `pure noalloc`:

| Family | Methods | Result |
|---|---|---|
| `wrapping_*` | `add` `sub` `mul` | modular two's complement |
| `saturating_*` | `add` `sub` `mul` | clamped |
| `checked_*` | `add` `sub` `mul` `div` `rem` `rem_floor` | `Maybe<T>`; `div`, `rem` and `rem_floor` are `None` on a zero divisor and on `INT_MIN ÷ -1` |
| `rotate_*` | `left` `right` | the bits that leave one end arrive at the other; the amount is **modulo the width**, so `rotate_right(0)` and `rotate_right(32)` on a `u32` are both the identity. Bit-level, so signedness does not enter (LLVM `fshl`/`fshr`) |

`checked_rem` is `%` and `checked_rem_floor` is `%%` — see the two remainders below.
`INT_MIN % -1` answers `None` although its mathematical value is 0: the name means "the
operation the operator would have refused, as a value", and `%` traps there.

### The two remainders

`%` is the **truncated** remainder and `%%` the **floored** one:

| | `11 % -3` | `11 %% -3` | `-11 % 3` | `-11 %% 3` |
|---|---|---|---|---|
| | `2` | `-1` | `-2` | `1` |

- `%` takes the sign of the **dividend** and is the partner of this language's truncating
  `/`, so `a == (a / b) * b + (a % b)` holds. It is what C, Go and Rust spell `%`.
- `%%` takes the sign of the **divisor**. It is what Python spells `%`. For unsigned
  operands the two are identical.
- The names follow: `checked_rem`/`rem_operator` for `%`, `checked_rem_floor`/
  `rem_floor_operator` for `%%`. Until 09/17 the grammar called `%` `mod_operator` and
  `%%` `remainder_operator`, which was backwards from every language that uses those two
  words; the rules were renamed rather than documented.
- Only `%` is overloadable, because `(_%%_)` is not a spellable method name
  (`std/prelude/math.lyra`).

### Places

A place is what `=` writes and `&` addresses: a binding, a field `p.x`, an element `xs[i]`, a tuple position `p.0`, or a deref `p^`, and any path of those (`b.t.1`, `xs[i].0`). A tuple position obeys a field's rules — the root must allow interior mutation, and the value must fit the element's type.

**A `mut` argument is exclusive by place.** `mut` and `ref` parameters point into the caller's storage, so within one call, no other `mut` or `ref` argument may reach the place a `mut` argument names (`lyra-E001`). Places overlap when one path is a prefix of the other (`s` and `s.a`, or `s.a` twice) and not when they part at a field or tuple position: `f(s.music, s.chip)` is two slots, as disjoint as two variables. Two elements `xs[i]`/`xs[j]` always overlap (an index is a runtime value), as do a union's members (one slot). Two `ref`s may share; scalars are exempt (passed by value). This is about the slots arguments point into, not heap aliasing: two fields holding one reference-counted array still share its buffer, as two variables can.

### Compound assignment

`+= -= *= /= %= &= |= ~= <<= >>=` target any place `=` accepts (`counts[i].n += 1`) under the same writability rules. **Not a desugaring**: the address is computed once, so `xs[idx()] += 5` calls `idx` once. An overloaded operator is reached through it.

### Tuple assignment

`(a, b) = (b, a)`, `(q, r) = divmod(n, d)`.

- Targets are places (name, `p.x`, `xs[i]`, `p.0`, `p^`); anything else is refused by the collector, a constructor target by name. `a, b = b, a` is not a form.
- **RHS evaluated to a tuple first, then places written left to right**; each address computed once, after the RHS.
- Collector desugaring (not a statement kind): `{ let (t0, t1) = rhs; p0 = t0; p1 = t1 }` with position-stamped names in their own scope. Later passes never see it. The target parses as `tuple_literal` (a place-tuple rule would be a reduce-reduce conflict).
- Places are the RHS's context: `(a, b) = (4.0, 0.5)` on f32 narrows both. The desugared `let` records its assignments in `DestructuringDeclStmt.Assigns` for this. A mismatch is reported with the stand-alone assignment's message.
- Any tuple destructuring arity mismatch reports once; unpaired names are bound untyped (no cascading "undefined identifier").

### Patterns

- **One meaning in every position** — a `match` arm, `if let`, `let`, `let … else` and a parameter. A literal, range or regex at any depth is checked against the type in its position by the rule a `match` on that type applies.
- **Tuple rest**: `(a, ...mid, z)` — positions after the rest count from the end, and `mid` binds a **tuple** of what it covers, whatever the count (one position is a one-element tuple, none is `()`). The same in a constructor payload: `Tri(i, ...more)`.
- **Array rest is the tail**: `[h, ...t]`. A rest before the end of an array pattern, or a second rest in any pattern, is `lyra-E076`.
- `Rect _` stands for `Rect(_, _)`, and `Rect pair` for `Rect(...pair)`: one name for a multi-field payload binds it as a tuple of the fields. With one field, `Some x` binds the field itself.
- A capitalized name in a pattern is a constructor. Comparing against a `const` is a guard: `v if v == LIMIT` (`lyra-E057`).
- **A range bound may be a `const`**: `LOW..<=HIGH`, `..<LIMIT`. It is the folded value, so exhaustiveness, overlap and `lyra-E048` read the number. The `const` must be in scope unqualified (import it by name) and fold to an integer, or a float for a float scrutinee; a float bound on an integer scrutinee is an error.
- An all-caps constructor takes its payload in parentheses in a pattern: `CD(x)`, not `CD x`.
- **A plain `let` and a parameter take only a pattern that cannot fail** (`lyra-E077`): `let Some(v) = m` needs `let … else`, and a parameter wants a plain name and a `match`. A literal, range, regex or array pattern can fail; a one-constructor `data` pattern cannot (`let W(x) = w`).
- **A `data` match covers a constructor only where it covers its payloads** (`lyra-E009`): `Some(0) => …, None => …` is not exhaustive. Coverage may be spread across arms at any depth, nested tuples and structs included.

### Operator overloading

Arithmetic/bitwise overload; comparisons do not.

```lyra
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
```

- `+ - * / % << >> & | ~`, prefix `-`/`~`, and compound assignments dispatch by **method name**; the trait name is the author's. Two traits providing one operator for one type is an ambiguity at the operator.
- `Eq`/`Ord` own the comparisons (`<` and `<=>` must agree); `(_==_)` as a method name is `lyra-E039`. The prelude marks them `@builtin(Eq)`/`@builtin(Ord)` — found by identity, so a user `trait Ord` is ordinary.
- A primitive (unstripped) is never routed through an impl: `impl Add for i64` is inert; a newtype over a scalar **is** routed.
- An operator is a call for `pure`/`det`/`noalloc`.
- Inert with a warning: `&&`/`||`, `!`, `**`, suffix `_++`/`_--`.
- A type-parameter operand resolves via a `where` bound: `let sum<t> where t: Add = (a: t, b: t) -> t => a + b`.
- Parses: `Cents(150) + Cents(275)`, `(a + b).x`. Each node has exactly one derivation path (grammar partition rule).

### `min` / `max` / `clamp`

Prelude, `where t: Ord`, `self` receiver (`a.min(b)` = `min(a, b)`). `min` keeps `self` on a tie, `max` takes `other`. `clamp` traps on `lo > hi`. Prelude implements `Ord` for integer widths and `rune` so the bound is satisfiable (`3 < 5` stays a machine compare).
- **Floats have no `Ord`** (NaN), so they cannot be sorted generically — but `min`/`max`/`clamp` have **concrete `f16`/`f32`/`f64` overloads** beside the generic ones (the generic is the set's fallback, below). Same tie rules (`0.0.min(-0.0)` is `0.0`). **A NaN operand traps** (`min: NaN has no order`), as `floor`/`round` do: IEEE `minNum` (C `fmin`) would answer the other operand and hide the bug, and propagating (JS, Go) would surface it downstream of its cause.

---

## 3. Collections and Strings

### Arrays

- **The spelling is the flavor**: `[1, 2, 3]` is a dynamic `[]T` (a heap box; allocates, so `noalloc` refuses it) and `#[1, 2, 3]` a fixed `[3]T` (inline storage; never allocates). No context changes it, anywhere — an annotation, argument, return, payload, receiver or branch.
- **Neither converts** (`lyra-E079`, naming the other spelling): `[1, 2]` where a `[2]i64` is wanted, `#[1, 2]` where a `[]i64` is. A fixed-array binding does not widen either (`let xs = #[1, 2]; xs.map(f)` is refused); `.slice(0, n)` is the explicit copy.
- **Element types still come from context**: `let xs: []u8 = [1, 2]`, `let m: [][]u8 = [[1], [2, 3]]`, `-> []u8 => if c { [1] } else { [2, 3] }`. An empty `[]` takes any element type; `#[]` is `[0]T`.
- `#[` is one token: `# [1]` is not a fixed array. Array *patterns* (`[a, b, ...rest]`) match either flavor.
- **`==`/`!=` are structural**: a fixed array compares element-wise, a dynamic array by length and then elements in order, recursing into nested arrays, strings, structs and `data` payloads.
- Elements: any type but `void` (tuples, raw pointers, anonymous structs), optionally with one allocation or `weak` modifier (`[]shared Node`).
- Layout `{rc, weak, len, cap, T*}`: elements behind a pointer so growth cannot move the box (one extra load per access).
- `xs.push(v)`: amortized doubling, `mut` receiver (same diagnostic as `xs[i] = v`), `noalloc` refuses.
- `xs.slice(start, end)`: half-open, **always `[]T`** even from `[N]T`, **copies** (`noalloc` refuses). `end == len` legal, `start == end` empty; negative, past-length or inverted bounds trap.

**Spread `[...xs, v]`**
- Operand is any postfix expression (`...f(x)`, `...h.xs`).
- Result is **always `[]T`**, even if all operands are fixed. `#[...xs]` is `lyra-E078`: a fixed array's length is part of its type.
- Allocates once, sized from operand lengths; each operand evaluated once in order.
- Only inside an array literal (`lyra-E068`); `f(...xs)` refused. Non-array operand refused by name (`[..."ab"]` names `to_runes()`).

**Repeat `[v; n]` / `#[v; n]`**
- Value evaluated **once**; each slot retains it.
- `[v; n]` is `[]T` for any integer count; `#[v; n]` is `[n]T` and its count must fold (const chains work; `lyra-E056` otherwise, naming `[v; n]`).
- **`lyra-W019`**: repeating a value with shared mutable structure aliases it — `[[' '; W]; H]` is one row H times. Fires for a `[]T`, a `shared` aggregate with a writable field, or a struct/tuple/`data`/`[N]T` containing one. Silent for strings and `shared` scalars. `readonly` does not prevent the sharing. See `examples/life.lyra`.

**Comprehensions `[x in xs | guard | result]`**
- **`x in xs` is a comprehension clause** and `xs` is its **source**. A `gen` function is a
  *generator function*; nothing here is called a generator, because the word used to name
  both. Always qualified, because bare "clause" is a multi-clause function's arm — the two
  never appear unqualified in the same prose. (The grammar node and the AST field keep the
  name `generator`: a contract older than the convention.)
- The guards are optional (`[x in xs | x * 2]`), comma-separated, and **all must hold**.
- **Several clauses nest left to right**: in `[y in ys, x in xs | …]` the `y` clause is the
  outer loop, so `ys = [10, 20]`, `xs = [1, 2, 3]` yields the pairs in the order `10:1
  10:2 10:3 20:1 …`.
- A source is an array, a range, a string (by rune), or one `Seq`. Capacity is the product
  of the sources' lengths, computed up front, which is why a clause whose source depends on
  an **earlier clause's binding** (`[row in grid, cell in row | cell]`) is refused today.
- The result is any expression, and the brackets make a `[]T` — there is no `collect`.

**Sorting** (all ordinary Lyra in `std/prelude/array.lyra`):

| Method | Bound | In place | Stable | Allocates |
|---|---|---|---|---|
| `sort()` | `t: Ord` | yes (`mut`) | no | no |
| `sort_by(cmp)` | none | yes | no | no |
| `sorted()` | `t: Ord` | no, `pure` | yes | twice |
| `sorted_by(cmp)` | none | no | yes | twice |

- `sort` is an introsort: median-of-three Hoare quicksort, insertion sort under 16, falls to `heap_sort` past depth 2·log2(n) (never quadratic). `sorted` is bottom-up merge sort over a scratch buffer.
- `cmp: (t, t) -> Ordering` — the way to sort floats, by one field, or reversed. The `Ord` forms delegate with `pure (a, b) => a.compare(b)`.

**Destructuring `let` names own their values** (`let (x, y) = (b, a)` retains each managed leaf); match arm and `if let` names borrow.

### Strings

UTF-8, immutable `{ptr, byte_len, rune_count}`. The language is **rune-indexed**.

- **Interpolation** is `"${expr}"`, and **`${` has no escape**: the two characters as text are a raw string, `` `${` ``. An interpolation that runs past the end of its line is **`lyra-E080`** — it swallows source to the next `}`, declarations included, and still parses, so the report would otherwise be an undefined name far below. No program writes a newline inside `${…}`.
- **Raw strings** — `` `…` ``, `` #`…`# ``, `` ##`…`## `` — take every byte between the delimiters as the value: newlines, `\`, `${`, no escapes, no interpolation, no indentation stripping. Add a `#` pair to hold a backtick. A `/* glsl */` comment right before one is an editor marker (both extensions highlight the content as GLSL); the compiler ignores it.
- A **NUL sits at `data[byte_len]`**, never read by the language (interior NULs are legal), so `s.cstring_ptr()` (`unsafe`, `^u8`, checks for interior NUL) hands C a pointer without copying. Literal bytes are read-only — C writing through it faults.
- `s[i]` is the i-th code point (O(i)); `s.len()` is the rune count, O(1). **Use `for i, c in s`**, not `for i in 0..<s.len() { s[i] }` (O(n²)).
- `s.slice(start, end)`: half-open rune range, **allocates** (so `noalloc` refuses `slice` and `trim`). Like `s[i]` it is **O(start)** — it walks to find the rune — so a scanner that slices at its cursor is quadratic; walk `to_runes()` and slice *that* instead (measured 09/17: 26 s against 0.53 s over 200 KB). `trim`/`trim_start`/`trim_end` strip the five ASCII whitespace chars only.
- **Negative index traps**; provable negative is `lyra-E022` (naming `from_end`; for `slice` naming `slice(0, len() - 1)`). `from_end(k)` is 1-based end-relative for strings and arrays. `s[n]` is not an index; `slice(n, n)` is `""`.
- `starts_with`/`ends_with`: byte-level over `byte_len()` + `compare_bytes_at(offset, other)` (memcmp; `== 0` is a prefix test); exact because UTF-8 is prefix-free. `pure noalloc`.
- `index(needle, offset = 0) -> Maybe<i64>`: naive scan, offset and result in **rune** indices.
- `index`/`contains`/`split` are generic over `pub trait Needle`, implemented for `rune` and `string`. Two methods: `found_at(haystack, offset)` returns an `(Index, Length)` span and **walks from rune 0** (a rune offset is only reachable by counting), and `matches_at(haystack, rune_at, byte_at, here)` tests one position, which is what lets a stepping caller stay linear. `matches_at` is defaulted in terms of `found_at`, so an existing needle keeps working and only pays the old cost. `split` walks once and cuts parts out of the bytes, so it is **linear** (it was quadratic in both respects until 09/17: 600 K runes took 4.7 s, now ~10 ms). `split` on an empty separator traps, naming `to_runes() -> []rune`.
- `split` keeps empty parts (`"a,,b"` → 3); `split_when(pred)` **collapses** runs of boundaries and drops leading/trailing empties.
- `lines()` splits on terminators, not on `\n`: `\r\n` is one, a trailing newline does not make a last empty line (`"a\nb\n"` → 2), and an interior blank line is a line. An empty string has none.
- `pad_start(width, fill = " ")` / `pad_end(width, fill = " ")` pad to `width` runes; a longer fill repeats and is cut to fit (JavaScript's `padStart`), and a string already at least `width` long is returned whole, never truncated.
- `strip_prefix`/`strip_suffix` answer `Maybe<string>` — the affix removed, or `None` when it was not there, so the affix's length is never written twice.
- `s.byte_offset(i) -> Maybe<i64>`: rune position → byte offset; end position answers `Some(byte_len)`; negative is `None`.
- ASCII classifiers (name is the boundary): `is_ascii_upper`, `_lower`, `_alpha`, `_digit`, `_punctuation`, `_printable` (space..`~`, not "not a control code"), `_control_code`, `is_ascii_space`. `is_ascii_alpha` splits `héllo`; use `is_ascii_space` for non-ASCII text.
- `to_ascii_lower`/`to_ascii_upper`: total (identity on non-letters), on both `rune` and `string`. ASCII only.
- **Bytes ↔ text** (builtins; both copy):
  - `bytes.decode_utf8() -> string` — **does not validate** (rune count = non-continuation bytes, like `read_line`).
  - `s.encode_utf8() -> []u8` — the only way to read a string's bytes.
  - `p.decode_utf8(byte_len)` on a `^u8` — `unsafe`; negative length traps, too-large cannot be caught. `std.ffi`'s `CBuffer.decode_utf8` is the checked form.
- A literal or constructor call is a postfix head: `"abc".len()`, `["x"; 3].join("-")`, `1.wrapping_add(2)`, `Some(1).unwrap_or(0)`.

### HashMap (`std.collections`)

`HashMap<k, v>` in `lyra/std/collections/hashmap.lyra`: one `[]Maybe<Entry<k, v>>`, open addressing, linear probing, power-of-two capacity, load ≤ 3/4, backward-shift deletion. Iteration order changes on growth — sort `keys()` if order matters.

- Key implements `Hash` (`hash: (Self) -> u64`; provided for ints, `rune`, `bool`, `string`). Invariant: `==`-equal values hash alike. Combine fields with `hash_combine`; the map finalizes every hash. No `Eq` bound (structural `==` works on type variables).
- `insert` returns nothing; `replace` returns the displaced value; `remove` returns the value (`let _ = m.remove(k)`) — avoids `lyra-W006` on discarded `Maybe`.
- Constructors `hashmap_new`, `hashmap_with_capacity` take type arguments from annotation or turbofish.

### JSON (`std.json`)

`parse_json(text) -> Result<JsonValue, JsonError>` in `lyra/std/json.lyra`. `JsonValue` = `JsonNull | JsonBool | JsonNumber(f64) | JsonString | JsonArray([]JsonValue) | JsonObject([]JsonMember)`; `JsonError` has a message and **byte** offset.

- Accessors never trap: `field`/`element`/`as_number`/`as_int`/`as_text`/`as_bool` → `Maybe`; `elements`/`members` → empty for the wrong shape. `doc.field("nodes").unwrap_or(JsonNull).elements()`.
- Objects keep members in order with duplicates; `field` answers the last.
- Numbers are `f64`: exact up to 18 significant digits with exponent within 22; longer may be an ulp or two off.
- Parser is `pure` (threads a byte offset).

### Dates (`std.temporal`)

After the web's **Temporal** API: a wall-clock value and an exact instant are different
types, and arithmetic on a date is calendar arithmetic. ISO 8601 calendar only. Built slice
by slice against `examples/calendar`; what exists is what that program has needed.

- **`PlainDate`** — `newtype PlainDate = i64`, days since 1970-01-01, range `-271821-04-19`
  to `+275760-09-13` (Temporal's; ±10⁸ days, one extra at the start). **A day count because
  Lyra has no field privacy**: a three-field struct could be forged as 30 February by any
  module, and a day count has no invalid values. `year()`/`month()`/`day()` are O(1)
  computed methods (Howard Hinnant's civil algorithms).
- **Constructors are bare functions** (no associated functions, `lyra-E035`):
  `plain_date(y, m, d) -> Maybe<PlainDate>` **rejects** a date that does not exist, like
  Temporal's constructor; `parse_plain_date(s)` reads `YYYY-MM-DD` and `±YYYYYY-MM-DD`
  exactly (not `2026-9-18`, not `-000000`), and `show` writes the same forms.
- Properties: `day_of_week()` (**1 = Monday**, ISO), `day_of_year()`, `week_of_year()` (ISO:
  a week belongs to the year of its Thursday), `days_in_month()`, `days_in_year()`,
  `in_leap_year()`.
- `add(d)`/`subtract(d)` take a `Duration` with Temporal's default **constrain** overflow:
  years and months first, the day clamped to the new month (31 Jan + 1 month = 28/29 Feb),
  then weeks and days; clock fields count in whole days. `with_day(n)` clamps.
  `until(other)`/`since(other)` answer days. `Ord`, so `<` and `sorted()` work.
- **`Duration`** — Temporal's ten fields, not a count of nanoseconds, since a month has no
  fixed length. Builders `years`/`months`/`weeks`/`days(n)`, `duration()` for the zero,
  `negated()`. No field privacy, so a mixed-sign duration can be written; `add` applies it
  in Temporal's field order.
- **`today()`** — `Now.plainDateISO()`: the system clock plus the local zone's current UTC
  offset, read from `localtime_r`'s `tm_gmtoff`. An `extern` rather than a builtin, because
  `struct tm` has **one layout on both platforms** (56 bytes, `tm_gmtoff` at 40 — measured),
  unlike `struct dirent`. It reads the clock, so it is neither `pure` nor `det`; take a
  `PlainDate` parameter and call it once at the edge.
- **`PlainTime`** — `newtype PlainTime = i64`, nanoseconds since midnight, for `PlainDate`'s
  reason. `plain_time(h, m = 0, s = 0) -> Maybe` rejects `24:00` and `:60`;
  `parse_plain_time` reads `HH:MM`, `HH:MM:SS` and a one-to-nine-digit fraction; `show`
  writes seconds always and a fraction only when there is one. `hour()`/`minute()`/
  `second()`, `Ord`. A day is exactly 24 hours: a wall-clock time has no zone.
- **`PlainDateTime`** — a **struct** `{ date, time }`, safely: any existing date with any
  existing time exists, so the invariant lives in the fields' types. `d.to_plain_date_time(t)`,
  `parse_plain_date_time("…T…")` (a `T`, not a space), `add`/`subtract` (calendar units
  first, then clock units **carrying whole days**: 23:30 + 1h is the next day),
  `until`/`since` balanced **days down to nanoseconds**, every field sharing the whole's sign.
- `Duration` clock builders `hours`/`minutes`/`seconds(n)`, and **`parse_duration`** for ISO
  8601 (`PT30M`, `-P1DT2H3.5S`) — fields **as written, not balanced** (`PT90M` stays ninety
  minutes); refuses units out of order or repeated, an empty `P` or `T`, and a fraction on
  anything but seconds.
- **`now_plain_date_time()`** — `Now.plainDateTimeISO()`, the clock plus the local offset;
  `today()` is its date. A wall-clock reading, so it can go backwards across a DST change.
- **`PlainTime` arithmetic**: `add`/`subtract` **wrap at midnight** and ignore the calendar
  units, days included (a time has no date to carry into); `until`/`since` never wrap and
  balance **hours down to nanoseconds** (23:00 until 01:00 is `-22h`).
- **`Duration` arithmetic** — `add`/`subtract -> Maybe<Duration>`, `None` when either side
  has years, months or weeks (their length needs a date); a day is 24 hours. The sum is
  balanced up to the **larger of the two largest units written**, Temporal's rule:
  `PT90M + PT30M` is `PT120M`, `PT1H + PT90M` is `PT2H30M`.
  `compare_durations(a, b) -> Maybe<Ordering>` — a function, not `Ord`, since the order is
  partial; field-identical durations are `Equal` even with calendar units.
  `total(unit: TimeUnit) -> Maybe<f64>`, `TimeUnit` being `Days` down to `Nanoseconds`.
- **`TimeZone`** — a zone's rules, read from the system's IANA database (`/usr/share/zoneinfo`, RFC 8536), not from libc: `load_time_zone(name)`, `offset_at`, `abbreviation_at`, `is_dst_at`, `next_transition`, all in epoch seconds. The database is `TZDIR` when set and non-empty, else `/usr/share/zoneinfo` — which is how a test pins the data it reads, since a country changing its rules moves the system copy under every test that reads it. The **64-bit block** is the one read, so a transition past 2038 survives, and **the POSIX footer** (`EST5EDT,M3.2.0,M11.1.0`) governs every instant from the last transition on — a table stops around 2037 and the rule after it does not, so a schedule projecting a decade ahead needs it. POSIX writes its offsets **west**-positive (`EST5` is −5·3600 east), `Mm.w.d` means the `w`th `d`-day of month `m` with 5 meaning the last, and `Jn`/`n` are the two day-count forms. `next_transition` projects from the rule once past the table. POSIX only, as `std.path` is. A name reaching the filesystem is checked: `..` and a leading `/` are refused.
- **`Instant`** — `newtype Instant = i128`, nanoseconds since the epoch (an `i64` of them runs out in 2262), range ±10⁸ days as Temporal's is. `instant(sec, nsec)`, `now_instant()`, `epoch_second`/`epoch_nanosecond`/`nanosecond_of_second`, `add`/`subtract` (**calendar units refused** — a day is not a fixed length of time where a zone changes offset), `until`/`since` balanced hours-down, `Ord`, and `Show` as `…Z`.
- **`ZonedDateTime`** — a struct `{ instant, zone }`, and the local fields are computed, never stored: the clock reading `01:30` on a fall-back morning names two instants, so a struct holding `01:30` could not say which. `to_zoned_date_time(zone)`, `now_in_zone`, `to_instant`, `offset_seconds`, `abbreviation`, `is_dst`, `to_plain_date_time`/`to_plain_date`/`to_plain_time`. `Show` is RFC 9557's `2026-09-22T14:30:00-04:00[America/New_York]` — all three parts, since any two are ambiguous — and **`parse_zoned_date_time` reads it back**: the bracketed zone is required, the offset optional, and where it is written *it decides*, which is how `01:30-04:00` and `01:30-05:00` name two different instants on a fall-back morning. An offset the zone was not using is refused (Temporal's `offset: 'reject'`). Not `pure`: it reads the zone database. `parse_instant` is the same for `…Z` or any offset, and is `pure` — an offset names an instant without a zone. **`<=>` compares instants and `==` is structural** (instant *and* zone), which is Temporal's `compare`/`equals` split.
- **Disambiguation** — `PlainDateTime.to_zoned_date_time(zone, how)` going the other way, where a wall clock is not a moment: `02:30` never happens on a spring-forward morning and `01:30` happens twice on a fall-back one. `possible_instants` answers 0, 1 or 2 candidates; `Disambiguation` is `Compatible` (the default: forward across a gap, the first of a repeat), `Earlier`, `Later`, `Reject` (`None`). Temporal's option, and its defaults.
- **`ZonedDateTime.add`/`subtract`** — **calendar units move the wall clock and clock units move the timeline**: a week after 09:00 is 09:00, while 168 hours after it is 08:00 or 10:00 when the clocks changed. This is what a `Duration`'s split between calendar and clock fields is *for*; the calendar part is resolved by `how`, since a monthly 02:30 meets the March morning that has no 02:30.
- Not built: a `reject` overflow on arithmetic,
  rounding, sub-second accessors, and month and weekday **names** (Temporal leaves those to
  `Intl`; the calendar holds its own).

### Lazy sequences

A `gen` function yields into a `Seq<t>`; combinators are Lyra in `std/prelude/seq.lyra` (`seq`, `map`, `filter`, `take`, `take_while`, `zip`, `to_array`, `sum`, `count`, `first`).

- **Consumed where written** (`for x in xs.seq().filter(p).map(f)`, or a terminal like `sum`): one fused loop, no allocation.
- **Held as a value** (binding, argument, field): a cursor over an LLVM coroutine; `s.next()` steps it (`mut` receiver); copies share the cursor; the last reference destroys the coroutine.
- `Seq<t>` is compiler-known by name, declared nowhere; a program's own `Seq` wins.
- A `gen` function (a **generator function** — never just "generator", which used to name a comprehension clause too) must declare `-> Seq<t>`; body is void; `yield e` checks `e: t`; `yield from s` lowers only for sequences (write the loop for arrays/ranges).
- Consumers: `for-in` and comprehensions. Brackets make an array; there is no `collect`. Eager `map`/`filter` on `[]t` coexist via receiver-keyed overloading.
- **Sequence values need clang ≥ 15** (coroutine splitting). `lyrac` probes once and refuses by name, keeping the `.ll`; backend tests skip.
- Still refused by name: a plain function returning a sequence from a block body, a lambda literal inside a `gen` used as a value, a `mut` parameter on one, `yield from` over a non-sequence.
- `examples/primes.lyra` is the target program.

---

## 4. Traits, Generics and Dispatch

In a trait method's signature `Self` is the implementing type at any depth: `(Self) -> []Self`, `Maybe<Self>`, `(Self, Self)` and a callback's `(Self) -> Self` all mean it, in the impl and at the call.

### Supertraits

`trait B: A`: every implementer of `B` must implement `A` (`lyra-E040`, at each `impl`), and a `where t: B` bound reaches `A`'s methods and satisfies `where u: A`. Cycles are legal (always implemented together). Bodies are optional:

```lyra
trait Arithmetic: Add + Sub + Mul + Div
impl Arithmetic for Vec2      // still required; missing Mul is refused here
```

### Default methods

```lyra
trait Named {
  pure name: (Self) -> string
  pure shout: (Self) -> string = (self) => self.name() ++ "!"
}
```

- A default is generic code: `Self` is a type variable bounded by the trait; checked once, compiled per implementer. It may call only methods of the trait or its supertraits.
- An impl's own clause wins; a default calling another default reaches that method's override.
- The trait's declared bound (e.g. `pure`) is enforced and reported **at the default**, not at inheriting impls.
- A bound the default needs goes on the trait as a supertrait (`trait Doubled: Show`).

### Calling on a receiver

Opt in by naming the first parameter `self`.

- **UFCS:** `m.unwrap_or(0)` → `unwrap_or(m, 0)`, rewritten before anything downstream. `own` receivers refused. A type-parameter receiver resolves against functions generic in their receiver (`a.max(b)` under `where t: Ord`).
- **A more specific impl wins** (09/23). Where several impls of *one* trait match a
  receiver, the one whose target the others subsume is taken: `impl Show for Box<i64>`
  beside `impl Show<t> for Box<t>` dispatches to the concrete one for a `Box<i64>` and to
  the generic one for everything else. Specificity is subsumption — `Box<t>` matches
  `Box<i64>` and `Box<i64>` matches `Box<t>` not at all. **Incomparable targets stay
  ambiguous** (`Pair<i64, b>` beside `Pair<a, i64>`: each covers values the other does not),
  and ranking never reaches **across traits** — two traits providing one method name is a
  choice for the caller to state (`Pilot::fly(b)`), not a question about which target is
  narrower. Identical targets are still refused at the impls (`lyra-E037`).
- **Receiver-keyed overloading:** one module may declare a name several times if each takes `self` with a different receiver type head (`Maybe<t>` vs `Result<t,e>`); a second `Maybe<…>` is refused. A name still may not be exported by two modules.
  - **One generic fallback per set:** a member whose receiver is a bare type variable (`self: t`) is admitted and **ranked below every concrete member** — it answers only when none accepts the receiver (the prelude's `min<t> where t: Ord` beside `min(self: f64, …)`). A second one is refused. "This type beats any type" is the only ranking; there is none between concrete types. It applies to method calls across modules too, so `x.f()` and `f(x)` agree.
  - An untyped literal receiver is read at its default for this choice (`min(1.5, 2.0)` reaches `self: f64`).
- Method calls resolve against the receiver's type and need no import of the underlying free function — but the function must be one this file could have named: `pub`, in a module this file imports (in any form), in this file's own module, or in the prelude. A module's **private** function is not a method anywhere else, which is what privacy has to mean: before 09/22 it was still a *candidate*, so importing one name from a module made an unrelated call ambiguous with a function the caller could not name, let alone call.
- **The import list breaks a tie between methods; it does not gate the call.** Two reachable candidates accepting one receiver is ambiguous (`lyra-E001`), and naming one of them in an import settles it — as does declaring your own, which wins first. Leaving a method out of the list is **not** an error: `import std.collections.{ parse_args }` still admits `args.value(…)`. Requiring the name was measured and rejected (todo.md, 09/22): accessors on types that cannot have public fields are ordinary methods here, so the rule would tax exactly the code the lack of field privacy forces, and would put names like `day` and `value` in the file's bare scope, where they shadow the locals those calls are assigned to. It is also **not** Rust's rule, which imports a *trait* and never binds the method name.
- A type named inside a declaration — a constructor's payload, a field — means what it means in the **declaring** module. `Cons(n) =>` in an importer binds `n` at the library's `Node`, even when `Node` is private there or the importer declares its own; naming the private type explicitly is still refused.

### Show

`print` and `"${…}"` choose a formatter per concrete type; a type-parameter value needs `where t: Show`:

```lyra
let describe<t> where t: Show = (v: t) -> string => "value ${v}"
```

- Trait and scalar impls are Lyra in `std/prelude/show.lyra`. The operand is rewritten to `v.show()`.
- Recognised **by method** (any in-scope bound declaring `show`); diagnostics suggest `Show`.
- A concrete type with a `show` impl prints through it; printable primitives always use the built-in formatter. `impl Show for Pt { show = (self) => "${self}" }` is refused (would recurse).

### A call's type arguments

Solved from argument types first, then:
- **Turbofish** `f::<i64>()` — positional, declaration order; beats context.
- **Context** (annotation, declared return type, parameter slot) — binds **only variables no parameter mentions**, and only on declarations that declare their `<…>` list. This is what makes `hashmap_with_capacity<k,v>(cap)` callable. A context reaches the value through an `if`'s branches, a block's tail and a match arm, never a statement inside them: `let q = make()` in a branch of a `-> Maybe<i64>` body is unsolvable, as it is in the body itself.

### A lambda literal's missing annotations

`(x) => x * 7` takes its missing parameter and return types from the function type its context expects: an annotated binding, a return position, or an argument slot of any call — a function, a trait method (`3.apply((x) => x * 7)`), a method through a `where` bound, a function-typed field.
- Only blanks are filled; a written annotation wins and is checked.
- A slot type mentioning a variable the call has not solved (the `u` of `(t) -> u`) is left blank for the body to solve; the enclosing declaration's own variable (`(t) -> t` inside `<t>`) is filled in.
- An arity mismatch fills nothing.

### Generic parameter lists

A lowercase type name is a type variable wherever it appears. What a written `<…>` list must agree with depends on what declares it:

- **A binding** (`let f<t> = …`): the list is optional; written, it is authoritative. A signature variable missing from it is `lyra-E031`, a listed one the signature never mentions is `lyra-W013`.
- **A type** (struct, `data`, named tuple, newtype, union): every variable its body mentions must be in its list (`lyra-E031`). A parameter may carry a bound, `struct Bx<t: Tag>`, **enforced at instantiation** (`lyra-E036`) as a function's is; a type-variable argument is left to the enclosing declaration's own bound — the list is how `Box<i64>` gives one a type, so there is no list-less generic type. An **unused** parameter is fine: `struct Id<t> { n: i64 }` is a phantom type. A type alias takes no list, so its body can mention no variable.
- **A trait**: a parameter no method mentions is `lyra-W013`. A parameter may carry a bound, `trait Holder<t: Tag>`, **enforced at the impl that binds it** (`lyra-E036`) — a trait's parameter has no value until an impl gives it one. A method may be generic in variables of its own (`mapv: (Self, (i64) -> b) -> b`), since there is no method-level list to declare them in. A call solves them from its arguments like a generic function's call — at a concrete receiver, or through a `where` bound, where the solution may name the enclosing function's variables and is specialized with it. One named like the impl's (or the bound receiver's) variable is a different variable. `Self<x…>` is the impl target's head at those arguments: `map: (Self<a>, (a) -> b) -> Self<b>` on `impl Functor for Box<t>` answers `Box<b>`, the receiver solving `a`. The target is either a generic type applied to exactly that many distinct type variables (`Box<t>`, `Pair<k, v>`) or one with exactly that many **holes**, `_`, marking the positions the arguments fill: `impl Functor for Result<_, e>` makes `Self<b>` `Result<b, e>`, `Table<k, _>` varies the last parameter, and `Table<string, _>` fixes the other. No positional convention decides for you — `Result<t, e>` without a hole is refused, as are `i64`, `Box<i64>` and `Pair<t, t>`. A hole is refused anywhere but an impl target, and a holed target refuses a trait whose method writes a bare `Self` (nothing says what fills `_`). A default method may write `Self<…>`: its body is checked with `Self` the abstract head, so `Self<a>` and `Self<b>` are distinct types there and `self.map(f)` resolves through the trait itself. A call through a `where` bound cannot reach a `Self<…>` method: a bare `t` has no head to apply (that would need a higher-kinded variable).
- **An impl** has no written list; its variables are lexical.

### Local generics

`let idf<t> = (a: t) -> t => a` inside a function is a full generic (turbofish, `where` bounds), may capture enclosing bindings, and may nest inside generic functions at any depth, mentioning outer type variables. Emitted as one closure per instantiation; a lambda calling it captures those closures (a captured `var` is read as at the declaration). Not a value: passing `idf` as a function is a type error.

### Calling on a type name

There isn't any. `Rng.seeded(42)` is **`lyra-E035`** (constructors are bare: `rng_seeded`). A trait gets its own message, since `Trait::method(…)` is valid.

### Calling without a receiver

A trait method whose first parameter is not `Self` (`zero: () -> Self`, `from_json: (JsonValue) -> Result<Self, string>`) is called as `Trait::method(…)`, and its impl is chosen from **what the result is used as**: the declared return type is unified with the context (`let port: i64 = FromJson::from_json(v)` picks the `i64` impl), or, failing that, with an argument whose parameter mentions `Self`. With neither there is nothing to choose by, and the call is refused naming the fix (an annotation). Inside a generic body the result may be a bound variable (`required<t> where t: FromJson`), and the call is then abstract, specialized with the body. A context does not reach through a method chain, so write `let decoded: Result<t, string> = FromJson::from_json(v)` and continue from `decoded`. A bare literal receiver on the ordinary form (`Pair::pair(1, 2)`) dispatches at the literal's default width.

### `?`, `From` and the context

- `?` propagates a `Result` from a `Result`-returning function and a `Maybe` from a `Maybe`-returning one, never across kinds (`ok_or`/`ok` convert). Across **error types** it applies a declared conversion: `parse_json(text)?` inside `-> Result<_, ConfigError>` runs `impl From<JsonError> for ConfigError`'s `from` on the error before propagating it. `?` knows both types, so the impl is found by a direct lookup — matched on the trait argument as well as the target, so a type may be built from several sources, one impl each. No impl is refused naming the one to write; `map_err` is the one-off spelling. The `from` runs on the failure path, so its effects are the `?`'s (a `pure` function refuses an impure conversion), and it takes the error by plain value (`own`/`ref`/`mut` are refused).
- **The context reaches through `?`**: what is wanted of `make(1)?` is the payload, so `make(1)` is inferred wanting `Result<payload, E>` (or `Maybe<payload>`), which is what solves a callee's return-only variable — `let port: i64 = required(json, "port")?` needs no turbofish.
- **The context settles what the arguments leave open.** A type variable only a lambda literal's return or an untyped literal reaches is bound from the call's context before they are: `b.map((x) => 200)` under `-> Box<u8>` gives the lambda a `u8` return and narrows the literal. A typed argument still wins, and a mismatch is then that argument's. A constructor's payload takes its slot's type from the construction's context: `Err(Zero::zero())` under `Result<i64, string>` infers the payload wanting a `string`.

---

## 5. Effects

`pure`, `det` and `noalloc` are **written** bounds a caller relies on; the compiler also infers the same facts whole-program.

- **`lyra-W018`**: a top-level function or trait-impl method with no observable effect that does not say `pure`. Nothing is refused: a `pure` function may call an unannotated one inferred clean. The bound decides **where blame lands** when an effect is later added — at the `println` in a marked helper, versus at the `pure` caller of an unmarked one.
- Only `pure` warns (`det`/`noalloc` candidates are too common to be useful). Not warned: inline closures, `main`, impl methods whose trait declares the bound. No `#[allow]` exists.
- On a **trait-impl method** the advice is to bound the **trait**, not the impl: a
  `where t: Trait` call is scored against every impl, so only the trait's bound is
  something a generic caller can rely on — and an impl inherits it, so writing it there
  satisfies every impl at once. Marking the impl alone is still offered, being the smaller
  commitment for a method reached only by direct dispatch.
- **A bound declared on a trait method is a contract, and an abstract call relies on it.** `trait Speak { pure say: (Self) -> i64 }` obliges every impl — and any default body — to be pure, reported at the impl that breaks it; in exchange, a `pure` function calling `x.say()` through `where t: Speak` is clean without consulting the impls at all. **With no bound declared there is nothing to rely on**, so the same call is scored as the join over every impl in the program: pure only if all of them are. That fallback is whole-program rather than modular, and the difference shows: until 09/22 the join ran even where a bound was declared, so one impure impl in *your* module made a library's `pure` generic fail to compile — on the library's line, for a type the generic was never instantiated at. A declared bound is how a library stays correct regardless of what implements its traits.
- **A call whose callee has no name is charged every effect.** `fs[0](n)` and `pick()(n)`
  cannot be resolved to a declaration, so `pure` refuses them and asks for a name or a
  parameter, either of which can be checked; a lambda literal called in place is the
  exception, judged by its body. Until 09/22 these were charged nothing at all, and an
  effect reached through one escaped a `pure` function.
- **A parameter's storage flavor is concrete even when unwritten, and a call across the
  `shared`/stack boundary is refused** (`lyra-E018`). A *binding* is polymorphic — `let q: E = s`
  inherits the flavor and the value is unboxed for it — but a function is compiled once and
  its parameter has one representation, so there is nothing left to inherit from at a call.
  Two positions stay polymorphic and are not refused: a **construction** (including a
  `match` or `if` whose arms are constructions) has no flavor of its own and takes the
  parameter's, and a **generic** parameter takes whatever the instantiation binds. Until
  09/23 the crossing was neither coerced nor reported: a `shared` argument in a plain
  parameter trapped, and a plain one in a `shared` parameter segfaulted.
- Effect classes referenced elsewhere: `EffectInput` (stdin, `program_arg*`, `read_key`, `wait_for_key_ms`, `terminal_size`), `EffectOutput` (`set_raw_mode`; `det`-legal), `EffectRand` (`random_seed`), `EffectTime` (`wall_clock_nanos`), `EffectMut` (a write the caller sees; `det`-legal). `det` permits output.
- **`EffectMut` is charged the same way whichever spelling writes**: `xs[0] = v`, `p.x = v`, and the mutating builtins `push`/`push_utf8`/`reserve`/`clear`. Through a `mut` parameter or a capture the write escapes and `pure` refuses it; on a local it does not and `pure` is fine. The builtins were exempt until 09/22, so `xs[0] = v` was refused from a `pure` function while `xs.push(v)` was accepted — and W018 *suggested* `pure` for a function whose only effect was the push.

---

## 6. Modules and Documentation

### A module is a file or a directory

`std.prelude` is `std/prelude.lyra` **or** every `*.lyra` directly in `std/prelude/` — identical semantics (one path, namespace, scope), so splitting a file changes nothing (overloading, `pub` and shadowing are keyed on the module).
- Every file in a module directory must declare the module; a single-file module needs no header.
- A subdirectory is a child module. Both forms in one root is an error.
- Compiling one file of a multi-file module brings its siblings.
- **`pub` is a top-level modifier.** On a binding inside a function body it is `lyra-E081`: `pub` exports a name from its module and a local binding has none to export. It was accepted and ignored before 09/16.

### Imports

- `import lib.{ listed }` admits `listed` only — no other exports, no types.
- `import lib` binds `lib.x` and no bare names. An alias binds only its local name.
- Resolution: **own scope → imports → prelude**. Using an export you didn't import names the fix (`add import lib.{ … }`), distinct from a missing `pub`.
- **Every position that writes a type's name takes the rule**: parameter, return type, local annotation, struct field, `type` alias. It is one pass over the written occurrences (`TypeRefs`), not a check inside a resolver — until 09/22 it was the latter, and the first two were refused while the last three were not. A type a value merely *carries* across the boundary is untouched: `m.col` on a value from a constructor payload writes no name, so there is nothing to refuse.

### Shadowing

A local declaration of an imported (or prelude) name wins every bare reference in that module and warns **`lyra-W016`** (the prelude's own is `lyra-W012`).

**Only a name the import admitted shadows.** `import lib` binds no bare name, so it shadows nothing; `import lib.{ a }` shadows `a` and no other export of `lib`; `import lib.{ a as b }` shadows `b`, never `a`. The shadowed declaration is reached by renaming one side — the local one, or the import (`import lib.{ a as other }`) — since a selective import binds no namespace to qualify through.

**Several modules may export one name**, and a module may re-export a name it imports (`pub let map = (n) => seq.map(n) + 1`). A bare name reaches a module only through its own member list, so the importer chooses; importing one name from two modules is an error, fixed with an alias (`import two.{ helper as other }`) or the namespace.

**Two modules' types of one name are different types**, wherever a value carries one: `let p: Point = two.make()` is refused when `Point` here is not two's, and the message spells them `two.Point` and `Point`.

### Documentation comments

`///` documents the declaration below; `//!` documents the module. Body is Markdown, **no `@param`/`@returns`**.

- Recognised headings (case-insensitive, singular): `# Examples`, `# Panics`, `# Errors`. Headings inside fenced code don't count.
- Documentable: top-level `let`/`var`/`const`, `type`, `trait`, `impl`, and members (struct fields, data constructors, trait method signatures, impl methods). Field docs live on the declaration (`TypeDeclStmt.MemberDocs`), never on `types.StructField`.
- **Attachment is adjacency**; an unattached doc warns **`lyra-W017`** (blank line before the declaration, EOF, local `let`, `//!` after the first declaration). Put implementation `//` notes **above** the doc block, never between it and the declaration.
- `//!` goes at file top or directly under `module`; multiple files join in file order into `SymbolTable.ModuleDocs`. `////` is an ordinary comment.
- Surfaced in LSP hover and rendered by `lyrac doc` (Markdown + Starlight frontmatter):

```bash
lyrac doc std/prelude/prelude.lyra -o ../lyra-website/src/content/docs/reference --strict
```

  Flags: `--private`, `--deps`, `--prelude`, `--strict` (fail on gaps). Undocumented public declarations are still listed. `pkg/docgen` renders source syntax, and a test re-parses every generated signature.

---

## 7. I/O and Runtime Builtins

### Environment (`std.env`)

`lookup(name) -> Maybe<string>` over libc's `getenv`. **A variable set to nothing is `Some("")`, not `None`**: POSIX distinguishes set-empty from unset and callers do too (`TZ=`), so collapsing them would make the difference unsayable. Reading only — `setenv` changes a table the C library and every thread share, so a library offering it hands its caller a way to change another library's answers underneath it. Not `pure`/`det`: read once at the edge, as with the clock.

### Console, arguments, files

| API | Kind | Notes |
|---|---|---|
| `print`/`println` | builtin | polymorphic over printable scalars |
| `read_line() -> Maybe<string>` | builtin | strips `\n` (and `\r`); `None` at EOF (distinct from `""`) |
| `line.parse_i64() -> Maybe<i64>` | prelude (`parse.lyra`) | strict: `None` on blank, lone sign, trailing garbage, whitespace, out of range |
| `program_args() -> []string` | prelude over `program_arg_count()`/`program_arg(i)` | index 0 is program name; `program_arg` traps out of range; EffectInput. `main` is emitted as `main(argc, argv)` |
| `read_file(path) -> Maybe<string>` | `std.io`, over `extern` `open`/`read`/`close` | `None` if unopenable; no UTF-8 validation; `open` declared variadic |
| `read_stdin()` | `std.io` | shares the chunked reader |
| `write_file(path, contents) -> bool` | `std.io`, over `creat` | loops on short writes |
| `append_file(path, contents) -> bool` | `std.io`, over `fopen(path, "a")` + `fileno` | creates if missing; shares `write_file`'s loop |
| `write_bytes(path, bytes: []u8) -> bool` | `std.io`, over `creat` | `write_file` for data that is not text (`read_bytes`'s pair); same loop |
| `read_dir(path) -> Maybe<[]string>` | `std.io`, over the `dir_names` builtin | names only, sorted, without `.`/`..`; dotfiles kept; `None` if unopenable, `Some([])` when empty |
| `dir_names(path) -> Maybe<[]string>` | builtin | what libc reported, in its order, `.`/`..` included. A builtin because `struct dirent`'s name offset and `readdir`'s symbol both differ by platform, and Lyra cannot ask which it is on; EffectInput |
| `is_symlink(path) -> bool` | `std.io`, over `readlink` | the link itself, not its target (`opendir` follows one); `false` for a missing path, `true` for a broken link |

- **Never `open` with flags beyond `O_RDONLY`**: `O_CREAT`/`O_TRUNC`/`O_APPEND` differ between macOS and Linux (only `O_RDONLY` = 0 is portable). Spell the combination with something portable instead — `creat` for overwrite, an `fopen` mode string for append. `examples/todo.lyra`, `examples/word_freq` show the shapes.

### Terminal

| Builtin | Effect | Behaviour |
|---|---|---|
| `set_raw_mode(on: bool)` | Output | echo/line-buffering/signals off; saves original termios on first enable, restores it on disable |
| `read_key() -> Maybe<rune>` | Input | one **code point**; no escape decoding (arrow = ESC, `[`, `A`); `None` at EOF |
| `terminal_size() -> (i64, i64)` | Input | **(columns, rows)**; 80x24 with no window |
| `wait_for_key_ms(timeout) -> bool` | Input | readable within timeout; closed fd reports readable; negative clamps to 0 |

- ANSI output is plain `print` (`\e` = `\x1b`). Everything above these four is Lyra in `std.tui`: `frame.lyra` (row-level diff, one cursor move per consecutive run), `box.lyra`, `status.lyra` (`status_bar`/`status_split`).
- **Mouse** needs no builtin, but must use **SGR mode** (`\e[?1006h`); X10's raw bytes break `read_key`'s decoding past column 95.
- **`std.tui` coordinates are 0-based** (`MouseEvent.col/row`, `move_to(col, row)`); only `mouse_event` and `move_to` convert.

### Randomness

`random_seed() -> u64` is the only builtin; `std/prelude/rand.lyra` has `rng_seeded(seed)`, `rng_from_entropy()`, `rng.next_u64()`, `rng.below(bound)` (half-open, unbiased via rejection), `rng.between(lo, hi)` (inclusive), and ambient `random_below`/`random_between`. xorshift64* — **not cryptographic**. A seeded draw is `EffectMut` (so `det`-legal); anything reaching `random_seed` is `EffectRand` (`det` refuses).

### Float math

- `floor`/`ceil`/`round` → **i64** (the fix for refused `i64(x)` on a float). Out-of-range or NaN **traps**; the check is dropped where the compiler proves the value finite and in range (`(f64(i) / 10.0).floor()` for a `u8` `i`).
- `log`, `log2`, `log10`, `sqrt`, `exp`, `exp2`, `sin`, `cos`, `tan`, `asin`, `acos`, `atan` → the receiver's own width (builtins; no libm access otherwise). `x.pow(y)` and `y.atan2(x)` take one argument of that same width. Angles are radians. Out of domain gives IEEE values (`log(0)` = `-inf`; `sqrt(-1)`, `asin(2)` and a negative base to a fractional power are NaN; `exp` overflows to `inf`).
- **`std.math`** has the constants (`PI`, `TAU`, `HALF_PI`, `E`, `SQRT_2`, `LN_2`, `LN_10`, `LOG2_E`, `LOG10_E`, `PHI` — all `f64`, so `f32(PI)` narrows) and the `f64` functions the builtins make writable: `to_radians`, `to_degrees`, `sinh`, `cosh`, `tanh`, `hypot`, `cbrt`.
- **Either spelling**: `5.0.sqrt()` or `sqrt(5.0)`, `2.0.pow(10.0)` or `pow(2.0, 10.0)` — the free form takes its receiver from the first argument and is desugared onto the method form. A declaration of the name wins over the builtin, as it does in member dispatch. `sqrt()` with no argument has no receiver and is an undefined function.
- **A `const` initializer may call them** (`const PHI = (1 + 5.0.sqrt()) / 2`), over a constant receiver and constant arguments. A `const` is inlined as its value *expression*, so the number is produced by the optimizer folding the call — at `-O0` nothing folds and the call survives at each use site. A declaration shadowing a builtin's name makes the initializer a function call and is refused (`lyra-E012`). **`sqrt` is the only one whose folded value is portable**: IEEE 754 requires it correctly rounded, while a folded `exp`/`log`/`sin`/`pow` is the *build host's* libm answer, which a cross-compile can differ on in the last place.
- `x.to_fixed(places)` (`std/prelude/format.lyra`): fixed decimals, never scientific. `print` writes the shortest round-trip form.

### Clock

`wall_clock_nanos() -> i64` = `clock_gettime(CLOCK_REALTIME)`; everything else is prelude. `EffectTime`, so `pure`/`det` refuse ambient reads; pass a timestamp as a parameter instead.

---

## 8. FFI and Unsafe

### Raw pointers

| Syntax | Meaning | Needs |
|---|---|---|
| `&x` | `^T` | `unsafe` |
| `&mut x` | `^mut T` | `unsafe`; **x** mutable |
| `p^` | read | `unsafe` |
| `p^ = v`, `p^.x = v`, `p.offset(i)^.y = v`, `p^.n += 1` | write | `unsafe`; nearest deref through `^mut T` (`lyra-E061`) |

- `unsafe { … }` block or `unsafe` function (`lyra-E011`); does not cross lambda boundaries. The block is its body (has a value; scopes its bindings). Calling an `unsafe` function also needs it.
- `^mut T` may be copied into a `let`; `^T` may point at a `var`.
- `p^ = v` **releases** the old value (as `xs[i] = v` does). Match-arm / `if let` bindings borrow, so `&mut s` and `h.s = v` on them are refused, as reassigning them already is (`lyra-E025`) — copy into a binding first.
- `&mut` on a closure-captured binding is **`lyra-E024`** (captures are by value). Watch for `s.with_cstring((p) => f(p, &mut size))` — take the pointer outside. `&n` is fine.
- Only storage has an address: `&f()` is `lyra-E059`; `^` on a non-pointer is `lyra-E060`.
- Arithmetic is `p.offset(n) -> ^T` only (elements, signed, preserves mutability). No `p[i]`.
- **`nullptr`**: safe (no `unsafe`) along with `==`/`!=` on pointers. Untyped with **no default** — context must pin the pointee (annotation, parameter, return, other side of `==`), else **`lyra-E069`**. Fills `^T` and `^mut T`; a mismatched pair pins to immutable. No `<` on pointers. `let nullptr = 5` is **`lyra-E070`** (collector).
- The only ways to make a pointer are `&` and `nullptr`.

**`std.ffi` helpers** (`unsafe` appears once in the library, not at call sites; *`unsafe` marks handing a pointer out to keep, not lending one for a call*):
- `CBuffer { ptr, len }` with bounds-checked `buf.get(i)`.
- `s.cstring()` — NUL-terminated `[]u8`; keep it alive while using its pointer.
- `xs.data()` / `xs.data_mut()` (`mut` receiver) — base address, no copy; trap on empty; dynamic arrays only.
- `with_cstring(s, f)` (`pure noalloc`, not `unsafe`), `with_cstrings(a, b, f)`.
- `is_null`, `to_maybe` — convert C's NULL convention to `Maybe` after the `extern` (a `Maybe` cannot cross: `lyra-E063`).

### Unions

`union` is an **untagged C union** (e.g. `SDL_Event`), distinct from `data` (tagged).
- All members at offset 0; size = largest member rounded to alignment (a `padding[128]` member counts).
- Reading a member is `unsafe` (`lyra-E011`).
- A literal names exactly one member (`Ev { kind: 7 }`); the rest is **zeroed**.
- Every member needs a C layout (`lyra-E071`; arrays and structs allowed — wider than `lyra-E063`). Hence a union owns nothing managed.
- No equality; no `readonly` or default on members (`lyra-E072`). Self-reference is `lyra-E014`.
- Crosses by value or pointer. Proof: `TestExec_UnionAgainstSDL3` (headless; skips without SDL3).

### Aggregates at the C boundary

A struct, union, tuple or fixed array crosses **by value**; a `data` type never does (its tag has no C type).
- Layout matches C; the per-target calling convention comes from **`pkg/abi`**, verified against clang for every shape, target and position. Targets disagree (e.g. `{i32 × 4}` is `[2 x i64]` on aarch64 but two `i64` parameters on x86-64 SysV; `{double × 3}` is registers vs. memory).
- On a target with no classifier the **backend** refuses by name; `lyrac check` answers the same on every machine.
- **The attributes go on the call site, not only the `declare`** (`byval`, `sret`): a call site's own attributes are what lower it. Dropping them passed a MEMORY aggregate's address in a register to a callee expecting a stack copy — a crash on x86-64, invisible on aarch64, where such an aggregate is passed as a plain pointer anyway (09/20).
- `examples/raylib/basic.lyra` is the proof.

### `@must_release(fn)`

On a `struct`: the value names a foreign resource released by `fn`. A binding leaving scope without that call is **`lyra-W022`**. Lyra has no destructors (would break `pure`/`det` and cross FFI ownership).
- **A borrow is not a discharge**; only passing to the named function discharges. Passing to an `own` parameter, returning, or storing untrackably is an escape.
- **A helper that releases is a release.** Passing to a parameter the callee releases — calls the release function on, directly, through a `match` payload (`Some(p) => close(p)`), or via another such helper — discharges, as the named function does. Inferred from the body, not declared. A helper that returns or stores its parameter does *not* release it, so its caller is still warned.
- `Maybe<Sound>` carries the obligation; the unwrapping alternative that sees the value must discharge. Tracked: `match` arm, `if let`, `let … else`, destructuring `let` (incl. `let Some(v) = load_sound(p) else { return }`). Patterns binding more than one name are not tracked.
- Field reads are borrows.
- Warning, not error (under-reports: a release on any branch counts; no `#[allow]`).
- `struct` only (`newtype Fd = i32` waits on the grammar). `bindings/raylib`'s `Sound` and `Wave` use it.
- **`@borrowed` on a function** says the resource it answers is someone else's: a binding of its result carries no obligation. raylib's `default_font()` is the case — its `Font` is raylib's static data. On a function whose declared result is not a `@must_release` type (or a wrapper of one) it is an error. It is the only attribute a `let` takes; any other is an error.
- **Releasing a borrowed resource is `lyra-W025`** — `unload_font(default_font())`, directly or through a binding or an unwrap: it frees what the lender still uses.

### `@symbol("Name")`

On an `extern`, names the C symbol verbatim (one per declaration); needed because Lyra identifiers are lowercase-leading (`SDL_PollEvent`). Without it the extern's name is the symbol. The backend dedupes `declare`s by symbol.
