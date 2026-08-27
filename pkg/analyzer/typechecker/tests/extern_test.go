package typechecker_test

import "testing"

// `extern` declares a foreign function: a signature with no body, and the effect bound its
// caller is asked to trust. The design is settled in todo.md (Foreign functions); this is
// the front end of it — the declaration is collected, calls are checked against the
// signature, effects come from the bound, and `@link` is gathered. Lowering is not built.

func TestExtern_CallChecksAgainstTheSignature(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
unsafe extern pure sqrt: (f64) -> f64
let main = () -> void => { unsafe { println("${sqrt(16.0)}") } }
`, false))
}

func TestExtern_CallArityIsChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `
unsafe extern pure sqrt: (f64) -> f64
let main = () -> void => { unsafe { println("${sqrt(16.0, 2.0)}") } }
`, false)
	assertHasErrorContaining(t, res, "expected 1 argument(s), got 2")
}

// Every extern is unsafe to *call*, whatever bound it claims — the bound is about effects,
// not about safety.
func TestExtern_CallNeedsAnUnsafeContext(t *testing.T) {
	res := parseCollectAndCheck(t, `
unsafe extern pure sqrt: (f64) -> f64
let main = () -> void => { println("${sqrt(16.0)}") }
`, false)
	assertHasErrorContaining(t, res, `calling unsafe function "sqrt" requires an `+"`unsafe`")
}

// The effect rule — that an unbound extern is not callable from `pure`, and a bound one
// is — is exercised in the checker suite, which runs the purity pass (extern_purity_test).

// **Narrowing the bound is the unsafe act.** Declaring an extern claims nothing and is
// safe; claiming `pure` asserts something no compiler can check, and a wrong claim does not
// fail here — it is believed, and corrupts every caller's effect analysis.
func TestExtern_BoundWithoutUnsafeIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `extern pure sqrt: (f64) -> f64`, false)
	assertHasErrorContaining(t, res, "write `unsafe extern` to assert it")
}

func TestExtern_NoBoundNeedsNoUnsafe(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `extern getpid: () -> i32`, false))
}

// The inverse is deliberately not an error: `unsafe extern` with no bound asserts nothing
// and is merely redundant, and a program mid-edit should not stop compiling for it.
func TestExtern_UnsafeWithoutABoundIsAllowed(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `unsafe extern getpid: () -> i32`, false))
}

// **FFI-safe types.** Refusing at the signature is what leaves no room for an implicit
// conversion, and so no nul-termination policy to get wrong.
//
// **A hint that describes the representation goes stale when the representation changes**,
// and this test was the reason nothing noticed: it asserted the string hint's claim that a
// Lyra string "is not NUL-terminated, so it needs a copy", which stopped being true on
// 08/26 — so the pin held the *stale* wording in place rather than catching it. Assert what
// a reader is told to *do*, which is stable, rather than the sentence explaining why.
func TestExtern_SignatureRefusesTypesWithNoCSpelling(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"string", `unsafe extern pure puts: (string) -> i32`, "`std.ffi`'s `with_cstring`"},
		{"array", `unsafe extern pure sum: ([]i64) -> i64`, "xs.data()"},
		{"closure", `unsafe extern pure go: ((i64) -> i64) -> i32`, "not a C function pointer"},
		{"bool", `unsafe extern pure ok: (bool) -> i32`, "C's `_Bool` is a byte"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			assertHasErrorContaining(t, res, c.want)
		})
	}
}

func TestExtern_SignatureAcceptsScalarsPointersAndVoid(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
unsafe extern det memcpy: (^mut u8, ^u8, i64) -> ^mut u8
unsafe extern pure scale: (f32, i16, rune) -> f64
extern flush: () -> void
`, false))
}

// A newtype is looked *through*: it is nominal only, so `newtype Fd = i32` is an i32 at the
// boundary — and refusing it would refuse the one wrapper that makes a foreign signature
// readable.
func TestExtern_SignatureLooksThroughANewtype(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Fd = i32
unsafe extern det close: (Fd) -> i32
`, false))
}

// **A borrow modifier is refused where the types are**, because it is the same question:
// what may cross. `mut`/`ref` is Lyra's own by-reference passing, decided by the compiler
// for a Lyra callee — at the boundary it is either inert (a `mut` scalar still goes by
// value, so the modifier says something the call does not do) or an outright ABI mismatch
// (`mut ^i64` reads as "a pointer" and would pass an `i64**`). What C has is the pointer,
// which the signature can already say.
func TestExtern_BorrowModifierOnAParameterIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
unsafe extern pure takes: (mut i64, ref ^u8) -> i64
`, false)
	assertHasErrorContaining(t, res, "parameter 1 of `extern takes` is `mut`")
	assertHasErrorContaining(t, res, "parameter 2 of `extern takes` is `ref`")
}

// `own` is not in that set: it is the move axis rather than the by-reference one, and an
// extern's FFI-safe types are all copied scalars and pointers, so moving one means
// nothing either way. Refusing it would be a rule with no failure behind it.
func TestExtern_OwnOnAParameterIsNotRefused(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
unsafe extern pure takes: (own i64) -> i64
`, false))
}
