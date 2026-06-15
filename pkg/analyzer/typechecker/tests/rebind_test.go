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
