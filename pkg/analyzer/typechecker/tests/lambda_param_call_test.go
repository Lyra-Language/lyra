package typechecker_test

import "testing"

// Calling a function-typed parameter — `f(x)` where `f: (u8) -> u8` is a
// parameter — resolves against the parameter's lambda-type signature rather
// than reporting "cannot resolve function".

func TestLambdaParam_CallResolves(t *testing.T) {
	res := parseCollectAndCheck(t, `let apply = (f: (u8) -> u8, x: u8) -> u8 => f(x)`, false)
	assertNoErrors(t, res)
}

func TestLambdaParam_NestedCall(t *testing.T) {
	res := parseCollectAndCheck(t, `let twice = (f: (i32) -> i32, x: i32) -> i32 => f(f(x))`, false)
	assertNoErrors(t, res)
}

func TestLambdaParam_ArgumentTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `let apply = (f: (u8) -> u8, x: string) -> u8 => f(x)`, false)
	assertErrorsAre(t, res, "f: argument 1: cannot assign string to u8")
}

func TestLambdaParam_ArityMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `let apply = (f: (u8) -> u8, x: u8) -> u8 => f(x, x)`, false)
	assertErrorsAre(t, res, "f: expected 1 argument(s), got 2")
}

func TestLambdaParam_NonFunctionParamNotCallable(t *testing.T) {
	res := parseCollectAndCheck(t, `let bad = (x: i32) -> i32 => x(1)`, false)
	assertErrorsAre(t, res, `identifier "x" is not callable (type i32)`)
}

// An unannotated lambda literal (`(n: u8) => n`) infers its return type from its
// body, so it can be passed where a concrete function type is expected. Before,
// its type was `(u8) -> ?` (nil return) and the assignment failed.

func TestInferredLambda_PassedToTypedParameter(t *testing.T) {
	res := parseCollectAndCheck(t, `let apply = (f: (u8) -> u8, x: u8) -> u8 => f(x)
let use = () -> u8 => apply((n: u8) => n, 0)`, false)
	assertNoErrors(t, res)
}

func TestInferredLambda_ReturnTypeIsInferredForMismatch(t *testing.T) {
	// The lambda's return type infers to u8 (its parameter n: u8), which does not
	// match the expected (u8) -> string — the error names the inferred (u8) -> u8,
	// not a nil/"?" return.
	res := parseCollectAndCheck(t, `let apply = (f: (u8) -> string, x: u8) -> string => f(x)
let use = () -> string => apply((n: u8) => n, 0)`, false)
	assertErrorsAre(t, res, "apply: argument 1 (f): cannot assign (u8) -> u8 to (u8) -> string")
}
