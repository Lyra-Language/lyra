package typechecker_test

import "testing"

// A trait method may be generic in a variable of its own — `mapv: (Self, (i64) -> b) -> b` —
// and a call at a concrete receiver solves it from the arguments, exactly as a generic
// function's call does. It used to stay unsolved: the call answered `b` and checked every
// argument against it, so `3.mapv((x: i64) -> i64 => x)` was refused as
// `cannot assign (i64) -> i64 to (i64) -> b`.

func TestTraitMethodTypeVars_Solved(t *testing.T) {
	t.Parallel()
	const mapper = `
trait Mapper { mapv: (Self, (i64) -> b) -> b }
impl Mapper for i64 { mapv = (self, f) => f(self) }
`
	cases := map[string]string{
		"from an annotated lambda": mapper + `
let a = () -> string => 3.mapv((x: i64) -> string => "n")`,
		"from an untyped lambda's body": mapper + `
let a = () -> i64 => 3.mapv((x) => x * 2)`,
		"from a plain argument, generic impl": `
struct Box<t> { v: t }
trait Pair { pair: (Self, b) -> (Self, b) }
impl Pair for Box<t> { pair = (self, x) => (self, x) }
let a = () -> string => Box { v: 1 }.pair("two").1`,
		"qualified call": mapper + `
let a = (n: i64) -> i64 => Mapper::mapv(n, (x) => x + 1)`,
		"default method": `
trait Mapper {
  get: (Self) -> i64
  mapv: (Self, (i64) -> b) -> b = (self, f) => f(self.get())
}
impl Mapper for i64 { get = (self) => self }
let a = () -> bool => 3.mapv((x) => x > 1)`,
		// The impl's `t` and the method's `t` are different variables; the method's is
		// renamed away from the impl's rather than conflated with it.
		"method variable named like the impl's": `
struct Box<t> { v: t }
trait Pair { pair: (Self, t) -> (Self, t) }
impl Pair for Box<t> { pair = (self, x) => (self, x) }
let a = () -> i64 => Box { v: 1 }.pair("two").0.v
let b = () -> string => Box { v: 1 }.pair("two").1`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// The solve is checked: a type the arguments do not determine, or determine two ways, is
// refused the way a generic function's call is.
func TestTraitMethodTypeVars_Unsolvable(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ src, want string }{
		"return-only": {`
trait Mk { mk: (Self) -> []b }
impl Mk for i64 { mk = (self) => [] }
let a = () -> i64 => { let xs = 3.mk(); 0 }`, "mk: cannot infer type variable b from these arguments"},
		"inconsistent": {`
trait Two { two: (Self, b, b) -> b }
impl Two for i64 { two = (self, x, y) => x }
let a = () -> bool => 3.two(1, true)`, "two: cannot infer type variable b from these arguments"},
		"answer checked against the solve": {`
trait Mapper { mapv: (Self, (i64) -> b) -> b }
impl Mapper for i64 { mapv = (self, f) => f(self) }
let a = () -> i64 => 3.mapv((x: i64) -> string => "n")`, "a: return type mismatch: expected i64, got string"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

// Through a `where` bound there is no per-call solution for the backend's candidates to carry,
// so the call is refused by name instead of as an argument mismatch.
func TestTraitMethodTypeVars_ThroughABoundRefused(t *testing.T) {
	t.Parallel()
	res := parseCollectAndCheck(t, `
trait Mapper { mapv: (Self, (i64) -> b) -> b }
impl Mapper for i64 { mapv = (self, f) => f(self) }
let twice<t> where t: Mapper = (v: t) -> i64 => v.mapv((x) => x)`, false)
	assertErrorsAre(t, res,
		"Mapper::mapv: a method generic in its own type variable b cannot yet be called through a `where` bound; call it on a concrete receiver")
}

// `Self<b>` is the implementing type's head at new arguments, for an impl whose target is a
// generic type applied to exactly that many distinct type variables. The receiver solves the
// `a` of `Self<a>` — nothing else in `map`'s signature says what it is.
func TestTraitSelfApplied_Accepted(t *testing.T) {
	t.Parallel()
	const functor = `
trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
`
	cases := map[string]string{
		"struct": `
struct Box<t> { v: t }` + functor + `
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
let a = () -> string => Box { v: 3 }.map((x) => "n").v
let b = () -> bool => Box { v: 3 }.map((x) => x * 2).map((y) => y > 1).v`,
		"data type": `
data Opt<t> = No | Yes(t)` + functor + `
impl Functor for Opt<t> { map = (self, f) => match self { Yes(v) => Yes(f(v)), No => No } }
let a = () -> Opt<bool> => Yes(4).map((x) => x > 1)`,
		"two arguments": `
struct Pair<k, v> { a: k, b: v }
trait Swap { swap: (Self<a, b>) -> Self<b, a> }
impl Swap for Pair<k, v> { swap = (self) => Pair { a: self.b, b: self.a } }
let a = () -> string => Pair { a: 1, b: "x" }.swap().a`,
		// The impl's `a` and the method's `a` are different variables, inside `Self<…>` too.
		"impl variable named like the method's": `
struct Box<a> { v: a }` + functor + `
impl Functor for Box<a> { map = (self, f) => Box { v: f(self.v) } }
let a = () -> bool => Box { v: 3 }.map((x) => x > 1).v`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// Every other target is refused at the impl, rather than given a reading a later rule would
// have to break: `Result<t, e>` could map either argument, and `i64` has no head to apply.
func TestTraitSelfApplied_Refused(t *testing.T) {
	t.Parallel()
	const functor = `
trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
`
	want := func(target string, n int) string {
		return "impl of Functor for " + target + `: method "map" writes Self with 1 type argument(s), so the target must be a generic type applied to 1 distinct type variable(s), like Box<t>`
	}
	cases := map[string]struct{ src, want string }{
		"not generic": {functor + `
impl Functor for i64 { map = (self, f) => self }`, want("i64", 1)},
		"more arguments than Self takes": {`
data Res<t, e> = Good(t) | Bad(e)` + functor + `
impl Functor for Res<t, e> { map = (self, f) => self }`, want("Res<t, e>", 1)},
		"a concrete argument": {`
struct Box<t> { v: t }` + functor + `
impl Functor for Box<i64> { map = (self, f) => self }`, want("Box<i64>", 1)},
		"a repeated variable": {`
struct Pair<k, v> { a: k, b: v }
trait F2 { m: (Self<a, b>) -> i64 }
impl F2 for Pair<t, t> { m = (self) => 1 }`,
			`impl of F2 for Pair<t, t>: method "m" writes Self with 2 type argument(s), so the target must be a generic type applied to 2 distinct type variable(s), like Box<t>`},
		"a default method": {`
struct Box<t> { v: t }
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  twice: (Self<a>, (a) -> a) -> Self<a> = (self, f) => self.map(f).map(f)
}
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }`,
			"Functor::twice: a default method cannot write Self with type arguments yet; implement it in each impl"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

// A lambda whose parameter nothing types infers as `(?) -> string`, and unifying that bound a
// variable to nil, which the solver counted as solved: a generic function's call type-checked
// and failed in the backend (`unknown type: <nil>`), a trait method's panicked.
func TestGenericCall_NilBindingIsNotASolve(t *testing.T) {
	t.Parallel()
	res := parseCollectAndCheck(t, `
let app2<a, b> = (f: (a) -> b) -> i64 => 0
let r = () -> i64 => app2((x) => "s")`, false)
	assertErrorsAre(t, res, "app2: cannot infer type variables a, b from these arguments")
}
