package typechecker_test

import "testing"

// The `rune` primitive type (a Unicode code point, i32 — Go/Odin's `rune`, the
// former `char`) flows through the typechecker like any other scalar. A `'a'`
// character literal has type `rune`.

func TestRune_AnnotatedBinding(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let c: rune = 'a'`, false))
}

func TestRune_ParamAndReturn(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let id = (c: rune) -> rune => c`, false))
}

func TestRune_StructField(t *testing.T) {
	src := `struct Cell { ch: rune, }
let first = () -> rune => {
  let c: Cell = Cell { ch: 'x' }
  c.ch
}`
	assertNoErrors(t, parseCollectAndCheck(t, src, false))
}

// Note: a `match` on a rune *literal pattern* (`'a' => …`) is intentionally not
// covered here — character literal patterns don't parse yet (a separate grammar
// gap, unrelated to `rune` as a type), so such a test would pass vacuously on a
// partial tree (parseCollectAndCheck doesn't fail on CST ERROR nodes).

// A real primitive rejects a mismatched initializer — the generic-variable
// representation used to mask this.
func TestRune_RejectsIntegerInitializer(t *testing.T) {
	res := parseCollectAndCheck(t, `let c: rune = 5`, false)
	if len(res.errors) == 0 {
		t.Error("assigning an integer to a `rune` binding should be a type error")
	}
}

// ── ordering and conversions ──────────────────────────────────────────────────
//
// A rune is an *ordered, convertible, non-arithmetic* scalar (the split Rust
// draws for `char`). Ordering makes classification expressible; explicit
// conversions are the escape hatch for the arithmetic rune itself doesn't have.
// Before this, none of the three worked, so is-digit/is-alpha logic could not be
// written at all.

func TestRune_OrderingComparison_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let isDigit = (c: rune) -> bool => c >= '0' && c <= '9'
	`, false))
}

// A rune orders only against another rune: mixing it with an integer needs an
// explicit conversion, so the code-point/number boundary stays written down.
func TestRune_OrderingAgainstInt_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (c: rune) -> bool => c < 5`, false)
	if len(res.errors) == 0 {
		t.Error("comparing a rune with an integer should require an explicit conversion")
	}
}

// Arithmetic stays rejected — `rune` is not numeric. Convert first.
func TestRune_Arithmetic_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (c: rune) -> rune => c + 1`, false)
	if len(res.errors) == 0 {
		t.Error("rune arithmetic should be rejected; convert with i32(c) first")
	}
}

func TestRune_ConversionsToAndFromInt_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let digitValue = (c: rune) -> i32 => i32(c) - i32('0')
		let widen = (c: rune) -> i64 => i64(c)
		let fromCode = (n: i32) -> rune => rune(n)
		let fromLiteral = () -> rune => rune(65)
	`, false))
}

// Only the integer types: a code point has no meaningful float reading.
func TestRune_ConversionWithFloat_Error(t *testing.T) {
	for _, src := range []string{
		`let f = (c: rune) -> f64 => f64(c)`,
		`let f = (x: f64) -> rune => rune(x)`,
		`let f = (s: string) -> rune => rune(s)`,
	} {
		res := parseCollectAndCheck(t, src, false)
		if len(res.errors) == 0 {
			t.Errorf("expected a conversion error for %q", src)
		}
	}
}
