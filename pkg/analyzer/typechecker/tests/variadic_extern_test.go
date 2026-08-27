package typechecker_test

import "testing"

// The C variadic marker, `...` in an `extern` signature. Lyra has no variadic functions of
// its own and adding this did not give it any: *calling* one needs nothing from the
// language, since every argument is known at the call site, while *defining* one would need
// an argument pack nothing else here would use.

// A variadic extern takes at least its named parameters and then anything.
func TestVariadic_TakesAnyNumberOfExtraArguments(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
unsafe extern take: (i32, ...) -> i32
let main = () -> void => unsafe {
  take(1)
  take(1, 2)
  take(1, 2, 3.5, 'x')
  ()
}
`, false))
}

// **The floor still holds.** `...` removes the arity *ceiling* and nothing else — C needs
// the named parameters, since they are how a `va_list` is started.
func TestVariadic_TheNamedParametersAreStillRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
unsafe extern take: (i32, i32, ...) -> i32
let main = () -> void => unsafe { take(1); () }
`, false)
	assertErrorsAre(t, res, "take: expected at least 2 argument(s), got 1")
}

// A variadic argument is still FFI-safe or nothing: `...` widens the arity, not the set of
// types that can cross.
func TestVariadic_AnExtraArgumentMustStillBeFFISafe(t *testing.T) {
	res := parseCollectAndCheck(t, `
unsafe extern take: (i32, ...) -> i32
let main = () -> void => unsafe { take(1, "nope"); () }
`, false)
	assertHasErrorContaining(t, res, `argument 2 is string, which has no C spelling`)
}
