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
