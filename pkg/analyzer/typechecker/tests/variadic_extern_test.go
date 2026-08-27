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

// **An aggregate at the boundary is refused on an ABI ground, not a layout one**, and the
// message has to say which — the layouts *do* match C's, so "take a pointer" is the right
// shape rather than a workaround for a representational mismatch.
//
// By value is a per-target calling convention: clang rewrites the signature differently for
// aarch64 and x86-64 SysV (a 16-byte all-int struct is `[2 x i64]` on one and two separate
// `i64` parameters on the other, and SysV can change the *arity*), so it needs a classifier
// this compiler does not have. See lyra-E063's note.
func TestFFISafe_AnAggregateIsToldToCrossByPointer(t *testing.T) {
	for _, sig := range []string{
		"struct Pt { x: i32, y: i32 }\nunsafe extern pure f: (n: Pt) -> i32",
		"unsafe extern pure f: (n: (i32, i32)) -> i32",
		"data Sh = A | B\nunsafe extern pure f: (n: Sh) -> i32",
	} {
		res := parseCollectAndCheck(t, sig+"\n", false)
		assertHasErrorContaining(t, res, "A struct crosses by pointer: take `^T` and pass `&value`")
		assertHasErrorContaining(t, res, "per-target calling convention")
	}
}

// The string hint names the **scoped lender**, and is kept current with the representation:
// a string carries a NUL past its bytes as of 08/26, so the crossing needs no copy and
// advising one would send a reader to build something that already exists.
func TestFFISafe_AStringIsToldAboutWithCString(t *testing.T) {
	res := parseCollectAndCheck(t, "unsafe extern pure f: (n: string) -> i32\n", false)
	assertHasErrorContaining(t, res, "`std.ffi`'s `with_cstring`, which needs no copy")
}
