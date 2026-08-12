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

// A newtype does NOT reach checked arithmetic through the base-method fallback
// (lyra-E043) — a deliberate reversal (08/12) of what this test used to pin. The old
// comment argued "a wrapped integer you cannot do arithmetic on is not a trade anyone
// would take", and missed that the operator half was already making exactly that
// trade: `Count + Count` is refused until the type has an operator impl, so the
// fallback handed out through methods the arithmetic the operators withheld — and
// accepted a mixed operand (`n.checked_add(plain_i64)`) doing it, since the
// signature's parameter is the base. Nor is the wrapped integer left with nothing:
// `let raw: i64 = n` is one step (documented assignability), and an operator impl is
// the opt-in. Refusal details and the full reasoning: constrained_type_test.go's
// lyra-E043 section, and COMPLETED.md 08/12.
func TestChecked_NotReachableThroughANewtype(t *testing.T) {
	res := parseCollectAndCheck(t, canonicalMaybe+`
newtype Count = i64
let n: Count = 5
let a: Maybe<i64> = n.checked_add(1)
`, false)
	assertHasErrorContaining(t, res, "arithmetic on a newtype is opt-in")
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

// ── 128-bit constant folding (08/08) ─────────────────────────────────────────
//
// Folding is arbitrary precision now. It was int64-bound, so an expression whose value
// exceeded that range simply declined to fold — and a declined fold is a *silent* one:
// the range check had nothing to check, and `let d: u8 = 10^20 + 1` reached the backend,
// where the operand had already been narrowed to a width it does not fit and the result
// was invalid IR.

func TestFolding_WideExpressionAgainstANarrowTarget(t *testing.T) {
	res := parseCollectAndCheck(t, `
let d: u8 = 100000000000000000000 + 1
`, false)
	assertHasErrorContaining(t, res, "literal value 100000000000000000001 overflows u8")
}

// The same for i64, which is the case that shows the old bound was the *folder's* rather
// than the language's: the target is an ordinary width and the value is simply too big.
func TestFolding_WideExpressionAgainstI64(t *testing.T) {
	res := parseCollectAndCheck(t, `
let d: i64 = 100000000000000000000 + 1
`, false)
	assertHasErrorContaining(t, res, "overflows i64")
}

// Both operands fit an int64 and their product does not — the case arbitrary precision
// exists for, since an int64 walk cannot represent the answer even though every leaf is
// representable.
func TestFolding_ProductThatEscapesInt64(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let d: i128 = 10000000000 * 10000000000
`, false))
}

func TestFolding_WideExpressionFittingItsTarget(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let d: i128 = 100000000000000000000 + 1
`, false))
}
