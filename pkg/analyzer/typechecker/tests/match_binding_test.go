package typechecker_test

import "testing"

// A match arm's pattern variables are bound in the arm body, so a payload binding
// can be used (and computed on) there. This previously failed with "undefined
// identifier" — the arm body was inferred without the pattern's bindings in scope.
func TestMatch_ArmBindingUsableInBody(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe = None | Some(i64)
let f = (m: Maybe) -> i64 => match m {
  None => 0,
  Some x => x + 1,
}
`, false)
	assertNoErrors(t, res)
}

// A multi-field payload binds each field positionally (`Rect(w, h)`), and both
// are usable in the arm body.
func TestMatch_MultiFieldPayloadBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Shape = Circle(i64) | Rect(i64, i64)
let area = (s: Shape) -> i64 => match s {
  Circle(r) => r,
  Rect(w, h) => w + h,
}
`, false)
	assertNoErrors(t, res)
}

// Arm bodies adapt widths to each other and to the outer context: a bare untyped
// literal arm (`None => 0`) unifies with a concrete sibling (`Some x => x`, x:u8)
// and with the declared return type (u8) instead of defaulting to i64 and
// clashing. Without match-arm width propagation this reported "match arms have
// incompatible types: i64 vs u8".
func TestMatch_ArmWidthPropagation(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe = None | Some(u8)
let f = (m: Maybe) -> u8 => match m {
  None => 0,
  Some x => x,
}
`, false)
	assertNoErrors(t, res)
}

// An all-untyped-literal match adapts to the function's declared return width
// (u8), rather than defaulting every arm to i64 and mismatching the return type.
func TestMatch_AllLiteralArmsTakeReturnWidth(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Color = Red | Green | Blue
let f = (c: Color) -> u8 => match c {
  Red => 1,
  Green => 2,
  Blue => 3,
}
`, false)
	assertNoErrors(t, res)
}

// An arm guard is type-checked with the pattern's bindings in scope, so it may
// reference them (`Some(x) if x > 0`). A guarded arm doesn't count toward
// exhaustiveness, so the following unguarded arms must still cover the type.
func TestMatch_GuardReferencesBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe = None | Some(u8)
let f = (m: Maybe) -> u8 => match m {
  Some(x) if x > 0 => x,
  Some(x) => 0,
  None => 9,
}
`, false)
	assertNoErrors(t, res)
}

// A guard condition that isn't a bool is an error.
func TestMatch_GuardMustBeBool(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (n: u8) -> u8 => match n {
  x if x => 1,
  _ => 2,
}
`, false)
	assertErrorsAre(t, res, "match arm guard must be a bool, got u8")
}
