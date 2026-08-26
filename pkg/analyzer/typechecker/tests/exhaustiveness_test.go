package typechecker_test

import "testing"

// Exhaustiveness of a `match` whose arms bind with `name @ pattern`.
// **An `@`-bound arm covers what it wraps.** The coverage scan asserted on the arm's pattern
// directly, so `w @ Box(n)` covered no constructor at all — which reported an *exhaustive*
// match as missing the very constructor it handles, rejecting correct code.
func TestExhaustiveness_ABindingPatternCoversWhatItWraps(t *testing.T) {
	res := parseCollectAndCheck(t, `data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
  w @ Box(n) => n,
  Dot => 0,
}`, false)
	assertNoErrors(t, res)
}

// And it covers only what it wraps: the arm above handles `Box`, not `Dot`.
func TestExhaustiveness_ABindingPatternCoversNoMore(t *testing.T) {
	res := parseCollectAndCheck(t, `data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s { w @ Box(n) => n }`, false)
	assertErrorContainsGeneric(t, res, "missing constructors: Dot")
}

// A catch-all spelled as a binding is still a catch-all — `all @ _` matches everything `_`
// does, since a binding tests nothing.
func TestExhaustiveness_ABindingCatchAllIsACatchAll(t *testing.T) {
	res := parseCollectAndCheck(t, `data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s { all @ _ => 0 }`, false)
	assertNoErrors(t, res)
}
