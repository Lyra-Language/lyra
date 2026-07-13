package typechecker_test

import "testing"

// Same-scope sequential rebinding (`let x = parse(x)`) is allowed; the
// type-checker resolves later references to the most recent binding.

func TestRebind_SameScope_UsesPriorValue(t *testing.T) {
	res := parseCollectAndCheck(t, `
let x = 5
let x = x + 1
let y: i64 = x
`, false)
	assertNoErrors(t, res)
}

// The motivating idiom: progressive refinement through one name, where each
// step changes the type (e.g. parse a string into a number). Later references
// must see the newest binding's type, not the original one.
func TestRebind_SameScope_ProgressiveRefinement(t *testing.T) {
	res := parseCollectAndCheck(t, `
let parse = (s: string) -> i64 => 0
let raw = "42"
let raw = parse(raw)
let n: i64 = raw
`, false)
	assertNoErrors(t, res)
}

// A rebind whose initializer reads its own prior value must actually type that
// self-reference — resolving it to the previous binding, not the not-yet-typed
// declaration being defined. A bare assertNoErrors can't catch a regression
// here: an untyped (nil) `x + 1` silently *passes* every assignability check
// because nil short-circuits it. So assert the opposite — the rebound value is
// concretely i64, provable by a downstream *incompatible* annotation that must
// error. Without the self-reference fix, `x + 1` is nil and this error vanishes.
func TestRebind_SameScope_SelfReferenceIsTyped(t *testing.T) {
	res := parseCollectAndCheck(t, `
let x = 5
let x = x + 1
let y: string = x
`, false)
	if len(res.errors) == 0 {
		t.Errorf("expected a type error assigning the rebound i64 value to string; got none — the self-reference `x + 1` was likely left untyped")
	}
}

// Rebinding does not erase type checking: assigning the newest binding to an
// incompatible annotation is still an error.
func TestRebind_SameScope_NewestTypeStillChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `
let x = 1
let x = "hello"
let y: i64 = x
`, false)
	if len(res.errors) == 0 {
		t.Errorf("expected a type error assigning a string-bound name to i64, got none")
	}
}
