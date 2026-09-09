package typechecker_test

import "testing"

// The C variadic marker, `...` in an `extern` signature. Lyra has no variadic functions of
// its own and adding this did not give it any: *calling* one needs nothing from the
// language, since every argument is known at the call site, while *defining* one would need
// an argument pack nothing else here would use.

// A variadic extern takes at least its named parameters and then anything.
func TestVariadic_TakesAnyNumberOfExtraArguments(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
unsafe extern take: (n: i32, ...) -> i32
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
unsafe extern take: (n: i32, n2: i32, ...) -> i32
let main = () -> void => unsafe { take(1); () }
`, false)
	assertErrorsAre(t, res, "take: expected at least 2 argument(s), got 1")
}

// A variadic argument is still FFI-safe or nothing: `...` widens the arity, not the set of
// types that can cross.
func TestVariadic_AnExtraArgumentMustStillBeFFISafe(t *testing.T) {
	res := parseCollectAndCheck(t, `
unsafe extern take: (n: i32, ...) -> i32
let main = () -> void => unsafe { take(1, "nope"); () }
`, false)
	assertHasErrorContaining(t, res, `argument 2 is string, which has no C spelling`)
}

// **An aggregate with a C layout crosses by value** (09/09), classified per target by
// `pkg/abi`. This used to be lyra-E063 with a message saying to take a pointer, and the
// reason it was refused was real — by value is a per-target calling convention, and a
// wrong one links cleanly and computes garbage. What changed is that the classifier now
// exists and is validated against clang shape by shape.
//
// The front end admits it **unconditionally**, on purpose: whether a given target can
// classify it is the backend's question, so `lyrac check` does not change its answer
// according to which clang happens to be installed.
func TestFFISafe_AnAggregateWithACLayoutCrossesByValue(t *testing.T) {
	for _, sig := range []string{
		"struct Pt { x: i32, y: i32 }\nunsafe extern pure f: (n: Pt) -> i32",
		"unsafe extern pure f: (n: (i32, i32)) -> i32",
		"struct Pt { x: i32, y: i32 }\nunsafe extern pure f: () -> Pt",
		"union U { a: u32, b: f32 }\nunsafe extern pure f: (n: U) -> i32",
	} {
		res := parseCollectAndCheck(t, sig+"\nlet main = () -> void => println(\"x\")\n", false)
		assertNoErrors(t, res)
	}
}

// **What is still refused is a type with no C storage at all** — a `data` type carries a
// tag Lyra invented, a string and a dynamic array are refcounted boxes, a closure is
// `{code, env}`. Having a layout is the question; a `data` type's layout is not C's.
func TestFFISafe_ATypeWithNoCLayoutIsStillRefused(t *testing.T) {
	for _, sig := range []string{
		"data Sh = A | B\nunsafe extern pure f: (n: Sh) -> i32",
		"unsafe extern pure f: (n: string) -> i32",
		"unsafe extern pure f: (n: []i32) -> i32",
	} {
		res := parseCollectAndCheck(t, sig+"\n", false)
		assertHasErrorContaining(t, res, "which has no C spelling")
	}
}

// The string hint names the **scoped lender**, and is kept current with the representation:
// a string carries a NUL past its bytes as of 08/26, so the crossing needs no copy and
// advising one would send a reader to build something that already exists.
func TestFFISafe_AStringIsToldAboutWithCString(t *testing.T) {
	res := parseCollectAndCheck(t, "unsafe extern pure f: (n: string) -> i32\n", false)
	assertHasErrorContaining(t, res, "`std.ffi`'s `with_cstring`, which needs no copy")
}
