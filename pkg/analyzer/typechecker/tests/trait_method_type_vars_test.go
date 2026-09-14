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
