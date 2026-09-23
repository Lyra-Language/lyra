# `pkg/types` — the type system

Every type implements `Type` (`typeNode()`, `String()`, `GetName()`). Language-level semantics of
each type are in [`LANGUAGE.md`](../../LANGUAGE.md); this file covers representation.

| Type | Notes |
|---|---|
| `PrimitiveType` | the fixed-width integers (incl. `i128`/`u128`), `f16`/`f32`/`f64`, `bool`, `string`, `rune`. Width tables live in `layout.go` (`LLVMPrimitive`, `IsSignedInt`, `IsNumericConversionTarget`, `primitiveSizeAndAlign`) plus `IsNumeric` and `assignable.go`'s int predicates — a new width must be added to all of them. `rune` is an i32: `IsSignedInt(rune)` is true, it converts to/from integers, but it is **not** `IsNumeric` (no arithmetic). |
| `PrimitiveType` (internal) | `untyped_int`, `untyped_signed_int`, `untyped_float`, `untyped_nullptr` — literal inference. A literal in `(int64max, u64max]` infers as concrete `u64` (`IntegerLiteralExpr.Unsigned`). |
| `NamedStructType`, `AnonymousStructType` | structs; `StructField` carries no documentation (docs live on the AST declaration) |
| `UnionType` | C union (`union.go`); nominal, size is max over members |
| `DataType` | sum type. A positional variant's fields are wrapped in **one anonymous `TupleType`** (`Rect(i64, i64)` → `Params [TupleType{i64,i64}]`); use `DataTypeConstructor.FieldTypes()` for the flat list |
| `LambdaType` | params and return type |
| `TupleType` | anonymous (`Name` `""`/`"?"`) or named (`tuple Point(i32, i32)`) |
| `StaticArrayType`, `DynamicArrayType` | `[N]T`, `[]T` |
| `ConstrainedType` | a `newtype` and its constraints. **Transparent to codegen** — every representation question (LLVM type, managed-ness, literal width, print) is answered against the base via `types.StripNewtype`. Nominally isolated: two newtypes over one base never interconvert. |
| `RawPointerType` | `^T` / `^mut T` |
| `WeakType` | `weak T` (`pointer.go`); pointer-sized, breaks a size cycle like `shared`. Managed with the **weak** half of the refcount protocol only. |
| `GenericType` | a type variable — a **lowercase** name; uppercase is an `UnresolvedType`. Reaching codegen unsubstituted is a loud error |
| `ParameterizedType` | a generic type applied (`Box<i64>`); two instantiations stay distinct; one LLVM type per instantiation |
| `SelfType`, `VoidType`, `NeverType` | `Self` in impls (`Substitute` binds it under `SelfVar`, at any depth; `Self<a>` applies the target's head, `ApplySelf`/`SelfApplicable`); `void`; the bottom type of `panic` |
| `UnresolvedType` | a named type not yet resolved; `Key` is stamped on names nested in a declaration's own type, since those resolve from the **declaring** module, not the reader's |
| `RangeType`, `FixedPointType` | range values; `fixed<i,f>` |

Single answers to use rather than re-derive: `StripNewtype`, `HeadName`, `Substitute`,
`CollectTypeVars`, `TypesEqual`, `IsCopiedScalar`, `IsSeq`, `IsNumeric`/`IsString`/`IsBoolean`.
Type modifiers: `Mut`, `Ref`.

## Allocation flavor

Modifiers: `Unspecified` (`""`, inherit from context, default stack), `Stack`, `Shared`.
**Allocation is a use-site flavor, never declared** — `shared struct Node {}` does not parse.

- `UnresolvedType` and `ParameterizedType` carry `Allocation` so a use-site modifier survives until
  resolution; the typechecker's `resolveType`/`resolveTypeIfKnown` apply it after lookup and
  recurse into array and tuple elements (otherwise: "cannot assign ?(Node, Node) to ?(Node, Node)").
- Read with `types.AllocationOf(t)` (`Unspecified` for primitives, generics, lambdas); override with
  `types.WithAllocation(t, mod)` (a copy, or `t` unchanged when `mod` is `Unspecified` or the type
  cannot carry one).
- **Not part of identity** — `TypesEqual`/`isAssignable` ignore it. It is a separate axis:
  `firstAllocationMismatch` + `tc.checkAllocationCompat` (`typechecker/assignable.go`) report owning
  a value across `stack`↔`shared` as `lyra-E018`, recursing into elements. Checked at annotated
  init, destructuring annotation, reassignment, interior writes, `own` arguments and non-borrow
  returns; fires only when both sides are concrete and differ. **An argument is checked
  separately** (`checkArgumentAllocation`) for exactly the pair this one exempts — one side
  Unspecified — because a parameter has one representation and no context to inherit from,
  unlike a binding. A construction, a `match` over constructions and a generic parameter
  stay polymorphic there. Not checked on the
  `LambdaType`-callee path (`inferLambdaCallFromType`), which has no parameter modes.
