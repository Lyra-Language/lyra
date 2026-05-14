package typechecker_test

import "testing"

// --- simple reassignment (x = value) ---

func TestTypeCheck_VarReassignment_CompatibleType(t *testing.T) {
	res := parseCollectAndCheck(t, `
var x: int = 1
x = 2
`)
	assertNoErrors(t, res)
}

func TestTypeCheck_VarReassignment_UntypedLiteralWidens(t *testing.T) {
	res := parseCollectAndCheck(t, `
var x: i32 = 1
x = 99
`)
	assertNoErrors(t, res)
}

func TestTypeCheck_VarReassignment_InferredType(t *testing.T) {
	// x has no annotation; its effective type is promoted to int.
	res := parseCollectAndCheck(t, `
var x = 42
x = 7
`)
	assertNoErrors(t, res)
}

func TestTypeCheck_VarReassignment_TypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: int = 1
		x = 3.14
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "x: cannot assign float literal to int")
}

func TestTypeCheck_VarReassignment_InferredTypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x = 42
		x = 3.14
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "x: cannot assign float literal to int")
}

// --- immutability enforcement ---

func TestTypeCheck_LetReassignment_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: int = 1
		x = 2
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "x: cannot assign to immutable binding")
}

func TestTypeCheck_ConstReassignment_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		const X: int = 1
		X = 2
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "X: cannot assign to immutable binding")
}

// --- compound assignment (x += value) ---

func TestTypeCheck_CompoundAssign_Valid(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: int = 1
		x += 5
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_CompoundAssign_AllOps(t *testing.T) {
	for _, op := range []string{"+=", "-=", "*=", "/=", "%="} {
		src := "var x: int = 10\nx " + op + " 3"
		res := parseCollectAndCheck(t, src)
		assertNoErrors(t, res)
	}
}

func TestTypeCheck_CompoundAssign_TypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: int = 1
		x += 3.14
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "x: cannot assign float literal to int")
}

func TestTypeCheck_CompoundAssign_LetImmutable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: int = 1
		x += 1
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "x: cannot assign to immutable binding")
}

func TestTypeCheck_CompoundAssign_ConstImmutable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		const X: int = 1
		X += 1
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "X: cannot assign to immutable binding")
}
