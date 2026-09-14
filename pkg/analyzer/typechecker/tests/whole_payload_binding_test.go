package typechecker_test

import "testing"

// `Rect pair` binds a multi-field payload as one tuple, typed as the fields in order.
func TestWholePayloadBinding_IsATuple(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Shape = Rect(i64, string) | Dot
let f = (s: Shape) -> (i64, string) => match s {
  Rect pair => pair,
  Dot => (0, ""),
}`, false))
}

// It covers the constructor, as `Rect(_, _)` does.
func TestWholePayloadBinding_IsExhaustive(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Shape = Rect(i64, i64) | Dot
let f = (s: Shape) -> i64 => match s {
  Rect pair => pair.0,
  Dot => 0,
}`, false))
}

// Its type is the tuple, not a field: using it as one is refused.
func TestWholePayloadBinding_NotAField(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Shape = Rect(i64, i64) | Dot
let f = (s: Shape) -> i64 => match s {
  Rect pair => pair,
  Dot => 0,
}`, false)
	if len(res.errors) == 0 {
		t.Fatal("returning the (i64, i64) payload as an i64 should be refused")
	}
}

// A constructor with no payload has nothing to bind; it stays an arity error rather than
// binding `()`.
func TestWholePayloadBinding_NoPayload(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Shape = Rect(i64, i64) | Dot
let f = (s: Shape) -> i64 => match s {
  Rect _ => 1,
  Dot x => 0,
}`, false)
	assertErrorsAre(t, res, "Dot takes 0 argument(s) but the pattern has 1")
}
