package typechecker_test

import (
	"strings"
	"testing"
)

// A bare construction — `None` — passed where a type variable is already bound by
// another argument adopts that binding instead of failing the solve. Both variables were
// reported unsolvable, for a call whose first argument fixes both.
func TestBareConstructionArgumentAdoptsTheBoundVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
struct Box<k, v> { key: k, value: v }
let put<k, v> = pure (b: Box<k, v>, key: k, value: v) -> Box<k, v> => Box { key: key, value: value }
let main = () -> void => {
  let b: Box<string, Maybe<string>> = Box { key: "a", value: Some("s") }
  let c = put(b, "b", None)
  let d = put(b, "c", Some("x"))
}
`, false)
	assertNoErrors(t, res)
}

// The bare form still binds a variable nothing else does — `t` solves to `Maybe` rather
// than being reported unsolvable, which is what this test has always been about.
//
// **What changed on 09/09 is one layer in**: `t` is solved, and `Maybe`'s *own* parameter
// is not, by anything anywhere. That program cannot be lowered — the pre-E073 compiler
// answers `unknown named type "Maybe"` the moment the value is used — so it now draws
// lyra-E073 instead of checking clean.
//
// The distinction the test still guards is that the failure is about `Maybe`'s parameter
// and **not** about `t`: the solve succeeded, and an error naming `t` would mean the
// adoption rule had regressed.
func TestBareConstructionArgumentStillBindsAFreeVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let wrap<t> = pure (v: t) -> t => v
let main = () -> void => {
  let n = wrap(None)
}
`, false)
	assertHasErrorContaining(t, res, "nothing here solves Maybe's type parameter")
	for _, e := range res.errors {
		if strings.Contains(e.Message, "type variable") || strings.Contains(e.Message, "unsolv") {
			t.Errorf("the solve for `t` should have succeeded; got: %s", e.Message)
		}
	}
}
