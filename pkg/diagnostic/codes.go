package diagnostic

const (
	CodeTypeError                = "lyra-E001"
	CodeUseBeforeDeclaration     = "lyra-E002"
	CodeReturnOutsideFunction    = "lyra-E003"
	CodeBreakContinueOutsideLoop = "lyra-E004"
	CodeAwaitOutsideAsync        = "lyra-E005"
	CodeYieldOutsideGenerator    = "lyra-E006"
	CodePurityViolation          = "lyra-E007"
	CodeTryOutsideResult         = "lyra-E008"

	// CodeNonExhaustiveMatch: a match on a closed type (`bool` or a `data`/sum
	// type) leaves cases uncovered. Error rather than warning because the set of
	// cases is finite and known — the author can still write an explicit `_ =>`
	// to opt out. Open types (wide integers, strings) stay a warning.
	CodeNonExhaustiveMatch = "lyra-E009"

	// CodeUninitializedDeclaration: a `let`/`var` binding has no initializer.
	// Initialization is required at declaration so a binding can never be read
	// before assignment (no use-of-uninitialized). Allowing uninitialized `var`
	// behind a definite-assignment pass may be revisited later.
	CodeUninitializedDeclaration = "lyra-E010"

	// CodeUnsafeOutsideUnsafe: a raw-pointer operation (`&x`, `*p`, a pointer
	// write) or a call to an `unsafe` function appears outside an `unsafe` block
	// or `unsafe` function body. The unsafe surface must be explicit and loud.
	CodeUnsafeOutsideUnsafe = "lyra-E011"

	// CodeNonConstantConstInitializer: a `const` binding's initializer is not a
	// compile-time constant. A `const` must be evaluable at compile time, so its
	// initializer may only be a literal, another constant, or an expression built
	// purely from those (arithmetic, boolean, string concatenation, array/tuple
	// of constants). Anything depending on runtime state (function calls, non-const
	// variables, interpolation, etc.) is rejected.
	CodeNonConstantConstInitializer = "lyra-E012"

	// CodeMissingStructField: a struct literal omits a declared field that has no
	// default value. Carries its own code (rather than the generic type error) so
	// the LSP can offer an "Add missing struct fields" quick fix.
	CodeMissingStructField = "lyra-E013"

	// CodeRecursiveType: a struct, data, or named-tuple type contains itself
	// by value (directly or through a cycle of by-value references), giving it
	// unbounded size. The fix is to break the cycle with a `shared` modifier on
	// the type declaration or on the recursive field/constructor-parameter.
	CodeRecursiveType = "lyra-E014"

	// CodeConflictingEffectBounds: a function or method carries two mutually
	// exclusive correctness-axis bounds — `pure` and `det`. They sit on the same
	// axis and `pure` is the stronger guarantee (it already implies determinism),
	// so writing both is contradictory. `noalloc` is a separate resource axis and
	// stacks freely with either, so it is never part of this conflict.
	CodeConflictingEffectBounds = "lyra-E015"

	// CodeEffectBoundViolation: a function or method declared `det` performs a
	// non-deterministic effect (reads external input, randomness, or the clock),
	// or one declared `noalloc` heap-allocates. `pure`'s equivalent is the older
	// CodePurityViolation (E007); `det`/`noalloc` get their own code.
	CodeEffectBoundViolation = "lyra-E016"

	// CodeMalformedBuiltin: a `@builtin(X)` attribute is invalid — an unrecognized
	// builtin name, a target whose shape doesn't match the canonical kind it
	// claims (e.g. `@builtin(Result)` on a type without single-payload Ok/Err
	// constructors), or a second declaration claiming a kind already taken.
	CodeMalformedBuiltin = "lyra-E017"

	// CodeAllocationMismatch: a value is *owned* into a slot whose storage flavor
	// differs — a `shared` (heap, ref-counted) value stored where a `stack`
	// (inline, value-semantics) value is expected, or vice versa. Allocation is
	// not part of nominal identity (assignability otherwise ignores it), but
	// crossing the flavor boundary changes representation, so it must be an
	// explicit operation rather than a silent coercion. Only fires when both
	// sides carry a concrete, differing flavor; an unspecified flavor inherits
	// from context and is compatible with either. Borrowed (`ref`/`mut`)
	// parameters are allocation-polymorphic and are not subject to this check.
	CodeAllocationMismatch = "lyra-E018"

	// CodeUseAfterMove: a binding is read after its value was moved into an `own`
	// parameter. `own` means the callee takes ownership — it may release the value
	// or, under Perceus reuse, overwrite its box in place — so the caller must
	// treat the binding as consumed. Flow-sensitive and conservative at joins: a
	// move in either branch of an `if`/`match` counts as moved afterwards, and a
	// move anywhere in a loop body counts as moved on the next iteration.
	// Reassigning the binding gives it a fresh value and clears the move.
	CodeUseAfterMove = "lyra-E019"

	// CodeIntegerOverflow: an integer `+`/`-`/`*`/unary-`-` whose operand ranges
	// (tracked by the value-range analysis) prove the result cannot fit its type on
	// any path — a guaranteed runtime overflow trap, caught at compile time. Unlike
	// the literal range check (lyra-E001) this fires on *non-constant* variables
	// whose range is known from a branch refinement (`if x > 100 { x + 100 }` on an
	// i8). Only definite overflow is reported; a merely *possible* overflow is left
	// to the runtime trap.
	CodeIntegerOverflow = "lyra-E020"

	// CodeDivideByZero: an integer `/`/`%`/`%%` whose divisor's value range (tracked
	// by the value-range analysis) proves it is *always* zero on a reachable path —
	// a guaranteed runtime divide-by-zero trap, caught at compile time. The
	// error-reporting twin of the divide-by-zero trap *elision* (which fires when the
	// divisor is provably *non*-zero); symmetric with lyra-E020, and flow-sensitive
	// the same way (`if b == 0 { a / b }` catches the refined divisor). Only a
	// *definite* divide-by-zero is reported; a merely *possible* one (the divisor
	// range includes 0 but also nonzero values) is left to the runtime trap.
	CodeDivideByZero = "lyra-E021"

	// CodeIndexOutOfBounds: an array index `xs[i]` whose value range (tracked by the
	// value-range analysis) proves it is *always* out of bounds on a reachable path
	// — a guaranteed runtime bounds-trap, caught at compile time. The error-reporting
	// twin of the bounds-trap *elision* (which fires when the index is provably in
	// bounds); symmetric with lyra-E020/E021 and flow-sensitive the same way
	// (`if i >= size { xs[i] }` catches the refined index). Reported only for a
	// *non-singleton* index range entirely outside `[-size, size)` (a negative index
	// counts from the end): a single constant index is already the typechecker's own
	// range check (inferIndexExpr / resolveConstantInt), and a non-singleton range
	// guarantees that check didn't fire, so there is no double report. Only a
	// *definite* OOB is reported; a merely *possible* one is left to the runtime trap.
	CodeIndexOutOfBounds = "lyra-E022"

	// CodeRangeConstraintViolation: a compile-time numeric constant assigned to a
	// range-constrained newtype (`newtype Percent = u8 where range(0..=100)`) falls
	// outside the declared range (`let p: Percent = 150`). The numeric analogue of
	// the string PatternConstraint check; checked for a foldable constant value (int
	// or float literal) against foldable literal bounds. A non-constant value is left
	// to the value-range analysis / runtime (not yet wired), so this is a
	// definite-only, compile-time check like the literal integer range check.
	CodeRangeConstraintViolation = "lyra-E023"

	CodeShadowing       = "lyra-W001"
	CodeUnreachableCode = "lyra-W002"
	CodeUnusedVariable  = "lyra-W003"
	CodeUnusedImport    = "lyra-W004"
	CodeUnusedParameter = "lyra-W005"
	CodeUnusedResult    = "lyra-W006"

	// CodeNonOptionalCoalescing: the left operand of `??` is not a Maybe<T>, so
	// it can never be null and the coalescing is pointless.
	CodeNonOptionalCoalescing = "lyra-W007"

	// CodeImpreciseFloatEquality: an exact floating-point equality test — the
	// `==`/`!=` operator on floats, or a float literal `match` pattern (which
	// lowers to `fcmp oeq`). A value off by an ULP silently compares unequal, so
	// results may be surprising; a tolerance check or a range pattern is safer.
	CodeImpreciseFloatEquality = "lyra-W008"

	// CodeScreamingCaseTypeName: a type declared with an all-uppercase
	// (SCREAMING_CASE) name. Such a name lexes as a `const_identifier`, not a
	// `user_defined_type_name`, so a struct literal `NAME { … }` won't parse — the
	// type can be referenced but never constructed. Give it a PascalCase name.
	CodeScreamingCaseTypeName = "lyra-W009"

	// CodeInertBorrowModifier: an `own`/`ref`/`mut` modifier on a parameter whose
	// type is a copied scalar primitive (a numeric type, `bool`, or `rune`). These
	// modifiers are calling conventions over a *reference* — `own` transfers
	// ownership, `ref`/`mut` borrow — but a scalar is passed by value with no
	// interior to borrow or transfer, so the modifier is equivalent to a plain
	// parameter and only misleads a reader into expecting move/borrow semantics.
	// `string` (a managed fat pointer) and generic type parameters are NOT scalars
	// and are never flagged. A warning, since the code is correct as written.
	CodeInertBorrowModifier = "lyra-W010"

	// CodeConstantComparison: an integer comparison whose operand ranges (tracked by
	// the value-range analysis) prove it always evaluates to the same result — e.g.
	// `x < 0` on a `u8` (always false), or a comparison made trivial by a branch
	// refinement. The branch it guards is dead code or a likely bug; a warning since
	// the code still compiles and runs.
	CodeConstantComparison = "lyra-W011"
)
