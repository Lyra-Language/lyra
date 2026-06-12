package typechecker_test

// Tests for block-scope and lambda-body-scope variable tracking.
//
// The TypeChecker must enter the child scope associated with each BlockExpr
// (stored in the ScopeTable during collection) so that locally-declared
// variables are visible when type-checking statements and expressions inside
// that block.
//
// Two paths exercise scope entry:
//
//  1. inferBlockType  — used when a block appears as an expression value
//     (e.g. `let x = { ... }`).  This path is fully implemented and all
//     tests in the "Block expression scope" section are expected to pass.
//
//  2. checkBlockReturn — used when walking a lambda's block body to verify
//     its return type.  This path currently does NOT enter the block's scope,
//     so lambda-body locals are invisible to the return-type check.  Tests in
//     the "Lambda body scope" section document the desired behaviour; the ones
//     marked "currently fails" will pass once checkBlockReturn is wrapped with
//     enterScope(block, ...).

import "testing"

// ── Block expression scope ────────────────────────────────────────────────────

// TestTypeCheck_BlockScope_LocalVisibleAtTail verifies that a variable declared
// at the top of a block is visible and correctly typed when referenced at the
// tail expression.
func TestTypeCheck_BlockScope_LocalVisibleAtTail(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let result: i64 = {
			let x = 42
			x
		}
	`, false)
	assertNoErrors(t, res)
}

// TestTypeCheck_BlockScope_LocalTypeMismatch verifies that a type error is
// reported when the tail expression of a block has a type incompatible with
// the outer binding's annotation.
func TestTypeCheck_BlockScope_LocalTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let result: string = {
			let x = 42
			x
		}
	`, false)
	assertErrorsAre(t, res, "result: cannot assign i64 to string")
}

// TestTypeCheck_BlockScope_AnnotatedLocal_TypeMismatch verifies the same for a
// block-local variable that carries its own type annotation.
func TestTypeCheck_BlockScope_AnnotatedLocal_TypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let result: i64 = {
			let x: string = "hello"
			x
		}
	`, false)
	assertErrorsAre(t, res, "result: cannot assign string to i64")
}

// TestTypeCheck_BlockScope_OuterVariableVisibleInBlock verifies that a variable
// declared before the block is still in scope inside it.
func TestTypeCheck_BlockScope_OuterVariableVisibleInBlock(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i32 = 10
		let result: i32 = {
			x
		}
	`, false)
	assertNoErrors(t, res)
}

// TestTypeCheck_BlockScope_OuterVariableMismatch verifies that a type error is
// reported when an outer variable's type is incompatible with the block's
// surrounding annotation.
func TestTypeCheck_BlockScope_OuterVariableMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i32 = 10
		let result: string = {
			x
		}
	`, false)
	assertErrorsAre(t, res, "result: cannot assign i32 to string")
}

// TestTypeCheck_BlockScope_NestedBlocks verifies that a variable declared in
// an outer block is visible inside a deeper nested block.
func TestTypeCheck_BlockScope_NestedBlocks(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let result: i64 = {
			let outer = 1
			let inner = {
				outer
			}
			inner
		}
	`, false)
	assertNoErrors(t, res)
}

// TestTypeCheck_BlockScope_NestedBlocks_InnerTypeMismatch verifies that a type
// error propagates correctly when the inner block's tail type conflicts with
// the outer annotation.
func TestTypeCheck_BlockScope_NestedBlocks_InnerTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let result: string = {
			let outer = 1
			let inner = {
				outer
			}
			inner
		}
	`, false)
	assertErrorsAre(t, res, "result: cannot assign i64 to string")
}

// TestTypeCheck_BlockScope_IfElse_LocalVariables verifies that variables
// declared inside if/else branches are independently visible within each
// branch and that the common type is resolved correctly.
func TestTypeCheck_BlockScope_IfElse_LocalVariables(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let result: i64 = if flag {
			let a = 1
			a
		} else {
			let b = 2
			b
		}
	`, false)
	assertNoErrors(t, res)
}

// TestTypeCheck_BlockScope_IfElse_BranchTypeMismatch verifies that a branch
// type incompatibility is caught when the branches yield different types.
func TestTypeCheck_BlockScope_IfElse_BranchTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let result = if flag {
			let a = 1
			a
		} else {
			let b = "hello"
			b
		}
	`, false)
	assertErrorsAre(t, res,
		"if/else branches have incompatible types: then is i64, else is string")
}

// TestTypeCheck_BlockScope_MultipleLocals verifies that several locals can be
// declared inside a block and all remain visible throughout the block.
func TestTypeCheck_BlockScope_MultipleLocals(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let result: i64 = {
			let a = 1
			let b = 2
			let c = 3
			a + b + c
		}
	`, false)
	assertNoErrors(t, res)
}

// ── Lambda body scope ─────────────────────────────────────────────────────────
//
// The tests below require that checkBlockReturn enters the lambda body's scope
// (via enterScope) so that block-local variables declared inside a function
// body are visible during return-type checking.  Without that fix,
// inferExprType for a local identifier returns nil and the mismatch goes
// undetected.

// TestTypeCheck_LambdaScope_AnnotatedLocal_ReturnMatch verifies that a lambda
// whose body declares an annotated local and returns it produces no errors when
// the local's type matches the declared return type.
func TestTypeCheck_LambdaScope_AnnotatedLocal_ReturnMatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> i32 => {
			let local: i32 = 5
			local
		}
	`, false)
	assertNoErrors(t, res)
}

// TestTypeCheck_LambdaScope_AnnotatedLocal_ReturnMismatch verifies that a type
// error is reported when a lambda returns a local variable whose annotated type
// conflicts with the declared return type.
//
// Currently fails: checkBlockReturn does not enter the block scope, so the
// local is invisible and the mismatch is silently missed.
// Fix: wrap checkBlockReturn's body with tc.enterScope(block, ...).
func TestTypeCheck_LambdaScope_AnnotatedLocal_ReturnMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> string => {
			let local: i32 = 5
			local
		}
	`, false)
	assertErrorsAre(t, res, "f: return type mismatch: expected string, got i32")
}

// TestTypeCheck_LambdaScope_ParamAndAnnotatedLocal verifies that a lambda that
// uses both a parameter and a body-local variable in its return expression
// produces no errors when the types are compatible.
func TestTypeCheck_LambdaScope_ParamAndAnnotatedLocal(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let add = (x: i64) -> i64 => {
			let y: i64 = 1
			x + y
		}
	`, false)
	assertNoErrors(t, res)
}
