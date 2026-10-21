package typechecker_test

import "testing"

// **A method whose result is its receiver passes the call's context on to the receiver**
// (10/21, LANGUAGE.md § A call's type arguments). A builder's leaf generic only in its result
// (`leaf<e>(tag) -> Nd<e>`) cannot solve `e` from its arguments; chained through `tagged`,
// whose every declaration returns its `self` type, it takes the binding's `Nd<Msg>` — and
// through two links as well as one.
func TestReceiverContext_ThroughAReceiverReturningMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Opt<e> = Nothing | Has(e)
		struct Nd<e> { tag: string, ev: Opt<e> }
		let leaf<e> = (tag: string) -> Nd<e> => {
			let ev: Opt<e> = Nothing
			Nd { tag: tag, ev: ev }
		}
		let tagged<e> = (self: Nd<e>, t: string) -> Nd<e> => Nd { self | tag: t }
		data Msg = Go
		let one: Nd<Msg> = leaf("a").tagged("b")
		let two: Nd<Msg> = leaf("a").tagged("b").tagged("c")
	`, false)
	assertNoErrors(t, res)
}

// The barrier stays where any candidate of the name returns something other than its
// receiver: here a `string` receiver's `renamed` answers an `i64`, so the context could be the
// wrong one for the receiver, and the leaf is unsolvable as before.
func TestReceiverContext_NotWhereACandidateReturnsSomethingElse(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Opt<e> = Nothing | Has(e)
		struct Nd<e> { tag: string, ev: Opt<e> }
		let leaf<e> = (tag: string) -> Nd<e> => {
			let ev: Opt<e> = Nothing
			Nd { tag: tag, ev: ev }
		}
		let renamed<e> = (self: Nd<e>, t: string) -> Nd<e> => Nd { self | tag: t }
		let renamed = (self: string, t: string) -> i64 => 0
		data Msg = Go
		let v: Nd<Msg> = leaf("a").renamed("b")
	`, false)
	assertErrorsAre(t, res, "leaf: cannot infer type variable e from these arguments; name them explicitly with ::<e>")
}

// A parameter that mentions none of a generic call's variables does not take part in solving
// them (10/21): `Has(1.0)` for an `Opt<f32>` is an `Opt<f64>` at its literal's default, and
// was compared with the parameter by equality — so the call reported "cannot infer type
// variable e", about the variable the second argument solves. The argument check still
// judges it, narrowing the literal; a real mismatch is still one.
func TestGenericCall_ConcreteParameterDoesNotSolve(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Opt<t> = Nothing | Has(t)
		let h<e> = (w: Opt<f32>, xs: []e) -> []e => xs
		let a = h(Has(1.0), [1])
	`, false)
	assertNoErrors(t, res)
	res = parseCollectAndCheck(t, `
		data Opt<t> = Nothing | Has(t)
		let h<e> = (w: Opt<f32>, xs: []e) -> []e => xs
		let a = h(Has("x"), [1])
	`, false)
	assertErrorsAre(t, res, "h: argument 1: cannot assign Opt<string> to Opt<f32>")
}

// A parameter left to its default solves nothing (10/21): `sized<e>(w, children: []e = [])`
// written `sized(1.0)` has an `e` only the context can give, as a variable no parameter
// mentions has — and the default itself, typed in the declaration's variables, no longer
// fails the solve against the context's binding.
func TestGenericCall_DefaultedParameterTakesTheContext(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let sized<e> = (w: f32, children: []e = []) -> []e => children
		let a: []string = sized(1.0)
		let b = sized(1.0, [true])
	`, false)
	assertNoErrors(t, res)
}

// An empty array literal solves nothing, whatever its parameter (10/21): `col([])` under a
// `[]Opt<string>` context takes its variable from the context, as the non-empty call takes
// it from the elements; with no context it is still unsolvable.
func TestGenericCall_EmptyArrayWaitsForTheContext(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Opt<t> = Nothing | Has(t)
		let col<e> = (children: []Opt<e>) -> []Opt<e> => children
		let a: []Opt<string> = col([])
		let b = col([Has(1)])
	`, false)
	assertNoErrors(t, res)
	res = parseCollectAndCheck(t, `
		data Opt<t> = Nothing | Has(t)
		let col<e> = (children: []Opt<e>) -> []Opt<e> => children
		let c = col([])
	`, false)
	assertErrorsAre(t, res, "col: cannot infer type variable e from these arguments")
}
