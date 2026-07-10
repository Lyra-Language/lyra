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

	CodeShadowing       = "lyra-W001"
	CodeUnreachableCode = "lyra-W002"
	CodeUnusedVariable  = "lyra-W003"
	CodeUnusedImport    = "lyra-W004"
	CodeUnusedParameter = "lyra-W005"
	CodeUnusedResult    = "lyra-W006"

	// CodeNonOptionalCoalescing: the left operand of `??` is not a Maybe<T>, so
	// it can never be null and the coalescing is pointless.
	CodeNonOptionalCoalescing = "lyra-W007"
)
