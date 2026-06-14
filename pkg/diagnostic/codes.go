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
