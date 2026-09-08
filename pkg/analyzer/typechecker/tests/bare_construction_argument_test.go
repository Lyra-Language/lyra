package typechecker_test

import "testing"

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

// The bare form still binds a variable nothing else does, as it always has — it only
// stops *contradicting* a binding.
func TestBareConstructionArgumentStillBindsAFreeVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let wrap<t> = pure (v: t) -> t => v
let main = () -> void => {
  let n = wrap(None)
}
`, false)
	assertNoErrors(t, res)
}
