# `pkg/types` — the type system

Type system. All types implement the `Type` interface (`typeNode()`, `String()`, `GetName()`).

Concrete types:

| Type | Notes |
|---|---|
| `PrimitiveType` | `i8`–`i64`, `i128`, `u8`–`u64`, `u128`, `f16`/`f32`/`f64`, `bool`, `string`, `rune` (a Unicode code point, i32, Go/Odin naming — renamed from `char` 07/21). A rune is an **ordered, convertible, non-arithmetic** scalar, the split Rust draws for `char`: `==`/`!=` and the ordering comparisons work *between two runes* (code-point order, an i32 `icmp` — this is what makes `c >= '0' && c <= '9'` expressible), and it converts explicitly to and from the **integer** types (`i32(c)`, `i64(c)`, `rune(n)` — added to `numericPrimitiveByName`/`IsNumericConversionTarget`), but it stays out of `types.IsNumeric`, so it has **no arithmetic of its own**: `c + 1` is rejected and the idiom is `i32(c) - i32('0')`, which writes the code-point/number crossing down. Comparing a rune against an *integer* likewise needs the explicit conversion. `IsSignedInt(rune)` is true — a rune lowers as a signed i32 (Go's rune *is* int32), so widening sign-extends and ordering uses the signed predicate; valid code points are non-negative, so the two readings differ only for a value built by `rune(n)` from a negative integer. Not yet: char *range* patterns (`'a'..'z'`) — the grammar's `range_pattern` bounds are number-literal-only, so classification in a `match` still needs literal arms or an `if` chain (no platform-dependent `int`/`uint`; no bare `float` — untyped literals default to `i64`/`f64`). `i128`/`u128` (added 07/27) lower to LLVM `i128` (16/16 ABI): the width tables in `layout.go` (`LLVMPrimitive`/`IsSignedInt`/`IsNumericConversionTarget`/`primitiveSizeAndAlign`), `types.IsNumeric`, and `assignable.go`'s int predicates enumerate them like the other widths, so checked arithmetic / `match` / conversions extend by width; division/`%%` go through the platform's builtins library (`__divti3`…), while the *signed* checked multiply is emitted by the compiler itself (`lyra_i128_mul_overflow`) rather than left to `llvm.smul.with.overflow.i128`, whose expansion calls compiler-rt's `__muloti4` — a symbol Linux clang does not link (`__divti3`…), 128-bit trap/saturating-bound constants are built with `big.Int` (`intMinConst`/`intMaxConst` in `intconst.go`, since `1<<127` overflows Go's int), and `print` uses a hand-written base-10 formatter (`lyra_i128_to_str`). **MVP literal gap:** `IntegerLiteralExpr.Value` is `int64`, so a >64-bit literal isn't representable — a 128-bit constant is reached via arithmetic or an `i128(x)`/`u128(x)` conversion of an i64/u64-range value (an unsigned literal in `(int64max, u64max]` converts to `u128` too). The value-range pass leaves i128/u128 ⊤ (untracked, sound) since their bounds don't fit int64, the same treatment `u64`'s upper half gets |
| `PrimitiveType` (internal) | `untyped_int`, `untyped_signed_int`, `untyped_float` — for numeric literal inference (exception: a literal in `(int64max, u64max]` is inferred as **concrete `u64`**, its only valid type — `IntegerLiteralExpr.Unsigned`, `Value` holds the int64 bit pattern; the sole negatable such literal is `2^63`, whose negation `inferNegationExpr` types as `untyped_signed_int` = `i64` min) |
| `StructType` | named struct with fields |
| `DataType` | sum type; each `DataTypeConstructor` has `Params`, but the collector wraps a positional variant's fields in a **single anonymous `TupleType`** (`Rect(i64, i64)` → `Params [TupleType{i64,i64}]`) — use `DataTypeConstructor.FieldTypes()` for the flat field list that matches a construction's positional args |
| `LambdaType` | function type with param types and return type |
| `TupleType` | anonymous (`Name` is `""`/`"?"`) or **named** tuple (`tuple Point(i32, i32)`, `Name: "Point"`) — see `TypesEqual` below for why they compare differently |
| `ArrayType` | with element type and optional size |
| `ConstrainedType` | range/literal/pattern/precision/step constraints. **Nominally isolated** (07/29): a value satisfying the base is assignable *to* a newtype (construction) and a newtype value is assignable to its base (there is no field accessor, so that is the only way to read it), but two *different* newtypes never interconvert — chaining those two rules used to make every newtype over a common base mutually assignable (`Meters` → `i64` → `Feet` type-checked silently), which is the mixup a newtype exists to prevent. **Nominal only, and transparent to codegen** (07/29): a `newtype Percent = u8` value *is* a u8 at run time — no wrapper, no tag, no LLVM type of its own — so every *representation* question (which LLVM type, is it refcount-managed, an untyped literal's width, how print formats it) is answered against the base via **`types.StripNewtype`**. The base is checked too: an out-of-range constant (`let s: Small = 300` on a u8 base) is an error, reported by `checkIntegerLiteralRange` unless the newtype declares its own `range(…)`, in which case that constraint owns the report (`lyra-E023`) so one mistake yields one diagnostic. **Open:** a *chained* newtype (`newtype UserId = Id` where `Id` is itself one) doesn't type-check — `isAssignable` has no symbol table, so it compares against the still-unresolved base name; codegen already handles the chain |
| `PointerType` | raw pointer |
| `WeakType` | a non-owning `weak T` reference (`pointer.go`) — pointer-sized, so it breaks a recursive **size** cycle like `shared` (E014), and lowered as the box pointer (an opaque `i8*`). **Runtime semantics landed 07/29** (`backend/llvm/weak.go`): created by `x.weak()` on a `shared` receiver, read *only* through `if let s = w { … }`, which upgrades it to a real `shared T` when the referent is still alive. It **is** managed — it holds the box's *weak* count, so a copy takes one and a death gives one back — but with the weak half of the protocol (`lyra_rc_weak_retain`/`_release`), never the strong half: a weak reference owns the storage, never the value. The **upgraded** strong reference, by contrast, exists only on the path the upgrade succeeded on, so both its name and its release live in a **branch-scoped** local scope + managed frame released at the end of the then-branch. Framing it in the enclosing scope instead puts the release in the *merge* block, which the failure path also reaches — and on that path nothing was ever stored, so it releases an uninitialized alloca (the same shape as the `[head, ...tail]` guard bug: a conditional binding needs a branch-scoped frame). Fixed 07/29 after it reached CI: **macOS ASan did not flag it and Linux did**, so the regression test asserts it against the emitted **CFG** — no block reachable from the failed-upgrade edge may load the upgraded slot — rather than by running the program, since releasing an uninitialized alloca is undefined and therefore not reliably observable (`TestEmit_WeakUpgradeBindingIsBranchScoped`). **Still open:** a `weak` *field* is unconstructible, because a field must be initialized and there is no empty weak — so the cycle-breaking use needs `Maybe<weak T>` (generics) or a nullable weak |
| `GenericType` | type variable (e.g. `T`) — a **lowercase** type name in a signature or declaration; an uppercase one is a concrete `UnresolvedType`. It has no representation, so reaching codegen unsubstituted is a loud error |
| `ParameterizedType` | a generic type applied to arguments (`Box<i64>`, `Maybe<string>`) — what a generic type's *construction* evaluates to, so field/element reads resolve to the argument's type and two instantiations of one declaration stay distinct. `String()` renders the applied form; `Allocation` is a use-site flavor and is **not** part of nominal identity. The backend emits one LLVM type per instantiation (see Generic types) |
| `SelfType` | the `Self` type inside trait impls |
| `VoidType` | `void` |
| `UnresolvedType` | placeholder for a named type not yet resolved |

Helper predicates: `types.IsNumeric(t)`, `types.IsString(t)`, `types.IsBoolean(t)`.

Allocation modifiers: `Unspecified` (`""`, the zero value / "inherit from context, default to
stack"), `Stack`, `Shared` (the old `None` was retired — it conflated unspecified with stack and
was applied only to arrays). **Allocation is a use-site flavor, never declared.** There is no
declaration-level modifier (`shared struct Node {}` does not parse); a value is flavored only
where it's used (`let n: shared Node`, a `shared` field, a `shared` param/return).
`UnresolvedType` and `ParameterizedType` carry an `Allocation` field so usage-site modifiers
survive in the AST until resolution. Read a type's flavor via `types.AllocationOf(t)` (covers
`NamedStructType`, `DataType`, `TupleType`, `StaticArrayType`, `DynamicArrayType`,
`ParameterizedType`, `UnresolvedType`; returns `Unspecified` for types that can't carry one —
primitives, generics, lambdas). Override a type's flavor via `types.WithAllocation(t, mod)` —
returns a copy with `Allocation` set, or `t` unchanged when `mod == Unspecified` or the type
can't carry a flavor. `resolveType`/`resolveTypeIfKnown` in the typechecker apply any
`UnresolvedType.Allocation` override onto the resolved type after the name lookup, and recurse
into array element and tuple element types so a named element (`[N]Node`, `(Node, Node)`)
resolves too — without that, an annotation keeps an `UnresolvedType` element and assignability
fails with a confusing "cannot assign ?(Node, Node) to ?(Node, Node)". Allocation is *not* part
of nominal identity — `TypesEqual`/`isAssignable` ignore it (see todo #5's
allocation-as-type-identity model). It is instead a *separate compatibility axis* checked
alongside assignability: `firstAllocationMismatch` (`typechecker/assignable.go`) +
`tc.checkAllocationCompat` flag *owning* a value across a concrete flavor boundary
(`stack`↔`shared`) as `lyra-E018` ("converting allocation is an explicit operation"). The walker
checks the top-level flavor and recurses structurally into array/tuple element types, returning
the offending pair so the message names the actual clashing flavors even several levels down.
Sites: annotated `let`/`var` init, destructuring-decl annotation, reassignment, interior lvalue
write, plus call arguments and returns gated by mode (`paramOwnsArgument`/`isOwnedReturn`): only
an `own` parameter adopts the argument (bare/`ref`/`mut` borrows are allocation-polymorphic and
skipped), and a return is checked unless its type carries a `ref`/`mut` borrow modifier —
matching FP/Imperative todo #5 Decision (b). Conservative: fires only when both sides carry a
concrete, differing flavor; `Unspecified` is polymorphic (inherits context). Not covered: the
`LambdaType`-callee path (`inferLambdaCallFromType`) — the collector never populates a param
mode for lambda-*type* params, so there's no ownership info to gate on; and array element-level
flavor isn't expressible in surface syntax yet (`[N]shared T` mis-parses). Type modifiers:
`Mut`, `Ref`.
