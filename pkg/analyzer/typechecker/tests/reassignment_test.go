package typechecker_test

import "testing"

// --- simple reassignment (x = value) ---

func TestTypeCheck_VarReassignment_CompatibleType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i64 = 1
		x = 2
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_VarReassignment_UntypedLiteralWidens(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i32 = 1
		x = 99
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_VarReassignment_InferredType(t *testing.T) {
	// x has no annotation; its effective type is promoted to i64.
	res := parseCollectAndCheck(t, `
		var x = 42
		x = 7
`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_VarReassignment_TypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i64 = 1
		x = 3.14
	`, false)
	assertErrorsAre(t, res, "x: cannot assign float literal to i64")
}

func TestTypeCheck_VarReassignment_InferredTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x = 42
		x = 3.14
	`, false)
	assertErrorsAre(t, res, "x: cannot assign float literal to i64")
}

// --- immutability enforcement ---

func TestTypeCheck_LetReassignment_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 1
		x = 2
	`, false)
	assertErrorsAre(t, res, "x: 'let' binding is immutable; use 'var' to allow reassignment")
}

func TestTypeCheck_ConstReassignment_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		const X: i64 = 1
		X = 2
	`, false)
	assertErrorsAre(t, res, "X: 'const' binding is immutable and cannot be reassigned")
}

// --- compound assignment (x += value) ---

func TestTypeCheck_CompoundAssign_Valid(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i64 = 1
		x += 5
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_CompoundAssign_AllOps(t *testing.T) {
	for _, op := range []string{"+=", "-=", "*=", "/=", "%="} {
		src := "var x: i64 = 10\nx " + op + " 3"
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

func TestTypeCheck_CompoundAssign_TypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i64 = 1
		x += 3.14
	`, false)
	assertErrorsAre(t, res, "x: cannot assign float literal to i64")
}

func TestTypeCheck_CompoundAssign_LetImmutable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 1
		x += 1
	`, false)
	assertErrorsAre(t, res, "x: 'let' binding is immutable; use 'var' to allow reassignment")
}

func TestTypeCheck_CompoundAssign_ConstImmutable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		const X: i64 = 1
		X += 1
	`, false)
	assertErrorsAre(t, res, "X: 'const' binding is immutable and cannot be reassigned")
}
