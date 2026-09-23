package typechecker_test

import "testing"

// **A parameter's storage flavor is concrete even when unwritten**, and a call across that
// boundary is refused rather than miscompiled.
//
// Until 09/23 both directions reached the backend and read the wrong shape. A `shared`
// argument in a plain parameter trapped at runtime (`match not exhaustive` — the tag read
// out of a box pointer), and a plain argument in a `shared` parameter **segfaulted**, an
// inline aggregate dereferenced as a box. Neither was reported, and the `own` path was no
// better: `checkAllocationCompat` fires only when *both* flavors are concrete, and an
// unwritten one is Unspecified.
//
// The old comment at the argument site said a borrowed parameter "is allocation-polymorphic
// and is skipped". That is the bug in one line. A binding really is polymorphic —
// `let q: E = s` inherits, and the backend unboxes for it — but a function is compiled once
// and its parameter has exactly one representation, so at a call there is no context left
// to inherit from.
func TestArgumentAllocation_CrossingIsRefusedNotMiscompiled(t *testing.T) {
	for _, c := range []struct{ name, source, want string }{
		{
			"a shared value into a plain parameter",
			`data E = Lit(i64) | Pair(i64)
let f = (e: E) -> i64 => match e { Lit(n) => n, Pair(m) => m }
let main = () -> u8 => {
  let s: shared E = Lit(1)
  u8(f(s))
}`,
			"f: argument 1 (e): this is a `shared E` and the parameter takes a plain one — declare the parameter `shared E`, since a function has one representation and cannot take either",
		},
		{
			"a plain value into a shared parameter",
			`data E = Lit(i64) | Pair(i64)
let f = (e: shared E) -> i64 => match e { Lit(n) => n, Pair(m) => m }
let main = () -> u8 => {
  let s: E = Lit(2)
  u8(f(s))
}`,
			"f: argument 1 (e): a `shared E` parameter takes a `shared` value, and this one is not — bind it `shared` where it is built, or construct it in the argument",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.source, false)
			assertErrorsAre(t, res, c.want)
		})
	}
}

// The two shapes that must **not** be refused, because each is genuinely polymorphic.
//
// A construction has no flavor of its own, so an argument position gives it one — that is
// what makes `f(Lit(3))` against a `shared` parameter correct rather than a crossing. A
// **match** is the same thing one level up: its arms are constructions that took the
// flavor, so the match is a `shared` value too, and the node now records that. It did not
// until 09/23, which cost nothing while nothing read it and produced a false report the
// moment this check asked.
//
// A **generic** parameter is polymorphic for a different reason: the call monomorphizes the
// body, so `t` takes whatever flavor this instantiation binds. Refusing here would reject
// every generic function called with a `shared` value, which is most of the prelude.
func TestArgumentAllocation_PolymorphicPositionsAreNotRefused(t *testing.T) {
	for _, c := range []struct{ name, source string }{
		{
			"a construction takes the parameter's flavor",
			`data E = Lit(i64) | Pair(i64)
let f = (e: shared E) -> i64 => match e { Lit(n) => n, Pair(m) => m }
let main = () -> u8 => u8(f(Lit(3)))`,
		},
		{
			"a match whose arms take it",
			`data E = Lit(i64) | Pair(i64)
let f = (e: shared E) -> i64 => match e { Lit(n) => n, Pair(m) => m }
let main = () -> u8 => u8(f(match 1 { 0 => Lit(1), _ => Pair(9) }))`,
		},
		{
			"a generic parameter stays polymorphic",
			`data E = Lit(i64) | Pair(i64)
let id<t> = (x: t) -> t => x
let main = () -> u8 => {
  let s: shared E = Lit(4)
  match id(s) { Lit(n) => u8(n), Pair(_) => 0 }
}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.source, false)
			assertNoErrors(t, res)
		})
	}
}
