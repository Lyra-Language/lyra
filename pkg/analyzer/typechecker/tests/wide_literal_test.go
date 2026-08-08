package typechecker_test

import "testing"

// Integer literals past 64 bits (08/08). The magnitude lives in a `Wide *big.Int` on
// the literal node; `Value` is 0 and meaningless for one, which is why `Int64()` answers
// ok=false rather than letting a consumer read the zero.

// A wide literal stays **untyped** while both 128-bit types could hold it, so context
// picks — the same courtesy a small literal gets, and the reason `73786976294838206464`
// is legal as either.
func TestWideLiteral_AdaptsToEither128BitType(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let a: i128 = 73786976294838206464
let b: u128 = 73786976294838206464
`, false))
}

func TestWideLiteral_AtTheI128Boundaries(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let mx: i128 = 170141183460469231731687303715884105727
let mn: i128 = -170141183460469231731687303715884105728
let um: u128 = 340282366920938463463374607431768211455
`, false))
}

// Above i128's positive range only u128 can hold it, so there the literal names a
// concrete type — the rule the large-u64 case already follows.
func TestWideLiteral_AboveI128IsConcreteU128(t *testing.T) {
	res := parseCollectAndCheck(t, `
let bad: i128 = 340282366920938463463374607431768211455
`, false)
	assertHasErrorContaining(t, res, "cannot assign u128 to i128")
}

// A narrower target is caught by the range check, which had to learn big magnitudes at
// the same time: the literal stays untyped where both 128-bit types fit, so
// assignability alone has nothing to object to.
func TestWideLiteral_NarrowerTargetOverflows(t *testing.T) {
	res := parseCollectAndCheck(t, `
let x: u8 = 170141183460469231731687303715884105727
`, false)
	assertHasErrorContaining(t, res, "literal value 170141183460469231731687303715884105727 overflows u8")
}

// Past 128 bits there is no type at all. That is a *collector* diagnostic, so it is
// pinned where collector errors are the subject — pkg/driver's
// TestAnalyze_HugeIntLiteral_NoPanic, which also asserts the placeholder node that keeps
// a later pass from crashing on a typed nil.

// A wide divisor is not zero. `Value` is 0 for one, and `isLiteralZero` folds through
// the same helper — so without `Int64` answering ok=false this reported a division by
// zero on a division by 10^38.
func TestWideLiteral_IsNotAZeroDivisor(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let q = (n: i128) -> i128 => n / 170141183460469231731687303715884105727
`, false))
}
