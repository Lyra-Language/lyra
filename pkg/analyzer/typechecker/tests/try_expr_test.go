package typechecker_test

import "testing"

// TestTry_SameKindResult: `?` on a Result inside a Result-returning function is
// accepted and unwraps to the success payload.
func TestTry_SameKindResult(t *testing.T) {
	source := `
data Result<t, e> = Ok t | Err e
let parse = (s: string) -> Result<i64, i64> => { Ok(0) }
let f = (s: string) -> Result<i64, i64> => {
    let x = parse(s)?
    Ok(x)
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

// TestTry_DeclaredCanonicalMaybe: a program that declares its own
// correctly-shaped `data Maybe<t> = Some t | None` still works with `?`, the
// positive counterpart to TestTry_NonCanonicalResultShape below (which
// declares a same-named-but-wrong-shaped Result and gets rejected).
func TestTry_DeclaredCanonicalMaybe(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let lookup = (s: string) -> Maybe<i64> => { Some(0) }
let f = (s: string) -> Maybe<i64> => {
    let x = lookup(s)?
    Some(x)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestTry_NonCanonicalResultShape: a `data Result` whose constructors aren't
// Ok/Err (e.g. a totally unrelated sum type that just happens to share the
// name and arity) is not treated as Result-shaped: `?` on it is rejected the
// same way as any other non-Result/Maybe operand.
func TestTry_NonCanonicalResultShape(t *testing.T) {
	source := `
data Result<a, b> = Foo a | Bar b
let make = (n: i64) -> Result<i64, i64> => { Foo(n) }
let f = (n: i64) -> i64 => {
    let x = make(n)?
    n
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"`?` operand must be a Result or Maybe, got Result")
}
