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

// A non-optional left operand can never be null: the `??` is pointless — the
// default is dead code that reads as a handled case — and is refused
// (lyra-E049; a warning until 08/13). The recovery still treats the left type
// as the payload, so no cascade follows the one report.
func TestTypeCheck_NullCoalescing_NonOptionalLeft_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 0
		let y: i64 = 1
		let z = x ?? y
	`, false)
	assertErrorsAre(t, res, "left operand of `??` is never null: expected a Maybe<T>, got i64 — remove the `??`")
}

// An int literal left operand is likewise never null.
func TestTypeCheck_NullCoalescing_IntLiteralLeft_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let b: i64 = 7
		let z = 42 ?? b
	`, false)
	assertErrorsAre(t, res, "left operand of `??` is never null: expected a Maybe<T>, got integer literal — remove the `??`")
}

// ── The default is a value position against the unified type ─────────────────
//
// An untyped default narrows to the payload's width (the backend's phi requires
// the arms to agree), and one that cannot hold its value is refused rather than
// truncated — the same rule as every other value position (08/13).

func TestTypeCheck_NullCoalescing_UntypedDefaultNarrows(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let get = (s: string) -> Maybe<u8> => { get(s) }
		let z = get("k") ?? 7
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NullCoalescing_DefaultTooWideRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let get = (s: string) -> Maybe<u8> => { get(s) }
		let z = get("k") ?? 300
	`, false)
	assertErrorsAre(t, res, "`??` default: literal value 300 overflows u8")
}
