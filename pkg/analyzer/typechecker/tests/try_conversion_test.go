package typechecker_test

import "testing"

// The declarations the prelude would supply: these tests run without it.
const resultAndFrom = `
data Result<t, e> = Ok(t) | Err(e)
data Maybe<t> = Some(t) | None
trait From<s> { from: (s) -> Self }
`

// `?` across error types applies a declared conversion (09/15): `impl From<Low> for High`
// is found by a direct lookup, since `?` knows both the operand's error and the enclosing
// function's. Without one the mismatch is refused naming the impl to write.
func TestTryConversion(t *testing.T) {
	t.Parallel()
	accepted := map[string]string{
		"a declared conversion": resultAndFrom + `
data Low = Oops(string)
data High = Wrapped(Low) | Other(string)
impl From<Low> for High { from = (e) => Wrapped(e) }
let low = (n: i64) -> Result<i64, Low> => if n > 0 { Ok(n) } else { Err(Oops("low")) }
let high = (n: i64) -> Result<i64, High> => Ok(low(n)?)`,
		"one impl per source type": resultAndFrom + `
data A = MkA
data B = MkB
data High = FromA(A) | FromB(B)
impl From<A> for High { from = (e) => FromA(e) }
impl From<B> for High { from = (e) => FromB(e) }
let a = () -> Result<i64, A> => Err(MkA)
let b = () -> Result<i64, B> => Err(MkB)
let high = () -> Result<i64, High> => Ok(a()? + b()?)`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	refused := map[string]struct{ src, want string }{
		"no conversion declared": {resultAndFrom + `
data Low = Oops(string)
data High = Other(string)
let low = (n: i64) -> Result<i64, Low> => Err(Oops("x"))
let high = (n: i64) -> Result<i64, High> => Ok(low(n)?)`,
			"cannot propagate Result with `?`: error type Low is not the enclosing function's error type High and no `impl From<Low> for High` declares the conversion; declare one, or convert at the call with map_err"},
		"a conversion declared for another source": {resultAndFrom + `
data Low = Oops(string)
data Mid = Meh
data High = Wrapped(Mid)
impl From<Mid> for High { from = (e) => Wrapped(e) }
let low = (n: i64) -> Result<i64, Low> => Err(Oops("x"))
let high = (n: i64) -> Result<i64, High> => Ok(low(n)?)`,
			"no `impl From<Low> for High` declares the conversion"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertHasErrorContaining(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

// The context reaches through `?` (09/15): what is wanted of `make(1)?` is the payload,
// so `make(1)` is inferred wanting a Result of it and the enclosing error — which is what
// solves a callee's return-only variable, exactly as the annotation did without the `?`.
func TestTryPropagatesExpectedType(t *testing.T) {
	t.Parallel()
	accepted := map[string]string{
		"a Result-returning callee": resultAndFrom + `
data E = Bad
let make<t> = (n: i64) -> Result<t, E> => Err(Bad)
let go = () -> Result<i64, E> => {
  let v: i64 = make(1)?
  Ok(v)
}`,
		"a Maybe-returning callee": resultAndFrom + `
let make<t> = (n: i64) -> Maybe<t> => None
let go = () -> Maybe<string> => {
  let v: string = make(1)?
  Some(v)
}`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	// The same call with no annotation still has nothing to solve `t` from.
	assertHasErrorContaining(t, parseCollectAndCheck(t, resultAndFrom+`
data E = Bad
let make<t> = (n: i64) -> Result<t, E> => Err(Bad)
let go = () -> Result<i64, E> => {
  let v = make(1)?
  Ok(1)
}`, false), "make: cannot infer type variable t")
}

// A trait method with no receiver — `zero: () -> Self`, `decode: (string) -> Maybe<Self>`
// — is dispatched from what its result is used as (09/15): the declared return is unified
// with the context, and inside a generic body `Self` may solve to a bound variable.
func TestReturnDirectedDispatch(t *testing.T) {
	t.Parallel()
	const zero = resultAndFrom + `
trait Zero { zero: () -> Self }
impl Zero for i64 { zero = () => 0 }
impl Zero for string { zero = () => "" }
`
	accepted := map[string]string{
		"chosen by an annotation": zero + `
let a = () -> i64 => { let z: i64 = Zero::zero(); z }
let b = () -> string => { let s: string = Zero::zero(); s }`,
		"chosen by the return type": zero + `
let a = () -> i64 => Zero::zero()`,
		"an argument beside Self": zero + `
trait Decode { decode: (string) -> Maybe<Self> }
impl Decode for i64 { decode = (s) => if s == "1" { Some(1) } else { None } }
let a = () -> Maybe<i64> => Decode::decode("1")`,
		"Self from an argument's type": zero + `
trait Pair { pair: (Self, i64) -> Self }
impl Pair for i64 { pair = (a, b) => a + b }
let a = () -> i64 => { let r = Pair::pair(1, 2); r }`,
		"through a where bound": zero + `
trait Decode { decode: (string) -> Maybe<Self> }
impl Decode for i64 { decode = (s) => Some(1) }
impl Decode for string { decode = (s) => Some(s) }
let field<t> where t: Decode = (raw: string) -> Maybe<t> => {
  let decoded: Maybe<t> = Decode::decode(raw)
  decoded
}
let a = () -> Maybe<i64> => field("1")
let b = () -> Maybe<string> => field("x")`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	refused := map[string]struct{ src, want string }{
		"no context": {zero + `
let a = () -> i64 => { let z = Zero::zero(); 1 }`,
			"Zero::zero takes no receiver, so its impl is chosen by what the result is used as, and nothing here says; give the result a type, as in `let x: T = Zero::zero(…)`"},
		"no impl for the wanted type": {zero + `
let a = () -> bool => Zero::zero()`,
			"no implementation of Zero::zero for boolean, the type the result is used as"},
		"a variable without the bound": {zero + `
let a<t> = (x: t) -> t => Zero::zero()`,
			"Zero::zero: the result is a t, which has no `where t: Zero` bound"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertHasErrorContaining(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}
