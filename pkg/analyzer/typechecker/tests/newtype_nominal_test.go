package typechecker_test

import "testing"

// Keeping a newtype's wrapper across a call boundary must not make it less **nominal** —
// nominal identity is the whole reason newtype exists.
//
// The one deliberate asymmetry: a base a conversion cannot *name* — an array, a function
// type — keeps its implicit read-out, because refusing with no spelling to offer would make
// the newtype write-only. So `Bag` flows to a `[]string` parameter and `Cents` does not flow
// to an `i64` one; the second has `i64(...)` to point at and the first has nothing.
func TestTypeCheck_NewtypeStaysNominalAcrossCalls(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		// Passed before the fix as well — the base was never implicitly admitted. Kept
		// because it is the half a change like this could plausibly loosen.
		{"base does not flow into a newtype", `
newtype Bag = []string
let take = (b: Bag) -> i64 => 1
let xs: []string = ["x"]
let out = take(xs)`,
			"cannot use DynamicArray<string> as Bag implicitly: Bag is a distinct type over DynamicArray<string>, so the conversion must be written — `Bag(...)`"},
		// Two newtypes over one base are distinct. The diagnostic names *both* newtypes,
		// which it could not do while the wrapper was being lost — it used to describe the
		// base instead.
		{"two newtypes over one base", `
newtype Bag = []string
newtype Sack = []string
let take = (s: Sack) -> i64 => 1
let b: Bag = ["x"]
let out = take(b)`,
			"take: argument 1 (s): cannot assign Bag to Sack"},
		// Also passed before: a scalar base never lost its wrapper.
		{"a scalar newtype needs its read-out written", `
newtype Cents = i64
let take = (n: i64) -> i64 => n
let c: Cents = 5
let out = take(c)`,
			"cannot use Cents as i64 implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `i64(...)`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertErrorsAre(t, parseCollectAndCheck(t, c.src, false), c.want)
		})
	}
}

// The documented asymmetry, pinned so it is a decision rather than an accident: an array
// base has no conversion spelling, so its read-out stays implicit.
func TestTypeCheck_NewtypeOverAnArrayKeepsItsImplicitReadOut(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Bag = []string
let take = (xs: []string) -> i64 => 1
let b: Bag = ["x"]
let out = take(b)
`, false))
}
