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

// Through a `where` bound the method's own variables are solved the same way, in the enclosing
// body's vocabulary — `b = string` from a lambda, `b = u` from a parameter, `b = t` from the
// receiver itself — and composed with each specialization after checking. They were refused
// by name until 09/14/26.
func TestTraitMethodTypeVars_ThroughABound(t *testing.T) {
	t.Parallel()
	const mapper = `
trait Mapper { mapv: (Self, (i64) -> b) -> b }
impl Mapper for i64 { mapv = (self, f) => f(self) }
`
	cases := map[string]string{
		"solved concretely": mapper + `
let twice<t> where t: Mapper = (v: t) -> string => v.mapv((x) => "n")`,
		"solved as the caller's variable": mapper + `
let viaB<t, u> where t: Mapper = (v: t, f: (i64) -> u) -> u => v.mapv(f)`,
		"solved as the receiver's variable": `
trait M2 { m2: (Self, (Self) -> b) -> b }
impl M2 for i64 { m2 = (self, f) => f(self) }
let idm<t> where t: M2 = (v: t) -> t => v.m2((x) => x)`,
		// The receiver's `b` and `mapv`'s own `b` are two variables.
		"receiver variable named like the method's": mapper + `
let go<b> where b: Mapper = (v: b) -> i64 => v.mapv((x) => x + 1)`,
		"in a default method": mapper + `
trait Loud: Mapper { loud: (Self) -> string = (self) => self.mapv((x) => "L") }
impl Loud for i64`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

func TestTraitMethodTypeVars_ThroughABoundRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ src, want string }{
		"unsolvable": {`
trait Mk { mk: (Self) -> []b }
impl Mk for i64 { mk = (self) => [] }
let g<t> where t: Mk = (v: t) -> i64 => { let xs = v.mk(); 0 }`, "Mk::mk: cannot infer type variable b from these arguments"},
		// A bare variable has no head for `Self<b>` to apply.
		"Self with type arguments": {`
struct Box<t> { v: t }
trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
let g<f> where f: Functor = (v: f) -> i64 => { let w = v.map((x) => x); 0 }`,
			"Functor::map: a method writing Self with type arguments cannot be called through a `where` bound; call it on a concrete receiver"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
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
		return "impl of Functor for " + target + `: method "map" writes Self with 1 type argument(s), so the target must be a generic type applied to 1 distinct type variable(s), like Box<t>, or mark the positions with holes, like Result<_, e>`
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
			`impl of F2 for Pair<t, t>: method "m" writes Self with 2 type argument(s), so the target must be a generic type applied to 2 distinct type variable(s), like Box<t>, or mark the positions with holes, like Result<_, e>`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

// A default method may write `Self<…>`: its body is checked once with `Self` the abstract
// head, so `Self<a>` is a type of its own there — `self.map(f)` dispatches through Self's
// bound, the receiver's arguments seed `map`'s own `a`, and the result is `Self<b>` at
// whatever `b` solved to. Refused by name until 09/15.
func TestTraitSelfApplied_DefaultMethod(t *testing.T) {
	t.Parallel()
	const box = `
struct Box<t> { v: t }
`
	accepted := map[string]string{
		"chained through the trait's own method": box + `
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  twice: (Self<a>, (a) -> a) -> Self<a> = (self, f) => self.map(f).map(f)
}
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
let a = () -> i64 => Box { v: 3 }.twice((x) => x * 2).v`,
		"a concrete argument to Self": box + `
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  flag: (Self<a>) -> Self<bool> = (self) => self.map((x) => true)
}
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
let a = () -> bool => Box { v: "s" }.flag().v`,
		"changing the argument": box + `
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  lens: (Self<string>) -> Self<i64> = (self) => self.map((s) => s.len())
}
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }
let a = () -> i64 => Box { v: "abc" }.lens().v`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	// `Self<a>` and `Self<b>` are different types inside the body: the head is the same
	// abstract `Self`, so the arguments decide.
	refused := map[string]struct{ src, want string }{
		"returning the receiver where a new argument is declared": {box + `
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  bad: (Self<a>, (a) -> b) -> Self<b> = (self, f) => self
}
impl Functor for Box<t> { map = (self, f) => Box { v: f(self.v) } }`,
			"bad: return type mismatch: expected Self<b>, got Self<a>"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

// A hole in an impl target marks the position a `Self<…>` argument fills: `Result<_, e>` at
// `Self<b>` is `Result<b, e>`, so a two-parameter type can implement a one-argument trait
// without a positional convention deciding which parameter varies. What is not a hole stays
// as the impl fixed it, so a concrete argument is fine beside one (09/15).
func TestTraitSelfApplied_Hole(t *testing.T) {
	t.Parallel()
	const functor = `
trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
`
	accepted := map[string]string{
		"the first parameter varies": `
data Res<t, e> = Good(t) | Bad(e)` + functor + `
impl Functor for Res<_, e> { map = (self, f) => match self { Good(v) => Good(f(v)), Bad(x) => Bad(x) } }
let a = () -> bool => {
  let r: Res<i64, string> = Good(3)
  match r.map((x) => x > 1) { Good(b) => b, Bad(_) => false }
}`,
		"the last parameter varies": `
struct Table<k, v> { key: k, val: v }` + functor + `
impl Functor for Table<k, _> { map = (self, f) => Table { key: self.key, val: f(self.val) } }
let a = () -> string => Table { key: 1, val: "x" }.map((s) => s ++ "y").val`,
		"a concrete argument beside the hole": `
struct Table<k, v> { key: k, val: v }` + functor + `
impl Functor for Table<string, _> { map = (self, f) => Table { key: self.key, val: f(self.val) } }
let a = () -> bool => Table { key: "k", val: 2 }.map((n) => n > 1).val`,
		"a default method through a holed target": `
data Res<t, e> = Good(t) | Bad(e)
trait Functor {
  map: (Self<a>, (a) -> b) -> Self<b>
  twice: (Self<a>, (a) -> a) -> Self<a> = (self, f) => self.map(f).map(f)
}
impl Functor for Res<_, e> { map = (self, f) => match self { Good(v) => Good(f(v)), Bad(x) => Bad(x) } }
let a = () -> i64 => {
  let r: Res<i64, string> = Good(3)
  match r.twice((x) => x + 1) { Good(v) => v, Bad(_) => 0 }
}`,
	}
	for name, src := range accepted {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
	refused := map[string]struct{ src, want string }{
		// A bare `Self` says nothing about what fills the hole.
		"a bare Self through a holed target": {`
data Res<t, e> = Good(t) | Bad(e)
trait Same { same: (Self) -> Self }
impl Same for Res<_, e> { same = (self) => self }`,
			`impl of Same for Res<_, e>: the target has a hole, but method "same" writes a bare Self, which does not say what fills ` + "`_`" + `; a hole serves only a trait whose methods write Self<…>`},
		"more holes than Self takes": {`
struct Pair<k, v> { a: k, b: v }` + functor + `
impl Functor for Pair<_, _> { map = (self, f) => self }`,
			`impl of Functor for Pair<_, _>: method "map" writes Self with 1 type argument(s), so the target must have 1 hole(s)`},
	}
	for name, tc := range refused {
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
