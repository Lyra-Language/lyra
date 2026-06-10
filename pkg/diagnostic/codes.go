package diagnostic

const (
	CodeTypeError                = "lyra-E001"
	CodeUseBeforeDeclaration     = "lyra-E002"
	CodeReturnOutsideFunction    = "lyra-E003"
	CodeBreakContinueOutsideLoop = "lyra-E004"
	CodeAwaitOutsideAsync        = "lyra-E005"
	CodeYieldOutsideGenerator    = "lyra-E006"

	CodeShadowing       = "lyra-W001"
	CodeUnreachableCode = "lyra-W002"
	CodeUnusedVariable  = "lyra-W003"
	CodeUnusedImport    = "lyra-W004"
	CodeUnusedParameter = "lyra-W005"
)
