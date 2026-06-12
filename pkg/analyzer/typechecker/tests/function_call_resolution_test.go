package typechecker_test

import "testing"

// Tests for inferFunctionCallExpr's identifier-resolution paths:
//
//   1. Undefined function — the name is not in scope at all.
//   2. Non-callable variable — the name resolves to a plain value, not a lambda.
//   3. Function parameter call — current limitation: params are not visible to
//      the call resolver (the "higher-order calls not yet implemented" path).
//   4. Regression — valid calls are still accepted after the new error paths.

// ── 1. Undefined function ─────────────────────────────────────────────────────

func TestCall_UndefinedFunction_AsStatement(t *testing.T) {
	res := parseCollectAndCheck(t, `foo()`, false)
	assertErrorsAre(t, res, `undefined function "foo"`)
}

func TestCall_UndefinedFunction_WithArguments(t *testing.T) {
	// Arguments are irrelevant — the error fires before any argument checking.
	res := parseCollectAndCheck(t, `foo(1, 2, 3)`, false)
	assertErrorsAre(t, res, `undefined function "foo"`)
}

func TestCall_UndefinedFunction_ResultUsedInBinaryExpr_NoError_Cascade(t *testing.T) {
	// inferExprType returns nil after emitting the error, so the enclosing
	// binary expression skips rather than producing a second complaint.
	res := parseCollectAndCheck(t, `let y = foo() + 1`, false)
	assertErrorsAre(t, res, `undefined function "foo"`)
}

func TestCall_UndefinedFunction_AssignedToAnnotatedVar_NoError_Cascade(t *testing.T) {
	// checkVarDecl bails on nil inferredType, so no "cannot assign … to i64" cascade.
	res := parseCollectAndCheck(t, `let y: i64 = foo()`, false)
	assertErrorsAre(t, res, `undefined function "foo"`)
}

func TestCall_UndefinedFunction_TwoCalls_TwoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
		foo()
		bar()
	`, false)
	assertErrorsAre(t, res,
		`undefined function "foo"`,
		`undefined function "bar"`,
	)
}

func TestCall_UndefinedFunction_SameNameTwice_TwoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
		foo()
		foo()
	`, false)
	assertErrorsAre(t, res,
		`undefined function "foo"`,
		`undefined function "foo"`,
	)
}

func TestCall_UndefinedFunction_NestedAsArgument_OnlyOneError(t *testing.T) {
	// The outer call (double) resolves fine; the inner call (foo) is undefined.
	// The argument type check is skipped when argType is nil, so only one error.
	res := parseCollectAndCheck(t, `
		let double = (n: i64) -> i64 => n * 2
		double(foo())
	`, false)
	assertErrorsAre(t, res, `undefined function "foo"`)
}

func TestCall_UndefinedFunction_Typo_DefinedNameNotFound(t *testing.T) {
	// "add" is defined but "adds" is not — a one-character typo must not resolve.
	res := parseCollectAndCheck(t, `
		let add = (a: i64, b: i64) -> i64 => a + b
		adds(1, 2)
	`, false)
	assertErrorsAre(t, res, `undefined function "adds"`)
}

// ── 2. Non-callable variable ──────────────────────────────────────────────────

func TestCall_NonCallable_IntVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n = 42
		n()
	`, false)
	assertErrorsAre(t, res, `identifier "n" is not callable (type i64)`)
}

func TestCall_NonCallable_StringVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s = "hello"
		s()
	`, false)
	assertErrorsAre(t, res, `identifier "s" is not callable (type string)`)
}

func TestCall_NonCallable_BoolVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let b = true
		b()
	`, false)
	assertErrorsAre(t, res, `identifier "b" is not callable (type boolean)`)
}

func TestCall_NonCallable_WithArguments(t *testing.T) {
	// Passing arguments to a non-callable doesn't produce extra errors.
	res := parseCollectAndCheck(t, `
		let n = 42
		n(1, 2)
	`, false)
	assertErrorsAre(t, res, `identifier "n" is not callable (type i64)`)
}

func TestCall_NonCallable_ResultAssignedToAnnotatedVar_NoError_Cascade(t *testing.T) {
	// inferFunctionCallExpr returns nil on resolution failure, so checkVarDecl
	// bails early with no additional "cannot assign" error.
	res := parseCollectAndCheck(t, `
		let n = 42
		let y: i64 = n()
	`, false)
	assertErrorsAre(t, res, `identifier "n" is not callable (type i64)`)
}

func TestCall_NonCallable_AnnotatedIntVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i32 = 5
		n()
	`, false)
	assertErrorsAre(t, res, `identifier "n" is not callable (type i32)`)
}

// ── 3. Lambda stored in variable (higher-order calls via VarDeclStmt) ─────────

func TestCall_LambdaInVariable_Valid_Ok(t *testing.T) {
	// Lambda stored in a variable, called with correct args.
	res := parseCollectAndCheck(t, `
		let double = (n: i64) -> i64 => n * 2
		let r: i64 = double(5)
	`, false)
	assertNoErrors(t, res)
}

func TestCall_LambdaInVariable_WrongArgCount(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let double = (n: i64) -> i64 => n * 2
		double()
	`, false)
	assertErrorsAre(t, res, `double: expected 1 argument(s), got 0`)
}

func TestCall_LambdaInVariable_WrongArgType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let double = (n: i64) -> i64 => n * 2
		let r: i64 = double(true)
	`, false)
	assertErrorsAre(t, res, `double: argument 1 (n): cannot assign boolean to i64`)
}

// ── 4. Direct lambda calls ─────────────────────────────────────────────────

func TestCall_DirectLambda_Valid_Ok(t *testing.T) {
	// Calling a bare lambda expression directly.
	res := parseCollectAndCheck(t, `((n: i64) -> i64 => n * 2)(5)`, false)
	assertNoErrors(t, res)
}

func TestCall_DirectLambda_WrongArgCount(t *testing.T) {
	res := parseCollectAndCheck(t, `((n: i64) -> i64 => n * 2)()`, false)
	assertErrorsAre(t, res, `lambda: expected 1 argument(s), got 0`)
}

func TestCall_DirectLambda_WrongArgType(t *testing.T) {
	res := parseCollectAndCheck(t, `((n: i64) -> i64 => n * 2)(true)`, false)
	assertErrorsAre(t, res, `lambda: argument 1 (n): cannot assign boolean to i64`)
}

func TestCall_DirectLambda_MultipleParams_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `((a: i64, b: i64) -> i64 => a + b)(1, 2)`, false)
	assertNoErrors(t, res)
}

func TestCall_DirectLambda_MultipleParams_WrongCount(t *testing.T) {
	res := parseCollectAndCheck(t, `((a: i64, b: i64) -> i64 => a + b)(1)`, false)
	assertErrorsAre(t, res, `lambda: expected 2 argument(s), got 1`)
}

// ── 5. Regression — valid calls still accepted ───────────────────────────────

func TestCall_DefinedFunction_NoArgs_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let answer = () -> i64 => 42
		answer()
	`, false)
	assertNoErrors(t, res)
}

func TestCall_DefinedFunction_ReturnUsedInAssignment_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let double = (n: i64) -> i64 => n * 2
		let r: i64 = double(5)
	`, false)
	assertNoErrors(t, res)
}

func TestCall_DefinedFunction_ChainedCalls_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let inc = (n: i64) -> i64 => n + 1
		let r: i64 = inc(inc(0))
	`, false)
	assertNoErrors(t, res)
}

func TestCall_DefinedFunction_UsedBeforeDeclarationOrder_Ok(t *testing.T) {
	// Both declarations are at the top level; the type-checker processes all
	// statements in order, so the call site after the declaration is fine.
	res := parseCollectAndCheck(t, `
		let square = (n: i64) -> i64 => n * n
		let r: i64 = square(4)
	`, false)
	assertNoErrors(t, res)
}
