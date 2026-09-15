package typechecker_test

import "testing"

// The context settles what the arguments leave open (09/16). A variable only a lambda
// literal's return reaches used to be bound to the literal's own default; now the call's
// context binds it first, and the lambda is elaborated against the settled type.
func TestContextSettlesLambdaSolvedVariable(t *testing.T) {
	t.Parallel()
	const functor = `
struct Box<t> { v: t }
trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
`
	accepted := map[string]string{
		"a trait method's variable from the return context": functor + `
let seven = (b: Box<bool>) -> Box<i64> => b.map((x) => 7)`,
		"narrowed to the context's width": functor + `
let small = (b: Box<bool>) -> Box<u8> => b.map((x) => 200)`,
		"inside a default body": `
struct Box<t> { v: t }
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  seven: (Self<a>) -> Self<i64> = (self) => self.map((x) => 7)
}
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
let a = () -> i64 => Box { v: "s" }.seven().v`,
		"a generic function's variable": `
struct Box<t> { v: t }
let wrap<t> = (f: () -> t) -> Box<t> => Box { v: f() }
let a = () -> Box<u8> => wrap(() => 9)`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	// A typed argument still wins over the context; the mismatch is then the argument's.
	assertHasErrorContaining(t, parseCollectAndCheck(t, functor+`
let bad = (b: Box<bool>) -> Box<i64> => b.map((x) -> string => "s")`, false), "return type mismatch")
	// Out of range for the settled width is the literal's error, not an inference failure.
	assertHasErrorContaining(t, parseCollectAndCheck(t, functor+`
let big = (b: Box<bool>) -> Box<u8> => b.map((x) => 300)`, false), "300")
}

// A constructor's payload sees its slot's type from the construction's own context
// (09/16): `Err(Zero::zero())` under `Result<string, string>` infers the payload wanting a
// `string`, which is what a receiver-less call or a generic callee there needs.
func TestConstructorPayloadTakesContext(t *testing.T) {
	t.Parallel()
	const base = `
data Result<t, e> = Ok(t) | Err(e)
data Maybe<t> = Some(t) | None
trait Zero { zero: () -> Self }
impl Zero for string { zero = () => "" }
impl Zero for i64 { zero = () => 0 }
`
	accepted := map[string]string{
		"a receiver-less call in the error slot": base + `
let a = () -> Result<i64, string> => Err(Zero::zero())`,
		"a receiver-less call in the value slot": base + `
let a = () -> Result<i64, string> => Ok(Zero::zero())`,
		"a generic callee in the payload": base + `
let mk<t> = () -> t => panic("x")
let a = () -> Maybe<string> => Some(mk())`,
		"a literal still narrows": base + `
let a = () -> Maybe<u8> => Some(250)`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	// With nothing in the context (`let d = Err(Zero::zero())`) there is still nothing to
	// choose by.
	assertHasErrorContaining(t, parseCollectAndCheck(t, base+`
let a = () -> i64 => { let d = Err(Zero::zero()); 1 }`, false), "takes no receiver")
}
