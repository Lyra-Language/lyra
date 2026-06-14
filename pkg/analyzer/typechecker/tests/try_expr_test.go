package typechecker_test

import "testing"

// TestTry_SameKindResult: `?` on a Result inside a Result-returning function is
// accepted and unwraps to the success payload.
func TestTry_SameKindResult(t *testing.T) {
	source := `
let parse = (s: string) -> Result<i64, i64> => { parse(s) }
let f = (s: string) -> Result<i64, i64> => {
    let x = parse(s)?
    parse(s)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestTry_SameKindMaybe: `?` on a Maybe inside a Maybe-returning function is
// accepted.
func TestTry_SameKindMaybe(t *testing.T) {
	source := `
let lookup = (s: string) -> Maybe<i64> => { lookup(s) }
let f = (s: string) -> Maybe<i64> => {
    let x = lookup(s)?
    lookup(s)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestTry_CrossKind_MaybeInResultFn: propagating a Maybe out of a
// Result-returning function requires an explicit conversion.
func TestTry_CrossKind_MaybeInResultFn(t *testing.T) {
	source := `
let lookup = (s: string) -> Maybe<i64> => { lookup(s) }
let f = (s: string) -> Result<i64, i64> => {
    let x = lookup(s)?
    f(s)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"cannot propagate Maybe with `?` from a Result-returning function; convert it explicitly")
}

// TestTry_NonResultOperand: `?` on a plain value is rejected.
func TestTry_NonResultOperand(t *testing.T) {
	source := `
let f = (n: i64) -> Result<i64, i64> => {
    let x = n?
    f(n)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"`?` operand must be a Result or Maybe, got i64")
}
