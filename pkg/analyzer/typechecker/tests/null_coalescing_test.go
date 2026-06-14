package typechecker_test

import "testing"

// ── Null coalescing operand types ────────────────────────────────────────────

// A Maybe<T> left operand coalesced with a matching default unwraps to T and
// produces no diagnostic.
func TestTypeCheck_NullCoalescing_MaybeAndMatchingDefault_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let lookup = (s: string) -> Maybe<i64> => { lookup(s) }
		let y: i64 = 1
		let z = lookup("k") ?? y
	`, false)
	assertNoErrors(t, res)
}

// The payload of the Maybe must be compatible with the default's type.
func TestTypeCheck_NullCoalescing_IncompatiblePayload_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let lookup = (s: string) -> Maybe<i64> => { lookup(s) }
		let s: string = "hello"
		let z = lookup("k") ?? s
	`, false)
	assertErrorsAre(t, res, "null coalescing operands have incompatible types: left is i64, right is string")
}

// A non-optional left operand can never be null: the `??` is pointless and is
// reported as a warning.
func TestTypeCheck_NullCoalescing_NonOptionalLeft_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 0
		let y: i64 = 1
		let z = x ?? y
	`, false)
	assertWarningsAre(t, res, "left operand of `??` is never null: expected a Maybe<T>, got i64")
}

// An int literal left operand is likewise never null.
func TestTypeCheck_NullCoalescing_IntLiteralLeft_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let b: i64 = 7
		let z = 42 ?? b
	`, false)
	assertWarningsAre(t, res, "left operand of `??` is never null: expected a Maybe<T>, got integer literal")
}
