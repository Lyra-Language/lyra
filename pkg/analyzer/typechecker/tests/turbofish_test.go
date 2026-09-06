package typechecker_test

import "testing"

// Diagnostics for explicit type arguments on a call — `empty::<i64>()`.
//
// The behaviour they pin is that an explicit binding is **not** validated against the
// arguments where it is bound. It does not need to be: the binding is substituted through
// the signature and the ordinary argument check then rejects a call that disagrees, which
// reports the disagreement in the vocabulary the author is thinking in (i64 against string)
// rather than as a failure to infer a variable they just named.

func TestTypeCheck_Turbofish_SolvesAnUnreachableVariable(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let xs = empty::<i64>()
`, false))
}

func TestTypeCheck_Turbofish_WrongArityIsReported(t *testing.T) {
	res := parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let xs = empty::<i64, string>()
`, false)
	assertErrorsAre(t, res, "empty: expected 1 type argument(s), got 2")
}

// A turbofish that contradicts an argument surfaces as an argument-type error, not as an
// inference failure — the binding won, and then the call did not match it.
func TestTypeCheck_Turbofish_ContradictingAnArgumentBlamesTheArgument(t *testing.T) {
	res := parseCollectAndCheck(t, `
let id<t> = pure (x: t) -> t => x
let a = id::<i64>("x")
`, false)
	assertErrorsAre(t, res, "id: argument 1: cannot assign string to i64")
}

// The hint is offered only where a turbofish is the actual fix: a variable no parameter
// mentions, which the arguments therefore cannot reach.
//
// The call must also sit in **no context**, since a context now solves such a variable on
// its own (seedFromExpectedReturn) — which is why this binding carries no annotation.
func TestTypeCheck_InferenceFailure_HintsTheTurbofishWhenUnreachable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let xs = empty()
`, false)
	assertErrorsAre(t, res,
		"empty: cannot infer type variable t from these arguments; name them explicitly with ::<t>")
}

// …and withheld where it is not. `t` is mentioned by `xs`, so the arguments *can* reach it
// and a failure here is a mismatch; naming `t` explicitly would move the same disagreement
// to the argument check rather than resolving it, so advising it would cost a round trip.
func TestTypeCheck_InferenceFailure_WithholdsTheHintWhenReachable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let first_of<t> = (xs: []t) -> t => xs[0]
let a = [1, 2, 3]
let b = first_of(a)
`, false)
	assertErrorsAre(t, res, "first_of: cannot infer type variable t from these arguments")
}
