package typechecker_test

import (
	"strings"
	"testing"
)

// i128/u128 are ordinary fixed-width integer types: they annotate bindings,
// participate in arithmetic (with the same same-width rule as every other
// concrete integer — no implicit widening across widths), and are valid targets
// of the explicit conversion syntax. These mirror the i64/u64 behaviors.

func TestTypeCheck_I128_AnnotationAndArithmetic(t *testing.T) {
	cases := []string{
		`let x: i128 = 5`,
		`let x: i128 = -5`,
		`let x: i128 = 5
let y: i128 = x + 10`,
		`let x: u128 = 5`,
		`let x: u128 = 5
let y: u128 = x * x`,
		// A negated literal denoting a large magnitude still type-checks against i128.
		`let x: i128 = 9000000000000000000`,
	}
	for _, src := range cases {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

func TestTypeCheck_I128_Conversions(t *testing.T) {
	cases := []string{
		// widen a narrower int into i128 / u128
		`let a: i64 = 5
let b: i128 = i128(a)`,
		`let a: u32 = 5
let b: u128 = u128(a)`,
		// narrow i128 back down (explicit, allowed for a non-constant)
		`let a: i128 = 5
let b: i64 = i64(a)`,
		// a large-unsigned literal (> i64 max) fits u128, like it fits u64
		`let a: u128 = u128(18446744073709551615)`,
	}
	for _, src := range cases {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// A negative value is not assignable to the unsigned u128, exactly as for u64.
func TestTypeCheck_U128_RejectsNegative(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u128 = -1`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected an assignability error for `let x: u128 = -1`, got none")
	}
	found := false
	for _, e := range res.errors {
		if strings.Contains(e.Message, "u128") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error mentioning u128, got: %v", res.errors)
	}
}

// Mixing two different concrete integer widths without an explicit conversion is
// an error — i128 is no exception to Lyra's no-implicit-widening rule.
func TestTypeCheck_I128_NoImplicitWidening(t *testing.T) {
	res := parseCollectAndCheck(t, `let a: i64 = 5
let b: i128 = 7
let c = a + b`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected an error mixing i64 and i128 without a conversion, got none")
	}
}
