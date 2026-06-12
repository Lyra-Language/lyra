package typechecker_test

import "testing"

// ── Null coalescing operand types ────────────────────────────────────────────

func TestTypeCheck_NullCoalescing_CompatibleTypes_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 0
		let y: i64 = 1
		let z = x ?? y
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NullCoalescing_IncompatibleTypes_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 0
		let s: string = "hello"
		let z = x ?? s
	`, false)
	assertErrorsAre(t, res, "null coalescing operands have incompatible types: left is i64, right is string")
}

func TestTypeCheck_NullCoalescing_IntAndFloat_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 0
		let f: f64 = 3.14
		let z = x ?? f
	`, false)
	assertErrorsAre(t, res, "null coalescing operands have incompatible types: left is i64, right is f64")
}

func TestTypeCheck_NullCoalescing_IntLiteralAndBool_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let b: bool = true
		let z = 42 ?? b
	`, false)
	assertErrorsAre(t, res, "null coalescing operands have incompatible types: left is integer literal, right is boolean")
}
