package typechecker_test

import "testing"

// A constructor applied to the wrong number of values was not checked until 09/28:
// `Two(1)` against `Two(i64, i64)` and `Val(1, 2)` against `Val(i64)` type-checked, the
// extra value ignored and the missing one never built. Found while making `Ok()` — a
// constructor applied to nothing — the spelling of a `void` payload.

const arityTypes = `
  data Pair = Two(i64, i64) | Empty
  data Opt = Val(i64) | Nothing
  data Box<t> = Wrap(t)
  data Step = Done(void) | Failed(i64)
`

func TestConstructorArity_TooFew(t *testing.T) {
	res := parseCollectAndCheck(t, arityTypes+`
  let p = Two(1)
	`, false)
	assertErrorsAre(t, res, "Two takes 2 argument(s) but was given 1")
}

func TestConstructorArity_TooMany(t *testing.T) {
	res := parseCollectAndCheck(t, arityTypes+`
  let o = Val(1, 2)
	`, false)
	assertErrorsAre(t, res, "Val takes 1 argument(s) but was given 2")
}

func TestConstructorArity_NullaryIsWrittenBare(t *testing.T) {
	res := parseCollectAndCheck(t, arityTypes+`
  let e = Empty()
  let f = Nothing(3)
	`, false)
	assertErrorsAre(t, res,
		"Empty takes no payload; write it bare, as `Empty`",
		"Nothing takes no payload but was given 1 value(s)")
}

// The empty application is only for a void payload: a concrete one refuses it.
func TestConstructorArity_EmptyApplicationNeedsAVoidPayload(t *testing.T) {
	res := parseCollectAndCheck(t, arityTypes+`
  let o = Val()
	`, false)
	assertErrorsAre(t, res, "Val takes 1 argument(s); `Val()` is only for a void payload")
}

// A declared void payload is built with `Done()`, and only that way.
func TestConstructorArity_DeclaredVoidPayload(t *testing.T) {
	res := parseCollectAndCheck(t, arityTypes+`
  let good = Done()
  let bad = Done(1)
	`, false)
	assertErrorsAre(t, res, "Done's payload is void, so it is written `Done()`")
}

// `Wrap()` binds the type variable to void: the value is a `Box<void>`, and matching it
// with `Wrap()` or `Wrap(_)` binds nothing.
func TestConstructorArity_EmptyApplicationBindsTheVariableToVoid(t *testing.T) {
	res := parseCollectAndCheck(t, arityTypes+`
  let b: Box<void> = Wrap()
  let n = match b {
    Wrap() => 1,
  }
  let m = match b {
    Wrap(_) => 2,
  }
	`, false)
	assertNoErrors(t, res)
}
