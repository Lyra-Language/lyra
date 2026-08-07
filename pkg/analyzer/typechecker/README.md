# `pkg/analyzer/typechecker` — inference and checking

Walks the collected AST and infers/verifies types, writing results into a `TypeTable`.

**Entry point:** `typechecker.New(symTable, scopeTable, typeTable)` → `tc.Check(program)
[]TypeError`

**`TypeError`** has `Message string`, `Location ast.Location`, `Severity` (`SeverityError` /
`SeverityWarning`).

## Key methods

### `inferExprType(expr)`

Returns the `types.Type` for an expression and records it in the `TypeTable`

### `propagateLiteralType(expr, concrete)`

**context-directed literal-width inference.** Bottom-up inference computes each expression's
result type but leaves an untyped literal (`5`, `3`) recorded as `untyped_int` until context
fixes its width. This helper pushes a concrete numeric width *down* onto untyped int/float
literal leaves, recursing through width-preserving arithmetic (`+ - * / % %%`, unary `-`),
through the branch bodies of an `if`/`match`/block (each arm/branch/last-statement is the
value), and — against a `TupleType` context — **element-wise into a tuple literal** (`(20, 22)`
vs `(u8, u8)` narrows each leaf, recursing into a nested tuple), **re-recording an anonymous
literal's own type** at the context's element widths as the array case does (fixed 07/30:
narrowing the leaves alone left the tuple *node* recorded at the untyped default, and the
backend builds the aggregate from that — so `f((10, 40))` against a `(u8, u8)` parameter emitted
`call i8 @f({ i64, i64 })` into a `{ i8, i8 }` parameter. Invalid IR, invisible to a modern
clang: opaque pointers make the two function types indistinguishable and arm64 passes small
structs in registers, so it produced the right answer anyway. Found by `./asan.sh`, whose older
typed-pointer clang rejects it. Anonymous only — a named tuple is nominal and already recorded
against its declaration, possibly as a generic instantiation), and stopping at
identifiers/calls/conversions (a conversion `i8(x)` is exactly where a new width begins). It
narrows a leaf **only when the value fits** the target width — a literal that doesn't (`i8(x) <
300`) is left untyped, so overflow surfaces loudly (the fold-based `checkIntegerLiteralRange`,
or a backend width mismatch) rather than silently wrapping, and propagation never double-reports
the overflow the range check owns. An int literal in a **float** context (`let x: f64 = 5`, an
argument against a float param, a float field/payload/return) is recorded at the float type the
same way — the backend then lowers it as a float constant (fixed 07/29: this case previously
bailed as "handled by assignability", so the leaf fell back to i64 and put an integer value in a
float slot — `print` showed garbage and float arithmetic emitted invalid IR only clang caught).
**Exception — a signed type's minimum written as a negated literal**
(`-128`/`-32768`/`-2147483648` for i8/i16/i32): the operand's positive magnitude `2^(bits-1)`
doesn't fit as a positive value, but the *negation* is exactly the type's min, so the
`NegationExpr` case narrows the operand leaf directly (via `signedTypeMinMagnitude`,
`overflow.go`) instead of bailing to i64 — the narrow-width analogue of `inferNegationExpr`'s
i64-min handling (i64's `2^63` overflows int64 and stays on that Unsigned path). Without it `let
a: i8 = -128` lowered an i64 value into an i8 slot and emitted invalid IR in typed arithmetic.
**A newtype context propagates its base** (07/29): `newtype Percent = u8` is nominal only, so
`let p: Percent = 40 + 2` narrows its leaves to u8 exactly as an annotated u8 would — without it
the leaves stayed untyped, the arithmetic lowered at the signed i64 default, and `let s: Small =
200 + 100` silently produced 44 where the same expression against a bare u8 traps. Called from
nine context sites: annotated `let` (`checkVarDecl`), a `MathBinaryOp` with a concrete result
(`inferMathBinaryExpr`, via `propagateOperandType` — which skips an operand that already *is*
the result type, since a nested arithmetic node's own inference already narrowed its subtree;
without it a deep/flat `a + b + … + z` chain re-descended at every level, quadratically),
numeric comparisons/`==` (`propagateComparisonWidth`, using the operands' common type since a
comparison's own result is bool), `var` reassignment (`checkVarReassignment`), the lambda/entry
return body (`checkLambdaBody`/`checkBlockReturn`), a call argument against its resolved
parameter type (`inferLambdaCall`), a named-tuple literal's element against its declared element
type (`inferNamedTupleLiteralExpr`), a struct literal's field value against its declared field
type (`inferStructInstanceExpr`), a data-constructor argument against its declared payload-field
type (`inferTupleLiteralExpr`'s data-constructor branch, via
`types.DataTypeConstructor.FieldTypes()`), and each `match` arm body against the arms' common
type (`checkMatchExpr`, so a bare `0` arm adapts to a concrete sibling or the match's outer
context rather than defaulting to i64). The backend reads these recorded leaf widths.

### `propagateAllocation(expr, mod)`

The allocation-flavor analogue of `propagateLiteralType`: pushes a context `shared` flavor down
onto the *construction* leaves (a data constructor, struct instance, or named-tuple literal)
that produce the value, recursing through the same `match`/`if`/block arm structure. Allocation
is a use-site flavor, so only a construction — whose flavor is context-determined — is stamped
(via `WithAllocation`); an identifier or call already carries its own definite flavor. Called
from the annotated `let` (`checkVarDecl`) and the three return-body sites
(`checkLambdaBody`/`checkBlockReturn`), so `(xs) -> shared List => match xs { … => Cons(…) }`
heap-boxes the value the arm builds (and the ownership pass then sees it as a managed /
reuse-eligible value). This closes the alloc-detection half of the deferred "`shared`
construction in a return position" gap for these contexts; a bare-argument-position construction
still isn't stamped.
- `inferTupleLiteralExpr` / `inferNamedTupleLiteralExpr` — a `TupleLiteralExpr` (`(1, 2)` or
  `Point(1, 2)`, both the same AST node — call syntax on a capitalized name is the only
  applied-constructor form, see the grammar notes below) splits three ways: a data-constructor
  name (`Some(42)`) resolves to the owning `DataType` via `findDataTypeByConstructor`; `Name ==
  "?"` (the collector's placeholder for no leading name) is a plain anonymous tuple, structural
  as always — its element leaves are left **untyped** (not eagerly promoted to i64/f64),
  mirroring `inferArrayLiteralType`, so a surrounding context (a tuple annotation, a
  data-ctor/struct tuple field) can narrow them via `propagateLiteralType`; with no such context
  they settle to their defaults at the no-annotation site (`promoteToDefault` now has a
  `TupleType` case). This is what makes `let a: (i32, i32) = (1, 2)` and a tuple-typed data
  payload (`Wrapped((20, 22))`) narrow correctly; `isAssignable` gained a structural
  anonymous-tuple→anonymous-tuple case so the widened literal is accepted. any other name is a
  **named tuple**, which is nominal (`todo.md` Pit-of-Success #8: "positional nominal", matching
  `NamedStructType`) and is delegated to `inferNamedTupleLiteralExpr`. That function requires
  the name to resolve to a declared `tuple Point(i32, i32)` in `symTable.Types` (mirroring
  `inferStructInstanceExpr` for structs — construction is validated against the declaration, not
  synthesized freely from the literal), then checks arity and each element positionally against
  the declared (turbofish-substituted, or left-unconstrained-if-still-generic) element type —
  propagating that declared type onto an untyped literal element (`propagateLiteralType`) before
  checking assignability, the same treatment `inferLambdaCall` gives a call argument. **Not yet
  done:** per-position generic *inference* when no turbofish is given (structs infer a field's
  generic param from the supplied value; named tuples don't attempt the positional analogue yet
  — an unbound generic-typed position is just left unconstrained). `types.TypesEqual`'s
  `TupleType` case mirrors this split: a named tuple (`!types.IsAnonymousTupleName(t.Name)` on
  either side) compares by name alone; both anonymous compares structurally (element-wise) as
  before.
- `inferTupleIndexExprType` — positional tuple access (`pair.0`, the `TupleIndexExpr` node from
  the grammar's `tuple_index_expr`). Resolves the object to a `TupleType` (via `resolveType`,
  since a tuple-typed binding may be recorded as an `UnresolvedType`), bounds-checks the
  (zero-based) index against the arity, and returns the indexed element's resolved type — the
  positional counterpart to `inferMemberExprType`. Works for both named and anonymous tuples
  (both carry `Elements`). Errors: index out of range, or access on a non-tuple.
- **Declared types that *name* a type are resolved where they are read out** (07/29) — a call's
  declared return type (`inferLambdaCall`/`inferLambdaCallFromType`, via `resolveTypeIfKnown`,
  matching how each parameter type was already resolved) and a struct field's declared type
  (`inferMemberExprType`). Both were returned raw, and a raw `UnresolvedType` compares unequal
  to the same type resolved from an annotation, so a named type could not survive a round trip
  through a function or a field read: `let p: Point = mk()` reported the tell-tale **"cannot
  assign Point to Point"**, and the newtype analogue made a newtype unusable across any call
  boundary. Distinctness is unaffected — resolving both sides is what lets `TypesEqual` compare
  them at all, so a `Meters`-returning call is still rejected against a `Feet` annotation.
- **`resolveTypeIfKnown` is `resolveType`'s twin, and since 08/05 they are one walk.**
  It exists only to skip the "unknown type" diagnostic where a caller would duplicate it
  (the return annotation in `checkLambdaBody`), so any composite the one walks the other
  must too — and it had drifted by `ParameterizedType` and `*LambdaType`, the argument-list
  pair hazard 8 names, with the same tell-tale self-rejection confined to return position:
  **"return type mismatch: expected `Maybe<weak Node>`, got `Maybe<weak Node>`"** (08/03).
  Both now delegate to **`resolveTypeWith(t, loc, leaf)`**, which owns the composite
  recursion, so a composite added later cannot reach one and miss the other. The leaves are
  where they genuinely differ, and by more than whether they report: the reporting leaf also
  follows alias chains, caches by resolved identity, checks visibility and guards
  circularity, none of which the quiet one does. `tests/named_type_in_composite_test.go`
  covers both, including every composite in return position as a guard on the fold.

### `propagateInstantiation(expr, want)`

The generic-type analogue of the two above: pushes a context's `ParameterizedType` down
onto the construction leaves that produce the value, through the same match/if/block arm
structure. It exists because a construction only evaluates to an instantiation when it
solves **every** type parameter itself — `Some(v)` fixes `t`, but `None` fixes nothing
and `Ok(v)` fixes `t` and not `e`, so both stay the bare declaration (deliberately:
inventing an instantiation from a partial substitution would claim precision the
construction did not supply). Before this they lowered only under an annotated `let`, the
one site that stamped its type onto the value wholesale, so `-> Maybe<i64> => None`
failed the build with `unknown named type "Maybe"` and the prelude's `Result` was
unusable in a return position. Called from the annotated `let`, the three return-body
sites, the concrete call-argument site, and the *generic* call's argument site
(`instantiate.go`) — the last is what makes `unwrap_or(None, 42)` work, since the
parameter is only `Maybe<i64>` once another argument has solved `t`.

**It checks rather than assumes.** A partly solved construction's payload was not
verified against the context at all, so `let r: Result<i64, string> = Ok("x")` passed the
front end and was caught only by the backend refusing to store a string into an i64
payload — a type error in the wrong layer, which survived only because the value could
not lower. Each payload element is re-checked under the context's substitution, and the
node is left bare on a mismatch so a wrong payload can never lower as that instantiation.
Three guards decide whether the stamp applies: the node must be **open** to a context, it
must be the same declaration the context names, and the arities must agree.

**Open means one of two things** (`stampableDataType`), and the second was missing until
08/03. The bare declaration is open, as above. So is an instantiation the construction
reached *by defaulting an untyped payload literal* — `Some 7` is a `Maybe<i64>` only
because an untyped 7 defaults to i64, which is the expression's guess rather than the
program's decision, and a guess must not outrank `let m: Maybe<u8> = Some 7`. That
annotation was rejected with "cannot assign Maybe<i64> to Maybe<u8>" until such nodes were
marked (`markDefaultedConstruction`) and the leaf left untyped for the context to narrow.
Everything else stays closed, which is the load-bearing half: an instantiation the program
determined has already been checked by ordinary assignability, and overriding it would let
a real mismatch through.

The line runs through the **declared field**, not through the payload
(`fieldTakesWidthFromSolve`): a field that is a type variable takes its width from the
substitution and may be deferred, while a concrete one (`Wrapped(u8)`) takes it from the
declaration and must be narrowed on the spot. Deferring both was the first version of that
fix, and it type-checked fine — the symptom appeared in the backend, storing an i64 into a
u8 slot. A narrowed literal is also range-checked against what it was narrowed *to*, since
assignability cannot catch a 300 that has just been given the type u8. (Tuple and array
narrowing still skip that check — see `todo.md`'s Known bugs.) Covers data constructors, generic **structs** and named **tuples**. The aggregates needed a
second pass because they fail differently: a bare `DataType` is assignable to any
instantiation of itself (so a partly solved data construction reached the backend), while a
bare `NamedStructType`/`TupleType` is not (so a partly solved one was rejected up front with
"return type mismatch … got Tagged", a spurious error on correct code). That is why every
context site goes through **`contextualType`**, which propagates *before* the assignability
check, re-reads the record, and reports whether it already emitted a diagnostic so the
caller suppresses its own coarser one.

### `checkNode(node)`

/ `checkVarDecl` / `checkVarReassignment` / `checkExpressionStmt` — statement-level checks.
**Assignment to a parameter** goes through the same path: `checkAssignToBinding` resolves
`tc.paramTypes` *before* the scope lookup (a parameter is not a `VarDeclStmt` in scope, and
shadows any outer binding of that name), then shares `checkAssignedValue` with the variable path
so both apply identical rules. Before 07/29 it bailed at the failed scope lookup, which left `n
= …` on a parameter **completely unchecked** — no assignability or literal-range check, and,
since the RHS was never inferred, not even an undefined-identifier report, plus no recorded
types for the backend: `n = n + 1` then failed the build with "type not found for
*ast.IdentifierExpr" (only integer arithmetic tripped it — a literal RHS needs no recorded type
and the float path doesn't consult signedness) and `n = "s"` on an `i64` parameter **panicked**
the backend on a mismatched store. **Reassignment is permitted only for `own` and `mut`
parameters** (`lyra-E025`, 07/30): `own` transfers the value to the callee, so rebinding its own
copy is meaningful, and `mut` is a reference to the caller's storage, which is what that
modifier means (a `mut` *scalar* stays by value and so doesn't propagate — exactly the case
`lyra-W010` flags as inert). A **borrowed** parameter — no modifier, or `ref` — is rejected: the
caller still owns the value, so the write could only reach the callee's copy and vanish, the
same lost-write class as assigning to a captured binding (`lyra-E024`) and the by-value `mut`
parameter that silently dropped its writes. It was also inconsistent with the binding model,
since `let x = 5; x = 6` is an error while a bare parameter accepted exactly that — making a
parameter the most permissive rung with no syntax for the immutable one (Swift removed `var`
parameters over the same confusion, SE-0003; Rust requires opt-in and keeps it local). The
replacement is **shadowing** (`let s = s ++ "!"`), which required teaching the
use-before-declaration checker that a parameter is in scope for the whole body
(`checkStatementsInScope`) — without that the derived form read as a use before declaration. The
same rule and code cover a **pattern binding** (a match-arm or `if let` name), which borrows
from the value being matched; it gets its own wording, since calling it a parameter would name
something the source doesn't contain. Removing this also **deleted a leak**: a borrowed
parameter reassigned to a managed value left the new value unreleased, and that program no
longer compiles. **Sequential-rebind self-reference:** the collector's `RedefineVariable`
overwrites a same-scope binding, so inside `let x = x + 1` the name `x` resolves in scope to the
declaration being defined (itself, not yet typed). To type the RHS against the *prior* value,
the collector records the replaced binding as `VarDeclStmt.Shadows`, and `checkVarDecl` sets
`tc.currentVarDecl` around inferring the initializer; the `IdentifierExpr` case redirects a
lookup that lands on `currentVarDecl` to its `.Shadows`. Without this the RHS inferred nil —
silently masked elsewhere by nil-guards, but it broke any consumer that *reads* the recorded
type (e.g. the LLVM backend's `getIntSignedness`).
- `checkIfDestructuringStmt` / `checkElseDestructuringStmt` — type-check `if let`/`let … else`
  bodies, reusing `checkDestructuringDecl` to bind pattern names with the right scope (if-let's
  names are local to `Then`, entered via `enterScope` against a scope the collector pushed and
  recorded against the `*ast.IfDestructuringStmt` node itself; let-else's persist in the
  enclosing scope, like a plain `let`)
- `assignable.go` — `effectiveType` and unification logic for type compatibility

**Destructuring parameters** — `((a, b): (i64, i64))`, `({ x, y }: Pt)` — bind their names in
`withParamScope`, which walks the pattern against the parameter's *annotation* with
`walkDestructuredPattern`, the same walker `checkDestructuringDecl` uses. Statically, so it
happens up front rather than lazily during body-checking. An **unannotated** one is skipped and
its names stay undefined, which is the honest outcome for a free function: there is no type to
destructure against. The exception is a **trait-impl method** (`checkTraitImplMethodBody`),
where the trait's signature supplies the type, so `total = ({ x, y }) => x + y` binds without
the impl writing an annotation the trait already gave. Before 07/31 that path bound identifier
patterns only, and reported every destructured name undefined.

### `resolveTraitMethod(receiverType, methodName, requiredTrait)`

(`typechecker_trait_dispatch.go`) — finds every impl whose target type matches `receiverType`
(`implTargetMatches`) providing that method, optionally restricted to one trait; multiple
matches with no `requiredTrait` is the "two traits, same method name" ambiguity a
fully-qualified `Trait::method(...)` call resolves. Drives both `inferMemberCall`'s fallback
(after struct-field lookup fails) and `inferTraitMethodPathCall`. Records each resolution in
`tc.MethodTable()` for the purity checker. **Generic impls dispatch** (`impl Show<t> for
Box<t>`): a target containing lowercase `GenericType`s (Lyra's implicit type variables — an
uppercase name is concrete) matches when it *unifies* with the receiver (`unifyGenericTarget`),
each variable binding to the receiver's corresponding subterm, with binding-consistency
(`Pair<t,t>` accepts `Pair<i64,i64>`, rejects `Pair<i64,string>`); targets can be parameterized,
array, or tuple. `Self` is substituted with the concrete receiver, so a Show/Debug/Hash-style
method (signature in terms of Self + concrete types) type-checks against the instantiation.
**Bounded impls are constraint-checked:** for `impl Ord<t> for Box<t> where t: Ord` dispatched
on `Box<Widget>`, `unifyGenericTarget` binds `t`→Widget and `checkImplConstraints` verifies each
`where` bound holds for the binding (via `typeImplementsTrait`, itself an `implTargetMatches`
search) — Widget with no `Ord` impl errors; `Box<i64>` with `impl Ord for i64` is accepted. The
bound check is single-level (a satisfying impl's *own* `where` bounds aren't recursively
re-verified). **Generic struct field access** works via `resolveGenericStruct`
(`typechecker.go`): member access on a `ParameterizedType` naming a generic struct resolves it
to the struct with its type arguments substituted into the field types (`substituteGenerics`) —
`Box<i64>.value` → `i64`, and `self.value` inside a generic impl body → the parameter `t`. This
is applied only to the field-lookup side of member access; trait dispatch keeps the original
`ParameterizedType` (the unifier needs its type arguments). The struct's generic-parameter
*names* are read from `decl.GenericParams` (the `TypeDeclStmt`), since
`NamedStructType.GenericParams` is not populated by the collector. **Bounded polymorphism in
method bodies** (`dispatchViaGenericBound`): calling a trait method on a value whose type is a
bare parameter (`self.value.show()`, `self.value : t`) dispatches through the parameter's
in-scope `where` bound. `checkTraitImpl` loads the impl's `where` constraints into
`tc.genericBounds` (param name → trait names) around its body checks; `inferMemberCall`, on a
`GenericType` receiver, looks up a bound trait declaring the method and type-checks the call
against that trait's signature with Self = the parameter (so a Self-returning bound method
yields `t`). This is *abstract* dispatch — no concrete impl exists here (it's chosen when the
enclosing generic is instantiated, where `checkImplConstraints` has already verified the bound).
It's recorded in the MethodTable as a `BoundMethodRef` (trait + method name) via
`SetBound`/`GetBound`, so the purity checker scores it as the **join over every impl of the
bound method** (`boundCallEffect` in `purity.go`: pure/det only if *all* impls are — the bound
admits any of them) rather than as an unverifiable external call. No bound → an actionable
error. **A trait's own type parameters are bound** (`trait Get<e> { get: (self) -> e }`, `impl
Get<t> for Box<t>`, `box.get()` on `Box<i64>` → `i64`): the impl's trait arguments (`Get<t>` →
`[t]`) are collected into `TraitImplStmt.TraitArgs`, and `resolveTraitMethod` builds a
substitution from each trait param (`e`) to the impl's positional arg (`t`) resolved through the
receiver bindings (`{t: i64}`), applying it to the method signature via `substituteSigGenerics`
(params and return). So `-> e` becomes `-> i64`, and an `e`-typed parameter is checked against
the concrete arg. (Note: the impl's `<…>` grammar field labels every child with one field name,
so `TraitArgs` is collected with `FieldNameForChild` iteration; `impl.GenericParams` stays empty
— it expects `generic_parameter` nodes — and the target's own type variables are read off the
target itself. The `where`-clause bounds are in `impl.Constraints`.)

**Builtin methods** (`builtins.go`): compiler-provided methods on primitive receivers, e.g.
`x.wrapping_add(y)` on integers. `builtinMethodSignature(recv, name)` returns a `*LambdaType`
specialized to the receiver (parameters are the *call* args only — `self` is the implicit
receiver), consulted by `inferMemberCall` **last**, after struct-field and trait-method
resolution miss, so a user type or trait impl always shadows a builtin. Currently registered:
the integer overflow-arithmetic ops `wrapping_{add,sub,mul}` / `saturating_{add,sub,mul}`
(`(self: T, other: T) -> T` for a concrete integer T) — the "somewhere to live" for
Pit-of-Success #2, a registry (NOT a prelude). A primitive is therefore a valid method receiver;
a missing method on one reports `T has no method "x"`. These are the explicit escape hatches
from checked-by-default arithmetic and **lower in the backend** (`wrapping.go`): wrapping = raw
two's-complement `add`/`sub`/`mul`; saturating add/sub = `llvm.{s,u}{add,sub}.sat`; saturating
mul = a `with.overflow` multiply + a `select` to the bound (LLVM has no plain `{s,u}mul.sat`).
Signedness comes from the receiver's type. Also registered: `floatRoundingOps` —
`floor`/`ceil`/`round`, float-receiver-only, zero call args, fixed `i64` return type (mirrors
the untyped-literal-default pattern rather than inferring a narrower width from context — narrow
further via the existing explicit int conversion, `i32(x.floor())`). This is the explicit escape
hatch `inferTypeConversion`'s float→int rejection points callers to; the backend lowers each to
a lazily-declared `llvm.<op>.<width>` intrinsic (`rounding.go`) + `fptosi`. Also registered:
**`len`** on any array receiver (fixed-size or dynamic) → i64, no args; the backend
`lowerArrayLen` (`dynarray.go`) returns the compile-time size for a `[N]T` and loads the box's
`len` field for a `[]T`. `checked_*` and the `truncate`/`saturate`/`narrow` conversions are not
registered yet (see `todo.md` #2/#5) — they share the rounding builtins' still-open "return type
from context" problem.

**Builtin functions** (`builtins.go` `isBuiltinPrintFn`/`isPrintableType`, resolution in
`typechecker_functions.go` `inferPrintCall`): the free-function analogue of the builtin methods
— compiler-provided functions resolved by name in `inferIdentifierCall` **only after** scope
resolution misses, so a user `let print = …` shadows them. `print`/`println` are **polymorphic
over the printable scalar types** (string, any integer/float, bool, rune → void) rather than a
single `LambdaType`, so `inferPrintCall` checks the one argument against `isPrintableType` and
settles an untyped numeric literal to its default width (`propagateLiteralType(arg,
promoteToDefault(argType))`) so the backend has a concrete type to format. Effect classification
`EffectOutput` lives separately in `checker/effects.go`'s `builtinEffects` — allowed in `det`,
forbidden in `pure`. The backend lowers per-type formatting to libc `write`/`snprintf` + a rune
UTF-8 encoder (see `pkg/backend/llvm`). Aggregates aren't printable (no Show/Display trait yet).

**Newtype constraint enforcement**: a value assigned/annotated to a constrained newtype
(`*ConstrainedType`) is checked against its constraints at the assignment sites (`checkVarDecl`,
`checkVarReassignment`, and the member-assign path). `checkPatternConstraints` tests a **string
literal** against a `PatternConstraint` (regex membership — the constraint stores the regex
literal's full source text, which `regexPatternBody` strips of its `r"…"` delimiters before
compiling; the syntax changed from `r/…/` on 07/29 to kill an ambiguity with division, see
`tree-sitter-lyra/CLAUDE.md`); **`checkRangeConstraints`** (`range_constraint.go`, `lyra-E023`)
tests a **compile-time numeric constant** (int or float literal, incl. negation / a folded
arithmetic constant) against a `RangeConstraint` — inclusive start, `..<` exclusive / `..<=`
inclusive end, either bound optional (`0..`, `..<=100`); bounds are folded from the constraint's
literal/negated-literal `MathConstraintExpr` (`foldConstraintInt`/`Float`, an unfoldable
identifier/compound bound leaves that side unenforced). Both are compile-time, definite-only
checks over constants; a non-constant value proven out of range by flow is caught by the range
analysis's `checkConstraintViolation` (`lyra-E023`, same code), which scopes to *identifier*
values so the two never double-report. `checkIntegerLiteralRange` **checks a newtype against its
base** — a Percent value is a u8 and cannot hold 300 either — but only when the newtype declares
no `range(…)` of its own, in which case `checkRangeConstraints` owns the report (the constraint
is ⊆ the base, so a range violation subsumes base overflow and reporting both would double up on
one mistake). Before 07/29 it skipped a `*ConstrainedType` outright, so an *unconstrained*
newtype — the common `newtype Meters = i64` shape — had no range check at all and an
out-of-range constant reached codegen to be silently truncated into the base's width.

Files split by concern: `typechecker.go` (core + var decls + expressions),
`typechecker_control_flow.go` (if/match), `typechecker_functions.go` (lambda/call/member-call
dispatch), `typechecker_trait_dispatch.go` (trait-method resolution), `typechecker_traits.go`
(impl conformance), `builtins.go` (builtin methods on primitives), `range_constraint.go`
(`RangeConstraint` value enforcement, `lyra-E023`), `errors.go` (error helpers), `assignable.go`
(type compatibility).

## Generic functions (instantiation + monomorphization)
A generic function declares lowercase type variables in its signature (`let identity = (x: t) ->
t => x`; the collector turns a lowercase type name into a `types.GenericType`, an uppercase one
into a concrete `UnresolvedType`). Two halves make it work, and both landed 07/29 — before this
a generic function did not even type-check, since a declared `t` is assignable from nothing
until it is bound.

**Instantiation** (`typechecker/instantiate.go`): at a call site, each declared parameter type
is unified against the argument's inferred type to solve the variables (`unifyGenericTarget` —
the *same* unifier trait dispatch uses, so "what does this type variable match" has one
definition), the call is checked against the substituted signature, and the result is the
substituted return type. An **untyped literal** argument settles to its default width before
binding (`identity(7)` gives `t = i64`), because a type variable is a real type in the
specialization — it decides an alloca's width and an instruction's signedness — so leaving it
untyped would push an unresolved literal type into codegen; a narrower width is reached by
saying so at the call (`identity(u8(7))`). Every variable in the signature must be solved: one
appearing only in the *return* type is reported at the call rather than discovered during
lowering. Arity is checked first, since a missing argument is exactly a variable with nothing to
bind it.

**Monomorphization** (`backend/llvm/monomorphize.go`): one emitted function per distinct
instantiation (`identity$i64`, `identity$boolean`), keyed by the instantiation's stable `Key()`
so two call sites solving to the same bindings share one function; the bare generic name is
never emitted, and an uninstantiated generic function costs nothing. It works **by substitution,
not by cloning the AST**: the same shared body is lowered once per binding set with a
substitution installed on the lowerer, consulted by the two accessors every lowering decision
already funnels through — `lowerType` for a type written in the source, `recordedType` for one
read off the TypeTable. That is enough to make the whole body concrete, including its locals'
widths and its arithmetic's signedness. Cloning per instantiation would mean deep-copying every
node and then re-typechecking each copy or hand-patching a parallel TypeTable — far more
machinery, and two ways for a specialization to disagree with the body it came from.
`defineFunctionInto` is shared with the ordinary function path so parameter binding, `own`-param
framing, and the void/typed return split cannot drift between a generic function and a plain
one.

**A managed type argument works**, because the **ownership pass runs once per instantiation**
(`ownership.AnalyzeLambda`, `driver.Result.OwnershipBySpec`, keyed by the instantiation's
`Key()`). That is not an optimization but a correctness requirement: every decision that pass
makes turns on whether a value is reference-counted, which is a property of the type *argument*.
Analyzed generically, a type variable is not managed, so `pick(a: t, b: t) -> t` recorded no
retain on its result and no release for the caller's temporaries — correct at `t = i64`, a
double free at `t = string` (measured: an ASan abort, 2 allocations against 3 releases). The
tables **cannot be merged**: they are keyed by AST node, and the same node carries different
annotations per instantiation — which is exactly the information one shared table could not
hold. The backend reads them through one accessor (`l.ownership()`), which returns the
specialization's table inside a generic body and the program-wide one elsewhere; the pass itself
applies the substitution at its single type lookup (`analyzer.typeOf`).

**The one remaining boundary is deliberate:** an **unbounded** type variable supports only what
every type supports — being passed, returned, stored. `x + x` on a `t` is rejected, correctly,
since `t` could be `bool`; arithmetic needs bounded polymorphism over an operator trait (`where
t: Add`), which does not exist. Still deferred: a generic function calling another at a
variable-dependent instantiation. (Multi-clause generic functions *do* work as of 07/31 —
they are desugared into a single-body match before instantiation, so nothing downstream sees
a clause list.)

## Generic types (`Box<t>`, `Maybe<t>`, `List<t>`)
Landed 07/29, and they **compose** with generic functions — `let wrap = (x: t) -> Box<t> => Box
{ value: x }` works, the two monomorphizers cooperating. All three aggregate shapes are covered:
a generic `struct`, a generic `data` type (including a recursive one), and a generic named
`tuple`.

**Front end.** The substitution was already being solved to check a construction's fields and
then *discarded*, which is what made a generic type unusable from both ends: the raw declaration
keeps `value: t`, so a field read returned the type *variable* ("cannot convert t to u8") and an
annotated binding compared the declaration against the annotation and reported the tell-tale
**"cannot assign Box to Box"**. A construction now evaluates to *that instantiation* — a
`types.ParameterizedType` carrying the solved arguments (`parameterizedResult`) — for all three
shapes: a struct infers each parameter from the field values, while a **data constructor** and a
**named tuple** solve theirs *positionally* from the supplied payload/elements
(`solveDataTypeVars`, reusing the same `unifyGenericTarget` a generic call and trait dispatch
use, with an untyped literal settled to its default width first for the same reason). The
named-tuple case had been deferred on the grounds that positions carry no field names to key
inference on; a data constructor is positional too, so the rule was already well defined. A
*partly* solved substitution deliberately does **not** become an instantiation — fabricating one
would claim precision the call site didn't supply, and the missing argument is what a turbofish
is for. `resolveGenericAggregate` (generalized from the struct-only `resolveGenericStruct`)
substitutes an instantiation's arguments into whichever member list applies, so field access,
tuple indexing, and method lookup all read concrete types; all three shapes go through one
function rather than a resolver each, which is what keeps them from disagreeing about what an
instantiation means. `ParameterizedType.String()` renders the applied form (`Box<i64>`), without
which a mismatch between two instantiations of one generic reads as nonsense.

**Backend** (`generic_types.go`). One emitted LLVM type per distinct instantiation (`%Box$i64`,
`%Box$boolean`, `%Maybe$string`), named by `typetable.TypeSymbol` — but materialized **lazily**,
on first use, rather than from a table of instantiations collected up front. Lazily because,
unlike a call, there is no single syntactic site that "uses" a type: `Box<i64>` can arrive as a
construction, a parameter, a return, a field of another type, an array element, or a type
argument of another generic, and every one of those already funnels through `lowerType`;
collecting instantiations separately would mean re-deriving that set from the AST *and* the
TypeTable and keeping the two in agreement, while materializing what `lowerType` is handed
cannot fall out of sync with what the program actually uses. An uninstantiated generic type
therefore costs nothing, exactly as an uninstantiated generic function does. The generic
*declaration* registers no type at all (`lowerTypeDecl` skips it — it has no single layout, and
registering it eagerly is what produced "unknown type: t"). The **declare-then-define split** is
load-bearing rather than inherited habit: the placeholder is registered *before* the fields are
lowered, so a recursive `shared List<t>` tail re-enters `lowerType` for the same instantiation,
finds the placeholder, and takes a pointer to it instead of recursing forever.
`resolveInstantiation` is the choke point that keeps generic types out of the rest of the
backend: it normalizes a `ParameterizedType` into the substituted declaration *renamed to the
mangled name* (so the fields satisfy every site that reads an aggregate's shape, and the name
resolves to this instantiation's LLVM struct), and it is applied in `recordedType` alongside the
newtype strip — a dozen construction/access/match/glue/layout sites switch on
`NamedStructType`/`TupleType`/`DataType` and would otherwise each need a case to keep in
agreement.

**A managed type argument** (`Box<string>`, `Maybe<string>`, `List<string>`) works, and getting
it wrong was a **double free**, not a leak. `ownership.OwnsManaged` had no `ParameterizedType`
case, so the two halves of the model read the type through different paths: the pass (which
decides where a +1 is minted) saw the raw `ParameterizedType` and judged `Box<string>` to own
nothing — the declaration's field type is the variable `t`, which owns nothing — and recorded no
retain for a copy, while the backend (which decides where a reference is released) reads types
through `recordedType` and so framed and deep-released *both* bindings. Drop twice, dup never.
`parameterizedOwnsManaged` fixes it at the root by substituting the declaration's parameters and
asking the same question of the result, restoring the invariant that one predicate serves both
sides. It terminates on the same grounds the backend's layout resolution does: a recursive type
must break its cycle with a `shared`/`weak` field (lyra-E014), and both are managed outright, so
`IsManaged` cuts the cycle first. **macOS ASan did not report the double free** — the regression
test compares the generic against the equivalent *concrete* declaration's retain/drop-glue
counts instead (`TestEmit_GenericManagedMatchesConcrete`), which is both the detector that
actually caught it and one that keeps its meaning as the ownership model gets more precise.

**Still open:** a `where` bound on a generic type's parameter is collected but not enforced at
the instantiation; **`Maybe<weak T>`** — the cycle-breaking use `weak` is waiting on — does not
*parse*, since the grammar won't take a `weak` type inside type arguments (a `tree-sitter-lyra`
change); and a trait-impl **method** on a generic type doesn't lower, which is not a generics
gap at all — trait-method calls don't lower for a non-generic receiver either (`llvm:
unsupported method call`).


## Match exhaustiveness

Match exhaustiveness is done for all scrutinee kinds today: numbers (range patterns), strings,
`runes` (char-literal arms + required catch-all, `checkRuneMatchArm`/`isRuneType`), `data`,
arrays, `bool`, tuples, and structs (`pkg/analyzer/typechecker/typechecker_control_flow.go`,
`*MatchIsExhaustive` functions; tests in `pkg/analyzer/typechecker/tests/match_expr_*.go`).
**Severity is by design** (`diag.CodeNonExhaustiveMatch`): a *closed* scrutinee (`bool`, `data`)
is a hard **error** — the case set is finite and known, and `_ =>` is always available to opt
out — while an *open* one (numbers, strings, runes, arrays, tuples, structs) is a **warning**,
since no arm list can enumerate the domain. Every one of them carries the `lyra-E009` code (the
warnings defaulted to the generic `lyra-E001` until 07/29), and the backend now traps on the
fall-through, so the warning is backed by defined runtime behavior rather than UB. A **struct**
match counts as exhaustive when any unguarded arm is *irrefutable* — every sub-pattern binds
rather than tests (`patternIsIrrefutable`/`aggregateMatchIsExhaustive`) — so
`match p { {x, y} => … }` is complete and no longer demands an unreachable wildcard; this
mirrors the backend's `aggPatternTest` returning a nil condition for exactly those patterns.

A **tuple** match is checked as a *pattern matrix* instead (`exhaustiveness.go`), because the
per-arm test cannot see coverage spread across arms: `(Some v, pred) => …, (None, _) => …`
covers every value while no single arm is irrefutable. That is the shape **every multi-clause
function desugars to**, so until 08/06 the prelude's own combinators each drew a false
`lyra-E009` — and a warning that fires on correct code is worse than none, since it trains the
reader to ignore the class. Checking columns *independently* would be unsound the other way
(`(Some v, None)` beside `(None, Some x)` covers both constructors in both columns and still
leaves `(Some, Some)` unmatched), so the check is Maranget's: specialize the matrix by each
constructor of column 0 and recurse, concluding coverage only from rows that agree on every
column to the left. Enumerable columns are `data` types and `bool`; anything else is covered
only by a row binding it whole. A pattern the matrix cannot interpret drops its row, which can
only make the answer "not exhaustive" — the direction that over-warns, never the one that goes
quiet on a match that can trap. A guarded arm never counts (the guard may fail).

An **array** match is over *lengths*, so a
*union* of arms can be exhaustive where no single arm is: `[e1..en]` covers exactly n and
`[e1..en, ...rest]` covers every length ≥ n, so `[] => …, [h, ...t] => …` — the recursive list
idiom — is complete and no longer warns. Only arms whose element sub-patterns are all
irrefutable contribute (a `[1, ...rest]` matches just the arrays starting with 1, so it proves
nothing about coverage), and without an open-ended arm infinitely many lengths are unmatched.

## Generic aggregate inference

A generic struct's type arguments are solved from its field *values* by `unifyGenericTarget`
— the same unifier data constructors and generic calls use — rather than by matching only
fields declared as a *bare* parameter. A field declared `Maybe<t>`, `[3]t` or `(t, i64)`
pins `t` down just as surely as one declared `t`, and matching only the bare form left
`struct Wrapper<t> { inner: Maybe<t> }` unsolvable from its own fields.

The declared field types are then substituted structurally too. Looking the field's type
*name* up in the solution silently accepted a wrong value: `Holder { tag: 1, inner:
Just("x") }` compared against the raw `Opt<t>`, which the "still generic, check leniently"
guard swallowed, while the surrounding instantiation looked complete because the other
field had solved `t`. That guard is `mentionsGenericParam`, which walks the type rather
than testing its name, so a partly-substituted `Maybe<t>` counts as incomplete exactly as a
bare `t` does — and what it defers, `propagateInstantiation` re-checks once the context
arrives.

## Unifying through a function type

`unifyGenericTarget` and `substituteGenerics` both handle `*types.LambdaType`, which is
what lets a higher-order generic be solved: `(m: Maybe<t>, f: () -> t) -> t` called with a
`() -> i64` binds `t` from the callback's signature, and the declared parameter is then
substituted to `() -> i64` for the assignability check. Missing either half makes every
callback-taking combinator uncallable — the first reports "cannot infer type variable t",
the second "cannot assign () -> i64 to () -> t".

Parameters unify in the same direction as the return type. A function type is
contravariant in its parameters, but this is unification against a *pattern*, not a
subtyping test: both sides are concrete apart from the variables being solved, so
direction only decides which side a variable may be read from. The substitution returns a
**copy**, since `LambdaType` is the one type here held by pointer and rewriting in place
would mutate the declaration every other call site shares.

## Contextual typing for lambda literals (`contextual_lambda.go`)

A lambda literal takes its missing parameter and return annotations from the type its context
expects — `elaborateLambda` fills the blanks **on the AST node**, before the body is inferred.
Everything downstream then sees a fully annotated lambda: `withParamScope` seeds the
parameters, `checkLambdaBody` checks and width-propagates the body, and the backend (which
reads `ast.Parameter.Type`) needs nothing. Before this, `(x) => x` reported `undefined symbol
"x"` and `() => 7` would not satisfy `() -> i64`, so every standard-library call site had to
restate types the signature already gave.

Two properties keep it honest: it only ever fills what was **left blank**, so an explicit
annotation wins and is still diagnosed if wrong; and it runs **before** the body is inferred,
which is the ordering bottom-up inference cannot give.

**Return-type inference is the same elaboration from the other direction** (`checkLambdaBody`
→ `inferLambdaReturnType`, 07/31/26): a function written without `-> T` has its return type
filled in from the body, *after* the body is walked rather than before. Same reason for
writing it onto the node — everything downstream reads `ReturnType`, and before this the
program type-checked and then failed the build with "needs a return type annotation".

Scoped to a body whose value is the return: an explicit `return` is refused with a
diagnostic, since inferring across several `return`s means joining candidates and deciding
what a disagreement or a diverging arm means. Recursion resolves whenever a non-recursive
branch fixes the type (an `if` takes its type from the first arm), and reports otherwise.

The one consumer that must distinguish a *written* signature from an inferred one is the
**entry point**: `let main = () => { 0 }` is a documented spelling of void, so
`ast.LambdaExpr.ReturnTypeInferred` lets `ResolveEntryPoint` discard the inferred type and
keep it void. Nothing else should read that flag.

Wired at the three sites that know what they want — an annotated binding (in the
*lambda-valued* branch of `checkVarDecl`, which returns before the general path), a direct
call's arguments, and a generic call's.

**The generic path has an ordering constraint worth knowing before touching it.** A bare
lambda cannot be inferred until it knows what is expected, but `(t) -> u` is not concrete
until the other arguments solve `t` — so `solveTypeVars` defers *incomplete* lambdas to a
second pass (`needsContextualTypes`). A fully annotated lambda is deliberately not deferred:
it can solve variables from its own signature, and deferring it would lose that. A type still
mentioning a variable is never planted (`isConcreteEnoughToElaborate`), because a variable
solved by the lambda's **own body** — `u` in `map(m, (x) => x * 2)` — would otherwise be
written as its declared return and never solved. The consequence is that a lambda's return
type can only be filled once solving finishes, which is why `inferGenericCall` elaborates
again after `instantiateSignature`.

## Multi-clause functions (`multi_clause.go`)

A multi-clause function *is* a match on its parameters, so it is desugared into one rather
than given a lowering of its own:

```
let fib = (n: i64, a: i64, b: i64) -> i64 {
  (0, a, _) => a,
  (n, a, b) => fib(n - 1, b, a + b),
}
```

becomes `… -> i64 => match (n, a, b) { (0, a, _) => …, (n, a, b) => … }`, built from the
parameter names the head declares. The grammar, collector and typechecker always accepted the
clause form; only the backend refused it, and the match machinery it needed — the ladder,
tuple destructuring, guards, the sealed fall-through — was already there and tested.

**It must happen in the front end.** The backend reads every type by AST-node identity
(`recordedType`), so a match synthesized *there* would have no entry for any of its nodes.
Synthesizing it in `checkLambdaBody`, before the body is walked, means the typechecker types
it like any other match and the ownership, capture and lowering passes need no changes.

Details worth knowing:

- **One parameter is matched directly**, not wrapped in a one-element tuple, so it reaches the
  scalar ladder rather than an aggregate that exists only in the desugaring.
- **The clauses are consumed** (`LambdaClauses = nil`). Leaving them would make
  `checkLambdaBody` check every clause body a second time — one mistake, two diagnostics.
- **Arity is checked here**, with the counts named. Left to the synthesized match it surfaces
  as a tuple-shape mismatch about a tuple the programmer never wrote.
- **No clause matching traps** rather than being undefined: the desugared match's
  fall-through is sealed like any other, so a function-clause error exits 101 with
  `lyra: match not exhaustive`.
- **Generic multi-clause functions work**, because the body is ordinary afterwards — the
  backend's `declareSpecialization` refusal is unreachable once every multi-clause lambda
  arrives desugared.

## Default parameter values (`default_args.go`)

`let add = (a: i64, b: i64 = 10) -> i64 => a + b` called as `add(5)`. The grammar, collector
and arity check already understood defaults; what was missing is that the **call site never
received them**, so the backend saw a call shorter than the parameter list and refused the
function outright.

`applyDefaultArguments` appends the declaration's default expressions for any trailing
arguments the call omits, before arity is counted or the generic path is taken. The rest of
the pipeline then sees a call that passes everything explicitly — the defaults are
type-checked against their parameters like any other argument, widths propagate, and the
backend needs no notion of defaults at all. Filling is idempotent, which matters because the
typechecker revisits nodes: after one pass the counts match, so a second adds nothing.

**The appended expression is the same AST node as the declaration's default, not a copy**, so
two call sites that both omit an argument share it. That is sound for everything keyed by
node — the recorded type is the parameter's type at every site, and a default is evaluated
against that type rather than varying by caller — and cloning would need a deep AST copy,
which this compiler avoids everywhere else. The case that would expose a problem, a
heap-allocating default at several call sites, is covered by an exec test.

`checkDefaultsAreTrailing` rejects a defaulted parameter followed by an undefaulted one.
Positional arguments fill left to right, so `(a: i64 = 1, b: i64)` cannot be called usefully —
and it used to be silently *accepted*, because the arity check counts required parameters
without checking their order, so `f(5)` bound 5 to `a` and left `b` unfilled.

**One place still refuses**: a default on a lambda used as a *value*. Defaults are filled from
the callee's declaration, and an indirect call has none — a `types.LambdaType` records *that*
a parameter has a default, not what it is. Named arguments, if they ever exist, would be the
other half of this feature and would lift the trailing rule.

## UFCS — method syntax for free functions (`typechecker_ufcs.go`)

`m.unwrap_or(0)` resolves to the free function `unwrap_or(m, 0)` when that function opts in
by **naming its first parameter `self`**. Everything else stays call-only, so adding a helper
to a module cannot change what `x.f()` means elsewhere in it. The rung sits in
`inferMemberCall`, giving the ladder **field → trait method → UFCS → builtin**: a real impl
wins, and a `self` function may shadow a compiler-provided method the way user code does
everywhere else. A receiver that is a bare type parameter never reaches it — that case exits
at the generic-bound rung above, so UFCS never competes with a `where` bound.

**A matching call is rewritten in place** (`desugarUFCSCall`): the receiver becomes
`Arguments[0]` and the callee an ordinary `IdentifierExpr`. Everything after this point —
generic solving, purity, ownership, captures, the backend — sees a direct call, and none of
them knows UFCS exists. The two spellings emit byte-identical IR, which is the test that says
so.

**That rewrite is the design, not a shortcut.** The alternative — keeping the member shape and
teaching each consumer about an implicit receiver — has a failure mode with no diagnostic. The
purity pass indexes arguments *positionally* against the declaration's parameters
(`callableParams`), so a receiver outside `Arguments` shifts every index by one and each
callback is checked against the declared bound of the parameter to its right; a function-typed
argument satisfies the wrong function-typed parameter quietly, so the bound simply stops being
enforced. Trait methods, whose receiver genuinely is implicit, pay exactly that tax through
`methodArgumentAt`. Desugaring means the two line up by construction, and
`checker/ufcs_bounds_test.go` fails loudly if that is ever traded away.

Mutating a call mid-check has precedent in `applyDefaultArguments` above, for the same reason:
so that the checker and the backend both see a call that passes everything explicitly. It is
structurally idempotent — once `Function` is an `IdentifierExpr` this rung is unreachable for
that node — and the passes that run *before* typechecking see the un-desugared form, which is
fine because none of them resolves a method name.

**Candidates are gathered by name, not resolved to one** (`ufcsFunction`, over
`SymbolTable.FunctionsNamed`). This is the part that is easy to get wrong, because the wrong
version removes methods without saying so. A name resolves through a single key, and the
candidates for a method call can live in different modules: when the prelude gained a `map`
for `Result`, it took the bare key, `std.maybe`'s `map` for `Maybe` moved to a
module-qualified one, and every `m.map(f)` in every program started reporting "member access
on non-struct type Maybe<i64>". Nothing was ambiguous and nothing was shadowed in a sense the
reader would recognise — one lookup simply could not see the other declaration. So every
declaration of the name is collected and filtered by the three things that decide the call:
it takes a `self` receiver, the file can reach it (`ufcsImportedIn` — imports still gate
this), and it accepts this receiver. A file's own module wins a tie; a surviving tie is
reported with a qualifier the reader can type, since the candidates come from a map and
"whichever came first" would not be stable between runs.

Checking a candidate otherwise reuses rather than reinvents: `unifyGenericTarget`
(the same predicate trait dispatch uses to match an impl against a receiver, so the function's
type variables act as wildcards — `self: Maybe<t>` accepts a `Maybe<i64>` receiver and binds
`t` from it). The `self` test is on the **declared parameter**, never on whether the function
has clauses: a multi-clause head must bind plain names, so it can name one `self`, and testing
`LambdaClauses` would make membership depend on *when* the check runs, since `desugarClauses`
consumes them.

Two rules that are decisions rather than mechanics:

- **An import is required.** A file's own module and the prelude need none; anything else must
  be named in that file's imports. What a file may call stays a property of its own import
  list, rather than of what some unrelated file happened to import. The cost is that the
  syntactic unused-import check cannot see such a use — a UFCS call never writes the module's
  name — so `UFCSModules()` carries the fact to it. Without that, the warning advises deleting
  the import that makes the program compile.
- **An `own` receiver is refused**, with its own error naming the call form. `own` transfers,
  and the receiver syntax hides the transfer; use-after-move catches a later read either way,
  so this is about the move staying legible. The refusal does not fall through to "has no
  method", which would give the reader two answers, one of them wrong.

`UFCSCallable` exports the predicate for the language server, so completion after `.` offers
exactly the calls that will compile.

## Receiver-keyed overloading (`typechecker_overload.go`)

A name may be declared **more than once in one module** when every declaration takes a `self`
receiver and their receivers have different type *heads* — the type constructor with its
arguments dropped (`types.HeadName`). So the prelude declares `unwrap_or` twice, once for
`Maybe<t>` and once for `Result<t,e>`, and the two types get the same vocabulary instead of
the second one being given a name it did not need.

This is the declaration-side half of what UFCS did for call sites. UFCS already dispatched
`m.map(f)` on the receiver's type; what it could not do is let two `map`s be *written* in one
module, since a second `let map` was a redeclaration. That is why the standard library split
`maybe.map` from `result.map` into separate modules — a split this removes the need for.

**Resolution happens in one place, because the desugar already normalized the two spellings.**
`m.f(x)` becomes `f(m, x)` before anything picks a member, so the receiver is argument 0
whichever way the call was written, and `receiverAccepts` — `unifyGenericTarget` again, the
predicate trait dispatch and UFCS both use — is asked from exactly two sites: the bare-call
path (`inferOverloadedCall`) and the UFCS rung. The rung has to resolve *before* desugaring,
since it decides whether `m.f` is a method call at all; asking an arbitrary member would
answer "no method" for a receiver some other member accepts.

Three consequences worth knowing:

- **Overlap is refused where it is written, not at each call.** Two members with one head
  could both match, and ranking them needs a specificity ordering the language does not have.
  `ast.OverloadableWith` refuses the pair once, with a message naming the shared receiver,
  rather than every call site reporting an ambiguity the author cannot resolve. A bare type
  variable (`self: t`) has no head at all — it accepts everything, so it can never be one
  candidate among several.
- **An overloaded name is absent from `SymbolTable.Functions`.** That map answers "which
  declaration does this name mean", and for a set the honest answer is "not decidable from a
  name". Leaving a member under the bare key would make every pass reading it silently pick
  one; leaving it empty makes them report the callee unresolved and take their conservative
  path. The set lives in `OverloadSets`, and a scope holds an `ast.OverloadSet` in place of
  the single declaration — so a pass that type-asserts to `*VarDeclStmt` fails rather than
  taking a member it had no business choosing.
- **The resolved callee is published** (`typetable.TypeTable.SetCallee`). Ownership, the
  use-after-move check, the purity pass and the backend all resolved a callee by name in
  order to read its parameter modes; that question has no answer here, so the pass that did
  resolve it records the answer, exactly as `MethodTable` does for trait dispatch. Each
  consumer reads it first and falls back to name lookup, which leaves every non-overloaded
  call resolving as it did.

A name used as a *value* rather than called (`let f = unwrap_or`) is an error: the members
have different signatures, so there is no one type to hand back.

**A bare call resolves like a method call** (`receiverFallback`, `bareCalleeFor`, 08/04).
The two spellings used different machinery: a method call gathers every reachable
declaration and picks by receiver, while a bare call resolves a *name* through the scope
chain (module → prelude → global) and stops at the first hit. With a `map` for `Box` in an
imported module, `b.map(f)` resolved and `map(b, f)` did not — the prelude's scope sits
nearer than the global one an import exports into. A bare call still tries the scope chain
first, so a local declaration wins as before; only when the name it lands on takes a `self`
receiver it does *not* accept does it gather candidates and pick by receiver. Additive: only
calls that were errors change meaning, and a plain non-receiver function whose first
argument does not fit is still an ordinary argument-type error.

One consequence reaches the backend: a call resolved this way finds its callee by
**identity**, and that callee is usually an ordinary singly-declared function rather than an
overload member — so every user function is recorded by declaration (`recordByDecl`), not
just the overloads.
