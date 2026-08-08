package typechecker_test

import "testing"

// The `checked_*` builtins (08/08): `(self: T, other: T) -> Maybe<T>` on any concrete
// integer width.
//
// These run without the prelude, so each declares the Maybe it needs. The canonical
// fallback stamps an unmarked, correctly-shaped `data Maybe` when nothing claims the
// kind — and the *shape* is load-bearing: with no canonical Maybe the builtin has no
// type to return, so it does not exist and the call reports "no such method" rather
// than naming a type the program does not have.
const canonicalMaybe = `
data Maybe<t> = Some(t) | None
`

func TestChecked_ReturnsAMaybeOfTheReceiverType(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, canonicalMaybe+`
let a: Maybe<i32> = i32(1).checked_add(2)
let b: Maybe<u8> = u8(1).checked_sub(2)
let c: Maybe<i64> = i64(3).checked_mul(4)
let d: Maybe<i16> = i16(9).checked_div(3)
`, false))
}

// The result is a Maybe, not the bare integer — which is the whole point, and the
// mistake the type system should catch first.
func TestChecked_ResultIsNotTheBareInteger(t *testing.T) {
	res := parseCollectAndCheck(t, canonicalMaybe+`
let bad: i32 = i32(1).checked_add(2)
`, false)
	assertHasErrorContaining(t, res, "cannot assign")
}

// Integers only. A float has no wrapping representation to overflow out of, so the
// method does not exist on one and the message is the ordinary no-such-method.
func TestChecked_NotAvailableOnFloats(t *testing.T) {
	res := parseCollectAndCheck(t, canonicalMaybe+`
let bad = f64(1.5).checked_add(2.5)
`, false)
	assertHasErrorContaining(t, res, "checked_add")
}

// The receiver fixes the width, so an untyped literal argument narrows to it rather
// than dragging the operation to the i64 default.
func TestChecked_ArgumentNarrowsToTheReceiverWidth(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, canonicalMaybe+`
let a: Maybe<u8> = u8(200).checked_mul(2)
`, false))
}

// A newtype over an integer reaches it through the base-method fallback, like every
// other builtin — a wrapped integer you cannot do arithmetic on is not a trade anyone
// would take.
func TestChecked_ReachableThroughANewtype(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, canonicalMaybe+`
newtype Count = i64
let n: Count = 5
let a: Maybe<i64> = n.checked_add(1)
`, false))
}

// With **no** Maybe declared anywhere, the signature still resolves: `canonicalTypeName`
// falls back to the bare name for truly ambient use, so the call type-checks against a
// `Maybe<i32>` the program does not declare and the *backend* reports it
// ("checked_add() must return a Maybe, got Maybe<i32>").
//
// That is the arrangement `read_line` already has, and it is rule 5 working rather than
// a hole: the error is loud and names the missing type. Pinned here so the front end's
// silence is a recorded decision rather than an oversight — the alternative, refusing
// the method when no declaration carries the kind, would also refuse it for a program
// that legitimately relies on the ambient fallback.
func TestChecked_TypeChecksAgainstTheAmbientMaybe(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let bad = i32(1).checked_add(2)
`, false))
}
